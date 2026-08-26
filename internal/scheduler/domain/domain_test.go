package domain

import (
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

// stubRingBuffer is the minimal OutputRingBuffer NewShellInfo needs.
type stubRingBuffer struct{ total int64 }

func (s *stubRingBuffer) Write(p []byte) (int, error)    { return len(p), nil }
func (s *stubRingBuffer) ReadFrom(int64) (string, int64) { return "", s.total }
func (s *stubRingBuffer) Recent(int) string              { return "" }
func (s *stubRingBuffer) TotalWritten() int64            { return s.total }
func (s *stubRingBuffer) Size() int                      { return int(s.total) }
func (s *stubRingBuffer) String() string                 { return "" }
func (s *stubRingBuffer) Clear()                         {}

func TestNewShellInfo(t *testing.T) {
	started := time.Now().Add(-10 * time.Minute)
	completed := started.Add(2 * time.Minute)
	exitCode := 3

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
				State:        ShellStateRunning,
				OutputBuffer: &stubRingBuffer{total: 42},
			},
			wantElapsed: func(d time.Duration) bool { return d >= 10*time.Minute && d < 11*time.Minute },
		},
		{
			name: "completed shell freezes elapsed at CompletedAt-StartedAt",
			shell: &BackgroundShell{
				ShellID:      "sh-2",
				Command:      "make build",
				StartedAt:    started,
				CompletedAt:  &completed,
				State:        ShellStateCompleted,
				ExitCode:     &exitCode,
				OutputBuffer: &stubRingBuffer{total: 1024},
			},
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
			if info.State != tt.shell.State {
				t.Errorf("State = %q, want %q", info.State, tt.shell.State)
			}
			if !info.StartedAt.Equal(tt.shell.StartedAt) {
				t.Errorf("StartedAt = %v, want %v", info.StartedAt, tt.shell.StartedAt)
			}
			if info.CompletedAt != tt.shell.CompletedAt {
				t.Errorf("CompletedAt = %v, want %v", info.CompletedAt, tt.shell.CompletedAt)
			}
			if info.ExitCode != tt.shell.ExitCode {
				t.Errorf("ExitCode = %v, want %v", info.ExitCode, tt.shell.ExitCode)
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
