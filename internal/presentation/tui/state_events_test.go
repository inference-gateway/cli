package tui

import (
	"testing"
	"time"
)

func TestTUIEventAccessors(t *testing.T) {
	id := "req-1"
	ts := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	events := []interface {
		GetRequestID() string
		GetTimestamp() time.Time
	}{
		ToolCallPreviewEvent{RequestID: id, Timestamp: ts},
		ToolApprovalNotificationEvent{RequestID: id, Timestamp: ts},
		NavigateBackInTimeEvent{RequestID: id, Timestamp: ts},
		MessageHistoryRestoreEvent{RequestID: id, Timestamp: ts},
		MessageEditSubmitEvent{RequestID: id, Timestamp: ts},
	}
	for _, e := range events {
		if e.GetRequestID() != id || !e.GetTimestamp().Equal(ts) {
			t.Errorf("%T accessors: got (%q, %v), want (%q, %v)", e, e.GetRequestID(), e.GetTimestamp(), id, ts)
		}
	}
}
