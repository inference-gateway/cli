package domain

import (
	"context"
	"time"
)

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

// SchedulerService manages the lifecycle of scheduled jobs. It is started by
// the `infer daemon` process and watches the schedule storage for changes so
// that jobs created by an agent process are picked up without a restart.
type SchedulerService interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	LoadJobs() error
}
