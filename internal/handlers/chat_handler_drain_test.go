package handlers

import (
	"testing"

	domain "github.com/inference-gateway/cli/internal/domain"
	mocks "github.com/inference-gateway/cli/tests/mocks/domain"
)

// TestHandleDrainQueueTickEvent is the regression guard for the queue-drain
// ticker (the reliable replacement for the supervisor "wake"). It must start a
// fresh agent turn - and mark the session pending to close the double-start
// window - ONLY when the chat view is active, the agent is idle, and the queue
// has content. In every other case it just reschedules itself.
func TestHandleDrainQueueTickEvent(t *testing.T) {
	tests := []struct {
		name       string
		view       domain.ViewState
		busy       bool
		queueEmpty bool
		wantStart  bool
	}{
		{"idle + queued + chat -> start a turn", domain.ViewStateChat, false, false, true},
		{"agent busy -> reschedule only", domain.ViewStateChat, true, false, false},
		{"empty queue -> reschedule only", domain.ViewStateChat, false, true, false},
		{"non-chat view -> reschedule only", domain.ViewStateModelSelection, false, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := &mocks.FakeStateManager{}
			sm.GetCurrentViewReturns(tt.view)
			sm.IsAgentBusyReturns(tt.busy)

			queue := &mocks.FakeMessageQueue{}
			queue.IsEmptyReturns(tt.queueEmpty)

			runner := &mocks.FakeChatCompletionRunner{}

			h := &ChatHandler{
				stateManager:     sm,
				messageQueue:     queue,
				completionRunner: runner,
				directExec:       &mocks.FakeDirectExecutionService{},
			}

			cmd := h.HandleDrainQueueTickEvent(domain.DrainQueueTickEvent{})
			if cmd == nil {
				t.Fatal("handler must always return a reschedule Cmd so the ticker keeps running")
			}

			if started := runner.StartCallCount() > 0; started != tt.wantStart {
				t.Fatalf("startChatCompletion called=%v, want %v", started, tt.wantStart)
			}

			wantPending := 0
			if tt.wantStart {
				wantPending = 1
			}
			if got := sm.SetChatPendingCallCount(); got != wantPending {
				t.Fatalf("SetChatPending called %d times, want %d (must guard the double-start window)", got, wantPending)
			}
		})
	}
}
