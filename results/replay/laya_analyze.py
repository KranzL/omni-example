import json, os
import common
ch = common.by_id("always-cheap")[0]
md = common.by_id("always-mid")[0]
bench_lab = {i: (0 if ch[i]["correct"] else 1) for i in ch}
seed_lab = {s["id"]: (1 if s["difficulty"] == "hard" else 0) for s in common.seeds()}
def auc(p, y):
    pos = [p[i] for i in p if y[i]]; neg = [p[i] for i in p if not y[i]]
    s = sum((a > b) + 0.5 * (a == b) for a in pos for b in neg)
    return s / (len(pos) * len(neg))
def replay(p, t):
    cost = 0; ok = 0; nc = 0
    for i in ch:
        r = md[i] if p[i] >= t else ch[i]
        nc += p[i] < t
        cost += r["cost_usd"]; ok += bool(r["correct"])
    return ok, cost, nc
def load(path):
    return {json.loads(l)["id"]: json.loads(l)["p_nested"] for l in open(path)}
out = []
mid_total = sum(md[i]["cost_usd"] for i in md)
for model in ["english", "typed-decisions"]:
    for ver in ["v0", "v1"]:
        pb = load(os.path.join(common.SP, "laya_bench_%s_%s.jsonl" % (model, ver)))
        ps = load(os.path.join(common.SP, "laya_seeds_%s_%s.jsonl" % (model, ver)))
        caught = sum(1 for i in pb if bench_lab[i] and pb[i] >= 0.5)
        fm = sum(1 for i in pb if not bench_lab[i] and pb[i] >= 0.5)
        hs = sum(1 for i in ps if seed_lab[i] and ps[i] >= 0.5)
        es = sum(1 for i in ps if not seed_lab[i] and ps[i] >= 0.5)
        ts = sorted(set(ps.values()))
        best = None
        for t in ts:
            if all(ps[i] >= t for i in ps if seed_lab[i]):
                best = t
        ok5, c5, n5 = replay(pb, 0.5)
        okb, cb, nb = replay(pb, best)
        row = dict(model=model, ver=ver, auc_bench=round(auc(pb, bench_lab), 3), auc_seeds=round(auc(ps, seed_lab), 3),
                   caught_at_05="%d/5" % caught, false_mid_at_05="%d/27" % fm, hard_seeds_at_05="%d/12" % hs, easy_seeds_flagged="%d/24" % es,
                   replay_05="%d/32 $%.4f %+.1f%% (%d cheap)" % (ok5, c5, (c5 / mid_total - 1) * 100, n5),
                   seed_thr=round(best, 4), replay_seed_thr="%d/32 $%.4f %+.1f%% (%d cheap)" % (okb, cb, (cb / mid_total - 1) * 100, nb),
                   x_probs={i: round(pb[i], 3) for i in sorted(pb) if i[0] == "x"})
        out.append(row)
        print(json.dumps(row))
json.dump(out, open(os.path.join(common.SP, "out_laya.json"), "w"), indent=1)
