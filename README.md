# omni-example

Agentic analytics harness that routes data questions to models of matching strength and measures cost against accuracy. Postgres holds the ecommerce data. Go 1.25, module github.com/KranzL/omni-example. The benchmark has 32 questions in bench/questions.yaml (8 easy, 8 moderate, 8 hard, 8 expert) with ground-truth answers in bench/answers.json.

## Architecture

```
bench/questions.yaml (32 questions) + bench/answers.json (ground-truth answers)
        |
        v
harness bench run --config ROUTER          internal/router picks one tier per question
        |                                  (or cascade: cheap, then mid, then top)
        v
internal/agent loop on that tier           cached system prompt: instructions plus
        |                                  semantic/ecomm.yaml render; tools run_sql
        v                                  and submit_answer; at most 8 turns
internal/bench grades the submitted        lenient verdict plus strict verdict,
answer against the ground truth            per-type rules in internal/bench/grade.go
        |
        v
results/<config>/<stamp>.jsonl             one line per question per repeat, then
+ results/<config>/latest.json             harness report aggregates every config
        |
        v
results/summary.md + pareto.svg            cost versus accuracy table, wrong answers,
+ difficulty.svg + README table            Pareto chart, per-difficulty bars
```

semantic/ecomm.yaml is the semantic layer: tables, enums, metrics, join paths, synonyms, conventions, gotchas and worked examples. internal/agent renders it into the cached system prompt. internal/router holds the baselines, the heuristic, classifier, embedding and Jev routers, and the cascades. internal/bench holds the runner, the grader, the rubric judge, compare, regress and drift. internal/report reads every latest.json and writes the summary and the charts. internal/llm holds the Anthropic and Venice providers with the pricing tables.

## Run

Copy .env.example to .env and fill the values. .env is gitignored; .env.example lists the names with empty values. The harness reads .env from the working directory, or OMNI_ENV_FILE when set, or the main checkout's .env when run from a worktree.

| variable | needed for | notes |
|---|---|---|
| DATABASE_URL | db-check, bench validate, ask, bench run | a Postgres URL for the ecommerce database, postgres://USER:PASSWORD@HOST:5432/DBNAME |
| ANTHROPIC_API_KEY | any Anthropic run | cheap, mid and top tiers, classifier, verifier, judge |
| VENICE_API_KEY | any Venice run and the embedding router | also serves the bge-m3 embeddings on either provider |
| JEV_API_KEY | jev-classifier, cascade-verify-jev, jev compare | TypeSafe endpoint, 0.042 USD per million input tokens |
| LLM_PROVIDER | default backend | anthropic (default) or venice; --provider overrides per command |
| ANTHROPIC_WORKSPACE_ID | optional | read when present |
| VENICE_MODEL_CHEAP | optional model override | drops the tier's reasoning setting |
| VENICE_MODEL_MID | optional model override | drops the tier's reasoning setting |
| VENICE_MODEL_TOP | optional model override | drops the tier's reasoning setting |

make build writes bin/harness. make vet runs go vet ./.... make test runs go test -race ./.... make fmt-check fails when gofmt -l lists any file. make run executes harness db-check.

End to end, in order: make build, then harness db-check (database reachable), then harness bench validate (ground-truth answers match the database), then harness ask "question" --tier mid (one question), then harness bench run --config always-mid (full 32), then harness bench compare, regress or drift (compare runs), then harness bench judge --run FILE (rubric second opinion), then harness report (summary table and charts).

harness db-check prints row counts for the six relations in schema public (users, order_items, events, inventory_items, products, distribution_centers) and shows the write guard rejecting an UPDATE. On 2026-09-23 the counts were users 156781, order_items 251191, events 5311537, inventory_items 261381, products 29120, distribution_centers 10.

harness bench validate runs the 32 ground-truth queries in bench/questions.yaml and writes bench/answers.json. Each query runs read-only with a 30s statement timeout.

Tests that need DATABASE_URL skip when the variable is absent. To run them with the file values, run set -a, then source .env, then set +a, then make test.

## Models and cost accounting

internal/llm wraps github.com/anthropics/anthropic-sdk-go with three tiers. cheap is claude-haiku-4-5 with thinking disabled and max_tokens 4096. mid is claude-sonnet-5 and top is claude-opus-5-5, both with adaptive thinking, effort medium and max_tokens 16000. tool_choice is auto whenever tools are passed. Settings per tier live in llm.DefaultSettings and can be replaced with Client.SetSettings.

The pricing table in internal/llm/pricing.go is keyed by model ID, in dollars per million tokens, checked against the Anthropic pricing page on 2026-09-23:

| model | input | output | 5m cache write | 1h cache write | cache read |
|---|---|---|---|---|---|
| claude-haiku-4-5 | 1.00 | 5.00 | 1.25 | 2.00 | 0.10 |
| claude-sonnet-5 | 2.00 | 10.00 | 2.50 | 4.00 | 0.20 |
| claude-opus-5-5 | 4.00 | 20.00 | 5.00 | 8.00 | 0.20 |

Responses report Haiku as claude-haiku-4-5-20251001, so lookups strip a trailing -YYYYMMDD. Every call produces an llm.CallRecord with model, tier, purpose, usage, cost, latency, stop reason, request ID and attempt count. The SDK retries 408, 409, 429, 5xx and connection errors up to 4 times. llm.WriteJSONL appends records to results/traces/NAME.jsonl, which is gitignored.

harness models sends one short prompt to each tier and prints tokens by category, cost and latency. On 2026-09-24 the three calls cost $0.000760 in total.

## Agent loop

internal/agent runs one question on one tier. agent.Run sends the question as the first user message and loops on stop_reason for at most 8 model turns. The system prompt is two text blocks: fixed harness instructions (agent.Instructions), then the rendered semantic layer with an ephemeral cache_control marker. Tools are sent in a fixed order: run_sql (one SELECT or WITH statement, returns row count, a capped flag at 100 rows, and CSV, or the Postgres error text with is_error true) and submit_answer (strict schema: answer, sql, confidence high|medium|low). Nothing before the cache breakpoint varies per request, so the tools plus system prefix is byte-identical across questions. A second breakpoint moves to the last block of the newest user message each turn, so later turns in a run read earlier turns from cache.

If the model stops without calling submit_answer, or hits the turn cap, the loop sends one nudge message. If it still does not submit, the result records the failure and uses the last text as the answer. agent.Result holds answer, SQL, confidence, turns, SQL error count, every SQL step, every llm.CallRecord, total usage and cost, wall time, and cache read and cache creation totals.

