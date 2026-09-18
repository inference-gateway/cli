// Package llm carries the one-shot, no-tools completion helper shared by the
// LLM side-calls: custom shortcut snippets and the insights analysis. Same
// shape as the judge and the conversation summarizer; those callers are
// expected to converge here over time.
package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"

	sdk "github.com/inference-gateway/sdk"

	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

// ErrTokenBudgetExhausted marks a model that spent max_tokens before answering.
var ErrTokenBudgetExhausted = errors.New("token budget exhausted")

// logUsage records what one side-call cost.
func logUsage(model string, maxTokens int, usage *sdk.CompletionUsage) {
	if usage == nil {
		return
	}
	var reasoning int64
	if usage.CompletionTokensDetails != nil && usage.CompletionTokensDetails.ReasoningTokens != nil {
		reasoning = *usage.CompletionTokensDetails.ReasoningTokens
	}
	logger.Debug("llm side-call usage",
		"model", model,
		"max_tokens", maxTokens,
		"prompt_tokens", usage.PromptTokens,
		"completion_tokens", usage.CompletionTokens,
		"reasoning_tokens", reasoning,
		"total_tokens", usage.TotalTokens)
}

// Call runs one no-tools completion: a single user message in, the trimmed
// assistant text out. Same shape as the judge and the conversation summarizer
// (direct client call, SkipMCP, bounded max_tokens). The caller owns the
// timeout via ctx.
func Call(ctx context.Context, client sdk.Client, model, prompt string, maxTokens int) (string, *sdk.CompletionUsage, error) {
	if client == nil {
		return "", nil, fmt.Errorf("SDK client not available")
	}
	if model == "" {
		return "", nil, fmt.Errorf("no model configured (use /model to select a model)")
	}

	provider, modelName, ok := strings.Cut(model, "/")
	if !ok {
		return "", nil, fmt.Errorf("invalid model format, expected 'provider/model'")
	}

	messages := []sdk.Message{
		{
			Role:    sdk.User,
			Content: sdk.NewMessageContent(prompt),
		},
	}

	response, err := client.
		WithOptions(&sdk.CreateChatCompletionRequest{
			MaxTokens: &maxTokens,
		}).
		WithMiddlewareOptions(&sdk.MiddlewareOptions{
			SkipMCP: true,
		}).
		GenerateContent(ctx, sdk.Provider(provider), modelName, messages)

	if err != nil {
		return "", nil, fmt.Errorf("LLM API call failed: %w", err)
	}

	if len(response.Choices) == 0 {
		return "", nil, fmt.Errorf("no response from LLM")
	}

	logUsage(model, maxTokens, response.Usage)

	contentStr, err := response.Choices[0].Message.Content.AsMessageContent0()
	if err != nil {
		return "", nil, fmt.Errorf("failed to extract LLM response content: %w", err)
	}

	// A reasoning model thinks against max_tokens and can spend the whole budget
	// returning nothing; yielding "" here is how an empty section ships.
	if strings.TrimSpace(contentStr) == "" {
		if response.Choices[0].FinishReason == sdk.Length {
			return "", response.Usage, fmt.Errorf("model spent all %d max_tokens before answering (reasoning models think against this budget): %w", maxTokens, ErrTokenBudgetExhausted)
		}
		return "", response.Usage, fmt.Errorf("model returned an empty response")
	}

	return strings.TrimSpace(contentStr), response.Usage, nil
}
