package shortcuts

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	config "github.com/inference-gateway/cli/config"
	colors "github.com/inference-gateway/cli/internal/ui/styles/colors"
	icons "github.com/inference-gateway/cli/internal/ui/styles/icons"
	sdk "github.com/inference-gateway/sdk"
)

// GitCommitShortcut handles git commit operations with AI-generated messages
type GitCommitShortcut struct {
	commitClient sdk.Client
	config       *config.Config
}

// NewGitCommitShortcut creates a new git commit shortcut
func NewGitCommitShortcut(commitClient sdk.Client, config *config.Config) *GitCommitShortcut {
	return &GitCommitShortcut{
		commitClient: commitClient,
		config:       config,
	}
}

func (g *GitCommitShortcut) GetName() string {
	return "git commit"
}

func (g *GitCommitShortcut) GetDescription() string {
	return "Commit staged changes with AI-generated message"
}

func (g *GitCommitShortcut) GetUsage() string {
	return "/git commit [additional git commit flags]"
}

func (g *GitCommitShortcut) CanExecute(args []string) bool {
	return true
}

func (g *GitCommitShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	if g.hasCommitMessage(args) {
		return g.executeCommitWithMessage(ctx, args)
	}

	return g.handleSmartCommit(ctx, args)
}

// hasCommitMessage checks if the commit command already has a message
func (g *GitCommitShortcut) hasCommitMessage(args []string) bool {
	for i, arg := range args {
		if arg == "-m" || arg == "--message" {
			return i+1 < len(args)
		}
		if strings.HasPrefix(arg, "-m=") || strings.HasPrefix(arg, "--message=") {
			return true
		}
	}
	return false
}

// executeCommitWithMessage executes commit with provided message
func (g *GitCommitShortcut) executeCommitWithMessage(ctx context.Context, args []string) (ShortcutResult, error) {
	gitArgs := append([]string{"git", "commit"}, args...)
	cmd := exec.CommandContext(ctx, gitArgs[0], gitArgs[1:]...)

	output, err := cmd.CombinedOutput()
	outputStr := strings.TrimSpace(string(output))

	if err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("Git commit failed: %s\n\nOutput:\n%s", err.Error(), outputStr),
			Success: false,
		}, nil
	}

	return g.formatCommitOutput(outputStr), nil
}

// handleSmartCommit generates an AI commit message and commits
func (g *GitCommitShortcut) handleSmartCommit(ctx context.Context, args []string) (ShortcutResult, error) {
	statusCmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	statusOutput, err := statusCmd.Output()
	if err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("Failed to check git status: %v", err),
			Success: false,
		}, nil
	}

	if len(strings.TrimSpace(string(statusOutput))) == 0 {
		return ShortcutResult{
			Output:  fmt.Sprintf("%sNo changes staged for commit. Use `git add` to stage changes first.%s", colors.Amber, colors.Reset),
			Success: false,
		}, nil
	}

	diffCmd := exec.CommandContext(ctx, "git", "diff", "--cached")
	diffOutput, err := diffCmd.Output()
	if err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("Failed to get diff: %v", err),
			Success: false,
		}, nil
	}

	if len(strings.TrimSpace(string(diffOutput))) == 0 {
		return ShortcutResult{
			Output:  fmt.Sprintf("%sNo staged changes found. Use `git add` to stage changes first.%s", colors.Amber, colors.Reset),
			Success: false,
		}, nil
	}

	// Show user feedback before starting generation
	return ShortcutResult{
		Output:     fmt.Sprintf("%sGenerating AI commit message from staged changes...%s", colors.Magenta, colors.Reset),
		Success:    true,
		SideEffect: SideEffectGenerateCommit,
		Data: map[string]interface{}{
			"context":           ctx,
			"args":              args,
			"diff":              string(diffOutput),
			"gitCommitShortcut": g,
		},
	}, nil
}

