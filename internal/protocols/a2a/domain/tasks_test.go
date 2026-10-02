package domain

import (
	"testing"

	adk "github.com/inference-gateway/adk/types"
)

func TestNormalizeTaskState(t *testing.T) {
	tests := []struct {
		reported adk.TaskState
		want     adk.TaskState
	}{
		{adk.TaskStateCompleted, adk.TaskStateCompleted},
		{"completed", adk.TaskStateCompleted},
		{"Failed", adk.TaskStateFailed},
		{"input-required", adk.TaskStateInputRequired},
		{"input_required", adk.TaskStateInputRequired},
		{"TASK_STATE_CANCELLED", adk.TaskStateCanceled},
		{"cancelled", adk.TaskStateCanceled},
		{"canceled", adk.TaskStateCanceled},
		{"paused", "paused"},
	}
	for _, tt := range tests {
		t.Run(string(tt.reported), func(t *testing.T) {
			if got := NormalizeTaskState(tt.reported); got != tt.want {
				t.Errorf("NormalizeTaskState(%q) = %q, want %q", tt.reported, got, tt.want)
			}
		})
	}
}
