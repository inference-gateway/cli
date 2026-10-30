package shortcuts

import (
	"context"

	config "github.com/inference-gateway/cli/config"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
)

// NewEnvironmentsShortcut opens the picker of the environments a daemon chat's
// session can run on.
func NewEnvironmentsShortcut() *PanelShortcut {
	return &PanelShortcut{name: "environments", description: "Show where this session runs and the environments it can teleport to", sideEffect: SideEffectShowEnvironments}
}

// TeleportShortcut teleports a daemon chat's session to another environment, with
// the files git does not track that should travel along. Without a target it
// opens the picker.
type TeleportShortcut struct {
	environments []config.EnvironmentEntry
}

// NewTeleportShortcut creates the /teleport shortcut, completing the
// environments daemon.yaml names.
func NewTeleportShortcut(environments []config.EnvironmentEntry) *TeleportShortcut {
	return &TeleportShortcut{environments: environments}
}

func (t *TeleportShortcut) GetName() string { return "teleport" }
func (t *TeleportShortcut) GetDescription() string {
	return "Teleport this session to another environment"
}
func (t *TeleportShortcut) GetUsage() string           { return "/teleport [environment] [file...]" }
func (t *TeleportShortcut) CanExecute(_ []string) bool { return true }

func (t *TeleportShortcut) Execute(_ context.Context, args []string) (ShortcutResult, error) {
	if len(args) == 0 {
		return ShortcutResult{Success: true, SideEffect: SideEffectShowEnvironments}, nil
	}
	return ShortcutResult{
		Success:    true,
		SideEffect: SideEffectTeleportSession,
		Data:       tui.TeleportRequestedEvent{Target: args[0], Files: args[1:]},
	}, nil
}

// GetSubcommands completes the environments daemon.yaml names.
func (t *TeleportShortcut) GetSubcommands() []Subcommand {
	subcommands := make([]Subcommand, 0, len(t.environments))
	for _, environment := range t.environments {
		subcommands = append(subcommands, Subcommand{
			Name:        environment.Name,
			Description: "Teleport the session to " + environment.Name + " (" + string(environment.Kind) + ")",
		})
	}
	return subcommands
}
