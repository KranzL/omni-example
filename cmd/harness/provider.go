package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/KranzL/omni-example/internal/config"
	"github.com/KranzL/omni-example/internal/llm"
	"github.com/KranzL/omni-example/internal/muse"
)

func resolveProvider(cfg config.Config, flag string) (string, error) {
	if flag != "" {
		return llm.ParseProvider(flag)
	}
	return llm.ParseProvider(cfg.LLMProvider)
}

func defaultProvider(flag string) (string, error) {
	if flag != "" {
		return llm.ParseProvider(flag)
	}
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}
	return resolveProvider(cfg, "")
}

func veniceModels(cfg config.Config) map[llm.Tier]string {
	out := map[llm.Tier]string{}
	for _, tier := range llm.Tiers {
		out[tier] = cfg.VeniceModels[string(tier)]
	}
	return out
}

func newProvider(cfg config.Config, name string) (llm.Provider, error) {
	switch name {
	case llm.ProviderVenice:
		if cfg.VeniceAPIKey == "" {
			return nil, fmt.Errorf("missing %s", config.VeniceAPIKeyName)
		}
		return llm.NewVeniceClient(cfg.VeniceAPIKey, veniceModels(cfg)), nil
	case llm.ProviderMuse:
		return nil, nil
	default:
		if cfg.AnthropicAPIKey == "" {
			return nil, fmt.Errorf("missing %s", config.AnthropicAPIKeyName)
		}
		return llm.NewClient(cfg.AnthropicAPIKey, cfg.AnthropicWorkspaceID), nil
	}
}

func museWorkspace() (string, error) {
	dir := filepath.Join(os.TempDir(), "omni-example-muse-workspace")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	dst := filepath.Join(dir, "harness")
	if err := copyExecutable(self, dst); err != nil {
		return "", err
	}
	return dir, nil
}

func copyExecutable(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func newMuseAgent(cfg config.Config, semText string) (*muse.Agent, error) {
	dir, err := museWorkspace()
	if err != nil {
		return nil, err
	}
	return muse.New(dir, semText, cfg.DatabaseURL), nil
}

func enableCapture(p llm.Provider, dir string) {
	cap := llm.NewCapture(dir)
	switch c := p.(type) {
	case *llm.Client:
		c.SetCapture(cap)
	case *llm.OpenAIClient:
		c.SetCapture(cap)
	}
}

func parseProviderOnly(args []string, usage string) (string, error) {
	provider := ""
	for i := 0; i < len(args); i++ {
		name, value, hasValue := strings.Cut(strings.TrimLeft(args[i], "-"), "=")
		if name != "provider" || !strings.HasPrefix(args[i], "-") {
			return "", fmt.Errorf("unexpected argument %s\n%s", args[i], usage)
		}
		if !hasValue {
			if i+1 >= len(args) {
				return "", fmt.Errorf("flag %s needs a value\n%s", args[i], usage)
			}
			i++
			value = args[i]
		}
		if _, err := llm.ParseProvider(value); err != nil {
			return "", err
		}
		provider = value
	}
	return provider, nil
}

func veniceModelsCmd() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.VeniceAPIKey == "" {
		return fmt.Errorf("missing %s", config.VeniceAPIKeyName)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	list, err := llm.FetchVeniceModels(ctx, llm.VeniceBaseURL, cfg.VeniceAPIKey)
	if err != nil {
		return err
	}
	byID := map[string]llm.VeniceModel{}
	for _, m := range list {
		byID[m.ID] = m
	}
	settings := llm.VeniceSettings(veniceModels(cfg))
	mismatches := 0
	for _, tier := range llm.Tiers {
		s := settings[tier]
		m, ok := byID[s.Model]
		if !ok {
			fmt.Printf("%-5s %-20s not listed by Venice\n", tier, s.Model)
			mismatches++
			continue
		}
		in, _ := m.PriceUSD("input")
		out, _ := m.PriceUSD("output")
		cached, hasCached := m.PriceUSD("cache_input")
		write, hasWrite := m.PriceUSD("cache_write")
		c := m.ModelSpec.Capabilities
		fmt.Printf("%-5s %-20s input=%.3f output=%.3f cache_input=%s cache_write=%s tools=%t reasoning=%t effort=%t options=%s default_effort=%s context=%d\n",
			tier, s.Model, in, out, optPrice(cached, hasCached), optPrice(write, hasWrite),
			c.SupportsFunctionCalling, c.SupportsReasoning, c.SupportsReasoningEffort,
			strings.Join(c.ReasoningEffortOptions, ","), c.DefaultReasoningEffort, m.ModelSpec.AvailableContextTokens)
		pinned, ok := llm.PriceFor(s.Model)
		if !ok {
			fmt.Printf("      no pinned price for %s\n", s.Model)
			mismatches++
			continue
		}
		wantCached := in
		if hasCached {
			wantCached = cached
		}
		if pinned.Input != in || pinned.Output != out || pinned.CacheRead != wantCached {
			fmt.Printf("      pinned price differs: input=%.3f output=%.3f cache_read=%.3f\n", pinned.Input, pinned.Output, pinned.CacheRead)
			mismatches++
		}
	}
	if mismatches > 0 {
		return fmt.Errorf("%d models differ from the pinned table", mismatches)
	}
	fmt.Println("pinned prices match the live catalog")
	return nil
}

func optPrice(v float64, ok bool) string {
	if !ok {
		return "-"
	}
	return fmt.Sprintf("%.3f", v)
}
