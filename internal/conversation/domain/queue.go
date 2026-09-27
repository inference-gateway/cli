package domain

import (
	"time"

	sdk "github.com/inference-gateway/sdk"
)

// QueuedMessageSource identifies where a queued message came from. It is set
// when the entry is enqueued so the UI can tell a typed message from a
// background job result without guessing from the message text.
type QueuedMessageSource string

const (
	QueueSourceComposer QueuedMessageSource = "composer"
	QueueSourceStdin    QueuedMessageSource = "stdin"
	QueueSourceA2A      QueuedMessageSource = "a2a"
	QueueSourceShell    QueuedMessageSource = "shell"
	QueueSourceSubagent QueuedMessageSource = "subagent"
	QueueSourceJob      QueuedMessageSource = "job"
)

// QueuedMessage represents a message in the input queue
type QueuedMessage struct {
	Message   sdk.Message
	QueuedAt  time.Time
	RequestID string
	Source    QueuedMessageSource
}
