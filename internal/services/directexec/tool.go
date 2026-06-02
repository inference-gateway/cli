package directexec

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	sdk "github.com/inference-gateway/sdk"

	domain "github.com/inference-gateway/cli/internal/domain"
)

// HandleToolCommand processes user-typed `!!ToolName(arg="value")` commands.
// Parses the syntax, validates the tool is enabled, and dispatches to an
// async executor.
func (s *Service) HandleToolCommand(commandText string) tea.Cmd {
	command := strings.TrimSpace(strings.TrimPrefix(commandText, "!!"))

	if command == "" {
		return func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  "No tool command provided. Use: !!ToolName(arg=\"value\")",
				Sticky: false,
			}
		}
	}

	toolName, args, err := s.ParseToolCall(command)
	if err != nil {
		return func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Invalid tool syntax: %v. Use: !!ToolName(arg=\"value\")", err),
				Sticky: false,
			}
		}
	}

	if !s.toolService.IsToolEnabled(toolName) {
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

	return s.executeToolCommand(commandText, toolName, string(argsJSON))
}

// executeToolCommand synthesizes the user-tool- conversation entries and
// dispatches the async tool execution.
func (s *Service) executeToolCommand(commandText, toolName, argsJSON string) tea.Cmd {
	toolCallID := fmt.Sprintf("user-tool-%d", time.Now().UnixNano())

	userEntry := domain.ConversationEntry{
		Message: sdk.Message{
			Role:    sdk.User,
			Content: sdk.NewMessageContent(commandText),
		},
		Time: time.Now(),
	}
	_ = s.conversationRepo.AddMessage(userEntry)

	return tea.Batch(
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    fmt.Sprintf("Executing: %s", toolName),
				Spinner:    true,
				StatusType: domain.StatusWorking,
				ToolName:   toolName,
			}
		},
		func() tea.Msg {
			return domain.ToolExecutionProgressEvent{
				BaseChatEvent: domain.BaseChatEvent{
					RequestID: toolCallID,
					Timestamp: time.Now(),
				},
				ToolCallID: toolCallID,
				ToolName:   toolName,
				Arguments:  argsJSON,
				Status:     "starting",
				Message:    "",
			}
		},
		s.executeToolCommandAsync(toolName, argsJSON, toolCallID),
	)
}

// executeToolCommandAsync spawns the goroutine that runs the tool and pipes
// progress events back over a fresh per-invocation channel.
func (s *Service) executeToolCommandAsync(toolName, argsJSON, toolCallID string) tea.Cmd {
	eventChan := make(chan tea.Msg, 100)

	s.setToolEventChannel(eventChan)

	go func() {
		defer func() {
			time.Sleep(100 * time.Millisecond)
			close(eventChan)
			s.setToolEventChannel(nil)
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

		ctx := domain.WithToolApproved(context.Background())
		ctx = domain.WithDirectExecution(ctx)
		result, err := s.toolService.ExecuteToolDirect(ctx, toolCallFunc)
		if err != nil {
			eventChan <- domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Failed to execute tool: %v", err),
				Sticky: false,
			}
			return
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

		toolEntry := domain.ConversationEntry{
			Message: sdk.Message{
				Role:       sdk.Tool,
				Content:    sdk.NewMessageContent(""),
				ToolCallID: &toolCallID,
			},
			ToolExecution: result,
			Time:          time.Now(),
		}
		_ = s.conversationRepo.AddMessage(toolEntry)

		status := "completed"
		message := "Completed successfully"
		if result != nil && !result.Success {
			status = "failed"
			message = "Execution failed"
		}

		var images []domain.ImageAttachment
		if result != nil && len(result.Images) > 0 {
			for _, img := range result.Images {
				images = append(images, domain.ImageAttachment{
					Data:        img.Data,
					MimeType:    img.MimeType,
					DisplayName: img.DisplayName,
				})
			}
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
			Images:     images,
		}

		eventChan <- domain.UpdateHistoryEvent{
			History: s.conversationRepo.GetMessages(),
		}

		eventChan <- domain.SetStatusEvent{
			Message:    fmt.Sprintf("%s %s", toolName, message),
			Spinner:    false,
			StatusType: domain.StatusDefault,
		}

		// Clear ToolCallRenderer previews now that the tool entry is in
		// conversation history.
		eventChan <- domain.ChatCompleteEvent{
			RequestID: toolCallID,
			Timestamp: time.Now(),
			Message:   "",
			ToolCalls: []sdk.ChatCompletionMessageToolCall{},
		}
	}()

	return s.listener.ListenForEvents(eventChan)
}
