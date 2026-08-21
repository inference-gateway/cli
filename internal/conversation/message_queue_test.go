package conversation

import (
	"testing"

	assert "github.com/stretchr/testify/assert"

	sdk "github.com/inference-gateway/sdk"
)

func userMsg(content string) sdk.Message {
	return sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent(content)}
}

func TestMessageQueueService(t *testing.T) {
	tests := []struct {
		name       string
		enqueue    []string // request IDs, enqueued in order
		dequeues   int
		wantSize   int
		wantEmpty  bool
		wantPeekID string // "" means Peek returns nil
	}{
		{name: "empty queue", wantSize: 0, wantEmpty: true},
		{name: "single message", enqueue: []string{"r1"}, wantSize: 1, wantPeekID: "r1"},
		{name: "fifo order after partial dequeue", enqueue: []string{"r1", "r2", "r3"}, dequeues: 1, wantSize: 2, wantPeekID: "r2"},
		{name: "dequeue drains queue", enqueue: []string{"r1", "r2"}, dequeues: 2, wantSize: 0, wantEmpty: true},
		{name: "dequeue past empty returns nil", enqueue: []string{"r1"}, dequeues: 3, wantSize: 0, wantEmpty: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mq := NewMessageQueueService()
			for _, id := range tt.enqueue {
				mq.Enqueue(userMsg("msg-"+id), id)
			}

			dequeued := make([]string, 0, tt.dequeues)
			for i := 0; i < tt.dequeues; i++ {
				if msg := mq.Dequeue(); msg != nil {
					dequeued = append(dequeued, msg.RequestID)
				}
			}

			// FIFO: dequeued IDs are the enqueue prefix
			expectedDequeued := append([]string{}, tt.enqueue[:min(tt.dequeues, len(tt.enqueue))]...)
			assert.Equal(t, expectedDequeued, dequeued)

			assert.Equal(t, tt.wantSize, mq.Size())
			assert.Equal(t, tt.wantEmpty, mq.IsEmpty())

			peeked := mq.Peek()
			if tt.wantPeekID == "" {
				assert.Nil(t, peeked)
			} else if assert.NotNil(t, peeked) {
				assert.Equal(t, tt.wantPeekID, peeked.RequestID)
				assert.Equal(t, tt.wantSize, mq.Size(), "Peek must not remove")
			}

			all := mq.GetAll()
			assert.Len(t, all, tt.wantSize)
			for i, qm := range all {
				assert.Equal(t, tt.enqueue[tt.dequeues+i], qm.RequestID)
			}

			mq.Clear()
			assert.True(t, mq.IsEmpty())
			assert.Zero(t, mq.Size())
			assert.Nil(t, mq.Dequeue())
		})
	}
}

func TestMessageQueueService_GetAllReturnsCopy(t *testing.T) {
	mq := NewMessageQueueService()
	mq.Enqueue(userMsg("a"), "r1")

	all := mq.GetAll()
	all[0].RequestID = "mutated"

	assert.Equal(t, "r1", mq.Peek().RequestID)
}
