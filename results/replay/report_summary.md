| policy | correct/32 | total cost | vs always-mid | 95% bootstrap interval of vs-mid | routing overhead | cheap/mid | source |
|---|---|---|---|---|---|---|---|
| always-mid | 32/32 | $0.3631 | +0.0% | +0.0% to +0.0% | $0.00000 | 0/32 | results/always-mid latest |
| always-cheap | 27/32 | $0.2476 | -31.8% | -51.5% to -8.0% | $0.00000 | 32/0 | results/always-cheap latest |
| hindsight: e/m/h cheap, x mid | 32/32 | $0.2356 | -35.1% | -45.9% to -25.1% | $0.00000 | 24/8 | difficulty labels |
| oracle: cheap iff cheap correct | 32/32 | $0.2483 | -31.6% | -46.3% to -15.1% | $0.00000 | 27/5 | outcome labels |
| oracle: cheapest correct tier | 32/32 | $0.2284 | -37.1% | -47.3% to -27.1% | $0.00000 | 25/7 | outcome labels and costs |
| exp1 heuristic, top->mid | 32/32 | $0.2791 | -23.1% | -34.0% to -13.7% | $0.00000 | 16/16 | results/heuristic/20260924T184116Z.jsonl |
| exp1 classifier, top->mid | 32/32 | $0.3594 | -1.0% | -8.7% to +5.1% | $0.04936 | 12/20 | results/classifier/20260924T043814Z.jsonl |
| exp1 embedding, top->mid | 32/32 | $0.3480 | -4.2% | -9.0% to -0.8% | $0.00011 | 4/28 | results/embedding/20260924T045143Z.jsonl |
| exp1 jev-classifier, top->mid | 32/32 | $0.3363 | -7.4% | -15.6% to -0.8% | $0.02621 | 12/20 | results/jev-classifier/20260924T190322Z.jsonl |
| exp1 jev-classifier, top->mid, Jev-only overhead | 32/32 | $0.3185 | -12.3% | -21.0% to -5.3% | $0.00844 | 12/20 | route_gate.primary_cost_usd |
| exp2 LR heur, LOO bench only, t=0.5 | 32/32 | $0.2497 | -31.2% | -42.6% to -20.9% | $0.00000 | 23/9 | out_exp2.json |
| exp2 LR heur, grouped bench only, t=0.5 | 28/32 | $0.2519 | -30.6% | -47.1% to -9.7% | $0.00000 | 27/5 | out_exp2.json |
| exp2 LR heur, seeds + grouped bench, t=0.5 | 32/32 | $0.2581 | -28.9% | -39.4% to -19.1% | $0.00000 | 20/12 | out_exp2.json |
| exp2 LR heur, seeds only, t=0.5 | 32/32 | $0.2807 | -22.7% | -33.4% to -13.4% | $0.00000 | 16/16 | out_exp2.json |
| exp2 GBM heur, seeds + grouped bench, t=0.5 | 28/32 | $0.2776 | -23.6% | -43.2% to -0.5% | $0.00000 | 26/6 | out_exp2.json |
| exp2 kNN5 heur, seeds + grouped bench, t=0.5 | 32/32 | $0.2767 | -23.8% | -38.1% to -8.3% | $0.00000 | 21/11 | out_exp2.json |
| exp2 kNN5 emb, LOO bench only, t=0.5 | 31/32 | $0.2257 | -37.8% | -48.1% to -28.0% | $0.00011 | 26/6 | out_exp2.json |
| exp2 kNN5 emb, grouped bench only, t=0.5 | 27/32 | $0.2279 | -37.2% | -52.2% to -16.6% | $0.00011 | 30/2 | out_exp2.json |
| exp2 kNN5 emb, seeds only, t=0.5 | 32/32 | $0.2769 | -23.7% | -34.5% to -14.3% | $0.00011 | 17/15 | out_exp2.json |
| exp2 LR emb, seeds + grouped bench, t=0.5 | 31/32 | $0.2521 | -30.6% | -41.4% to -20.6% | $0.00011 | 22/10 | out_exp2.json |
| exp2 LR heur+emb, seeds + grouped bench, t=0.5 | 30/32 | $0.2283 | -37.1% | -47.4% to -27.3% | $0.00011 | 25/7 | out_exp2.json |
| exp3 Jev binary nosl_v0 t=0.5 | 32/32 | $0.2594 | -28.5% | -39.5% to -18.8% | $0.00058 | 20/12 | jev_bench_nosl_v0.jsonl |
| exp3 Jev binary nosl_v0 t=0.19 picked on seeds | 32/32 | $0.2797 | -23.0% | -33.8% to -13.6% | $0.00058 | 16/16 | jev_bench_nosl_v0.jsonl |
| exp3 Jev binary sl_v0 t=0.5 | 32/32 | $0.2785 | -23.3% | -34.2% to -13.8% | $0.00834 | 18/14 | jev_bench_sl_v0.jsonl |
| exp3 Jev binary sl_v0 t=0.88 picked on seeds | 32/32 | $0.2563 | -29.4% | -40.1% to -19.9% | $0.00834 | 22/10 | jev_bench_sl_v0.jsonl |
| exp3 Jev binary nosl_v1 t=0.5, criteria written after reading bench | 32/32 | $0.2459 | -32.3% | -43.1% to -22.4% | $0.00070 | 22/10 | jev_bench_nosl_v1.jsonl |
| exp3 Jev binary sl_v1 t=0.5, criteria written after reading bench | 32/32 | $0.2654 | -26.9% | -37.8% to -17.3% | $0.00846 | 20/12 | jev_bench_sl_v1.jsonl |

Wilson 95% interval for a 5/5 recall: 0.57 to 1.00; for 0 misses among 27 cheap-able routed cheap, error-rate upper bound 0.12
