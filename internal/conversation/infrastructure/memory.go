package infrastructure

import (
	"context"
	"sync"
	"time"

	sdk "github.com/inference-gateway/sdk"

	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
)

// MessageQueueService manages the in-memory message queue: the process-local
// default backend where nothing can fail and nothing survives a restart.
type MessageQueueService struct {
	mu       sync.RWMutex
	messages []convdomain.QueuedMessage
}

// NewMessageQueueService creates a new message queue service
func NewMessageQueueService() *MessageQueueService {
	return &MessageQueueService{
		messages: make([]convdomain.QueuedMessage, 0),
	}
}

// Enqueue adds a message to the queue with its origin source and request ID
func (mq *MessageQueueService) Enqueue(_ context.Context, message sdk.Message, source convdomain.QueuedMessageSource, requestID string) error {
	mq.mu.Lock()
	defer mq.mu.Unlock()

	mq.messages = append(mq.messages, convdomain.QueuedMessage{
		Message:   message,
		QueuedAt:  time.Now(),
		RequestID: requestID,
		Source:    source,
	})
	return nil
}

// Drain removes and returns every queued message, oldest first
func (mq *MessageQueueService) Drain(_ context.Context) ([]convdomain.QueuedMessage, error) {
	mq.mu.Lock()
	defer mq.mu.Unlock()

	drained := mq.messages
	mq.messages = make([]convdomain.QueuedMessage, 0)
	return drained, nil
}

// Dequeue removes and returns the next message from the queue
// Returns nil if the queue is empty
func (mq *MessageQueueService) Dequeue(_ context.Context) (*convdomain.QueuedMessage, error) {
	mq.mu.Lock()
	defer mq.mu.Unlock()

	if len(mq.messages) == 0 {
		return nil, nil
	}

	msg := mq.messages[0]
	mq.messages = mq.messages[1:]
	return &msg, nil
}

// Size returns the number of messages in the queue
func (mq *MessageQueueService) Size() int {
	mq.mu.RLock()
	defer mq.mu.RUnlock()

	return len(mq.messages)
}

// IsEmpty returns true if the queue has no messages
func (mq *MessageQueueService) IsEmpty() bool {
	mq.mu.RLock()
	defer mq.mu.RUnlock()

	return len(mq.messages) == 0
}

// Clear removes all messages from the queue
func (mq *MessageQueueService) Clear(_ context.Context) error {
	mq.mu.Lock()
	defer mq.mu.Unlock()

	mq.messages = make([]convdomain.QueuedMessage, 0)
	return nil
}

// GetAll returns all messages in the queue without removing them
func (mq *MessageQueueService) GetAll() []convdomain.QueuedMessage {
	mq.mu.RLock()
	defer mq.mu.RUnlock()

	result := make([]convdomain.QueuedMessage, len(mq.messages))
	copy(result, mq.messages)
	return result
}
