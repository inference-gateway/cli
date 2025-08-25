package shortcuts

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	config "github.com/inference-gateway/cli/config"
	sdk "github.com/inference-gateway/sdk"
)

// GitShortcut handles common git operations
type GitShortcut struct {
	commitClient sdk.Client
	config       *config.Config
}

// NewGitShortcut creates a new git shortcut
func NewGitShortcut(commitClient sdk.Client, config *config.Config) *GitShortcut {
	return &GitShortcut{
		commitClient: commitClient,
		config:       config,
	}
}

func (g *GitShortcut) GetName() string {
	return "git"
}

func (g *GitShortcut) GetDescription() string {
	return "Execute git commands (commit, push, status, etc.)"
}

func (g *GitShortcut) GetUsage() string {
	return "/git <command> [args...] (e.g., /git status, /git commit, /git commit -m \"message\", /git push)"
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

	// Handle special cases before executing command
	command := args[0]
	if command == "commit" && !g.hasCommitMessage(args) {
		return g.handleSmartCommit(ctx, args)
	}

	// Build the git command
	gitArgs := append([]string{"git"}, args...)
	cmd := exec.CommandContext(ctx, gitArgs[0], gitArgs[1:]...)

	// Execute the command
	output, err := cmd.CombinedOutput()
	outputStr := strings.TrimSpace(string(output))

	if err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("Git command failed: %s\n\nOutput:\n%s", err.Error(), outputStr),
			Success: false,
		}, nil
	}

	// Format output based on the git command
	switch command {
	case "status":
		return g.formatStatusOutput(outputStr), nil
	case "commit":
		return g.formatCommitOutput(outputStr), nil
	case "push":
		return g.formatPushOutput(outputStr), nil
	case "pull":
		return g.formatPullOutput(outputStr), nil
	case "log":
		return g.formatLogOutput(outputStr), nil
	default:
		return ShortcutResult{
			Output:  fmt.Sprintf("✅ Git %s completed successfully\n\n%s", command, outputStr),
			Success: true,
		}, nil
	}
}

func (g *GitShortcut) formatStatusOutput(output string) ShortcutResult {
	if output == "" {
		return ShortcutResult{
			Output:  "✅ Working tree clean - no changes to commit",
			Success: true,
		}
	}

	return ShortcutResult{
		Output:  fmt.Sprintf("📋 **Git Status**\n\n```\n%s\n```", output),
		Success: true,
	}
}

func (g *GitShortcut) formatCommitOutput(output string) ShortcutResult {
	if strings.Contains(output, "nothing to commit") {
		return ShortcutResult{
			Output:  "ℹ️ Nothing to commit - working tree clean",
			Success: true,
		}
	}

	return ShortcutResult{
		Output:  fmt.Sprintf("✅ **Commit Created**\n\n```\n%s\n```", output),
		Success: true,
	}
}

func (g *GitShortcut) formatPushOutput(output string) ShortcutResult {
	if output == "" {
		return ShortcutResult{
			Output:  "✅ Successfully pushed to remote repository",
			Success: true,
		}
	}

	return ShortcutResult{
		Output:  fmt.Sprintf("✅ **Push Completed**\n\n```\n%s\n```", output),
		Success: true,
	}
}

func (g *GitShortcut) formatPullOutput(output string) ShortcutResult {
	if strings.Contains(output, "Already up to date") {
		return ShortcutResult{
			Output:  "✅ Repository is already up to date",
			Success: true,
		}
	}

	return ShortcutResult{
		Output:  fmt.Sprintf("✅ **Pull Completed**\n\n```\n%s\n```", output),
		Success: true,
	}
}

func (g *GitShortcut) formatLogOutput(output string) ShortcutResult {
	return ShortcutResult{
		Output:  fmt.Sprintf("📜 **Git Log**\n\n```\n%s\n```", output),
		Success: true,
	}
}

// hasCommitMessage checks if the commit command already has a message
func (g *GitShortcut) hasCommitMessage(args []string) bool {
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

// handleSmartCommit generates an AI commit message and commits
func (g *GitShortcut) handleSmartCommit(ctx context.Context, args []string) (ShortcutResult, error) {
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
			Output:  "ℹ️ No changes staged for commit. Use `git add` to stage changes first.",
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
			Output:  "ℹ️ No staged changes found. Use `git add` to stage changes first.",
			Success: false,
		}, nil
	}

	// Show user feedback before starting generation
	return ShortcutResult{
		Output:     "🤖 Generating AI commit message from staged changes...",
		Success:    true,
		SideEffect: SideEffectGenerateCommit,
		Data: map[string]interface{}{
			"context":     ctx,
			"args":        args,
			"diff":        string(diffOutput),
			"gitShortcut": g,
		},
	}, nil
}

// generateCommitMessage uses AI to generate a commit message from the diff
func (g *GitShortcut) generateCommitMessage(ctx context.Context, diff string) (string, error) {
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

// PerformCommit executes the actual commit with AI-generated message (called by side effect handler)
func (g *GitShortcut) PerformCommit(ctx context.Context, args []string, diff string) (string, error) {
	commitMessage, err := g.generateCommitMessage(ctx, diff)
	if err != nil {
		return "", fmt.Errorf("failed to generate commit message: %w", err)
	}

	if strings.TrimSpace(commitMessage) == "" {
		return "", fmt.Errorf("generated commit message is empty")
	}

	commitArgs := append([]string{"git", "commit", "-m", commitMessage}, args[1:]...)
	commitCmd := exec.CommandContext(ctx, commitArgs[0], commitArgs[1:]...)
	commitOutput, err := commitCmd.CombinedOutput()

	if err != nil {
		return "", fmt.Errorf("commit failed: %v\n\nOutput:\n%s\n\nGenerated message was: %s", err, string(commitOutput), commitMessage)
	}

	return fmt.Sprintf("✅ **AI-Generated Commit Created**\n\n**Message:** %s\n\n```\n%s\n```", commitMessage, strings.TrimSpace(string(commitOutput))), nil
}
