// Kernel-side service contracts the agent and its tools consume: A2A task
// tracking, reminder/hook providers, and interactive brokers.

package domain

import (
	"context"
	"time"
)

// A2ATaskTracker handles A2A task ID and context ID tracking within chat
// sessions. Following A2A spec: supports multi-tenant with multiple
// contexts per agent. This is one half of the BackgroundTaskRegistry; the
// other half is ShellTracker (defined in shell.go). Code that only needs
// the A2A surface can depend on this narrower interface.
type A2ATaskTracker interface {
	// Context management (contexts are server-generated and tracked here).
	// Multiple contexts per agent enable multi-tenant/multi-session support.
	RegisterContext(agentURL, contextID string)
	GetLatestContextForAgent(agentURL string) string
	HasContext(contextID string) bool

	// Task management (tasks are server-generated and scoped to contexts per A2A spec)
	AddTask(contextID, taskID string)
	GetLatestTaskForContext(contextID string) string
	RemoveTask(taskID string)

	// Agent management
	ClearAllAgents()

	// Polling state management (one polling state per task)
	StartPolling(taskID string, state *TaskPollingState)
	StopPolling(taskID string)
	GetPollingState(taskID string) *TaskPollingState
	GetAllPollingTasks() []string
}

// TaskPollingState is the data record for one in-flight A2A task that the task
// view reads. Monitoring is owned by the job supervisor (a2aJob), which polls the
// remote agent and updates LastKnownState here.
type TaskPollingState struct {
	TaskID          string
	ContextID       string
	AgentURL        string
	TaskDescription string
	IsPolling       bool
	StartedAt       time.Time
	LastKnownState  string
}

// SystemReminderProvider decides which system reminders are due for a given
// ReminderQuery (hook point, per-run turn, cumulative session turn, max turns,
// and the already-fired set). It is implemented by config from the user's
// reminders list; the agent depends on this interface so reminder policy can be
// faked in tests.
type SystemReminderProvider interface {
	RemindersDue(q ReminderQuery) []SystemReminder
}

// HookCommandProvider resolves which command hooks are due at a hook point. It
// is the command-action sibling of SystemReminderProvider, implemented by config
// from the user's hooks list. The provider only resolves the commands; the agent
// runs them through the existing bash allow-list, so config stays free of
// os/exec. The agent depends on this interface so the command set can be faked
// in tests.
type HookCommandProvider interface {
	CommandsDue(hook HookPoint) []HookCommand
}

// JudgeApprover decides one pending tool call by asking a small LLM whether it
// serves the user's intent and is safe. intent is the latest non-hidden user
// message and action the pending tool call (name + arguments). The judge is
// the approver selected by approval_behaviour "judge" / agent mode
// auto-with-judge: it is always reachable, so headless and CI get a real
// approver instead of blocking.
type JudgeApprover interface {
	Judge(ctx context.Context, intent, action string) (JudgeVerdict, error)
}

// BashDetachChannelHolder manages the bash detach channel for background shell operations
type BashDetachChannelHolder interface {
	SetBashDetachChan(chan<- struct{})
	GetBashDetachChan() chan<- struct{}
	ClearBashDetachChan()
}

// UserQuestionBroker publishes an interactive clarifying-question request to the
// TUI and blocks until the user answers or the context is cancelled. It is
// injected into the AskUserQuestion tool's execution context only on the chat
// path (where a TTY/event loop exists). Returns ok=false when the user dismisses
// the form (the response channel is closed without a value) or on cancellation.
type UserQuestionBroker interface {
	AskUserQuestions(ctx context.Context, questions []UserQuestion) (answers []UserQuestionAnswer, ok bool, err error)
}
