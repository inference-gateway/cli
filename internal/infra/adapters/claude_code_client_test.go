package adapters

import (
	"encoding/json"
	"testing"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
)

func TestClaudeUsageToCompletionUsage(t *testing.T) {
	u := &ClaudeUsage{
		InputTokens:              100,
		OutputTokens:             20,
		CacheCreationInputTokens: 30,
		CacheReadInputTokens:     50,
	}

	got := u.toCompletionUsage()

	if got.PromptTokens != 180 {
		t.Errorf("PromptTokens = %d, want 180", got.PromptTokens)
	}
	if got.CompletionTokens != 20 {
		t.Errorf("CompletionTokens = %d, want 20", got.CompletionTokens)
	}
	if got.TotalTokens != 200 {
		t.Errorf("TotalTokens = %d, want 200", got.TotalTokens)
	}
}

// chunkShape mirrors the JSON the adapter emits for a usage chunk.
type chunkShape struct {
	Choices []struct {
		Delta map[string]any `json:"delta"`
		Index int            `json:"index"`
	} `json:"choices"`
	Usage *sdk.CompletionUsage `json:"usage"`
}

func TestTransformMessageResultEmitsUsageChunk(t *testing.T) {
	c := &ClaudeCodeClient{}

	events := c.transformMessage(ClaudeCodeMessage{
		Type:  "result",
		Usage: &ClaudeUsage{InputTokens: 100, OutputTokens: 20, CacheCreationInputTokens: 30, CacheReadInputTokens: 50},
	})

	if len(events) != 2 {
		t.Fatalf("got %d events, want 2 (usage chunk + message_stop)", len(events))
	}

	if events[0].Data == nil {
		t.Fatal("usage chunk has nil data")
	}
	var chunk chunkShape
	if err := json.Unmarshal(*events[0].Data, &chunk); err != nil {
		t.Fatalf("unmarshal usage chunk: %v", err)
	}
	if chunk.Usage == nil {
		t.Fatal("usage chunk is missing the usage object")
	}
	if chunk.Usage.PromptTokens != 180 || chunk.Usage.CompletionTokens != 20 || chunk.Usage.TotalTokens != 200 {
		t.Errorf("usage = %+v, want prompt=180 completion=20 total=200", chunk.Usage)
	}
	if len(chunk.Choices) != 1 || len(chunk.Choices[0].Delta) != 0 {
		t.Errorf("want exactly one empty-delta choice, got %+v", chunk.Choices)
	}

	if events[1].Event == nil || string(*events[1].Event) != "message_stop" {
		t.Errorf("second event = %v, want message_stop", events[1].Event)
	}
	if events[1].Data == nil || string(*events[1].Data) != "done" {
		t.Errorf("second event data = %v, want done", events[1].Data)
	}
}

func TestTransformMessageResultNilUsageEmitsZeroUsage(t *testing.T) {
	c := &ClaudeCodeClient{}

	events := c.transformMessage(ClaudeCodeMessage{Type: "result"})
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}

	var chunk chunkShape
	if err := json.Unmarshal(*events[0].Data, &chunk); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if chunk.Usage == nil {
		t.Fatal("want a zero usage object, got nil")
	}
	if chunk.Usage.PromptTokens != 0 || chunk.Usage.CompletionTokens != 0 || chunk.Usage.TotalTokens != 0 {
		t.Errorf("want zero usage, got %+v", chunk.Usage)
	}
}

func TestProcessEventsCapturesUsage(t *testing.T) {
	c := &ClaudeCodeClient{}

	ch := make(chan sdk.SSEvent, 8)
	for _, ev := range c.transformMessage(ClaudeCodeMessage{
		Type:    "assistant",
		Message: json.RawMessage(`{"role":"assistant","content":[{"type":"text","text":"hello"}]}`),
	}) {
		ch <- ev
	}
	for _, ev := range c.transformMessage(ClaudeCodeMessage{
		Type:  "result",
		Usage: &ClaudeUsage{InputTokens: 10, OutputTokens: 5},
	}) {
		ch <- ev
	}
	close(ch)

	content, _, usage, err := c.processEvents(ch)
	if err != nil {
		t.Fatalf("processEvents: %v", err)
	}
	if content != "hello" {
		t.Errorf("content = %q, want hello (empty-delta usage chunk must not corrupt content)", content)
	}
	if usage == nil {
		t.Fatal("usage is nil")
	}
	if usage.PromptTokens != 10 || usage.CompletionTokens != 5 || usage.TotalTokens != 15 {
		t.Errorf("usage = %+v, want prompt=10 completion=5 total=15", usage)
	}
}

func TestBuildArgsStripsProviderPrefix(t *testing.T) {
	c := &ClaudeCodeClient{config: &config.ClaudeCodeConfig{MaxTurns: 10}}

	if got := modelArg(c.buildArgs("anthropic/claude-sonnet-4-5-20250929")); got != "claude-sonnet-4-5-20250929" {
		t.Errorf("--model = %q, want bare claude-sonnet-4-5-20250929", got)
	}
	if got := modelArg(c.buildArgs("claude-opus-4-5")); got != "claude-opus-4-5" {
		t.Errorf("--model = %q, want claude-opus-4-5 (bare passes through)", got)
	}
}

func modelArg(args []string) string {
	for i, a := range args {
		if a == "--model" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}
