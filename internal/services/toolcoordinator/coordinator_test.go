package toolcoordinator

import (
	"testing"

	mocksdomain "github.com/inference-gateway/cli/tests/mocks/domain"

	sdk "github.com/inference-gateway/sdk"

	domain "github.com/inference-gateway/cli/internal/domain"
	services "github.com/inference-gateway/cli/internal/services"
)

func newCoordinatorForTest() (*Coordinator, *services.InMemoryConversationRepository, *services.StateManager, *mocksdomain.FakeDirectExecutionService) {
	repo := services.NewInMemoryConversationRepository(nil, nil)
	state := services.NewStateManager(false)
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

// recordingEventBridge is a minimal domain.EventBridge that counts Publish
// calls, standing in for the deleted BroadcastEvent spy.
type recordingEventBridge struct {
	published []domain.ChatEvent
}

func (r *recordingEventBridge) Tap(input <-chan domain.ChatEvent) <-chan domain.ChatEvent {
	return input
}
func (r *recordingEventBridge) Publish(event domain.ChatEvent) {
	r.published = append(r.published, event)
}
func (r *recordingEventBridge) Subscribe() chan domain.ChatEvent     { return make(chan domain.ChatEvent) }
func (r *recordingEventBridge) Unsubscribe(ch chan domain.ChatEvent) {}

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
		state.SetupApprovalUIState(&sdk.ChatCompletionMessageToolCall{ID: "tc-1"}, responseChan)

		cmd := c.HandleToolApprovalResponse(domain.ToolApprovalResponseEvent{
			Action:   domain.ApprovalApprove,
			ToolCall: sdk.ChatCompletionMessageToolCall{ID: "tc-1"},
		})

		if cmd == nil {
			t.Fatalf("expected non-nil cmd")
		}
		if state.GetApprovalUIState() != nil {
			t.Errorf("expected approval UI state cleared, got %+v", state.GetApprovalUIState())
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
		state.SetupApprovalUIState(&sdk.ChatCompletionMessageToolCall{ID: "tc-1"}, responseChan)

		_ = c.HandleToolApprovalResponse(domain.ToolApprovalResponseEvent{
			Action:   domain.ApprovalAutoAccept,
			ToolCall: sdk.ChatCompletionMessageToolCall{ID: "tc-1"},
		})

		if mode := state.GetAgentMode(); mode != domain.AgentModeAutoAccept {
			t.Errorf("expected agent mode AutoAccept after auto-accept approval, got %v", mode)
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
		state.SetupApprovalUIState(&sdk.ChatCompletionMessageToolCall{ID: "tc-1"}, responseChan)

		_ = c.HandleToolApprovalResponse(domain.ToolApprovalResponseEvent{
			Action:   domain.ApprovalReject,
			ToolCall: sdk.ChatCompletionMessageToolCall{ID: "tc-1"},
		})

		if mode := state.GetAgentMode(); mode != domain.AgentModeStandard {
			t.Errorf("reject should not change agent mode, got %v", mode)
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
		c, _, _, direct := newCoordinatorForTest()
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
		c, _, _, direct := newCoordinatorForTest()
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

	t.Run("completed status emits a history refresh", func(t *testing.T) {
		c, _, _, _ := newCoordinatorForTest()

		cmds := c.progressStatusCmds(domain.ToolExecutionProgressEvent{
			ToolCallID: "tc-1",
			Status:     "completed",
			Message:    "done",
		})

		foundHistory := false
		for _, cmd := range cmds {
			if cmd == nil {
				continue
			}
			if _, ok := cmd().(domain.UpdateHistoryEvent); ok {
				foundHistory = true
				break
			}
		}
		if !foundHistory {
			t.Errorf("expected an UpdateHistoryEvent cmd on completed status, got none")
		}
	})

	t.Run("unknown status returns nil cmd when no channels active", func(t *testing.T) {
		c, _, _, direct := newCoordinatorForTest()
		direct.PendingToolChannelReturns(nil)
		direct.PendingBashChannelReturns(nil)

		cmd := c.HandleToolExecutionProgress(domain.ToolExecutionProgressEvent{
			Status: "executing",
		})
		if cmd != nil {
			t.Errorf("expected nil cmd for unknown status with no channels, got %v", cmd)
		}
	})
}

func TestCoordinator_HandleToolApprovalRequested(t *testing.T) {
	t.Run("sets approval UI state, broadcasts notification, returns non-nil cmd", func(t *testing.T) {
		c, _, state, _ := newCoordinatorForTest()
		rec := &recordingEventBridge{}
		state.SetEventBridge(rec)
		responseChan := make(chan domain.ApprovalAction, 1)

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
		if state.GetApprovalUIState() == nil {
			t.Errorf("expected SetupApprovalUIState to establish approval UI state")
		}
		if len(rec.published) != 1 {
			t.Errorf("expected BroadcastEvent once, got %d", len(rec.published))
		}
	})
}

func TestCoordinator_HandleToolExecutionCompleted(t *testing.T) {
	t.Run("clears active tool id and returns non-nil cmd", func(t *testing.T) {
		c, _, _, _ := newCoordinatorForTest()
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
