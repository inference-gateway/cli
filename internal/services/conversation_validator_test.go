package services_test

import (
	"testing"

	services "github.com/inference-gateway/cli/internal/services"
	sdk "github.com/inference-gateway/sdk"
)

func assistantWithToolCalls(ids ...string) sdk.Message {
	calls := make([]sdk.ChatCompletionMessageToolCall, 0, len(ids))
	for _, id := range ids {
		calls = append(calls, sdk.ChatCompletionMessageToolCall{
			ID:   id,
			Type: sdk.Function,
			Function: sdk.ChatCompletionMessageToolCallFunction{
				Name:      "Tool_" + id,
				Arguments: "{}",
			},
		})
	}
	return sdk.Message{
		Role:      sdk.Assistant,
		Content:   sdk.NewMessageContent("calling tools"),
		ToolCalls: &calls,
	}
}

func toolResponse(id, content string) sdk.Message {
	idCopy := id
	return sdk.Message{
		Role:       sdk.Tool,
		Content:    sdk.NewMessageContent(content),
		ToolCallID: &idCopy,
	}
}

func userMsg(content string) sdk.Message {
	return sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent(content)}
}

func checkSynthetics(t *testing.T, synthetics []services.SyntheticToolResponse, wantSynthCount int, wantSynthIDs []string) {
	t.Helper()
	if wantSynthCount > 0 {
		if len(synthetics) != wantSynthCount {
			t.Fatalf("expected %d synthetics, got %d", wantSynthCount, len(synthetics))
		}
		return
	}
	if wantSynthIDs != nil {
		if len(synthetics) != len(wantSynthIDs) {
			t.Fatalf("expected %d synthetics, got %d", len(wantSynthIDs), len(synthetics))
		}
		for i, id := range wantSynthIDs {
			if synthetics[i].ToolCallID != id {
				t.Errorf("synthetics[%d].ToolCallID = %s, want %s", i, synthetics[i].ToolCallID, id)
			}
		}
		return
	}
	if len(synthetics) != 0 {
		t.Errorf("expected no synthetics, got %d", len(synthetics))
	}
}

