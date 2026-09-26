import json, os, hashlib, math
import yaml

SP = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.abspath(os.path.join(SP, "..", ".."))
EMB_MODEL = "text-embedding-bge-m3"
EMB_CACHES = [os.path.join(SP, "embcache"), os.path.join(REPO, "results/embeddings")]
PINNED = {
    "always-cheap": "results/always-cheap/20260924T190155Z.jsonl",
    "always-mid": "results/always-mid/20260924T162945Z.jsonl",
    "heuristic": "results/heuristic/20260924T184116Z.jsonl",
    "heuristic-v2": "results/heuristic-v2/20260924T183941Z.jsonl",
    "classifier": "results/classifier/20260924T043814Z.jsonl",
    "embedding": "results/embedding/20260924T045143Z.jsonl",
    "jev-classifier": "results/jev-classifier/20260924T190322Z.jsonl",
    "bird/always-cheap": "results/bird/always-cheap/20260924T235053Z.jsonl",
    "bird/always-top": "results/bird/always-top/20260924T235715Z.jsonl",
    "bird/heuristic": "results/bird/heuristic/20260924T235352Z.jsonl",
}


def load_env():
    env = {}
    with open(os.environ.get("OMNI_ENV_FILE") or os.path.join(REPO, ".env")) as fh:
        for line in fh:
            line = line.strip()
            if not line or line.startswith("#") or "=" not in line:
                continue
            k, v = line.split("=", 1)
            env[k.strip()] = v.strip().strip('"').strip("'")
    return env


def latest_source(cfg):
    if cfg in PINNED:
        return os.path.join(REPO, PINNED[cfg])
    d = json.load(open(os.path.join(REPO, "results", cfg, "latest.json")))
    return os.path.join(REPO, d["source"])


def read_lines(path):
    out = []
    with open(path) as fh:
        for line in fh:
            line = line.strip()
            if line:
                out.append(json.loads(line))
    return out


def by_id(cfg):
    src = latest_source(cfg)
    rows = {}
    for d in read_lines(src):
        rows.setdefault(d["id"], d)
    return rows, src


def bench_questions():
    return yaml.safe_load(open(os.path.join(REPO, "bench/questions.yaml")))


def seeds():
    return yaml.safe_load(open(os.path.join(REPO, "bench/router_seed.yaml")))


def bird_questions():
    return yaml.safe_load(open(os.path.join(REPO, "bench/bird/questions.yaml")))


def bird_seeds():
    return yaml.safe_load(open(os.path.join(REPO, "bench/bird/router_seed.yaml")))


def features():
    return json.load(open(os.path.join(SP, "features.json")))


FEATURE_KEYS = ["words", "schema_mentions", "aggregation", "time", "ranking", "reasoning", "qualifier"]


def emb_key(text):
    return hashlib.sha256((EMB_MODEL + "\x00" + text).encode()).hexdigest()


def load_vec(text):
    k = emb_key(text)
    for d in EMB_CACHES:
        p = os.path.join(d, k + ".json")
        if os.path.exists(p):
            c = json.load(open(p))
            if c.get("model") == EMB_MODEL and c.get("text") == text and c.get("embedding"):
                return c["embedding"], d
    return None, None


class Bench:
    def __init__(self):
        self.cheap, self.cheap_src = by_id("always-cheap")
        self.mid, self.mid_src = by_id("always-mid")
        self.qs = bench_questions()
        self.ids = [q["id"] for q in self.qs]
        self.diff = {q["id"]: q["difficulty"] for q in self.qs}
        self.text = {q["id"]: q["question"] for q in self.qs}
        self.label = {i: ("cheap" if self.cheap[i]["correct"] else "mid") for i in self.ids}

    def replay(self, policy, overhead=0.0):
        correct = 0
        agent = 0.0
        for i in self.ids:
            t = policy[i]
            r = self.cheap[i] if t == "cheap" else self.mid[i]
            correct += 1 if r["correct"] else 0
            agent += r["cost_usd"]
        return {"correct": correct, "agent_cost": agent, "overhead": overhead, "total": agent + overhead,
                "n_cheap": sum(1 for i in self.ids if policy[i] == "cheap")}

    def mid_total(self):
        return sum(self.mid[i]["cost_usd"] for i in self.ids)


def wilson(k, n, z=1.96):
    if n == 0:
        return (0.0, 0.0)
    p = k / n
    den = 1 + z * z / n
    c = (p + z * z / (2 * n)) / den
    h = z * math.sqrt(p * (1 - p) / n + z * z / (4 * n * n)) / den
    return (max(0.0, c - h), min(1.0, c + h))


def fmt_row(name, res, mid_total, extra=""):
    pct = 100.0 * (res["total"] - mid_total) / mid_total
    return "| %s | %d/32 | $%.4f | %+.1f%% | $%.5f | %d | %s |" % (name, res["correct"], res["total"], pct, res["overhead"], res["n_cheap"], extra)
