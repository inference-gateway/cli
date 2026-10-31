package tui

import (
	"context"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// SessionPlacement is where a daemon chat's session runs: Local is the
// workspace of the daemon the TUI talks to and Workspace the one the
// session lives on. The zero value is an in-process chat.
type SessionPlacement struct {
	Daemon       bool
	Local        string
	Workspace    string
	WorkspaceDir string
	Teleporting  string
	Offline      bool
}

// Remote reports whether the session runs on a workspace other than the
// daemon the TUI talks to, so laptop-only features do not apply to it.
func (p SessionPlacement) Remote() bool {
	return p.Daemon && p.Workspace != "" && p.Workspace != p.Local
}

// Where names the workspace the session runs on.
func (p SessionPlacement) Where() string {
	if p.Workspace != "" {
		return p.Workspace
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
			return "the session runs on " + p.Workspace + ", whose checkout is not this one"
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

// SessionTeleporter lists the workspaces a daemon chat's session can teleport to and
// teleports it there, for /workspaces and /teleport.
type SessionTeleporter interface {
	Placement() SessionPlacement
	ListWorkspaces(ctx context.Context) ([]agentdomain.WorkspaceStatus, error)
	Teleport(target string, files []string) error
	CancelTeleport() error
}

// SessionPlacementChangedEvent reports where the session runs now. Teleported is
// set once a teleport landed, with the interrupts the teleport cancelled.
type SessionPlacementChangedEvent struct {
	Placement           SessionPlacement
	Teleported          bool
	CancelledInterrupts int
}

// FollowRunEvent asks an idle daemon chat to follow a run it did not start:
// another client's turn, or a run suspended on a prompt it should answer.
type FollowRunEvent struct{}

// ShowWorkspacesEvent opens the workspaces picker.
type ShowWorkspacesEvent struct{}

// WorkspacesLoadedEvent carries the workspaces the session can teleport to,
// or why they could not be listed.
type WorkspacesLoadedEvent struct {
	Workspaces []agentdomain.WorkspaceStatus
	Err        error
}

// TeleportRequestedEvent asks to teleport the session to Target, with Files
// git does not track carried along.
type TeleportRequestedEvent struct {
	Target string
	Files  []string
}

// TeleportProgressEvent reports one stage of the session's running teleport.
type TeleportProgressEvent struct {
	Progress agentdomain.TeleportProgress
}
