package domain

import (
	"fmt"
	"slices"
	"strings"
	"time"

	uuid "github.com/google/uuid"
	cron "github.com/robfig/cron/v3"
)

// CronParser accepts the schedules a ScheduledJob may carry: standard 5-field
// expressions and @descriptors. The scheduler runs jobs with the same parser.
var CronParser = cron.NewParser(
	cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
)

// ParseCron reports whether expr is a valid job schedule.
func ParseCron(expr string) error {
	_, err := CronParser.Parse(expr)
	return err
}

// ValidJobID reports whether id is safe to use as a single file-name
// component. Job IDs are UUIDs at creation, but they also arrive as raw LLM
// tool arguments and become file paths (schedules dir, workflow files), so
// anything containing path separators or dot-segments is rejected to prevent
// path traversal.
func ValidJobID(id string) bool {
	return id != "" && id != "." && id != ".." && !strings.ContainsAny(id, `/\`)
}

// ScheduledJob describes a task that the LLM has asked the system to run on a
// cron schedule. Jobs are persisted through the configured storage backend and
// executed by the scheduler running inside the `infer daemon` process.
//
// Each fire spawns a fresh `infer headless` subprocess with its own session
// ID - no context is carried between fires. Channel/RecipientID are an
// optional delivery target: when set, run output is forwarded to that channel;
// when empty, the run is record-only and its output lives in storage (run
// record + conversation).
type ScheduledJob struct {
	ID             string     `yaml:"id" json:"id"`
	Name           string     `yaml:"name,omitempty" json:"name,omitempty"`
	Description    string     `yaml:"description,omitempty" json:"description,omitempty"`
	CronExpression string     `yaml:"cron_expression" json:"cron_expression"`
	Prompt         string     `yaml:"prompt" json:"prompt"`
	Channel        string     `yaml:"channel,omitempty" json:"channel,omitempty"`
	RecipientID    string     `yaml:"recipient_id,omitempty" json:"recipient_id,omitempty"`
	Model          string     `yaml:"model,omitempty" json:"model,omitempty"`
	RunOnce        bool       `yaml:"run_once,omitempty" json:"run_once,omitempty"`
	CreatedAt      time.Time  `yaml:"created_at" json:"created_at"`
	UpdatedAt      time.Time  `yaml:"updated_at" json:"updated_at"`
	LastRun        *time.Time `yaml:"last_run,omitempty" json:"last_run,omitempty"`
	LastError      string     `yaml:"last_error,omitempty" json:"last_error,omitempty"`
}

// NewScheduledJob returns spec as a new job: a fresh ID and creation stamp,
// with the schedule validated.
func NewScheduledJob(spec ScheduledJob, now time.Time) (*ScheduledJob, error) {
	if err := ParseCron(spec.CronExpression); err != nil {
		return nil, fmt.Errorf("invalid cron_expression: %w", err)
	}
	spec.ID = uuid.New().String()
	spec.CreatedAt = now
	spec.UpdatedAt = now
	return &spec, nil
}

// JobPatch carries edits to a ScheduledJob; a nil field is left unchanged.
type JobPatch struct {
	Name           *string
	Description    *string
	CronExpression *string
	Prompt         *string
	Model          *string
	RunOnce        *bool
}

// Apply validates and applies p, stamping UpdatedAt. It reports false (and
// changes nothing) when p carries no edits.
func (j *ScheduledJob) Apply(p JobPatch, now time.Time) (bool, error) {
	if p.CronExpression != nil {
		if err := ParseCron(*p.CronExpression); err != nil {
			return false, fmt.Errorf("invalid cron_expression: %w", err)
		}
	}
	changed := slices.Contains([]bool{
		set(&j.Name, p.Name),
		set(&j.Description, p.Description),
		set(&j.CronExpression, p.CronExpression),
		set(&j.Prompt, p.Prompt),
		set(&j.Model, p.Model),
		set(&j.RunOnce, p.RunOnce),
	}, true)
	if changed {
		j.UpdatedAt = now
	}
	return changed, nil
}

// set copies *src into *dst when src is non-nil and reports whether it did.
func set[T any](dst, src *T) bool {
	if src == nil {
		return false
	}
	*dst = *src
	return true
}

// RunStatus is the lifecycle state of a single scheduled-job run.
type RunStatus string

const (
	RunStatusRunning   RunStatus = "running"
	RunStatusCompleted RunStatus = "completed"
	RunStatusFailed    RunStatus = "failed"
)

// RunRecord is the persisted record of one scheduled-job fire. SessionID is
// both the record key and the conversation ID of the `infer headless` run, so
// consumers (e.g. the desktop app) can load the full transcript from
// conversation storage.
type RunRecord struct {
	SessionID  string     `yaml:"session_id" json:"session_id"`
	JobID      string     `yaml:"job_id" json:"job_id"`
	Status     RunStatus  `yaml:"status" json:"status"`
	Error      string     `yaml:"error,omitempty" json:"error,omitempty"`
	StartedAt  time.Time  `yaml:"started_at" json:"started_at"`
	FinishedAt *time.Time `yaml:"finished_at,omitempty" json:"finished_at,omitempty"`
}

// RunEvent is emitted by the scheduler as a job run progresses. Line events
// carry one raw agent stdout line (valid only for the duration of the
// callback); the terminal event has Done set, with Err populated on failure.
type RunEvent struct {
	Line []byte
	Err  error
	Done bool
}
