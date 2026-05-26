package directexec

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	sdk "github.com/inference-gateway/sdk"

	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	utils "github.com/inference-gateway/cli/internal/utils"
)

// HandleBashCommand processes bash commands starting with `!`. A trailing `&`
// hands the command off to the background-shell service; otherwise it
// executes in the foreground with progress events piped over the bash event
// channel.
func (s *Service) HandleBashCommand(commandText string) tea.Cmd {
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

		return s.executeBashCommandInBackground(commandText, command)
	}

	return s.executeBashCommand(commandText, command)
}

// HandleBashOutputChunk is invoked after a bash output chunk reaches the UI.
// It keeps the bash event channel pumping until the command completes; if no
// bash channel is active, it falls back to the chat event channel.
func (s *Service) HandleBashOutputChunk(_ domain.BashOutputChunkEvent) tea.Cmd {
	if bashEventChan := s.PendingBashChannel(); bashEventChan != nil {
		return s.listener.ListenForEvents(bashEventChan)
	}

	if chatSession := s.stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		return s.listener.ListenForChatEvents(chatSession.EventChannel)
	}

	return nil
}

// HandleBashCommandCompleted handles the terminal event for a foreground
// bash run: refreshes history, surfaces an error message if the run failed,
// and (for user-initiated `!command` runs) emits a UserInputEvent to trigger
// auto-fix.
func (s *Service) HandleBashCommandCompleted(msg domain.BashCommandCompletedEvent) tea.Cmd {
	s.stateManager.EndToolExecution()

	cmds := []tea.Cmd{
		func() tea.Msg {
			return domain.UpdateHistoryEvent{History: msg.History}
		},
	}

	if msg.ErrorMessage != "" {
		cmds = append(cmds, func() tea.Msg {
			return domain.ShowErrorEvent{Error: msg.ErrorMessage, Sticky: false}
		})
	}

	if msg.Failed && msg.UserInitiated {
		logger.Info("User-initiated bash command failed - triggering auto-fix")
		cmds = append(cmds, func() tea.Msg {
			return domain.UserInputEvent{
				Content: "The bash command failed. Please analyze the error and help me fix it.",
			}
		})
	}

	return tea.Sequence(cmds...)
}

// HandleBackgroundShellRequest signals the currently-running bash command to
// detach to the background.
func (s *Service) HandleBackgroundShellRequest() tea.Cmd {
	detachChan := s.GetBashDetachChan()

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

// executeBashCommand runs a foreground bash command without approval, with a
// synthesized user-bash- tool-call entry so the model sees it in history.
func (s *Service) executeBashCommand(commandText, command string) tea.Cmd {
	toolCallID := fmt.Sprintf("user-bash-%d", time.Now().UnixNano())

	userEntry := domain.ConversationEntry{
		Message: sdk.Message{
			Role:    sdk.User,
			Content: sdk.NewMessageContent(commandText),
		},
		Time: time.Now(),
	}
	_ = s.conversationRepo.AddMessage(userEntry)

	if err := s.stateManager.StartToolExecution([]sdk.ChatCompletionMessageToolCall{{
		ID: toolCallID,
		Function: sdk.ChatCompletionMessageToolCallFunction{
			Name:      "Bash",
			Arguments: fmt.Sprintf(`{"command":"%s"}`, command),
		},
	}}); err != nil {
		return func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Failed to start tool execution: %v", err),
				Sticky: false,
			}
		}
	}

	return tea.Batch(
		func() tea.Msg {
			history := s.conversationRepo.GetMessages()
			return domain.UpdateHistoryEvent{
				History: history,
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
		s.executeBashCommandAsync(command, toolCallID),
	)
}

// executeBashCommandAsync spawns the goroutine that runs the bash command and
// pipes progress events over a fresh per-invocation channel.
func (s *Service) executeBashCommandAsync(command string, toolCallID string) tea.Cmd {
	eventChan := make(chan tea.Msg, 10000)
	detachChan := make(chan struct{}, 1)

	s.setBashEventChannel(eventChan)
	s.SetBashDetachChan(detachChan)

	go func() {
		defer func() {
			time.Sleep(100 * time.Millisecond)
			close(eventChan)
			s.setBashEventChannel(nil)
			s.ClearBashDetachChan()
		}()

		toolCallFunc := sdk.ChatCompletionMessageToolCallFunction{
			Name:      "Bash",
			Arguments: fmt.Sprintf(`{"command": "%s"}`, strings.ReplaceAll(command, `"`, `\"`)),
		}

		eventChan <- domain.ToolExecutionProgressEvent{
			BaseChatEvent: domain.BaseChatEvent{
				RequestID: toolCallID,
				Timestamp: time.Now(),
			},
			ToolCallID: toolCallID,
			ToolName:   "Bash",
			Arguments:  toolCallFunc.Arguments,
			Status:     "running",
			Message:    "",
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

		ctx := domain.WithToolApproved(context.Background())
		ctx = domain.WithBashOutputCallback(ctx, bashCallback)
		ctx = domain.WithBashDetachChannel(ctx, detachChan)
		ctx = domain.WithDirectExecution(ctx)
		result, err := s.toolService.ExecuteToolDirect(ctx, toolCallFunc)

		if err != nil {
			eventChan <- domain.BashCommandCompletedEvent{
				History:       s.conversationRepo.GetMessages(),
				Failed:        true,
				UserInitiated: strings.HasPrefix(toolCallID, "user-bash-"),
				ErrorMessage:  fmt.Sprintf("Failed to execute command: %v", err),
			}
			return
		}

		status := "completed"
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
			Status:     status,
			Message:    message,
		}

		toolCalls := []sdk.ChatCompletionMessageToolCall{
			{
				ID:       toolCallID,
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
		_ = s.conversationRepo.AddMessage(assistantEntry)

		var formattedContent string
		if result != nil {
			formattedContent = s.conversationRepo.FormatToolResultForLLM(result)
		} else {
			formattedContent = "Tool execution failed: no result returned"
		}
		toolEntry := domain.ConversationEntry{
			Message: sdk.Message{
				Role:       sdk.Tool,
				Content:    sdk.NewMessageContent(formattedContent),
				ToolCallID: &toolCallID,
			},
			ToolExecution: result,
			Time:          time.Now(),
		}
		_ = s.conversationRepo.AddMessage(toolEntry)

		isUserInitiated := strings.HasPrefix(toolCallID, "user-bash-")
		failed := result != nil && !result.Success

		eventChan <- domain.BashCommandCompletedEvent{
			History:       s.conversationRepo.GetMessages(),
			Failed:        failed,
			UserInitiated: isUserInitiated,
		}
	}()

	return s.listener.ListenForEvents(eventChan)
}

// executeBashCommandInBackground runs the bash command immediately under the
// background-shell service and returns a status update describing the shell
// id.
func (s *Service) executeBashCommandInBackground(commandText, command string) tea.Cmd {
	userEntry := domain.ConversationEntry{
		Message: sdk.Message{
			Role:    sdk.User,
			Content: sdk.NewMessageContent(commandText),
		},
		Time: time.Now(),
	}
	_ = s.conversationRepo.AddMessage(userEntry)

	go func() {
		ctx := domain.WithToolApproved(context.Background())

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

		shellID, err := s.backgroundShellService.DetachToBackground(
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
		_ = s.conversationRepo.AddMessage(assistantEntry)
	}()

	return tea.Batch(
		func() tea.Msg {
			history := s.conversationRepo.GetMessages()
			return domain.UpdateHistoryEvent{
				History: history,
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
