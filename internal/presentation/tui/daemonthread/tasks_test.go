package daemonthread

import (
	"strings"
	"testing"

	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
)

func TestTasksListsTheWorkersJobs(t *testing.T) {
	d := startDaemon(t, 0)
	thread := remoteThread(t, d)
	d.answer("list_tasks", `{"type":"tasks","tasks":[{"id":"shell-1","kind":"shell","label":"build","detail":"go build","status":"running","started_at":"2026-11-06T10:00:00Z"},`+
		`{"id":"sub-1","session_id":"s1","kind":"subagent","label":"probe","status":"completed","started_at":"2026-11-06T10:00:00Z","completed_at":"2026-11-06T10:01:00Z","output":"done","stats":{"tools_succeeded":2}}]}`)

	jobs, err := NewTasks(thread).List(t.Context())

	if err != nil || len(jobs) != 2 {
		t.Fatalf("List = %+v, %v", jobs, err)
	}
	if shell := jobs[0]; shell.Meta.ID != "shell-1" || shell.Meta.Kind != scheddomain.JobKindShell || shell.Meta.Detail != "go build" || shell.Status != scheddomain.JobRunning || shell.CompletedAt != nil {
		t.Errorf("shell job = %+v", shell)
	}
	if sub := jobs[1]; sub.Meta.SessionID != "s1" || sub.Status != scheddomain.JobCompleted || sub.CompletedAt == nil || sub.Output != "done" || sub.Stats == nil {
		t.Errorf("subagent job = %+v", sub)
	}
	request := d.frames("list_tasks")
	if len(request) != 1 || request[0]["id"] != "c1" || request[0]["project_dir"] != "/proj" {
		t.Fatalf("list_tasks named %+v, want the session on its project", request)
	}
}

func TestTasksStopNamesTheJobAndReportsTheWorkersError(t *testing.T) {
	d := startDaemon(t, 0)
	thread := remoteThread(t, d)
	d.answer("cancel_task", `{"type":"task_cancelled","task_id":"shell-1","error":"job not found"}`)

	err := NewTasks(thread).Stop(t.Context(), "shell-1")

	if err == nil || !strings.Contains(err.Error(), "job not found") {
		t.Fatalf("Stop err = %v, want the worker's error", err)
	}
	request := d.frames("cancel_task")
	if len(request) != 1 || request[0]["task_id"] != "shell-1" || request[0]["id"] != "c1" {
		t.Fatalf("cancel_task named %+v, want the job on the session", request)
	}
}
