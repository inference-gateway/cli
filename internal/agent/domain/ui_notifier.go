package domain

import (
	"time"

	sdk "github.com/inference-gateway/sdk"
)

// UINotifier delivers a background-originated event to the single Bubble Tea
// Update loop. It is the one ingress every background producer uses to push work
// or status changes into the UI, replacing the per-source self-rescheduling
// pollers. The only production implementation wraps (*tea.Program).Send and lives
// in cmd/chat.go; keeping this interface tea-free lets services depend on it
// without importing bubbletea. The event is an `any` (tea.Msg is itself `any`).
type UINotifier interface {
	Notify(event any)
}

// NoopUINotifier is the useful zero value: producers can always call Notify even
// before the program exists or after shutdown, with no nil checks. The container
// defaults to it until cmd/chat.go swaps in the real (program-backed) notifier.
type NoopUINotifier struct{}

// Notify discards the event.
func (NoopUINotifier) Notify(any) {}

// Events sent through UINotifier by non-UI code (capabilities, job supervisor).

// UserInputEvent represents user input submission. FromExtension marks input
// injected over the browser-extension bridge: the sender watches the side
// panel, which has no terminal question UI, so interactive gates (like the
// catalog-skill install confirmation) must not block on it.
type UserInputEvent struct {
	Content       string
	Images        []ImageAttachment
	FromExtension bool
}

// ToolApprovalResponseEvent captures the user's approval decision
type ToolApprovalResponseEvent struct {
	Action   ApprovalAction
	ToolCall sdk.ChatCompletionMessageToolCall
}

// BackgroundTasksChangedEvent signals that a background job's status changed
// (submitted, signalled, completed, or failed). The supervisor pushes it so the
// /tasks view and the inline conversation rows refresh on real change instead of
// polling at render time.
type BackgroundTasksChangedEvent struct{}

// HeartbeatEvent is the app's single periodic tick, pushed through the UI
// notifier by one background goroutine (cmd/chat) at a fixed slow interval. It
// exists so freshness checks that cannot be event-driven (state changed outside
// the TUI, e.g. git status after an editor save) have one clock to ride instead
// of each re-arming its own tea.Tick. Handlers must stay cheap: kick off a
// tea.Cmd for any I/O, never do it inline. Consumers that want a slower cadence
// compare At against their own last-run time.
type HeartbeatEvent struct {
	At time.Time
}

// DrainQueueEvent asks the orchestrator to start a fresh agent turn when the
// agent is idle on the chat view and the shared message queue has content
// (background-job completion notes or user messages typed while busy). Unlike the
// old queue-drain tick it is not a clock: it is pushed exactly once per real
// trigger (a background job landing work, a turn completing with a non-empty
// queue, or re-entering the chat view), and HandleDrainQueueEvent is a pure gate
// that starts a turn (Idle -> CheckingQueue -> ... -> Completing -> Idle) or
// returns nil. There is no self-reschedule.
type DrainQueueEvent struct{}

// ToolExecutionCompletedEvent indicates tool execution is complete
type ToolExecutionCompletedEvent struct {
	SessionID     string
	RequestID     string
	Timestamp     time.Time
	TotalExecuted int
	SuccessCount  int
	FailureCount  int
	Results       []*ToolExecutionResult
}

func (e ToolExecutionCompletedEvent) GetRequestID() string    { return e.RequestID }
func (e ToolExecutionCompletedEvent) GetTimestamp() time.Time { return e.Timestamp }
