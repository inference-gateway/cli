package llm

import (
	"context"
	"errors"
	"strings"
	"testing"

	sdkmocks "github.com/inference-gateway/cli/tests/mocks/sdk"

	sdk "github.com/inference-gateway/sdk"
)

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
			client := &sdkmocks.FakeClient{}
			client.WithOptionsReturns(client)
			client.WithMiddlewareOptionsReturns(client)
			client.GenerateContentReturns(&sdk.CreateChatCompletionResponse{
				Choices: []sdk.ChatCompletionChoice{{
					FinishReason: tt.finish,
					Message:      sdk.Message{Content: sdk.NewMessageContent(tt.content)},
				}},
			}, nil)

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
	client := &sdkmocks.FakeClient{}
	client.WithOptionsReturns(client)
	client.WithMiddlewareOptionsReturns(client)
	client.GenerateContentReturns(&sdk.CreateChatCompletionResponse{
		Choices: []sdk.ChatCompletionChoice{{FinishReason: sdk.Length, Message: sdk.Message{Content: sdk.NewMessageContent("")}}},
	}, nil)

	_, _, err := Call(context.Background(), client, "openai/gpt-4o", "prompt", 4000)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, ErrTokenBudgetExhausted) {
		t.Errorf("budget failure must be identifiable with errors.Is, got: %v", err)
	}
}
