package shortcuts

import (
	"context"
	"strings"

	setup "github.com/inference-gateway/cli/internal/github/setup"
)

// InstallOpentaskShortcut submits the canonical OpenTask install prompt as a
// regular chat message; its leading "/opentask" mention activates the catalog
// skill that guides the agent to create .github/workflows/tasks.yml.
type InstallOpentaskShortcut struct{}

// NewInstallOpentaskShortcut creates a new install-opentask shortcut.
func NewInstallOpentaskShortcut() *InstallOpentaskShortcut { return &InstallOpentaskShortcut{} }

func (c *InstallOpentaskShortcut) GetName() string { return "install-opentask" }
func (c *InstallOpentaskShortcut) GetDescription() string {
	return "Prompt the agent to install or update the OpenTask GitHub workflow"
}
func (c *InstallOpentaskShortcut) GetUsage() string {
	return "/install-opentask [owner/repo] [extra instructions...]"
}
func (c *InstallOpentaskShortcut) CanExecute(_ []string) bool { return true }

func (c *InstallOpentaskShortcut) Execute(_ context.Context, args []string) (ShortcutResult, error) {
	repo := ""
	rest := args
	if len(args) > 0 {
		repo = args[0]
		rest = args[1:]
	}
	return ShortcutResult{
		Success:    true,
		SideEffect: SideEffectSendMessage,
		Data:       setup.InstallChatPrompt(repo, strings.Join(rest, " ")),
	}, nil
}
