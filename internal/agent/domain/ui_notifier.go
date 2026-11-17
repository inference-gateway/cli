package domain

import (
	"time"

	sdk "github.com/inference-gateway/sdk"
)

// UINotifier delivers a background-originated event to the single Bubble Tea
// Update loop, the one ingress every background producer uses. The production
// implementation wraps (*tea.Program).Send. Keeping it tea-free lets services
// depend on it without importing bubbletea.
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

// UserInputEvent represents user input submission.
type UserInputEvent struct {
	Content string
	Images  []ImageAttachment
}

// ToolApprovalResponseEvent captures the user's approval decision
type ToolApprovalResponseEvent struct {
	Action   ApprovalAction
	ToolCall sdk.ChatCompletionMessageToolCall
}

// BackgroundTasksChangedEvent signals that a background job's status changed
// (submitted, signalled, completed, or failed). The supervisor pushes it so the
// /tasks view and the inline conversation rows refresh on real change instead of
// polling at render time. It is also a ChatEvent so headless can bridge it into
// the rendered stream as a backgroundTasks state patch.
type BackgroundTasksChangedEvent struct {
	Timestamp time.Time
}

func (e BackgroundTasksChangedEvent) GetRequestID() string    { return "" }
func (e BackgroundTasksChangedEvent) GetTimestamp() time.Time { return e.Timestamp }

// DrainQueueEvent asks the orchestrator to start a fresh agent turn when it is
// idle on the chat view and the message queue has content. It is pushed once
// per real trigger, never on a clock, and HandleDrainQueueEvent is a pure gate
// that either starts a turn or returns nil.
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
