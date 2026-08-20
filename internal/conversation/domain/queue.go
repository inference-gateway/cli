package domain

import (
	"time"

	sdk "github.com/inference-gateway/sdk"
)

// QueuedMessage represents a message in the input queue
type QueuedMessage struct {
	Message   sdk.Message
	QueuedAt  time.Time
	RequestID string
}
