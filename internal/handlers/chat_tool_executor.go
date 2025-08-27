package handlers

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	services "github.com/inference-gateway/cli/internal/services"
	icons "github.com/inference-gateway/cli/internal/ui/styles/icons"
	sdk "github.com/inference-gateway/sdk"
)

// ChatToolExecutor handles tool execution logic
type ChatToolExecutor struct {
	handler *ChatHandler
}

// NewChatToolExecutor creates a new tool executor
func NewChatToolExecutor(handler *ChatHandler) *ChatToolExecutor {
	return &ChatToolExecutor{
		handler: handler,
	}
}

// executeToolDirectly executes a tool directly and adds the result to conversation history
func (t *ChatToolExecutor) executeToolDirectly(
	toolName string,
	args map[string]any,
	_ *services.StateManager,
) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		startTime := time.Now()

		if err := t.handler.toolService.ValidateTool(toolName, args); err != nil {
			return t.handleToolValidationError(toolName, err)
		}

		result, err := t.handler.toolService.ExecuteTool(ctx, toolName, args)
		duration := time.Since(startTime)

		if err != nil {
			return t.handleToolExecutionError(toolName, duration, err)
		}

		t.addToolExecutionToHistory(result)

		return t.createToolUIUpdate(result.Success, toolName)
	}
}

// handleToolValidationError handles tool validation errors
func (t *ChatToolExecutor) handleToolValidationError(_ string, err error) tea.Msg {
	errorEntry := domain.ConversationEntry{
		Message: sdk.Message{
			Role:    sdk.Assistant,
			Content: fmt.Sprintf("%s Tool validation error: %v", icons.CrossMarkStyle.Render(icons.CrossMark), err),
		},
		Model: t.handler.modelService.GetCurrentModel(),
		Time:  time.Now(),
	}

	if addErr := t.handler.conversationRepo.AddMessage(errorEntry); addErr != nil {
		logger.Error("failed to add error message to conversation", "error", addErr)
	}

	return tea.Batch(
		func() tea.Msg {
			return domain.UpdateHistoryEvent{
				History: t.handler.conversationRepo.GetMessages(),
			}
		},
		func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Tool validation failed: %v", err),
				Sticky: false,
			}
		},
	)()
}

// handleToolExecutionError handles tool execution errors
func (t *ChatToolExecutor) handleToolExecutionError(_ string, _ time.Duration, err error) tea.Msg {
	errorEntry := domain.ConversationEntry{
		Message: sdk.Message{
			Role:    sdk.Assistant,
			Content: fmt.Sprintf("%s Tool execution failed: %v", icons.CrossMarkStyle.Render(icons.CrossMark), err),
		},
		Model: t.handler.modelService.GetCurrentModel(),
		Time:  time.Now(),
	}

	if addErr := t.handler.conversationRepo.AddMessage(errorEntry); addErr != nil {
		logger.Error("failed to add error message to conversation", "error", addErr)
	}

	return tea.Batch(
		func() tea.Msg {
			return domain.UpdateHistoryEvent{
				History: t.handler.conversationRepo.GetMessages(),
			}
		},
		func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Tool execution failed: %v", err),
				Sticky: false,
			}
		},
	)()
}

// addToolExecutionToHistory adds tool execution result to conversation history
func (t *ChatToolExecutor) addToolExecutionToHistory(result *domain.ToolExecutionResult) {
	assistantEntry := domain.ConversationEntry{
		Message: sdk.Message{
			Role:    sdk.Tool,
			Content: fmt.Sprintf("Tool '%s' executed successfully", result.ToolName),
		},
		Model:         t.handler.modelService.GetCurrentModel(),
		Time:          time.Now(),
		ToolExecution: result,
	}

	if err := t.handler.conversationRepo.AddMessage(assistantEntry); err != nil {
		logger.Error("failed to add assistant message with tool result", "error", err)
	}
}

// createToolUIUpdate creates UI update for tool execution
func (t *ChatToolExecutor) createToolUIUpdate(success bool, toolName string) tea.Msg {
	statusMsg := fmt.Sprintf("Tool '%s' completed", toolName)
	if !success {
		statusMsg = fmt.Sprintf("Tool '%s' failed", toolName)
	}

	return tea.Batch(
		func() tea.Msg {
			return domain.UpdateHistoryEvent{
				History: t.handler.conversationRepo.GetMessages(),
			}
		},
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    statusMsg,
				Spinner:    false,
				TokenUsage: t.handler.getCurrentTokenUsage(),
				StatusType: domain.StatusDefault,
			}
		},
	)()
}

// executeBashCommand executes a bash command using the tool service
func (t *ChatToolExecutor) executeBashCommand(
	command string,
	_ *services.StateManager,
) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		startTime := time.Now()

		args := map[string]any{
			"command": command,
			"format":  "text",
		}

		if err := t.handler.toolService.ValidateTool("Bash", args); err != nil {
			return t.handleBashValidationError(command, err)
		}

		result, err := t.handler.toolService.ExecuteTool(ctx, "Bash", args)
		duration := time.Since(startTime)

		if err != nil {
			return t.handleBashExecutionError(command, duration, err)
		}

		responseContent := t.formatBashResponse(result)
		t.addBashResponseToHistory(responseContent)

		return t.createBashUIUpdate(result.Success)
	}
}

