package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	domain "github.com/inference-gateway/cli/internal/domain"
	utils "github.com/inference-gateway/cli/internal/utils"
	sdk "github.com/inference-gateway/sdk"
)

// ChatCommandHandler handles various command types
type ChatCommandHandler struct {
	handler            *ChatHandler
	shortcutHandler    *ChatShortcutHandler
	bashEventChannel   <-chan tea.Msg
	bashEventChannelMu sync.RWMutex
	toolEventChannel   <-chan tea.Msg
	toolEventChannelMu sync.RWMutex
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

	if strings.HasSuffix(command, " &") || strings.HasSuffix(command, "&") {
		command = strings.TrimSuffix(command, " &")
		command = strings.TrimSuffix(command, "&")
		command = strings.TrimSpace(command)

		if command == "" {
			return func() tea.Msg {
				return domain.ShowErrorEvent{
					Error:  "No bash command provided. Use: !<command>",
					Sticky: false,
				}
			}
		}

		return c.executeBashCommandInBackground(commandText, command)
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
				ToolName:   "Bash",
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
	eventChan := make(chan tea.Msg, 10000)
	detachChan := make(chan struct{}, 1)

	c.bashEventChannelMu.Lock()
	c.bashEventChannel = eventChan
	c.bashEventChannelMu.Unlock()

	c.handler.SetBashDetachChan(detachChan)

	go func() {
		defer func() {
			time.Sleep(100 * time.Millisecond)
			close(eventChan)
			c.bashEventChannelMu.Lock()
			c.bashEventChannel = nil
			c.bashEventChannelMu.Unlock()

			c.handler.ClearBashDetachChan()
		}()

		eventChan <- domain.ToolExecutionProgressEvent{
			BaseChatEvent: domain.BaseChatEvent{
				RequestID: toolCallID,
				Timestamp: time.Now(),
			},
			ToolCallID: toolCallID,
			ToolName:   "Bash",
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
		ctx = context.WithValue(ctx, domain.BashDetachChannelKey, (<-chan struct{})(detachChan))
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

// executeBashCommandInBackground executes a bash command immediately in the background
func (c *ChatCommandHandler) executeBashCommandInBackground(commandText, command string) tea.Cmd {
	userEntry := domain.ConversationEntry{
		Message: sdk.Message{
			Role:    sdk.User,
			Content: sdk.NewMessageContent(commandText),
		},
		Time: time.Now(),
	}
	_ = c.handler.conversationRepo.AddMessage(userEntry)

	go func() {
		ctx := context.WithValue(context.Background(), domain.ToolApprovedKey, true)

		cmd := exec.CommandContext(ctx, "bash", "-c", command)

		outputBuffer := utils.NewOutputRingBuffer(1024 * 1024)

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return
		}

		stderr, err := cmd.StderrPipe()
		if err != nil {
			return
		}

		if err := cmd.Start(); err != nil {
			return
		}

		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			scanner := bufio.NewScanner(stdout)
			for scanner.Scan() {
				line := scanner.Text()
				_, _ = outputBuffer.Write([]byte(line + "\n"))
			}
		}()

		go func() {
			defer wg.Done()
			scanner := bufio.NewScanner(stderr)
			for scanner.Scan() {
				line := scanner.Text()
				_, _ = outputBuffer.Write([]byte(line + "\n"))
			}
		}()

		shellID, err := c.handler.backgroundShellService.DetachToBackground(
			ctx,
			cmd,
			command,
			outputBuffer,
		)

		if err != nil {
			return
		}

		wg.Wait()

		assistantEntry := domain.ConversationEntry{
			Message: sdk.Message{
				Role:    sdk.Assistant,
				Content: sdk.NewMessageContent(fmt.Sprintf("Command sent to the background with ID: %s. Use ListShells() to view background shells or BashOutput(shell_id=\"%s\") to view output.", shellID, shellID)),
			},
			Time: time.Now(),
		}
		_ = c.handler.conversationRepo.AddMessage(assistantEntry)
	}()

	return tea.Batch(
		func() tea.Msg {
			return domain.UpdateHistoryEvent{
				History: c.handler.conversationRepo.GetMessages(),
			}
		},
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    fmt.Sprintf("Starting command in background: %s", command),
				Spinner:    false,
				StatusType: domain.StatusDefault,
			}
		},
	)
}

