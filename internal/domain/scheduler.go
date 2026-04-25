package domain

import (
	"context"
	"time"
)

// ScheduledJob describes a recurring task that the LLM has asked the system
// to run on a cron schedule. Jobs are persisted as YAML files and loaded by
// the SchedulerService running inside the channels-manager daemon.
//
// Each fire spawns a fresh `infer agent` subprocess with a brand-new session
// ID — no context is carried between fires, matching the issue's requirement.
type ScheduledJob struct {
	ID             string     `yaml:"id" json:"id"`
	Name           string     `yaml:"name,omitempty" json:"name,omitempty"`
	Description    string     `yaml:"description,omitempty" json:"description,omitempty"`
	CronExpression string     `yaml:"cron_expression" json:"cron_expression"`
	Prompt         string     `yaml:"prompt" json:"prompt"`
	Channel        string     `yaml:"channel" json:"channel"`
	RecipientID    string     `yaml:"recipient_id" json:"recipient_id"`
	Model          string     `yaml:"model,omitempty" json:"model,omitempty"`
	RunOnce        bool       `yaml:"run_once,omitempty" json:"run_once,omitempty"`
	CreatedAt      time.Time  `yaml:"created_at" json:"created_at"`
	UpdatedAt      time.Time  `yaml:"updated_at" json:"updated_at"`
	LastRun        *time.Time `yaml:"last_run,omitempty" json:"last_run,omitempty"`
	LastError      string     `yaml:"last_error,omitempty" json:"last_error,omitempty"`
}

// SchedulerService manages the lifecycle of scheduled jobs. It is started by
// the channels-manager daemon and watches the schedule storage directory for
// changes so that jobs created by an agent process are picked up without a
// daemon restart.
type SchedulerService interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	LoadJobs() error
}
