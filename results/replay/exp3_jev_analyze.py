import json, os
import numpy as np
from sklearn.metrics import roc_auc_score
import common, ml_data


def load(name):
    return {json.loads(l)["id"]: json.loads(l) for l in open(os.path.join(common.SP, name))}


def lat(rows):
    v = np.array([r["latency_ms"] for r in rows.values()])
    return v.mean(), np.percentile(v, 50), np.percentile(v, 95)


def main():
    b = common.Bench()
    M = b.mid_total()
    seeds = common.seeds()
    sy = {s["id"]: (1 if s["difficulty"] == "hard" else 0) for s in seeds}
    seed_best = {}
    for mode in ("nosl", "sl"):
        S = load("jev_seeds_%s_v0.jsonl" % mode)
        ids = [s["id"] for s in seeds]
        p = np.array([S[i]["probs"]["nested"] for i in ids])
        y = np.array([sy[i] for i in ids])
        best = None
        for t in sorted(set([0.5] + list(p))):
            acc = int(((p >= t).astype(int) == y).sum())
            if best is None or acc > best[1]:
                best = (t, acc)
        seed_best[mode] = best
        pred = (p >= 0.5).astype(int)
        byd = {}
        for s in seeds:
            byd.setdefault(s["difficulty"], [0, 0])
            byd[s["difficulty"]][0] += 1 if S[s["id"]]["probs"]["nested"] >= 0.5 else 0
            byd[s["difficulty"]][1] += 1
        m, p50, p95 = lat(S)
        print("seeds v0 %s: AUC hard-vs-rest %.2f, acc@0.5 %d/36, nested@0.5 by hand label %s, seed-best threshold %.3f (acc %d/36), cost $%.6f, latency mean %.0f p50 %.0f p95 %.0f ms" % (mode, roc_auc_score(y, p), (pred == y).sum(), byd, best[0], best[1], sum(r["cost_usd"] for r in S.values()), m, p50, p95))
    print()
    print("| variant | AUC vs label | nested@0.5 among 5 mid | nested@0.5 among 27 cheap | replay correct@0.5 | replay total@0.5 | vs mid | Jev cost/32 | per call | tokens/call | latency mean/p50/p95 ms |")
    print("|---|---|---|---|---|---|---|---|---|---|---|")
    res = {}
    y = np.array([1 if b.label[i] == "mid" else 0 for i in b.ids])
    for f in ("jev_bench_nosl_v0.jsonl", "jev_bench_sl_v0.jsonl", "jev_bench_nosl_v1.jsonl", "jev_bench_sl_v1.jsonl"):
        R = load(f)
        p = np.array([R[i]["probs"]["nested"] for i in b.ids])
        cost = sum(R[i]["cost_usd"] for i in b.ids)
        toks = np.mean([R[i]["input_tokens"] for i in b.ids])
        pol = {i: ("mid" if R[i]["probs"]["nested"] >= 0.5 else "cheap") for i in b.ids}
        r = b.replay(pol, cost)
        m, p50, p95 = lat(R)
        tp = int(((p >= 0.5) & (y == 1)).sum())
        fp = int(((p >= 0.5) & (y == 0)).sum())
        print("| %s | %.2f | %d/5 | %d/27 | %d/32 | $%.4f | %+.1f%% | $%.6f | $%.7f | %.0f | %.0f/%.0f/%.0f |" % (f, roc_auc_score(y, p), tp, fp, r["correct"], r["total"], 100 * (r["total"] - M) / M, cost, cost / 32, toks, m, p50, p95))
        sw = []
        for t in sorted(set([0.0, 0.5, 0.9, 0.99, 0.999, 1.01] + [round(x, 4) for x in p])):
            pol = {i: ("mid" if R[i]["probs"]["nested"] >= t else "cheap") for i in b.ids}
            rr = b.replay(pol, cost)
            sw.append((t, rr["correct"], rr["total"], rr["n_cheap"]))
        res[f] = {"p": dict(zip(b.ids, map(float, p))), "sweep": sw, "cost": cost}
    print()
    for f in ("jev_bench_nosl_v0.jsonl", "jev_bench_sl_v0.jsonl"):
        print("sweep", f)
        print("| threshold on p(nested) | correct | total incl. Jev | n cheap |")
        print("|---|---|---|---|")
        seen = set()
        for t, c, tot, n in res[f]["sweep"]:
            key = (c, n)
            if key in seen:
                continue
            seen.add(key)
            print("| %.4f | %d/32 | $%.4f | %d |" % (t, c, tot, n))
        mode = "nosl" if "nosl" in f else "sl"
        t = seed_best[mode][0]
        R = load(f)
        pol = {i: ("mid" if R[i]["probs"]["nested"] >= t else "cheap") for i in b.ids}
        rr = b.replay(pol, res[f]["cost"])
        print("threshold picked on seeds (%.3f): %d/32 $%.4f, %d cheap" % (t, rr["correct"], rr["total"], rr["n_cheap"]))
        print()
    bd = ml_data.bird()
    print("BIRD, label = always-cheap EX wrong; strong tier = Opus (always-top), EX grading")
    for mode in ("nosl", "sl"):
        R = load("jev_bird_%s_v0.jsonl" % mode)
        p = np.array([R[i]["probs"]["nested"] for i in bd["ids"]])
        cost = sum(R[i]["cost_usd"] for i in bd["ids"])
        pol = {i: ("mid" if R[i]["probs"]["nested"] >= 0.5 else "cheap") for i in bd["ids"]}
        r = ml_data.bird_replay(bd, pol, cost)
        yb = bd["y"]
        m, p50, p95 = lat(R)
        print("bird %s: AUC %.2f, nested@0.5 %d of %d label-strong, %d of %d label-cheap; replay EX %d/30 $%.4f (%d cheap); Jev $%.6f; latency mean %.0f p50 %.0f p95 %.0f" % (mode, roc_auc_score(yb, p), int(((p >= 0.5) & (yb == 1)).sum()), yb.sum(), int(((p >= 0.5) & (yb == 0)).sum()), (yb == 0).sum(), r["correct"], r["total"], r["n_cheap"], cost, m, p50, p95))
    json.dump(res, open(os.path.join(common.SP, "out_exp3.json"), "w"), indent=1)


main()
