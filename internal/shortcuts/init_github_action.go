package shortcuts

import (
	"context"
	"fmt"

	icons "github.com/inference-gateway/cli/internal/ui/styles/icons"
)

// InitGithubActionShortcut helps setup Init GitHub Action for infer-action
type InitGithubActionShortcut struct{}

// NewInitGithubActionShortcut creates a new Init GitHub Action setup shortcut
func NewInitGithubActionShortcut() *InitGithubActionShortcut {
	return &InitGithubActionShortcut{}
}

func (g *InitGithubActionShortcut) GetName() string {
	return "init-github-action"
}

func (g *InitGithubActionShortcut) GetDescription() string {
	return "Setup GitHub App for infer-action bot identity (interactive wizard)"
}

func (g *InitGithubActionShortcut) GetUsage() string {
	return "/init-github-action"
}

func (g *InitGithubActionShortcut) CanExecute(args []string) bool {
	return len(args) == 0
}

func (g *InitGithubActionShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	return ShortcutResult{
		Output:     fmt.Sprintf("%s Launching GitHub App Setup Wizard...", icons.Robot),
		Success:    true,
		SideEffect: SideEffectShowInitGithubActionSetup,
	}, nil
}
