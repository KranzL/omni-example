ecomm bench n=32 positives=5; seeds n=36 hard=12; bird n=30 ex-wrong-on-cheap=10

| model | regime | AUC | label acc@0.5 | mid caught@0.5 (of 5) | false mid@0.5 | replay correct@0.5 | replay cost@0.5 | vs mid | cheapest 32/32 threshold (in-sample pick) |
|---|---|---|---|---|---|---|---|---|---|
| LR heur | LOO bench only | 0.99 | 28/32 | 5 | 4 | 32/32 | $0.2497 | -31.2% | t=0.513 $0.2425 (24 cheap) |
| LR heur | seeds + LOO bench | 0.96 | 25/32 | 5 | 7 | 32/32 | $0.2581 | -28.9% | t=0.775 $0.2394 (25 cheap) |
| LR heur | grouped bench only | 0.76 | 24/32 | 1 | 4 | 28/32 | $0.2519 | -30.6% | t=0.146 $0.2719 (18 cheap) |
| LR heur | seeds + grouped bench | 0.96 | 25/32 | 5 | 7 | 32/32 | $0.2581 | -28.9% | t=0.775 $0.2394 (25 cheap) |
| LR heur | seeds only | 0.85 | 21/32 | 5 | 11 | 32/32 | $0.2807 | -22.7% | t=1.000 $0.2473 (22 cheap) |
| LR emb | LOO bench only | 0.95 | 28/32 | 4 | 3 | 31/32 | $0.2558 | -29.5% | t=0.399 $0.2594 (20 cheap) |
| LR emb | seeds + LOO bench | 0.93 | 25/32 | 4 | 6 | 31/32 | $0.2520 | -30.6% | t=0.480 $0.2652 (19 cheap) |
| LR emb | grouped bench only | 0.67 | 24/32 | 0 | 3 | 27/32 | $0.2580 | -28.9% | t=0.348 $0.2897 (14 cheap) |
| LR emb | seeds + grouped bench | 0.85 | 25/32 | 4 | 6 | 31/32 | $0.2520 | -30.6% | t=0.480 $0.2652 (19 cheap) |
| LR emb | seeds only | 0.90 | 18/32 | 5 | 14 | 32/32 | $0.2995 | -17.5% | t=0.535 $0.2699 (18 cheap) |
| kNN3 emb | LOO bench only | 0.89 | 28/32 | 4 | 3 | 31/32 | $0.2327 | -35.9% | t=0.000 $0.3631 (0 cheap) |
| kNN3 emb | seeds + LOO bench | 0.85 | 27/32 | 4 | 4 | 31/32 | $0.2409 | -33.6% | t=0.000 $0.3631 (0 cheap) |
| kNN3 emb | grouped bench only | 0.43 | 24/32 | 0 | 3 | 27/32 | $0.2350 | -35.3% | t=0.000 $0.3631 (0 cheap) |
| kNN3 emb | seeds + grouped bench | 0.38 | 23/32 | 0 | 4 | 27/32 | $0.2432 | -33.0% | t=0.000 $0.3631 (0 cheap) |
| kNN3 emb | seeds only | 0.93 | 21/32 | 5 | 11 | 32/32 | $0.2832 | -22.0% | t=0.668 $0.2543 (21 cheap) |
| kNN5 emb | LOO bench only | 0.86 | 29/32 | 4 | 2 | 31/32 | $0.2256 | -37.9% | t=0.000 $0.3631 (0 cheap) |
| kNN5 emb | seeds + LOO bench | 0.91 | 27/32 | 4 | 4 | 31/32 | $0.2409 | -33.6% | t=0.190 $0.2816 (16 cheap) |
| kNN5 emb | grouped bench only | 0.56 | 25/32 | 0 | 2 | 27/32 | $0.2278 | -37.3% | t=0.000 $0.3631 (0 cheap) |
| kNN5 emb | seeds + grouped bench | 0.64 | 23/32 | 0 | 4 | 27/32 | $0.2432 | -33.0% | t=0.144 $0.2963 (14 cheap) |
| kNN5 emb | seeds only | 0.96 | 22/32 | 5 | 10 | 32/32 | $0.2768 | -23.8% | t=0.605 $0.2497 (22 cheap) |
| kNN5 heur | LOO bench only | 0.85 | 26/32 | 4 | 5 | 31/32 | $0.2671 | -26.4% | t=0.200 $0.2529 (21 cheap) |
| kNN5 heur | seeds + LOO bench | 0.91 | 26/32 | 5 | 6 | 32/32 | $0.2767 | -23.8% | t=0.400 $0.2596 (20 cheap) |
| kNN5 heur | grouped bench only | 0.47 | 22/32 | 0 | 5 | 27/32 | $0.2694 | -25.8% | t=0.000 $0.3631 (0 cheap) |
| kNN5 heur | seeds + grouped bench | 0.85 | 26/32 | 5 | 6 | 32/32 | $0.2767 | -23.8% | t=0.400 $0.2596 (20 cheap) |
| kNN5 heur | seeds only | 0.87 | 22/32 | 5 | 10 | 32/32 | $0.2768 | -23.8% | t=1.000 $0.2587 (20 cheap) |
| GBM heur | LOO bench only | 0.92 | 29/32 | 5 | 3 | 32/32 | $0.2687 | -26.0% | t=0.001 $0.2687 (24 cheap) |
| GBM heur | seeds + LOO bench | 0.96 | 27/32 | 5 | 5 | 32/32 | $0.2753 | -24.2% | t=0.941 $0.2550 (26 cheap) |
| GBM heur | grouped bench only | 0.18 | 25/32 | 1 | 3 | 28/32 | $0.2709 | -25.4% | t=0.000 $0.3631 (0 cheap) |
| GBM heur | seeds + grouped bench | 0.84 | 23/32 | 1 | 5 | 28/32 | $0.2776 | -23.6% | t=0.095 $0.2633 (20 cheap) |
| GBM heur | seeds only | 0.79 | 19/32 | 5 | 13 | 32/32 | $0.2917 | -19.7% | t=0.968 $0.2703 (19 cheap) |
| LR heur+emb | LOO bench only | 0.91 | 30/32 | 4 | 1 | 31/32 | $0.2427 | -33.2% | t=0.028 $0.2774 (16 cheap) |
| LR heur+emb | seeds + LOO bench | 0.88 | 27/32 | 4 | 4 | 31/32 | $0.2379 | -34.5% | t=0.032 $0.2944 (14 cheap) |
| LR heur+emb | grouped bench only | 0.59 | 26/32 | 0 | 1 | 27/32 | $0.2449 | -32.5% | t=0.015 $0.3077 (10 cheap) |
| LR heur+emb | seeds + grouped bench | 0.79 | 26/32 | 3 | 4 | 30/32 | $0.2282 | -37.2% | t=0.032 $0.2944 (14 cheap) |
| LR heur+emb | seeds only | 0.90 | 24/32 | 5 | 8 | 32/32 | $0.2644 | -27.2% | t=0.630 $0.2613 (20 cheap) |

