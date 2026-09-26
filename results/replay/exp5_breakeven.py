import json, os
import numpy as np
import common

b = common.Bench()
M = b.mid_total()
n = len(b.ids)
lab_oracle = b.replay(dict(b.label))["total"]
cost_oracle_pol = {i: ("cheap" if b.cheap[i]["correct"] and b.cheap[i]["cost_usd"] <= b.mid[i]["cost_usd"] else "mid") for i in b.ids}
cost_oracle = b.replay(cost_oracle_pol)["total"]
hind = b.replay({i: ("mid" if b.diff[i] == "expert" else "cheap") for i in b.ids})["total"]
print("always-mid $%.4f, %.5f per question" % (M, M / n))
for name, t in (("label oracle", lab_oracle), ("cost oracle", cost_oracle), ("hindsight difficulty", hind)):
    print("%s $%.4f; saving $%.4f = %.1f%%; break-even overhead per question $%.5f (%.0f%% of a mean mid call)" % (name, t, M - t, 100 * (M - t) / M, (M - t) / n, 100 * (M - t) / M))

print()
print("Break-even overhead per question for a perfect binary router under other traffic mixes (label oracle; per-difficulty means from the 32 recorded questions):")
per = {}
for d in ("easy", "moderate", "hard", "expert"):
    ids = [i for i in b.ids if b.diff[i] == d]
    mid = np.mean([b.mid[i]["cost_usd"] for i in ids])
    routed = np.mean([(b.cheap[i]["cost_usd"] if b.label[i] == "cheap" else b.mid[i]["cost_usd"]) for i in ids])
    per[d] = (mid, routed)
    print("  %s: mean mid $%.5f, mean perfect-routed $%.5f, saving per question $%.5f" % (d, mid, routed, mid - routed))
mixes = {"benchmark 25/25/25/25": (0.25, 0.25, 0.25, 0.25), "50/25/15/10": (0.5, 0.25, 0.15, 0.10), "70/20/7/3": (0.7, 0.2, 0.07, 0.03), "90/7/2/1": (0.9, 0.07, 0.02, 0.01), "0/0/0/100 expert only": (0, 0, 0, 1.0)}
print()
print("| traffic mix easy/moderate/hard/expert | always-mid per question | perfect router per question | break-even overhead per question | saving % |")
print("|---|---|---|---|---|")
rows = []
for name, w in mixes.items():
    mid = sum(w[k] * per[d][0] for k, d in enumerate(per))
    rt = sum(w[k] * per[d][1] for k, d in enumerate(per))
    rows.append((name, mid, rt))
    print("| %s | $%.5f | $%.5f | $%.5f | %.1f%% |" % (name, mid, rt, mid - rt, 100 * (mid - rt) / mid))

print()
print("Cheap-able share s (two classes: 27 cheap-able, 5 need-mid, class means from the benchmark):")
ca = [i for i in b.ids if b.label[i] == "cheap"]
nm = [i for i in b.ids if b.label[i] == "mid"]
c1 = np.mean([b.cheap[i]["cost_usd"] for i in ca])
m1 = np.mean([b.mid[i]["cost_usd"] for i in ca])
m2 = np.mean([b.mid[i]["cost_usd"] for i in nm])
print("  cheap-able: mean cheap $%.5f, mean mid $%.5f; need-mid: mean mid $%.5f" % (c1, m1, m2))
print("| cheap-able share | break-even overhead per question | saving % of always-mid |")
print("|---|---|---|")
for s in (0.5, 0.6, 0.7, 0.84375, 0.9, 0.95):
    save = s * (m1 - c1)
    mid = s * m1 + (1 - s) * m2
    print("| %.2f | $%.5f | %.1f%% |" % (s, save, 100 * save / mid))

print()
print("Imperfect router, benchmark mix: overhead budget left after false mids (every need-mid question caught):")
print("| false mids among 27 cheap-able | remaining break-even overhead per question |")
print("|---|---|")
gains = sorted([b.mid[i]["cost_usd"] - b.cheap[i]["cost_usd"] for i in ca])
for fm in (0, 3, 5, 7, 11, 15):
    lost_mean = fm * np.mean(gains)
    print("| %d | $%.5f (mean-gain assumption) |" % (fm, ((M - lab_oracle) - lost_mean) / n))

print()
print("Reference router overheads per question: Jev with semantic layer $0.000261 (exp3), Jev question only $0.000018 (exp3), bge-m3 embedding $0.0000034 (736 tokens / 32 at $0.15/M, docs/routers.md), Haiku classifier $0.00154 ($0.04936/32, results/classifier), Haiku verifier ~$0.0028 (docs/routers.md).")
json.dump({"M": M, "label_oracle": lab_oracle, "cost_oracle": cost_oracle, "hindsight": hind, "per": per, "mixes": rows}, open(os.path.join(common.SP, "out_exp5.json"), "w"), indent=1, default=float)
