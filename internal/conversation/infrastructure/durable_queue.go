package infrastructure

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	sdk "github.com/inference-gateway/sdk"

	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
)

// queuedRecord is the wire format every durable backend stores: role, content,
// source, request_id and queued_at. External producers enqueue by writing this
// JSON, e.g. RPUSH infer:queue:<name> '<json>' on Redis.
type queuedRecord struct {
	Role      string          `json:"role"`
	Content   json.RawMessage `json:"content"`
	Source    string          `json:"source"`
	RequestID string          `json:"request_id"`
	QueuedAt  time.Time       `json:"queued_at"`

	// Seq is the backend-internal ordering hint (row id on SQL backends) and
	// stays out of the wire JSON.
	Seq int64 `json:"-"`
}

// queueBackend is the raw primitive a durable backing store provides. The
// wrapper adds the render snapshot, cross-process polling and failure tracking.
type queueBackend interface {
	enqueue(ctx context.Context, name string, record queuedRecord) error
	drain(ctx context.Context, name string) ([]queuedRecord, error)
	dequeue(ctx context.Context, name string) (*queuedRecord, error)
	clear(ctx context.Context, name string) error
	list(ctx context.Context, name string) ([]queuedRecord, error)
}

// durableMessageQueue is the orchestration shared by the sqlite, postgres and
// redis adapters: writes go straight to the backend and refresh a local
// snapshot, the render-path reads answer from the snapshot without a network
// call, and the poller notices entries pushed by other processes and wakes the
// idle agent through the wake callback.
type durableMessageQueue struct {
	backendName string
	backend     queueBackend
	name        func() string
	wake        func()

	interval time.Duration
	stop     chan struct{}
	stopOnce sync.Once
	done     chan struct{}

	mu          sync.RWMutex
	snapshot    []convdomain.QueuedMessage
	unreachable bool
}

// newDurableMessageQueue assembles the wrapper and starts its poller.
func newDurableMessageQueue(backendName string, backend queueBackend, name func() string, interval time.Duration, wake func()) *durableMessageQueue {
	q := &durableMessageQueue{
		backendName: backendName,
		backend:     backend,
		name:        name,
		wake:        wake,
		interval:    interval,
		stop:        make(chan struct{}),
		done:        make(chan struct{}),
	}
	go q.pollLoop()
	return q
}

// Stop ends the poller. Production runs never need it - the goroutine dies with
// the process; tests use it to stay hermetic.
func (q *durableMessageQueue) Stop() {
	q.stopOnce.Do(func() { close(q.stop) })
	<-q.done
}

// Enqueue appends a message to the tail of the backend queue, tagged with its
// source, and reflects it in the local snapshot.
func (q *durableMessageQueue) Enqueue(ctx context.Context, message sdk.Message, source convdomain.QueuedMessageSource, requestID string) error {
	record, err := toRecord(source, message, requestID)
	if err != nil {
		return fmt.Errorf("queue(%s) enqueue: %w", q.backendName, err)
	}

	if err := q.backend.enqueue(ctx, q.name(), record); err != nil {
		q.setUnreachable()
		return fmt.Errorf("queue(%s) enqueue: %w", q.backendName, err)
	}
	q.clearUnreachable()

	queued, _ := record.toQueued()
	q.mu.Lock()
	q.snapshot = append(q.snapshot, queued)
	q.mu.Unlock()
	return nil
}

// Drain atomically removes and returns every queued message, oldest first.
func (q *durableMessageQueue) Drain(ctx context.Context) ([]convdomain.QueuedMessage, error) {
	records, err := q.backend.drain(ctx, q.name())
	if err != nil {
		q.setUnreachable()
		return nil, fmt.Errorf("queue(%s) drain: %w", q.backendName, err)
	}
	q.clearUnreachable()

	queued, decodeErr := toQueuedMessages(records)
	q.resetSnapshot()
	return queued, decodeErr
}

// Dequeue removes and returns the head message, or nil when the queue is empty.
func (q *durableMessageQueue) Dequeue(ctx context.Context) (*convdomain.QueuedMessage, error) {
	record, err := q.backend.dequeue(ctx, q.name())
	if err != nil {
		q.setUnreachable()
		return nil, fmt.Errorf("queue(%s) dequeue: %w", q.backendName, err)
	}
	q.clearUnreachable()
	if record == nil {
		return nil, nil
	}

	queued, decodeErr := record.toQueued()
	if decodeErr != nil {
		return nil, fmt.Errorf("queue(%s) dequeue: %w", q.backendName, decodeErr)
	}
	q.dropSnapshotHead()
	return &queued, nil
}

// Clear removes all messages from the backend queue.
func (q *durableMessageQueue) Clear(ctx context.Context) error {
	if err := q.backend.clear(ctx, q.name()); err != nil {
		q.setUnreachable()
		return fmt.Errorf("queue(%s) clear: %w", q.backendName, err)
	}
	q.clearUnreachable()
	q.resetSnapshot()
	return nil
}

// Size returns the number of queued messages, answered from the local snapshot.
func (q *durableMessageQueue) Size() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return len(q.snapshot)
}

// IsEmpty reports whether the queue holds no messages, from the local snapshot.
func (q *durableMessageQueue) IsEmpty() bool {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return len(q.snapshot) == 0
}

