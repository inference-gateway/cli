// Kernel-side service contracts the agent and its tools consume: reminder/hook
// providers and interactive brokers.

package domain

import (
	"context"
)

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

// BashDetachChannelHolder manages the bash detach channel for background shell operations
type BashDetachChannelHolder interface {
	SetBashDetachChan(chan<- struct{})
	GetBashDetachChan() chan<- struct{}
	ClearBashDetachChan()
}

// UserQuestionBroker publishes an interactive clarifying-question request to the
// user (chat TUI, or a headless host over stdin) and blocks until the user
// answers or the context is cancelled. It is injected into the AskUserQuestion
// tool's execution context when UserQuestionsAvailable reports a host that can
// render the form. Returns ok=false when the user dismisses
// the form (the response channel is closed without a value) or on cancellation.
type UserQuestionBroker interface {
	AskUserQuestions(ctx context.Context, questions []UserQuestion) (answers []UserQuestionAnswer, ok bool, err error)
}
