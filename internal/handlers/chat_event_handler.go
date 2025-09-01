package handlers

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	domain "github.com/inference-gateway/cli/internal/domain"
	services "github.com/inference-gateway/cli/internal/services"
	components "github.com/inference-gateway/cli/internal/ui/components"
	sdk "github.com/inference-gateway/sdk"
)

// ChatEventHandler handles chat events
type ChatEventHandler struct {
	handler          *ChatHandler
	toolCallRenderer *components.ToolCallRenderer
}

// NewChatEventHandler creates a new event handler
func NewChatEventHandler(handler *ChatHandler) *ChatEventHandler {
	return &ChatEventHandler{
		handler:          handler,
		toolCallRenderer: components.NewToolCallRenderer(),
	}
}

// handleChatStart processes chat start events
func (e *ChatEventHandler) handleChatStart(
	event domain.ChatStartEvent,
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
			return domain.StreamingContentEvent{
				RequestID: msg.RequestID,
				Content:   msg.Content,
				Delta:     true,
			}
		},
	}

	// Add live token usage updates during streaming when available
	if msg.Usage != nil {
		tokenUsage := e.formatLiveTokenUsage(msg.Usage)
		if tokenUsage != "" {
			cmds = append(cmds, func() tea.Msg {
				return domain.SetStatusEvent{
					Message:    "Streaming response...",
					Spinner:    true,
					TokenUsage: tokenUsage,
					StatusType: domain.StatusGenerating,
				}
			})
		}
	}

	statusCmds := e.handleStatusUpdate(msg, chatSession, stateManager)
	cmds = append(cmds, statusCmds...)

	if chatSession := stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, e.handler.listenForChatEvents(chatSession.EventChannel))
	}

	return nil, tea.Batch(cmds...)
}

// handleOptimizationStatus processes optimization status events
func (e *ChatEventHandler) handleOptimizationStatus(
	event domain.OptimizationStatusEvent,
	stateManager *services.StateManager,
) (tea.Model, tea.Cmd) {
	if event.IsActive {
		return nil, func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    event.Message,
				Spinner:    true,
				StatusType: domain.StatusProcessing,
			}
		}
	}

	return nil, func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    event.Message,
			Spinner:    false,
			StatusType: domain.StatusDefault,
		}
	}
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

// updateConversationHistory handles streaming content for UI display only (no database writes)
func (e *ChatEventHandler) updateConversationHistory(msg domain.ChatChunkEvent, chatSession *domain.ChatSession) {
	// During streaming, we don't update the database - agent handles that at completion
	// Just accumulate content in the chat session for UI display
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

	var cmds []tea.Cmd

	cmds = append(cmds, func() tea.Msg {
		return domain.UpdateHistoryEvent{
			History: e.handler.conversationRepo.GetMessages(),
		}
	})

	statusMsg := "Response complete"
	tokenUsage := ""
	if msg.Metrics != nil {
		e.handler.addTokenUsageToSession(msg.Metrics)
		tokenUsage = e.FormatMetrics(msg.Metrics)
	}

	cmds = append(cmds, func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    statusMsg,
			Spinner:    false,
			TokenUsage: tokenUsage,
			StatusType: domain.StatusDefault,
		}
	})

	if chatSession := stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, e.handler.listenForChatEvents(chatSession.EventChannel))
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

// handleToolCallPreview processes initial tool call preview events
func (e *ChatEventHandler) handleToolCallPreview(
	msg domain.ToolCallPreviewEvent,
	stateManager *services.StateManager,
) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	cmds = append(cmds, func() tea.Msg {
		return domain.UpdateHistoryEvent{
			History: e.handler.conversationRepo.GetMessages(),
		}
	})

	statusMsg := fmt.Sprintf("Preparing %s...", msg.ToolName)
	cmds = append(cmds, func() tea.Msg {
		return domain.UpdateStatusEvent{
			Message:    statusMsg,
			StatusType: domain.StatusWorking,
		}
	})

	if chatSession := stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, e.handler.listenForChatEvents(chatSession.EventChannel))
	}

	return nil, tea.Batch(cmds...)
}

