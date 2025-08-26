package handlers

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	services "github.com/inference-gateway/cli/internal/services"
	sdk "github.com/inference-gateway/sdk"
)

// ChatEventHandler handles chat events
type ChatEventHandler struct {
	handler *ChatHandler
}

// NewChatEventHandler creates a new event handler
func NewChatEventHandler(handler *ChatHandler) *ChatEventHandler {
	return &ChatEventHandler{
		handler: handler,
	}
}

// handleChatStart processes chat start events
func (e *ChatEventHandler) handleChatStart(
	_ domain.ChatStartEvent,
	stateManager *services.StateManager,
) (tea.Model, tea.Cmd) {
	_ = stateManager.UpdateChatStatus(domain.ChatStatusStarting)

	var cmds []tea.Cmd
	cmds = append(cmds, func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    "Starting response...",
			Spinner:    true,
			StatusType: domain.StatusGenerating,
		}
	})

	if chatSession := stateManager.GetChatSession(); chatSession != nil {
		cmds = append(cmds, e.handler.listenForChatEvents(chatSession.EventChannel))
	}

	return nil, tea.Batch(cmds...)
}

// handleChatChunk processes chat chunk events
func (e *ChatEventHandler) handleChatChunk(
	msg domain.ChatChunkEvent,
	stateManager *services.StateManager,
) (tea.Model, tea.Cmd) {
	chatSession := stateManager.GetChatSession()
	if chatSession == nil {
		return e.handleNoChatSession(msg)
	}

	if msg.Content == "" && msg.ReasoningContent == "" {
		return e.handleEmptyContent(chatSession)
	}

	e.updateConversationHistory(msg, chatSession)

	cmds := []tea.Cmd{
		func() tea.Msg {
			return domain.UpdateHistoryEvent{
				History: e.handler.conversationRepo.GetMessages(),
			}
		},
	}

	statusCmds := e.handleStatusUpdate(msg, chatSession, stateManager)
	cmds = append(cmds, statusCmds...)

	if chatSession := stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, e.handler.listenForChatEvents(chatSession.EventChannel))
	}

	return nil, tea.Batch(cmds...)
}

// handleNoChatSession handles the case when there's no active chat session
func (e *ChatEventHandler) handleNoChatSession(msg domain.ChatChunkEvent) (tea.Model, tea.Cmd) {
	if msg.ReasoningContent != "" {
		return nil, func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    "Thinking...",
				Spinner:    true,
				StatusType: domain.StatusThinking,
			}
		}
	}
	return nil, nil
}

// handleEmptyContent handles the case when the message has no content
func (e *ChatEventHandler) handleEmptyContent(chatSession *domain.ChatSession) (tea.Model, tea.Cmd) {
	if chatSession != nil && chatSession.EventChannel != nil {
		return nil, e.handler.listenForChatEvents(chatSession.EventChannel)
	}
	return nil, nil
}

// updateConversationHistory updates the conversation history with the new message
func (e *ChatEventHandler) updateConversationHistory(msg domain.ChatChunkEvent, chatSession *domain.ChatSession) {
	messages := e.handler.conversationRepo.GetMessages()
	shouldUpdateLast := e.shouldUpdateLastMessage(messages, chatSession)

	if shouldUpdateLast {
		e.updateLastMessage(messages, msg, chatSession)
	} else {
		e.addNewMessage(msg, chatSession)
	}
}

// shouldUpdateLastMessage determines if we should update the last message or add a new one
func (e *ChatEventHandler) shouldUpdateLastMessage(messages []domain.ConversationEntry, chatSession *domain.ChatSession) bool {
	return len(messages) > 0 &&
		messages[len(messages)-1].Message.Role == sdk.Assistant &&
		chatSession.RequestID != ""
}

// updateLastMessage updates the existing last message with new content
func (e *ChatEventHandler) updateLastMessage(messages []domain.ConversationEntry, msg domain.ChatChunkEvent, _ *domain.ChatSession) {
	existingContent := messages[len(messages)-1].Message.Content
	newContent := existingContent + msg.Content

	if err := e.handler.conversationRepo.UpdateLastMessage(newContent); err != nil {
		logger.Error("failed to update last message", "error", err)
	}
}