// GetAll returns a copy of the queued messages, oldest first, from the snapshot.
func (q *durableMessageQueue) GetAll() []convdomain.QueuedMessage {
	q.mu.RLock()
	defer q.mu.RUnlock()

	result := make([]convdomain.QueuedMessage, len(q.snapshot))
	copy(result, q.snapshot)
	return result
}

// BackendName names the active backend for the status bar indicator.
func (q *durableMessageQueue) BackendName() string {
	return q.backendName
}

// Unreachable reports whether the backend failed its latest operation.
func (q *durableMessageQueue) Unreachable() bool {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.unreachable
}

// setUnreachable marks the backend as failing after a store error.
func (q *durableMessageQueue) setUnreachable() {
	q.mu.Lock()
	q.unreachable = true
	q.mu.Unlock()
}

// clearUnreachable drops the failure mark after a successful store operation.
func (q *durableMessageQueue) clearUnreachable() {
	q.mu.Lock()
	q.unreachable = false
	q.mu.Unlock()
}

func (q *durableMessageQueue) resetSnapshot() {
	q.mu.Lock()
	q.snapshot = nil
	q.mu.Unlock()
}

func (q *durableMessageQueue) dropSnapshotHead() {
	q.mu.Lock()
	if len(q.snapshot) > 0 {
		q.snapshot = q.snapshot[1:]
	}
	q.mu.Unlock()
}

// pollLoop refreshes the local snapshot from the backend every interval.
func (q *durableMessageQueue) pollLoop() {
	defer close(q.done)

	ticker := time.NewTicker(q.interval)
	defer ticker.Stop()
	for {
		select {
		case <-q.stop:
			return
		case <-ticker.C:
			q.pollOnce()
		}
	}
}

// pollOnce syncs the snapshot from the backend and wakes the idle agent when a
// remote producer pushed new entries. Decode failures of single records are
// ignored here so one malformed payload written by a producer cannot flip the
// backend into unreachable or wake-storm the agent.
func (q *durableMessageQueue) pollOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), q.interval)
	defer cancel()

	records, err := q.backend.list(ctx, q.name())
	if err != nil {
		q.setUnreachable()
		return
	}
	q.clearUnreachable()

	synced, _ := toQueuedMessages(records)

	q.mu.Lock()
	unchanged := sameQueueContent(synced, q.snapshot)
	q.snapshot = synced
	q.mu.Unlock()
	if unchanged || len(synced) == 0 || q.wake == nil {
		return
	}
	q.wake()
}

// toRecord converts a domain message into its stored wire format.
func toRecord(source convdomain.QueuedMessageSource, message sdk.Message, requestID string) (queuedRecord, error) {
	content, err := json.Marshal(message.Content)
	if err != nil {
		return queuedRecord{}, fmt.Errorf("encoding message content: %w", err)
	}
	return queuedRecord{
		Role:      string(message.Role),
		Content:   content,
		Source:    string(source),
		RequestID: requestID,
		QueuedAt:  time.Now(),
	}, nil
}

// toQueued decodes a stored record back into a domain message.
func (r queuedRecord) toQueued() (convdomain.QueuedMessage, error) {
	content, err := decodeContent(r.Content)
	if err != nil {
		return convdomain.QueuedMessage{}, fmt.Errorf("decoding queued message content of %q: %w", r.RequestID, err)
	}
	return convdomain.QueuedMessage{
		Message:   sdk.Message{Role: sdk.MessageRole(r.Role), Content: content},
		QueuedAt:  r.QueuedAt,
		RequestID: r.RequestID,
		Source:    convdomain.QueuedMessageSource(r.Source),
	}, nil
}

// decodeRecord parses one stored payload.
func decodeRecord(payload []byte) (queuedRecord, error) {
	var record queuedRecord
	if err := json.Unmarshal(payload, &record); err != nil {
		return queuedRecord{}, fmt.Errorf("decoding queued record: %w", err)
	}
	return record, nil
}

// decodeContent turns the stored content JSON back into SDK message content.
func decodeContent(raw json.RawMessage) (sdk.MessageContent, error) {
	content := sdk.NewMessageContent("")
	body := strings.TrimSpace(string(raw))
	if body == "" || body == "null" {
		return content, nil
	}
	if err := json.Unmarshal(raw, &content); err != nil {
		return sdk.MessageContent{}, fmt.Errorf("raw content %s: %w", body, err)
	}
	return content, nil
}

// toQueuedMessages decodes every record oldest first, dropping malformed
// entries so one bad payload cannot wedge a drain or the poller.
func toQueuedMessages(records []queuedRecord) ([]convdomain.QueuedMessage, error) {
	slices.SortStableFunc(records, func(a, b queuedRecord) int { return cmp.Compare(a.Seq, b.Seq) })

	queued := make([]convdomain.QueuedMessage, 0, len(records))
	var decodeErrs []error
	for _, record := range records {
		entry, err := record.toQueued()
		if err != nil {
			decodeErrs = append(decodeErrs, err)
			continue
		}
		queued = append(queued, entry)
	}
	return queued, errors.Join(decodeErrs...)
}

// sameQueueContent cheaply tells whether a fresh backend snapshot matches the
// rendered snapshot (identity by request id, source and queue time).
func sameQueueContent(a, b []convdomain.QueuedMessage) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].RequestID != b[i].RequestID || a[i].Source != b[i].Source || !a[i].QueuedAt.Equal(b[i].QueuedAt) {
			return false
		}
	}
	return true
}
