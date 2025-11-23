package shortcuts

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
)

// ClearShortcut clears the conversation history
type ClearShortcut struct {
	repo        domain.ConversationRepository
	taskTracker domain.TaskTracker
}

func NewClearShortcut(repo domain.ConversationRepository, taskTracker domain.TaskTracker) *ClearShortcut {
	return &ClearShortcut{
		repo:        repo,
		taskTracker: taskTracker,
	}
}

func (c *ClearShortcut) GetName() string               { return "clear" }
func (c *ClearShortcut) GetDescription() string        { return "Clear conversation history" }
func (c *ClearShortcut) GetUsage() string              { return "/clear" }
func (c *ClearShortcut) CanExecute(args []string) bool { return len(args) == 0 }

func (c *ClearShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	if err := c.repo.Clear(); err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("Failed to clear conversation: %v", err),
			Success: false,
		}, nil
	}

	if c.taskTracker != nil {
		c.taskTracker.ClearAllAgents()
	}

	return ShortcutResult{
		Output:     "🧹 Conversation history cleared!",
		Success:    true,
		SideEffect: SideEffectClearConversation,
	}, nil
}

// ExportShortcut exports the conversation
type ExportShortcut struct {
	repo         domain.ConversationRepository
	agentService domain.AgentService
	modelService domain.ModelService
	config       *config.Config
}

func NewExportShortcut(repo domain.ConversationRepository, agentService domain.AgentService, modelService domain.ModelService, config *config.Config) *ExportShortcut {
	return &ExportShortcut{
		repo:         repo,
		agentService: agentService,
		modelService: modelService,
		config:       config,
	}
}

func (c *ExportShortcut) GetName() string               { return "export" }
func (c *ExportShortcut) GetDescription() string        { return "Export conversation to markdown" }
func (c *ExportShortcut) GetUsage() string              { return "/export [format]" }
func (c *ExportShortcut) CanExecute(args []string) bool { return len(args) <= 1 }

func (c *ExportShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	if c.repo.GetMessageCount() == 0 {
		return ShortcutResult{
			Output:  "📝 No conversation to export - conversation history is empty",
			Success: true,
		}, nil
	}

	return ShortcutResult{
		Output:     "🔄 Generating summary and exporting conversation...",
		Success:    true,
		SideEffect: SideEffectExportConversation,
		Data:       ctx,
	}, nil
}

// ExportResult contains the results of an export operation
type ExportResult struct {
	FilePath string
	Summary  string
}

// PerformExport performs the actual export operation (called by side effect handler)
func (c *ExportShortcut) PerformExport(ctx context.Context) (*ExportResult, error) {
	filename := fmt.Sprintf("chat_export_%s.md", time.Now().Format("20060102_150405"))

	outputDir := c.config.Export.OutputDir
	if outputDir == "" {
		outputDir = ".infer"
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	filePath := filepath.Join(outputDir, filename)

	summary, err := c.generateSummary(ctx)
	if err != nil {
		summary = fmt.Sprintf("*Summary generation failed: %v*", err)
	}

	conversationData, err := c.repo.Export(domain.ExportMarkdown)
	if err != nil {
		return nil, fmt.Errorf("failed to export conversation: %w", err)
	}

	content := c.createCompactMarkdown(summary, string(conversationData))

	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return nil, fmt.Errorf("failed to write export file: %w", err)
	}

	return &ExportResult{
		FilePath: filePath,
		Summary:  summary,
	}, nil
}

