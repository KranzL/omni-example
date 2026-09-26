package llm

import (
	"context"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
)

const (
	ProviderAnthropic = "anthropic"
	ProviderVenice    = "venice"
	ProviderMuse      = "muse"
)

var Providers = []string{ProviderAnthropic, ProviderVenice, ProviderMuse}

type Provider interface {
	Name() string
	Call(ctx context.Context, req Request, trace *Trace) (*anthropic.Message, CallRecord, error)
}

func ParseProvider(s string) (string, error) {
	if s == "" {
		return ProviderAnthropic, nil
	}
	for _, p := range Providers {
		if p == s {
			return p, nil
		}
	}
	return "", fmt.Errorf("unknown provider %q, want anthropic, venice, or muse", s)
}

func (c *Client) Name() string {
	return ProviderAnthropic
}
