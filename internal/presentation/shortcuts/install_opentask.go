package shortcuts

import (
	"context"
	"fmt"

	icons "github.com/inference-gateway/cli/internal/presentation/tui/styles/icons"
)

// InstallOpentaskShortcut helps setup Init GitHub Action for infer-action
type InstallOpentaskShortcut struct{}

// NewInstallOpentaskShortcut creates a new Init GitHub Action setup shortcut
func NewInstallOpentaskShortcut() *InstallOpentaskShortcut {
	return &InstallOpentaskShortcut{}
}

func (g *InstallOpentaskShortcut) GetName() string {
	return "install-opentask"
}

func (g *InstallOpentaskShortcut) GetDescription() string {
	return "Setup GitHub Action (interactive wizard)"
}

func (g *InstallOpentaskShortcut) GetUsage() string {
	return "/install-opentask"
}

func (g *InstallOpentaskShortcut) CanExecute(args []string) bool {
	return len(args) == 0
}

func (g *InstallOpentaskShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	return ShortcutResult{
		Output:     fmt.Sprintf("%s Launching GitHub App Setup Wizard...", icons.Robot),
		Success:    true,
		SideEffect: SideEffectShowInstallOpentaskSetup,
	}, nil
}
