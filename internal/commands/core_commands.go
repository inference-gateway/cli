package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/inference-gateway/cli/internal/domain"
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
	repo domain.ConversationRepository
}

func NewExportCommand(repo domain.ConversationRepository) *ExportCommand {
	return &ExportCommand{repo: repo}
}

func (c *ExportCommand) GetName() string               { return "compact" }
func (c *ExportCommand) GetDescription() string        { return "Export conversation to markdown" }
func (c *ExportCommand) GetUsage() string              { return "/compact [format]" }
func (c *ExportCommand) CanExecute(args []string) bool { return len(args) <= 1 }

func (c *ExportCommand) Execute(ctx context.Context, args []string) (CommandResult, error) {
	format := domain.ExportMarkdown
	if len(args) > 0 {
		switch strings.ToLower(args[0]) {
		case "json":
			format = domain.ExportJSON
		case "text":
			format = domain.ExportText
		case "markdown", "md":
			format = domain.ExportMarkdown
		default:
			return CommandResult{
				Output:  fmt.Sprintf("Unknown format '%s'. Available: markdown, json, text", args[0]),
				Success: false,
			}, nil
		}
	}

	if c.repo.GetMessageCount() == 0 {
		return CommandResult{
			Output:  "📝 No conversation to export - conversation history is empty",
			Success: true,
		}, nil
	}

	data, err := c.repo.Export(format)
	if err != nil {
		return CommandResult{
			Output:  fmt.Sprintf("Failed to export conversation: %v", err),
			Success: false,
		}, nil
	}

	return CommandResult{
		Output:     "📝 Conversation exported successfully",
		Success:    true,
		SideEffect: SideEffectExportConversation,
		Data:       data,
	}, nil
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
		// Show help for specific command
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

	// Show all commands
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

// HistoryCommand shows conversation history
type HistoryCommand struct {
	repo domain.ConversationRepository
}

func NewHistoryCommand(repo domain.ConversationRepository) *HistoryCommand {
	return &HistoryCommand{repo: repo}
}

func (c *HistoryCommand) GetName() string               { return "history" }
func (c *HistoryCommand) GetDescription() string        { return "Show conversation history" }
func (c *HistoryCommand) GetUsage() string              { return "/history" }
func (c *HistoryCommand) CanExecute(args []string) bool { return len(args) == 0 }

func (c *HistoryCommand) Execute(ctx context.Context, args []string) (CommandResult, error) {
	messages := c.repo.GetMessages()
	if len(messages) == 0 {
		return CommandResult{
			Output:  "💬 Conversation history is empty",
			Success: true,
		}, nil
	}

	var output strings.Builder
	output.WriteString("💬 Conversation History:\n")

	for i, entry := range messages {
		var role string
		switch entry.Message.Role {
		case "user":
			role = "You"
		case "assistant":
			if entry.Model != "" {
				role = fmt.Sprintf("Assistant (%s)", entry.Model)
			} else {
				role = "Assistant"
			}
		case "system":
			role = "System"
		case "tool":
			role = "Tool"
		default:
			role = string(entry.Message.Role)
		}
		output.WriteString(fmt.Sprintf("  %d. %s: %s\n", i+1, role, entry.Message.Content))
	}

	return CommandResult{
		Output:     output.String(),
		Success:    true,
		SideEffect: SideEffectShowHistory,
	}, nil
}

// ModelsCommand shows available models
type ModelsCommand struct {
	modelService domain.ModelService
}

func NewModelsCommand(modelService domain.ModelService) *ModelsCommand {
	return &ModelsCommand{modelService: modelService}
}

func (c *ModelsCommand) GetName() string               { return "models" }
func (c *ModelsCommand) GetDescription() string        { return "Show available models" }
func (c *ModelsCommand) GetUsage() string              { return "/models" }
func (c *ModelsCommand) CanExecute(args []string) bool { return len(args) == 0 }

func (c *ModelsCommand) Execute(ctx context.Context, args []string) (CommandResult, error) {
	current := c.modelService.GetCurrentModel()
	models, err := c.modelService.ListModels(ctx)
	if err != nil {
		return CommandResult{
			Output:  fmt.Sprintf("Failed to fetch models: %v", err),
			Success: false,
		}, nil
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("Current model: %s\n", current))
	output.WriteString("Available models:\n")

	for _, model := range models {
		if model == current {
			output.WriteString(fmt.Sprintf("  • %s (current)\n", model))
		} else {
			output.WriteString(fmt.Sprintf("  • %s\n", model))
		}
	}

	return CommandResult{
		Output:  output.String(),
		Success: true,
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
		// Interactive model selection will be triggered by the side effect
		return CommandResult{
			Output:     "Select a model from the dropdown",
			Success:    true,
			SideEffect: SideEffectSwitchModel,
		}, nil
	}

	// Direct model switch
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
