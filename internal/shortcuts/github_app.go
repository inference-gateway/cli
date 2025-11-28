package shortcuts

import (
	"context"
	"fmt"

	"github.com/inference-gateway/cli/internal/ui/styles/icons"
)

// GitHubAppShortcut helps setup GitHub App for infer-action
type GitHubAppShortcut struct{}

// NewGitHubAppShortcut creates a new GitHub App setup shortcut
func NewGitHubAppShortcut() *GitHubAppShortcut {
	return &GitHubAppShortcut{}
}

func (g *GitHubAppShortcut) GetName() string {
	return "init-github-action"
}

func (g *GitHubAppShortcut) GetDescription() string {
	return "Setup GitHub App for infer-action bot identity (interactive wizard)"
}

func (g *GitHubAppShortcut) GetUsage() string {
	return "/init-github-action"
}

func (g *GitHubAppShortcut) CanExecute(args []string) bool {
	return len(args) == 0
}

func (g *GitHubAppShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	return ShortcutResult{
		Output:     fmt.Sprintf("%s Launching GitHub App Setup Wizard...", icons.Robot),
		Success:    true,
		SideEffect: SideEffectShowGitHubAppSetup,
	}, nil
}
