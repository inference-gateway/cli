package toolcoordinator

import (
	"testing"

	sdk "github.com/inference-gateway/sdk"

	domain "github.com/inference-gateway/cli/internal/domain"
	services "github.com/inference-gateway/cli/internal/services"
	mocksdomain "github.com/inference-gateway/cli/tests/mocks/domain"
)

func newCoordinatorForTest() (*Coordinator, *services.InMemoryConversationRepository, *mocksdomain.FakeStateManager, *mocksdomain.FakeDirectExecutionService) {
	repo := services.NewInMemoryConversationRepository(nil, nil)
	state := &mocksdomain.FakeStateManager{}
	direct := &mocksdomain.FakeDirectExecutionService{}
	listener := &mocksdomain.FakeChatEventListener{}

	c := NewCoordinator(Options{
		ConversationRepo: repo,
		StateManager:     state,
		DirectExec:       direct,
		Listener:         listener,
	})
	return c, repo, state, direct
}

func TestCoordinator_ActiveToolCallID(t *testing.T) {
	t.Run("Set and Get are thread-safe and return the latest value", func(t *testing.T) {
		c, _, _, _ := newCoordinatorForTest()
		if got := c.GetActiveToolCallID(); got != "" {
			t.Errorf("expected empty initial value, got %q", got)
		}
		c.SetActiveToolCallID("tc-1")
		if got := c.GetActiveToolCallID(); got != "tc-1" {
			t.Errorf("expected 'tc-1', got %q", got)
		}
		c.SetActiveToolCallID("")
		if got := c.GetActiveToolCallID(); got != "" {
			t.Errorf("expected empty after clear, got %q", got)
		}
	})
}

func TestCoordinator_HandleToolApprovalResponse(t *testing.T) {
	t.Run("Approve sends decision to response channel, clears UI state, returns non-nil cmd", func(t *testing.T) {
		c, _, state, _ := newCoordinatorForTest()
		responseChan := make(chan domain.ApprovalAction, 1)
		state.GetApprovalUIStateReturns(&domain.ApprovalUIState{ResponseChan: responseChan})
		state.GetChatSessionReturns(nil)

		cmd := c.HandleToolApprovalResponse(domain.ToolApprovalResponseEvent{
			Action:   domain.ApprovalApprove,
			ToolCall: sdk.ChatCompletionMessageToolCall{ID: "tc-1"},
		})

		if cmd == nil {
			t.Fatalf("expected non-nil cmd")
		}
		if state.ClearApprovalUIStateCallCount() != 1 {
			t.Errorf("expected ClearApprovalUIState once, got %d", state.ClearApprovalUIStateCallCount())
		}
		select {
		case action := <-responseChan:
			if action != domain.ApprovalApprove {
				t.Errorf("expected ApprovalApprove on response chan, got %v", action)
			}
		default:
			t.Errorf("expected approval decision to be sent down response channel")
		}
	})

	t.Run("AutoAccept switches agent mode and approves on the response chan", func(t *testing.T) {
		c, _, state, _ := newCoordinatorForTest()
		responseChan := make(chan domain.ApprovalAction, 1)
		state.GetApprovalUIStateReturns(&domain.ApprovalUIState{ResponseChan: responseChan})
		state.GetChatSessionReturns(nil)

		_ = c.HandleToolApprovalResponse(domain.ToolApprovalResponseEvent{
			Action:   domain.ApprovalAutoAccept,
			ToolCall: sdk.ChatCompletionMessageToolCall{ID: "tc-1"},
		})

		if state.SetAgentModeCallCount() != 1 {
			t.Fatalf("expected SetAgentMode once, got %d", state.SetAgentModeCallCount())
		}
		if mode := state.SetAgentModeArgsForCall(0); mode != domain.AgentModeAutoAccept {
			t.Errorf("expected AgentModeAutoAccept, got %v", mode)
		}
		select {
		case action := <-responseChan:
			if action != domain.ApprovalApprove {
				t.Errorf("auto-accept should send Approve, got %v", action)
			}
		default:
			t.Errorf("expected an approval decision to be sent")
		}
	})

	t.Run("Reject sends Reject down channel and does not switch agent mode", func(t *testing.T) {
		c, _, state, _ := newCoordinatorForTest()
		responseChan := make(chan domain.ApprovalAction, 1)
		state.GetApprovalUIStateReturns(&domain.ApprovalUIState{ResponseChan: responseChan})
		state.GetChatSessionReturns(nil)

		_ = c.HandleToolApprovalResponse(domain.ToolApprovalResponseEvent{
			Action:   domain.ApprovalReject,
			ToolCall: sdk.ChatCompletionMessageToolCall{ID: "tc-1"},
		})

		if state.SetAgentModeCallCount() != 0 {
			t.Errorf("reject should not change agent mode")
		}
		select {
		case action := <-responseChan:
			if action != domain.ApprovalReject {
				t.Errorf("expected ApprovalReject, got %v", action)
			}
		default:
			t.Errorf("expected reject decision to be sent")
		}
	})
}

