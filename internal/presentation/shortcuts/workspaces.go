package shortcuts

import (
	"context"

	config "github.com/inference-gateway/cli/config"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
)

// NewWorkspacesShortcut opens the picker of the workspaces a daemon chat's
// session can run on.
func NewWorkspacesShortcut() *PanelShortcut {
	return &PanelShortcut{name: "workspaces", description: "Show where this session runs and the workspaces it can teleport to", sideEffect: SideEffectShowWorkspaces}
}

// TeleportShortcut teleports a daemon chat's session to another workspace, with
// the files git does not track that should travel along. Without a target it
// opens the picker.
type TeleportShortcut struct {
	workspaces []config.WorkspaceEntry
}

// NewTeleportShortcut creates the /teleport shortcut, completing the
// workspaces daemon.yaml names.
func NewTeleportShortcut(workspaces []config.WorkspaceEntry) *TeleportShortcut {
	return &TeleportShortcut{workspaces: workspaces}
}

func (t *TeleportShortcut) GetName() string { return "teleport" }
func (t *TeleportShortcut) GetDescription() string {
	return "Teleport this session to another workspace"
}
func (t *TeleportShortcut) GetUsage() string           { return "/teleport [workspace] [file...]" }
func (t *TeleportShortcut) CanExecute(_ []string) bool { return true }

func (t *TeleportShortcut) Execute(_ context.Context, args []string) (ShortcutResult, error) {
	if len(args) == 0 {
		return ShortcutResult{Success: true, SideEffect: SideEffectShowWorkspaces}, nil
	}
	return ShortcutResult{
		Success:    true,
		SideEffect: SideEffectTeleportSession,
		Data:       tui.TeleportRequestedEvent{Target: args[0], Files: args[1:]},
	}, nil
}

// GetSubcommands completes the workspaces daemon.yaml names.
func (t *TeleportShortcut) GetSubcommands() []Subcommand {
	subcommands := make([]Subcommand, 0, len(t.workspaces))
	for _, workspace := range t.workspaces {
		subcommands = append(subcommands, Subcommand{
			Name:        workspace.Name,
			Description: "Teleport the session to " + workspace.Name + " (" + string(workspace.Kind) + ")",
		})
	}
	return subcommands
}