harness ask "question" --tier cheap|mid|top [--runs N] prints the answer, SQL, turns, tokens by category per call, cost and latency. It appends the call records to results/traces/ask-TIER-TIMESTAMP.jsonl and writes the full results to the matching .result.json. --capture-requests writes each rendered request body to results/requests/NNNN.json with a sequence number that resumes past existing files, using SDK middleware on Anthropic and a payload save on Venice. harness bench run accepts the same flag. results/requests/ is gitignored.

On 2026-09-24, "How many users signed up in 2024?" returned 34530 on all three tiers, which matches psql (SELECT COUNT(*) FROM users WHERE created_at >= '2024-01-01' AND created_at < '2025-01-01'). Each tier was run twice in a row with --runs 2:

| tier | run | cache_write | cache_read | cost |
|---|---|---|---|---|
| cheap | 1 | 463 | 13271 | $0.003080 |
| cheap | 2 | 135 | 13600 | $0.002708 |
| mid | 1 | 9318 | 9201 | $0.027533 |
| mid | 2 | 0 | 18519 | $0.006102 |
| top | 1 | 9259 | 9133 | $0.053126 |
| top | 2 | 0 | 18392 | $0.008682 |

The cheap run 1 already read from cache because the integration test had warmed the prefix minutes earlier. When this table was recorded the cached prefix was about 6800 tokens on Haiku and about 9200 on Sonnet and Opus, above every tier's minimum cacheable length (4096 on Haiku).

TestIntegrationCheapTierReadsCache in internal/agent runs a question twice on the cheap tier and fails if the second run's first call reads zero cached tokens. It skips unless ANTHROPIC_API_KEY and DATABASE_URL are exported, and costs about $0.013.

## Semantic layer

semantic/ecomm.yaml describes the six tables (68 column descriptions), 11 enum lists, 11 named metrics with SQL, 6 named join paths, 4 synonyms, 10 conventions, 6 gotchas and 14 worked question and SQL examples. internal/semantic renders it into one text block deterministically (no timestamps), and the agent, classifier and verifier each read that text after their own instructions.

harness semantic render prints the render with token counts. harness semantic render --context-levels model prints only the tables skeleton with conventions and gotchas; --context-levels model,topic adds metrics, paths and worked examples; the default all adds column descriptions, enum values and synonyms. As of the context-loop gotchas the full render is 6516 tokens on claude-haiku-4-5 and, with instructions, the cached prefix is 6900 tokens on Haiku. Turn-1 cache reads, which also cover the tools and the question, are about 10300 tokens on Sonnet (e01 in always-mid 20260924T162945Z reads 20664 over two turns) and 10145 on Opus (h02 in heuristic-v2 20260924T183941Z). The model-only render was 1545 tokens when first measured, before the context-loop gotchas added 144, so the harness pads the instructions with fixed neutral sentences (no schema facts) to keep the prefix above the 4096-token caching minimum.

The gotchas section fixed the systematic x04 miss on Anthropic: always-mid went from 31/32 to 32/32 after the 3-bullet addition, with the SQL changing from the CASE form to the prescribed WHERE form. The join paths and synonyms name the 6 edges between the tables; they changed SQL shape (h02 switched to the prescribed join) but no verdict. The context-levels ablation ran always-mid with each level on both providers: model plus topic scores 32/32 on Anthropic and on Venice, identical to all levels, while model alone scores 26/32 and 28/32; the field level buys nothing measurable on these 32 questions because their wording already matches schema terms.

## Benchmark runner and grader

harness bench run --config always-cheap|always-mid|always-top|heuristic|heuristic-v2|classifier|embedding|jev-classifier|cascade-signals|cascade-verify-haiku|cascade-verify-sonnet|cascade-verify-jev [--set starter|full] [--repeat N] [--concurrency K] [--only ID[,ID...]] [--difficulty easy,moderate,hard] [--shadow RATE] [--provider anthropic|venice] [--context-levels model,topic,field] [--tag TAG] [--no-cache] [--capture-requests] [--route-context] [--cheap-thinking BUDGET] runs the 32 questions in bench/questions.yaml against the ground truth in bench/answers.json. The first question runs serially to warm the prompt cache, then the rest run with the given concurrency (default 3). Each question is retried once on a transport error and never on a wrong answer. --only runs the listed questions, --difficulty filters to the 24 non-expert questions, --shadow sends a share of Jev-gated decisions to the fallback as well, --no-cache drops the cache markers on agent, classifier and verifier requests, --capture-requests writes each rendered request body for the caching audit, --route-context appends a gotcha and a worked example to questions a rule flags as per-user sequencing (cmd/harness/route_context.go), and --cheap-thinking BUDGET turns on budget thinking for the cheap tier only. Both new flags need --tag, so their runs land in their own folder.

internal/bench grades by answer_type with lenient rules and a strict verdict kept. Numbers parse the first number in the answer and compare within the question's relative tolerance. Numbers may carry a leading $ and comma thousands separators. Strings compare trimmed and case-insensitive with surrounding quotes ignored, and a trailing parenthetical on the answer is ignored when the expected value has none. Lists compare as order-insensitive sets and ranked lists in order, comparing the label part when an answer line has label colon value shape and the expected items contain no colon, stripping a trailing parenthetical, and matching expected YYYY-MM against answer YYYY-MM-DD or YYYY-MM-DD HH:MM:SS by prefix. Tables match rows by first-column label with the same month prefix rule and compare numeric cells by extracting the numbers from the value part in order within tolerance, with the count of numbers equal to the expected numeric column count. Tables also accept CSV rows (label, then value fields), skipping a header line; that is a lenient pass and a strict fail with format_reason csv_rows. free_text questions go to an LLM judge on the cheap tier with the ground truth in the prompt; no current question uses free_text.

Each run writes results/<config>/<timestamp>.jsonl with one line per question per repeat (id, difficulty, tier, correct, correct_strict, answer, expected, sql, turns, sql_errors, tokens by category, cost, latency, cache hit ratio) and rewrites results/<config>/latest.json with overall and per-difficulty accuracy and accuracy_strict, total cost, cost per correct answer, and mean and p95 latency. Each run also writes results/traces/<run folder>-<timestamp>.jsonl (the output folder name with its suffixes) with one line per model call (agent turns, routing, verifier and judge calls), each tagged with its question id, repeat and attempt tier. Runs filtered with --only or --difficulty write to results/[provider/]<config>-subset/, --shadow runs to <config>-shadow, and --tag adds -TAG, so a partial run never replaces the full-set latest.json. When the context is cancelled or more than half the lines carry an agent error, the run keeps the .jsonl, leaves latest.json alone and exits non-zero. A failed agent attempt's cost, tokens and latency are added to the retry's, so a line carries all spend. Jev-gated shadow calls are reported in shadow_cost_usd on the line and summary and are not included in total cost. The harness changes directory to the repo root (the parent of bench/) at startup, so results/ is the same from any subdirectory. bench judge, jev compare and ask exit non-zero when every line or run failed.

