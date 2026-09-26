# Offline replay and article figures

Everything in this folder is computed from recorded run files, with no model calls. The one exception is the Jev and Laya responses, which were recorded once and are replayed from the jsonl files here.

## Which runs the replay uses

The replay assigns each of the 32 benchmark questions to Haiku 4.5 or Sonnet 5 and charges the recorded per-question cost and verdict of these two runs:

- Haiku 4.5: `results/always-cheap/20260924T190155Z.jsonl` (27/32, $0.2476). Haiku missed x01, x03, x04, x05 and x08 in this run.
- Sonnet 5: `results/always-mid/20260924T162945Z.jsonl` (32/32, $0.3631).

The two recorded always-cheap runs missed different questions. The earlier run, `results/always-cheap/20260924T034617Z.jsonl`, missed x01, x03, x05, x06 and x08. Every replay here uses the later run, and `common.PINNED` fixes that choice along with the recorded router runs, so a later `latest.json` cannot change these numbers.

A question "needs Sonnet" when Haiku got it wrong in the Haiku run above. That is 5 of 32 questions, and four of them come from one question template.

## Rerun

From the repository root, with Python 3.10 or newer and `numpy`, `scikit-learn` and `pyyaml` installed:

```
cd results/replay
python3 exp1_two_tier.py      # recorded routers with Opus picks sent to Sonnet
python3 exp2_ml.py            # trained routers, grouped cross-validation
python3 exp3_jev_analyze.py   # Jev two-option question, from the recorded Jev responses
python3 exp4_online.py        # routing that learns from eval labels, 200 random orders
python3 exp5_breakeven.py     # how much a routing decision can cost
python3 summary_table.py      # every policy in one table
python3 laya_analyze.py       # Laya, from the recorded Laya responses
python3 article_figures.py    # per-million figures, break-even, latency, repeats, BIRD
```

Each script rewrites its `out_*.json` and `report_*.md`. The outputs committed here match a clean rerun byte for byte.

`features.json` and `semantic_ecomm.txt` come from the Go code: `go run ./results/replay/godump DIR` writes both into DIR from `semantic/ecomm.yaml`, `bench/questions.yaml` and `bench/router_seed.yaml`. `embcache/` holds the bge-m3 vectors for the 32 questions and the 36 seeds, so the embedding models need no API call.

Calling Jev or Laya again needs keys or a local install: `jev_binary.py` calls the Jev API with `JEV_API_KEY`, `embed_fill.py` fetches missing embeddings with `VENICE_API_KEY`, and `laya_binary.py` needs `pip install laya` (0.3.20 was used) and runs the model on the local machine. `spend.jsonl` logs what those calls cost when they were made.

## Files the article cites

| figure | file |
|---|---|
| Recorded routers with Opus sent to Sonnet, trained word-count model, Jev two-option question, perfect Haiku-or-Sonnet router | `report_summary.md` |
| Routing that learns from eval labels | `report_exp4.md`, tables "LR heur, static" and "LR heur, free labels" |
| What a routing decision can cost | `out_article_figures.json`, `decision_cost_share_of_perfect_saving` |
| Per-million figures, break-even misroute rates, latency, repeats, BIRD | `out_article_figures.json` |
| Laya | `out_laya.json` |
| Why Haiku missed the expert questions | `haiku_misses.md` |
