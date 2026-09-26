seeds v0 nosl: AUC hard-vs-rest 1.00, acc@0.5 34/36, nested@0.5 by hand label {'easy': [0, 12], 'moderate': [0, 12], 'hard': [10, 12]}, seed-best threshold 0.190 (acc 35/36), cost $0.000643, latency mean 355 p50 298 p95 596 ms
seeds v0 sl: AUC hard-vs-rest 0.99, acc@0.5 33/36, nested@0.5 by hand label {'easy': [0, 12], 'moderate': [1, 12], 'hard': [10, 12]}, seed-best threshold 0.880 (acc 34/36), cost $0.009368, latency mean 393 p50 392 p95 424 ms

| variant | AUC vs label | nested@0.5 among 5 mid | nested@0.5 among 27 cheap | replay correct@0.5 | replay total@0.5 | vs mid | Jev cost/32 | per call | tokens/call | latency mean/p50/p95 ms |
|---|---|---|---|---|---|---|---|---|---|---|
| jev_bench_nosl_v0.jsonl | 0.98 | 5/5 | 7/27 | 32/32 | $0.2594 | -28.5% | $0.000580 | $0.0000181 | 432 | 313/306/354 |
| jev_bench_sl_v0.jsonl | 0.93 | 5/5 | 9/27 | 32/32 | $0.2785 | -23.3% | $0.008337 | $0.0002605 | 6203 | 399/396/428 |
| jev_bench_nosl_v1.jsonl | 0.96 | 5/5 | 5/27 | 32/32 | $0.2459 | -32.3% | $0.000701 | $0.0000219 | 522 | 298/296/329 |
| jev_bench_sl_v1.jsonl | 0.96 | 5/5 | 7/27 | 32/32 | $0.2654 | -26.9% | $0.008458 | $0.0002643 | 6293 | 400/392/453 |

sweep jev_bench_nosl_v0.jsonl
| threshold on p(nested) | correct | total incl. Jev | n cheap |
|---|---|---|---|
| 0.0000 | 32/32 | $0.3637 | 0 |
| 0.0100 | 32/32 | $0.3164 | 10 |
| 0.0200 | 32/32 | $0.3115 | 11 |
| 0.0300 | 32/32 | $0.3041 | 12 |
| 0.0600 | 32/32 | $0.2930 | 14 |
| 0.1300 | 32/32 | $0.2863 | 15 |
| 0.2400 | 32/32 | $0.2797 | 16 |
| 0.3500 | 32/32 | $0.2766 | 17 |
| 0.4700 | 32/32 | $0.2715 | 18 |
| 0.4800 | 32/32 | $0.2676 | 19 |
| 0.5000 | 32/32 | $0.2594 | 20 |
| 0.7500 | 32/32 | $0.2543 | 21 |
| 0.8000 | 32/32 | $0.2486 | 22 |
| 0.9000 | 32/32 | $0.2421 | 23 |
| 0.9900 | 32/32 | $0.2350 | 24 |
| 0.9990 | 32/32 | $0.2548 | 26 |
| 1.0100 | 27/32 | $0.2482 | 32 |
threshold picked on seeds (0.190): 32/32 $0.2797, 16 cheap

sweep jev_bench_sl_v0.jsonl
| threshold on p(nested) | correct | total incl. Jev | n cheap |
|---|---|---|---|
| 0.0000 | 32/32 | $0.3714 | 0 |
| 0.0100 | 32/32 | $0.3125 | 12 |
| 0.0300 | 32/32 | $0.2998 | 14 |
| 0.0800 | 32/32 | $0.2875 | 16 |
| 0.3100 | 32/32 | $0.2836 | 17 |
| 0.5000 | 32/32 | $0.2785 | 18 |
| 0.7400 | 32/32 | $0.2703 | 19 |
| 0.7800 | 32/32 | $0.2671 | 20 |
| 0.8600 | 32/32 | $0.2613 | 21 |
| 0.9000 | 32/32 | $0.2563 | 22 |
| 0.9900 | 32/32 | $0.2499 | 23 |
| 1.0100 | 27/32 | $0.2560 | 32 |
threshold picked on seeds (0.880): 32/32 $0.2563, 22 cheap

BIRD, label = always-cheap EX wrong; strong tier = Opus (always-top), EX grading
bird nosl: AUC 0.78, nested@0.5 0 of 10 label-strong, 0 of 20 label-cheap; replay EX 20/30 $0.1639 (30 cheap); Jev $0.000539; latency mean 304 p50 302 p95 335
bird sl: AUC 0.74, nested@0.5 0 of 10 label-strong, 1 of 20 label-cheap; replay EX 20/30 $0.1704 (29 cheap); Jev $0.002501; latency mean 313 p50 304 p95 366
