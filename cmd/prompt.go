package cmd

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/container"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/services"
	sdk "github.com/inference-gateway/sdk"
	"github.com/spf13/cobra"
)

var promptCmd = &cobra.Command{
	Use:   "prompt [prompt_text]",
	Short: "Execute a one-off prompt in background mode",
	Long: `Execute a one-off prompt that runs in background mode until the task is complete.
This command can automatically fetch GitHub issues and work on them iteratively.

Examples:
  infer prompt "Please fix the github issue #123"
  infer prompt "Please fix the github owner/repo#456"
  infer prompt "Please fix the github https://github.com/owner/repo/issues/789"
  infer prompt "Optimize the database queries in the user service"`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		promptText := args[0]
		return executeBackgroundPrompt(promptText)
	},
}

// BackgroundExecutor handles background execution of prompts
type BackgroundExecutor struct {
	services       *container.ServiceContainer
	githubService  *services.GitHubService
	maxIterations  int
	currentContext string
}

// executeBackgroundPrompt executes a prompt in background mode
func executeBackgroundPrompt(promptText string) error {
	cfg, err := config.LoadConfig("")
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	serviceContainer := container.NewServiceContainer(cfg)
	githubService := services.NewGitHubService()

	executor := &BackgroundExecutor{
		services:      serviceContainer,
		githubService: githubService,
		maxIterations: 10, // Prevent infinite loops
	}

	fmt.Println("🚀 Starting background execution...")
	return executor.Execute(promptText)
}

// Execute runs the background prompt execution
func (e *BackgroundExecutor) Execute(promptText string) error {
	ctx := context.Background()

	// Check if this involves a GitHub issue
	if issueContext, err := e.extractGitHubIssueContext(ctx, promptText); err == nil && issueContext != "" {
		fmt.Println("📄 GitHub issue detected, fetching context...")
		e.currentContext = issueContext
		promptText = e.enhancePromptWithIssueContext(promptText, issueContext)
	}

	// Get available models
	models, err := e.getAvailableModels(ctx)
	if err != nil {
		return fmt.Errorf("failed to get available models: %w", err)
	}

	// Select or use default model
	model, err := e.selectModel(models)
	if err != nil {
		return fmt.Errorf("failed to select model: %w", err)
	}

	fmt.Printf("🤖 Using model: %s\n", model)

	// Create initial system prompt for background execution
	systemPrompt := e.createSystemPrompt()

	// Execute iteratively
	return e.executeIteratively(ctx, model, systemPrompt, promptText)
}

// extractGitHubIssueContext extracts and fetches GitHub issue information from prompt
func (e *BackgroundExecutor) extractGitHubIssueContext(ctx context.Context, promptText string) (string, error) {
	// Look for GitHub issue references in the prompt
	patterns := []string{
		`github\s+issue\s+#(\d+)`,                                       // "github issue #123"
		`github\s+(\w+/\w+)#(\d+)`,                                      // "github owner/repo#123"
		`github\s+(https://github\.com/[\w\-\.]+/[\w\-\.]+/issues/\d+)`, // "github https://github.com/..."
		`#(\d+)`, // "#123"
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(`(?i)` + pattern)
		matches := re.FindStringSubmatch(promptText)

		if len(matches) > 1 {
			return e.fetchIssueContext(ctx, matches)
		}
	}

	return "", fmt.Errorf("no GitHub issue reference found")
}

// fetchIssueContext fetches the GitHub issue context
func (e *BackgroundExecutor) fetchIssueContext(ctx context.Context, matches []string) (string, error) {
	var repository string
	var issueNumber int
	var err error

	// Parse based on match pattern
	if len(matches) == 2 {
		// Simple number pattern (#123 or issue #123)
		if issueNumber, err = strconv.Atoi(matches[1]); err != nil {
			return "", fmt.Errorf("invalid issue number: %s", matches[1])
		}

		// Try to infer repository from current directory or ask user
		repository = e.inferRepository()
		if repository == "" {
			return "", fmt.Errorf("could not determine repository for issue #%d. Please specify as owner/repo#%d", issueNumber, issueNumber)
		}
	} else if len(matches) == 3 {
		// owner/repo#123 pattern
		repository = matches[1]
		if issueNumber, err = strconv.Atoi(matches[2]); err != nil {
			return "", fmt.Errorf("invalid issue number: %s", matches[2])
		}
	} else if strings.HasPrefix(matches[1], "https://") {
		// Full URL pattern
		repository, issueNumber, err = e.githubService.ParseIssueReference(matches[1])
		if err != nil {
			return "", err
		}
	}

	// Fetch the issue
	issue, err := e.githubService.FetchIssue(ctx, repository, issueNumber)
	if err != nil {
		return "", fmt.Errorf("failed to fetch GitHub issue: %w", err)
	}

	fmt.Printf("📋 Fetched issue #%d: %s\n", issue.Number, issue.Title)
	return e.githubService.FormatIssueForPrompt(issue), nil
}

// inferRepository tries to infer the repository from the current directory
func (e *BackgroundExecutor) inferRepository() string {
	// This is a simplified implementation
	// In a real scenario, you might want to check git remotes, etc.
	// For now, return empty to require explicit specification
	return ""
}

