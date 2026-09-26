import glob
import json
import os
import statistics

SP = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.abspath(os.path.join(SP, "..", ".."))
M = 1_000_000

SONNET = "results/always-mid/20260924T162945Z.jsonl"
HAIKU = "results/always-cheap/20260924T190155Z.jsonl"
SONNET_CACHED_24 = "results/always-mid/20260924T034753Z.jsonl"
SONNET_UNCACHED_24 = "results/always-mid-nocache/20260924T043458Z.jsonl"

ROWS = [
    ("Sonnet 5 on everything", "always-mid"),
    ("Haiku 4.5 on everything", "always-cheap"),
    ("Opus 5.5 on everything", "always-top"),
    ("DeepSeek V4 Flash on everything, trimmed layer", "venice/always-mid-levels-model-topic"),
    ("DeepSeek V4 Flash on everything, full layer", "venice/always-mid"),
    ("Qwen 3.5 9B on everything", "venice/always-cheap"),
    ("Muse on everything", "muse/always-muse"),
    ("Word-count router", "heuristic"),
    ("Haiku classifier", "classifier"),
    ("Similar-questions router", "embedding"),
    ("Jev classifier", "jev-classifier"),
    ("Haiku, escalate on errors", "cascade-signals"),
    ("Haiku with Haiku checker", "cascade-verify-haiku"),
    ("Haiku with Jev checker", "cascade-verify-jev"),
    ("Haiku with Sonnet checker", "cascade-verify-sonnet"),
]

REPEATS = ["always-mid", "always-cheap", "heuristic", "cascade-signals", "cascade-verify-jev"]


def path(p):
    return os.path.join(REPO, p)


def lines(p):
    with open(path(p)) as fh:
        return [json.loads(line) for line in fh if line.strip()]


def latest(cfg):
    with open(path(os.path.join("results", cfg, "latest.json"))) as fh:
        return json.load(fh)


