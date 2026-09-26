# Why Haiku missed the order-gap questions

x01, x03, x05 and x08 ask for the median days between a user's nth and mth non-cancelled order, for users "acquired through" a traffic source in 2024. The ground truth reads that as users created in 2024 through that source, with all of their orders. Every query below runs read-only through `harness sql`.

## The Haiku run the article uses

In `results/always-cheap/20260924T190155Z.jsonl`, Haiku's SQL for x01, x03 and x08 groups `order_items` by `order_id`, takes `MIN(created_at)` as the order time and ranks orders. That follows gotcha 2 in `semantic/ecomm.yaml` ("Rank and difference orders, never raw items"). x05 groups by user and calendar day instead of by order.

What Haiku got wrong is the cohort. It took every user from the source, whatever their signup date, and kept only orders placed in 2024. That reading reproduces Haiku's answers from the database:

```
WITH cohort AS (SELECT id FROM users WHERE traffic_source = 'Facebook'),
o AS (SELECT oi.user_id, MIN(oi.created_at) AS at,
             ROW_NUMBER() OVER (PARTITION BY oi.user_id ORDER BY MIN(oi.created_at)) AS rn
      FROM order_items oi JOIN cohort c ON c.id = oi.user_id
      WHERE oi.status <> 'Cancelled' AND oi.created_at >= '2024-01-01' AND oi.created_at < '2025-01-01'
      GROUP BY oi.user_id, oi.order_id)
SELECT PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY o2.at::date - o1.at::date)
FROM o o1 JOIN o o2 ON o2.user_id = o1.user_id AND o1.rn = 1 AND o2.rn = 2
```

| source | this reading | Haiku answered | ground truth |
|---|---|---|---|
| Facebook (x01) | 75 | 75 | 95.82 |
| Organic (x05) | 74 | 75 | 94.46 |
| Search (x08) | 74 | 74 | 94.49 |

The ground truth for each row is the question's `ground_truth_sql` in `bench/questions.yaml`. Gotcha 3 covers this case ("Cohort questions join each user to that user's own orders inside a window relative to the user's created_at"), and it did not prevent the misreading.

## Whether the gotchas were in the prompt

No request bodies were captured for this run, so this rests on git history in the private working repository. The run file was committed in the same commit as a `semantic/ecomm.yaml` that already had gotchas 1 to 3, which entered the layer on 2026-09-24 at 05:23 UTC, about 14 hours before the run. The harness renders the gotchas section into the cached system prompt, so gotcha 2 and gotcha 3 were in that prompt unless the run was made from a different checkout than the one it was committed from.

## The earlier Haiku run

`results/always-cheap/20260924T034617Z.jsonl` ran before the gotchas existed. There Haiku ranked raw `order_items` rows for x05 and x08, without grouping by `order_id`, and made the same cohort misreading. Between the two runs the item-row mistake disappeared and the cohort mistake stayed.
