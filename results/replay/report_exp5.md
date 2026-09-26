always-mid $0.3631, 0.01135 per question
label oracle $0.2483; saving $0.1148 = 31.6%; break-even overhead per question $0.00359 (32% of a mean mid call)
cost oracle $0.2284; saving $0.1347 = 37.1%; break-even overhead per question $0.00421 (37% of a mean mid call)
hindsight difficulty $0.2356; saving $0.1275 = 35.1%; break-even overhead per question $0.00398 (35% of a mean mid call)

Break-even overhead per question for a perfect binary router under other traffic mixes (label oracle; per-difficulty means from the 32 recorded questions):
  easy: mean mid $0.00696, mean perfect-routed $0.00300, saving per question $0.00395
  moderate: mean mid $0.01022, mean perfect-routed $0.00411, saving per question $0.00610
  hard: mean mid $0.01190, mean perfect-routed $0.00602, saving per question $0.00588
  expert: mean mid $0.01632, mean perfect-routed $0.01790, saving per question $-0.00158

| traffic mix easy/moderate/hard/expert | always-mid per question | perfect router per question | break-even overhead per question | saving % |
|---|---|---|---|---|
| benchmark 25/25/25/25 | $0.01135 | $0.00776 | $0.00359 | 31.6% |
| 50/25/15/10 | $0.00945 | $0.00522 | $0.00423 | 44.7% |
| 70/20/7/3 | $0.00824 | $0.00388 | $0.00435 | 52.8% |
| 90/7/2/1 | $0.00738 | $0.00329 | $0.00409 | 55.4% |
| 0/0/0/100 expert only | $0.01632 | $0.01790 | $-0.00158 | -9.7% |

Cheap-able share s (two classes: 27 cheap-able, 5 need-mid, class means from the benchmark):
  cheap-able: mean cheap $0.00611, mean mid $0.01036; need-mid: mean mid $0.01669
| cheap-able share | break-even overhead per question | saving % of always-mid |
|---|---|---|
| 0.50 | $0.00213 | 15.7% |
| 0.60 | $0.00255 | 19.8% |
| 0.70 | $0.00298 | 24.3% |
| 0.84 | $0.00359 | 31.6% |
| 0.90 | $0.00383 | 34.8% |
| 0.95 | $0.00404 | 37.8% |

Imperfect router, benchmark mix: overhead budget left after false mids (every need-mid question caught):
| false mids among 27 cheap-able | remaining break-even overhead per question |
|---|---|
| 0 | $0.00359 (mean-gain assumption) |
| 3 | $0.00319 (mean-gain assumption) |
| 5 | $0.00292 (mean-gain assumption) |
| 7 | $0.00266 (mean-gain assumption) |
| 11 | $0.00213 (mean-gain assumption) |
| 15 | $0.00159 (mean-gain assumption) |

Reference router overheads per question: Jev with semantic layer $0.000261 (exp3), Jev question only $0.000018 (exp3), bge-m3 embedding $0.0000034 (736 tokens / 32 at $0.15/M, docs/routers.md), Haiku classifier $0.00154 ($0.04936/32, results/classifier), Haiku verifier ~$0.0028 (docs/routers.md).
