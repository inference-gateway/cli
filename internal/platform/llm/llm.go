package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

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

// progressInterval bounds how often a running call reports on progress.
const progressInterval = time.Second

// Call runs one no-tools completion: a single user message in, the trimmed
// assistant text out. Same shape as the judge and the conversation summarizer
// (direct client call, SkipMCP, bounded max_tokens). The caller owns the
// timeout via ctx.
func Call(ctx context.Context, client sdk.Client, model, prompt string, maxTokens int) (string, *sdk.CompletionUsage, error) {
	return Stream(ctx, client, model, prompt, maxTokens, io.Discard)
}

// Stream is Call with a live status line on progress: the request streams so
// a reasoning model's first bytes arrive before the gateway's header timeout,
// and the bytes seen so far are reported as they come.
func Stream(ctx context.Context, client sdk.Client, model, prompt string, maxTokens int, progress io.Writer) (string, *sdk.CompletionUsage, error) {
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

	events, err := client.
		WithOptions(&sdk.CreateChatCompletionRequest{
			MaxTokens:     &maxTokens,
			StreamOptions: &sdk.ChatCompletionStreamOptions{IncludeUsage: true},
		}).
		WithMiddlewareOptions(&sdk.MiddlewareOptions{
			SkipMCP: true,
		}).
		GenerateContentStream(ctx, sdk.Provider(provider), modelName, messages)
	if err != nil {
		return "", nil, fmt.Errorf("LLM API call failed: %w", err)
	}

	content, finish, usage, err := collect(events, progress)
	if err != nil {
		return "", nil, fmt.Errorf("LLM API call failed: %w", err)
	}

	logUsage(model, maxTokens, usage)

	if strings.TrimSpace(content) == "" {
		if finish == sdk.Length {
			return "", usage, fmt.Errorf("model spent all %d max_tokens before answering (reasoning models think against this budget): %w", maxTokens, ErrTokenBudgetExhausted)
		}
		return "", usage, fmt.Errorf("model returned an empty response")
	}

	return strings.TrimSpace(content), usage, nil
}

// collect folds a chat completion stream back into one answer, reporting the
// reasoning and answer seen so far on progress. An event with no event type
// carries the SDK's transport error.
func collect(events <-chan sdk.SSEvent, progress io.Writer) (string, sdk.FinishReason, *sdk.CompletionUsage, error) {
	var content strings.Builder
	var finish sdk.FinishReason
	var usage *sdk.CompletionUsage
	reasoning := 0
	started := time.Now()
	lastReport := started

	for event := range events {
		if event.Data == nil {
			continue
		}
		if event.Event == nil {
			return "", "", nil, errors.New(string(*event.Data))
		}
		var chunk sdk.CreateChatCompletionStreamResponse
		if err := json.Unmarshal(*event.Data, &chunk); err != nil {
			return "", "", nil, fmt.Errorf("decoding stream chunk: %w", err)
		}
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
		for _, choice := range chunk.Choices {
			content.WriteString(choice.Delta.Content)
			reasoning += len(reasoningOf(choice.Delta))
			if choice.FinishReason != "" {
				finish = choice.FinishReason
			}
		}
		if time.Since(lastReport) >= progressInterval {
			lastReport = time.Now()
			_, _ = fmt.Fprintf(progress, "\r  %s elapsed · %d chars of reasoning · %d chars of answer",
				time.Since(started).Round(time.Second), reasoning, content.Len())
		}
	}
	return content.String(), finish, usage, nil
}

func reasoningOf(delta sdk.ChatCompletionStreamResponseDelta) string {
	if delta.ReasoningContent != nil {
		return *delta.ReasoningContent
	}
	if delta.Reasoning != nil {
		return *delta.Reasoning
	}
	return ""
}
