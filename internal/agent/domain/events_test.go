package domain

import (
	"testing"
	"time"
)

type requestEvent interface {
	GetRequestID() string
	GetTimestamp() time.Time
}

// One table covering every event type's GetRequestID/GetTimestamp accessors.
func TestEventAccessors(t *testing.T) {
	id := "req-1"
	ts := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	events := []requestEvent{
		ChatStartEvent{RequestID: id, Timestamp: ts},
		ChatChunkEvent{RequestID: id, Timestamp: ts},
		ChatCompleteEvent{RequestID: id, Timestamp: ts},
		ChatErrorEvent{RequestID: id, Timestamp: ts},
		ToolCallPreviewEvent{RequestID: id, Timestamp: ts},
		ToolCallUpdateEvent{RequestID: id, Timestamp: ts},
		ToolCallReadyEvent{RequestID: id, Timestamp: ts},
		OptimizationStatusEvent{RequestID: id, Timestamp: ts},
		A2AToolCallExecutedEvent{RequestID: id, Timestamp: ts},
		A2ATaskSubmittedEvent{RequestID: id, Timestamp: ts},
		A2ATaskStatusUpdateEvent{RequestID: id, Timestamp: ts},
		A2ATaskCompletedEvent{RequestID: id, Timestamp: ts},
		A2ATaskFailedEvent{RequestID: id, Timestamp: ts},
		A2ATaskInputRequiredEvent{RequestID: id, Timestamp: ts},
		SubagentSubmittedEvent{RequestID: id, Timestamp: ts},
		SubagentCompletedEvent{RequestID: id, Timestamp: ts},
		SubagentFailedEvent{RequestID: id, Timestamp: ts},
		MessageQueuedEvent{RequestID: id, Timestamp: ts},
		ToolApprovalRequestedEvent{RequestID: id, Timestamp: ts},
		ToolApprovalResolvedEvent{RequestID: id, Timestamp: ts},
		ToolCancelledEvent{RequestID: id, Timestamp: ts},
		ComputerUsePausedEvent{RequestID: id, Timestamp: ts},
		ComputerUseResumedEvent{RequestID: id, Timestamp: ts},
		ToolApprovalNotificationEvent{RequestID: id, Timestamp: ts},
		PlanApprovalRequestedEvent{RequestID: id, Timestamp: ts},
		UserQuestionRequestedEvent{RequestID: id, Timestamp: ts},
		ShellDetachedEvent{RequestID: id, Timestamp: ts},
		ShellCompletedEvent{RequestID: id, Timestamp: ts},
		ShellFailedEvent{RequestID: id, Timestamp: ts},
		ShellCancelledEvent{RequestID: id, Timestamp: ts},
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
