package domain

import (
	"context"
	"os/exec"
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

// BackgroundShell represents a command running in the background.
type BackgroundShell struct {
	ShellID      string
	Command      string
	Cmd          *exec.Cmd
	StartedAt    time.Time
	CompletedAt  *time.Time
	State        ShellState
	ExitCode     *int
	OutputBuffer OutputRingBuffer
	CancelFunc   context.CancelFunc
	ReadOffset   int64
}

// OutputRingBuffer defines the interface for the circular output buffer.
type OutputRingBuffer interface {
	Write(p []byte) (n int, err error)
	ReadFrom(offset int64) (string, int64)
	Recent(maxBytes int) string
	TotalWritten() int64
	Size() int
	String() string
	Clear()
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

	// Cleanup removes shells in terminal states older than the specified duration.
	Cleanup(olderThan time.Duration) int

	// Count returns the number of tracked shells.
	Count() int

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
	elapsed := time.Since(shell.StartedAt)
	if shell.CompletedAt != nil {
		elapsed = shell.CompletedAt.Sub(shell.StartedAt)
	}

	return &ShellInfo{
		ShellID:     shell.ShellID,
		Command:     shell.Command,
		State:       shell.State,
		StartedAt:   shell.StartedAt,
		CompletedAt: shell.CompletedAt,
		ExitCode:    shell.ExitCode,
		OutputSize:  shell.OutputBuffer.TotalWritten(),
		Elapsed:     elapsed,
	}
}

// BackgroundShellService defines the interface for managing background shells
type BackgroundShellService interface {
	// DetachToBackground moves a running command to background
	DetachToBackground(ctx context.Context, cmd *exec.Cmd, command string, outputBuffer OutputRingBuffer) (string, error)

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
