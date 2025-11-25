package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	domain "github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
)

// ChatCommandHandler handles various command types
type ChatCommandHandler struct {
	handler            *ChatHandler
	shortcutHandler    *ChatShortcutHandler
	bashEventChannel   <-chan tea.Msg
	bashEventChannelMu sync.RWMutex
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
) tea.Cmd {
	if c.handler.shortcutRegistry == nil {
		return func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  "Shortcut registry not available",
				Sticky: false,
			}
		}
	}

	mainShortcut, args, err := c.handler.shortcutRegistry.ParseShortcut(commandText)
	if err != nil {
		return func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Invalid shortcut format: %v", err),
				Sticky: false,
			}
		}
	}

	return c.shortcutHandler.executeShortcut(mainShortcut, args)
}

// handleBashCommand processes bash commands starting with !
func (c *ChatCommandHandler) handleBashCommand(
	commandText string,
) tea.Cmd {
	command := strings.TrimSpace(strings.TrimPrefix(commandText, "!"))

	if command == "" {
		return func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  "No bash command provided. Use: !<command>",
				Sticky: false,
			}
		}
	}

	return c.executeBashCommand(commandText, command)
}

// executeBashCommand executes a bash command without approval
func (c *ChatCommandHandler) executeBashCommand(commandText, command string) tea.Cmd {
	toolCallID := fmt.Sprintf("user-bash-%d", time.Now().UnixNano())

	userEntry := domain.ConversationEntry{
		Message: sdk.Message{
			Role:    sdk.User,
			Content: sdk.NewMessageContent(commandText),
		},
		Time: time.Now(),
	}
	_ = c.handler.conversationRepo.AddMessage(userEntry)

	return tea.Batch(
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
		// Emit ParallelToolsStartEvent so UI knows to display this tool
		func() tea.Msg {
			return domain.ParallelToolsStartEvent{
				BaseChatEvent: domain.BaseChatEvent{
					RequestID: toolCallID,
					Timestamp: time.Now(),
				},
				Tools: []domain.ToolInfo{
					{
						CallID: toolCallID,
						Name:   "Bash",
						Status: "starting",
					},
				},
			}
		},
		c.executeBashCommandAsync(command, toolCallID),
	)
}

// executeBashCommandAsync executes the bash command and returns results
func (c *ChatCommandHandler) executeBashCommandAsync(command string, toolCallID string) tea.Cmd {
	eventChan := make(chan tea.Msg, 100)

	c.bashEventChannelMu.Lock()
	c.bashEventChannel = eventChan
	c.bashEventChannelMu.Unlock()

	go func() {
		defer func() {
			close(eventChan)
			c.bashEventChannelMu.Lock()
			c.bashEventChannel = nil
			c.bashEventChannelMu.Unlock()
		}()

		eventChan <- domain.ToolExecutionProgressEvent{
			BaseChatEvent: domain.BaseChatEvent{
				RequestID: toolCallID,
				Timestamp: time.Now(),
			},
			ToolCallID: toolCallID,
			Status:     "running",
			Message:    "Executing...",
		}

		toolCallFunc := sdk.ChatCompletionMessageToolCallFunction{
			Name:      "Bash",
			Arguments: fmt.Sprintf(`{"command": "%s"}`, strings.ReplaceAll(command, `"`, `\"`)),
		}

		bashCallback := func(line string) {
			eventChan <- domain.BashOutputChunkEvent{
				BaseChatEvent: domain.BaseChatEvent{
					RequestID: toolCallID,
					Timestamp: time.Now(),
				},
				ToolCallID: toolCallID,
				Output:     line,
				IsComplete: false,
			}
		}

		ctx := context.WithValue(context.Background(), domain.ToolApprovedKey, true)
		ctx = context.WithValue(ctx, domain.BashOutputCallbackKey, domain.BashOutputCallback(bashCallback))
		result, err := c.handler.toolService.ExecuteToolDirect(ctx, toolCallFunc)

		if err != nil {
			eventChan <- domain.ToolExecutionProgressEvent{
				BaseChatEvent: domain.BaseChatEvent{
					RequestID: toolCallID,
					Timestamp: time.Now(),
				},
				ToolCallID: toolCallID,
				Status:     "failed",
				Message:    "Execution failed",
			}
			eventChan <- domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Failed to execute command: %v", err),
				Sticky: false,
			}
			return
		}

		eventChan <- domain.ToolExecutionProgressEvent{
			BaseChatEvent: domain.BaseChatEvent{
				RequestID: toolCallID,
				Timestamp: time.Now(),
			},
			ToolCallID: toolCallID,
			Status:     "complete",
			Message:    "Completed successfully",
		}

		toolCalls := []sdk.ChatCompletionMessageToolCall{
			{
				Id:       toolCallID,
				Type:     "function",
				Function: toolCallFunc,
			},
		}
		assistantEntry := domain.ConversationEntry{
			Message: sdk.Message{
				Role:      sdk.Assistant,
				Content:   sdk.NewMessageContent(""),
				ToolCalls: &toolCalls,
			},
			Time: time.Now(),
		}
		_ = c.handler.conversationRepo.AddMessage(assistantEntry)

		toolEntry := domain.ConversationEntry{
			Message: sdk.Message{
				Role:       sdk.Tool,
				Content:    sdk.NewMessageContent(""),
				ToolCallId: &toolCallID,
			},
			ToolExecution: result,
			Time:          time.Now(),
		}
		_ = c.handler.conversationRepo.AddMessage(toolEntry)

		eventChan <- domain.BashCommandCompletedEvent{
			History: c.handler.conversationRepo.GetMessages(),
		}
	}()

	return c.listenToBashEvents(eventChan)
}

