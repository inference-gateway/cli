package headless

import (
	"time"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

const todoInProgress = "in_progress"

// todoClock attributes a run's wall time to the todo that was in progress at
// each moment, and to no todo for the rest, so the parts add up to the run.
type todoClock struct {
	started time.Time
	last    time.Time
	active  string
	order   []string
	content map[string]string
	spent   map[string]time.Duration
	outside time.Duration
}

func newTodoClock(now time.Time) *todoClock {
	return &todoClock{started: now, last: now, content: map[string]string{}, spent: map[string]time.Duration{}}
}

// observe closes the stretch that ran until now and follows the todo the new
// list has in progress.
func (c *todoClock) observe(now time.Time, todos []agentdomain.TodoItem) {
	c.advance(now)
	c.active = ""
	for _, todo := range todos {
		if _, known := c.content[todo.ID]; !known {
			c.order = append(c.order, todo.ID)
		}
		c.content[todo.ID] = todo.Content
		if todo.Status == todoInProgress {
			c.active = todo.ID
		}
	}
}

func (c *todoClock) advance(now time.Time) {
	stretch := now.Sub(c.last)
	c.last = now
	if c.active == "" {
		c.outside += stretch
		return
	}
	c.spent[c.active] += stretch
}

// summary is the run's time per todo as its terminal event reports it, or nil
// for a run that never had a todo list.
func (c *todoClock) summary(now time.Time) map[string]any {
	if len(c.order) == 0 {
		return nil
	}
	c.advance(now)
	todos := make([]any, 0, len(c.order))
	for _, id := range c.order {
		todos = append(todos, map[string]any{"id": id, "content": c.content[id], "ms": c.spent[id].Milliseconds()})
	}
	return map[string]any{
		"runMs":     now.Sub(c.started).Milliseconds(),
		"outsideMs": c.outside.Milliseconds(),
		"todos":     todos,
	}
}
