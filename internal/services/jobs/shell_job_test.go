package jobs

import (
	"os/exec"
	"testing"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
	utils "github.com/inference-gateway/cli/internal/utils"
	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"
)

func startShell(t *testing.T, id, name string, args ...string) *domain.BackgroundShell {
	t.Helper()
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", name, err)
	}
	return &domain.BackgroundShell{
		ShellID:      id,
		Command:      name,
		Cmd:          cmd,
		StartedAt:    time.Now(),
		State:        domain.ShellStateRunning,
		OutputBuffer: utils.NewOutputRingBuffer(1024),
	}
}

// waitTerminal polls the supervisor snapshot (mutex-synchronised) until the job
// finishes, then returns its status. Observing the terminal status this way
// establishes a happens-before with the job's Run, so the caller may then read
// the shell record's fields without racing the monitor goroutine.
func waitTerminal(t *testing.T, sup *Supervisor, id string) domain.JobStatus {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, j := range sup.Snapshot() {
			if j.Meta.ID == id && j.Status.IsTerminal() {
				return j.Status
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("job %s did not finish", id)
	return ""
}

func TestShellJob_CompletesAndNotifies(t *testing.T) {
	tracker := utils.NewShellTracker(10)
	queue := &domainmocks.FakeMessageQueue{}
	sup := NewSupervisor(queue, &domainmocks.FakeConversationRepository{})
	defer sup.Stop()

	shell := startShell(t, "s-ok", "true")
	_ = tracker.Add(shell)
	sup.Submit(NewShellJob(shell, tracker))

	if got := waitTerminal(t, sup, "s-ok"); got != domain.JobCompleted {
		t.Fatalf("status = %s, want completed", got)
	}
	if shell.State != domain.ShellStateCompleted || shell.ExitCode == nil || *shell.ExitCode != 0 {
		t.Fatalf("shell state=%s exit=%v, want completed/0", shell.State, shell.ExitCode)
	}
	if n := queue.EnqueueCallCount(); n != 1 {
		t.Fatalf("Enqueue called %d times, want 1", n)
	}
}

func TestShellJob_FailureRecordsExitCode(t *testing.T) {
	tracker := utils.NewShellTracker(10)
	sup := NewSupervisor(&domainmocks.FakeMessageQueue{}, &domainmocks.FakeConversationRepository{})
	defer sup.Stop()

	shell := startShell(t, "s-fail", "false")
	_ = tracker.Add(shell)
	sup.Submit(NewShellJob(shell, tracker))

	if got := waitTerminal(t, sup, "s-fail"); got != domain.JobFailed {
		t.Fatalf("status = %s, want failed", got)
	}
	if shell.State != domain.ShellStateFailed || shell.ExitCode == nil || *shell.ExitCode == 0 {
		t.Fatalf("shell state=%s exit=%v, want failed/non-zero", shell.State, shell.ExitCode)
	}
}

func TestShellJob_WindStopCancels(t *testing.T) {
	tracker := utils.NewShellTracker(10)
	sup := NewSupervisor(&domainmocks.FakeMessageQueue{}, &domainmocks.FakeConversationRepository{})
	defer sup.Stop()

	shell := startShell(t, "s-sleep", "sleep", "60")
	_ = tracker.Add(shell)
	sup.Submit(NewShellJob(shell, tracker))

	time.Sleep(20 * time.Millisecond) // let the monitor start waiting
	if err := sup.Wind("s-sleep", domain.WindStop); err != nil {
		t.Fatalf("Wind: %v", err)
	}
	waitTerminal(t, sup, "s-sleep")
	if shell.State != domain.ShellStateCancelled {
		t.Fatalf("shell state = %s, want cancelled", shell.State)
	}

	NewShellJob(shell, tracker).Close()
	if tracker.Get("s-sleep") != nil {
		t.Fatalf("Close did not remove the shell from the tracker")
	}
}