Results dated before 2026-09-24T02:35Z predate the numeric rendering fix and are kept for the record only.

On 2026-09-24, one repeat of each single-model baseline on 32 questions (24 blind plus 8 expert calibrated against the cheap tier). always-top below is the current latest.json file. always-mid below is run 20260924T055046Z; results/always-mid/latest.json now points at 20260924T162945Z, the rerun after the context-loop gotcha (32/32 lenient and 30/32 strict after the 2026-09-25 regrade that accepts CSV table rows; 30/32 lenient as first recorded; $0.363103). always-cheap latest.json now points at a second repeat plus the regrade; the first repeat is the file named below:

| config | accuracy | accuracy_strict | easy | moderate | hard | expert | total cost | per correct |
|---|---|---|---|---|---|---|---|---|
| always-cheap (20260924T034617Z, regraded) | 0.844 (27/32) | 0.750 (24/32) | 8/8 | 8/8 | 8/8 | 3/8 | $0.183458 | $0.006795 |
| always-cheap (latest, regraded) | 0.844 (27/32) | 0.750 (24/32) | 8/8 | 8/8 | 8/8 | 3/8 | $0.247600 | $0.009170 |
| always-mid (20260924T055046Z) | 1.000 (32/32) | 0.906 (29/32) | 8/8 | 8/8 | 8/8 | 8/8 | $0.395770 | $0.012368 |
| always-top | 1.000 (32/32) | 0.906 (29/32) | 8/8 | 8/8 | 8/8 | 8/8 | $0.634884 | $0.019840 |

The expert tier separates the tiers where the blind tiers do not: cheap scores 3 of 8 on expert, mid 8 of 8 and top 8 of 8. Cheap missed x01, x03, x05, x06 and x08 (all order-gap and median questions); h08 (month names instead of YYYY-MM) failed in the first repeat and passes under the current grader after harness bench regrade --verdicts. Mid missed only x04 (148.08 instead of 149.95, the same CASE-versus-WHERE mistake cheap made in calibration) before the gotchas addition fixed it. Top got every expert question. An earlier always-top 32-question attempt on 2026-09-24T03:48Z failed after 8 questions when API credits ran out; its file is kept for the record and latest.json points at the successful rerun.

