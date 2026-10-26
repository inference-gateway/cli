package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	sdkmocks "github.com/inference-gateway/cli/tests/mocks/sdk"

	sdk "github.com/inference-gateway/sdk"
)

// streamOf serialises chunks the way the SDK's SSE reader hands them out.
func streamOf(chunks ...sdk.CreateChatCompletionStreamResponse) <-chan sdk.SSEvent {
	events := make(chan sdk.SSEvent, len(chunks)+1)
	for _, chunk := range chunks {
		data, _ := json.Marshal(chunk)
		delta := sdk.ContentDelta
		events <- sdk.SSEvent{Event: &delta, Data: &data}
	}
	end := sdk.StreamEnd
	events <- sdk.SSEvent{Event: &end}
	close(events)
	return events
}

func chunk(content string, finish sdk.FinishReason) sdk.CreateChatCompletionStreamResponse {
	return sdk.CreateChatCompletionStreamResponse{
		Choices: []sdk.ChatCompletionStreamChoice{{
			FinishReason: finish,
			Delta:        sdk.ChatCompletionStreamResponseDelta{Content: content},
		}},
	}
}

func fakeClient(events <-chan sdk.SSEvent) *sdkmocks.FakeClient {
	client := &sdkmocks.FakeClient{}
	client.WithOptionsReturns(client)
	client.WithMiddlewareOptionsReturns(client)
	client.GenerateContentStreamReturns(events, nil)
	return client
}

// TestCallRejectsEmptyResponse covers the bug behind an empty "## Analysis":
// a reasoning model spends max_tokens thinking and returns no content, which
// Call used to hand back as a successful empty string.
func TestCallRejectsEmptyResponse(t *testing.T) {
	tests := []struct {
		name    string
		finish  sdk.FinishReason
		content string
		wantErr string
	}{
		{"budget exhausted", sdk.Length, "", "max_tokens"},
		{"empty for another reason", sdk.Stop, "  \n ", "empty response"},
		{"real content passes", sdk.Stop, " analysis ", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fakeClient(streamOf(chunk(tt.content, tt.finish)))

			got, _, err := Call(context.Background(), client, "openai/gpt-4o", "prompt", 4000)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got != "analysis" {
					t.Errorf("expected trimmed content, got %q", got)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected an error naming %q, got content %q", tt.wantErr, got)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error must mention %q, got: %v", tt.wantErr, err)
			}
		})
	}
}

// TestCallMarksBudgetExhausted keeps the sentinel error addressable through
// errors.Is for callers that raise a config knob in response.
func TestCallMarksBudgetExhausted(t *testing.T) {
	client := fakeClient(streamOf(chunk("", sdk.Length)))

	_, _, err := Call(context.Background(), client, "openai/gpt-4o", "prompt", 4000)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, ErrTokenBudgetExhausted) {
		t.Errorf("budget failure must be identifiable with errors.Is, got: %v", err)
	}
}

// TestStreamJoinsDeltasAndKeepsUsage is the streaming contract: content split
// across chunks is joined, the trailing usage-only chunk is kept, and the
// request asks for usage.
func TestStreamJoinsDeltasAndKeepsUsage(t *testing.T) {
	usage := sdk.CreateChatCompletionStreamResponse{Usage: &sdk.CompletionUsage{
		PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30,
	}}
	client := fakeClient(streamOf(chunk("ana", ""), chunk("lysis", sdk.Stop), usage))

	got, gotUsage, err := Stream(context.Background(), client, "openai/gpt-4o", "prompt", 4000, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if got != "analysis" {
		t.Errorf("deltas not joined: %q", got)
	}
	if gotUsage == nil || gotUsage.TotalTokens != 30 {
		t.Errorf("usage chunk lost: %+v", gotUsage)
	}
	opts := client.WithOptionsArgsForCall(0)
	if opts.StreamOptions == nil || !opts.StreamOptions.IncludeUsage {
		t.Error("request must ask for usage in the stream")
	}
}

// TestCallSurfacesTransportError covers a connection dropped mid-stream, which
// the SDK reports as an event without a type.
func TestCallSurfacesTransportError(t *testing.T) {
	events := make(chan sdk.SSEvent, 1)
	data := []byte(`{"error": "unexpected EOF"}`)
	events <- sdk.SSEvent{Data: &data}
	close(events)

	_, _, err := Call(context.Background(), fakeClient(events), "openai/gpt-4o", "prompt", 4000)
	if err == nil || !strings.Contains(err.Error(), "unexpected EOF") {
		t.Errorf("transport error must reach the caller, got: %v", err)
	}
}
