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
	repo domain.ConversationRepository
}

func NewClearShortcut(repo domain.ConversationRepository) *ClearShortcut {
	return &ClearShortcut{repo: repo}
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

func (c *ExportShortcut) GetName() string               { return "compact" }
func (c *ExportShortcut) GetDescription() string        { return "Export conversation to markdown" }
func (c *ExportShortcut) GetUsage() string              { return "/compact [format]" }
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
		Data:       ctx, // Pass context to side effect handler
	}, nil
}

// PerformExport performs the actual export operation (called by side effect handler)
func (c *ExportShortcut) PerformExport(ctx context.Context) (string, error) {
	filename := fmt.Sprintf("chat_export_%s.md", time.Now().Format("20060102_150405"))

	outputDir := c.config.Compact.OutputDir
	if outputDir == "" {
		outputDir = ".infer"
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create output directory: %w", err)
	}

	filePath := filepath.Join(outputDir, filename)

	summary, err := c.generateSummary(ctx)
	if err != nil {
		summary = fmt.Sprintf("*Summary generation failed: %v*", err)
	}

	conversationData, err := c.repo.Export(domain.ExportMarkdown)
	if err != nil {
		return "", fmt.Errorf("failed to export conversation: %w", err)
	}

	content := c.createCompactMarkdown(summary, string(conversationData))

	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("failed to write export file: %w", err)
	}

	return filePath, nil
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
		Content: `You are a helpful assistant that creates concise summaries of chat conversations. Please provide:
1. A brief overview of the main topics discussed
2. Key questions asked and answers provided
3. Important decisions or conclusions reached
4. Any action items or next steps mentioned

Keep the summary concise but informative, using bullet points where appropriate.`,
	})

	for _, entry := range entries {
		if !entry.Hidden && (entry.Message.Role == sdk.User || entry.Message.Role == sdk.Assistant || entry.Message.Role == sdk.Tool) {
			messages = append(messages, entry.Message)
		}
	}

	messages = append(messages, sdk.Message{
		Role:    sdk.User,
		Content: "Please provide a summary of our conversation above.",
	})

	summaryModel := c.config.Compact.SummaryModel
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

// NewShortcut starts a new conversation
type NewShortcut struct {
	repo PersistentConversationRepository
}

func NewNewShortcut(repo PersistentConversationRepository) *NewShortcut {
	return &NewShortcut{repo: repo}
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

	output.WriteString("\n💡 *Type `/help <shortcut>` for detailed usage information.*")

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
