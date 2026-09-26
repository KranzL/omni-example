import json, os
import common

b = common.Bench()
M = b.mid_total()
out = {"mid_total": M, "rows": []}
lines = []
lines.append("| policy | correct | total cost | vs always-mid | routing overhead | n cheap | source |")
lines.append("|---|---|---|---|---|---|---|")


def add(name, policy, overhead, src):
    r = b.replay(policy, overhead)
    r["name"] = name
    r["source"] = src
    out["rows"].append(r)
    lines.append(common.fmt_row(name, r, M, src))
    return r


add("always-mid", {i: "mid" for i in b.ids}, 0.0, os.path.relpath(b.mid_src, common.REPO))
add("always-cheap", {i: "cheap" for i in b.ids}, 0.0, os.path.relpath(b.cheap_src, common.REPO))
add("hindsight e/m/h cheap, x mid", {i: ("mid" if b.diff[i] == "expert" else "cheap") for i in b.ids}, 0.0, "difficulty labels")
add("oracle (label)", dict(b.label), 0.0, "per-question outcome")

cache_note = {}
for name, rows in (("cheap", b.cheap), ("mid", b.mid)):
    w = [(i, rows[i]["tokens"].get("cache_creation_input_tokens", 0), rows[i]["cost_usd"]) for i in b.ids if rows[i]["tokens"].get("cache_creation_input_tokens", 0) > 1000]
    cache_note[name] = w
out["cache_writes"] = cache_note

recorded = {}
for cfg in ["heuristic", "heuristic-v2", "classifier", "embedding", "jev-classifier"]:
    rows, src = common.by_id(cfg)
    allrows = common.read_lines(src)
    rep1 = {}
    for d in allrows:
        if str(d.get("repeat", 1)) == "1":
            rep1.setdefault(d["id"], d)
    tiers = {i: rep1[i]["tier"] for i in b.ids}
    overhead = sum(rep1[i].get("route_cost_usd", 0) or 0 for i in b.ids)
    note = ""
    if cfg == "embedding":
        overhead = 0.000110
        note = " (cold cost from docs/routers.md; run was cached)"
    if cfg == "jev-classifier":
        jev_part = sum((rep1[i].get("route_gate") or {}).get("primary_cost_usd", 0) for i in b.ids)
        fb = [i for i in b.ids if (rep1[i].get("route_gate") or {}).get("fell_back")]
        recorded["jev_fallbacks"] = fb
        recorded["jev_primary_cost"] = jev_part
    policy = {i: ("cheap" if tiers[i] == "cheap" else "mid") for i in b.ids}
    mix = {t: sum(1 for i in b.ids if tiers[i] == t) for t in ("cheap", "mid", "top")}
    recorded[cfg] = {"mix": mix, "overhead": overhead, "src": os.path.relpath(src, common.REPO)}
    missed = [i for i in b.ids if policy[i] == "cheap" and b.label[i] == "mid"]
    over = [i for i in b.ids if policy[i] == "mid" and b.label[i] == "cheap"]
    r = add(cfg + " two-tier", policy, overhead, os.path.relpath(src, common.REPO) + note)
    r["missed"] = missed
    r["over"] = over
    r["mix3"] = mix
    if cfg == "jev-classifier":
        add("jev-classifier two-tier, Jev-only overhead", policy, recorded["jev_primary_cost"], "primary_cost_usd only")

out["recorded"] = recorded
json.dump(out, open(os.path.join(common.SP, "out_exp1.json"), "w"), indent=1)
print("\n".join(lines))
print()
for r in out["rows"]:
    if "missed" in r:
        print(r["name"], "mix3", r["mix3"], "cheap-routed label-mid", r["missed"], "mid-routed label-cheap", len(r["over"]))
print("jev fallbacks", recorded.get("jev_fallbacks"), "jev primary cost %.6f" % recorded.get("jev_primary_cost", 0))
print("cache writes >1000 tokens", cache_note)