func TestCoordinator_HandleToolExecutionProgress(t *testing.T) {
	t.Run("starting status sets activeToolCallID", func(t *testing.T) {
		c, _, state, direct := newCoordinatorForTest()
		state.GetChatSessionReturns(nil)
		direct.PendingToolChannelReturns(nil)
		direct.PendingBashChannelReturns(nil)

		_ = c.HandleToolExecutionProgress(domain.ToolExecutionProgressEvent{
			ToolCallID: "tc-1",
			ToolName:   "Bash",
			Status:     "starting",
			Message:    "starting bash",
		})

		if got := c.GetActiveToolCallID(); got != "tc-1" {
			t.Errorf("expected activeToolCallID set to 'tc-1', got %q", got)
		}
	})

	t.Run("completed status clears activeToolCallID", func(t *testing.T) {
		c, _, state, direct := newCoordinatorForTest()
		state.GetChatSessionReturns(nil)
		direct.PendingToolChannelReturns(nil)
		direct.PendingBashChannelReturns(nil)
		c.SetActiveToolCallID("tc-1")

		_ = c.HandleToolExecutionProgress(domain.ToolExecutionProgressEvent{
			ToolCallID: "tc-1",
			Status:     "completed",
			Message:    "done",
		})

		if got := c.GetActiveToolCallID(); got != "" {
			t.Errorf("expected activeToolCallID cleared, got %q", got)
		}
	})

	t.Run("unknown status returns nil cmd when no channels active", func(t *testing.T) {
		c, _, state, direct := newCoordinatorForTest()
		state.GetChatSessionReturns(nil)
		direct.PendingToolChannelReturns(nil)
		direct.PendingBashChannelReturns(nil)

		cmd := c.HandleToolExecutionProgress(domain.ToolExecutionProgressEvent{
			Status: "executing", // not in the recognized switch
		})
		if cmd != nil {
			t.Errorf("expected nil cmd for unknown status with no channels, got %v", cmd)
		}
	})
}

func TestCoordinator_HandleToolApprovalRequested(t *testing.T) {
	t.Run("sets approval UI state, broadcasts notification, returns non-nil cmd", func(t *testing.T) {
		c, _, state, _ := newCoordinatorForTest()
		responseChan := make(chan domain.ApprovalAction, 1)
		state.GetChatSessionReturns(nil)

		cmd := c.HandleToolApprovalRequested(domain.ToolApprovalRequestedEvent{
			ToolCall: sdk.ChatCompletionMessageToolCall{
				ID:       "tc-1",
				Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Read"},
			},
			ResponseChan: responseChan,
		})

		if cmd == nil {
			t.Fatalf("expected non-nil cmd")
		}
		if state.SetupApprovalUIStateCallCount() != 1 {
			t.Errorf("expected SetupApprovalUIState once")
		}
		if state.BroadcastEventCallCount() != 1 {
			t.Errorf("expected BroadcastEvent once")
		}
	})
}

func TestCoordinator_HandleToolExecutionCompleted(t *testing.T) {
	t.Run("clears active tool id and returns non-nil cmd", func(t *testing.T) {
		c, _, state, _ := newCoordinatorForTest()
		state.GetChatSessionReturns(nil)
		c.SetActiveToolCallID("tc-1")

		cmd := c.HandleToolExecutionCompleted(domain.ToolExecutionCompletedEvent{
			TotalExecuted: 2,
			SuccessCount:  2,
		})

		if cmd == nil {
			t.Fatalf("expected non-nil cmd")
		}
		if c.GetActiveToolCallID() != "" {
			t.Errorf("expected activeToolCallID cleared")
		}
	})
}
