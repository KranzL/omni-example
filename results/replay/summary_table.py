import json, os
import numpy as np
import common

b = common.Bench()
M = b.mid_total()
ids = b.ids
e1 = json.load(open(os.path.join(common.SP, "out_exp1.json")))
e2 = json.load(open(os.path.join(common.SP, "out_exp2.json")))["ecomm"]
EMB_OVERHEAD = 0.000110


def jev(name):
    return {json.loads(l)["id"]: json.loads(l) for l in open(os.path.join(common.SP, name))}


policies = []


def add(name, pol, overhead, src):
    policies.append((name, pol, overhead, src))


add("always-mid", {i: "mid" for i in ids}, 0.0, "results/always-mid latest")
add("always-cheap", {i: "cheap" for i in ids}, 0.0, "results/always-cheap latest")
add("hindsight: e/m/h cheap, x mid", {i: ("mid" if b.diff[i] == "expert" else "cheap") for i in ids}, 0.0, "difficulty labels")
add("oracle: cheap iff cheap correct", dict(b.label), 0.0, "outcome labels")
add("oracle: cheapest correct tier", {i: ("cheap" if b.cheap[i]["correct"] and b.cheap[i]["cost_usd"] <= b.mid[i]["cost_usd"] else "mid") for i in ids}, 0.0, "outcome labels and costs")
for cfg in ["heuristic", "classifier", "embedding", "jev-classifier"]:
    rows, src = common.by_id(cfg)
    pol = {i: ("cheap" if rows[i]["tier"] == "cheap" else "mid") for i in ids}
    oh = [r for r in e1["rows"] if r["name"] == cfg + " two-tier"][0]["overhead"]
    add("exp1 %s, top->mid" % cfg, pol, oh, os.path.relpath(src, common.REPO))
    if cfg == "jev-classifier":
        add("exp1 jev-classifier, top->mid, Jev-only overhead", pol, e1["recorded"]["jev_primary_cost"], "route_gate.primary_cost_usd")
sel = [
    ("LR heur", "LOO bench only", 0.0),
    ("LR heur", "grouped bench only", 0.0),
    ("LR heur", "seeds + grouped bench", 0.0),
    ("LR heur", "seeds only", 0.0),
    ("GBM heur", "seeds + grouped bench", 0.0),
    ("kNN5 heur", "seeds + grouped bench", 0.0),
    ("kNN5 emb", "LOO bench only", EMB_OVERHEAD),
    ("kNN5 emb", "grouped bench only", EMB_OVERHEAD),
    ("kNN5 emb", "seeds only", EMB_OVERHEAD),
    ("LR emb", "seeds + grouped bench", EMB_OVERHEAD),
    ("LR heur+emb", "seeds + grouped bench", EMB_OVERHEAD),
]
for m, rg, oh in sel:
    p = e2[m + " | " + rg]["p"]
    add("exp2 %s, %s, t=0.5" % (m, rg), {i: ("mid" if p[i] >= 0.5 else "cheap") for i in ids}, oh, "out_exp2.json")
for f, t, tag in [("jev_bench_nosl_v0.jsonl", 0.5, "t=0.5"), ("jev_bench_nosl_v0.jsonl", 0.19, "t=0.19 picked on seeds"), ("jev_bench_sl_v0.jsonl", 0.5, "t=0.5"), ("jev_bench_sl_v0.jsonl", 0.88, "t=0.88 picked on seeds"), ("jev_bench_nosl_v1.jsonl", 0.5, "t=0.5, criteria written after reading bench"), ("jev_bench_sl_v1.jsonl", 0.5, "t=0.5, criteria written after reading bench")]:
    R = jev(f)
    cost = sum(R[i]["cost_usd"] for i in ids)
    add("exp3 Jev binary %s %s" % (f.replace("jev_bench_", "").replace(".jsonl", ""), tag), {i: ("mid" if R[i]["probs"]["nested"] >= t else "cheap") for i in ids}, cost, f)

rng = np.random.default_rng(11)
B = 10000
idx = rng.integers(0, len(ids), size=(B, len(ids)))
mid_v = np.array([b.mid[i]["cost_usd"] for i in ids])
print("| policy | correct/32 | total cost | vs always-mid | 95% bootstrap interval of vs-mid | routing overhead | cheap/mid | source |")
print("|---|---|---|---|---|---|---|---|")
out = []
for name, pol, oh, src in policies:
    r = b.replay(pol, oh)
    v = np.array([(b.cheap[i] if pol[i] == "cheap" else b.mid[i])["cost_usd"] for i in ids]) + oh / len(ids)
    ratio = v[idx].sum(1) / mid_v[idx].sum(1) - 1
    lo, hi = np.percentile(ratio, [2.5, 97.5])
    pct = 100 * (r["total"] - M) / M
    print("| %s | %d/32 | $%.4f | %+.1f%% | %+.1f%% to %+.1f%% | $%.5f | %d/%d | %s |" % (name, r["correct"], r["total"], pct, 100 * lo, 100 * hi, oh, r["n_cheap"], 32 - r["n_cheap"], src))
    out.append({"name": name, "correct": r["correct"], "total": r["total"], "pct": pct, "ci": [lo, hi], "overhead": oh})
json.dump(out, open(os.path.join(common.SP, "out_summary.json"), "w"), indent=1, default=float)
lo, hi = common.wilson(5, 5)
print()
print("Wilson 95%% interval for a 5/5 recall: %.2f to %.2f; for 0 misses among 27 cheap-able routed cheap, error-rate upper bound %.2f" % (lo, hi, common.wilson(0, 27)[1]))
