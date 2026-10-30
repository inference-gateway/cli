package tui

import (
	"context"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// SessionPlacement is where a daemon chat's session runs: Local is the
// environment of the daemon the TUI talks to and Environment the one the
// session lives on. The zero value is an in-process chat.
type SessionPlacement struct {
	Daemon       bool
	Local        string
	Environment  string
	Workspace    string
	Transferring string
	Offline      bool
}

// Remote reports whether the session runs on an environment other than the
// daemon the TUI talks to, so laptop-only features do not apply to it.
func (p SessionPlacement) Remote() bool {
	return p.Daemon && p.Environment != "" && p.Environment != p.Local
}

// Where names the environment the session runs on.
func (p SessionPlacement) Where() string {
	if p.Environment != "" {
		return p.Environment
	}
	return p.Local
}

// Feature is a chat feature that acts in the TUI's own process, so whether it
// applies depends on where the session runs.
type Feature int

const (
	// FeatureLocalFiles reads this machine's checkout: @file, /diff and /explorer.
	FeatureLocalFiles Feature = iota
	// FeatureCompaction compacts the conversation the TUI holds.
	FeatureCompaction
	// FeatureHistoryRewind rewinds or edits the conversation the TUI holds.
	FeatureHistoryRewind
	// FeatureBackgroundShell detaches the shell the TUI runs a command in.
	FeatureBackgroundShell
	// FeatureTasks manages the A2A tasks the TUI's own agent started.
	FeatureTasks
)

// Unavailable says why feature does not apply to the session at p, or "" when
// it does. An in-process chat has every feature, a daemon chat's worker owns
// the conversation, and a remote session's checkout is not this one.
func (p SessionPlacement) Unavailable(feature Feature) string {
	if !p.Daemon {
		return ""
	}
	switch feature {
	case FeatureLocalFiles:
		if p.Remote() {
			return "the session runs on " + p.Environment + ", whose checkout is not this one"
		}
	case FeatureCompaction:
		return "the daemon's worker compacts the session itself at compact.auto_at"
	case FeatureHistoryRewind:
		return "the conversation lives on the daemon's worker"
	case FeatureBackgroundShell:
		return "the session's commands run in the daemon's worker"
	case FeatureTasks:
		return "a daemon chat starts no A2A tasks of its own"
	}
	return ""
}

// SessionMover lists the environments a daemon chat's session can move to and
// moves it there, for /environments and /transfer.
type SessionMover interface {
	Placement() SessionPlacement
	ListEnvironments(ctx context.Context) ([]agentdomain.EnvironmentStatus, error)
	Transfer(target string, files []string) error
	CancelTransfer() error
}

// SessionPlacementChangedEvent reports where the session runs now. Moved is
// set once a transfer landed, with the interrupts the move cancelled.
type SessionPlacementChangedEvent struct {
	Placement           SessionPlacement
	Moved               bool
	CancelledInterrupts int
}

// FollowRunEvent asks an idle daemon chat to follow a run it did not start:
// another client's turn, or a run suspended on a prompt it should answer.
type FollowRunEvent struct{}

// ShowEnvironmentsEvent opens the environments picker.
type ShowEnvironmentsEvent struct{}

// EnvironmentsLoadedEvent carries the environments the session can move to,
// or why they could not be listed.
type EnvironmentsLoadedEvent struct {
	Environments []agentdomain.EnvironmentStatus
	Err          error
}

// TransferRequestedEvent asks to move the session to Target, with Files
// git does not track carried along.
type TransferRequestedEvent struct {
	Target string
	Files  []string
}

// TransferProgressEvent reports one stage of the session's running transfer.
type TransferProgressEvent struct {
	Progress agentdomain.TransferProgress
}