def p95(values):
    s = sorted(values)
    idx = min(max((len(s) * 95 + 99) // 100 - 1, 0), len(s) - 1)
    return s[idx]


def latency(rows):
    ms = [r["latency_ms"] for r in rows]
    return {"mean_s": round(statistics.mean(ms) / 1000, 1), "median_s": round(statistics.median(ms) / 1000, 1), "p95_s": round(p95(ms) / 1000, 1)}


def pass_stats(rows):
    passes = sorted({int(r.get("repeat", 1)) for r in rows})
    out = []
    for p in passes:
        pr = [r for r in rows if int(r.get("repeat", 1)) == p]
        out.append({"correct": sum(1 for r in pr if r.get("correct")), "total": len(pr), "cost_usd": sum(r["cost_usd"] for r in pr)})
    return out


def per_million(cost_per_question):
    return round(cost_per_question * M)


def main():
    sonnet = {r["id"]: r for r in lines(SONNET)}
    haiku = {r["id"]: r for r in lines(HAIKU)}
    ids = sorted(sonnet)
    n = len(ids)
    sonnet_q = sum(r["cost_usd"] for r in sonnet.values()) / n
    haiku_ok = [i for i in ids if haiku[i]["correct"]]
    needs_sonnet = [i for i in ids if not haiku[i]["correct"]]
    oracle = sum((haiku[i] if haiku[i]["correct"] else sonnet[i])["cost_usd"] for i in ids) / n
    rule = sum((sonnet[i] if sonnet[i]["difficulty"] == "expert" else haiku[i])["cost_usd"] for i in ids) / n

    cached = [r for r in lines(SONNET_CACHED_24) if r["difficulty"] != "expert"]
    uncached = lines(SONNET_UNCACHED_24)
    cached_q = sum(r["cost_usd"] for r in cached) / len(cached)
    uncached_q = sum(r["cost_usd"] for r in uncached) / len(uncached)

    classifier = latest("classifier")
    classifier_q = classifier["route_cost_usd"] / classifier["total"]
    with open(os.path.join(SP, "out_exp3.json")) as fh:
        exp3 = json.load(fh)
    jev_q = exp3["jev_bench_nosl_v0.jsonl"]["cost"] / n
    jev_sl_q = exp3["jev_bench_sl_v0.jsonl"]["cost"] / n
    verify = latest("cascade-verify-haiku")
    verify_calls = sum(1 for r in lines(verify["source"]) for a in (r.get("attempts") or []) if a.get("verdict"))
    verify_call = verify["route_cost_usd"] / verify_calls if verify_calls else None

    def cost_of(cfg):
        L = latest(cfg)
        return L["total_cost_usd"] / L["repeat"] / (L["total"] / L["repeat"])

    saving = sonnet_q - oracle
    figures = {
        "sources": {"sonnet": SONNET, "haiku": HAIKU, "sonnet_cached_24": SONNET_CACHED_24 + " (24 non-expert questions)", "sonnet_uncached_24": SONNET_UNCACHED_24},
        "per_million_usd": {
            "sonnet_32": per_million(sonnet_q),
            "haiku_32": per_million(sum(r["cost_usd"] for r in haiku.values()) / n),
            "opus_32": per_million(cost_of("always-top")),
            "deepseek_trimmed_32": per_million(cost_of("venice/always-mid-levels-model-topic")),
            "sonnet_cached_24": per_million(cached_q),
            "sonnet_uncached_24": per_million(uncached_q),
            "caching_saving_24": per_million(uncached_q - cached_q),
            "perfect_haiku_or_sonnet_saving": per_million(saving),
            "expert_to_sonnet_rule_saving": per_million(sonnet_q - rule),
            "haiku_classifier_decision": per_million(classifier_q),
            "jev_question_only_decision": per_million(jev_q),
            "jev_with_layer_decision": per_million(jev_sl_q),
            "haiku_checker_call": per_million(verify_call) if verify_call else None,
        },
        "shares_of_sonnet": {
            "perfect_haiku_or_sonnet": round(1 - oracle / sonnet_q, 4),
            "expert_to_sonnet_rule": round(1 - rule / sonnet_q, 4),
            "caching_24": round(1 - cached_q / uncached_q, 4),
        },
        "decision_cost_share_of_perfect_saving": {
            "jev_question_only": round(jev_q / saving, 4),
            "jev_with_layer": round(jev_sl_q / saving, 4),
            "haiku_classifier": round(classifier_q / saving, 4),
            "haiku_checker_call": round(verify_call / saving, 4) if verify_call else None,
        },
    }

    s = len(haiku_ok) / n
    delta = statistics.mean(sonnet[i]["cost_usd"] - haiku[i]["cost_usd"] for i in haiku_ok)
    delta_x = statistics.mean(haiku[i]["cost_usd"] - sonnet[i]["cost_usd"] for i in needs_sonnet)
    breakeven = []
    for e in (0.10, 1.0, 10.0):
        b = (s * delta - jev_q) / ((1 - s) * (delta_x + e))
        b = max(0.0, min(1.0, b))
        breakeven.append({"wrong_answer_cost_usd": e, "max_misroute_share": round(b, 4), "wrong_answers_per_million": round((1 - s) * b * M)})
    figures["breakeven"] = {
        "assumptions": "Router sends every Haiku-safe question to Haiku, pays Jev question-only per decision, and misroutes a share b of the questions Haiku gets wrong; each misroute is a wrong answer.",
        "haiku_safe_share": round(s, 4),
        "needs_sonnet_share": round(1 - s, 4),
        "needs_sonnet_per_million": round((1 - s) * M),
        "sonnet_minus_haiku_on_safe": round(delta, 6),
        "haiku_minus_sonnet_on_needs_sonnet": round(delta_x, 6),
        "rows": breakeven,
    }

    rows = []
    for label, cfg in ROWS:
        L = latest(cfg)
        rs = lines(L["source"])
        rows.append({"label": label, "config": cfg, "source": L["source"], "correct": L["correct"], "total": L["total"], "passes": L["repeat"], "cost_per_pass_usd": round(L["total_cost_usd"] / L["repeat"], 4), **latency(rs)})
    figures["rows"] = rows

    repeats = []
    for cfg in REPEATS:
        orig = latest(cfg)
        orig_rows = lines(orig["source"])
        runs = [{"file": orig["source"], **p} for p in pass_stats(orig_rows)[:1]]
        for f in sorted(glob.glob(path(os.path.join("results", cfg + "-repeats", "*.jsonl")))):
            rel = os.path.relpath(f, REPO)
            runs += [{"file": rel, **p} for p in pass_stats(lines(rel))]
        costs = [r["cost_usd"] for r in runs]
        corr = [r["correct"] for r in runs]
        repeats.append({
            "config": cfg,
            "runs": [{**r, "cost_usd": round(r["cost_usd"], 4)} for r in runs],
            "original": {"correct": runs[0]["correct"], "cost_usd": round(runs[0]["cost_usd"], 4)},
            "mean_correct": round(statistics.mean(corr), 2),
            "correct_range": [min(corr), max(corr)],
            "mean_cost_usd": round(statistics.mean(costs), 4),
            "cost_range_usd": [round(min(costs), 4), round(max(costs), 4)],
        })
    figures["repeats"] = repeats

    def expert_block(files, label):
        rows = []
        for f in files:
            rows += [r for r in lines(f) if r["difficulty"] == "expert"]
        per = {}
        for r in rows:
            per.setdefault(r["id"], []).append(bool(r.get("correct")))
        return {"label": label, "files": files, "answers": len(rows), "correct": sum(1 for r in rows if r.get("correct")),
                "cost_per_answer_usd": round(sum(r["cost_usd"] for r in rows) / len(rows), 5), **latency(rows),
                "per_question": {k: "%d/%d" % (sum(v), len(v)) for k, v in sorted(per.items())}}

    thinking = sorted(glob.glob(path("results/always-cheap-subset-thinking/*.jsonl")))
    haiku_runs = [HAIKU] + [os.path.relpath(f, REPO) for f in sorted(glob.glob(path("results/always-cheap-repeats/*.jsonl")))]
    sonnet_runs = [SONNET] + [os.path.relpath(f, REPO) for f in sorted(glob.glob(path("results/always-mid-repeats/*.jsonl")))]
    figures["expert_questions"] = [expert_block(haiku_runs, "Haiku 4.5, thinking off"), expert_block(sonnet_runs, "Sonnet 5")]
    if thinking:
        figures["expert_questions"].insert(1, expert_block([os.path.relpath(f, REPO) for f in thinking], "Haiku 4.5, 4096-token thinking budget"))
    ctx = sorted(glob.glob(path("results/always-cheap-routectx/*.jsonl")))
    if ctx:
        cr = lines(os.path.relpath(ctx[-1], REPO))
        figures["route_context"] = {"file": os.path.relpath(ctx[-1], REPO), "passes": pass_stats(cr), **latency(cr),
                                    "expert": expert_block([os.path.relpath(ctx[-1], REPO)], "Haiku 4.5 with context routing")}

    bird = []
    for label, cfg in (("Opus 5.5", "bird/always-top"), ("Haiku 4.5", "bird/always-cheap"), ("Qwen 3.5 9B", "bird/venice/always-cheap"), ("Qwen 3.7 Plus", "bird/venice/always-top"), ("DeepSeek V4 Flash", "bird/venice/always-mid")):
        L = latest(cfg)
        bird.append({"label": label, "config": cfg, "source": L["source"], "execution_check": L["ex_correct"], "total": L["total"], "cost_usd": round(L["total_cost_usd"], 4)})
    figures["bird"] = bird

    with open(os.path.join(SP, "out_article_figures.json"), "w") as fh:
        json.dump(figures, fh, indent=1)
    print(json.dumps(figures, indent=1))


main()
