package shortcuts

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	colors "github.com/inference-gateway/cli/internal/ui/styles/colors"
	icons "github.com/inference-gateway/cli/internal/ui/styles/icons"
)

// GitPushShortcut handles git push operations
type GitPushShortcut struct{}

// NewGitPushShortcut creates a new git push shortcut
func NewGitPushShortcut() *GitPushShortcut {
	return &GitPushShortcut{}
}

func (g *GitPushShortcut) GetName() string {
	return "git push"
}

func (g *GitPushShortcut) GetDescription() string {
	return "Push commits to remote repository"
}

func (g *GitPushShortcut) GetUsage() string {
	return "/git push [remote] [branch] [additional git push flags]"
}

func (g *GitPushShortcut) CanExecute(args []string) bool {
	return true // Accept any arguments
}

func (g *GitPushShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	gitArgs := append([]string{"git", "push"}, args...)
	cmd := exec.CommandContext(ctx, gitArgs[0], gitArgs[1:]...)

	output, err := cmd.CombinedOutput()
	outputStr := strings.TrimSpace(string(output))

	if err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("Git push failed: %s\n\nOutput:\n%s", err.Error(), outputStr),
			Success: false,
		}, nil
	}

	return g.formatPushOutput(outputStr), nil
}

// formatPushOutput formats the push command output
func (g *GitPushShortcut) formatPushOutput(output string) ShortcutResult {
	if output == "" {
		return ShortcutResult{
			Output:  fmt.Sprintf("%s %sSuccessfully pushed to remote repository%s", icons.CheckMark, colors.Green, colors.Reset),
			Success: true,
		}
	}

	return ShortcutResult{
		Output:  fmt.Sprintf("%s %s**Push Completed**%s\n\n```\n%s\n```", icons.CheckMark, colors.Green, colors.Reset, output),
		Success: true,
	}
}