// addNewMessage adds a new assistant message to the conversation
func (e *ChatEventHandler) addNewMessage(msg domain.ChatChunkEvent, _ *domain.ChatSession) {
	assistantEntry := domain.ConversationEntry{
		Message: sdk.Message{
			Role:    sdk.Assistant,
			Content: msg.Content,
		},
		Model: e.handler.modelService.GetCurrentModel(),
		Time:  msg.Timestamp,
	}

	if err := e.handler.conversationRepo.AddMessage(assistantEntry); err != nil {
		logger.Error("failed to add assistant message", "error", err)
	}
}

// handleStatusUpdate handles updating the chat status and returns appropriate commands
func (e *ChatEventHandler) handleStatusUpdate(msg domain.ChatChunkEvent, chatSession *domain.ChatSession, stateManager *services.StateManager) []tea.Cmd {
	newStatus, shouldUpdateStatus := e.determineNewStatus(msg, chatSession.Status, chatSession.IsFirstChunk)

	if !shouldUpdateStatus {
		return nil
	}

	_ = stateManager.UpdateChatStatus(newStatus)

	if chatSession.IsFirstChunk {
		chatSession.IsFirstChunk = false
		return e.createFirstChunkStatusCmd(newStatus)
	}

	if newStatus != chatSession.Status {
		return e.createStatusUpdateCmd(newStatus)
	}

	return nil
}

// determineNewStatus determines what the new status should be based on message content
func (e *ChatEventHandler) determineNewStatus(msg domain.ChatChunkEvent, currentStatus domain.ChatStatus, _ bool) (domain.ChatStatus, bool) {
	if msg.ReasoningContent != "" {
		return domain.ChatStatusThinking, true
	}

	if msg.Content != "" {
		return domain.ChatStatusGenerating, true
	}

	return currentStatus, false
}

// createFirstChunkStatusCmd creates status command for the first chunk
func (e *ChatEventHandler) createFirstChunkStatusCmd(status domain.ChatStatus) []tea.Cmd {
	switch status {
	case domain.ChatStatusThinking:
		return []tea.Cmd{func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    "Thinking...",
				Spinner:    true,
				StatusType: domain.StatusThinking,
			}
		}}
	case domain.ChatStatusGenerating:
		return []tea.Cmd{func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    "Generating response...",
				Spinner:    true,
				StatusType: domain.StatusGenerating,
			}
		}}
	}
	return nil
}

// createStatusUpdateCmd creates status update command for status changes
func (e *ChatEventHandler) createStatusUpdateCmd(status domain.ChatStatus) []tea.Cmd {
	switch status {
	case domain.ChatStatusThinking:
		return []tea.Cmd{func() tea.Msg {
			return domain.UpdateStatusEvent{
				Message:    "Thinking...",
				StatusType: domain.StatusThinking,
			}
		}}
	case domain.ChatStatusGenerating:
		return []tea.Cmd{func() tea.Msg {
			return domain.UpdateStatusEvent{
				Message:    "Generating response...",
				StatusType: domain.StatusGenerating,
			}
		}}
	}
	return nil
}

// handleChatComplete processes chat completion events
func (e *ChatEventHandler) handleChatComplete(
	msg domain.ChatCompleteEvent,
	stateManager *services.StateManager,
) (tea.Model, tea.Cmd) {
	_ = stateManager.UpdateChatStatus(domain.ChatStatusCompleted)

	stateManager.EndChatSession()

	if len(msg.ToolCalls) > 0 {
		_, cmd := e.handler.toolOrchestrator.StartToolExecution(msg.RequestID, msg.ToolCalls)

		return nil, tea.Batch(
			func() tea.Msg {
				return domain.UpdateHistoryEvent{
					History: e.handler.conversationRepo.GetMessages(),
				}
			},
			cmd,
		)
	}

	statusMsg := "Response complete"
	tokenUsage := ""
	if msg.Metrics != nil {
		e.handler.addTokenUsageToSession(msg.Metrics)
		tokenUsage = e.FormatMetrics(msg.Metrics)
	}

	e.handler.assistantMessageCounter++

	cmds := []tea.Cmd{
		func() tea.Msg {
			return domain.UpdateHistoryEvent{
				History: e.handler.conversationRepo.GetMessages(),
			}
		},
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    statusMsg,
				Spinner:    false,
				TokenUsage: tokenUsage,
				StatusType: domain.StatusDefault,
			}
		},
	}

	if e.handler.shouldInjectSystemReminder() {
		cmds = append(cmds, e.handler.injectSystemReminder())
	}

	return nil, tea.Batch(cmds...)
}

