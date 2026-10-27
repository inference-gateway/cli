package llm

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	sdkmocks "github.com/inference-gateway/cli/tests/mocks/sdk"

	sdk "github.com/inference-gateway/sdk"
)

type chunk = sdk.CreateChatCompletionStreamResponse

func content(text string, finish sdk.FinishReason) chunk {
	return chunk{Choices: []sdk.ChatCompletionStreamChoice{{
		FinishReason: finish,
		Delta:        sdk.ChatCompletionStreamResponseDelta{Content: text},
	}}}
}

func reasoning(text string) chunk {
	return chunk{Choices: []sdk.ChatCompletionStreamChoice{{
		Delta: sdk.ChatCompletionStreamResponseDelta{ReasoningContent: &text},
	}}}
}

// events serialises chunks the way the SDK's SSE reader hands them out.
func events(chunks ...chunk) <-chan sdk.SSEvent {
	out := make(chan sdk.SSEvent, len(chunks)+1)
	for _, c := range chunks {
		data, _ := json.Marshal(c)
		delta := sdk.ContentDelta
		out <- sdk.SSEvent{Event: &delta, Data: &data}
	}
	end := sdk.StreamEnd
	out <- sdk.SSEvent{Event: &end}
	close(out)
	return out
}

func fakeClient(stream <-chan sdk.SSEvent) *sdkmocks.FakeClient {
	client := &sdkmocks.FakeClient{}
	client.WithOptionsReturns(client)
	client.WithMiddlewareOptionsReturns(client)
	client.GenerateContentStreamReturns(stream, nil)
	return client
}

func request(prompt string) Request {
	return Request{Model: "openai/gpt-4o", Prompt: prompt, MaxTokens: 4000}
}

// TestCompleteRejectsEmptyResponse covers the bug behind an empty
// "## Analysis": a reasoning model spends max_tokens thinking and returns no
// content, which used to come back as a successful empty string.
func TestCompleteRejectsEmptyResponse(t *testing.T) {
	tests := []struct {
		name    string
		finish  sdk.FinishReason
		content string
		wantErr error
	}{
		{"budget exhausted", sdk.Length, "", ErrTokenBudgetExhausted},
		{"empty for another reason", sdk.Stop, "  \n ", ErrEmptyResponse},
		{"real content passes", sdk.Stop, " analysis ", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fakeClient(events(content(tt.content, tt.finish)))

			resp, err := New(client).Complete(context.Background(), request("prompt"))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Complete() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && resp.Answer() != "analysis" {
				t.Errorf("Answer() = %q, want trimmed content", resp.Answer())
			}
		})
	}
}

// TestCompleteJoinsDeltasAndKeepsUsage is the streaming contract: content
// split across chunks is joined, reasoning is kept apart, the trailing
// usage-only chunk is kept, and the request asks for usage.
func TestCompleteJoinsDeltasAndKeepsUsage(t *testing.T) {
	client := fakeClient(events(
		reasoning("thinking"),
		content("ana", ""),
		content("lysis", sdk.Stop),
		chunk{Usage: &sdk.CompletionUsage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30}},
	))

	resp, err := New(client).Complete(context.Background(), request("prompt"))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "analysis" || resp.Reasoning != "thinking" {
		t.Errorf("deltas not folded: %+v", resp)
	}
	if resp.Usage == nil || resp.Usage.TotalTokens != 30 {
		t.Errorf("usage chunk lost: %+v", resp.Usage)
	}
	opts := client.WithOptionsArgsForCall(0)
	if opts.StreamOptions == nil || !opts.StreamOptions.IncludeUsage {
		t.Error("request must ask for usage in the stream")
	}
}

// TestAnswerFallsBackToReasoning covers a reasoning model that leaves the
// whole answer in reasoning_content and nothing in content.
func TestAnswerFallsBackToReasoning(t *testing.T) {
	client := fakeClient(events(reasoning(`{"decision": "approved"}`), content("", sdk.Stop)))

	resp, err := New(client).Complete(context.Background(), request("prompt"))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Answer() != `{"decision": "approved"}` {
		t.Errorf("Answer() = %q, want the reasoning text", resp.Answer())
	}
}

// TestCompleteSendsSystemPrompt keeps the optional system message first.
func TestCompleteSendsSystemPrompt(t *testing.T) {
	client := fakeClient(events(content("ok", sdk.Stop)))
	req := request("prompt")
	req.System = "rules"

	if _, err := New(client).Complete(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	_, _, _, messages := client.GenerateContentStreamArgsForCall(0)
	if len(messages) != 2 || messages[0].Role != sdk.System || messages[1].Role != sdk.User {
		t.Errorf("messages = %+v, want system then user", messages)
	}
}

// TestCompleteSurfacesTransportError covers a connection dropped mid-stream.
func TestCompleteSurfacesTransportError(t *testing.T) {
	dropped := make(chan sdk.SSEvent, 1)
	data := []byte(`{"error": "unexpected EOF"}`)
	dropped <- sdk.SSEvent{Data: &data}
	close(dropped)

	_, err := New(fakeClient(dropped)).Complete(context.Background(), request("prompt"))
	if err == nil || !strings.Contains(err.Error(), "unexpected EOF") {
		t.Errorf("transport error must reach the caller, got: %v", err)
	}
}

// TestCompleteRejectsBadModel keeps the provider/model contract.
func TestCompleteRejectsBadModel(t *testing.T) {
	for _, model := range []string{"", "no-slash"} {
		_, err := New(&sdkmocks.FakeClient{}).Complete(context.Background(), Request{Model: model, Prompt: "p"})
		if err == nil {
			t.Errorf("model %q must be rejected", model)
		}
	}
}
