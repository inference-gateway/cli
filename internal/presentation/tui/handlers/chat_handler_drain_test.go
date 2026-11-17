package handlers

import (
	"testing"

	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"
	tuimocks "github.com/inference-gateway/cli/tests/mocks/tui"

	tea "charm.land/bubbletea/v2"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	statemanager "github.com/inference-gateway/cli/internal/presentation/tui/statemanager"
)

// TestHandleDrainQueueEvent guards the drain gate: it starts a turn only on an
// idle chat view with queued content, arms a single retry while the agent is
// busy, and returns nil when nothing is stranded or the user is off-chat.
// initialArmed checks that a second push arms no second timer.
func TestHandleDrainQueueEvent(t *testing.T) {
	tests := []struct {
		name         string
		view         tui.ViewState
		busy         bool
		queueEmpty   bool
		initialArmed bool
		wantStart    bool
		wantCmd      bool
		wantArmed    bool
	}{
		{"idle + queued + chat -> start a turn (and arm retry)", tui.ViewStateChat, false, false, false, true, true, true},
		{"busy + queued + chat -> arm retry only, no start", tui.ViewStateChat, true, false, false, false, true, true},
		{"busy + queued + chat, retry already armed -> dedup (nil, no 2nd timer)", tui.ViewStateChat, true, false, true, false, false, true},
		{"idle + queued + chat, retry already armed -> start, no 2nd timer", tui.ViewStateChat, false, false, true, true, true, true},
		{"empty queue + chat -> nil (no idle ticker)", tui.ViewStateChat, false, true, false, false, false, false},
		{"non-chat view + queued -> nil", tui.ViewStateModelSelection, false, false, false, false, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := statemanager.NewStore(false)
			_ = sm.TransitionToView(tt.view)
			if tt.busy {
				_ = sm.StartToolExecution([]sdk.ChatCompletionMessageToolCall{{ID: "busy"}})
			}

			queue := &convmocks.FakeMessageQueue{}
			queue.IsEmptyReturns(tt.queueEmpty)

			runner := &tuimocks.FakeChatCompletionRunner{}
			runner.StartReturns(func() tea.Msg { return nil })

			h := &ChatHandler{
				stateManager:     sm,
				messageQueue:     queue,
				completionRunner: runner,
				directExec:       &tuimocks.FakeDirectExecutionService{},
			}
			h.drainRetryArmed = tt.initialArmed

			cmd := h.HandleDrainQueueEvent(agentdomain.DrainQueueEvent{})

			if (cmd != nil) != tt.wantCmd {
				t.Fatalf("returned a Cmd = %v, want %v", cmd != nil, tt.wantCmd)
			}
			if started := runner.StartCallCount() > 0; started != tt.wantStart {
				t.Fatalf("startChatCompletion called=%v, want %v", started, tt.wantStart)
			}
			if h.drainRetryArmed != tt.wantArmed {
				t.Fatalf("drainRetryArmed = %v, want %v", h.drainRetryArmed, tt.wantArmed)
			}

			if got := sm.GetChatSession() != nil; got != tt.wantStart {
				t.Fatalf("SetChatPending observable (pending chat session present) = %v, want %v", got, tt.wantStart)
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
			sm := statemanager.NewStore(false)
			_ = sm.TransitionToView(tui.ViewStateChat)
			if tt.busy {
				_ = sm.StartToolExecution([]sdk.ChatCompletionMessageToolCall{{ID: "busy"}})
			}

			queue := &convmocks.FakeMessageQueue{}
			queue.IsEmptyReturns(tt.queueEmpty)

			runner := &tuimocks.FakeChatCompletionRunner{}
			runner.StartReturns(func() tea.Msg { return nil })

			h := &ChatHandler{
				stateManager:     sm,
				messageQueue:     queue,
				completionRunner: runner,
				directExec:       &tuimocks.FakeDirectExecutionService{},
			}
			h.drainRetryArmed = true

			cmd := h.HandleDrainQueueRetryEvent(tui.DrainQueueRetryEvent{})

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
// when the queue is non-empty. A turn that still has tool calls never drains.
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
			queue := &convmocks.FakeMessageQueue{}
			queue.IsEmptyReturns(tt.queueEmpty)
			h := &ChatHandler{messageQueue: queue}

			msg := agentdomain.ChatCompleteEvent{Cancelled: tt.cancelled, ToolCalls: tt.toolCalls}
			if got := h.shouldDrainAfterComplete(msg); got != tt.want {
				t.Errorf("shouldDrainAfterComplete = %v, want %v", got, tt.want)
			}
		})
	}
}
