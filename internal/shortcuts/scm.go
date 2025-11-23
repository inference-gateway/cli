package shortcuts

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	colors "github.com/inference-gateway/cli/internal/ui/styles/colors"
	sdk "github.com/inference-gateway/sdk"
)

// SCMShortcut handles source control management operations for creating PRs
type SCMShortcut struct {
	client       sdk.Client
	config       *config.Config
	modelService domain.ModelService
}

// NewSCMShortcut creates a new SCM shortcut
func NewSCMShortcut(client sdk.Client, cfg *config.Config, modelService domain.ModelService) *SCMShortcut {
	return &SCMShortcut{
		client:       client,
		config:       cfg,
		modelService: modelService,
	}
}

func (s *SCMShortcut) GetName() string {
	return "scm"
}

func (s *SCMShortcut) GetDescription() string {
	return "Source control management (e.g., /scm pr create)"
}

func (s *SCMShortcut) GetUsage() string {
	return "/scm pr create - Create a PR with AI-generated branch name, commit message, and PR description"
}

func (s *SCMShortcut) CanExecute(args []string) bool {
	if len(args) < 2 {
		return false
	}
	// Only support "pr create" for now
	return args[0] == "pr" && args[1] == "create"
}

func (s *SCMShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	if len(args) < 2 {
		return ShortcutResult{
			Output:  "Invalid usage. " + s.GetUsage(),
			Success: false,
		}, nil
	}

	subcommand := args[0]
	action := args[1]

	if subcommand == "pr" && action == "create" {
		return s.executePRCreate(ctx)
	}

	return ShortcutResult{
		Output:  fmt.Sprintf("Unknown SCM command: %s %s. %s", subcommand, action, s.GetUsage()),
		Success: false,
	}, nil
}

// executePRCreate handles the PR creation workflow
func (s *SCMShortcut) executePRCreate(ctx context.Context) (ShortcutResult, error) {
	// Check if we're in a git repository
	if err := s.checkGitRepository(ctx); err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("❌ Not in a git repository: %v", err),
			Success: false,
		}, nil
	}

	// Check for uncommitted changes
	diff, err := s.getGitDiff(ctx)
	if err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("❌ Failed to get git diff: %v", err),
			Success: false,
		}, nil
	}

	if strings.TrimSpace(diff) == "" {
		return ShortcutResult{
			Output:  fmt.Sprintf("%sNo changes detected. Stage your changes with `git add` first.%s", colors.Amber, colors.Reset),
			Success: false,
		}, nil
	}

	// Get the current branch to check if we're on main/master
	currentBranch, err := s.getCurrentBranch(ctx)
	if err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("❌ Failed to get current branch: %v", err),
			Success: false,
		}, nil
	}

	// Check if already on a feature branch (not main/master)
	isMainBranch := currentBranch == "main" || currentBranch == "master"

	// Return with SideEffectSetInput to populate the input with a prompt for the LLM
	prompt := s.buildPRCreationPrompt(diff, currentBranch, isMainBranch)

	return ShortcutResult{
		Output:     fmt.Sprintf("%s🚀 Preparing PR creation workflow...%s\n\nReview the prompt below and press Enter to let the AI generate your branch, commit, and PR.", colors.Magenta, colors.Reset),
		Success:    true,
		SideEffect: SideEffectSetInput,
		Data:       prompt,
	}, nil
}

// checkGitRepository verifies we're in a git repository
func (s *SCMShortcut) checkGitRepository(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--git-dir")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("not a git repository")
	}
	return nil
}

// getGitDiff returns the current git diff (staged + unstaged)
func (s *SCMShortcut) getGitDiff(ctx context.Context) (string, error) {
	// First try to get staged changes
	stagedCmd := exec.CommandContext(ctx, "git", "diff", "--cached")
	stagedOutput, err := stagedCmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get staged diff: %w", err)
	}

	// If there are staged changes, return those
	if len(strings.TrimSpace(string(stagedOutput))) > 0 {
		return string(stagedOutput), nil
	}

	// Otherwise get unstaged changes
	unstagedCmd := exec.CommandContext(ctx, "git", "diff")
	unstagedOutput, err := unstagedCmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get unstaged diff: %w", err)
	}

	return string(unstagedOutput), nil
}

// getCurrentBranch returns the current git branch name
func (s *SCMShortcut) getCurrentBranch(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "branch", "--show-current")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get current branch: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// buildPRCreationPrompt builds the prompt for AI to generate PR details
func (s *SCMShortcut) buildPRCreationPrompt(diff, currentBranch string, isMainBranch bool) string {
	// Use custom template from config if available
	template := s.config.SCM.PRCreate.Prompt
	if template == "" {
		template = defaultPRCreatePrompt()
	}

	// Build context about current state
	var contextInfo strings.Builder
	contextInfo.WriteString("## Current Git State\n")
	contextInfo.WriteString(fmt.Sprintf("- Current branch: `%s`\n", currentBranch))
	if isMainBranch {
		contextInfo.WriteString("- Status: On main/master branch - will need to create a new feature branch\n")
	} else {
		contextInfo.WriteString("- Status: Already on a feature branch\n")
	}
	contextInfo.WriteString(fmt.Sprintf("\n## Changes to be committed\n```diff\n%s\n```\n", truncateDiff(diff, 8000)))

	// Combine template with context
	return fmt.Sprintf("%s\n\n%s", template, contextInfo.String())
}

// truncateDiff truncates the diff to a maximum length to avoid token limits
func truncateDiff(diff string, maxLen int) string {
	if len(diff) <= maxLen {
		return diff
	}
	return diff[:maxLen] + "\n... (diff truncated for brevity)"
}

// defaultPRCreatePrompt returns the default prompt template for PR creation
func defaultPRCreatePrompt() string {
	return `Please help me create a pull request with the following workflow:

1. **Analyze the changes** and determine:
   - A descriptive branch name (use conventional format like feat/, fix/, docs/, refactor/)
   - A concise commit message following conventional commits
   - A PR title and description

2. **Execute the git workflow**:
   - If on main/master: Create and checkout a new branch
   - Stage all changes with git add
   - Commit with the generated message
   - Push to the remote repository
   - Create a pull request using the GitHub CLI (gh pr create)

3. **Cleanup** (after PR is created):
   - Return to the original branch (main/master)
   - Optionally delete the local feature branch if requested

Please proceed with analyzing the changes below and executing the workflow. Ask for confirmation before each destructive action.`
}
