package domain

import (
	"context"
	"os/exec"
	"sync"
	"time"
)

// ShellState represents the state of a background shell.
type ShellState string

const (
	ShellStateRunning   ShellState = "running"
	ShellStateCompleted ShellState = "completed"
	ShellStateFailed    ShellState = "failed"
	ShellStateCancelled ShellState = "cancelled"
)

// String returns the string representation of the shell state.
func (s ShellState) String() string {
	return string(s)
}

// IsTerminal returns true if the state is a terminal state (completed, failed, or cancelled).
func (s ShellState) IsTerminal() bool {
	return s == ShellStateCompleted || s == ShellStateFailed || s == ShellStateCancelled
}

// BackgroundShell represents a command running in the background. The
// supervisor records its outcome while parallel tool calls read it, so the
// outcome and the read position sit behind mu.
type BackgroundShell struct {
	ShellID      string
	Command      string
	Cmd          *exec.Cmd
	StartedAt    time.Time
	OutputBuffer *OutputRingBuffer
	CancelFunc   context.CancelFunc

	ReadersDone <-chan struct{}

	mu         sync.RWMutex
	outcome    ShellStatus
	readOffset int64
}

// ShellStatus is a snapshot of a shell's state. ExitCode and CompletedAt stay
// nil while it runs.
type ShellStatus struct {
	State       ShellState
	ExitCode    *int
	CompletedAt *time.Time
}

// Status returns a snapshot of the shell's state. A shell that has not
// finished reports ShellStateRunning.
func (s *BackgroundShell) Status() ShellStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.statusLocked()
}

func (s *BackgroundShell) statusLocked() ShellStatus {
	if s.outcome.State == "" {
		return ShellStatus{State: ShellStateRunning}
	}
	return s.outcome
}

// Finish records the shell's terminal state once its process has been waited on.
func (s *BackgroundShell) Finish(state ShellState, exitCode int, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.outcome = ShellStatus{State: state, ExitCode: &exitCode, CompletedAt: &at}
}

// Read returns the output from offset, or from where the previous Read
// stopped when offset is negative, and the state seen before reading. The
// state is read first, so a terminal state means the output is complete.
func (s *BackgroundShell) Read(offset int64) (string, int64, ShellState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.statusLocked().State
	if offset < 0 {
		offset = s.readOffset
	}
	output, next := s.OutputBuffer.ReadFrom(offset)
	s.readOffset = next
	return output, next, state
}

// ShellTracker defines the interface for managing background shells.
type ShellTracker interface {
	// Add adds a new shell to the tracker.
	// Returns an error if max concurrent limit is reached.
	Add(shell *BackgroundShell) error

	// Get retrieves a shell by ID.
	// Returns nil if not found.
	Get(shellID string) *BackgroundShell

	// GetAll returns all tracked shells.
	GetAll() []*BackgroundShell

	// Remove removes a shell from the tracker.
	Remove(shellID string) error

	// CountRunning returns the number of shells in running state.
	CountRunning() int
}

// ShellInfo provides summary information about a shell for UI display.
type ShellInfo struct {
	ShellID     string
	Command     string
	State       ShellState
	StartedAt   time.Time
	CompletedAt *time.Time
	ExitCode    *int
	OutputSize  int64
	Elapsed     time.Duration
}

// NewShellInfo creates a ShellInfo from a BackgroundShell.
func NewShellInfo(shell *BackgroundShell) *ShellInfo {
	status := shell.Status()
	elapsed := time.Since(shell.StartedAt)
	if status.CompletedAt != nil {
		elapsed = status.CompletedAt.Sub(shell.StartedAt)
	}

	return &ShellInfo{
		ShellID:     shell.ShellID,
		Command:     shell.Command,
		State:       status.State,
		StartedAt:   shell.StartedAt,
		CompletedAt: status.CompletedAt,
		ExitCode:    status.ExitCode,
		OutputSize:  shell.OutputBuffer.TotalWritten(),
		Elapsed:     elapsed,
	}
}

// BackgroundShellService defines the interface for managing background shells
type BackgroundShellService interface {
	// DetachToBackground moves a running command to background. readersDone, when
	// non-nil, is closed once the caller's pipe readers reach EOF; the supervisor
	// waits on it before reaping so trailing output is not truncated.
	DetachToBackground(ctx context.Context, cmd *exec.Cmd, command string, outputBuffer *OutputRingBuffer, readersDone <-chan struct{}) (string, error)

	// GetShellOutput retrieves output from a shell
	GetShellOutput(shellID string, fromOffset int64) (string, int64, ShellState, error)

	// GetShellOutputWithFilter retrieves filtered output from a shell
	GetShellOutputWithFilter(shellID string, fromOffset int64, filterPattern string) (string, int64, ShellState, error)

	// GetShell returns a specific shell by ID
	GetShell(shellID string) *BackgroundShell

	// GetAllShells returns all tracked shells
	GetAllShells() []*BackgroundShell

	// CancelShell cancels a running background shell
	CancelShell(shellID string) error

	// RemoveShell removes a shell from tracking
	RemoveShell(shellID string) error
}
