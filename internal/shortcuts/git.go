package shortcuts

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	colors "github.com/inference-gateway/cli/internal/ui/styles/colors"
	icons "github.com/inference-gateway/cli/internal/ui/styles/icons"
	sdk "github.com/inference-gateway/sdk"
)

// GitShortcut handles all git operations (status, pull, log, commit, push, etc.)
type GitShortcut struct {
	commitClient sdk.Client
	config       *config.Config
	modelService domain.ModelService
}

// NewGitShortcut creates a new unified git shortcut
func NewGitShortcut(commitClient sdk.Client, config *config.Config, modelService domain.ModelService) *GitShortcut {
	return &GitShortcut{
		commitClient: commitClient,
		config:       config,
		modelService: modelService,
	}
}

func (g *GitShortcut) GetName() string {
	return "git"
}

func (g *GitShortcut) GetDescription() string {
	return "Execute git commands (status, pull, log, commit, push, etc.)"
}

func (g *GitShortcut) GetUsage() string {
	return "/git <command> [args...] (e.g., /git status, /git pull, /git log, /git commit, /git push)"
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

	switch command {
	case "commit":
		return g.executeCommit(ctx, args[1:])
	case "push":
		return g.executePush(ctx, args[1:])
	default:
		return g.executeGenericGitCommand(ctx, args)
	}
}

// executeGenericGitCommand handles standard git operations (status, pull, log, etc.)
func (g *GitShortcut) executeGenericGitCommand(ctx context.Context, args []string) (ShortcutResult, error) {
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

	command := args[0]
	switch command {
	case "status":
		return g.formatStatusOutput(outputStr), nil
	case "pull":
		return g.formatPullOutput(outputStr), nil
	case "log":
		return g.formatLogOutput(outputStr), nil
	default:
		return ShortcutResult{
			Output:  fmt.Sprintf("%s Git %s completed successfully\n\n%s", icons.StyledCheckMark(), command, outputStr),
			Success: true,
		}, nil
	}
}

// executeCommit handles git commit operations with AI-generated messages
func (g *GitShortcut) executeCommit(ctx context.Context, args []string) (ShortcutResult, error) {
	if g.hasCommitMessage(args) {
		return g.executeCommitWithMessage(ctx, args)
	}

	return g.handleSmartCommit(ctx, args)
}

// executePush handles git push operations
func (g *GitShortcut) executePush(ctx context.Context, args []string) (ShortcutResult, error) {
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

// executeCommitWithMessage executes commit with provided message
func (g *GitShortcut) executeCommitWithMessage(ctx context.Context, args []string) (ShortcutResult, error) {
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

	return ShortcutResult{
		Output:     fmt.Sprintf("%sGenerating AI commit message from staged changes...%s", colors.Magenta, colors.Reset),
		Success:    true,
		SideEffect: SideEffectGenerateCommit,
		Data: map[string]any{
			"context":     ctx,
			"args":        args,
			"diff":        string(diffOutput),
			"gitShortcut": g,
		},
	}, nil
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

	commitArgs := append([]string{"git", "commit", "-m", commitMessage}, args...)
	commitCmd := exec.CommandContext(ctx, commitArgs[0], commitArgs[1:]...)
	commitOutput, err := commitCmd.CombinedOutput()

	if err != nil {
		return "", fmt.Errorf("commit failed: %v\n\nOutput:\n%s\n\nGenerated message was: %s", err, string(commitOutput), commitMessage)
	}

	return fmt.Sprintf("%s %s**AI-Generated Commit Created**%s\n\n**Message:** %s\n\n```\n%s\n```",
		icons.StyledCheckMark(), colors.Blue, colors.Reset, commitMessage, strings.TrimSpace(string(commitOutput))), nil
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
	if model == "" && g.modelService != nil {
		model = g.modelService.GetCurrentModel()
	}
	if model == "" {
		return "", fmt.Errorf("no model configured for commit messages (set git.commit_message.model, agent.model, or select a model with /switch)")
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
		{Role: sdk.System, Content: sdk.NewMessageContent(systemPrompt)},
		{Role: sdk.User, Content: sdk.NewMessageContent(fmt.Sprintf("%s\n\nGenerate a commit message for these changes:\n\n```diff\n%s\n```", systemPrompt, diff))},
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
		}).
		GenerateContent(ctx, providerType, modelName, messages)
	if err != nil {
		return "", fmt.Errorf("failed to generate commit message: %w", err)
	}

	if len(response.Choices) == 0 {
		return "", fmt.Errorf("no commit message generated")
	}

	contentStr, err := response.Choices[0].Message.Content.AsMessageContent0()
	if err != nil {
		return "", fmt.Errorf("failed to extract commit message content: %w", err)
	}
	message := strings.TrimSpace(contentStr)
	message = strings.Trim(message, `"'`)
	return message, nil
}

// Output formatting functions
func (g *GitShortcut) formatStatusOutput(output string) ShortcutResult {
	if output == "" {
		return ShortcutResult{
			Output:  fmt.Sprintf("%s Working tree clean - no changes to commit", icons.StyledCheckMark()),
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
			Output:  fmt.Sprintf("%s Repository is already up to date", icons.StyledCheckMark()),
			Success: true,
		}
	}

	return ShortcutResult{
		Output:  fmt.Sprintf("%s **Pull Completed**\n\n```\n%s\n```", icons.StyledCheckMark(), output),
		Success: true,
	}
}

func (g *GitShortcut) formatLogOutput(output string) ShortcutResult {
	return ShortcutResult{
		Output:  fmt.Sprintf("%s**Git Log**%s\n\n```\n%s\n```", colors.Blue, colors.Reset, output),
		Success: true,
	}
}

func (g *GitShortcut) formatCommitOutput(output string) ShortcutResult {
	if strings.Contains(output, "nothing to commit") {
		return ShortcutResult{
			Output:  fmt.Sprintf("%sNothing to commit - working tree clean%s", colors.Gray, colors.Reset),
			Success: true,
		}
	}

	return ShortcutResult{
		Output:  fmt.Sprintf("%s **Commit Created**\n\n```\n%s\n```", icons.StyledCheckMark(), output),
		Success: true,
	}
}

func (g *GitShortcut) formatPushOutput(output string) ShortcutResult {
	if output == "" {
		return ShortcutResult{
			Output:  fmt.Sprintf("%s Successfully pushed to remote repository", icons.StyledCheckMark()),
			Success: true,
		}
	}

	return ShortcutResult{
		Output:  fmt.Sprintf("%s **Push Completed**\n\n```\n%s\n```", icons.StyledCheckMark(), output),
		Success: true,
	}
}
