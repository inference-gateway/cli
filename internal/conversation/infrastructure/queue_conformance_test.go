package infrastructure

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
)

// queueFactory returns a fresh MessageQueue adapter. Every queue built by one
// factory inside one subtest talks to the same backend scope; scopes differ per
// subtest so groups never see each other's entries.
type queueFactory func(t *testing.T) convdomain.MessageQueue

// runMessageQueueConformance runs the behavioural suite every MessageQueue
// adapter must satisfy, including the process-local memory queue.
func runMessageQueueConformance(t *testing.T, newQueue queueFactory) {
	t.Helper()

	t.Run("Enqueue and drain FIFO", func(t *testing.T) {
		q := newQueue(t)
		ctx := context.Background()

		require.NoError(t, q.Enqueue(ctx, userMsg("first"), convdomain.QueueSourceComposer, "r1"))
		require.NoError(t, q.Enqueue(ctx, userMsg("second"), convdomain.QueueSourceStdin, "r2"))
		require.NoError(t, q.Enqueue(ctx, userMsg("third"), convdomain.QueueSourceJob, "r3"))

		drained, err := q.Drain(ctx)
		require.NoError(t, err)
		require.Len(t, drained, 3)
		assert.Equal(t, []string{"r1", "r2", "r3"}, queuedRequestIDs(drained))
		assert.Equal(t, convdomain.QueueSourceComposer, drained[0].Source)
		assert.Equal(t, convdomain.QueueSourceStdin, drained[1].Source)
		assert.Equal(t, convdomain.QueueSourceJob, drained[2].Source)
		assert.Equal(t, "first", queuedContent(t, drained[0]))
		assert.True(t, q.IsEmpty(), "a full drain empties the queue")
		assert.Zero(t, q.Size())
	})

	t.Run("Drain of an empty queue", func(t *testing.T) {
		q := newQueue(t)

		drained, err := q.Drain(context.Background())
		assert.NoError(t, err)
		assert.Empty(t, drained)
	})

	t.Run("Dequeue takes the head", func(t *testing.T) {
		q := newQueue(t)
		ctx := context.Background()

		head, err := q.Dequeue(ctx)
		assert.NoError(t, err)
		assert.Nil(t, head, "an empty queue dequeues nil")

		require.NoError(t, q.Enqueue(ctx, userMsg("a"), convdomain.QueueSourceComposer, "r1"))
		require.NoError(t, q.Enqueue(ctx, userMsg("b"), convdomain.QueueSourceComposer, "r2"))

		head, err = q.Dequeue(ctx)
		require.NoError(t, err)
		require.NotNil(t, head)
		assert.Equal(t, "r1", head.RequestID)

		drained, err := q.Drain(ctx)
		require.NoError(t, err)
		require.Len(t, drained, 1)
		assert.Equal(t, "r2", drained[0].RequestID)
	})

	t.Run("Clear empties the queue", func(t *testing.T) {
		q := newQueue(t)
		ctx := context.Background()
		require.NoError(t, q.Enqueue(ctx, userMsg("a"), convdomain.QueueSourceComposer, "r1"))
		require.NoError(t, q.Enqueue(ctx, userMsg("b"), convdomain.QueueSourceStdin, "r2"))

		require.NoError(t, q.Clear(ctx))
		assert.True(t, q.IsEmpty())
		assert.Zero(t, q.Size())
		assert.Empty(t, q.GetAll())

		drained, err := q.Drain(ctx)
		assert.NoError(t, err)
		assert.Empty(t, drained)
	})

	t.Run("GetAll copies without consuming", func(t *testing.T) {
		q := newQueue(t)
		ctx := context.Background()
		require.NoError(t, q.Enqueue(ctx, userMsg("a"), convdomain.QueueSourceComposer, "r1"))

		all := q.GetAll()
		require.Len(t, all, 1)
		all[0].RequestID = "mutated"

		assert.Equal(t, "r1", q.GetAll()[0].RequestID)
		assert.False(t, q.IsEmpty(), "GetAll must not consume")
	})
}

