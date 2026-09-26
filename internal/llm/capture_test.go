package llm

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

func TestCaptureSavesAnthropicBodies(t *testing.T) {
	srv, _ := fakeServer(t, 0, 500)
	dir := filepath.Join(t.TempDir(), "requests")
	c := NewClient("test-key", "", option.WithBaseURL(srv.URL), option.WithMaxRetries(0))
	c.SetCapture(NewCapture(dir))
	var trace Trace
	req := Request{
		Tier:     TierCheap,
		Purpose:  "capture-test",
		System:   []anthropic.TextBlockParam{{Text: "sys"}},
		Messages: []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("hi"))},
	}
	if _, _, err := c.Call(context.Background(), req, &trace); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Call(context.Background(), req, &trace); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"0001.json", "0002.json"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(data) == 0 || string(data[:1]) != "{" {
			t.Fatalf("%s is not a JSON object: %q", name, data)
		}
	}
	c2 := NewCapture(dir)
	if got := c2.Dir(); got != dir {
		t.Fatalf("dir %q", got)
	}
	path, err := c2.Save([]byte(`{"x":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "0003.json" {
		t.Fatalf("resume path %q", path)
	}
}

func TestCaptureSavesOpenAIPayload(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "requests")
	c := NewOpenAIClient("test", "http://127.0.0.1:1", "k", map[Tier]OpenAISettings{TierCheap: {Model: "m", MaxTokens: 8}}, nil)
	c.SetCapture(NewCapture(dir))
	body, err := TranslateRequest(Request{
		Tier:     TierCheap,
		Messages: []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("hi"))},
	}, c.Settings(TierCheap))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := c.payload(body, c.Settings(TierCheap))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.capture.Save(payload); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "0001.json")); err != nil {
		t.Fatal(err)
	}
}
