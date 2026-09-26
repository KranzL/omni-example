package bench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/KranzL/omni-example/internal/llm"
)

type TraceRecord struct {
	QuestionID  string   `json:"question_id"`
	Repeat      int      `json:"repeat"`
	AttemptTier llm.Tier `json:"attempt_tier"`
	llm.CallRecord
}

func NewTraceRecord(id string, repeat int, tier llm.Tier, rec llm.CallRecord) TraceRecord {
	return TraceRecord{QuestionID: id, Repeat: repeat, AttemptTier: tier, CallRecord: rec}
}

func appendTraceRecords(dst []TraceRecord, id string, repeat int, tier llm.Tier, recs []llm.CallRecord) []TraceRecord {
	for _, rec := range recs {
		dst = append(dst, NewTraceRecord(id, repeat, tier, rec))
	}
	return dst
}

type TraceCollector struct {
	mu    sync.Mutex
	byJob map[int][]TraceRecord
}

func NewTraceCollector() *TraceCollector {
	return &TraceCollector{byJob: map[int][]TraceRecord{}}
}

func (c *TraceCollector) AddAll(index int, recs []TraceRecord) {
	if c == nil || len(recs) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.byJob[index] = append(c.byJob[index], recs...)
}

func (c *TraceCollector) Ordered(total int) []TraceRecord {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []TraceRecord
	for i := 0; i < total; i++ {
		out = append(out, c.byJob[i]...)
	}
	return out
}

func TraceFilePath(provider, config string, noCache bool, stamp string) string {
	name := config
	if provider != "" && provider != llm.ProviderAnthropic {
		name = provider + "-" + name
	}
	if noCache {
		name += "-nocache"
	}
	return filepath.Join(llm.TraceDir, name+"-"+stamp+".jsonl")
}

func BirdTraceFilePath(provider, config string, noCache, noEvidence bool, stamp string) string {
	name := BirdDirName + "-" + config
	if provider != "" && provider != llm.ProviderAnthropic {
		name = BirdDirName + "-" + provider + "-" + config
	}
	if noCache {
		name += "-nocache"
	}
	if noEvidence {
		name += "-noevidence"
	}
	return filepath.Join(llm.TraceDir, name+"-"+stamp+".jsonl")
}

func WriteTraceFile(path string, records []TraceRecord) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
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