// runMessageQueueRestartPersistence enqueues through one adapter and drains
// through a second one built afterwards over the same backend: a restart (or a
// second consumer) must keep order, source and request id. Only the durable
// adapters satisfy this, so only they call it.
func runMessageQueueRestartPersistence(t *testing.T, newQueue queueFactory) {
	t.Helper()

	t.Run("Restart persistence", func(t *testing.T) {
		ctx := context.Background()

		left := newQueue(t)
		require.NoError(t, left.Enqueue(ctx, userMsg("a"), convdomain.QueueSourceSubagent, "r1"))
		require.NoError(t, left.Enqueue(ctx, userMsg("b"), convdomain.QueueSourceShell, "r2"))

		right := newQueue(t)
		drained, err := right.Drain(ctx)
		require.NoError(t, err)
		require.Len(t, drained, 2)
		assert.Equal(t, []string{"r1", "r2"}, queuedRequestIDs(drained))
		assert.Equal(t, convdomain.QueueSourceSubagent, drained[0].Source)
		assert.Equal(t, convdomain.QueueSourceShell, drained[1].Source)
	})
}

// runMessageQueueDrainRace drains one shared queue from two adapters at once:
// no message may ever be delivered twice and the union of the drains is the
// whole queue. Run it under -race.
func runMessageQueueDrainRace(t *testing.T, newQueue queueFactory) {
	t.Helper()

	t.Run("Concurrent drains never duplicate", func(t *testing.T) {
		const total = 32
		ctx := context.Background()

		first, second := newQueue(t), newQueue(t)
		for i := range total {
			require.NoError(t, first.Enqueue(ctx, userMsg("m"), convdomain.QueueSourceJob, fmt.Sprintf("r%02d", i)))
		}

		drained := [2][]string{}
		var wg sync.WaitGroup
		wg.Go(func() {
			msgs, err := first.Drain(ctx)
			assert.NoError(t, err)
			drained[0] = queuedRequestIDs(msgs)
		})
		wg.Go(func() {
			msgs, err := second.Drain(ctx)
			assert.NoError(t, err)
			drained[1] = queuedRequestIDs(msgs)
		})
		wg.Wait()

		seen := make(map[string]bool, total)
		for _, ids := range drained {
			for _, id := range ids {
				assert.False(t, seen[id], "request %s was delivered twice", id)
				seen[id] = true
			}
		}
		assert.Len(t, seen, total, "the two drains together received every message")
	})
}

// queuedRequestIDs narrows drained messages to their request ids, in order.
func queuedRequestIDs(msgs []convdomain.QueuedMessage) []string {
	ids := make([]string, len(msgs))
	for i, msg := range msgs {
		ids[i] = msg.RequestID
	}
	return ids
}

// queuedContent extracts the text of a message a conformance case enqueued.
func queuedContent(t *testing.T, msg convdomain.QueuedMessage) string {
	t.Helper()
	content, err := msg.Message.Content.AsMessageContent0()
	require.NoError(t, err)
	return content
}

// sanitizeQueueScope turns a subtest name into a backend-safe queue scope.
func sanitizeQueueScope(name string) string {
	replacer := strings.NewReplacer("/", "_", " ", "_", "=", "")
	return strings.ToLower(replacer.Replace(name))
}

// newScopedTestQueue opens an adapter whose queue scope is unique per subtest
// but shared by every adapter the subtest builds, so restart and drain-race
// groups see one backend while groups stay isolated. prefix is a unique token
// the test function computes once. open dials the backend for one scope.
func newScopedTestQueue(t *testing.T, prefix string, open func(scope string) (convdomain.MessageQueue, error)) convdomain.MessageQueue {
	t.Helper()
	scope := prefix + "-" + sanitizeQueueScope(t.Name())
	q, err := open(scope)
	require.NoError(t, err)
	t.Cleanup(func() { q.(*durableMessageQueue).Stop() })
	return q
}