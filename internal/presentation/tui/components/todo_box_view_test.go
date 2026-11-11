package components

import (
	"testing"
	"time"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

func TestTodoElapsed(t *testing.T) {
	started := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	now := started.Add(90 * time.Second)
	tests := []struct {
		name string
		todo agentdomain.TodoItem
		want string
	}{
		{"pending and never started", agentdomain.TodoItem{Status: "pending"}, ""},
		{"completed", agentdomain.TodoItem{Status: "completed", ElapsedMs: 230_400}, "3m50s"},
		{"in progress counts up to now", agentdomain.TodoItem{Status: "in_progress", StartedAt: &started}, "1m30s"},
		{"in progress again adds to the earlier stretch", agentdomain.TodoItem{Status: "in_progress", StartedAt: &started, ElapsedMs: 30_000}, "2m0s"},
		{"under half a second shows nothing", agentdomain.TodoItem{Status: "completed", ElapsedMs: 400}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := todoElapsed(tt.todo, now); got != tt.want {
				t.Errorf("todoElapsed() = %q, want %q", got, tt.want)
			}
		})
	}
}
