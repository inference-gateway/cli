package conversation

import (
	"strings"
	"testing"

	sdk "github.com/inference-gateway/sdk"

	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
)

func TestBuildAgentMessagesFromEntries_FiltersPlanEntries(t *testing.T) {
	planContent := "## Context\nDo X."
	reasoning := "thought process"
	planTitle := "Add Feature X"

	entries := []convdomain.ConversationEntry{
		{
			Message: sdk.Message{
				Role:    sdk.User,
				Content: sdk.NewMessageContent("Please plan it"),
			},
		},
		{
			Message: sdk.Message{
				Role:    sdk.Assistant,
				Content: sdk.NewMessageContent("Submitting plan"),
				ToolCalls: &[]sdk.ChatCompletionMessageToolCall{
					{
						ID:   "call_1",
						Type: sdk.Function,
						Function: sdk.ChatCompletionMessageToolCallFunction{
							Name:      "RequestPlanApproval",
							Arguments: `{"title":"` + planTitle + `","plan":"` + planContent + `"}`,
						},
					},
				},
				Reasoning:        &reasoning,
				ReasoningContent: &reasoning,
			},
			ReasoningContent: reasoning,
		},
		{
			Message: sdk.Message{
				Role:       sdk.Tool,
				Content:    sdk.NewMessageContent("Plan approval requested. Plan saved to ..."),
				ToolCallID: new("call_1"),
			},
		},
		{
			Message: sdk.Message{
				Role:    sdk.Assistant,
				Content: sdk.NewMessageContent(planContent),
			},
			IsPlan:             true,
			PlanApprovalStatus: convdomain.PlanApprovalAccepted,
		},
		{
			Message: sdk.Message{
				Role:    sdk.User,
				Content: sdk.NewMessageContent("The plan has been approved."),
			},
			Hidden: true,
		},
	}

	out := BuildAgentMessagesFromEntries(entries)

	if len(out) != 4 {
		t.Fatalf("expected 4 messages after filtering plan entry, got %d", len(out))
	}

	if out[1].Role != sdk.Assistant {
		t.Errorf("expected message[1] to be the assistant tool-call turn, got role %s", out[1].Role)
	}
	if out[1].ReasoningContent == nil || *out[1].ReasoningContent != reasoning {
		t.Errorf("expected reasoning_content preserved on assistant tool-call turn, got %v", out[1].ReasoningContent)
	}

	for i, msg := range out {
		if msg.Role == sdk.Assistant && msg.ToolCalls == nil {
			content, _ := msg.Content.AsMessageContent0()
			if strings.Contains(content, "## Context") {
				t.Errorf("plan-mode synthesized assistant message leaked into request at index %d", i)
			}
		}
	}
}

// TestBuildAgentMessagesFromEntries_FiltersPendingToolCallEntries is a
// regression test for issue #786: the UI-only pending-approval placeholder
// (empty assistant entry) left behind by a rejected tool must not be
// serialized between the assistant tool_calls message and its tool response.
func TestBuildAgentMessagesFromEntries_FiltersPendingToolCallEntries(t *testing.T) {
	entries := []convdomain.ConversationEntry{
		{Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("edit the file")}},
		{
			Message: sdk.Message{
				Role:    sdk.Assistant,
				Content: sdk.NewMessageContent(""),
				ToolCalls: &[]sdk.ChatCompletionMessageToolCall{
					{
						ID:   "call_1",
						Type: sdk.Function,
						Function: sdk.ChatCompletionMessageToolCallFunction{
							Name:      "Edit",
							Arguments: `{}`,
						},
					},
				},
			},
		},
		{
			Message: sdk.Message{
				Role:    sdk.Assistant,
				Content: sdk.NewMessageContent(""),
			},
			PendingToolCall:    &sdk.ChatCompletionMessageToolCall{ID: "call_1"},
			ToolApprovalStatus: convdomain.ToolApprovalRejected,
		},
		{
			Message: sdk.Message{
				Role:       sdk.Tool,
				Content:    sdk.NewMessageContent("Tool execution rejected by user: Edit"),
				ToolCallID: new("call_1"),
			},
		},
	}

	out := BuildAgentMessagesFromEntries(entries)

	if len(out) != 3 {
		t.Fatalf("expected 3 messages after filtering placeholder, got %d", len(out))
	}
	if out[1].ToolCalls == nil {
		t.Fatalf("expected message[1] to carry tool_calls")
	}
	if out[2].Role != sdk.Tool {
		t.Fatalf("expected tool response directly after tool_calls message, got role %s", out[2].Role)
	}
}

