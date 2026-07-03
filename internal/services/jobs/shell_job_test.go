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

// isTerminal reports whether the job is currently in a terminal state without
// blocking, for asserting that a job has NOT yet finished.
func isTerminal(sup *Supervisor, id string) bool {
	for _, j := range sup.Snapshot() {
		if j.Meta.ID == id {
			return j.Status.IsTerminal()
		}
	}
	return false
}

// TestShellJob_WaitsForReadersDoneBeforeReaping asserts Run does not call
// Cmd.Wait until the pipe readers have drained (ReadersDone closed), even after
// the process has already exited - otherwise Wait closes the pipes mid-drain and
// truncates trailing output.
func TestShellJob_WaitsForReadersDoneBeforeReaping(t *testing.T) {
	tracker := utils.NewShellTracker(10)
	sup := NewSupervisor(&domainmocks.FakeMessageQueue{}, &domainmocks.FakeConversationRepository{}, nil)
	defer sup.Stop()

	// `true` exits immediately; the job must still park on ReadersDone.
	shell := startShell(t, "s-drain", "true")
	readersDone := make(chan struct{})
	shell.ReadersDone = readersDone
	_ = tracker.Add(shell)
	sup.Submit(NewShellJob(shell, tracker))

	time.Sleep(50 * time.Millisecond)
	if isTerminal(sup, "s-drain") {
		t.Fatal("shell reaped before the pipe readers signalled done")
	}

	close(readersDone)
	if got := waitTerminal(t, sup, "s-drain"); got != domain.JobCompleted {
		t.Fatalf("status = %s, want completed", got)
	}
	if shell.State != domain.ShellStateCompleted {
		t.Fatalf("shell state = %s, want completed", shell.State)
	}
}

// TestShellJob_WindStopUnblocksReadersWait guards the cancellable-wait escape:
// when ReadersDone never closes (e.g. a grandchild holds the pipe open),
// Wind(WindStop) must still reap the job via ctx cancellation rather than
// wedging Run - which would also hang Supervisor.Stop().
func TestShellJob_WindStopUnblocksReadersWait(t *testing.T) {
	tracker := utils.NewShellTracker(10)
	sup := NewSupervisor(&domainmocks.FakeMessageQueue{}, &domainmocks.FakeConversationRepository{}, nil)
	defer sup.Stop()

	shell := startShell(t, "s-stuck", "sleep", "60")
	shell.ReadersDone = make(chan struct{}) // never closed
	_ = tracker.Add(shell)
	sup.Submit(NewShellJob(shell, tracker))

	time.Sleep(20 * time.Millisecond)
	if err := sup.Wind("s-stuck", domain.WindStop); err != nil {
		t.Fatalf("Wind: %v", err)
	}
	waitTerminal(t, sup, "s-stuck")
	if shell.State != domain.ShellStateCancelled {
		t.Fatalf("shell state = %s, want cancelled", shell.State)
	}
}

func TestShellJob_CompletesAndNotifies(t *testing.T) {
	tracker := utils.NewShellTracker(10)
	queue := &domainmocks.FakeMessageQueue{}
	sup := NewSupervisor(queue, &domainmocks.FakeConversationRepository{}, nil)
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
	sup := NewSupervisor(&domainmocks.FakeMessageQueue{}, &domainmocks.FakeConversationRepository{}, nil)
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
	sup := NewSupervisor(&domainmocks.FakeMessageQueue{}, &domainmocks.FakeConversationRepository{}, nil)
	defer sup.Stop()

	shell := startShell(t, "s-sleep", "sleep", "60")
	_ = tracker.Add(shell)
	sup.Submit(NewShellJob(shell, tracker))

	time.Sleep(20 * time.Millisecond)
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