// handleToolCallUpdate processes streaming updates to tool calls
func (e *ChatEventHandler) handleToolCallUpdate(
	msg domain.ToolCallUpdateEvent,
	stateManager *services.StateManager,
) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	// Reload conversation from database to show latest state
	cmds = append(cmds, func() tea.Msg {
		return domain.UpdateHistoryEvent{
			History: e.handler.conversationRepo.GetMessages(),
		}
	})

	// Update status based on tool call status
	statusMsg := fmt.Sprintf("Streaming %s...", msg.ToolName)
	if msg.Status == domain.ToolCallStreamStatusComplete {
		statusMsg = fmt.Sprintf("Completed %s", msg.ToolName)
	}

	if msg.Status == domain.ToolCallStreamStatusStreaming {
		cmds = append(cmds, func() tea.Msg {
			return domain.UpdateStatusEvent{
				Message:    statusMsg,
				StatusType: domain.StatusWorking,
			}
		})
	} else {
		cmds = append(cmds, func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    statusMsg,
				Spinner:    false,
				StatusType: domain.StatusWorking,
			}
		})
	}

	if chatSession := stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, e.handler.listenForChatEvents(chatSession.EventChannel))
	}

	return nil, tea.Batch(cmds...)
}

// handleToolCallReady is no longer used since tools are handled directly in agent
func (e *ChatEventHandler) handleToolCallReady(
	msg domain.ToolCallReadyEvent,
	stateManager *services.StateManager,
) (tea.Model, tea.Cmd) {
	cmds := []tea.Cmd{
		func() tea.Msg {
			return domain.UpdateHistoryEvent{
				History: e.handler.conversationRepo.GetMessages(),
			}
		},
	}

	if chatSession := stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, e.handler.listenForChatEvents(chatSession.EventChannel))
	}

	return nil, tea.Batch(cmds...)
}

func (e *ChatEventHandler) handleToolCallComplete(
	msg domain.ToolCallCompleteEvent,
	stateManager *services.StateManager,
) (tea.Model, tea.Cmd) {
	statusMsg := fmt.Sprintf("Completed %s", msg.ToolName)
	if !msg.Success {
		statusMsg = fmt.Sprintf("Failed %s", msg.ToolName)
	}

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
				StatusType: domain.StatusDefault,
			}
		},
	}

	if chatSession := stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, e.handler.listenForChatEvents(chatSession.EventChannel))
	}

	return nil, tea.Batch(cmds...)
}

func (e *ChatEventHandler) handleToolCallError(
	msg domain.ToolCallErrorEvent,
	stateManager *services.StateManager,
) (tea.Model, tea.Cmd) {
	statusMsg := fmt.Sprintf("Failed %s: %v", msg.ToolName, msg.Error)

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
				StatusType: domain.StatusError,
			}
		},
	}

	if chatSession := stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, e.handler.listenForChatEvents(chatSession.EventChannel))
	}

	return nil, tea.Batch(cmds...)
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
	return nil, func() tea.Msg {
		return domain.UpdateStatusEvent{
			Message: fmt.Sprintf("Tool %d/%d: %s (%s)",
				msg.CurrentTool, msg.TotalTools, msg.ToolName, msg.Status),
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

func (e *ChatEventHandler) FormatMetrics(metrics *domain.ChatMetrics) string {
	if metrics == nil {
		return ""
	}

	var parts []string

	messages := e.handler.conversationRepo.GetMessages()
	if len(messages) > 0 {
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Message.Role == sdk.User {
				actualDuration := time.Since(messages[i].Time).Round(time.Millisecond)
				parts = append(parts, fmt.Sprintf("Time: %v", actualDuration))
				break
			}
		}
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

// formatLiveTokenUsage formats token usage during streaming
func (e *ChatEventHandler) formatLiveTokenUsage(usage *sdk.CompletionUsage) string {
	if usage == nil {
		return ""
	}

	var parts []string
	if usage.PromptTokens > 0 {
		parts = append(parts, fmt.Sprintf("Input: %d tokens", usage.PromptTokens))
	}
	if usage.CompletionTokens > 0 {
		parts = append(parts, fmt.Sprintf("Output: %d tokens", usage.CompletionTokens))
	}
	if usage.TotalTokens > 0 {
		parts = append(parts, fmt.Sprintf("Total: %d tokens", usage.TotalTokens))
	}

	if len(parts) > 0 {
		return strings.Join(parts, " | ")
	}

	return ""
}