// generateSummary uses the LLM to generate a summary of the conversation
func (c *ExportShortcut) generateSummary(ctx context.Context) (string, error) {
	entries := c.repo.GetMessages()
	if len(entries) == 0 {
		return "No conversation to summarize", nil
	}

	messages := make([]sdk.Message, 0, len(entries)+1)

	messages = append(messages, sdk.Message{
		Role: sdk.System,
		Content: sdk.NewMessageContent(`You are a helpful assistant that creates concise summaries of chat conversations. Please provide:
1. A brief overview of the main topics discussed
2. Key questions asked and answers provided
3. Important decisions or conclusions reached
4. Any action items or next steps mentioned

Keep the summary concise but informative, using bullet points where appropriate.`),
	})

	for _, entry := range entries {
		if !entry.Hidden && (entry.Message.Role == sdk.User || entry.Message.Role == sdk.Assistant || entry.Message.Role == sdk.Tool) {
			messages = append(messages, entry.Message)
		}
	}

	messages = append(messages, sdk.Message{
		Role:    sdk.User,
		Content: sdk.NewMessageContent("Please provide a summary of our conversation above."),
	})

	summaryModel := c.config.Export.SummaryModel
	if summaryModel == "" {
		summaryModel = c.modelService.GetCurrentModel()
		if summaryModel == "" {
			return "No model selected for summary generation", nil
		}
	}

	requestID := fmt.Sprintf("req_%d", time.Now().UnixNano())

	req := &domain.AgentRequest{
		RequestID: requestID,
		Model:     summaryModel,
		Messages:  messages,
	}

	eventChan, err := c.agentService.RunWithStream(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed to start summary generation: %w", err)
	}

	var summaryBuilder strings.Builder
	for event := range eventChan {
		switch e := event.(type) {
		case domain.ChatChunkEvent:
			summaryBuilder.WriteString(e.Content)
		case domain.ChatCompleteEvent:
			return summaryBuilder.String(), nil
		case domain.ChatErrorEvent:
			return "", fmt.Errorf("summary generation failed: %w", e.Error)
		}
	}

	return summaryBuilder.String(), nil
}

// createCompactMarkdown creates the final markdown content with summary and full conversation
func (c *ExportShortcut) createCompactMarkdown(summary, fullConversation string) string {
	var content strings.Builder

	content.WriteString("# Chat Conversation Export\n\n")
	content.WriteString(fmt.Sprintf("**Generated:** %s\n", time.Now().Format("January 2, 2006 at 3:04 PM")))
	content.WriteString(fmt.Sprintf("**Total Messages:** %d\n", c.repo.GetMessageCount()))

	sessionStats := c.repo.GetSessionTokens()
	if sessionStats.RequestCount > 0 {
		content.WriteString(fmt.Sprintf("**Total Input Tokens:** %d\n", sessionStats.TotalInputTokens))
		content.WriteString(fmt.Sprintf("**Total Output Tokens:** %d\n", sessionStats.TotalOutputTokens))
		content.WriteString(fmt.Sprintf("**Total Tokens:** %d\n", sessionStats.TotalTokens))
		content.WriteString(fmt.Sprintf("**API Requests:** %d\n", sessionStats.RequestCount))
	}
	content.WriteString("\n")

	content.WriteString("---\n\n")
	content.WriteString("## Summary\n\n")
	content.WriteString(summary)
	content.WriteString("\n\n---\n\n")
	content.WriteString("## Full Conversation\n\n")
	content.WriteString(fullConversation)
	content.WriteString("\n\n---\n\n")
	content.WriteString(fmt.Sprintf("*Generated by Inference Gateway CLI on %s*\n", time.Now().Format("2006-01-02 15:04:05")))

	return content.String()
}

// CompactShortcut runs conversation optimization to reduce token usage
type CompactShortcut struct {
	repo domain.ConversationRepository
}

func NewCompactShortcut(repo domain.ConversationRepository) *CompactShortcut {
	return &CompactShortcut{
		repo: repo,
	}
}

func (c *CompactShortcut) GetName() string { return "compact" }
func (c *CompactShortcut) GetDescription() string {
	return "Optimize conversation to reduce token usage"
}
func (c *CompactShortcut) GetUsage() string              { return "/compact" }
func (c *CompactShortcut) CanExecute(args []string) bool { return len(args) == 0 }