Seeds, LOO within the 36 seeds, label hard vs easy+moderate (hand labels):
| model | AUC | label acc@0.5 | hard caught (of 12) | false hard |
|---|---|---|---|---|
| LR heur | 0.97 | 32/36 | 11 | 3 |
| LR emb | 0.87 | 29/36 | 8 | 3 |
| kNN3 emb | 0.75 | 26/36 | 7 | 5 |
| kNN5 emb | 0.75 | 27/36 | 8 | 5 |
| kNN5 heur | 0.98 | 33/36 | 10 | 1 |
| GBM heur | 0.98 | 33/36 | 10 | 1 |
| LR heur+emb | 0.91 | 29/36 | 7 | 2 |

BIRD (Anthropic, EX grading; strong tier is Opus because no Anthropic always-mid BIRD run exists): always-cheap 20/30 $0.1634, always-top 27/30 $0.4875, oracle 28/30 $0.2755
| model | AUC | label acc@0.5 | strong caught@0.5 (of 10) | false strong | replay EX@0.5 | replay cost@0.5 |
|---|---|---|---|---|---|---|
| LR heur | 0.50 | 16/30 | 3 | 7 | 23/30 | $0.2635 |
| LR emb | 0.42 | 19/30 | 2 | 3 | 22/30 | $0.1927 |
| kNN3 emb | 0.63 | 18/30 | 3 | 5 | 21/30 | $0.2394 |
| kNN5 emb | 0.52 | 19/30 | 0 | 1 | 20/30 | $0.1633 |
| kNN5 heur | 0.41 | 15/30 | 0 | 5 | 20/30 | $0.2105 |
| GBM heur | 0.43 | 14/30 | 0 | 6 | 20/30 | $0.2396 |
| LR heur+emb | 0.58 | 18/30 | 1 | 3 | 21/30 | $0.1855 |
