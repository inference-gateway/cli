// Kernel-side service contracts the agent and its tools consume: reminder/hook
// providers and interactive brokers.

package domain

import (
	"context"
)

// SystemReminderProvider decides which system reminders are due for a given
// ReminderQuery (hook point, per-run turn, cumulative session turn, max turns,
// and the already-fired set). It is implemented by config from the user's
// reminders list. The agent depends on this interface so reminder policy can be
// faked in tests.
type SystemReminderProvider interface {
	RemindersDue(q ReminderQuery) []SystemReminder
}

// HookCommandProvider resolves which command hooks are due at a hook point. It
// is the command sibling of SystemReminderProvider. The agent runs the commands
// through the bash allow-list, so config stays free of os/exec, and tests can
// fake the command set.
type HookCommandProvider interface {
	CommandsDue(hook HookPoint) []HookCommand
}

// BashDetachChannelHolder manages the bash detach channel for background shell operations
type BashDetachChannelHolder interface {
	SetBashDetachChan(chan<- struct{})
	GetBashDetachChan() chan<- struct{}
	ClearBashDetachChan()
}

// UserQuestionBroker publishes a clarifying-question request to the user and
// blocks until they answer or the context is cancelled. It is injected into
// AskUserQuestion when a host can render the form. It returns ok=false when the
// user dismisses the form or on cancellation.
type UserQuestionBroker interface {
	AskUserQuestions(ctx context.Context, questions []UserQuestion) (answers []UserQuestionAnswer, ok bool, err error)
}