func (c *CompactShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	if c.repo.GetMessageCount() == 0 {
		return ShortcutResult{
			Output:  "No conversation to compact - conversation history is empty",
			Success: true,
		}, nil
	}

	return ShortcutResult{
		Output:     "Compacting conversation history...",
		Success:    true,
		SideEffect: SideEffectCompactConversation,
		Data:       ctx,
	}, nil
}

// ContextShortcut shows context window usage information
type ContextShortcut struct {
	repo         domain.ConversationRepository
	modelService domain.ModelService
}

func NewContextShortcut(repo domain.ConversationRepository, modelService domain.ModelService) *ContextShortcut {
	return &ContextShortcut{
		repo:         repo,
		modelService: modelService,
	}
}

func (c *ContextShortcut) GetName() string               { return "context" }
func (c *ContextShortcut) GetDescription() string        { return "Show context window usage" }
func (c *ContextShortcut) GetUsage() string              { return "/context" }
func (c *ContextShortcut) CanExecute(args []string) bool { return len(args) == 0 }

func (c *ContextShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	stats := c.repo.GetSessionTokens()
	messageCount := c.repo.GetMessageCount()
	currentModel := c.modelService.GetCurrentModel()

	contextWindowSize := c.estimateContextWindow(currentModel)

	var output strings.Builder
	output.WriteString("## Context Window Usage\n\n")

	if currentModel != "" {
		output.WriteString(fmt.Sprintf("**Model:** %s\n", currentModel))
	}
	output.WriteString(fmt.Sprintf("**Messages:** %d\n", messageCount))
	output.WriteString(fmt.Sprintf("**Current Context Size:** %d tokens\n", stats.LastInputTokens))
	output.WriteString(fmt.Sprintf("**API Requests:** %d\n", stats.RequestCount))
	output.WriteString(fmt.Sprintf("**Session Totals:** %d input, %d output\n", stats.TotalInputTokens, stats.TotalOutputTokens))

	if contextWindowSize > 0 && stats.LastInputTokens > 0 {
		usagePercent := float64(stats.LastInputTokens) * 100 / float64(contextWindowSize)
		remaining := contextWindowSize - stats.LastInputTokens
		if remaining < 0 {
			remaining = 0
		}

		output.WriteString(fmt.Sprintf("\n**Context Window:** %d tokens\n", contextWindowSize))
		output.WriteString(fmt.Sprintf("**Usage:** %.1f%%\n", usagePercent))
		output.WriteString(fmt.Sprintf("**Remaining:** ~%d tokens\n", remaining))

		barWidth := 20
		filledWidth := int(usagePercent * float64(barWidth) / 100)
		if filledWidth > barWidth {
			filledWidth = barWidth
		}
		bar := strings.Repeat("█", filledWidth) + strings.Repeat("░", barWidth-filledWidth)
		output.WriteString(fmt.Sprintf("\n`[%s]` %.1f%%\n", bar, usagePercent))

		if usagePercent > 80 {
			output.WriteString("\n**Warning:** Context window is getting full. Consider using `/compact` to optimize.")
		}
	}

	return ShortcutResult{
		Output:  output.String(),
		Success: true,
	}, nil
}

