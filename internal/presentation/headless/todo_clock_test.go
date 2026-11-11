package headless

import (
	"testing"
	"time"

	assert "github.com/stretchr/testify/assert"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

func TestTodoClockSplitsARunBetweenItsTodos(t *testing.T) {
	start := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	at := func(seconds int) time.Time { return start.Add(time.Duration(seconds) * time.Second) }
	list := func(first, second string) []agentdomain.TodoItem {
		return []agentdomain.TodoItem{{ID: "1", Content: "stabilise", Status: first}, {ID: "2", Content: "export", Status: second}}
	}

	clock := newTodoClock(at(0))
	clock.observe(at(10), list("in_progress", "pending"))
	clock.observe(at(70), list("completed", "pending"))
	clock.observe(at(75), list("completed", "in_progress"))
	clock.observe(at(195), list("in_progress", "pending"))
	clock.observe(at(200), list("completed", "completed"))

	assert.Equal(t, map[string]any{
		"runMs":     int64(210_000),
		"outsideMs": int64(25_000),
		"todos": []any{
			map[string]any{"id": "1", "content": "stabilise", "ms": int64(65_000)},
			map[string]any{"id": "2", "content": "export", "ms": int64(120_000)},
		},
	}, clock.summary(at(210)))
}

func TestTodoClockCountsATodoStillInProgressToTheEndOfTheRun(t *testing.T) {
	start := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	clock := newTodoClock(start)
	clock.observe(start.Add(2*time.Second), []agentdomain.TodoItem{{ID: "1", Content: "grade", Status: "in_progress"}})

	summary := clock.summary(start.Add(10 * time.Second))
	assert.Equal(t, int64(2_000), summary["outsideMs"])
	assert.Equal(t, []any{map[string]any{"id": "1", "content": "grade", "ms": int64(8_000)}}, summary["todos"])
}

func TestTodoClockOfARunWithoutTodosReportsNothing(t *testing.T) {
	start := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	assert.Nil(t, newTodoClock(start).summary(start.Add(time.Minute)))
}
