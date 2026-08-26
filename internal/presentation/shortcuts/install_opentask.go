package shortcuts

import (
	"context"
	"strings"

	setup "github.com/inference-gateway/cli/internal/github/setup"
)

// InstallOpentaskShortcut submits the canonical OpenTask workflow-install task
// as a regular chat message, so the run streams in the conversation like any
// other turn. The browser extension's Install tab sends this same shortcut,
// making the CLI the single source of truth for the install prompt.
type InstallOpentaskShortcut struct{}

// NewInstallOpentaskShortcut creates the /install-opentask shortcut.
func NewInstallOpentaskShortcut() *InstallOpentaskShortcut {
	return &InstallOpentaskShortcut{}
}

func (g *InstallOpentaskShortcut) GetName() string {
	return "install-opentask"
}

func (g *InstallOpentaskShortcut) GetDescription() string {
	return "Install the OpenTask GitHub workflow via the chat agent"
}

func (g *InstallOpentaskShortcut) GetUsage() string {
	return "/install-opentask [owner/repo] [extra context...]"
}

func (g *InstallOpentaskShortcut) CanExecute(args []string) bool {
	return true
}

func (g *InstallOpentaskShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	repo := ""
	rest := args
	if len(args) > 0 && strings.Contains(args[0], "/") {
		repo = args[0]
		rest = args[1:]
	}
	return ShortcutResult{
		Output:     "Sending the OpenTask install task to the agent...",
		Success:    true,
		SideEffect: SideEffectSendMessage,
		Data:       setup.InstallChatPrompt(repo, strings.Join(rest, " ")),
	}, nil
}
