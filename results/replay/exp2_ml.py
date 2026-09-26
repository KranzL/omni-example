import json, os, warnings
import numpy as np
from sklearn.linear_model import LogisticRegression
from sklearn.neighbors import KNeighborsClassifier
from sklearn.ensemble import GradientBoostingClassifier
from sklearn.preprocessing import StandardScaler
from sklearn.pipeline import make_pipeline
from sklearn.metrics import roc_auc_score
import common, ml_data

warnings.filterwarnings("ignore")


def models():
    return {
        "LR heur": ("H", lambda: make_pipeline(StandardScaler(), LogisticRegression(C=1.0, class_weight="balanced", max_iter=2000))),
        "LR emb": ("E", lambda: LogisticRegression(C=1.0, class_weight="balanced", max_iter=5000)),
        "kNN3 emb": ("E", lambda: KNeighborsClassifier(n_neighbors=3, metric="cosine", weights="distance")),
        "kNN5 emb": ("E", lambda: KNeighborsClassifier(n_neighbors=5, metric="cosine", weights="distance")),
        "kNN5 heur": ("H", lambda: make_pipeline(StandardScaler(), KNeighborsClassifier(n_neighbors=5))),
        "GBM heur": ("H", lambda: GradientBoostingClassifier(n_estimators=50, max_depth=2, learning_rate=0.1, random_state=0)),
        "LR heur+emb": ("HE", lambda: make_pipeline(StandardScaler(), LogisticRegression(C=0.05, class_weight="balanced", max_iter=5000))),
    }


def X(d, kind, prefix=""):
    H = d[prefix + "H"]
    E = d[prefix + "E"]
    if kind == "H":
        return H
    if kind == "E":
        return E
    return np.hstack([H, E])


def fit_predict(make, Xtr, ytr, Xte):
    if len(set(ytr)) < 2:
        return np.full(len(Xte), float(ytr[0]))
    m = make()
    m.fit(Xtr, ytr)
    return m.predict_proba(Xte)[:, 1]


TEMPLATE = {"x01", "x03", "x05", "x08"}


def groups_for(ids):
    return [("tmpl" if i in TEMPLATE else i) for i in ids]


def loo(make, Xa, ya, Xextra=None, yextra=None, groups=None):
    n = len(ya)
    p = np.zeros(n)
    if groups is None:
        groups = list(range(n))
    for k in range(n):
        tr = np.array([j for j in range(n) if groups[j] != groups[k]])
        Xtr, ytr = Xa[tr], ya[tr]
        if Xextra is not None:
            Xtr = np.vstack([Xtr, Xextra])
            ytr = np.concatenate([ytr, yextra])
        p[k] = fit_predict(make, Xtr, ytr, Xa[k:k + 1])[0]
    return p


def auc(y, p):
    if len(set(y)) < 2:
        return float("nan")
    return roc_auc_score(y, p)


def bench_eval(d, p, t):
    pol = {i: ("mid" if p[k] >= t else "cheap") for k, i in enumerate(d["ids"])}
    r = d["b"].replay(pol)
    y = d["y"]
    pred = (p >= t).astype(int)
    r["label_acc"] = int((pred == y).sum())
    r["tp"] = int(((pred == 1) & (y == 1)).sum())
    r["fn"] = int(((pred == 0) & (y == 1)).sum())
    r["fp"] = int(((pred == 1) & (y == 0)).sum())
    return r


def sweep(d, p, replay_fn):
    ts = sorted(set([0.0] + [round(x, 4) for x in p] + [1.01]))
    out = []
    for t in ts:
        pol = {i: ("mid" if p[k] >= t else "cheap") for k, i in enumerate(d["ids"])}
        r = replay_fn(pol)
        out.append((t, r["correct"], r["total"], r["n_cheap"]))
    return out


def best_full(sw, n):
    full = [s for s in sw if s[1] == n]
    return min(full, key=lambda s: s[2]) if full else None


