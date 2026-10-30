package domain

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestJobStatusIsTerminal(t *testing.T) {
	tests := []struct {
		name   string
		status JobStatus
		want   bool
	}{
		{"running", JobRunning, false},
		{"completed", JobCompleted, true},
		{"failed", JobFailed, true},
		{"unknown", JobStatus("cancelled"), false},
		{"empty", JobStatus(""), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.status.IsTerminal(); got != tt.want {
				t.Fatalf("JobStatus(%q).IsTerminal() = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

func TestShellStateIsTerminal(t *testing.T) {
	tests := []struct {
		name  string
		state ShellState
		want  bool
	}{
		{"running", ShellStateRunning, false},
		{"completed", ShellStateCompleted, true},
		{"failed", ShellStateFailed, true},
		{"cancelled", ShellStateCancelled, true},
		{"unknown", ShellState("paused"), false},
		{"empty", ShellState(""), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.state.IsTerminal(); got != tt.want {
				t.Fatalf("ShellState(%q).IsTerminal() = %v, want %v", tt.state, got, tt.want)
			}
		})
	}
}

func TestShellStateString(t *testing.T) {
	tests := []struct {
		state ShellState
		want  string
	}{
		{ShellStateRunning, "running"},
		{ShellStateCompleted, "completed"},
		{ShellStateFailed, "failed"},
		{ShellStateCancelled, "cancelled"},
		{ShellState("weird"), "weird"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.state.String(); got != tt.want {
				t.Fatalf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWindSignalString(t *testing.T) {
	tests := []struct {
		name string
		sig  WindSignal
		want string
	}{
		{"wrap-up", WindWrapUp, "wrap-up"},
		{"stop", WindStop, "stop"},
		{"unknown defaults to wrap-up", WindSignal(99), "wrap-up"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.sig.String(); got != tt.want {
				t.Fatalf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

// shellBuffer returns an OutputRingBuffer with total bytes already written.
func shellBuffer(total int) *OutputRingBuffer {
	rb := NewOutputRingBuffer(total)
	_, _ = rb.Write(make([]byte, total))
	return rb
}

func finishedShell(shell *BackgroundShell, state ShellState, exitCode int, at time.Time) *BackgroundShell {
	shell.Finish(state, exitCode, at)
	return shell
}

func TestBackgroundShellReadAndFinish(t *testing.T) {
	shell := &BackgroundShell{OutputBuffer: NewOutputRingBuffer(64)}
	_, _ = shell.OutputBuffer.Write([]byte("hello"))
	if got := shell.Status().State; got != ShellStateRunning {
		t.Fatalf("unfinished shell state = %q, want running", got)
	}

	var mu sync.Mutex
	var got strings.Builder
	var wg sync.WaitGroup
	wg.Go(func() { shell.Finish(ShellStateCompleted, 0, time.Now()) })
	for range 8 {
		wg.Go(func() {
			out, _, _ := shell.Read(-1)
			mu.Lock()
			got.WriteString(out)
			mu.Unlock()
		})
	}
	wg.Wait()

	if got.String() != "hello" {
		t.Errorf("parallel reads returned %q, want each byte exactly once", got.String())
	}
	if out, next, state := shell.Read(0); out != "hello" || next != 5 || state != ShellStateCompleted {
		t.Errorf("Read(0) = %q, %d, %q, want hello, 5, completed", out, next, state)
	}
}

func TestNewShellInfo(t *testing.T) {
	started := time.Now().Add(-10 * time.Minute)
	completed := started.Add(2 * time.Minute)

	tests := []struct {
		name        string
		shell       *BackgroundShell
		wantElapsed func(d time.Duration) bool
	}{
		{
			name: "running shell measures elapsed from start until now",
			shell: &BackgroundShell{
				ShellID:      "sh-1",
				Command:      "sleep 600",
				StartedAt:    started,
				OutputBuffer: shellBuffer(42),
			},
			wantElapsed: func(d time.Duration) bool { return d >= 10*time.Minute && d < 11*time.Minute },
		},
		{
			name: "completed shell freezes elapsed at CompletedAt-StartedAt",
			shell: finishedShell(&BackgroundShell{
				ShellID:      "sh-2",
				Command:      "make build",
				StartedAt:    started,
				OutputBuffer: shellBuffer(1024),
			}, ShellStateCompleted, 3, completed),
			wantElapsed: func(d time.Duration) bool { return d == 2*time.Minute },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := NewShellInfo(tt.shell)
			if info.ShellID != tt.shell.ShellID {
				t.Errorf("ShellID = %q, want %q", info.ShellID, tt.shell.ShellID)
			}
			if info.Command != tt.shell.Command {
				t.Errorf("Command = %q, want %q", info.Command, tt.shell.Command)
			}
			status := tt.shell.Status()
			if info.State != status.State {
				t.Errorf("State = %q, want %q", info.State, status.State)
			}
			if !info.StartedAt.Equal(tt.shell.StartedAt) {
				t.Errorf("StartedAt = %v, want %v", info.StartedAt, tt.shell.StartedAt)
			}
			if info.CompletedAt != status.CompletedAt {
				t.Errorf("CompletedAt = %v, want %v", info.CompletedAt, status.CompletedAt)
			}
			if info.ExitCode != status.ExitCode {
				t.Errorf("ExitCode = %v, want %v", info.ExitCode, status.ExitCode)
			}
			if want := tt.shell.OutputBuffer.TotalWritten(); info.OutputSize != want {
				t.Errorf("OutputSize = %d, want %d", info.OutputSize, want)
			}
			if !tt.wantElapsed(info.Elapsed) {
				t.Errorf("Elapsed = %v out of expected range", info.Elapsed)
			}
		})
	}
}

func TestValidJobID(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{"6f1c0f6e-2f7a-4d3e-9a1b-0c2d3e4f5a6b", true},
		{"job-1", true},
		{"", false},
		{".", false},
		{"..", false},
		{"../escape", false},
		{"a/b", false},
		{`a\b`, false},
	}
	for _, tt := range tests {
		if got := ValidJobID(tt.id); got != tt.want {
			t.Errorf("ValidJobID(%q) = %v, want %v", tt.id, got, tt.want)
		}
	}
}