func TestBuildAgentMessagesFromEntries_PreservesNonPlanEntries(t *testing.T) {
	entries := []convdomain.ConversationEntry{
		{Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("hi")}},
		{Message: sdk.Message{Role: sdk.Assistant, Content: sdk.NewMessageContent("hello")}},
	}
	out := BuildAgentMessagesFromEntries(entries)
	if len(out) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(out))
	}
}

// Regression for issue #474: when finalizeStream stored an assistant entry
// without populating Message.Reasoning (the pre-fix behavior for non-tool-call
// assistant turns), the rebuilt request would lack reasoning_content and
// thinking-mode providers (e.g. Deepseek) would 400. The helper now backfills
// Message.Reasoning/ReasoningContent from the entry's top-level
// ReasoningContent so legacy entries and any future writers stay safe.
func TestBuildAgentMessagesFromEntries_BackfillsReasoningFromEntry(t *testing.T) {
	reasoning := "I should retry with a different path."

	entries := []convdomain.ConversationEntry{
		{
			Message: sdk.Message{
				Role:    sdk.Assistant,
				Content: sdk.NewMessageContent("Let me try a different path."),
			},
			ReasoningContent: reasoning,
		},
	}

	out := BuildAgentMessagesFromEntries(entries)

	if len(out) != 1 {
		t.Fatalf("expected 1 message, got %d", len(out))
	}
	if out[0].Reasoning == nil || *out[0].Reasoning != reasoning {
		t.Errorf("expected Reasoning backfilled to %q, got %v", reasoning, out[0].Reasoning)
	}
	if out[0].ReasoningContent == nil || *out[0].ReasoningContent != reasoning {
		t.Errorf("expected ReasoningContent backfilled to %q, got %v", reasoning, out[0].ReasoningContent)
	}
}

// Regression for the second flavor of issue #474: user-typed `!command`
// shortcuts synthesize an assistant entry (with tool_calls but no
// reasoning_content) followed by a tool result. Tool-call IDs are prefixed
// with `user-bash-`. Previously these were sent verbatim to the model and
// rejected by thinking-mode providers (DeepSeek 400) on the next turn. Both
// the assistant and the matching tool entry must be filtered.
func TestBuildAgentMessagesFromEntries_FiltersUserBashEntries(t *testing.T) {
	userBashID := "user-bash-1234567890"

	entries := []convdomain.ConversationEntry{
		{Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("!task lint")}},
		{
			Message: sdk.Message{
				Role:    sdk.Assistant,
				Content: sdk.NewMessageContent(""),
				ToolCalls: &[]sdk.ChatCompletionMessageToolCall{
					{
						ID:   userBashID,
						Type: sdk.Function,
						Function: sdk.ChatCompletionMessageToolCallFunction{
							Name:      "Bash",
							Arguments: `{"command":"task lint"}`,
						},
					},
				},
			},
		},
		{
			Message: sdk.Message{
				Role:       sdk.Tool,
				Content:    sdk.NewMessageContent("0 issues."),
				ToolCallID: &userBashID,
			},
		},
		{Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("anything else?")}},
	}

	out := BuildAgentMessagesFromEntries(entries)

	if len(out) != 2 {
		t.Fatalf("expected 2 messages after filtering user-bash pair, got %d", len(out))
	}
	for i, msg := range out {
		if msg.ToolCalls != nil {
			for _, tc := range *msg.ToolCalls {
				if strings.HasPrefix(tc.ID, "user-bash-") {
					t.Errorf("user-bash tool call leaked into request at message %d", i)
				}
			}
		}
		if msg.ToolCallID != nil && strings.HasPrefix(*msg.ToolCallID, "user-bash-") {
			t.Errorf("user-bash tool result leaked into request at message %d", i)
		}
	}
}

func TestBuildAgentMessagesFromEntries_DoesNotOverwriteExistingReasoning(t *testing.T) {
	existing := "from message"
	other := "from entry"

	entries := []convdomain.ConversationEntry{
		{
			Message: sdk.Message{
				Role:             sdk.Assistant,
				Content:          sdk.NewMessageContent("hello"),
				Reasoning:        &existing,
				ReasoningContent: &existing,
			},
			ReasoningContent: other,
		},
	}

	out := BuildAgentMessagesFromEntries(entries)

	if len(out) != 1 {
		t.Fatalf("expected 1 message, got %d", len(out))
	}
	if out[0].Reasoning == nil || *out[0].Reasoning != existing {
		t.Errorf("expected Reasoning preserved as %q, got %v", existing, out[0].Reasoning)
	}
	if out[0].ReasoningContent == nil || *out[0].ReasoningContent != existing {
		t.Errorf("expected ReasoningContent preserved as %q, got %v", existing, out[0].ReasoningContent)
	}
}