// handleBackgroundShellRequest handles a request to background a running bash command
func (c *ChatCommandHandler) handleBackgroundShellRequest() tea.Cmd {
	detachChan := c.handler.GetBashDetachChan()

	if detachChan == nil {
		return func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  "No running Bash command to background",
				Sticky: false,
			}
		}
	}

	select {
	case detachChan <- struct{}{}:
		return func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    "Moving command to background...",
				Spinner:    false,
				StatusType: domain.StatusDefault,
			}
		}
	default:
		return func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  "Failed to signal detach to running command",
				Sticky: false,
			}
		}
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

	argsJSON, err := json.Marshal(args)
	if err != nil {
		return func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Failed to marshal arguments: %v", err),
				Sticky: false,
			}
		}
	}

	return c.executeToolCommand(commandText, toolName, string(argsJSON))
}

// executeToolCommand executes a tool command without approval
func (c *ChatCommandHandler) executeToolCommand(commandText, toolName, argsJSON string) tea.Cmd {
	toolCallID := fmt.Sprintf("user-tool-%d", time.Now().UnixNano())

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
				Message:    fmt.Sprintf("Executing: %s", toolName),
				Spinner:    true,
				StatusType: domain.StatusWorking,
				ToolName:   toolName,
			}
		},
		func() tea.Msg {
			return domain.ParallelToolsStartEvent{
				BaseChatEvent: domain.BaseChatEvent{
					RequestID: toolCallID,
					Timestamp: time.Now(),
				},
				Tools: []domain.ToolInfo{
					{
						CallID: toolCallID,
						Name:   toolName,
						Status: "starting",
					},
				},
			}
		},
		c.executeToolCommandAsync(toolName, argsJSON, toolCallID),
	)
}

// executeToolCommandAsync executes the tool command asynchronously and returns results
func (c *ChatCommandHandler) executeToolCommandAsync(toolName, argsJSON, toolCallID string) tea.Cmd {
	eventChan := make(chan tea.Msg, 100)

	c.toolEventChannelMu.Lock()
	c.toolEventChannel = eventChan
	c.toolEventChannelMu.Unlock()

	go func() {
		defer func() {
			time.Sleep(100 * time.Millisecond)
			close(eventChan)
			c.toolEventChannelMu.Lock()
			c.toolEventChannel = nil
			c.toolEventChannelMu.Unlock()
		}()

		eventChan <- domain.ToolExecutionProgressEvent{
			BaseChatEvent: domain.BaseChatEvent{
				RequestID: toolCallID,
				Timestamp: time.Now(),
			},
			ToolCallID: toolCallID,
			ToolName:   toolName,
			Status:     "running",
			Message:    "Executing...",
		}

		toolCallFunc := sdk.ChatCompletionMessageToolCallFunction{
			Name:      toolName,
			Arguments: argsJSON,
		}

		ctx := context.WithValue(context.Background(), domain.ToolApprovedKey, true)
		result, err := c.handler.toolService.ExecuteToolDirect(ctx, toolCallFunc)
		if err != nil {
			eventChan <- domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Failed to execute tool: %v", err),
				Sticky: false,
			}
			return
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

		status := "complete"
		message := "Completed successfully"
		if result != nil && !result.Success {
			status = "failed"
			message = "Execution failed"
		}

		eventChan <- domain.ToolExecutionProgressEvent{
			BaseChatEvent: domain.BaseChatEvent{
				RequestID: toolCallID,
				Timestamp: time.Now(),
			},
			ToolCallID: toolCallID,
			ToolName:   toolName,
			Status:     status,
			Message:    message,
		}

		eventChan <- domain.UpdateHistoryEvent{
			History: c.handler.conversationRepo.GetMessages(),
		}

		eventChan <- domain.SetStatusEvent{
			Message:    fmt.Sprintf("%s %s", toolName, message),
			Spinner:    false,
			StatusType: domain.StatusDefault,
		}
	}()

	return c.listenToToolEvents(eventChan)
}

// listenToToolEvents listens for tool execution events from the channel
func (c *ChatCommandHandler) listenToToolEvents(eventChan <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-eventChan
		if !ok {
			return nil
		}
		return msg
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
