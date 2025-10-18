package services

import (
	"sync"

	domain "github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
)

// MessageQueueService manages a centralized queue for messages waiting to be processed
type MessageQueueService struct {
	mu       sync.RWMutex
	messages []domain.QueuedMessage
}

// NewMessageQueueService creates a new message queue service
func NewMessageQueueService() *MessageQueueService {
	return &MessageQueueService{
		messages: make([]domain.QueuedMessage, 0),
	}
}

// Enqueue adds a message to the queue
func (mq *MessageQueueService) Enqueue(message sdk.Message, requestID string) {
	mq.mu.Lock()
	defer mq.mu.Unlock()

	mq.messages = append(mq.messages, domain.QueuedMessage{
		Message:   message,
		RequestID: requestID,
	})
}

// Dequeue removes and returns the next message from the queue
// Returns nil if the queue is empty
func (mq *MessageQueueService) Dequeue() *domain.QueuedMessage {
	mq.mu.Lock()
	defer mq.mu.Unlock()

	if len(mq.messages) == 0 {
		return nil
	}

	msg := mq.messages[0]
	mq.messages = mq.messages[1:]
	return &msg
}

// Peek returns the next message without removing it
// Returns nil if the queue is empty
func (mq *MessageQueueService) Peek() *domain.QueuedMessage {
	mq.mu.RLock()
	defer mq.mu.RUnlock()

	if len(mq.messages) == 0 {
		return nil
	}

	return &mq.messages[0]
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
func (mq *MessageQueueService) Clear() {
	mq.mu.Lock()
	defer mq.mu.Unlock()

	mq.messages = make([]domain.QueuedMessage, 0)
}

// GetAll returns all messages in the queue without removing them
func (mq *MessageQueueService) GetAll() []domain.QueuedMessage {
	mq.mu.RLock()
	defer mq.mu.RUnlock()

	// Return a copy to prevent external modification
	result := make([]domain.QueuedMessage, len(mq.messages))
	copy(result, mq.messages)
	return result
}