// estimateContextWindow returns an estimated context window size based on model name
func (c *ContextShortcut) estimateContextWindow(model string) int {
	model = strings.ToLower(model)

	// DeepSeek models (128K context)
	if strings.Contains(model, "deepseek") {
		return 128000
	}

	// OpenAI models
	// o1/o3 models have 200K context
	if strings.Contains(model, "o1") || strings.Contains(model, "o3") {
		return 200000
	}
	if strings.Contains(model, "gpt-4o") || strings.Contains(model, "gpt-4-turbo") {
		return 128000
	}
	if strings.Contains(model, "gpt-4-32k") {
		return 32768
	}
	if strings.Contains(model, "gpt-4") {
		return 8192
	}
	// All current GPT-3.5-turbo models support 16K
	if strings.Contains(model, "gpt-3.5") {
		return 16384
	}

	// Anthropic models
	// Claude 4, Claude 3.5, and Claude 3 all have 200K context
	if strings.Contains(model, "claude-4") || strings.Contains(model, "claude-3.5") || strings.Contains(model, "claude-3") {
		return 200000
	}
	// Claude 2 has 100K context
	if strings.Contains(model, "claude-2") {
		return 100000
	}
	// Other Claude models default to 200K
	if strings.Contains(model, "claude") {
		return 200000
	}

	// Google models
	// Gemini 2.0 models have 1M context
	if strings.Contains(model, "gemini-2") {
		return 1000000
	}
	// Gemini 1.5 Pro has 2M, Flash has 1M - use 1M as conservative default
	if strings.Contains(model, "gemini-1.5") {
		return 1000000
	}
	// Gemini 1.0 Pro has 32K
	if strings.Contains(model, "gemini") {
		return 32768
	}

	// Mistral models
	// Mistral Large has 128K context
	if strings.Contains(model, "mistral-large") {
		return 128000
	}
	// Other Mistral models (Small, 7B, Mixtral) have 32K
	if strings.Contains(model, "mistral") || strings.Contains(model, "mixtral") {
		return 32768
	}

	// Llama models
	// Llama 3.1, 3.2, 3.3 have 128K context
	if strings.Contains(model, "llama-3.1") || strings.Contains(model, "llama-3.2") || strings.Contains(model, "llama-3.3") {
		return 128000
	}
	// Llama 3 (original) has 8K context
	if strings.Contains(model, "llama-3") {
		return 8192
	}
	// Llama 2 has 4K context
	if strings.Contains(model, "llama") {
		return 4096
	}

	// Qwen models (all have 128K context)
	if strings.Contains(model, "qwen") {
		return 128000
	}

	// Cohere Command models
	if strings.Contains(model, "command-r") {
		return 128000
	}

	// Default fallback
	return 8192
}

// NewShortcut starts a new conversation
type NewShortcut struct {
	repo        PersistentConversationRepository
	taskTracker domain.TaskTracker
}

func NewNewShortcut(repo PersistentConversationRepository, taskTracker domain.TaskTracker) *NewShortcut {
	return &NewShortcut{
		repo:        repo,
		taskTracker: taskTracker,
	}
}

func (c *NewShortcut) GetName() string               { return "new" }
func (c *NewShortcut) GetDescription() string        { return "Start a new conversation" }
func (c *NewShortcut) GetUsage() string              { return "/new [title]" }
func (c *NewShortcut) CanExecute(args []string) bool { return len(args) <= 1 }

func (c *NewShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	title := "New Conversation"
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		title = strings.TrimSpace(args[0])
	}

	if c.taskTracker != nil {
		c.taskTracker.ClearAllAgents()
	}

	return ShortcutResult{
		Output:     fmt.Sprintf("🆕 Starting new conversation: %s", title),
		Success:    true,
		SideEffect: SideEffectStartNewConversation,
		Data:       title,
	}, nil
}

// HelpShortcut shows available shortcuts
type HelpShortcut struct {
	registry *Registry
}

func NewHelpShortcut(registry *Registry) *HelpShortcut {
	return &HelpShortcut{registry: registry}
}

func (c *HelpShortcut) GetName() string               { return "help" }
func (c *HelpShortcut) GetDescription() string        { return "Show available shortcuts" }
func (c *HelpShortcut) GetUsage() string              { return "/help [shortcut]" }
func (c *HelpShortcut) CanExecute(args []string) bool { return len(args) <= 1 }

