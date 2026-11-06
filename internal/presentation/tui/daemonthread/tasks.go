package daemonthread

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
)

// Tasks lists and stops a daemon chat's background jobs, which run in the worker
// of its thread.
type Tasks struct {
	thread *Thread
}

// NewTasks builds the daemon-mode job list over thread.
func NewTasks(thread *Thread) *Tasks {
	return &Tasks{thread: thread}
}

// List asks the thread's worker for its jobs.
func (t *Tasks) List(ctx context.Context) ([]scheddomain.TrackedJob, error) {
	frame := map[string]string{"type": "list_tasks", "project_dir": t.thread.projectDir(), "id": t.thread.Live()}
	data, err := t.thread.Request(ctx, frame, "tasks")
	if err != nil {
		return nil, err
	}
	var listed struct {
		Tasks []struct {
			ID          string                        `json:"id"`
			SessionID   string                        `json:"session_id"`
			Kind        scheddomain.JobKind           `json:"kind"`
			Label       string                        `json:"label"`
			Description string                        `json:"description"`
			Detail      string                        `json:"detail"`
			Status      scheddomain.JobStatus         `json:"status"`
			StartedAt   time.Time                     `json:"started_at"`
			CompletedAt *time.Time                    `json:"completed_at"`
			Output      string                        `json:"output"`
			Stats       *scheddomain.SubagentRunStats `json:"stats"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(data, &listed); err != nil {
		return nil, err
	}
	jobs := make([]scheddomain.TrackedJob, 0, len(listed.Tasks))
	for _, task := range listed.Tasks {
		jobs = append(jobs, scheddomain.TrackedJob{
			Meta: scheddomain.JobMeta{
				ID: task.ID, SessionID: task.SessionID, Kind: task.Kind, Label: task.Label,
				Description: task.Description, Detail: task.Detail, StartedAt: task.StartedAt,
			},
			Status: task.Status, CompletedAt: task.CompletedAt, Output: task.Output, Stats: task.Stats,
		})
	}
	return jobs, nil
}

// Stop asks the thread's worker to stop the job.
func (t *Tasks) Stop(ctx context.Context, jobID string) error {
	frame := map[string]string{"type": "cancel_task", "project_dir": t.thread.projectDir(), "id": t.thread.Live(), "task_id": jobID}
	data, err := t.thread.Request(ctx, frame, "task_cancelled")
	if err != nil {
		return err
	}
	var reply struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(data, &reply); err != nil {
		return err
	}
	if reply.Error != "" {
		return errors.New(reply.Error)
	}
	return nil
}