The expert tier was calibrated adversarially against claude-haiku-4-5: 33 candidates ran twice on cheap and twice on top, and only the 8 that cheap missed at least once while top got at least once were kept. The kept 8 are all per-user sequencing questions, 5 of them one template (days between a user's nth and mth order: median for x01, x03, x05 and x08, 95th percentile for x07) applied to different cohorts. The dropped 25, covering thresholds, percentiles over orders, session funnels, two-period comparisons and month rankings, were solved by the cheap tier every time.

Strict deviations on substantively right answers: ranked lists with counts appended (m02, m08) and unit words inside table values (h08 on cheap). Ground truth h03 returns distribution_center and margin only.

## Omni metrics and comparison

The harness takes its metrics and eval loop from four Omni posts and from BIRD; the Omni alignment note under Decisions says what was adopted and what was done differently.

Every latest.json carries an omni block next to the existing fields, and harness bench run prints it. The metrics, by Omni's names:

- accuracy by difficulty: lenient correct over result lines, overall and per difficulty, with the strict verdict alongside (accuracy, accuracy_strict, by_difficulty).
- cost per correct answer: agent, routing, verifier and judge cost over lenient correct answers (cost_per_correct_usd).
- tokens per correct answer: input, cache write, cache read and output tokens of agent and routing calls over lenient correct answers (omni.tokens_per_correct).
- median response time: median wall time per result line including routing, with mean and p95 kept (omni.median_latency_ms).
- error-free response rate: share of result lines with no agent error, no SQL error and a submitted answer (omni.error_free_rate).
- consistency across runs: with two or more repeats, share of questions whose lenient verdict is the same in every repeat (omni.consistency_rate).
- answers changed between runs: with two or more repeats, questions whose lenient verdict differs across repeats (omni.answers_changed); omni.answer_texts_changed counts differing answer text.

harness bench compare A B [--provider P] prints two runs side by side and lists every question whose verdict flipped, with each side's answer and cost. A and B are run .jsonl files, latest.json files or config names. harness bench metrics [latest.json ...] adds the omni block to existing latest.json files from their source run files, changing no other field and making no API calls; with no arguments it updates every results/*/latest.json and results/*/*/latest.json.

## Eval loop

This follows the loop in Omni's evals post (https://omni.co/blog/run-your-agent-like-a-data-product-with-ai-evals): a small set of real questions, a pass or fail per question with a written reason, a change on a branch compared against the last good run, and the same runs read over time as monitoring.

Per-question reasons. Every result line with a lenient fail carries fail_reason, and every line that passes lenient but fails strict carries format_reason. Both are derived in code from the ground-truth answer and the submitted answer (internal/bench/reason.go), not by a model. The text starts with a code and names the cause:

- fail_reason codes: agent_error (with the error text), no_answer (no submit_answer call, with the agent's last text), no_number and wrong_number (got, want, percent off and the tolerance), wrong_value (got and want), wrong_items (missing and extra list items by name), wrong_order (first ranked position that differs), wrong_row_labels (missing and extra table labels), wrong_row_values (rows whose numbers differ, with both values), unparsed_rows, judge_rejected (free_text only), and stale_verdict (the recorded fail predates a grader change and the current grader passes the answer).
- format_reason codes: count_appended (a count or value after a list label), annotation (a non-numeric parenthetical), month_format (month names or full dates for a YYYY-MM key), unit_words (words, $ or % inside table values), csv_rows (a table given as comma-separated rows) and number_format (including $ or thousands separators on a number answer).

latest.json carries fail_reasons and format_reasons with a count per code, and harness bench run writes them for new runs. harness bench regrade [run.jsonl ...] adds the reasons to committed run files without re-running any model and keeps every recorded verdict: it inserts the two fields after correct_strict, leaves every other byte of the line as it was, and updates the counts in each latest.json that points at a regraded file. With no arguments it regrades every run file under results/ except results/jev-shadow. On 2026-09-24 it regraded 34 files: fail reasons wrong_number 37, agent_error 24 (the always-top run that hit the credit limit), stale_verdict 14, wrong_value 8, wrong_row_values 5, no_answer 4, wrong_row_labels 4, wrong_items 1; format reasons count_appended 63, unit_words 18, month_format 16. Lines written before the strict verdict existed get no format_reason.

harness bench regrade --verdicts [run.jsonl ...] also recomputes correct and correct_strict with the current grader, including the per-attempt verdicts of cascade runs, and rewrites only those fields and the reasons; answers, SQL, costs and every other byte stay as recorded. Free-text judge verdicts and lines with an agent error keep their recorded verdict. Each latest.json and run summary that points at a regraded file is recomputed from the regraded lines and gets a regraded block with the time and each changed verdict. results/judge is skipped. On 2026-09-24 it regraded 40 run files: 14 correct verdicts went from false to true (the 14 stale_verdict lines), 0 correct_strict verdicts changed. 3 of them sit in files that latest.json points at: h08 in always-cheap and cascade-signals, m07 in cascade-verify-jev; the other 11 are in superseded runs.

Starter set. bench/starter.yaml holds the ten questions a user asks first, chosen from the 32: m01 revenue by category, m07 orders per month, m04 average order value, m08 top categories, m06 revenue by traffic source, m03 return rate, h08 the acquisition-month cohort, e05 new users, e04 cancelled items and e07 purchase events. The full 32 in bench/questions.yaml is the grown set. harness bench run --config NAME --set starter runs only those ten and writes to results/[provider/]<config>-starter/ so it never replaces the full-set latest.json. From the committed runs, the same ten questions cost $0.0501 on always-cheap on Anthropic, $0.0072 on always-mid on Venice and $0.0130 on always-cheap on Venice (the Venice cheap model takes more turns).

Regression gate. harness bench regress --baseline A --candidate B [--max-drop N] [--max-flips M] [--provider P] compares the two runs question by question. A baseline question missing from the candidate is listed, and when it passed in the baseline it counts as a pass-to-fail flip; candidates in a -starter or -subset folder are compared only on the questions they ran. A question passes when the majority of its repeats pass. The gate exits non-zero when the candidate passes more than N fewer questions than the baseline, or when more than M questions flip from pass to fail; both default to 0. It prints every flip in both directions with both answers and the failing side's reason. A and B take the same forms as bench compare: run .jsonl files, latest.json files or config names.

Drift. harness bench drift --config NAME [--provider P], or harness bench drift DIR, reads every run file for that config in timestamp order and prints accuracy, strict count and cost per run, then the questions whose verdict changed from the previous run, with the reason for each new fail. Runs with a different question count show added and removed counts. This is the monitoring half of the loop: a config that passed can slip between runs with no code change, as results/venice/always-mid shows (h02 fails on 20260924T053704Z and passes again on the next run).

The loop for a change:

```
git fetch origin && git switch -c my-change origin/main
# edit semantic/ecomm.yaml, a prompt or a router
./bin/harness bench run --config always-mid --provider venice --set starter
./bin/harness bench regress --baseline always-mid --provider venice --candidate results/venice/always-mid-starter/latest.json
./bin/harness bench compare always-mid results/venice/always-mid-starter/latest.json --provider venice
gh pr create --base main   # paste the compare table and the regress result into the body
```

regress runs against the latest.json committed on main, so run it before rebasing onto a newer main. When the starter set passes, run the full 32 on the configs the change touches and regress those too.

## Routers

--config names a router from internal/router: the three baselines always-cheap, always-mid and always-top, the heuristic router (weighted keyword and schema features, no model call) and heuristic-v2 (same features with the aggregation weight raised from 0.5 to 0.7), the classifier router (one cached claude-haiku-4-5 call that labels the question easy, moderate or hard), the embedding router (nearest of 36 labelled seed questions by Venice bge-m3 cosine similarity, 5-nearest similarity-weighted vote, mid fallback under 0.6), the jev-classifier router (Jev difficulty label with the Haiku classifier as fallback), and four cascades that start on cheap and escalate on signals from the agent's own run, three of them with a claude-haiku-4-5, claude-sonnet-5 or Jev-gated verifier. Routing cost is added to each question's cost, and a cascade charges every attempt, including abandoned ones. Current latest.json results on Anthropic, 32 questions:

| config | accuracy | total cost | routing cost | per correct |
|---|---|---|---|---|
| heuristic | 32/32 | $0.3672 | $0 | $0.01148 |
| heuristic-v2 | 64/64 (repeat 2) | $0.8548 | $0 | $0.01336 |
| classifier | 32/32 | $0.4734 | $0.0494 | $0.01480 |
| embedding | 32/32 | $0.5519 | $0.0002 | $0.01725 |
| jev-classifier | 32/32 | $0.5801 | $0.0262 | $0.01813 |
| cascade-signals | 26/32 | $0.2252 | $0 | $0.00866 |
| cascade-verify-haiku | 30/32 | $0.4425 | $0.1081 | $0.01475 |
| cascade-verify-sonnet | 30/32 | $0.4619 | $0.2118 | $0.01540 |
| cascade-verify-jev | 30/32 | $0.3444 | $0.0881 | $0.01148 |

heuristic-v2 scored 32/32 on both Anthropic repeats (mean accuracy 1.0000, stdev 0.0000, mean cost $0.4274; $0.4728 and $0.3820). The heuristic second repeat (20260924T184116Z) also scored 32/32 at $0.3672, cheaper than either heuristic-v2 repeat, so its latest.json shows 32/32 at $0.3672. Its first repeat (20260924T043628Z) missed x04 on mid with the CASE form (148.08); it ran before the gotchas section was added, and the second repeat, after gotcha 1, sent x04 to mid and got 149.95. The heuristic versus heuristic-v2 difference has not been measured on the same semantic layer with repeats. cascade-signals moved from 25/32 to 26/32 in the regrade (h08 now passes). The full table with Venice runs, tier mix and latency is in the Report section below.

The classifier and verifier both implement the router.Decider interface, which is what makes confidence-gated routing possible: a cheap decider answers first and a fallback runs only when its confidence is low.

## Jev

Jev (TypeSafe AI, model jev-1.13.0 behind the alias jev-latest) answers typed questions with a label, per-option probabilities and a confidence, instead of generated text. internal/jev is a standard library HTTP client for POST https://api.typesafe.ai/v1/systemone; input costs 0.042 USD per million tokens and output is free. internal/router/gated.go implements the Decider interface: it keeps the Jev label when its confidence is at or above the per-label threshold and otherwise calls the Haiku fallback, charging both calls.

harness jev compare --set seeds|bench [--difficulty easy,moderate,hard] [--provider anthropic|venice] [--concurrency K] runs Jev and the Haiku classifier side by side on the same inputs and prints agreement, both confusion matrices, latency and a threshold sweep. Thresholds were set from bench/router_seed.yaml (36 hand-labelled seeds): easy 0.85, moderate 0.70, hard 0.50, where Jev agreed with Haiku on 32/36 and with the hand labels on 34/36; and from a 38-attempt Venice shadow run for the verdict gate (accept 0.90, reject 0.80).

harness bench run --config jev-classifier and --config cascade-verify-jev run the two Jev routers. On the full 32 questions on Anthropic, jev-classifier scores 32/32 at $0.5801 ($0.01813 per correct, routing $0.0262) and cascade-verify-jev scores 30/32 at $0.3444 ($0.01148 per correct, routing $0.0881), missing x03 and x04 where the Jev verifier accepted a wrong cheap-tier answer. Jev never produced a confident reject in any run: it acts as a confident-accept filter that cut Haiku verifier calls from 25 to 6 on the 24-question set. On the 24-question compare Jev averaged 493 ms against 1514 ms for the Haiku classifier.

## Muse provider

Muse Code (Meta) is a full autonomous coding-agent CLI, not a raw completion API; it runs its own tool loop, sandbox and approval gate rather than being called by internal/agent. `harness sql "SELECT ..."` (cmd/harness/sql.go) is a guarded, standalone command that runs one statement through the exact same read-only guard, timeout and row cap as the agent's own run_sql tool; it is the only way Muse is allowed to touch the database. internal/muse/muse.go shells out to `muse exec` in an isolated workspace holding nothing but a copy of the harness binary (no source, no .env, no other provider's API keys), instructs it to answer using only that one command, and reads its structured final answer back via `--output-schema`.

`harness bench run --config always-muse --provider muse` runs the fixed model muse-spark-1.3-contributor (pricing: $0.10 / $0.20 per MTok in/out, $0.002 cached, owner-supplied) as a baseline alongside Anthropic and Venice. On the full 32-question set: 31/32 correct lenient (96.9 percent), 25/32 strict, $0.1225 total, $0.003951 per correct, cheaper than every Anthropic configuration but with markedly higher latency (mean 55s) since it runs its own multi-step agent loop per question. A first attempt with an 8-step cap (matched to this harness's own agent convention) was far too tight for Muse's step economy and produced an all-timeout 65.6 percent; the cap was raised before the result above.

## Rubric judge

harness bench judge --run FILE [--provider P] [--rubrics PATH] [--questions PATH] [--concurrency K] [--only ID] re-grades every line of an existing result file with a rubric judge in the style of Omni's published methodology, with no change to the recorded verdicts. Each of the 32 questions has a rubric in bench/rubrics.yaml (correct answer, failure criteria, close-but-wrong examples). The judge runs on the cheap tier and records a strict pass or fail with confidence and a one-sentence reason; a mid-tier supervisor reviews fail verdicts, low-confidence passes and passes the lenient grader marked wrong. Output goes to results/judge/<run folder>-<stamp>.jsonl plus a .summary.json with agreement counts and the disagreement table.

Over 96 judged lines (always-cheap, always-mid and classifier runs), the judge agreed with the lenient verdict on 91 and with the strict verdict on 93. All 8 disagreements are formatting-forgiveness cases (appended counts on m02 and m08); the human arbitration sides with lenient on all 8. Judging cost 0.268158 USD for 96 lines (0.0028 USD per line). The rubric judge is not worth its cost for grading this benchmark as it stands, because the deterministic grader is free, instant and consistent on machine-checkable answers.

## Venice provider

Venice AI serves the open-weight and hosted models as a second provider. LLM_PROVIDER in .env picks the backend (anthropic, the default, or venice), and --provider anthropic|venice (or --provider=X) on harness ask, harness models, harness bench run, compare, regress, drift, judge and context verify overrides it for one command. The Anthropic path is unchanged.

Venice only speaks the OpenAI chat completions format, so internal/llm/openai.go is a second implementation of llm.Provider built on net/http and encoding/json. It translates at the client boundary and internal/agent and internal/bench do not know which backend they talk to. The system text blocks become one system message joined with a blank line, with cache markers dropped. Assistant text plus tool_use blocks become one assistant message with tool_calls. Each tool_result becomes a role tool message with the matching tool_call_id. Responses map back to text and tool_use blocks, finish_reason tool_calls, stop and length map to stop_reason tool_use, end_turn and max_tokens, and prompt_tokens minus cached_tokens is reported as input tokens with cached_tokens as cache reads. Every request sets venice_parameters include_venice_system_prompt false, strip_thinking_response true and enable_web_search off. 429 and 5xx responses are retried with exponential backoff from 1 s up to 4 attempts, and the attempt count lands in the CallRecord, which also carries provider venice.

Default tiers, with Venice prices in USD per million tokens as listed on 2026-09-24:

| tier | model | reasoning | max_tokens | input | cached input | output |
|---|---|---|---|---|---|---|
| cheap | qwen3-5-9b | reasoning_effort none | 4096 | 0.10 | not listed, billed as input | 0.15 |
| mid | deepseek-v4-flash | reasoning_effort low | 16000 | 0.138 | 0.028 | 0.275 |
| top | qwen-3-7-plus | model default (no effort control) | 16000 | 0.50 | 0.05 | 2.00 |

VENICE_MODEL_CHEAP, VENICE_MODEL_MID and VENICE_MODEL_TOP replace a tier's model; an override drops the tier's reasoning setting and turns thinking off on the cheap tier. The pinned table is llm.VenicePrices. Cost is input times the input price plus cached tokens times the cached price plus output times the output price. harness venice-models fetches https://api.venice.ai/api/v1/models?type=text and prints live pricing and capabilities for the configured models, and fails when the pinned table differs.

Venice runs of harness bench run write under results/venice/<config>/, so Anthropic results under results/<config>/ are never overwritten. Every result line and latest.json carry provider and model. Free-text questions would be judged by the Venice cheap tier; no current question uses free_text.

Venice also serves claude-sonnet-5 (3.00 input, 15.00 output, 0.30 cached) and claude-opus-5-5 (4.80, 24.00, 0.24), 1.2 times the Anthropic prices, but no Haiku. Pointing VENICE_MODEL_MID and VENICE_MODEL_TOP at them would need their prices added to llm.VenicePrices under names that do not collide with the Anthropic table.

On 2026-09-24, one repeat of each single-model baseline on the 32 questions through Venice:

| config | model | accuracy | accuracy_strict | easy | moderate | hard | expert | total cost | per correct |
|---|---|---|---|---|---|---|---|---|---|
| always-cheap | qwen3-5-9b | 0.625 (20/32) | 0.531 (17/32) | 8/8 | 7/8 | 4/8 | 1/8 | $0.073900 | $0.003695 |
| always-mid | deepseek-v4-flash | 0.938 (30/32) | 0.812 (26/32) | 8/8 | 8/8 | 8/8 | 6/8 | $0.027172 | $0.000906 |
| always-top | qwen-3-7-plus | 0.906 (29/32) | 0.812 (26/32) | 8/8 | 8/8 | 7/8 | 6/8 | $0.146793 | $0.005062 |

deepseek-v4-flash is both the cheapest and the most accurate of the three. It is cheaper than qwen3-5-9b because Venice bills deepseek-v4-flash cached input at 0.028 per million against 0.138 uncached, while qwen3-5-9b cached input is billed at its full 0.10 input price, so its cache reads save nothing: always-cheap read 562320 cached tokens (hit ratio 0.81) and still paid $0.0562 for them. qwen3-5-9b also used more turns and tokens: 98 turns and 693799 prompt tokens against 78 turns and 475674 for deepseek-v4-flash. Three cheap-tier questions (h05, x06, x07) and one mid-tier question (x03) hit the 8-turn cap and ignored the nudge, so they have no submitted answer. The three runs cost $0.247865 together.

## Reproduce the article

The article "Routing Data Questions by Difficulty" takes every figure from the run files in this repository. The replay and figure scripts in results/replay make no model calls; see results/replay/README.md for setup. Recorded run files are never edited. New runs use --tag and land in their own folder.

| figure in the article | command | file |
|---|---|---|
| Sonnet 5 on everything, 32/32, $0.3631 | harness bench run --config always-mid | results/always-mid/20260924T162945Z.jsonl |
| Haiku 4.5 on everything, 27/32, $0.2476, 19.2 s mean, 6.5 s median, 190.5 s p95 | harness bench run --config always-cheap | results/always-cheap/20260924T190155Z.jsonl |
| Opus 5.5 on everything, 32/32, $0.6349 | harness bench run --config always-top | results/always-top/20260924T040934Z.jsonl |
| Prompt caching, 75.6% less on 24 questions | harness bench run --config always-mid --no-cache, one of the 24 non-expert questions at a time with --only (see What the caching ablation showed) | results/always-mid-nocache/20260924T043458Z.jsonl against the 24 non-expert lines of results/always-mid/20260924T034753Z.jsonl |
| DeepSeek V4 Flash, 32/32, $0.0245, 40.1 s mean | harness bench run --config always-mid --provider venice --context-levels model,topic | results/venice/always-mid-levels-model-topic/20260924T063449Z.jsonl |
| Per-million figures, break-even misroute rates, median and p95 for every row | python3 results/replay/article_figures.py | results/replay/out_article_figures.json |
| Three runs each of Sonnet, Haiku, the word-count router and two cascades | harness bench run --config NAME --repeat 2 --tag repeats | results/NAME-repeats/*.jsonl, summarized in out_article_figures.json under repeats |
| Replayed routers: Opus picks sent to Sonnet 23%, trained word-count model 28.9%, Jev 28.5%, perfect Haiku-or-Sonnet router 31.6% | python3 results/replay/summary_table.py | results/replay/report_summary.md |
| Routing that learns from eval labels, 72% against 77% of Sonnet's cost | python3 results/replay/exp4_online.py | results/replay/report_exp4.md |
| What a routing decision can cost | python3 results/replay/article_figures.py | out_article_figures.json under decision_cost_share_of_perfect_saving |
| Laya, 14% less at 32/32 | python3 results/replay/laya_analyze.py | results/replay/out_laya.json |
| Why Haiku missed x01, x03, x05 and x08 | harness sql with the queries in the file | results/replay/haiku_misses.md |
| Haiku with a 4,096-token thinking budget on the expert questions | harness bench run --config always-cheap --only x01,x02,x03,x04,x05,x06,x07,x08 --repeat 3 --cheap-thinking 4096 --tag thinking | results/always-cheap-subset-thinking/20260926T155727Z.jsonl |
| Haiku with context routing, and the byte-identical cached prefix | harness bench run --config always-cheap --repeat 3 --route-context --capture-requests --tag routectx | results/always-cheap-routectx/20260926T160100Z.jsonl, results/replay/out_route_context_prefix.json |
| BIRD execution check for Opus, Haiku, Qwen 3.5 9B and DeepSeek | harness bench run --bench bird --config NAME [--provider venice] | out_article_figures.json under bird |
| Context levels, model only 26/32 and model plus topic 32/32 on Sonnet | harness bench run --config always-mid --context-levels model and --context-levels model,topic | results/always-mid-levels-model and results/always-mid-levels-model-topic |

## Report

harness report reads every results/*/latest.json and results/*/*/latest.json plus the run files they point at, with no API calls, and writes results/summary.md (one row per configuration, the regraded verdicts, and each configuration's wrong answers with reasons), results/pareto.svg and results/difficulty.svg. Those three files are gitignored and stay local. The charts are plain SVG written by internal/report with the standard library. It also rewrites the block below between the report markers.

<!-- report:start -->
Regenerate with harness report. The full report and the two charts are written locally to results/, which keeps them out of git.

Lowest cost per correct answer on the full 32-question set: venice/always-mid-levels-model-topic at $0.00077 (32/32 correct over 1 pass, total $0.0245).
Lowest on anthropic: anthropic/cascade-signals at $0.00866 (26/32 correct).
Lowest on muse: muse/always-muse at $0.00395 (31/32 correct).
Lowest on venice: venice/always-mid-levels-model-topic at $0.00077 (32/32 correct).
Pareto frontier on anthropic, cheapest first: anthropic/cascade-signals ($0.2252 per pass, 0.812), anthropic/always-cheap ($0.2476 per pass, 0.844), anthropic/cascade-verify-jev ($0.3444 per pass, 0.938), anthropic/always-mid ($0.3631 per pass, 1.000).
Pareto frontier on muse, cheapest first: muse/always-muse ($0.1225 per pass, 0.969).
Pareto frontier on venice, cheapest first: venice/always-mid-levels-model-topic ($0.0245 per pass, 1.000).

| config | kind | passes | accuracy | strict | easy | moderate | hard | expert | total cost | routing overhead | cost per correct | mean latency | p95 latency | cache hit | tier mix cheap/mid/top % |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| anthropic/always-cheap | baseline | 1 | 0.844 (27/32) | 0.750 (24/32) | 8/8 | 8/8 | 8/8 | 3/8 | $0.2476 | $0.0000 | $0.00917 | 19.2s | 190.5s | 0.89 | - |
| anthropic/always-mid | baseline | 1 | 1.000 (32/32) | 0.938 (30/32) | 8/8 | 8/8 | 8/8 | 8/8 | $0.3631 | $0.0000 | $0.01135 | 7.4s | 12.9s | 0.98 | - |
| anthropic/always-mid-levels-model | baseline | 1 | 0.812 (26/32) | 0.719 (23/32) | 8/8 | 5/8 | 5/8 | 8/8 | $0.3666 | $0.0000 | $0.01410 | 8.4s | 13.1s | 0.95 | - |
| anthropic/always-mid-levels-model-topic | baseline | 1 | 1.000 (32/32) | 0.906 (29/32) | 8/8 | 8/8 | 8/8 | 8/8 | $0.3642 | $0.0000 | $0.01138 | 7.4s | 12.2s | 0.96 | - |
| anthropic/always-mid-nocache | baseline | 1 | 0.917 (22/24) | 0.833 (20/24) | 8/8 | 8/8 | 6/8 | - | $1.0124 | $0.0000 | $0.04602 | 6.6s | 9.9s | 0.00 | - |
| anthropic/always-mid-verify | baseline | 1 | 1.000 (3/3) | 0.667 (2/3) | - | 2/2 | 1/1 | - | $0.0594 | $0.0000 | $0.01979 | 7.6s | 8.7s | 0.81 | - |
| anthropic/always-top | baseline | 1 | 1.000 (32/32) | 0.906 (29/32) | 8/8 | 8/8 | 8/8 | 8/8 | $0.6349 | $0.0000 | $0.01984 | 7.5s | 12.9s | 0.96 | - |
| anthropic/cascade-signals | router | 1 | 0.812 (26/32) | 0.719 (23/32) | 8/8 | 8/8 | 8/8 | 2/8 | $0.2252 | $0.0000 | $0.00866 | 7.1s | 12.8s | 0.94 | 91/9/0 |
| anthropic/cascade-verify-haiku | router | 1 | 0.938 (30/32) | 0.844 (27/32) | 8/8 | 8/8 | 8/8 | 6/8 | $0.4425 | $0.1081 | $0.01475 | 12.6s | 36.6s | 0.96 | 81/16/3 |
| anthropic/cascade-verify-jev | router | 1 | 0.938 (30/32) | 0.812 (26/32) | 8/8 | 8/8 | 8/8 | 6/8 | $0.3444 | $0.0881 | $0.01148 | 12.4s | 37.2s | 0.96 | 84/16/0 |
| anthropic/cascade-verify-sonnet | router | 1 | 0.938 (30/32) | 0.844 (27/32) | 8/8 | 8/8 | 8/8 | 6/8 | $0.4619 | $0.2118 | $0.01540 | 12.4s | 44.0s | 0.97 | 88/12/0 |
| anthropic/classifier | router | 1 | 1.000 (32/32) | 0.906 (29/32) | 8/8 | 8/8 | 8/8 | 8/8 | $0.4734 | $0.0494 | $0.01480 | 8.7s | 13.1s | 0.98 | 38/22/41 |
| anthropic/embedding | router | 1 | 1.000 (32/32) | 0.906 (29/32) | 8/8 | 8/8 | 8/8 | 8/8 | $0.5519 | $0.0000 | $0.01725 | 7.6s | 12.9s | 0.93 | 12/47/41 |
| anthropic/heuristic | router | 1 | 1.000 (32/32) | 0.906 (29/32) | 8/8 | 8/8 | 8/8 | 8/8 | $0.3672 | $0.0000 | $0.01148 | 7.5s | 12.3s | 0.98 | 50/22/28 |
| anthropic/heuristic-v2 | router | 2 | 1.000 (64/64) | 0.906 (58/64) | 16/16 (2 passes) | 16/16 (2 passes) | 16/16 (2 passes) | 16/16 (2 passes) | $0.8548 | $0.0000 | $0.01336 | 7.3s | 12.1s | 0.95 | 50/19/31 |
| anthropic/jev-classifier | router | 1 | 1.000 (32/32) | 0.875 (28/32) | 8/8 | 8/8 | 8/8 | 8/8 | $0.5801 | $0.0262 | $0.01813 | 8.6s | 15.1s | 0.92 | 38/19/44 |
| muse/always-muse | baseline | 1 | 0.969 (31/32) | 0.781 (25/32) | 8/8 | 8/8 | 7/8 | 8/8 | $0.1225 | $0.0000 | $0.00395 | 55.4s | 143.8s | 0.86 | - |
| venice/always-cheap | baseline | 3 | 0.667 (64/96) | 0.635 (61/96) | 23/24 (3 passes) | 20/24 (3 passes) | 15/24 (3 passes) | 6/24 (3 passes) | $0.2594 | $0.0000 | $0.00405 | 13.5s | 38.0s | 0.87 | - |
| venice/always-mid | baseline | 1 | 0.969 (31/32) | 0.844 (27/32) | 8/8 | 8/8 | 8/8 | 7/8 | $0.0285 | $0.0000 | $0.00092 | 38.5s | 147.8s | 0.92 | - |
| venice/always-mid-levels-model | baseline | 1 | 0.875 (28/32) | 0.750 (24/32) | 8/8 | 6/8 | 6/8 | 8/8 | $0.0346 | $0.0000 | $0.00124 | 72.3s | 180.4s | 0.93 | - |
| venice/always-mid-levels-model-topic | baseline | 1 | 1.000 (32/32) | 0.875 (28/32) | 8/8 | 8/8 | 8/8 | 8/8 | $0.0245 | $0.0000 | $0.00077 | 40.1s | 128.5s | 0.92 | - |
| venice/always-top | baseline | 3 | 0.958 (92/96) | 0.865 (83/96) | 24/24 (3 passes) | 24/24 (3 passes) | 24/24 (3 passes) | 20/24 (3 passes) | $0.3904 | $0.0000 | $0.00424 | 24.7s | 83.6s | 0.94 | - |
| venice/cascade-verify-jev | router | 3 | 0.854 (82/96) | 0.792 (76/96) | 23/24 (3 passes) | 21/24 (3 passes) | 18/24 (3 passes) | 20/24 (3 passes) | $0.4310 | $0.0660 | $0.00526 | 36.8s | 191.4s | 0.89 | 72/18/10 |
| venice/classifier | router | 1 | 0.906 (29/32) | 0.781 (25/32) | 8/8 | 8/8 | 8/8 | 5/8 | $0.1139 | $0.0204 | $0.00393 | 20.3s | 55.5s | 0.90 | 38/22/41 |
| venice/heuristic | router | 3 | 0.885 (85/96) | 0.792 (76/96) | 24/24 (3 passes) | 19/24 (3 passes) | 23/24 (3 passes) | 19/24 (3 passes) | $0.3294 | $0.0000 | $0.00388 | 25.9s | 89.3s | 0.83 | 50/22/28 |
| venice/heuristic-v2 | router | 3 | 0.875 (84/96) | 0.792 (76/96) | 23/24 (3 passes) | 18/24 (3 passes) | 23/24 (3 passes) | 20/24 (3 passes) | $0.3097 | $0.0000 | $0.00369 | 24.0s | 77.6s | 0.89 | 50/19/31 |
| venice/jev-classifier | router | 3 | 0.927 (89/96) | 0.823 (79/96) | 22/24 (3 passes) | 24/24 (3 passes) | 24/24 (3 passes) | 19/24 (3 passes) | $0.3549 | $0.0369 | $0.00399 | 20.4s | 65.8s | 0.95 | 38/19/44 |
<!-- report:end -->

## Decisions

Why Go. The harness is one Go module with four third-party dependencies (Anthropic SDK, pgx, godotenv, yaml). The OpenAI-compatible Venice client, the Jev client, the embedding client, the SVG charts and the report are all standard library. One command, make build, produces bin/harness with no runtime to install. go.mod pins Go 1.25.

Why these three tiers. cheap, mid and top are three price points about 2x apart per step (Anthropic input 1.00, 2.00 and 4.00 USD per million; output 5.00, 10.00 and 20.00), which is enough spread to measure routing: always-cheap costs $0.2476 for 27/32 on Anthropic while always-top costs $0.6349 for 32/32. The tiers map to claude-haiku-4-5 (thinking off, 4096 max tokens), claude-sonnet-5 and claude-opus-5-5 (adaptive thinking, effort medium, 16000 max tokens), with Venice equivalents qwen3-5-9b, deepseek-v4-flash and qwen-3-7-plus. Every router maps its labels onto these three: easy to cheap, moderate to mid, hard and expert to top. Settings live in llm.DefaultSettings in internal/llm/tier.go and the prices in internal/llm/pricing.go and llm.VenicePrices.

Why explicit date windows. The ecommerce database grows daily, so every ground-truth query in bench/questions.yaml pins a closed 2023 or 2024 window (created_at >= '2024-01-01' AND created_at < '2025-01-01) or needs no window at all. Semantic conventions 1 to 3 require the agent to do the same and forbid comparing a partial current period against a full past period. harness bench validate re-runs the 32 ground-truth queries and rewrites bench/answers.json, so a moved answer fails loudly instead of silently shifting accuracy.

How the semantic layer is used and cached. semantic/ecomm.yaml renders into the second system block with an ephemeral cache marker; the tools plus system prefix is byte-identical across questions, so the first question of a run warms the cache and the rest read it. As of the context-loop gotchas the prefix is 6900 tokens on Haiku, and turn-1 cache reads are about 10300 tokens on Sonnet and 10145 on Opus, above Haiku's 4096-token caching minimum. Observed cache hit ratios are 0.93 to 0.99 on Anthropic. Agent, classifier and verifier share the layer text but start with different instructions, so they do not share cache entries. The --context-levels ablation shows model plus topic scores 32/32 on both providers at 4842 render tokens, identical to the full render, which was 6372 tokens at the time; the field level adds 2040 tokens and no verdict.

How grading works and its limits. internal/bench/grade.go compares the submitted answer to the ground-truth value by answer type: numbers within 1 percent relative tolerance, strings trimmed and case-insensitive, lists as multisets or in order, tables row by row with numeric cells within tolerance. Every line gets two verdicts: lenient (correct) forgives a trailing parenthetical, values appended to a list label, CSV table rows, full dates for YYYY-MM keys and month names for YYYY-MM keys; strict (correct_strict) forgives none of that. Grading costs nothing and is deterministic. Its limits: no partial credit, no second reading of an ambiguous question, and no written reason beyond the verdict (fail_reason and format_reason codes name the diff shape only). The rubric-judge comparison ran a cheap-tier judge plus a mid-tier supervisor over 96 lines at 0.0028 USD per line: it agreed with lenient on 91 of 96, diverged only on formatting cases, misread its own rubric on 5 of those, and gave different finals for byte-identical answers across runs. Deterministic grading stays because the answers are machine-checkable.

What the caching ablation showed. always-mid on the 24 non-expert questions costs $1.0124 uncached and $0.2468 cached, a saving of $0.7656 (75.6 percent) for identical prompt tokens within 0.02 percent. Cache writes cost 1.25x input and reads 0.1x, so caching wins at the first read; the observed break-even is the second turn of the first question, which already costs $0.0270 against the $0.0422 uncached mean. The warm-first-then-parallel runner loses no hits: Venice concurrency 3 reaches a 0.917 hit ratio against 0.876 sequential. A request-capture audit checked the five silent invalidators (variable prefix content, tool schema order, thinking and tool_choice, history rewriting, model-scoped caches): all five are clean, with run-1 versus run-2 turn-1 bodies byte-identical at 23549 bytes.

Omni alignment. This harness maps to four Omni posts and to BIRD; each item below states in one sentence what was adopted and what was deliberately done differently. Rubric-judged evals: adopted the metric names (accuracy by difficulty, consistency, error-free rate, median latency, cost and tokens per correct answer) into every latest.json, but grades against deterministic ground-truth answers instead of an Opus judge with a supervisor because ground-truth answers are free and deterministic (the judge cost 0.0028 USD per line for no verdict gain). Forced semantic model: adopted the semantic layer with metric SQL the agent is told to use verbatim, but carries it in a cached prompt with free run_sql instead of forcing queries through a compiled model because a compiler is out of scope for the routing study and expert medians need window functions it would not express. Three AI context levels: adopted the model, Topic and field split as the --context-levels flag over the YAML sections (conventions and gotchas, metrics with paths and examples, column notes with enums and synonyms), but renders all levels into one cached prompt with no Topic scoping because six tables fit and per-Topic prompts would split the cache. Checkr human review: adopted human review of agent-authored context, but the review happens per pull request (whole change) instead of per context entry because there were no transcripts or wiki pages to extract from. Ten-question start: adopted the starter set as bench/starter.yaml with regress and drift as the gate and monitor, but grew the set to a calibrated 32 with an adversarial expert tier because the first valid baselines scored 24/24 on every tier and could not separate them. BIRD evidence and EX: the BIRD extension adopts the evidence field as a prompt context block measured with and without, and EX (comparing result sets) for that task only, while this benchmark keeps grading the submitted answer because that is what a chat analytics user sees.