// listenToBashEvents listens for bash execution events from the channel
func (c *ChatCommandHandler) listenToBashEvents(eventChan <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-eventChan
		if !ok {
			return nil
		}
		return msg
	}
}

// handleToolCommand processes tool commands starting with !!
func (c *ChatCommandHandler) handleToolCommand(
	commandText string,
) tea.Cmd {
	command := strings.TrimSpace(strings.TrimPrefix(commandText, "!!"))

	if command == "" {
		return func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  "No tool command provided. Use: !!ToolName(arg=\"value\")",
				Sticky: false,
			}
		}
	}

	toolName, args, err := c.ParseToolCall(command)
	if err != nil {
		return func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Invalid tool syntax: %v. Use: !!ToolName(arg=\"value\")", err),
				Sticky: false,
			}
		}
	}

	if !c.handler.toolService.IsToolEnabled(toolName) {
		return func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Tool '%s' is not enabled. Check 'infer config tools list' for available tools.", toolName),
				Sticky: false,
			}
		}
	}

	requiresApproval := c.handler.configService.IsApprovalRequired(toolName)

	argsJSON, err := json.Marshal(args)
	if err != nil {
		return func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Failed to marshal arguments: %v", err),
				Sticky: false,
			}
		}
	}

	if requiresApproval {
		return c.handleToolCommandWithApproval(toolName, string(argsJSON))
	}

	return c.executeToolCommand(commandText, toolName, string(argsJSON))
}

// executeToolCommand executes a tool command without approval
func (c *ChatCommandHandler) executeToolCommand(commandText, toolName, argsJSON string) tea.Cmd {
	return func() tea.Msg {
		toolCallID := fmt.Sprintf("user-tool-%d", time.Now().UnixNano())

		toolCallFunc := sdk.ChatCompletionMessageToolCallFunction{
			Name:      toolName,
			Arguments: argsJSON,
		}

		result, err := c.handler.toolService.ExecuteTool(context.Background(), toolCallFunc)
		if err != nil {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Failed to execute tool: %v", err),
				Sticky: false,
			}
		}

		userEntry := domain.ConversationEntry{
			Message: sdk.Message{
				Role:    sdk.User,
				Content: sdk.NewMessageContent(commandText),
			},
			Time: time.Now(),
		}
		_ = c.handler.conversationRepo.AddMessage(userEntry)

		toolCalls := []sdk.ChatCompletionMessageToolCall{
			{
				Id:       toolCallID,
				Type:     "function",
				Function: toolCallFunc,
			},
		}
		assistantEntry := domain.ConversationEntry{
			Message: sdk.Message{
				Role:      sdk.Assistant,
				Content:   sdk.NewMessageContent(""),
				ToolCalls: &toolCalls,
			},
			Time: time.Now(),
		}
		_ = c.handler.conversationRepo.AddMessage(assistantEntry)

		toolEntry := domain.ConversationEntry{
			Message: sdk.Message{
				Role:       sdk.Tool,
				Content:    sdk.NewMessageContent(""),
				ToolCallId: &toolCallID,
			},
			ToolExecution: result,
			Time:          time.Now(),
		}
		_ = c.handler.conversationRepo.AddMessage(toolEntry)

		return domain.UpdateHistoryEvent{
			History: c.handler.conversationRepo.GetMessages(),
		}
	}
}

// handleToolCommandWithApproval requests approval before executing a tool command
func (c *ChatCommandHandler) handleToolCommandWithApproval(toolName, argsJSON string) tea.Cmd {
	return func() tea.Msg {
		toolCall := sdk.ChatCompletionMessageToolCall{
			Id:   fmt.Sprintf("manual-%d", time.Now().UnixNano()),
			Type: "function",
			Function: sdk.ChatCompletionMessageToolCallFunction{
				Name:      toolName,
				Arguments: argsJSON,
			},
		}

		responseChan := make(chan domain.ApprovalAction, 1)

		return domain.ToolApprovalRequestedEvent{
			RequestID:    fmt.Sprintf("manual-tool-%d", time.Now().UnixNano()),
			Timestamp:    time.Now(),
			ToolCall:     toolCall,
			ResponseChan: responseChan,
		}
	}
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
