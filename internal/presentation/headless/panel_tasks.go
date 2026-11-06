package headless

import (
	"errors"
	"slices"
	"time"

	a2adomain "github.com/inference-gateway/cli/internal/protocols/a2a/domain"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
)

// The panel frames a client manages a session's background jobs with, answered
// by the worker that runs them.
const (
	inboundListTasks  = "list_tasks"
	inboundCancelTask = "cancel_task"

	outboundTasks         = "tasks"
	outboundTaskCancelled = "task_cancelled"
)

// taskEntry is one background job as list_tasks reports it.
type taskEntry struct {
	ID          string                        `json:"id"`
	SessionID   string                        `json:"session_id,omitempty"`
	Kind        string                        `json:"kind"`
	Label       string                        `json:"label"`
	Description string                        `json:"description,omitempty"`
	Detail      string                        `json:"detail,omitempty"`
	Status      string                        `json:"status"`
	StartedAt   time.Time                     `json:"started_at"`
	CompletedAt *time.Time                    `json:"completed_at,omitempty"`
	Output      string                        `json:"output,omitempty"`
	Stats       *scheddomain.SubagentRunStats `json:"stats,omitempty"`
}

// tasksFrame answers list_tasks with the jobs of this worker's thread.
type tasksFrame struct {
	Type  string      `json:"type"`
	Tasks []taskEntry `json:"tasks"`
}

// taskCancelledFrame answers cancel_task once the job was told to stop.
type taskCancelledFrame struct {
	Type   string `json:"type"`
	TaskID string `json:"task_id"`
	Error  string `json:"error,omitempty"`
}

// tasks answers the task frames from the supervisor of this worker's jobs.
type tasks struct {
	write frameWriter
	jobs  scheddomain.BackgroundTaskRegistry
	a2a   a2adomain.BackgroundTaskService
}

func (t *tasks) list() {
	entries := []taskEntry{}
	if t.jobs != nil {
		for _, job := range t.jobs.Snapshot() {
			entries = append(entries, taskEntry{
				ID: job.Meta.ID, SessionID: job.Meta.SessionID, Kind: string(job.Meta.Kind), Label: job.Meta.Label,
				Description: job.Meta.Description, Detail: job.Meta.Detail, Status: string(job.Status),
				StartedAt: job.Meta.StartedAt, CompletedAt: job.CompletedAt, Output: job.Output, Stats: job.Stats,
			})
		}
	}
	t.write(tasksFrame{Type: outboundTasks, Tasks: entries})
}

func (t *tasks) cancel(id string) {
	frame := taskCancelledFrame{Type: outboundTaskCancelled, TaskID: id}
	if err := t.stop(id); err != nil {
		frame.Error = err.Error()
	}
	t.write(frame)
}

// stop routes by kind like the task view of an in-process chat: an A2A task also
// has to be cancelled on its agent, which winding the poll job does not do.
func (t *tasks) stop(id string) error {
	if t.jobs == nil {
		return errors.New("this worker runs no background jobs")
	}
	if t.a2a != nil && t.isA2A(id) {
		return t.a2a.CancelBackgroundTask(id)
	}
	return t.jobs.WindJob(id, scheddomain.WindStop)
}

func (t *tasks) isA2A(id string) bool {
	return slices.ContainsFunc(t.jobs.Snapshot(), func(job scheddomain.TrackedJob) bool {
		return job.Meta.ID == id && job.Meta.Kind == scheddomain.JobKindA2A
	})
}
