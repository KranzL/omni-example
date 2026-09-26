import json, os, sys, time
import common
from laya import Router
sys.argv = [sys.argv[0]]
import importlib.util
spec = importlib.util.spec_from_file_location("jb", os.path.join(common.SP, "jev_binary.py"))
src = open(os.path.join(common.SP, "jev_binary.py")).read().replace("\nmain()\n", "\n")
ns = {}
exec(compile(src, "jev_binary", "exec"), ns)
INSTR_Q, V0, V1 = ns["INSTR_Q"], ns["CRITERIA_V0"], ns["CRITERIA_V1"]
router = Router()
sets = {"bench": [(q["id"], q["question"]) for q in common.bench_questions()],
        "seeds": [(s["id"], s["question"]) for s in common.seeds()]}
for model in ["english", "typed-decisions"]:
    for ver, crit in [("v0", V0), ("v1", V1)]:
        for which, its in sets.items():
            q = {"structure": {"type": "choice", "instructions": INSTR_Q, "criteria": crit}}
            router.predict({"question": its[0][1]}, q, model=model)
            rows = []
            for qid, text in its:
                t = time.time()
                out = router.predict({"question": text}, q, model=model)
                ms = (time.time() - t) * 1000
                a = out["answers"]["structure"]
                rows.append({"id": qid, "choice": a["choice"], "p_nested": a["probabilities"]["nested"], "latency_ms": round(ms, 1), "model": out["routing"]["model"]})
            path = os.path.join(common.SP, "laya_%s_%s_%s.jsonl" % (which, model, ver))
            with open(path, "w") as fh:
                for r in rows:
                    fh.write(json.dumps(r) + "\n")
            print("wrote", path, "routed", rows[0]["model"], "median ms", sorted(r["latency_ms"] for r in rows)[len(rows) // 2])