// handleChatError processes chat error events
func (e *ChatEventHandler) handleChatError(
	msg domain.ChatErrorEvent,
	stateManager *services.StateManager,
) (tea.Model, tea.Cmd) {
	_ = stateManager.UpdateChatStatus(domain.ChatStatusError)
	stateManager.EndChatSession()
	stateManager.EndToolExecution()

	_ = stateManager.TransitionToView(domain.ViewStateChat)

	errorMsg := fmt.Sprintf("Chat error: %v", msg.Error)
	if strings.Contains(msg.Error.Error(), "timed out") {
		errorMsg = fmt.Sprintf("⏰ %v\n\nSuggestions:\n• Try breaking your request into smaller parts\n• Check if the server is overloaded\n• Verify your network connection", msg.Error)
	}

	return nil, func() tea.Msg {
		return domain.ShowErrorEvent{
			Error:  errorMsg,
			Sticky: true,
		}
	}
}

// handleToolCallStart processes tool call start events
func (e *ChatEventHandler) handleToolCallStart(
	_ domain.ToolCallStartEvent,
	stateManager *services.StateManager,
) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	cmds = append(cmds, func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    "Working...",
			Spinner:    true,
			StatusType: domain.StatusWorking,
		}
	})

	if chatSession := stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, e.handler.listenForChatEvents(chatSession.EventChannel))
	}

	return nil, tea.Batch(cmds...)
}

// handleToolCall processes individual tool call events and executes tools immediately when JSON is complete
func (e *ChatEventHandler) handleToolCall(
	msg domain.ToolCallEvent,
	stateManager *services.StateManager,
) (tea.Model, tea.Cmd) {
	args := strings.TrimSpace(msg.Args)
	toolName := strings.TrimSpace(msg.ToolName)

	if args != "" && toolName != "" && strings.HasSuffix(args, "}") {
		var temp any
		if json.Unmarshal([]byte(args), &temp) == nil {
			return nil, tea.Batch(
				func() tea.Msg {
					return domain.SetStatusEvent{
						Message:    fmt.Sprintf("Executing tool: %s", toolName),
						Spinner:    true,
						StatusType: domain.StatusWorking,
					}
				},
				e.executeToolCall(msg.RequestID, msg.ToolCallID, toolName, args, stateManager),
			)
		}
	}

	return nil, func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    fmt.Sprintf("Receiving tool call: %s", toolName),
			Spinner:    true,
			StatusType: domain.StatusWorking,
		}
	}
}

// executeToolCall executes a single tool call and adds the result to conversation history
func (e *ChatEventHandler) executeToolCall(
	requestID string,
	toolCallID string,
	toolName string,
	arguments string,
	_ *services.StateManager,
) tea.Cmd {
	return func() tea.Msg {
		var argsMap map[string]any
		if err := json.Unmarshal([]byte(arguments), &argsMap); err != nil {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Failed to parse tool arguments for %s: %v", toolName, err),
				Sticky: false,
			}
		}

		toolCall := sdk.ChatCompletionMessageToolCall{
			Id:   toolCallID,
			Type: sdk.Function,
			Function: sdk.ChatCompletionMessageToolCallFunction{
				Name:      toolName,
				Arguments: arguments,
			},
		}

		assistantEntry := domain.ConversationEntry{
			Message: sdk.Message{
				Role:      sdk.Assistant,
				Content:   "",
				ToolCalls: &[]sdk.ChatCompletionMessageToolCall{toolCall},
			},
			Model: e.handler.modelService.GetCurrentModel(),
			Time:  time.Now(),
		}

		if err := e.handler.conversationRepo.AddMessage(assistantEntry); err != nil {
			logger.Error("failed to add assistant message with tool call", "error", err)
		}

		toolCalls := []sdk.ChatCompletionMessageToolCall{toolCall}

		_, cmd := e.handler.toolOrchestrator.StartToolExecution(requestID, toolCalls)
		return tea.Batch(
			func() tea.Msg {
				return domain.UpdateHistoryEvent{
					History: e.handler.conversationRepo.GetMessages(),
				}
			},
			cmd,
		)()
	}
}