//nolint:gocyclo,cyclop
func TestEnsureToolCallsClosed(t *testing.T) {
	tests := []struct {
		name           string
		conv           []sdk.Message
		wantSynthIDs   []string
		wantSynthCount int
		wantLen        int
		check          func(t *testing.T, repaired []sdk.Message, synthetics []services.SyntheticToolResponse)
	}{
		{
			name:           "empty conversation",
			conv:           nil,
			wantSynthCount: 0,
			wantLen:        0,
		},
		{
			name: "no tool calls",
			conv: []sdk.Message{
				userMsg("hi"),
				{Role: sdk.Assistant, Content: sdk.NewMessageContent("hello")},
				userMsg("bye"),
			},
			wantSynthCount: 0,
			wantLen:        3,
		},
		{
			name: "all tool calls resolved",
			conv: []sdk.Message{
				userMsg("read files"),
				assistantWithToolCalls("a", "b", "c"),
				toolResponse("a", "result-a"),
				toolResponse("b", "result-b"),
				toolResponse("c", "result-c"),
				{Role: sdk.Assistant, Content: sdk.NewMessageContent("done")},
			},
			wantSynthCount: 0,
			wantLen:        6,
		},
		{
			name: "one partial resolved",
			conv: []sdk.Message{
				userMsg("read 3 files"),
				assistantWithToolCalls("a", "b", "c"),
				toolResponse("b", "result-b"),
				userMsg("hi"),
			},
			wantSynthIDs: []string{"a", "c"},
			wantLen:      6,
			check: func(t *testing.T, repaired []sdk.Message, synthetics []services.SyntheticToolResponse) {
				if repaired[2].Role != sdk.Tool || *repaired[2].ToolCallID != "b" {
					t.Errorf("expected real response b at index 2, got role=%s id=%v", repaired[2].Role, repaired[2].ToolCallID)
				}
				if repaired[3].Role != sdk.Tool || *repaired[3].ToolCallID != "a" {
					t.Errorf("expected synthetic a at index 3, got role=%s id=%v", repaired[3].Role, repaired[3].ToolCallID)
				}
				if repaired[4].Role != sdk.Tool || *repaired[4].ToolCallID != "c" {
					t.Errorf("expected synthetic c at index 4, got role=%s id=%v", repaired[4].Role, repaired[4].ToolCallID)
				}
				if repaired[5].Role != sdk.User {
					t.Errorf("expected user msg at index 5, got role=%s", repaired[5].Role)
				}
			},
		},
		{
			name: "none resolved",
			conv: []sdk.Message{
				userMsg("read files"),
				assistantWithToolCalls("a", "b", "c"),
				userMsg("hi"),
			},
			wantSynthIDs: []string{"a", "b", "c"},
			wantLen:      6,
			check: func(t *testing.T, repaired []sdk.Message, synthetics []services.SyntheticToolResponse) {
				if repaired[5].Role != sdk.User {
					t.Errorf("user message should land after synthetics, got role=%s at index 5", repaired[5].Role)
				}
			},
		},
		{
			name: "idempotent",
			conv: []sdk.Message{
				userMsg("read files"),
				assistantWithToolCalls("a", "b"),
				userMsg("hi"),
			},
			wantSynthCount: 2,
			check: func(t *testing.T, repaired []sdk.Message, synthetics []services.SyntheticToolResponse) {
				twice, synth2 := services.EnsureToolCallsClosed(repaired)
				if len(synth2) != 0 {
					t.Errorf("second pass expected 0 synthetics, got %d", len(synth2))
				}
				if len(twice) != len(repaired) {
					t.Errorf("second pass changed conversation length: %d -> %d", len(repaired), len(twice))
				}
			},
		},
		{
			name: "multiple assistant messages",
			conv: []sdk.Message{
				userMsg("first"),
				assistantWithToolCalls("a1"),
				userMsg("second"),
				assistantWithToolCalls("b1", "b2"),
				toolResponse("b2", "ok"),
				userMsg("third"),
			},
			wantSynthIDs: []string{"a1", "b1"},
			check: func(t *testing.T, repaired []sdk.Message, synthetics []services.SyntheticToolResponse) {
				if repaired[2].Role != sdk.Tool || *repaired[2].ToolCallID != "a1" {
					t.Errorf("synthetic for a1 should land at index 2, got role=%s id=%v",
						repaired[2].Role, repaired[2].ToolCallID)
				}
				if repaired[3].Role != sdk.User {
					t.Errorf("'second' user msg should land at index 3, got role=%s", repaired[3].Role)
				}
			},
		},
		{
			name: "reasoning preserved",
			conv: func() []sdk.Message {
				reasoning := "I should read these files"
				asst := assistantWithToolCalls("a", "b")
				asst.Reasoning = &reasoning
				asst.ReasoningContent = &reasoning
				return []sdk.Message{userMsg("read"), asst, userMsg("hi")}
			}(),
			wantSynthCount: 2,
			check: func(t *testing.T, repaired []sdk.Message, synthetics []services.SyntheticToolResponse) {
				reasoning := "I should read these files"
				if repaired[1].Reasoning == nil || *repaired[1].Reasoning != reasoning {
					t.Errorf("Reasoning lost: got %v", repaired[1].Reasoning)
				}
				if repaired[1].ReasoningContent == nil || *repaired[1].ReasoningContent != reasoning {
					t.Errorf("ReasoningContent lost: got %v", repaired[1].ReasoningContent)
				}
				if repaired[1].ToolCalls == nil || len(*repaired[1].ToolCalls) != 2 {
					t.Errorf("ToolCalls altered on assistant message")
				}
			},
		},
		{
			name: "nil tool call ID ignored",
			conv: []sdk.Message{
				userMsg("read"),
				assistantWithToolCalls("a"),
				{Role: sdk.Tool, Content: sdk.NewMessageContent("orphan-tool"), ToolCallID: nil},
				userMsg("hi"),
			},
			wantSynthIDs: []string{"a"},
		},
		{
			name: "preserves tool name for UI",
			conv: func() []sdk.Message {
				calls := []sdk.ChatCompletionMessageToolCall{
					{
						ID:   "tc1",
						Type: sdk.Function,
						Function: sdk.ChatCompletionMessageToolCallFunction{
							Name:      "Read",
							Arguments: `{"file_path":"/x"}`,
						},
					},
				}
				return []sdk.Message{
					userMsg("read x"),
					{Role: sdk.Assistant, Content: sdk.NewMessageContent(""), ToolCalls: &calls},
					userMsg("hi"),
				}
			}(),
			wantSynthCount: 1,
			check: func(t *testing.T, repaired []sdk.Message, synthetics []services.SyntheticToolResponse) {
				if len(synthetics) != 1 || synthetics[0].ToolName != "Read" {
					t.Errorf("expected synthetic with ToolName=Read, got %+v", synthetics)
				}
			},
		},
		{
			name: "empty ID skipped",
			conv: func() []sdk.Message {
				calls := []sdk.ChatCompletionMessageToolCall{
					{ID: "", Type: sdk.Function, Function: sdk.ChatCompletionMessageToolCallFunction{Name: "X"}},
					{ID: "good", Type: sdk.Function, Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Y"}},
				}
				return []sdk.Message{
					userMsg("u"),
					{Role: sdk.Assistant, Content: sdk.NewMessageContent(""), ToolCalls: &calls},
					userMsg("v"),
				}
			}(),
			wantSynthIDs: []string{"good"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repaired, synthetics := services.EnsureToolCallsClosed(tt.conv)

			checkSynthetics(t, synthetics, tt.wantSynthCount, tt.wantSynthIDs)

			if tt.wantLen > 0 && len(repaired) != tt.wantLen {
				t.Errorf("expected len %d, got %d", tt.wantLen, len(repaired))
			}

			if tt.check != nil {
				tt.check(t, repaired, synthetics)
			}
		})
	}
}