func (c *HelpShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	if len(args) == 1 {
		shortcutName := args[0]
		shortcut, exists := c.registry.Get(shortcutName)
		if !exists {
			return ShortcutResult{
				Output:  fmt.Sprintf("Unknown shortcut: %s", shortcutName),
				Success: false,
			}, nil
		}

		output := fmt.Sprintf("Shortcut: %s\nDescription: %s\nUsage: %s",
			shortcut.GetName(), shortcut.GetDescription(), shortcut.GetUsage())

		return ShortcutResult{
			Output:  output,
			Success: true,
		}, nil
	}

	var output strings.Builder
	output.WriteString("## Available Shortcuts\n\n")

	shortcuts := c.registry.GetAll()
	for _, shortcut := range shortcuts {
		output.WriteString(fmt.Sprintf("• **`/%s`** - %s\n", shortcut.GetName(), shortcut.GetDescription()))
	}

	output.WriteString("\n• *Type `/help <shortcut>` for detailed usage information.*")

	return ShortcutResult{
		Output:     output.String(),
		Success:    true,
		SideEffect: SideEffectShowHelp,
	}, nil
}

// ExitShortcut exits the application
type ExitShortcut struct{}

func NewExitShortcut() *ExitShortcut {
	return &ExitShortcut{}
}

func (c *ExitShortcut) GetName() string               { return "exit" }
func (c *ExitShortcut) GetDescription() string        { return "Exit the chat session" }
func (c *ExitShortcut) GetUsage() string              { return "/exit" }
func (c *ExitShortcut) CanExecute(args []string) bool { return len(args) == 0 }

func (c *ExitShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	return ShortcutResult{
		Output:     "👋 Chat session ended!",
		Success:    true,
		SideEffect: SideEffectExit,
	}, nil
}

// SwitchShortcut switches the active model
type SwitchShortcut struct {
	modelService domain.ModelService
}

func NewSwitchShortcut(modelService domain.ModelService) *SwitchShortcut {
	return &SwitchShortcut{modelService: modelService}
}

func (c *SwitchShortcut) GetName() string               { return "switch" }
func (c *SwitchShortcut) GetDescription() string        { return "Switch to a different model" }
func (c *SwitchShortcut) GetUsage() string              { return "/switch [model]" }
func (c *SwitchShortcut) CanExecute(args []string) bool { return len(args) <= 1 }

func (c *SwitchShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	if len(args) == 0 {
		return ShortcutResult{
			Output:     "Select a model from the dropdown",
			Success:    true,
			SideEffect: SideEffectSwitchModel,
		}, nil
	}

	modelID := args[0]
	if err := c.modelService.SelectModel(modelID); err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("Failed to switch model: %v", err),
			Success: false,
		}, nil
	}

	return ShortcutResult{
		Output:     fmt.Sprintf("Switched to model: %s", modelID),
		Success:    true,
		SideEffect: SideEffectSwitchModel,
		Data:       modelID,
	}, nil
}

// ThemeShortcut switches the active theme
type ThemeShortcut struct {
	themeService domain.ThemeService
}

func NewThemeShortcut(themeService domain.ThemeService) *ThemeShortcut {
	return &ThemeShortcut{themeService: themeService}
}

func (c *ThemeShortcut) GetName() string               { return "theme" }
func (c *ThemeShortcut) GetDescription() string        { return "Switch to a different theme" }
func (c *ThemeShortcut) GetUsage() string              { return "/theme [theme-name]" }
func (c *ThemeShortcut) CanExecute(args []string) bool { return len(args) <= 1 }

func (c *ThemeShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	if len(args) == 0 {
		return ShortcutResult{
			Output:     "",
			Success:    true,
			SideEffect: SideEffectSwitchTheme,
		}, nil
	}

	themeName := args[0]
	if err := c.themeService.SetTheme(themeName); err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("❌ Failed to switch theme: %v", err),
			Success: false,
		}, nil
	}

	return ShortcutResult{
		Output:     fmt.Sprintf("🎨 Switched to theme: **%s**", themeName),
		Success:    true,
		SideEffect: SideEffectSwitchTheme,
		Data:       themeName,
	}, nil
}
