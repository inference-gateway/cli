package domain

import "time"

const (
	todoStatusInProgress = "in_progress"
	todoStatusCompleted  = "completed"
)

// StampTodos returns next with the times of a session's todo list: each item
// keeps what previous recorded for its id, and a status that changed is
// stamped at now. The times of next itself are ignored, so only the session
// decides them.
func StampTodos(previous, next []TodoItem, now time.Time) []TodoItem {
	recorded := make(map[string]TodoItem, len(previous))
	for _, item := range previous {
		recorded[item.ID] = item
	}
	stamped := make([]TodoItem, len(next))
	for i, item := range next {
		stamped[i] = item.stampedAfter(recorded[item.ID], now)
	}
	return stamped
}

// stampedAfter is the item with the times it has once it follows before, the
// same item as the session last saw it, or the zero item when it is new.
func (t TodoItem) stampedAfter(before TodoItem, now time.Time) TodoItem {
	t.StartedAt, t.CompletedAt, t.ElapsedMs = nil, before.CompletedAt, before.ElapsedAt(now).Milliseconds()
	if t.Status == todoStatusInProgress {
		t.ElapsedMs = before.ElapsedMs
		t.StartedAt = before.StartedAt
		if t.StartedAt == nil {
			t.StartedAt = &now
		}
	}
	switch {
	case t.Status != todoStatusCompleted:
		t.CompletedAt = nil
	case t.CompletedAt == nil:
		t.CompletedAt = &now
	}
	return t
}

// ElapsedAt is the time the item has spent in progress by now, the stretch it
// is in progress for included.
func (t TodoItem) ElapsedAt(now time.Time) time.Duration {
	elapsed := time.Duration(t.ElapsedMs) * time.Millisecond
	if t.StartedAt != nil && now.After(*t.StartedAt) {
		elapsed += now.Sub(*t.StartedAt)
	}
	return elapsed
}