// PerformCommit executes the actual commit with AI-generated message (called by side effect handler)
func (g *GitCommitShortcut) PerformCommit(ctx context.Context, args []string, diff string) (string, error) {
	commitMessage, err := g.generateCommitMessage(ctx, diff)
	if err != nil {
		return "", fmt.Errorf("failed to generate commit message: %w", err)
	}

	if strings.TrimSpace(commitMessage) == "" {
		return "", fmt.Errorf("generated commit message is empty")
	}

	commitArgs := append([]string{"git", "commit", "-m", commitMessage}, args...)
	commitCmd := exec.CommandContext(ctx, commitArgs[0], commitArgs[1:]...)
	commitOutput, err := commitCmd.CombinedOutput()

	if err != nil {
		return "", fmt.Errorf("commit failed: %v\n\nOutput:\n%s\n\nGenerated message was: %s", err, string(commitOutput), commitMessage)
	}

	return fmt.Sprintf("%s %s**AI-Generated Commit Created**%s\n\n%s**Message:**%s %s\n\n```\n%s\n```",
		icons.CheckMark, colors.Green, colors.Reset, colors.Blue, colors.Reset, commitMessage, strings.TrimSpace(string(commitOutput))), nil
}

// generateCommitMessage uses AI to generate a commit message from the diff
func (g *GitCommitShortcut) generateCommitMessage(ctx context.Context, diff string) (string, error) {
	if g.commitClient == nil {
		return "", fmt.Errorf("commit client not available")
	}

	model := g.config.Git.CommitMessage.Model
	if model == "" {
		model = g.config.Agent.Model
	}
	if model == "" {
		return "", fmt.Errorf("no model configured for commit messages (set git.commit_message.model or agent.model)")
	}

	systemPrompt := g.config.Git.CommitMessage.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = `Generate a concise git commit message following conventional commit format.

REQUIREMENTS:
- MUST use format: "type: Brief description"
- MUST be under 50 characters total
- MUST use imperative mood (e.g., "Add", "Fix", "Update")
- Types: feat, fix, docs, style, refactor, test, chore

Respond with ONLY the commit message, no quotes or explanation.`
	}

	messages := []sdk.Message{
		{Role: sdk.System, Content: systemPrompt},
		{Role: sdk.User, Content: fmt.Sprintf("%s\n\nGenerate a commit message for these changes:\n\n```diff\n%s\n```", systemPrompt, diff)},
	}

	slashIndex := strings.Index(model, "/")
	if slashIndex == -1 {
		return "", fmt.Errorf("invalid model format, expected 'provider/model'")
	}

	provider := model[:slashIndex]
	modelName := strings.TrimPrefix(model, provider+"/")
	providerType := sdk.Provider(provider)

	response, err := g.commitClient.
		WithOptions(&sdk.CreateChatCompletionRequest{
			MaxTokens: &[]int{100}[0],
		}).
		WithMiddlewareOptions(&sdk.MiddlewareOptions{
			SkipMCP: true,
			SkipA2A: true,
		}).
		GenerateContent(ctx, providerType, modelName, messages)
	if err != nil {
		return "", fmt.Errorf("failed to generate commit message: %w", err)
	}

	if len(response.Choices) == 0 {
		return "", fmt.Errorf("no commit message generated")
	}

	message := strings.TrimSpace(response.Choices[0].Message.Content)
	message = strings.Trim(message, `"'`)
	return message, nil
}

// formatCommitOutput formats the commit command output
func (g *GitCommitShortcut) formatCommitOutput(output string) ShortcutResult {
	if strings.Contains(output, "nothing to commit") {
		return ShortcutResult{
			Output:  fmt.Sprintf("%sNothing to commit - working tree clean%s", colors.Gray, colors.Reset),
			Success: true,
		}
	}

	return ShortcutResult{
		Output:  fmt.Sprintf("%s %s**Commit Created**%s\n\n```\n%s\n```", icons.CheckMark, colors.Green, colors.Reset, output),
		Success: true,
	}
}
