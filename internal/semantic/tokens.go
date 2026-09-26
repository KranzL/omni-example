package semantic

import (
	"context"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

func ApproxTokens(text string) int {
	if text == "" {
		return 0
	}
	if n := len(text) / 4; n > 0 {
		return n
	}
	return 1
}

func CountTokens(ctx context.Context, text, apiKey, workspaceID string, opts ...option.RequestOption) (int64, error) {
	base := []option.RequestOption{option.WithAPIKey(apiKey)}
	if workspaceID != "" {
		base = append(base, option.WithHeader("anthropic-workspace-id", workspaceID))
	}
	client := anthropic.NewClient(append(base, opts...)...)
	res, err := client.Messages.CountTokens(ctx, anthropic.MessageCountTokensParams{
		Model:    anthropic.ModelClaudeHaiku4_5,
		Messages: []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(text))},
	})
	if err != nil {
		return 0, err
	}
	return res.InputTokens, nil
}
