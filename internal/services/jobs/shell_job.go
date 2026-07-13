package jobs

import (
	"context"
	"fmt"
	"os/exec"
	"syscall"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
)

// shellJob adapts a detached background bash shell to a BackgroundJob: Run blocks
// on the process via cmd.Wait (the same wait the old per-shell monitor did),
// updates the shell record, and reports the outcome. The supervisor owns the
// goroutine, the completion notification, and reaping - this replaces
// BackgroundShellService.monitorShell.
type shellJob struct {
	shell   *domain.BackgroundShell
	tracker domain.ShellTracker
}

// NewShellJob wraps a running background shell as a supervised job. The shell
// must already be registered in tracker (so the bash tools can read its output);
// Close removes it on reap.
func NewShellJob(shell *domain.BackgroundShell, tracker domain.ShellTracker) domain.BackgroundJob {
	return &shellJob{shell: shell, tracker: tracker}
}

// Meta describes the shell for the task view.
func (j *shellJob) Meta() domain.JobMeta {
	return domain.JobMeta{
		ID:           j.shell.ShellID,
		Kind:         domain.JobKindShell,
		Label:        j.shell.ShellID,
		Description:  j.shell.Command,
		Detail:       j.shell.Command,
		StartedAt:    j.shell.StartedAt,
		HoldsSession: true,
	}
}

// Run waits for the shell process to exit and records the outcome on the shell.
// It first waits for the pipe readers to drain (ReadersDone) so Cmd.Wait doesn't
// close the pipes mid-read and lose output; the wait honours ctx so a kill or
// shutdown can't wedge when a grandchild is holding the pipe open.
func (j *shellJob) Run(ctx context.Context, _ func(domain.JobSignal)) domain.ToolExecutionResult {
	if j.shell.ReadersDone != nil {
		select {
		case <-j.shell.ReadersDone:
		case <-ctx.Done():
		}
	}

	waitErr := j.shell.Cmd.Wait()

	now := time.Now()
	j.shell.CompletedAt = &now
	duration := now.Sub(j.shell.StartedAt)

	switch {
	case ctx.Err() != nil:
		code := -1
		j.shell.ExitCode = &code
		j.shell.State = domain.ShellStateCancelled
		return domain.ToolExecutionResult{ToolName: "Bash", Success: false, Duration: duration, Error: "shell cancelled"}
	case waitErr != nil:
		code := exitCodeFromErr(waitErr)
		j.shell.ExitCode = &code
		j.shell.State = domain.ShellStateFailed
		return domain.ToolExecutionResult{ToolName: "Bash", Success: false, Duration: duration, Error: waitErr.Error()}
	default:
		code := 0
		j.shell.ExitCode = &code
		j.shell.State = domain.ShellStateCompleted
		return domain.ToolExecutionResult{ToolName: "Bash", Success: true, Duration: duration}
	}
}

// Wind signals the running process: WindWrapUp -> SIGTERM, WindStop -> SIGKILL.
func (j *shellJob) Wind(_ context.Context, sig domain.WindSignal) error {
	proc := j.shell.Cmd.Process
	if proc == nil {
		return nil
	}
	if sig == domain.WindStop {
		_ = proc.Kill()
		return nil
	}
	_ = proc.Signal(syscall.SIGTERM)
	return nil
}

// Close removes the shell from the tracker when the supervisor reaps it.
func (j *shellJob) Close() {
	_ = j.tracker.Remove(j.shell.ShellID)
}

// Output returns the shell's captured stdout/stderr from the ring buffer.
// For a running shell this is the output so far; for a finished one it is the
// full captured output (bounded by the ring buffer's max size).
func (j *shellJob) Output() string {
	if j.shell.OutputBuffer == nil {
		return ""
	}
	return j.shell.OutputBuffer.String()
}

// Notification formats the shell's completion summary (the generic tool-result
// formatter does not know about exit codes or durations).
func (j *shellJob) Notification(result domain.ToolExecutionResult) string {
	code := 0
	if j.shell.ExitCode != nil {
		code = *j.shell.ExitCode
	}
	dur := result.Duration.Round(time.Millisecond)
	if !result.Success {
		return fmt.Sprintf("Command: %s | Exit code: %d | Duration: %s | Error: %s. Use BashOutput to retrieve the full output.",
			j.shell.Command, code, dur, result.Error)
	}
	return fmt.Sprintf("Command: %s | Exit code: %d | Duration: %s. Use BashOutput to retrieve the full output.",
		j.shell.Command, code, dur)
}

// exitCodeFromErr extracts a process exit code from a cmd.Wait error.
func exitCodeFromErr(err error) int {
	if exitErr, ok := err.(*exec.ExitError); ok {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			return status.ExitStatus()
		}
	}
	return -1
}
