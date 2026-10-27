package llm

import (
	"cmp"
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

// ErrEmptyResponse marks a model that finished without any text at all.
var ErrEmptyResponse = errors.New("model returned an empty response")

// progressInterval bounds how often a running call reports on progress.
const progressInterval = time.Second

// Request is one no-tools side call: an optional system prompt, one user
// prompt and a token budget. Progress, when set, receives a status line while
// the model works. The caller owns the deadline via ctx.
type Request struct {
	Model     string
	System    string
	Prompt    string
	MaxTokens int
	Progress  io.Writer
}

// Response is what the model answered, split the way the API splits it.
type Response struct {
	Content   string
	Reasoning string
	Usage     *sdk.CompletionUsage
}

// Answer is the trimmed content, or the reasoning when a reasoning model
// left its whole answer there and nothing in content.
func (r Response) Answer() string {
	return cmp.Or(strings.TrimSpace(r.Content), strings.TrimSpace(r.Reasoning))
}

// Completer answers one side call. The judge, insights and custom shortcuts
// consume it, so their tests script an answer without a stream.
type Completer interface {
	Complete(ctx context.Context, req Request) (Response, error)
}

// Client is the Completer over an SDK client.
type Client struct {
	sdk sdk.Client
}

func New(client sdk.Client) *Client {
	return &Client{sdk: client}
}

// Complete runs the request as a stream, so a reasoning model's first bytes
// arrive before the gateway's header timeout, and folds the chunks back into
// one Response. The SDK's reader goroutine exits with the context, so Complete
// owns a cancel for it.
func (c *Client) Complete(ctx context.Context, req Request) (Response, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if c == nil || c.sdk == nil {
		return Response{}, fmt.Errorf("SDK client not available")
	}
	provider, modelName, err := splitModel(req.Model)
	if err != nil {
		return Response{}, err
	}

	events, err := c.sdk.
		WithOptions(&sdk.CreateChatCompletionRequest{
			MaxTokens:     &req.MaxTokens,
			StreamOptions: &sdk.ChatCompletionStreamOptions{IncludeUsage: true},
		}).
		WithMiddlewareOptions(&sdk.MiddlewareOptions{SkipMCP: true}).
		GenerateContentStream(ctx, provider, modelName, req.messages())
	if err != nil {
		return Response{}, fmt.Errorf("LLM API call failed: %w", err)
	}

	resp, finish, err := collect(events, cmp.Or(req.Progress, io.Discard))
	if err != nil {
		return Response{}, fmt.Errorf("LLM API call failed: %w", err)
	}
	logUsage(req, resp.Usage)

	if resp.Answer() == "" {
		if finish == sdk.Length {
			return resp, fmt.Errorf("model spent all %d max_tokens before answering (reasoning models think against this budget): %w", req.MaxTokens, ErrTokenBudgetExhausted)
		}
		return resp, fmt.Errorf("%w (finish_reason=%q)", ErrEmptyResponse, finish)
	}
	return resp, nil
}

func splitModel(model string) (sdk.Provider, string, error) {
	if model == "" {
		return "", "", fmt.Errorf("no model configured (use /model to select a model)")
	}
	provider, name, ok := strings.Cut(model, "/")
	if !ok {
		return "", "", fmt.Errorf("invalid model format %q, expected 'provider/model'", model)
	}
	return sdk.Provider(provider), name, nil
}

func (r Request) messages() []sdk.Message {
	messages := make([]sdk.Message, 0, 2)
	if r.System != "" {
		messages = append(messages, sdk.Message{Role: sdk.System, Content: sdk.NewMessageContent(r.System)})
	}
	return append(messages, sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent(r.Prompt)})
}

// collect folds a chat completion stream into one Response, reporting the
// reasoning and answer seen so far on progress. An event with no event type
// carries the SDK's transport error.
func collect(events <-chan sdk.SSEvent, progress io.Writer) (Response, sdk.FinishReason, error) {
	var content, reasoning strings.Builder
	var finish sdk.FinishReason
	var usage *sdk.CompletionUsage
	started := time.Now()
	lastReport := started

	for event := range events {
		if event.Data == nil {
			continue
		}
		if event.Event == nil {
			return Response{}, "", errors.New(string(*event.Data))
		}
		var chunk sdk.CreateChatCompletionStreamResponse
		if err := json.Unmarshal(*event.Data, &chunk); err != nil {
			return Response{}, "", fmt.Errorf("decoding stream chunk: %w", err)
		}
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
		for _, choice := range chunk.Choices {
			content.WriteString(choice.Delta.Content)
			reasoning.WriteString(reasoningOf(choice.Delta))
			if choice.FinishReason != "" {
				finish = choice.FinishReason
			}
		}
		if time.Since(lastReport) >= progressInterval {
			lastReport = time.Now()
			_, _ = fmt.Fprintf(progress, "\r  %s elapsed · %d chars of reasoning · %d chars of answer",
				time.Since(started).Round(time.Second), reasoning.Len(), content.Len())
		}
	}
	return Response{Content: content.String(), Reasoning: reasoning.String(), Usage: usage}, finish, nil
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

// logUsage records what one side-call cost.
func logUsage(req Request, usage *sdk.CompletionUsage) {
	if usage == nil {
		return
	}
	var reasoning int64
	if usage.CompletionTokensDetails != nil && usage.CompletionTokensDetails.ReasoningTokens != nil {
		reasoning = *usage.CompletionTokensDetails.ReasoningTokens
	}
	logger.Debug("llm side-call usage",
		"model", req.Model,
		"max_tokens", req.MaxTokens,
		"prompt_tokens", usage.PromptTokens,
		"completion_tokens", usage.CompletionTokens,
		"reasoning_tokens", reasoning,
		"total_tokens", usage.TotalTokens)
}
