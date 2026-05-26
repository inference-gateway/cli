package chatcompletion

import (
	"strings"
	"testing"
	"time"

	sdk "github.com/inference-gateway/sdk"

	domain "github.com/inference-gateway/cli/internal/domain"
	services "github.com/inference-gateway/cli/internal/services"
	mocksdomain "github.com/inference-gateway/cli/tests/mocks/domain"
)

// newRunnerForTest wires a Runner with the in-memory conversation repository
// and counterfeiter fakes for everything else.
func newRunnerForTest() (*Runner, *services.InMemoryConversationRepository, *mocksdomain.FakeStateManager, *mocksdomain.FakeAgentService, *mocksdomain.FakeModelService) {
	repo := services.NewInMemoryConversationRepository(nil, nil)
	state := &mocksdomain.FakeStateManager{}
	agent := &mocksdomain.FakeAgentService{}
	model := &mocksdomain.FakeModelService{}
	listener := &mocksdomain.FakeChatEventListener{}

	runner := NewRunner(Options{
		AgentService:     agent,
		ConversationRepo: repo,
		ModelService:     model,
		StateManager:     state,
		Listener:         listener,
	})
	return runner, repo, state, agent, model
}

// The synthesized plan-mode assistant entry duplicates the args of the
// preceding RequestPlanApproval tool call and lacks reasoning_content.
// Sending it on the next turn breaks DeepSeek's thinking-mode contract
// ("The reasoning_content in the thinking mode must be passed back to
// the API.") with HTTP 400. The helper below filters those entries out.
func TestBuildAgentMessagesFromEntries_FiltersPlanEntries(t *testing.T) {
	planContent := "## Context\nDo X."
	reasoning := "thought process"
	planTitle := "Add Feature X"

	entries := []domain.ConversationEntry{
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
			PlanApprovalStatus: domain.PlanApprovalAccepted,
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

func TestBuildAgentMessagesFromEntries_PreservesNonPlanEntries(t *testing.T) {
	entries := []domain.ConversationEntry{
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

	entries := []domain.ConversationEntry{
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

	entries := []domain.ConversationEntry{
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

	entries := []domain.ConversationEntry{
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

func TestRunner_Start(t *testing.T) {
	t.Run("returns ChatErrorEvent when no model is selected", func(t *testing.T) {
		runner, _, _, _, model := newRunnerForTest()
		model.GetCurrentModelReturns("")

		cmd := runner.Start(nil)
		if cmd == nil {
			t.Fatalf("expected non-nil cmd")
		}
		msg := cmd()
		errEvt, ok := msg.(domain.ChatErrorEvent)
		if !ok {
			t.Fatalf("expected ChatErrorEvent, got %T", msg)
		}
		if errEvt.Error == nil || !strings.Contains(errEvt.Error.Error(), "no model selected") {
			t.Errorf("expected 'no model selected' error, got %v", errEvt.Error)
		}
	})
}

func TestRunner_HandleChatComplete(t *testing.T) {
	t.Run("non-cancelled, no tool calls: updates status to Completed and returns non-nil cmd", func(t *testing.T) {
		runner, _, state, _, _ := newRunnerForTest()
		state.GetChatSessionReturns(nil)

		cmd := runner.HandleChatComplete(domain.ChatCompleteEvent{
			RequestID: "req-1",
			Timestamp: time.Now(),
		})

		if cmd == nil {
			t.Fatalf("expected non-nil cmd")
		}
		if state.UpdateChatStatusCallCount() != 1 {
			t.Errorf("expected UpdateChatStatus once, got %d", state.UpdateChatStatusCallCount())
		}
		if status := state.UpdateChatStatusArgsForCall(0); status != domain.ChatStatusCompleted {
			t.Errorf("expected ChatStatusCompleted, got %v", status)
		}
	})

	t.Run("cancelled: tears down session and updates status to Cancelled", func(t *testing.T) {
		runner, _, state, _, _ := newRunnerForTest()
		state.GetChatSessionReturns(nil)

		cmd := runner.HandleChatComplete(domain.ChatCompleteEvent{
			RequestID: "req-1",
			Cancelled: true,
		})

		if cmd == nil {
			t.Fatalf("expected non-nil cmd")
		}
		if state.UpdateChatStatusArgsForCall(0) != domain.ChatStatusCancelled {
			t.Errorf("expected ChatStatusCancelled")
		}
		if state.EndChatSessionCallCount() != 1 {
			t.Errorf("expected EndChatSession once")
		}
		if state.EndToolExecutionCallCount() != 1 {
			t.Errorf("expected EndToolExecution once")
		}
	})

	t.Run("with tool calls: does not update status to Completed (waits for tool round-trip)", func(t *testing.T) {
		runner, _, state, _, _ := newRunnerForTest()
		state.GetChatSessionReturns(nil)

		_ = runner.HandleChatComplete(domain.ChatCompleteEvent{
			RequestID: "req-1",
			ToolCalls: []sdk.ChatCompletionMessageToolCall{{
				ID:   "tc-1",
				Type: sdk.Function,
				Function: sdk.ChatCompletionMessageToolCallFunction{
					Name: "Read", Arguments: `{}`,
				},
			}},
		})

		// Should NOT have called UpdateChatStatus on a non-cancelled
		// completion with tool calls — the tool round-trip will end the
		// status update later.
		if state.UpdateChatStatusCallCount() != 0 {
			t.Errorf("expected no UpdateChatStatus call when tool calls present, got %d", state.UpdateChatStatusCallCount())
		}
	})
}

func TestRunner_SetPendingRestoration_RestoresOnComplete(t *testing.T) {
	t.Run("restoration runs SelectModel on next HandleChatComplete", func(t *testing.T) {
		runner, _, state, _, model := newRunnerForTest()
		state.GetChatSessionReturns(nil)

		runner.SetPendingRestoration("gpt-4")

		_ = runner.HandleChatComplete(domain.ChatCompleteEvent{RequestID: "r"})

		if model.SelectModelCallCount() != 1 {
			t.Fatalf("expected SelectModel called once, got %d", model.SelectModelCallCount())
		}
		if got := model.SelectModelArgsForCall(0); got != "gpt-4" {
			t.Errorf("expected SelectModel(\"gpt-4\"), got %q", got)
		}

		// Second completion should NOT restore again — the pending value
		// is cleared after the first restoration.
		_ = runner.HandleChatComplete(domain.ChatCompleteEvent{RequestID: "r"})
		if model.SelectModelCallCount() != 1 {
			t.Errorf("expected SelectModel still 1 after second complete, got %d", model.SelectModelCallCount())
		}
	})
}
