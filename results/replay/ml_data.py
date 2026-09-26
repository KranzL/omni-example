import json, os, re
import numpy as np
import common


def unit(v):
    v = np.asarray(v, dtype=float)
    return v / np.linalg.norm(v)


def ecomm():
    b = common.Bench()
    feats = {r["id"]: r for r in common.features()}
    bench_ids = b.ids
    H = np.array([[feats[i]["features"][k] for k in common.FEATURE_KEYS] for i in bench_ids], dtype=float)
    E = np.array([unit(common.load_vec(b.text[i])[0]) for i in bench_ids])
    y = np.array([1 if b.label[i] == "mid" else 0 for i in bench_ids])
    sd = common.seeds()
    sid = [s["id"] for s in sd]
    SH = np.array([[feats[i]["features"][k] for k in common.FEATURE_KEYS] for i in sid], dtype=float)
    SE = np.array([unit(common.load_vec(s["question"])[0]) for s in sd])
    sy = np.array([1 if s["difficulty"] == "hard" else 0 for s in sd])
    return {"b": b, "ids": bench_ids, "H": H, "E": E, "y": y, "seed_ids": sid, "SH": SH, "SE": SE, "sy": sy, "seed_label": [s["difficulty"] for s in sd]}


REASON = re.compile(r"words=(\d+) schema=(\d+) agg=(\d+) time=(\d+) rank=(\d+) reason=(\d+) qual=(\d+)")


def bird():
    qs = common.bird_questions()
    ids = [q["id"] for q in qs]
    text = {q["id"]: q["question"] for q in qs}
    cheap, csrc = common.by_id("bird/always-cheap")
    top, tsrc = common.by_id("bird/always-top")
    heur, hsrc = common.by_id("bird/heuristic")
    H = np.array([[int(x) for x in REASON.search(heur[i]["route_reason"]).groups()] for i in ids], dtype=float)
    E = np.array([unit(common.load_vec(text[i])[0]) for i in ids])
    y = np.array([0 if cheap[i]["ex_correct"] else 1 for i in ids])
    return {"ids": ids, "H": H, "E": E, "y": y, "cheap": cheap, "top": top, "src": (csrc, tsrc, hsrc), "diff": {q["id"]: q["difficulty"] for q in qs}}


def bird_replay(d, policy, overhead=0.0):
    c = 0
    cost = 0.0
    for i in d["ids"]:
        r = d["cheap"][i] if policy[i] == "cheap" else d["top"][i]
        c += 1 if r["ex_correct"] else 0
        cost += r["cost_usd"]
    return {"correct": c, "total": cost + overhead, "n_cheap": sum(1 for i in d["ids"] if policy[i] == "cheap")}
