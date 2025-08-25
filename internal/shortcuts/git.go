package shortcuts

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	config "github.com/inference-gateway/cli/config"
	colors "github.com/inference-gateway/cli/internal/ui/styles/colors"
	icons "github.com/inference-gateway/cli/internal/ui/styles/icons"
)

// GitShortcut handles common git operations (status, pull, log, etc.)
// Commit and push operations are handled by dedicated shortcuts
type GitShortcut struct {
	config *config.Config
}

// NewGitShortcut creates a new git shortcut
func NewGitShortcut(config *config.Config) *GitShortcut {
	return &GitShortcut{
		config: config,
	}
}

func (g *GitShortcut) GetName() string {
	return "git"
}

func (g *GitShortcut) GetDescription() string {
	return "Execute git commands (status, pull, log, etc.)"
}

func (g *GitShortcut) GetUsage() string {
	return "/git <command> [args...] (e.g., /git status, /git pull, /git log). Use /git commit and /git push for dedicated commit/push operations."
}

func (g *GitShortcut) CanExecute(args []string) bool {
	return len(args) >= 1
}

func (g *GitShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	if len(args) == 0 {
		return ShortcutResult{
			Output:  "No git command specified. " + g.GetUsage(),
			Success: false,
		}, nil
	}

	command := args[0]

	// Redirect commit and push to dedicated shortcuts
	if command == "commit" {
		return ShortcutResult{
			Output:  "Please use `/git commit` for commit operations with AI integration, or `git commit -m \"message\"` for manual commits.",
			Success: false,
		}, nil
	}
	if command == "push" {
		return ShortcutResult{
			Output:  "Please use `/git push` for push operations.",
			Success: false,
		}, nil
	}

	gitArgs := append([]string{"git"}, args...)
	cmd := exec.CommandContext(ctx, gitArgs[0], gitArgs[1:]...)

	output, err := cmd.CombinedOutput()
	outputStr := strings.TrimSpace(string(output))

	if err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("Git command failed: %s\n\nOutput:\n%s", err.Error(), outputStr),
			Success: false,
		}, nil
	}

	switch command {
	case "status":
		return g.formatStatusOutput(outputStr), nil
	case "pull":
		return g.formatPullOutput(outputStr), nil
	case "log":
		return g.formatLogOutput(outputStr), nil
	default:
		return ShortcutResult{
			Output:  fmt.Sprintf("%s %sGit %s completed successfully%s\n\n%s", icons.CheckMark, colors.Green, command, colors.Reset, outputStr),
			Success: true,
		}, nil
	}
}

func (g *GitShortcut) formatStatusOutput(output string) ShortcutResult {
	if output == "" {
		return ShortcutResult{
			Output:  fmt.Sprintf("%s %sWorking tree clean - no changes to commit%s", icons.CheckMark, colors.Green, colors.Reset),
			Success: true,
		}
	}

	return ShortcutResult{
		Output:  fmt.Sprintf("%s**Git Status**%s\n\n```\n%s\n```", colors.Blue, colors.Reset, output),
		Success: true,
	}
}

func (g *GitShortcut) formatPullOutput(output string) ShortcutResult {
	if strings.Contains(output, "Already up to date") {
		return ShortcutResult{
			Output:  fmt.Sprintf("%s %sRepository is already up to date%s", icons.CheckMark, colors.Green, colors.Reset),
			Success: true,
		}
	}

	return ShortcutResult{
		Output:  fmt.Sprintf("%s %s**Pull Completed**%s\n\n```\n%s\n```", icons.CheckMark, colors.Green, colors.Reset, output),
		Success: true,
	}
}

func (g *GitShortcut) formatLogOutput(output string) ShortcutResult {
	return ShortcutResult{
		Output:  fmt.Sprintf("%s**Git Log**%s\n\n```\n%s\n```", colors.Blue, colors.Reset, output),
		Success: true,
	}
}
