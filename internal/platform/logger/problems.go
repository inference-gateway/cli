package logger

import (
	"sync"
	"time"

	zapcore "go.uber.org/zap/zapcore"
)

// maxRecentProblems bounds the warnings and errors kept in memory.
const maxRecentProblems = 256

// Problem is one warning or error this process logged, reduced to what a
// reader needs: no caller, no stack trace and no fields but the error itself
// and the conversation it belongs to.
type Problem struct {
	Time           time.Time
	Severity       string
	Message        string
	Detail         string
	ConversationID string
}

// problemRing keeps the most recent problems, dropping the oldest.
type problemRing struct {
	mu       sync.Mutex
	problems []Problem
}

var recentProblems = &problemRing{}

func (r *problemRing) add(problem Problem) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.problems) == maxRecentProblems {
		r.problems = r.problems[1:]
	}
	r.problems = append(r.problems, problem)
}

func (r *problemRing) since(moment time.Time) []Problem {
	r.mu.Lock()
	defer r.mu.Unlock()
	var recent []Problem
	for _, problem := range r.problems {
		if !problem.Time.Before(moment) {
			recent = append(recent, problem)
		}
	}
	return recent
}

// ProblemsSince lists the warnings and errors this process logged from moment
// on, oldest first. It reaches back at most maxRecentProblems entries.
func ProblemsSince(moment time.Time) []Problem {
	return recentProblems.since(moment)
}

// problemCore is the zap core that records warnings and errors into a ring,
// next to wherever the log is written.
type problemCore struct {
	ring   *problemRing
	fields []zapcore.Field
}

func (c *problemCore) Enabled(level zapcore.Level) bool {
	return level >= zapcore.WarnLevel
}

func (c *problemCore) With(fields []zapcore.Field) zapcore.Core {
	return &problemCore{ring: c.ring, fields: append(c.fields[:len(c.fields):len(c.fields)], fields...)}
}

func (c *problemCore) Check(entry zapcore.Entry, checked *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(entry.Level) {
		return checked.AddCore(entry, c)
	}
	return checked
}

func (c *problemCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	encoded := zapcore.NewMapObjectEncoder()
	for _, field := range c.fields {
		field.AddTo(encoded)
	}
	for _, field := range fields {
		field.AddTo(encoded)
	}
	c.ring.add(Problem{
		Time:           entry.Time,
		Severity:       entry.Level.String(),
		Message:        entry.Message,
		Detail:         textOf(encoded.Fields["error"]),
		ConversationID: textOf(encoded.Fields["conversation_id"]),
	})
	return nil
}

func (c *problemCore) Sync() error {
	return nil
}

func textOf(value any) string {
	text, _ := value.(string)
	return text
}

// recordingProblems tees a core with the recorder of recent problems.
func recordingProblems(core zapcore.Core) zapcore.Core {
	return zapcore.NewTee(core, &problemCore{ring: recentProblems})
}
