package main

import (
	"context"
	"fmt"
	"time"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/KranzL/omni-example/internal/config"
	"github.com/KranzL/omni-example/internal/llm"
)

const modelsPrompt = "Reply with one short sentence: what is 2 + 2?"

const modelsUsage = "usage: harness models [--provider anthropic|venice]"

func models(args []string) error {
	flag, err := parseProviderOnly(args, modelsUsage)
	if err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	provider, err := resolveProvider(cfg, flag)
	if err != nil {
		return err
	}
	client, err := newProvider(cfg, provider)
	if err != nil {
		return err
	}
	var trace llm.Trace
	var failed int
	for _, tier := range llm.Tiers {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		_, rec, err := client.Call(ctx, llm.Request{
			Tier:     tier,
			Purpose:  "models-smoke",
			Messages: []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(modelsPrompt))},
		}, &trace)
		cancel()
		if err != nil {
			failed++
			fmt.Printf("%-5s %s error after %d attempts: %v\n", tier, rec.Model, rec.Attempts, err)
			continue
		}
		u := rec.Usage
		fmt.Printf("%-5s %-25s input=%d output=%d cache_write=%d cache_read=%d cost=$%.6f latency=%dms attempts=%d stop=%s\n",
			tier, rec.Model, u.InputTokens, u.OutputTokens, u.CacheCreationInputTokens, u.CacheReadInputTokens,
			rec.CostUSD, rec.LatencyMS, rec.Attempts, rec.StopReason)
	}
	path := llm.TracePath("models-" + provider + "-" + time.Now().UTC().Format("20060102T150405Z"))
	if err := llm.WriteJSONL(path, trace.Records()); err != nil {
		return err
	}
	fmt.Printf("provider=%s total cost=$%.6f trace=%s\n", provider, trace.TotalCost(), path)
	if failed > 0 {
		return fmt.Errorf("%d of %d tiers failed", failed, len(llm.Tiers))
	}
	return nil
}
