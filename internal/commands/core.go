package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
)

// ClearCommand clears the conversation history
type ClearCommand struct {
	repo domain.ConversationRepository
}

func NewClearCommand(repo domain.ConversationRepository) *ClearCommand {
	return &ClearCommand{repo: repo}
}

func (c *ClearCommand) GetName() string               { return "clear" }
func (c *ClearCommand) GetDescription() string        { return "Clear conversation history" }
func (c *ClearCommand) GetUsage() string              { return "/clear" }
func (c *ClearCommand) CanExecute(args []string) bool { return len(args) == 0 }

func (c *ClearCommand) Execute(ctx context.Context, args []string) (CommandResult, error) {
	if err := c.repo.Clear(); err != nil {
		return CommandResult{
			Output:  fmt.Sprintf("Failed to clear conversation: %v", err),
			Success: false,
		}, nil
	}

	return CommandResult{
		Output:     "🧹 Conversation history cleared!",
		Success:    true,
		SideEffect: SideEffectClearConversation,
	}, nil
}

// ExportCommand exports the conversation
type ExportCommand struct {
	repo         domain.ConversationRepository
	chatService  domain.ChatService
	modelService domain.ModelService
	config       *config.Config
}

func NewExportCommand(repo domain.ConversationRepository, chatService domain.ChatService, modelService domain.ModelService, config *config.Config) *ExportCommand {
	return &ExportCommand{
		repo:         repo,
		chatService:  chatService,
		modelService: modelService,
		config:       config,
	}
}

func (c *ExportCommand) GetName() string               { return "compact" }
func (c *ExportCommand) GetDescription() string        { return "Export conversation to markdown" }
func (c *ExportCommand) GetUsage() string              { return "/compact [format]" }
func (c *ExportCommand) CanExecute(args []string) bool { return len(args) <= 1 }

func (c *ExportCommand) Execute(ctx context.Context, args []string) (CommandResult, error) {
	if c.repo.GetMessageCount() == 0 {
		return CommandResult{
			Output:  "📝 No conversation to export - conversation history is empty",
			Success: true,
		}, nil
	}

	return CommandResult{
		Output:     "🔄 Generating summary and exporting conversation...",
		Success:    true,
		SideEffect: SideEffectExportConversation,
		Data:       ctx, // Pass context to side effect handler
	}, nil
}

// PerformExport performs the actual export operation (called by side effect handler)
func (c *ExportCommand) PerformExport(ctx context.Context) (string, error) {
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
func (c *ExportCommand) generateSummary(ctx context.Context) (string, error) {
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
		if !entry.IsSystemReminder && (entry.Message.Role == sdk.User || entry.Message.Role == sdk.Assistant || entry.Message.Role == sdk.Tool) {
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
	eventChan, err := c.chatService.SendMessage(ctx, requestID, summaryModel, messages)
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
func (c *ExportCommand) createCompactMarkdown(summary, fullConversation string) string {
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

// HelpCommand shows available commands
type HelpCommand struct {
	registry *Registry
}

func NewHelpCommand(registry *Registry) *HelpCommand {
	return &HelpCommand{registry: registry}
}

func (c *HelpCommand) GetName() string               { return "help" }
func (c *HelpCommand) GetDescription() string        { return "Show available commands" }
func (c *HelpCommand) GetUsage() string              { return "/help [command]" }
func (c *HelpCommand) CanExecute(args []string) bool { return len(args) <= 1 }

func (c *HelpCommand) Execute(ctx context.Context, args []string) (CommandResult, error) {
	if len(args) == 1 {
		cmdName := args[0]
		cmd, exists := c.registry.Get(cmdName)
		if !exists {
			return CommandResult{
				Output:  fmt.Sprintf("Unknown command: %s", cmdName),
				Success: false,
			}, nil
		}

		output := fmt.Sprintf("Command: %s\nDescription: %s\nUsage: %s",
			cmd.GetName(), cmd.GetDescription(), cmd.GetUsage())

		return CommandResult{
			Output:  output,
			Success: true,
		}, nil
	}

	var output strings.Builder
	output.WriteString("Available commands:\n")

	commands := c.registry.GetAll()
	for _, cmd := range commands {
		output.WriteString(fmt.Sprintf("  /%s - %s\n", cmd.GetName(), cmd.GetDescription()))
	}

	output.WriteString("\nType /help <command> for detailed usage information.")

	return CommandResult{
		Output:     output.String(),
		Success:    true,
		SideEffect: SideEffectShowHelp,
	}, nil
}

// ExitCommand exits the application
type ExitCommand struct{}

func NewExitCommand() *ExitCommand {
	return &ExitCommand{}
}

func (c *ExitCommand) GetName() string               { return "exit" }
func (c *ExitCommand) GetDescription() string        { return "Exit the chat session" }
func (c *ExitCommand) GetUsage() string              { return "/exit" }
func (c *ExitCommand) CanExecute(args []string) bool { return len(args) == 0 }

func (c *ExitCommand) Execute(ctx context.Context, args []string) (CommandResult, error) {
	return CommandResult{
		Output:     "👋 Chat session ended!",
		Success:    true,
		SideEffect: SideEffectExit,
	}, nil
}

// SwitchCommand switches the active model
type SwitchCommand struct {
	modelService domain.ModelService
}

func NewSwitchCommand(modelService domain.ModelService) *SwitchCommand {
	return &SwitchCommand{modelService: modelService}
}

func (c *SwitchCommand) GetName() string               { return "switch" }
func (c *SwitchCommand) GetDescription() string        { return "Switch to a different model" }
func (c *SwitchCommand) GetUsage() string              { return "/switch [model]" }
func (c *SwitchCommand) CanExecute(args []string) bool { return len(args) <= 1 }

func (c *SwitchCommand) Execute(ctx context.Context, args []string) (CommandResult, error) {
	if len(args) == 0 {
		return CommandResult{
			Output:     "Select a model from the dropdown",
			Success:    true,
			SideEffect: SideEffectSwitchModel,
		}, nil
	}

	modelID := args[0]
	if err := c.modelService.SelectModel(modelID); err != nil {
		return CommandResult{
			Output:  fmt.Sprintf("Failed to switch model: %v", err),
			Success: false,
		}, nil
	}

	return CommandResult{
		Output:     fmt.Sprintf("Switched to model: %s", modelID),
		Success:    true,
		SideEffect: SideEffectSwitchModel,
		Data:       modelID,
	}, nil
}
