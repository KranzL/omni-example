import glob
import hashlib
import json
import os

SP = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.abspath(os.path.join(SP, "..", ".."))
RUN = "results/always-cheap-routectx/20260926T160100Z.jsonl"


def digest(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True).encode()).hexdigest()


def first_user_text(body):
    content = body["messages"][0]["content"]
    if isinstance(content, str):
        return content
    return "".join(block.get("text", "") for block in content if isinstance(block, dict))


files = sorted(glob.glob(os.path.join(REPO, "results", "requests", "*.json")))
prefixes = set()
with_hint = 0
hint_in_prefix = 0
for f in files:
    with open(f) as fh:
        body = json.load(fh)
    prefixes.add(digest({"model": body["model"], "system": body["system"], "tools": body["tools"]}))
    if "Context for this question:" in first_user_text(body):
        with_hint += 1
    if "Context for this question:" in json.dumps(body["system"]) + json.dumps(body["tools"]):
        hint_in_prefix += 1

with open(os.path.join(REPO, RUN)) as fh:
    rows = [json.loads(line) for line in fh if line.strip()]
expert = [r for r in rows if r["difficulty"] == "expert"]
out = {
    "run": RUN,
    "captured_requests": len(files),
    "distinct_model_system_tools_prefixes": len(prefixes),
    "prefix_sha256": sorted(prefixes),
    "requests_with_hint_in_first_user_message": with_hint,
    "requests_with_hint_in_system_or_tools": hint_in_prefix,
    "expert_answers": len(expert),
    "expert_answers_reading_cache": sum(1 for r in expert if r["tokens"]["cache_read_input_tokens"] > 0),
    "note": "Captured with --capture-requests during the run above. results/requests/ is gitignored, so this file records the check.",
}
with open(os.path.join(SP, "out_route_context_prefix.json"), "w") as fh:
    json.dump(out, fh, indent=1)
print(json.dumps(out, indent=1))
