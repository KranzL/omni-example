import json, os, warnings
import numpy as np
from sklearn.linear_model import LogisticRegression
from sklearn.neighbors import KNeighborsClassifier
from sklearn.preprocessing import StandardScaler
from sklearn.pipeline import make_pipeline
import common, ml_data

warnings.filterwarnings("ignore")
ORDERS = 200
CHECK = [4, 8, 16, 24, 32]

MODELS = {
    "kNN5 emb": ("E", lambda: KNeighborsClassifier(n_neighbors=5, metric="cosine", weights="distance")),
    "LR heur": ("H", lambda: make_pipeline(StandardScaler(), LogisticRegression(C=1.0, class_weight="balanced", max_iter=2000))),
}


def predict(make, X, y, x):
    if len(set(y)) < 2:
        return float(y[0])
    m = make()
    m.fit(X, y)
    return m.predict_proba(x.reshape(1, -1))[0, 1]


def simulate(d, kind, make, shadow, rng):
    b = d["b"]
    Xb = d[kind]
    Xs = d["S" + kind]
    ids = d["ids"]
    order = rng.permutation(len(ids))
    X = list(Xs)
    y = list(d["sy"])
    stats = []
    cum_cost = cum_mid = 0.0
    cum_route_ok = cum_correct = 0
    for step, k in enumerate(order, 1):
        i = ids[k]
        p = predict(make, np.array(X), np.array(y), Xb[k])
        tier = "mid" if p >= 0.5 else "cheap"
        label = d["y"][k]
        cum_route_ok += 1 if (tier == "mid") == (label == 1) else 0
        cum_mid += b.mid[i]["cost_usd"]
        shadowed = shadow >= 1.0 or (shadow > 0 and rng.random() < shadow)
        if shadowed:
            cum_cost += b.cheap[i]["cost_usd"] + b.mid[i]["cost_usd"]
            cum_correct += 1 if b.mid[i]["correct"] else 0
            X.append(Xb[k])
            y.append(label)
        else:
            r = b.cheap[i] if tier == "cheap" else b.mid[i]
            cum_cost += r["cost_usd"]
            cum_correct += 1 if r["correct"] else 0
        if step in CHECK:
            rest = order[step:]
            if len(rest):
                pr = np.array([predict(make, np.array(X), np.array(y), Xb[j]) >= 0.5 for j in rest]).astype(int)
                yr = d["y"][rest]
                rest_acc = (pr == yr).mean()
                pos = yr.sum()
                rest_rec = ((pr == 1) & (yr == 1)).sum() / pos if pos else np.nan
            else:
                rest_acc = rest_rec = np.nan
            stats.append((step, cum_route_ok / step, cum_correct, cum_cost, cum_mid, rest_acc, rest_rec))
    return stats


def free_labels(d, kind, make, rng):
    b = d["b"]
    Xb = d[kind]
    ids = d["ids"]
    order = rng.permutation(len(ids))
    X = list(d["S" + kind])
    y = list(d["sy"])
    stats = []
    cum_cost = cum_mid = 0.0
    ok = corr = 0
    for step, k in enumerate(order, 1):
        i = ids[k]
        p = predict(make, np.array(X), np.array(y), Xb[k])
        tier = "mid" if p >= 0.5 else "cheap"
        label = d["y"][k]
        ok += 1 if (tier == "mid") == (label == 1) else 0
        r = b.cheap[i] if tier == "cheap" else b.mid[i]
        cum_cost += r["cost_usd"]
        cum_mid += b.mid[i]["cost_usd"]
        corr += 1 if r["correct"] else 0
        X.append(Xb[k])
        y.append(label)
        if step in CHECK:
            rest = order[step:]
            if len(rest):
                pr = np.array([predict(make, np.array(X), np.array(y), Xb[j]) >= 0.5 for j in rest]).astype(int)
                yr = d["y"][rest]
                rest_acc = (pr == yr).mean()
                pos = yr.sum()
                rest_rec = ((pr == 1) & (yr == 1)).sum() / pos if pos else np.nan
            else:
                rest_acc = rest_rec = np.nan
            stats.append((step, ok / step, corr, cum_cost, cum_mid, rest_acc, rest_rec))
    return stats


def summarize(all_stats):
    arr = np.array(all_stats, dtype=float)
    rows = []
    for j, n in enumerate(CHECK):
        a = arr[:, j, :]
        wrong = n - a[:, 2]
        rows.append({"n": n, "route_acc": np.nanmean(a[:, 1]), "correct": np.mean(a[:, 2]), "wrong_mean": np.mean(wrong), "p_any_wrong": np.mean(wrong > 0), "cost": np.mean(a[:, 3]), "mid_cost": np.mean(a[:, 4]), "ratio": np.mean(a[:, 3] / a[:, 4]), "rest_acc": np.nanmean(a[:, 5]), "rest_rec": np.nanmean(a[:, 6])})
    return rows


def main():
    d = ml_data.ecomm()
    out = {}
    print("%d random orders per row. Start: 36 seeds (hard -> mid). Label from always-cheap outcome." % ORDERS)
    for name, (kind, make) in MODELS.items():
        for mode in ["static (no learning)", "free labels (eval labels every question)", "shadow 0.25", "shadow 0.5", "shadow 1.0"]:
            rng = np.random.default_rng(7)
            runs = []
            for _ in range(ORDERS):
                if mode.startswith("static"):
                    runs.append(simulate(d, kind, make, 0.0, rng))
                elif mode.startswith("free"):
                    runs.append(free_labels(d, kind, make, rng))
                else:
                    runs.append(simulate(d, kind, make, float(mode.split()[1]), rng))
            rows = summarize(runs)
            out[name + " | " + mode] = rows
            print()
            print("%s, %s" % (name, mode))
            print("| questions seen | cumulative routing acc | mean correct | P(any wrong so far) | mean cumulative cost | always-mid on same questions | cost / always-mid | routing acc on unseen | mid recall on unseen |")
            print("|---|---|---|---|---|---|---|---|---|")
            for r in rows:
                print("| %d | %.3f | %.2f/%d | %.2f | $%.4f | $%.4f | %.3f | %s | %s |" % (r["n"], r["route_acc"], r["correct"], r["n"], r["p_any_wrong"], r["cost"], r["mid_cost"], r["ratio"], "-" if np.isnan(r["rest_acc"]) else "%.3f" % r["rest_acc"], "-" if np.isnan(r["rest_rec"]) else "%.3f" % r["rest_rec"]))
    json.dump(out, open(os.path.join(common.SP, "out_exp4.json"), "w"), indent=1, default=float)


main()