func (e *ChatEventHandler) handleToolExecutionStarted(
	msg domain.ToolExecutionStartedEvent,
	_ *services.StateManager,
) (tea.Model, tea.Cmd) {

	return nil, func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    fmt.Sprintf("Starting tool execution (%d tools)", msg.TotalTools),
			Spinner:    true,
			StatusType: domain.StatusWorking,
		}
	}
}

func (e *ChatEventHandler) handleToolExecutionProgress(
	msg domain.ToolExecutionProgressEvent,
	stateManager *services.StateManager,
) (tea.Model, tea.Cmd) {

	if msg.RequiresApproval {
		_ = stateManager.SetToolApprovalRequired(true)
		stateManager.SetupApprovalUI()
		_ = stateManager.TransitionToView(domain.ViewStateToolApproval)
	}

	return nil, func() tea.Msg {
		return domain.SetStatusEvent{
			Message: fmt.Sprintf("Tool %d/%d: %s (%s)",
				msg.CurrentTool, msg.TotalTools, msg.ToolName, msg.Status),
			Spinner:    true,
			StatusType: domain.StatusWorking,
		}
	}
}

func (e *ChatEventHandler) handleToolExecutionCompleted(
	msg domain.ToolExecutionCompletedEvent,
	stateManager *services.StateManager,
) (tea.Model, tea.Cmd) {
	return nil, tea.Batch(
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message: fmt.Sprintf("Tools completed (%d/%d successful) - preparing response...",
					msg.SuccessCount, msg.TotalExecuted),
				Spinner:    true,
				StatusType: domain.StatusPreparing,
			}
		},
		e.handler.startChatCompletion(stateManager),
	)
}

func (e *ChatEventHandler) handleToolApprovalRequest(
	_ domain.ToolApprovalRequestEvent,
	_ *services.StateManager,
) (tea.Model, tea.Cmd) {
	return nil, nil
}

func (e *ChatEventHandler) handleToolApprovalResponse(
	msg domain.ToolApprovalResponseEvent,
	_ *services.StateManager,
) (tea.Model, tea.Cmd) {
	return nil, e.handler.toolOrchestrator.HandleApprovalResponse(msg.Approved, msg.ToolIndex)
}

func (e *ChatEventHandler) FormatMetrics(metrics *domain.ChatMetrics) string {
	if metrics == nil {
		return ""
	}

	var parts []string

	if metrics.Duration > 0 {
		duration := metrics.Duration.Round(time.Millisecond)
		parts = append(parts, fmt.Sprintf("Time: %v", duration))
	}

	if metrics.Usage != nil {
		if metrics.Usage.PromptTokens > 0 {
			parts = append(parts, fmt.Sprintf("Input: %d tokens", metrics.Usage.PromptTokens))
		}
		if metrics.Usage.CompletionTokens > 0 {
			parts = append(parts, fmt.Sprintf("Output: %d tokens", metrics.Usage.CompletionTokens))
		}
		if metrics.Usage.TotalTokens > 0 {
			parts = append(parts, fmt.Sprintf("Total: %d tokens", metrics.Usage.TotalTokens))
		}
	}

	sessionStats := e.handler.conversationRepo.GetSessionTokens()
	if sessionStats.TotalInputTokens > 0 {
		parts = append(parts, fmt.Sprintf("Session Input: %d tokens", sessionStats.TotalInputTokens))
	}

	return strings.Join(parts, " | ")
}
