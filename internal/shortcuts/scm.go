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
	if err := s.checkGitRepository(ctx); err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("❌ Not in a git repository: %v", err),
			Success: false,
		}, nil
	}

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

	currentBranch, err := s.getCurrentBranch(ctx)
	if err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("❌ Failed to get current branch: %v", err),
			Success: false,
		}, nil
	}

	isMainBranch := currentBranch == "main" || currentBranch == "master"

	return ShortcutResult{
		Output:     fmt.Sprintf("%s🚀 Analyzing changes and generating PR plan...%s", colors.Magenta, colors.Reset),
		Success:    true,
		SideEffect: SideEffectGeneratePRPlan,
		Data: map[string]any{
			"context":       ctx,
			"diff":          diff,
			"currentBranch": currentBranch,
			"isMainBranch":  isMainBranch,
			"scmShortcut":   s,
		},
	}, nil
}

// GeneratePRPlan generates a concise PR plan by calling the LLM
func (s *SCMShortcut) GeneratePRPlan(ctx context.Context, diff, currentBranch string, isMainBranch bool) (string, error) {
	if s.client == nil {
		return "", fmt.Errorf("client not available")
	}

	model := s.config.SCM.PRCreate.Model
	if model == "" {
		model = s.config.Agent.Model
	}
	if model == "" && s.modelService != nil {
		model = s.modelService.GetCurrentModel()
	}
	if model == "" {
		return "", fmt.Errorf("no model configured (set scm.pr_create.model, agent.model, or select a model with /switch)")
	}

	baseBranch := s.config.SCM.PRCreate.BaseBranch
	if baseBranch == "" {
		baseBranch = "main"
	}

	systemPrompt := s.getPRPlanSystemPrompt()

	userPrompt := s.buildPRPlanUserPrompt(diff, currentBranch, isMainBranch, baseBranch)

	messages := []sdk.Message{
		{Role: sdk.System, Content: sdk.NewMessageContent(systemPrompt)},
		{Role: sdk.User, Content: sdk.NewMessageContent(userPrompt)},
	}

	slashIndex := strings.Index(model, "/")
	if slashIndex == -1 {
		return "", fmt.Errorf("invalid model format, expected 'provider/model'")
	}

	provider := model[:slashIndex]
	modelName := strings.TrimPrefix(model, provider+"/")
	providerType := sdk.Provider(provider)

	response, err := s.client.
		WithOptions(&sdk.CreateChatCompletionRequest{
			MaxTokens: &[]int{500}[0],
		}).
		WithMiddlewareOptions(&sdk.MiddlewareOptions{
			SkipMCP: true,
		}).
		GenerateContent(ctx, providerType, modelName, messages)
	if err != nil {
		return "", fmt.Errorf("failed to generate PR plan: %w", err)
	}

	if len(response.Choices) == 0 {
		return "", fmt.Errorf("no PR plan generated")
	}

	contentStr, err := response.Choices[0].Message.Content.AsMessageContent0()
	if err != nil {
		return "", fmt.Errorf("failed to extract PR plan content: %w", err)
	}

	return strings.TrimSpace(contentStr), nil
}

// getPRPlanSystemPrompt returns the system prompt for PR plan generation
func (s *SCMShortcut) getPRPlanSystemPrompt() string {
	return `You are a helpful assistant that analyzes code changes and generates a concise PR creation plan.

Your task is to analyze the diff and output a structured plan with EXACTLY this format (each field on its own line):

**Branch:** feat/descriptive-branch-name

**Commit:** type: concise commit message

**PR Title:** Clear, descriptive title

**PR Description:**
Brief summary of changes (2-3 sentences max)

REQUIREMENTS:
- Each field must be on its own separate line with a blank line between fields
- Branch name: Use conventional format (feat/, fix/, docs/, refactor/, chore/)
- Commit message: Follow conventional commits, under 50 chars, first letter after colon must be capitalized (e.g., "feat: Add new feature")
- PR Title: Clear and descriptive
- PR Description: Brief, focused on what and why
- NEVER use words like "comprehensive", "enhance", "robust", or other filler adjectives
- Use simple, direct verbs: Add, Fix, Update, Remove, Refactor, etc.

Output ONLY the plan in the format above. No explanations or additional text.`
}

// buildPRPlanUserPrompt builds the user prompt for PR plan generation
func (s *SCMShortcut) buildPRPlanUserPrompt(diff, currentBranch string, isMainBranch bool, baseBranch string) string {
	var sb strings.Builder

	sb.WriteString("Analyze these changes and generate a PR plan:\n\n")

	sb.WriteString(fmt.Sprintf("Current branch: %s\n", currentBranch))
	sb.WriteString(fmt.Sprintf("Base branch: %s\n", baseBranch))
	if isMainBranch {
		sb.WriteString("Status: On main branch - will create a new feature branch\n")
	} else {
		sb.WriteString("Status: Already on a feature branch\n")
	}

	sb.WriteString(fmt.Sprintf("\n```diff\n%s\n```", truncateDiff(diff, 6000)))

	return sb.String()
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
	stagedCmd := exec.CommandContext(ctx, "git", "diff", "--cached")
	stagedOutput, err := stagedCmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get staged diff: %w", err)
	}

	if len(strings.TrimSpace(string(stagedOutput))) > 0 {
		return string(stagedOutput), nil
	}

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

// truncateDiff truncates the diff to a maximum length to avoid token limits
func truncateDiff(diff string, maxLen int) string {
	if len(diff) <= maxLen {
		return diff
	}
	return diff[:maxLen] + "\n... (diff truncated for brevity)"
}
