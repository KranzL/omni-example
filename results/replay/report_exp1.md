| policy | correct | total cost | vs always-mid | routing overhead | n cheap | source |
|---|---|---|---|---|---|---|
| always-mid | 32/32 | $0.3631 | +0.0% | $0.00000 | 0 | results/always-mid/20260924T162945Z.jsonl |
| always-cheap | 27/32 | $0.2476 | -31.8% | $0.00000 | 32 | results/always-cheap/20260924T190155Z.jsonl |
| hindsight e/m/h cheap, x mid | 32/32 | $0.2356 | -35.1% | $0.00000 | 24 | difficulty labels |
| oracle (label) | 32/32 | $0.2483 | -31.6% | $0.00000 | 27 | per-question outcome |
| heuristic two-tier | 32/32 | $0.2791 | -23.1% | $0.00000 | 16 | results/heuristic/20260924T184116Z.jsonl |
| heuristic-v2 two-tier | 32/32 | $0.2791 | -23.1% | $0.00000 | 16 | results/heuristic-v2/20260924T183941Z.jsonl |
| classifier two-tier | 32/32 | $0.3594 | -1.0% | $0.04936 | 12 | results/classifier/20260924T043814Z.jsonl |
| embedding two-tier | 32/32 | $0.3480 | -4.2% | $0.00011 | 4 | results/embedding/20260924T045143Z.jsonl (cold cost from docs/routers.md; run was cached) |
| jev-classifier two-tier | 32/32 | $0.3363 | -7.4% | $0.02621 | 12 | results/jev-classifier/20260924T190322Z.jsonl |
| jev-classifier two-tier, Jev-only overhead | 32/32 | $0.3185 | -12.3% | $0.00844 | 12 | primary_cost_usd only |

heuristic two-tier mix3 {'cheap': 16, 'mid': 7, 'top': 9} cheap-routed label-mid [] mid-routed label-cheap 11
heuristic-v2 two-tier mix3 {'cheap': 16, 'mid': 6, 'top': 10} cheap-routed label-mid [] mid-routed label-cheap 11
classifier two-tier mix3 {'cheap': 12, 'mid': 7, 'top': 13} cheap-routed label-mid [] mid-routed label-cheap 15
embedding two-tier mix3 {'cheap': 4, 'mid': 15, 'top': 13} cheap-routed label-mid [] mid-routed label-cheap 23
jev-classifier two-tier mix3 {'cheap': 12, 'mid': 6, 'top': 14} cheap-routed label-mid [] mid-routed label-cheap 15
jev fallbacks ['m04', 'm05', 'm06', 'm07', 'h02', 'h04'] jev primary cost 0.008441
cache writes >1000 tokens {'cheap': [('h02', 1305, 0.00860975), ('x01', 21061, 0.04419415), ('x02', 1154, 0.0075854), ('x06', 17384, 0.0334616), ('x07', 8518, 0.018743799999999998), ('x08', 8103, 0.01595345)], 'mid': []}
