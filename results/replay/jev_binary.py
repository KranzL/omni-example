import json, os, sys, time, threading, urllib.request, urllib.error
from concurrent.futures import ThreadPoolExecutor
import common

PRICE = 0.042 / 1e6
CAP = 0.045
URL = "https://api.typesafe.ai/v1/systemone"
SPEND = os.path.join(common.SP, "spend.jsonl")
lock = threading.Lock()

INSTR_SL = "A SQL agent will answer `question` by querying the database described in `semantic_layer`. Classify the computational structure of the SQL the question needs. Ignore how long or detailed the question is; judge only the shape of the computation."
INSTR_Q = "A SQL agent will answer `question` by querying a relational database. Classify the computational structure of the SQL the question needs. Ignore how long or detailed the question is; judge only the shape of the computation."
CRITERIA_V0 = {
    "single_pass": "The answer comes from one aggregation over existing rows, possibly with joins, filters, grouping or ranking.",
    "nested": "Answering requires computing a per-entity intermediate, such as each user's order sequence, gaps between ordered events, or a percentile over per-entity aggregates, and then aggregating again.",
}
CRITERIA_V1 = {
    "single_pass": "One level of aggregation over rows that already exist in the tables: a lookup, a filtered count or sum, a standard metric, a GROUP BY or ranking over one dimension, a share or ratio of two totals, a condition that a row or an entity has at least N matching rows, or a window after an entity's own creation time. Joins and date filters do not change this.",
    "nested": "The SQL must first build a per-entity intermediate that does not exist as a column, such as each user's ordered sequence of orders (first, second, third order), the gap or elapsed days between an entity's consecutive events, or each user's total spend, and then aggregate that intermediate again across entities with a median, percentile or other statistic.",
}


CRITERIA = [CRITERIA_V0]


def spent():
    if not os.path.exists(SPEND):
        return 0.0
    t = 0.0
    for line in open(SPEND):
        d = json.loads(line)
        if d["provider"] == "jev":
            t += d["cost_usd"]
    return t


def ask(key, state, instr):
    body = json.dumps({"state": state, "model": "jev-latest", "questions": {"structure": {"type": "choice", "instructions": instr, "criteria": CRITERIA[0]}}}).encode()
    last = None
    for attempt in range(3):
        req = urllib.request.Request(URL, data=body, headers={"Content-Type": "application/json", "Authorization": "Bearer " + key})
        t0 = time.time()
        try:
            with urllib.request.urlopen(req, timeout=30) as r:
                resp = json.loads(r.read())
            return resp, int((time.time() - t0) * 1000), attempt + 1
        except urllib.error.HTTPError as e:
            last = "status %d" % e.code
            if e.code not in (429, 529) and e.code < 500:
                break
        except Exception as e:
            last = str(e)[:200]
        time.sleep(0.5 * (2 ** attempt))
    raise RuntimeError(last)


def items(which):
    if which == "bench":
        return [(q["id"], q["question"], "ecomm") for q in common.bench_questions()]
    if which == "seeds":
        return [(s["id"], s["question"], "ecomm") for s in common.seeds()]
    if which == "bird":
        return [(q["id"], q["question"], q["db_id"]) for q in common.bird_questions()]
    raise SystemExit("unknown set")


def semtext(db):
    name = "semantic_ecomm.txt" if db == "ecomm" else "semantic_bird_" + db + ".txt"
    return open(os.path.join(common.SP, name)).read()


def main():
    which, mode, ver = sys.argv[1], sys.argv[2], sys.argv[3]
    CRITERIA[0] = CRITERIA_V0 if ver == "v0" else CRITERIA_V1
    only = sys.argv[4].split(",") if len(sys.argv) > 4 else None
    key = common.load_env()["JEV_API_KEY"]
    its = items(which)
    if only:
        its = [x for x in its if x[0] in only]
    before = spent()
    est_tokens = sum((len(semtext(db)) // 3 if mode == "sl" else 0) + 300 for _, _, db in its)
    if before + est_tokens * PRICE > CAP:
        raise SystemExit("would exceed cap: spent %.5f, estimate %.5f" % (before, est_tokens * PRICE))
    out_path = os.path.join(common.SP, "jev_%s_%s_%s.jsonl" % (which, mode, ver))
    results = []

    def one(it):
        qid, text, db = it
        state = {"semantic_layer": semtext(db), "question": text} if mode == "sl" else {"question": text}
        instr = INSTR_SL if mode == "sl" else INSTR_Q
        try:
            resp, ms, att = ask(key, state, instr)
        except Exception as e:
            return {"id": qid, "error": str(e)}
        a = resp["answers"]["structure"]
        u = resp.get("usage", {})
        cost = u.get("input_tokens", 0) * PRICE
        return {"id": qid, "choice": a.get("choice"), "probs": a.get("probabilities"), "confidence": a.get("confidence"), "input_tokens": u.get("input_tokens"), "output_tokens": u.get("output_tokens"), "cost_usd": cost, "latency_ms": ms, "attempts": att, "model": resp.get("model")}

    with ThreadPoolExecutor(max_workers=6) as ex:
        results = list(ex.map(one, its))
    total = sum(r.get("cost_usd", 0) for r in results)
    toks = sum(r.get("input_tokens") or 0 for r in results)
    with open(out_path, "w") as fh:
        for r in results:
            fh.write(json.dumps(r) + "\n")
    with open(SPEND, "a") as fh:
        fh.write(json.dumps({"provider": "jev", "what": "%s %s %s" % (which, mode, ver), "calls": len(results), "tokens": toks, "cost_usd": total}) + "\n")
    errs = [r for r in results if "error" in r]
    print("wrote", out_path, "calls", len(results), "errors", len(errs), "tokens", toks, "cost %.6f" % total, "cumulative jev %.6f" % spent())
    for r in results:
        print(r["id"], r.get("choice"), r.get("probs"), r.get("latency_ms"), r.get("error", ""))


main()