func (t *ChatToolExecutor) handleBashValidationError(_ string, err error) tea.Msg {
	errorEntry := domain.ConversationEntry{
		Message: sdk.Message{
			Role:    sdk.Assistant,
			Content: fmt.Sprintf("%s Error: %v", icons.CrossMarkStyle.Render(icons.CrossMark), err),
		},
		Model: t.handler.modelService.GetCurrentModel(),
		Time:  time.Now(),
	}

	if addErr := t.handler.conversationRepo.AddMessage(errorEntry); addErr != nil {
		logger.Error("failed to add error message to conversation", "error", addErr)
	}

	return tea.Batch(
		func() tea.Msg {
			return domain.UpdateHistoryEvent{
				History: t.handler.conversationRepo.GetMessages(),
			}
		},
		func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Command validation failed: %v", err),
				Sticky: false,
			}
		},
	)()
}

func (t *ChatToolExecutor) handleBashExecutionError(_ string, _ time.Duration, err error) tea.Msg {
	errorEntry := domain.ConversationEntry{
		Message: sdk.Message{
			Role:    sdk.Assistant,
			Content: fmt.Sprintf("%s Execution failed: %v", icons.CrossMarkStyle.Render(icons.CrossMark), err),
		},
		Model: t.handler.modelService.GetCurrentModel(),
		Time:  time.Now(),
	}

	if addErr := t.handler.conversationRepo.AddMessage(errorEntry); addErr != nil {
		logger.Error("failed to add error message to conversation", "error", addErr)
	}

	return tea.Batch(
		func() tea.Msg {
			return domain.UpdateHistoryEvent{
				History: t.handler.conversationRepo.GetMessages(),
			}
		},
		func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Command execution failed: %v", err),
				Sticky: false,
			}
		},
	)()
}

func (t *ChatToolExecutor) formatBashResponse(result *domain.ToolExecutionResult) string {
	if result.Success {
		return t.formatSuccessfulBashResponse(result)
	}
	return t.formatFailedBashResponse(result)
}

func (t *ChatToolExecutor) formatSuccessfulBashResponse(result *domain.ToolExecutionResult) string {
	if bashResult, ok := result.Data.(*domain.BashToolResult); ok {
		responseContent := fmt.Sprintf("%s Command executed successfully:\n\n```bash\n$ %s\n```\n\n", icons.CheckMarkStyle.Render(icons.CheckMark), bashResult.Command)
		if bashResult.Output != "" {
			responseContent += fmt.Sprintf("**Output:**\n```\n%s\n```", strings.TrimSpace(bashResult.Output))
		}
		if bashResult.Duration != "" {
			responseContent += fmt.Sprintf("\n\n*Execution time: %s*", bashResult.Duration)
		}
		return responseContent
	}
	return fmt.Sprintf("%s Command executed successfully (no output)", icons.CheckMarkStyle.Render(icons.CheckMark))
}

func (t *ChatToolExecutor) formatFailedBashResponse(result *domain.ToolExecutionResult) string {
	if bashResult, ok := result.Data.(*domain.BashToolResult); ok {
		responseContent := fmt.Sprintf("%s Command failed with exit code %d:\n\n```bash\n$ %s\n```\n\n", icons.CrossMarkStyle.Render(icons.CrossMark), bashResult.ExitCode, bashResult.Command)
		if bashResult.Output != "" {
			responseContent += fmt.Sprintf("**Output:**\n```\n%s\n```", strings.TrimSpace(bashResult.Output))
		}
		if bashResult.Error != "" {
			responseContent += fmt.Sprintf("\n\n**Error:** %s", bashResult.Error)
		}
		return responseContent
	} else if result.Error != "" {
		return fmt.Sprintf("%s Command failed: %s", icons.CrossMarkStyle.Render(icons.CrossMark), result.Error)
	}
	return fmt.Sprintf("%s Command failed for unknown reason", icons.CrossMarkStyle.Render(icons.CrossMark))
}

func (t *ChatToolExecutor) addBashResponseToHistory(responseContent string) {
	assistantEntry := domain.ConversationEntry{
		Message: sdk.Message{
			Role:    sdk.Assistant,
			Content: responseContent,
		},
		Model: t.handler.modelService.GetCurrentModel(),
		Time:  time.Now(),
	}

	if err := t.handler.conversationRepo.AddMessage(assistantEntry); err != nil {
		logger.Error("failed to add final assistant message", "error", err)
	}
}

func (t *ChatToolExecutor) createBashUIUpdate(success bool) tea.Msg {
	statusMsg := "Command completed"
	if !success {
		statusMsg = "Command failed"
	}

	return tea.Batch(
		func() tea.Msg {
			return domain.UpdateHistoryEvent{
				History: t.handler.conversationRepo.GetMessages(),
			}
		},
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    statusMsg,
				Spinner:    false,
				TokenUsage: t.handler.getCurrentTokenUsage(),
				StatusType: domain.StatusDefault,
			}
		},
	)()
}
