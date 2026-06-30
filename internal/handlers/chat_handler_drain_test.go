package handlers

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	sdk "github.com/inference-gateway/sdk"

	domain "github.com/inference-gateway/cli/internal/domain"
	mocks "github.com/inference-gateway/cli/tests/mocks/domain"
)

// TestHandleDrainQueueEvent guards the drain gate. It starts a fresh agent turn -
// marking the session pending to close the double-start window - ONLY when the
// chat view is active, the agent is idle, and the queue has content. When the
// agent is busy with work still queued it arms a single retry Cmd (so a transient
// gate-block can't strand the queue) WITHOUT starting a turn. When there is nothing
// stranded (empty queue) or the user is off-chat it returns nil, so the retry never
// fires when idle - no resurrected idle ticker. initialArmed exercises the dedup
// guard: a second push while a retry is already outstanding arms no second timer.
func TestHandleDrainQueueEvent(t *testing.T) {
	tests := []struct {
		name         string
		view         domain.ViewState
		busy         bool
		queueEmpty   bool
		initialArmed bool
		wantStart    bool
		wantCmd      bool
		wantArmed    bool
	}{
		{"idle + queued + chat -> start a turn (and arm retry)", domain.ViewStateChat, false, false, false, true, true, true},
		{"busy + queued + chat -> arm retry only, no start", domain.ViewStateChat, true, false, false, false, true, true},
		{"busy + queued + chat, retry already armed -> dedup (nil, no 2nd timer)", domain.ViewStateChat, true, false, true, false, false, true},
		{"idle + queued + chat, retry already armed -> start, no 2nd timer", domain.ViewStateChat, false, false, true, true, true, true},
		{"empty queue + chat -> nil (no idle ticker)", domain.ViewStateChat, false, true, false, false, false, false},
		{"non-chat view + queued -> nil", domain.ViewStateModelSelection, false, false, false, false, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := &mocks.FakeStateManager{}
			sm.GetCurrentViewReturns(tt.view)
			sm.IsAgentBusyReturns(tt.busy)

			queue := &mocks.FakeMessageQueue{}
			queue.IsEmptyReturns(tt.queueEmpty)

			runner := &mocks.FakeChatCompletionRunner{}
			runner.StartReturns(func() tea.Msg { return nil })

			h := &ChatHandler{
				stateManager:     sm,
				messageQueue:     queue,
				completionRunner: runner,
				directExec:       &mocks.FakeDirectExecutionService{},
			}
			h.drainRetryArmed = tt.initialArmed

			cmd := h.HandleDrainQueueEvent(domain.DrainQueueEvent{})

			if (cmd != nil) != tt.wantCmd {
				t.Fatalf("returned a Cmd = %v, want %v", cmd != nil, tt.wantCmd)
			}
			if started := runner.StartCallCount() > 0; started != tt.wantStart {
				t.Fatalf("startChatCompletion called=%v, want %v", started, tt.wantStart)
			}
			if h.drainRetryArmed != tt.wantArmed {
				t.Fatalf("drainRetryArmed = %v, want %v", h.drainRetryArmed, tt.wantArmed)
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

// TestHandleDrainQueueRetryEvent pins the bounded-retry chain: when the retry tick
// fires it disarms (this timer is spent) then re-runs the gate, re-arming exactly
// one fresh timer while work is still stranded and stopping the moment the queue
// drains. This is what keeps a burst of pushes to a single retry chain.
func TestHandleDrainQueueRetryEvent(t *testing.T) {
	tests := []struct {
		name       string
		busy       bool
		queueEmpty bool
		wantStart  bool
		wantCmd    bool
		wantArmed  bool
	}{
		{"still busy + queued -> re-arm one timer", true, false, false, true, true},
		{"queue drained -> stop (nil, disarmed)", false, true, false, false, false},
		{"idle + queued -> start + re-arm", false, false, true, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := &mocks.FakeStateManager{}
			sm.GetCurrentViewReturns(domain.ViewStateChat)
			sm.IsAgentBusyReturns(tt.busy)

			queue := &mocks.FakeMessageQueue{}
			queue.IsEmptyReturns(tt.queueEmpty)

			runner := &mocks.FakeChatCompletionRunner{}
			runner.StartReturns(func() tea.Msg { return nil })

			h := &ChatHandler{
				stateManager:     sm,
				messageQueue:     queue,
				completionRunner: runner,
				directExec:       &mocks.FakeDirectExecutionService{},
			}
			h.drainRetryArmed = true

			cmd := h.HandleDrainQueueRetryEvent(domain.DrainQueueRetryEvent{})

			if (cmd != nil) != tt.wantCmd {
				t.Fatalf("returned a Cmd = %v, want %v", cmd != nil, tt.wantCmd)
			}
			if started := runner.StartCallCount() > 0; started != tt.wantStart {
				t.Fatalf("startChatCompletion called=%v, want %v", started, tt.wantStart)
			}
			if h.drainRetryArmed != tt.wantArmed {
				t.Fatalf("drainRetryArmed = %v, want %v (exactly one chain)", h.drainRetryArmed, tt.wantArmed)
			}
		})
	}
}

// TestShouldDrainAfterComplete pins the emit decision for HandleChatCompleteEvent:
// a terminal turn (cancelled, or a final answer with no tool calls) drains only
// when the queue is non-empty; a turn that still has tool calls never drains.
func TestShouldDrainAfterComplete(t *testing.T) {
	toolCall := []sdk.ChatCompletionMessageToolCall{{}}
	tests := []struct {
		name       string
		cancelled  bool
		toolCalls  []sdk.ChatCompletionMessageToolCall
		queueEmpty bool
		want       bool
	}{
		{"final answer + queued -> drain", false, nil, false, true},
		{"cancelled + queued -> drain", true, toolCall, false, true},
		{"final answer + empty queue -> no drain", false, nil, true, false},
		{"has tool calls (not terminal) + queued -> no drain", false, toolCall, false, false},
		{"has tool calls + empty queue -> no drain", false, toolCall, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queue := &mocks.FakeMessageQueue{}
			queue.IsEmptyReturns(tt.queueEmpty)
			h := &ChatHandler{messageQueue: queue}

			msg := domain.ChatCompleteEvent{Cancelled: tt.cancelled, ToolCalls: tt.toolCalls}
			if got := h.shouldDrainAfterComplete(msg); got != tt.want {
				t.Errorf("shouldDrainAfterComplete = %v, want %v", got, tt.want)
			}
		})
	}
}
