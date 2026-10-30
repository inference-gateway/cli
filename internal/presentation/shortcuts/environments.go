package shortcuts

import (
	"context"

	config "github.com/inference-gateway/cli/config"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
)

// EnvironmentsShortcut opens the picker of the environments a daemon chat's
// session can run on.
type EnvironmentsShortcut struct{}

// NewEnvironmentsShortcut creates the /environments shortcut.
func NewEnvironmentsShortcut() *EnvironmentsShortcut { return &EnvironmentsShortcut{} }

func (e *EnvironmentsShortcut) GetName() string { return "environments" }
func (e *EnvironmentsShortcut) GetDescription() string {
	return "Show where this session runs and the environments it can move to"
}
func (e *EnvironmentsShortcut) GetUsage() string              { return "/environments" }
func (e *EnvironmentsShortcut) CanExecute(args []string) bool { return len(args) == 0 }

func (e *EnvironmentsShortcut) Execute(_ context.Context, _ []string) (ShortcutResult, error) {
	return ShortcutResult{Success: true, SideEffect: SideEffectShowEnvironments}, nil
}

// TransferShortcut moves a daemon chat's session to another environment, with
// the files git does not track that should travel along. Without a target it
// opens the picker.
type TransferShortcut struct {
	environments []config.EnvironmentEntry
}

// NewTransferShortcut creates the /transfer shortcut, completing the
// environments daemon.yaml names.
func NewTransferShortcut(environments []config.EnvironmentEntry) *TransferShortcut {
	return &TransferShortcut{environments: environments}
}

func (t *TransferShortcut) GetName() string { return "transfer" }
func (t *TransferShortcut) GetDescription() string {
	return "Move this session to another environment"
}
func (t *TransferShortcut) GetUsage() string           { return "/transfer [environment] [file...]" }
func (t *TransferShortcut) CanExecute(_ []string) bool { return true }

func (t *TransferShortcut) Execute(_ context.Context, args []string) (ShortcutResult, error) {
	if len(args) == 0 {
		return ShortcutResult{Success: true, SideEffect: SideEffectShowEnvironments}, nil
	}
	return ShortcutResult{
		Success:    true,
		SideEffect: SideEffectTransferSession,
		Data:       tui.TransferRequestedEvent{Target: args[0], Files: args[1:]},
	}, nil
}

// GetSubcommands completes the environments daemon.yaml names.
func (t *TransferShortcut) GetSubcommands() []Subcommand {
	subcommands := make([]Subcommand, 0, len(t.environments))
	for _, environment := range t.environments {
		subcommands = append(subcommands, Subcommand{
			Name:        environment.Name,
			Description: "Move the session to " + environment.Name + " (" + string(environment.Kind) + ")",
		})
	}
	return subcommands
}
