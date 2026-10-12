package infrastructure

import (
	"context"
	"testing"

	assert "github.com/stretchr/testify/assert"

	sdk "github.com/inference-gateway/sdk"

	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
)

func userMsg(content string) sdk.Message {
	return sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent(content)}
}

func TestMessageQueueService(t *testing.T) {
	tests := []struct {
		name       string
		enqueue    []string
		dequeues   int
		wantSize   int
		wantEmpty  bool
		wantHeadID string
	}{
		{name: "empty queue", wantSize: 0, wantEmpty: true},
		{name: "single message", enqueue: []string{"r1"}, wantSize: 1, wantHeadID: "r1"},
		{name: "fifo order after partial dequeue", enqueue: []string{"r1", "r2", "r3"}, dequeues: 1, wantSize: 2, wantHeadID: "r2"},
		{name: "dequeue drains queue", enqueue: []string{"r1", "r2"}, dequeues: 2, wantSize: 0, wantEmpty: true},
		{name: "dequeue past empty returns nil", enqueue: []string{"r1"}, dequeues: 3, wantSize: 0, wantEmpty: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mq := NewMessageQueueService()
			ctx := context.Background()
			for _, id := range tt.enqueue {
				assert.NoError(t, mq.Enqueue(ctx, userMsg("msg-"+id), convdomain.QueueSourceComposer, id))
			}

			dequeued := make([]string, 0, tt.dequeues)
			for i := 0; i < tt.dequeues; i++ {
				msg, err := mq.Dequeue(ctx)
				assert.NoError(t, err)
				if msg != nil {
					dequeued = append(dequeued, msg.RequestID)
				}
			}

			expectedDequeued := append([]string{}, tt.enqueue[:min(tt.dequeues, len(tt.enqueue))]...)
			assert.Equal(t, expectedDequeued, dequeued)

			assert.Equal(t, tt.wantSize, mq.Size())
			assert.Equal(t, tt.wantEmpty, mq.IsEmpty())

			all := mq.GetAll()
			assert.Len(t, all, tt.wantSize)
			for i, qm := range all {
				assert.Equal(t, tt.enqueue[tt.dequeues+i], qm.RequestID)
				assert.Equal(t, convdomain.QueueSourceComposer, qm.Source)
			}

			assert.NoError(t, mq.Clear(ctx))
			assert.True(t, mq.IsEmpty())
			assert.Zero(t, mq.Size())
			msg, err := mq.Dequeue(ctx)
			assert.NoError(t, err)
			assert.Nil(t, msg)
		})
	}
}

func TestMessageQueueService_GetAllReturnsCopy(t *testing.T) {
	mq := NewMessageQueueService()
	ctx := context.Background()
	assert.NoError(t, mq.Enqueue(ctx, userMsg("a"), convdomain.QueueSourceComposer, "r1"))

	all := mq.GetAll()
	all[0].RequestID = "mutated"

	assert.Equal(t, "r1", mq.GetAll()[0].RequestID)
}

func TestMessageQueueService_DrainReturnsAllInOrder(t *testing.T) {
	mq := NewMessageQueueService()
	ctx := context.Background()
	assert.NoError(t, mq.Enqueue(ctx, userMsg("a"), convdomain.QueueSourceComposer, "r1"))
	assert.NoError(t, mq.Enqueue(ctx, userMsg("b"), convdomain.QueueSourceStdin, "r2"))
	assert.NoError(t, mq.Enqueue(ctx, userMsg("c"), convdomain.QueueSourceShell, "r3"))

	drained, err := mq.Drain(ctx)
	assert.NoError(t, err)
	assert.Equal(t, []string{"r1", "r2", "r3"}, []string{drained[0].RequestID, drained[1].RequestID, drained[2].RequestID})
	assert.True(t, mq.IsEmpty(), "drain must empty the queue")

	drained, err = mq.Drain(ctx)                                                                                    
	assert.NoError(t, err)                                                                                          
	assert.Empty(t, drained)                                                                                        
}                                                                                                                   

func TestMessageQueueService_Conformance(t *testing.T) {
	runMessageQueueConformance(t, func(t *testing.T) convdomain.MessageQueue {
		return NewMessageQueueService()
	})
}

func TestMessageQueueService_BackendIdentity(t *testing.T) {
	q := NewMessageQueueService()
	assert.Equal(t, "memory", q.BackendName())
	assert.False(t, q.Unreachable())
}
