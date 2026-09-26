package llm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const TraceDir = "results/traces"

type CallRecord struct {
	Time       time.Time `json:"time"`
	Provider   string    `json:"provider,omitempty"`
	Model      string    `json:"model"`
	Tier       Tier      `json:"tier"`
	Purpose    string    `json:"purpose"`
	Usage      Usage     `json:"usage"`
	CostUSD    float64   `json:"cost_usd"`
	LatencyMS  int64     `json:"latency_ms"`
	StopReason string    `json:"stop_reason"`
	RequestID  string    `json:"request_id"`
	MessageID  string    `json:"message_id,omitempty"`
	Attempts   int       `json:"attempts"`
	Error      string    `json:"error,omitempty"`
}

type Trace struct {
	mu      sync.Mutex
	records []CallRecord
}

func (t *Trace) Append(r CallRecord) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.records = append(t.records, r)
}

func (t *Trace) Records() []CallRecord {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]CallRecord, len(t.records))
	copy(out, t.records)
	return out
}

func (t *Trace) TotalCost() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	var total float64
	for _, r := range t.records {
		total += r.CostUSD
	}
	return total
}

func (t *Trace) TotalUsage() Usage {
	t.mu.Lock()
	defer t.mu.Unlock()
	var total Usage
	for _, r := range t.records {
		total = total.Add(r.Usage)
	}
	return total
}

func TracePath(name string) string {
	return filepath.Join(TraceDir, name+".jsonl")
}

func WriteJSONL(path string, records []CallRecord) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	for _, r := range records {
		if err := enc.Encode(r); err != nil {
			f.Close()
			return err
		}
	}
	return f.Close()
}

func ReadJSONL(path string) ([]CallRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []CallRecord
	dec := json.NewDecoder(f)
	for dec.More() {
		var r CallRecord
		if err := dec.Decode(&r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}