def main():
    d = ml_data.ecomm()
    bd = ml_data.bird()
    M = d["b"].mid_total()
    res = {"ecomm": {}, "seeds": {}, "bird": {}}
    print("ecomm bench n=%d positives=%d; seeds n=%d hard=%d; bird n=%d ex-wrong-on-cheap=%d" % (len(d["y"]), d["y"].sum(), len(d["sy"]), d["sy"].sum(), len(bd["y"]), bd["y"].sum()))
    print()
    print("| model | regime | AUC | label acc@0.5 | mid caught@0.5 (of 5) | false mid@0.5 | replay correct@0.5 | replay cost@0.5 | vs mid | cheapest 32/32 threshold (in-sample pick) |")
    print("|---|---|---|---|---|---|---|---|---|---|")
    for name, (kind, make) in models().items():
        Xb = X(d, kind)
        Xs = X(d, kind, "S")
        regimes = {
            "LOO bench only": loo(make, Xb, d["y"]),
            "seeds + LOO bench": loo(make, Xb, d["y"], Xs, d["sy"]),
            "grouped bench only": loo(make, Xb, d["y"], groups=groups_for(d["ids"])),
            "seeds + grouped bench": loo(make, Xb, d["y"], Xs, d["sy"], groups=groups_for(d["ids"])),
            "seeds only": fit_predict(make, Xs, d["sy"], Xb),
        }
        for rg, p in regimes.items():
            r = bench_eval(d, p, 0.5)
            sw = sweep(d, p, d["b"].replay)
            bf = best_full(sw, 32)
            bft = "t=%.3f $%.4f (%d cheap)" % (bf[0], bf[2], bf[3]) if bf else "-"
            print("| %s | %s | %.2f | %d/32 | %d | %d | %d/32 | $%.4f | %+.1f%% | %s |" % (name, rg, auc(d["y"], p), r["label_acc"], r["tp"], r["fp"], r["correct"], r["total"], 100 * (r["total"] - M) / M, bft))
            res["ecomm"][name + " | " + rg] = {"p": dict(zip(d["ids"], map(float, p))), "at05": r, "sweep": sw, "auc": auc(d["y"], p)}
    print()
    print("Seeds, LOO within the 36 seeds, label hard vs easy+moderate (hand labels):")
    print("| model | AUC | label acc@0.5 | hard caught (of 12) | false hard |")
    print("|---|---|---|---|---|")
    for name, (kind, make) in models().items():
        Xs = X(d, kind, "S")
        p = loo(make, Xs, d["sy"])
        pred = (p >= 0.5).astype(int)
        y = d["sy"]
        print("| %s | %.2f | %d/36 | %d | %d |" % (name, auc(y, p), (pred == y).sum(), ((pred == 1) & (y == 1)).sum(), ((pred == 1) & (y == 0)).sum()))
        res["seeds"][name] = {"p": dict(zip(d["seed_ids"], map(float, p)))}
    print()
    ball = ml_data.bird_replay(bd, {i: "cheap" for i in bd["ids"]})
    btop = ml_data.bird_replay(bd, {i: "mid" for i in bd["ids"]})
    orac = ml_data.bird_replay(bd, {i: ("mid" if bd["y"][k] else "cheap") for k, i in enumerate(bd["ids"])})
    print("BIRD (Anthropic, EX grading; strong tier is Opus because no Anthropic always-mid BIRD run exists): always-cheap %d/30 $%.4f, always-top %d/30 $%.4f, oracle %d/30 $%.4f" % (ball["correct"], ball["total"], btop["correct"], btop["total"], orac["correct"], orac["total"]))
    print("| model | AUC | label acc@0.5 | strong caught@0.5 (of %d) | false strong | replay EX@0.5 | replay cost@0.5 |" % bd["y"].sum())
    print("|---|---|---|---|---|---|---|")
    for name, (kind, make) in models().items():
        Xb = X(bd, kind)
        p = loo(make, Xb, bd["y"])
        pred = (p >= 0.5).astype(int)
        y = bd["y"]
        r = ml_data.bird_replay(bd, {i: ("mid" if p[k] >= 0.5 else "cheap") for k, i in enumerate(bd["ids"])})
        print("| %s | %.2f | %d/30 | %d | %d | %d/30 | $%.4f |" % (name, auc(y, p), (pred == y).sum(), ((pred == 1) & (y == 1)).sum(), ((pred == 1) & (y == 0)).sum(), r["correct"], r["total"]))
        res["bird"][name] = {"p": dict(zip(bd["ids"], map(float, p))), "at05": r}
    json.dump(res, open(os.path.join(common.SP, "out_exp2.json"), "w"), indent=1, default=float)


main()
