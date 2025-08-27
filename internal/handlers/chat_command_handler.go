package handlers

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	services "github.com/inference-gateway/cli/internal/services"
	sdk "github.com/inference-gateway/sdk"
)

// ChatCommandHandler handles various command types
type ChatCommandHandler struct {
	handler         *ChatHandler
	shortcutHandler *ChatShortcutHandler
}

// NewChatCommandHandler creates a new command handler
func NewChatCommandHandler(handler *ChatHandler) *ChatCommandHandler {
	return &ChatCommandHandler{
		handler:         handler,
		shortcutHandler: NewChatShortcutHandler(handler),
	}
}

// handleCommand processes slash commands
func (c *ChatCommandHandler) handleCommand(
	commandText string,
	stateManager *services.StateManager,
) (tea.Model, tea.Cmd) {
	if c.handler.shortcutRegistry == nil {
		return nil, func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  "Shortcut registry not available",
				Sticky: false,
			}
		}
	}

	mainShortcut, args, err := c.handler.shortcutRegistry.ParseShortcut(commandText)
	if err != nil {
		return nil, func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Invalid shortcut format: %v", err),
				Sticky: false,
			}
		}
	}

	return nil, c.shortcutHandler.executeShortcut(mainShortcut, args, stateManager)
}

// handleBashCommand processes bash commands starting with !
func (c *ChatCommandHandler) handleBashCommand(
	commandText string,
	stateManager *services.StateManager,
) (tea.Model, tea.Cmd) {
	command := strings.TrimSpace(strings.TrimPrefix(commandText, "!"))

	if command == "" {
		return nil, func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  "No bash command provided. Use: !<command>",
				Sticky: false,
			}
		}
	}

	if !c.handler.toolService.IsToolEnabled("Bash") {
		return nil, func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  "Bash tool is not enabled. Run 'infer config tool bash enable' to enable it.",
				Sticky: false,
			}
		}
	}

	userEntry := domain.ConversationEntry{
		Message: sdk.Message{
			Role:    sdk.User,
			Content: commandText,
		},
		Time: time.Now(),
	}

	if err := c.handler.conversationRepo.AddMessage(userEntry); err != nil {
		logger.Error("failed to add user message", "error", err)
		return nil, func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Failed to save message: %v", err),
				Sticky: false,
			}
		}
	}

	return nil, tea.Batch(
		func() tea.Msg {
			return domain.UpdateHistoryEvent{
				History: c.handler.conversationRepo.GetMessages(),
			}
		},
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    fmt.Sprintf("Executing: %s", command),
				Spinner:    true,
				StatusType: domain.StatusWorking,
			}
		},
		c.handler.toolExecutor.executeBashCommand(command, stateManager),
	)
}

// handleToolCommand processes tool commands starting with !!
func (c *ChatCommandHandler) handleToolCommand(
	commandText string,
	stateManager *services.StateManager,
) (tea.Model, tea.Cmd) {
	command := strings.TrimSpace(strings.TrimPrefix(commandText, "!!"))

	if command == "" {
		return nil, func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  "No tool command provided. Use: !!ToolName(arg=\"value\")",
				Sticky: false,
			}
		}
	}

	toolName, args, err := c.ParseToolCall(command)
	if err != nil {
		return nil, func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Invalid tool syntax: %v. Use: !!ToolName(arg=\"value\")", err),
				Sticky: false,
			}
		}
	}

	if !c.handler.toolService.IsToolEnabled(toolName) {
		return nil, func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Tool '%s' is not enabled. Check 'infer config tools list' for available tools.", toolName),
				Sticky: false,
			}
		}
	}

	userEntry := domain.ConversationEntry{
		Message: sdk.Message{
			Role:    sdk.User,
			Content: commandText,
		},
		Time: time.Now(),
	}

	if err := c.handler.conversationRepo.AddMessage(userEntry); err != nil {
		return nil, func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Failed to save message: %v", err),
				Sticky: false,
			}
		}
	}

	return nil, tea.Batch(
		func() tea.Msg {
			return domain.UpdateHistoryEvent{
				History: c.handler.conversationRepo.GetMessages(),
			}
		},
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    fmt.Sprintf("Executing tool: %s", toolName),
				Spinner:    true,
				StatusType: domain.StatusWorking,
			}
		},
		c.handler.toolExecutor.executeToolDirectly(toolName, args, stateManager),
	)
}

// ParseToolCall parses a tool call in the format ToolName(arg="value", arg2="value2") (exposed for testing)
func (c *ChatCommandHandler) ParseToolCall(input string) (string, map[string]any, error) {
	parenIndex := strings.Index(input, "(")
	if parenIndex == -1 {
		return "", nil, fmt.Errorf("missing opening parenthesis")
	}

	toolName := strings.TrimSpace(input[:parenIndex])
	if toolName == "" {
		return "", nil, fmt.Errorf("missing tool name")
	}

	argsStr := strings.TrimSpace(input[parenIndex+1:])
	if !strings.HasSuffix(argsStr, ")") {
		return "", nil, fmt.Errorf("missing closing parenthesis")
	}

	argsStr = strings.TrimSuffix(argsStr, ")")
	argsStr = strings.TrimSpace(argsStr)

	args := make(map[string]any)
	if argsStr == "" {
		return toolName, args, nil
	}

	parsedArgs, err := c.ParseArguments(argsStr)
	if err != nil {
		return "", nil, fmt.Errorf("failed to parse arguments: %v", err)
	}

	return toolName, parsedArgs, nil
}

// ParseArguments parses function arguments in the format key="value", key2="value2" (exposed for testing)
func (c *ChatCommandHandler) ParseArguments(argsStr string) (map[string]any, error) {
	args := make(map[string]any)

	if argsStr == "" {
		return args, nil
	}

	argPattern := regexp.MustCompile(`(\w+)=("[^"]*"|'[^']*'|\w+)`)
	matches := argPattern.FindAllStringSubmatch(argsStr, -1)

	for _, match := range matches {
		if len(match) != 3 {
			continue
		}

		key := match[1]
		value := match[2]

		if (strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"")) ||
			(strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'")) {
			value = value[1 : len(value)-1]
		}

		if numValue, err := strconv.ParseFloat(value, 64); err == nil {
			args[key] = numValue
		} else {
			args[key] = value
		}
	}

	return args, nil
}
