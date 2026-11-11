package domain

import (
	"testing"
	"time"
)

func TestStampTodos(t *testing.T) {
	start := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	at := func(minutes int) time.Time { return start.Add(time.Duration(minutes) * time.Minute) }
	ptr := func(minutes int) *time.Time { stamp := at(minutes); return &stamp }
	const minute = int64(time.Minute / time.Millisecond)

	tests := []struct {
		name     string
		previous []TodoItem
		next     []TodoItem
		now      time.Time
		want     TodoItem
	}{
		{
			name: "a new pending item has no times",
			next: []TodoItem{{ID: "1", Status: "pending"}},
			now:  at(0),
			want: TodoItem{ID: "1", Status: "pending"},
		},
		{
			name: "an item that starts is stamped",
			next: []TodoItem{{ID: "1", Status: "in_progress"}},
			now:  at(1),
			want: TodoItem{ID: "1", Status: "in_progress", StartedAt: ptr(1)},
		},
		{
			name:     "an item still in progress keeps its start",
			previous: []TodoItem{{ID: "1", Status: "in_progress", StartedAt: ptr(1)}},
			next:     []TodoItem{{ID: "1", Status: "in_progress"}},
			now:      at(3),
			want:     TodoItem{ID: "1", Status: "in_progress", StartedAt: ptr(1)},
		},
		{
			name:     "an item that completes adds up its stretch",
			previous: []TodoItem{{ID: "1", Status: "in_progress", StartedAt: ptr(1)}},
			next:     []TodoItem{{ID: "1", Status: "completed"}},
			now:      at(5),
			want:     TodoItem{ID: "1", Status: "completed", CompletedAt: ptr(5), ElapsedMs: 4 * minute},
		},
		{
			name:     "a completed item keeps its times",
			previous: []TodoItem{{ID: "1", Status: "completed", CompletedAt: ptr(5), ElapsedMs: 4 * minute}},
			next:     []TodoItem{{ID: "1", Status: "completed"}},
			now:      at(9),
			want:     TodoItem{ID: "1", Status: "completed", CompletedAt: ptr(5), ElapsedMs: 4 * minute},
		},
		{
			name:     "an item put back to pending keeps the time it took",
			previous: []TodoItem{{ID: "1", Status: "in_progress", StartedAt: ptr(1)}},
			next:     []TodoItem{{ID: "1", Status: "pending"}},
			now:      at(3),
			want:     TodoItem{ID: "1", Status: "pending", ElapsedMs: 2 * minute},
		},
		{
			name:     "an item started again adds to the time it took",
			previous: []TodoItem{{ID: "1", Status: "pending", ElapsedMs: 2 * minute}},
			next:     []TodoItem{{ID: "1", Status: "in_progress"}},
			now:      at(6),
			want:     TodoItem{ID: "1", Status: "in_progress", StartedAt: ptr(6), ElapsedMs: 2 * minute},
		},
		{
			name:     "a completed item that is reopened is no longer completed",
			previous: []TodoItem{{ID: "1", Status: "completed", CompletedAt: ptr(5), ElapsedMs: 4 * minute}},
			next:     []TodoItem{{ID: "1", Status: "in_progress"}},
			now:      at(7),
			want:     TodoItem{ID: "1", Status: "in_progress", StartedAt: ptr(7), ElapsedMs: 4 * minute},
		},
		{
			name: "an item completed without ever being in progress took no time",
			next: []TodoItem{{ID: "1", Status: "completed"}},
			now:  at(2),
			want: TodoItem{ID: "1", Status: "completed", CompletedAt: ptr(2)},
		},
		{
			name:     "the times a caller supplies are ignored",
			previous: []TodoItem{{ID: "1", Status: "in_progress", StartedAt: ptr(1)}},
			next:     []TodoItem{{ID: "1", Status: "completed", CompletedAt: ptr(0), ElapsedMs: 99 * minute}},
			now:      at(2),
			want:     TodoItem{ID: "1", Status: "completed", CompletedAt: ptr(2), ElapsedMs: minute},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StampTodos(tt.previous, tt.next, tt.now)[0]
			if got.ID != tt.want.ID || got.Status != tt.want.Status || got.ElapsedMs != tt.want.ElapsedMs ||
				!sameTime(got.StartedAt, tt.want.StartedAt) || !sameTime(got.CompletedAt, tt.want.CompletedAt) {
				t.Errorf("StampTodos() = %+v, want %+v", describe(got), describe(tt.want))
			}
		})
	}
}

func TestTodoItemElapsedAtCountsTheRunningStretch(t *testing.T) {
	started := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	item := TodoItem{Status: "in_progress", StartedAt: &started, ElapsedMs: 60_000}
	if got, want := item.ElapsedAt(started.Add(30*time.Second)), 90*time.Second; got != want {
		t.Errorf("ElapsedAt() = %v, want %v", got, want)
	}
	done := TodoItem{Status: "completed", ElapsedMs: 60_000}
	if got, want := done.ElapsedAt(started.Add(time.Hour)), time.Minute; got != want {
		t.Errorf("ElapsedAt() of a completed item = %v, want %v", got, want)
	}
}

func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

func describe(item TodoItem) map[string]any {
	out := map[string]any{"id": item.ID, "status": item.Status, "elapsed_ms": item.ElapsedMs}
	if item.StartedAt != nil {
		out["started_at"] = item.StartedAt.Format(time.RFC3339)
	}
	if item.CompletedAt != nil {
		out["completed_at"] = item.CompletedAt.Format(time.RFC3339)
	}
	return out
}
