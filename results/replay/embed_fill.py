import json, os, sys, time, urllib.request
import common

PRICE = 0.15 / 1e6


def texts():
    t = [q["question"] for q in common.bench_questions()]
    t += [s["question"] for s in common.seeds()]
    t += [q["question"] for q in common.bird_questions()]
    t += [s["question"] for s in common.bird_seeds()]
    return t


def main():
    dry = "--dry" in sys.argv
    all_t = texts()
    missing = []
    hits = {}
    for t in all_t:
        v, d = common.load_vec(t)
        if v is None:
            if t not in missing:
                missing.append(t)
        else:
            hits[d] = hits.get(d, 0) + 1
    print("texts", len(all_t), "cached", hits, "missing", len(missing))
    if dry or not missing:
        return
    key = common.load_env()["VENICE_API_KEY"]
    body = json.dumps({"model": common.EMB_MODEL, "input": missing}).encode()
    req = urllib.request.Request("https://api.venice.ai/api/v1/embeddings", data=body, headers={"Content-Type": "application/json", "Authorization": "Bearer " + key})
    t0 = time.time()
    with urllib.request.urlopen(req, timeout=120) as r:
        resp = json.loads(r.read())
    ms = int((time.time() - t0) * 1000)
    tokens = resp.get("usage", {}).get("prompt_tokens") or resp.get("usage", {}).get("total_tokens") or 0
    os.makedirs(common.EMB_CACHES[0], exist_ok=True)
    data = sorted(resp["data"], key=lambda d: d["index"])
    for t, d in zip(missing, data):
        with open(os.path.join(common.EMB_CACHES[0], common.emb_key(t) + ".json"), "w") as fh:
            json.dump({"model": common.EMB_MODEL, "text": t, "embedding": d["embedding"]}, fh)
    cost = tokens * PRICE
    with open(os.path.join(common.SP, "spend.jsonl"), "a") as fh:
        fh.write(json.dumps({"provider": "venice", "what": "bge-m3 embeddings", "texts": len(missing), "tokens": tokens, "cost_usd": cost, "latency_ms": ms}) + "\n")
    print("embedded", len(missing), "tokens", tokens, "cost_usd %.6f" % cost, "latency_ms", ms)


main()