// enhancePromptWithIssueContext enhances the original prompt with issue context
func (e *BackgroundExecutor) enhancePromptWithIssueContext(originalPrompt, issueContext string) string {
	return fmt.Sprintf("%s\n\nHere is the GitHub issue context:\n\n%s\n\nPlease analyze this issue and provide a solution.", originalPrompt, issueContext)
}

// getAvailableModels gets the list of available models
func (e *BackgroundExecutor) getAvailableModels(ctx context.Context) ([]string, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	models, err := e.services.GetModelService().ListModels(timeoutCtx)
	if err != nil {
		return nil, fmt.Errorf("inference gateway is not available: %w", err)
	}

	if len(models) == 0 {
		return nil, fmt.Errorf("no models available from inference gateway")
	}

	return models, nil
}

// selectModel selects a model (use default if configured, otherwise pick first available)
func (e *BackgroundExecutor) selectModel(models []string) (string, error) {
	cfg := e.services.GetConfig()

	// Use default model if configured
	if cfg.Chat.DefaultModel != "" {
		for _, model := range models {
			if model == cfg.Chat.DefaultModel {
				return cfg.Chat.DefaultModel, nil
			}
		}
		fmt.Printf("⚠️  Default model '%s' not available, using %s\n", cfg.Chat.DefaultModel, models[0])
	}

	// Use first available model
	return models[0], nil
}

// createSystemPrompt creates a system prompt for background execution
func (e *BackgroundExecutor) createSystemPrompt() string {
	return `You are an AI assistant running in background mode to solve a specific task or issue.

Your goal is to:
1. Analyze the given task/issue thoroughly
2. Break it down into actionable steps
3. Provide detailed solutions or implementations
4. Consider edge cases and potential problems
5. Suggest next steps or follow-up actions

When working on GitHub issues:
- Understand the problem described in the issue
- Consider the codebase context if available
- Provide specific, actionable solutions
- Consider implementation details and best practices

Be thorough, practical, and focused on delivering complete solutions.`
}

// executeIteratively executes the prompt iteratively until completion
func (e *BackgroundExecutor) executeIteratively(ctx context.Context, model, systemPrompt, promptText string) error {
	messages := []sdk.Message{
		{Role: sdk.System, Content: systemPrompt},
		{Role: sdk.User, Content: promptText},
	}

	for iteration := 1; iteration <= e.maxIterations; iteration++ {
		fmt.Printf("\n📝 Iteration %d/%d\n", iteration, e.maxIterations)
		fmt.Println("" + strings.Repeat("=", 50))

		// Send message to the model
		events, err := e.services.GetChatService().SendMessage(ctx, model, messages)
		if err != nil {
			return fmt.Errorf("failed to send message: %w", err)
		}

		// Process the response
		response, completed, err := e.processResponse(events)
		if err != nil {
			return fmt.Errorf("error processing response: %w", err)
		}

		// Add response to conversation
		messages = append(messages, sdk.Message{
			Role:    sdk.Assistant,
			Content: response,
		})

		// Check if task is completed
		if completed || e.isTaskCompleted(response) {
			fmt.Println("\n✅ Task completed successfully!")
			fmt.Println("📄 Final solution:")
			fmt.Println(response)
			return nil
		}

		// If not completed, ask for next steps or refinement
		followUpPrompt := e.generateFollowUpPrompt(response, iteration)
		messages = append(messages, sdk.Message{
			Role:    sdk.User,
			Content: followUpPrompt,
		})
	}

	fmt.Printf("\n⚠️  Reached maximum iterations (%d). Task may not be fully completed.\n", e.maxIterations)
	return nil
}

// processResponse processes the chat response events
func (e *BackgroundExecutor) processResponse(events <-chan domain.ChatEvent) (string, bool, error) {
	var fullResponse strings.Builder
	completed := false

	for event := range events {
		switch evt := event.(type) {
		case domain.ChatChunkEvent:
			fullResponse.WriteString(evt.Content)
			fmt.Print(evt.Content) // Real-time output

		case domain.ChatCompleteEvent:
			completed = true
			if evt.Message != "" {
				fullResponse.WriteString(evt.Message)
			}

		case domain.ChatErrorEvent:
			return "", false, evt.Error
		}
	}

	return fullResponse.String(), completed, nil
}

// isTaskCompleted checks if the task appears to be completed based on the response
func (e *BackgroundExecutor) isTaskCompleted(response string) bool {
	completionIndicators := []string{
		"task completed",
		"solution implemented",
		"issue resolved",
		"implementation complete",
		"problem solved",
		"finished",
		"done",
	}

	responseLower := strings.ToLower(response)
	for _, indicator := range completionIndicators {
		if strings.Contains(responseLower, indicator) {
			return true
		}
	}

	return false
}

// generateFollowUpPrompt generates a follow-up prompt to continue the task
func (e *BackgroundExecutor) generateFollowUpPrompt(response string, iteration int) string {
	prompts := []string{
		"Please continue with the next steps to complete this task.",
		"What additional work is needed to fully resolve this issue?",
		"Are there any remaining steps or considerations for this task?",
		"Please provide any additional implementation details or next steps.",
	}

	// Use different prompts based on iteration to add variety
	promptIndex := (iteration - 1) % len(prompts)
	return prompts[promptIndex]
}

func init() {
	rootCmd.AddCommand(promptCmd)
}
