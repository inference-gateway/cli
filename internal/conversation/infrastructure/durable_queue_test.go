package infrastructure

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
)

// stubQueueBackend is a hand-rolled queueBackend the wrapper tests drive: it
// keeps one shared bucket, counts its list reads and can be made to fail a
// chosen operation.
type stubQueueBackend struct {
	mu      sync.Mutex
	records []queuedRecord
	lists   atomic.Int32
	failure atomic.Pointer[stubBackendFailure]
}

type stubBackendFailure struct{ op string }

func (b *stubQueueBackend) failWith(op string) { b.failure.Store(&stubBackendFailure{op}) }
func (b *stubQueueBackend) clearFailure()      { b.failure.Store(nil) }
func (b *stubQueueBackend) failed(op string) bool {
	if fail := b.failure.Load(); fail != nil {
		return fail.op == op
	}
	return false
}

func (b *stubQueueBackend) enqueue(_ context.Context, _ string, record queuedRecord) error {
	if b.failed("enqueue") {
		return errors.New("backend down")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	record.Seq = int64(len(b.records) + 1)
	b.records = append(b.records, record)
	return nil
}

func (b *stubQueueBackend) drain(_ context.Context, _ string) ([]queuedRecord, error) {
	if b.failed("drain") {
		return nil, errors.New("backend down")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	records := b.records
	b.records = nil
	return records, nil
}

func (b *stubQueueBackend) dequeue(_ context.Context, _ string) (*queuedRecord, error) {
	if b.failed("dequeue") {
		return nil, errors.New("backend down")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.records) == 0 {
		return nil, nil
	}
	record := b.records[0]
	b.records = b.records[1:]
	return &record, nil
}

func (b *stubQueueBackend) clear(_ context.Context, _ string) error {
	if b.failed("clear") {
		return errors.New("backend down")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.records = nil
	return nil
}

func (b *stubQueueBackend) list(_ context.Context, _ string) ([]queuedRecord, error) {
	b.lists.Add(1)
	b.mu.Lock()
	defer b.mu.Unlock()
	records := make([]queuedRecord, len(b.records))
	copy(records, b.records)
	return records, nil
}

// injectRemote plants records as if another process had written them.
func (b *stubQueueBackend) injectRemote(records ...queuedRecord) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range records {
		records[i].Seq = int64(len(b.records) + i + 1)
	}
	b.records = append(b.records, records...)
}

// newXlatedQueue builds a durable wrapper over a stub at an interval no tick
// reaches during a test, stopping its poller at cleanup. wake counts its calls.
func newTestDurableQueue(t *testing.T, backend queueBackend) (*durableMessageQueue, *atomic.Int32) {
	t.Helper()
	var wakeCalls atomic.Int32
	q := newDurableMessageQueue("redis", backend, func() string { return "shared" }, time.Hour, func() {
		wakeCalls.Add(1)
	})
	t.Cleanup(q.Stop)
	return q, &wakeCalls
}

func TestDurableQueue_RenderReadsStayLocal(t *testing.T) {
	backend := &stubQueueBackend{}
	q, _ := newTestDurableQueue(t, backend)
	ctx := context.Background()

	require.NoError(t, q.Enqueue(ctx, userMsg("a"), convdomain.QueueSourceComposer, "r1"))

	assert.Equal(t, 1, q.Size())
	assert.False(t, q.IsEmpty())
	assert.Len(t, q.GetAll(), 1)
	assert.Zero(t, backend.lists.Load(), "render reads must not reach the backend")

	q.pollOnce()
	assert.Equal(t, int32(1), backend.lists.Load(), "polling lists once")

	assert.Equal(t, 1, q.Size())
	assert.Equal(t, int32(1), backend.lists.Load())
}

func TestDurableQueue_PollSyncsSnapshotAndWakes(t *testing.T) {
	backend := &stubQueueBackend{}
	q, wakeCalls := newTestDurableQueue(t, backend)
	backend.injectRemote(queuedRecord{Role: "user", Content: []byte(`"hello"`), Source: "a2a", RequestID: "r1"})

	q.pollOnce()

	assert.Equal(t, int32(1), wakeCalls.Load(), "a remote entry wakes the agent")
	assert.Len(t, q.GetAll(), 1)

	q.pollOnce()

	assert.Equal(t, int32(1), wakeCalls.Load(), "an unchanged snapshot does not wake again")
}

func TestDurableQueue_PollSkipsWakeWhenEmpty(t *testing.T) {
	backend := &stubQueueBackend{}
	q, wakeCalls := newTestDurableQueue(t, backend)

	q.pollOnce()

	assert.Zero(t, wakeCalls.Load(), "an empty snapshot cannot wake the agent")
}

func TestDurableQueue_PollIgnoresMalformedRecords(t *testing.T) {
	backend := &stubQueueBackend{}
	q, wakeCalls := newTestDurableQueue(t, backend)
	backend.injectRemote(
		queuedRecord{Role: "user", Content: []byte("not-json"), Source: "a2a", RequestID: "bad"},
		queuedRecord{Role: "user", Content: []byte(`"hello"`), Source: "a2a", RequestID: "good"},
	)

	q.pollOnce()

	assert.False(t, q.Unreachable(), "one malformed record is not a backend failure")
	assert.Equal(t, int32(1), wakeCalls.Load(), "the good record still wakes the agent")
	all := q.GetAll()
	require.Len(t, all, 1)
	assert.Equal(t, "good", all[0].RequestID)
}

func TestDurableQueue_BackendFailureMarksUnreachableAndRecovers(t *testing.T) {
	backend := &stubQueueBackend{}
	q, _ := newTestDurableQueue(t, backend)
	ctx := context.Background()

	backend.failWith("enqueue")
	err := q.Enqueue(ctx, userMsg("a"), convdomain.QueueSourceComposer, "r1")
	require.Error(t, err)
	assert.ErrorContains(t, err, "queue(redis) enqueue")
	assert.True(t, q.Unreachable())

	backend.clearFailure()
	require.NoError(t, q.Enqueue(ctx, userMsg("a"), convdomain.QueueSourceComposer, "r1"))
	assert.False(t, q.Unreachable())
}

func TestDurableQueue_DrainResetsSnapshot(t *testing.T) {
	backend := &stubQueueBackend{}
	q, _ := newTestDurableQueue(t, backend)
	ctx := context.Background()

	require.NoError(t, q.Enqueue(ctx, userMsg("a"), convdomain.QueueSourceComposer, "r1"))

	drained, err := q.Drain(ctx)
	require.NoError(t, err)
	require.Len(t, drained, 1)
	assert.True(t, q.IsEmpty(), "the snapshot resets after a drain")

	again, err := q.Drain(ctx)
	assert.NoError(t, err)
	assert.Empty(t, again)
}

func TestDurableQueue_OwnEnqueueDoesNotWakeOnPoll(t *testing.T) {
	backend := &stubQueueBackend{}
	q, wakeCalls := newTestDurableQueue(t, backend)
	ctx := context.Background()

	require.NoError(t, q.Enqueue(ctx, userMsg("a"), convdomain.QueueSourceComposer, "r1"))
	q.pollOnce()

	assert.Zero(t, wakeCalls.Load(), "the wrapper must not wake on its own writes")
}

func TestDurableQueue_DequeueDropsSnapshotHead(t *testing.T) {
	backend := &stubQueueBackend{}
	q, _ := newTestDurableQueue(t, backend)
	ctx := context.Background()

	require.NoError(t, q.Enqueue(ctx, userMsg("a"), convdomain.QueueSourceComposer, "r1"))
	require.NoError(t, q.Enqueue(ctx, userMsg("b"), convdomain.QueueSourceComposer, "r2"))

	head, err := q.Dequeue(ctx)
	require.NoError(t, err)
	require.NotNil(t, head)
	assert.Equal(t, "r1", head.RequestID)
	assert.Equal(t, 1, q.Size())
}

func TestDurableQueue_BackendNameAndStop(t *testing.T) {
	q, _ := newTestDurableQueue(t, &stubQueueBackend{})
	assert.Equal(t, "redis", q.BackendName())
	q.Stop()
	q.Stop()
	assert.Equal(t, "redis", q.BackendName(), "reads after a stop stay safe")
}