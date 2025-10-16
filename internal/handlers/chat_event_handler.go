package handlers

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	domain "github.com/inference-gateway/cli/internal/domain"
	components "github.com/inference-gateway/cli/internal/ui/components"
	sdk "github.com/inference-gateway/sdk"
)

type ChatEventHandler struct {
	handler          *ChatHandler
	toolCallRenderer *components.ToolCallRenderer
}

func NewChatEventHandler(handler *ChatHandler) *ChatEventHandler {
	return &ChatEventHandler{
		handler:          handler,
		toolCallRenderer: components.NewToolCallRenderer(),
	}
}

func (e *ChatEventHandler) handleChatStart(
	event domain.ChatStartEvent,
) tea.Cmd {
	_ = e.handler.stateManager.UpdateChatStatus(domain.ChatStatusStarting)

	var cmds []tea.Cmd
	cmds = append(cmds, func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    "Starting response...",
			Spinner:    true,
			StatusType: domain.StatusGenerating,
		}
	})

	if chatSession := e.handler.stateManager.GetChatSession(); chatSession != nil {
		cmds = append(cmds, e.handler.listenForChatEvents(chatSession.EventChannel))
	}

	return tea.Batch(cmds...)
}

func (e *ChatEventHandler) handleChatChunk(
	msg domain.ChatChunkEvent,
) tea.Cmd {
	chatSession := e.handler.stateManager.GetChatSession()
	if chatSession == nil {
		return e.handleNoChatSession(msg)
	}

	if msg.Content == "" && msg.ReasoningContent == "" {
		return e.handleEmptyContent(chatSession)
	}

	cmds := []tea.Cmd{
		func() tea.Msg {
			return domain.StreamingContentEvent{
				RequestID: msg.RequestID,
				Content:   msg.Content,
				Delta:     true,
			}
		},
	}

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

	statusCmds := e.handleStatusUpdate(msg, chatSession)
	cmds = append(cmds, statusCmds...)

	if chatSession := e.handler.stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, e.handler.listenForChatEvents(chatSession.EventChannel))
	}

	return tea.Batch(cmds...)
}

func (e *ChatEventHandler) handleOptimizationStatus(
	event domain.OptimizationStatusEvent,
) tea.Cmd {
	if event.IsActive {
		return func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    event.Message,
				Spinner:    true,
				StatusType: domain.StatusProcessing,
			}
		}
	}

	return func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    event.Message,
			Spinner:    false,
			StatusType: domain.StatusDefault,
		}
	}
}

func (e *ChatEventHandler) handleNoChatSession(msg domain.ChatChunkEvent) tea.Cmd {
	if msg.ReasoningContent != "" {
		return func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    "Thinking...",
				Spinner:    true,
				StatusType: domain.StatusThinking,
			}
		}
	}
	return nil
}

func (e *ChatEventHandler) handleEmptyContent(chatSession *domain.ChatSession) tea.Cmd {
	if chatSession != nil && chatSession.EventChannel != nil {
		return e.handler.listenForChatEvents(chatSession.EventChannel)
	}
	return nil
}

func (e *ChatEventHandler) handleStatusUpdate(msg domain.ChatChunkEvent, chatSession *domain.ChatSession) []tea.Cmd {
	newStatus, shouldUpdateStatus := e.determineNewStatus(msg, chatSession.Status, chatSession.IsFirstChunk)

	if !shouldUpdateStatus {
		return nil
	}

	_ = e.handler.stateManager.UpdateChatStatus(newStatus)

	if chatSession.IsFirstChunk {
		chatSession.IsFirstChunk = false
		return e.createFirstChunkStatusCmd(newStatus)
	}

	if newStatus != chatSession.Status {
		return e.createStatusUpdateCmd(newStatus)
	}

	return nil
}

func (e *ChatEventHandler) determineNewStatus(msg domain.ChatChunkEvent, currentStatus domain.ChatStatus, _ bool) (domain.ChatStatus, bool) {
	if msg.ReasoningContent != "" {
		return domain.ChatStatusThinking, true
	}

	if msg.Content != "" {
		return domain.ChatStatusGenerating, true
	}

	return currentStatus, false
}

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

func (e *ChatEventHandler) handleChatComplete(
	msg domain.ChatCompleteEvent,

) tea.Cmd {
	_ = e.handler.stateManager.UpdateChatStatus(domain.ChatStatusCompleted)

	var cmds []tea.Cmd

	cmds = append(cmds, func() tea.Msg {
		return domain.UpdateHistoryEvent{
			History: e.handler.conversationRepo.GetMessages(),
		}
	})

	statusMsg := "Response complete"
	tokenUsage := ""
	if msg.Metrics != nil {
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

	if chatSession := e.handler.stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, e.handler.listenForChatEvents(chatSession.EventChannel))
	}

	return tea.Batch(cmds...)
}

func (e *ChatEventHandler) handleChatError(
	msg domain.ChatErrorEvent,

) tea.Cmd {
	_ = e.handler.stateManager.UpdateChatStatus(domain.ChatStatusError)
	e.handler.stateManager.EndChatSession()
	e.handler.stateManager.EndToolExecution()

	_ = e.handler.stateManager.TransitionToView(domain.ViewStateChat)

	errorMsg := fmt.Sprintf("Chat error: %v", msg.Error)
	if strings.Contains(msg.Error.Error(), "timed out") {
		errorMsg = fmt.Sprintf("⏰ %v\n\nSuggestions:\n• Try breaking your request into smaller parts\n• Check if the server is overloaded\n• Verify your network connection", msg.Error)
	}

	return func() tea.Msg {
		return domain.ShowErrorEvent{
			Error:  errorMsg,
			Sticky: true,
		}
	}
}

func (e *ChatEventHandler) handleToolCallPreview(
	msg domain.ToolCallPreviewEvent,

) tea.Cmd {
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

	return tea.Batch(cmds...)
}

func (e *ChatEventHandler) handleToolCallUpdate(
	msg domain.ToolCallUpdateEvent,

) tea.Cmd {
	var cmds []tea.Cmd

	cmds = append(cmds, func() tea.Msg {
		return domain.UpdateHistoryEvent{
			History: e.handler.conversationRepo.GetMessages(),
		}
	})

	statusMsg := e.formatToolCallStatusMessage(msg.ToolName, msg.Status)

	switch msg.Status {
	case domain.ToolCallStreamStatusStreaming:
		cmds = append(cmds, func() tea.Msg {
			return domain.UpdateStatusEvent{
				Message:    statusMsg,
				StatusType: domain.StatusWorking,
			}
		})
	default:
		cmds = append(cmds, func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    statusMsg,
				Spinner:    false,
				StatusType: domain.StatusWorking,
			}
		})
	}

	return tea.Batch(cmds...)
}

func (e *ChatEventHandler) handleToolCallReady(
	msg domain.ToolCallReadyEvent,

) tea.Cmd {
	cmds := []tea.Cmd{
		func() tea.Msg {
			return domain.UpdateHistoryEvent{
				History: e.handler.conversationRepo.GetMessages(),
			}
		},
	}

	return tea.Batch(cmds...)
}

func (e *ChatEventHandler) handleToolExecutionStarted(
	msg domain.ToolExecutionStartedEvent,

) tea.Cmd {

	return func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    fmt.Sprintf("Starting tool execution (%d tools)", msg.TotalTools),
			Spinner:    true,
			StatusType: domain.StatusWorking,
		}
	}
}

func (e *ChatEventHandler) handleToolExecutionProgress(
	msg domain.ToolExecutionProgressEvent,

) tea.Cmd {
	var cmds []tea.Cmd
	cmds = append(cmds, func() tea.Msg {
		statusEvent := domain.UpdateStatusEvent{
			Message:    msg.Message,
			StatusType: domain.StatusWorking,
		}
		return statusEvent
	})

	if chatSession := e.handler.stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, e.handler.listenForChatEvents(chatSession.EventChannel))
	}

	return tea.Batch(cmds...)
}

func (e *ChatEventHandler) handleToolExecutionCompleted(
	msg domain.ToolExecutionCompletedEvent,

) tea.Cmd {
	return tea.Batch(
		func() tea.Msg {
			return domain.UpdateHistoryEvent{
				History: e.handler.conversationRepo.GetMessages(),
			}
		},
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message: fmt.Sprintf("Tools completed (%d/%d successful) - preparing response...",
					msg.SuccessCount, msg.TotalExecuted),
				Spinner:    true,
				StatusType: domain.StatusPreparing,
			}
		},
		e.handler.startChatCompletion(),
	)
}

func (e *ChatEventHandler) handleParallelToolsStart(
	msg domain.ParallelToolsStartEvent,

) tea.Cmd {
	var cmds []tea.Cmd
	cmds = append(cmds, func() tea.Msg {
		statusEvent := domain.SetStatusEvent{
			Message:    fmt.Sprintf("Executing %d tools in parallel...", len(msg.Tools)),
			Spinner:    true,
			StatusType: domain.StatusWorking,
		}
		return statusEvent
	})

	if chatSession := e.handler.stateManager.GetChatSession(); chatSession != nil {
		cmds = append(cmds, e.handler.listenForChatEvents(chatSession.EventChannel))
	}

	return tea.Batch(cmds...)
}

func (e *ChatEventHandler) handleParallelToolsComplete(
	msg domain.ParallelToolsCompleteEvent,

) tea.Cmd {
	var cmds []tea.Cmd
	cmds = append(cmds, func() tea.Msg {
		historyEvent := domain.UpdateHistoryEvent{
			History: e.handler.conversationRepo.GetMessages(),
		}
		return historyEvent
	})

	cmds = append(cmds, func() tea.Msg {
		statusEvent := domain.SetStatusEvent{
			Message: fmt.Sprintf("Completed %d tools in %v - preparing response...",
				msg.TotalExecuted,
				msg.Duration.Round(time.Millisecond),
			),
			Spinner:    true,
			StatusType: domain.StatusPreparing,
		}
		return statusEvent
	})

	if chatSession := e.handler.stateManager.GetChatSession(); chatSession != nil {
		cmds = append(cmds, e.handler.listenForChatEvents(chatSession.EventChannel))
	}

	return tea.Batch(cmds...)
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
	parts = append(parts, fmt.Sprintf("Session Input: %d tokens", sessionStats.TotalInputTokens))
	parts = append(parts, fmt.Sprintf("Session Output: %d tokens", sessionStats.TotalOutputTokens))

	return strings.Join(parts, " | ")
}

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

func (e *ChatEventHandler) formatToolCallStatusMessage(toolName string, status domain.ToolCallStreamStatus) string {
	switch status {
	case domain.ToolCallStreamStatusStreaming:
		return fmt.Sprintf("Streaming %s...", toolName)
	case domain.ToolCallStreamStatusComplete:
		return fmt.Sprintf("Completed %s", toolName)
	default:
		return ""
	}
}

func (e *ChatEventHandler) handleCancelled(
	msg domain.CancelledEvent,

) tea.Cmd {
	if chatSession := e.handler.stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		return e.handler.listenForChatEvents(chatSession.EventChannel)
	}
	return nil
}

func (e *ChatEventHandler) handleA2AToolCallExecuted(
	msg domain.A2AToolCallExecutedEvent,

) tea.Cmd {
	if chatSession := e.handler.stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		return e.handler.listenForChatEvents(chatSession.EventChannel)
	}
	return nil
}

func (e *ChatEventHandler) handleA2ATaskSubmitted(
	msg domain.A2ATaskSubmittedEvent,

) tea.Cmd {
	if chatSession := e.handler.stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		return e.handler.listenForChatEvents(chatSession.EventChannel)
	}
	return nil
}

func (e *ChatEventHandler) handleA2ATaskStatusUpdate(
	msg domain.A2ATaskStatusUpdateEvent,

) tea.Cmd {
	if chatSession := e.handler.stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		return e.handler.listenForChatEvents(chatSession.EventChannel)
	}
	return nil
}

func (e *ChatEventHandler) handleA2ATaskCompleted(
	msg domain.A2ATaskCompletedEvent,

) tea.Cmd {
	if chatSession := e.handler.stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		return e.handler.listenForChatEvents(chatSession.EventChannel)
	}
	return nil
}

func (e *ChatEventHandler) handleA2ATaskInputRequired(
	msg domain.A2ATaskInputRequiredEvent,

) tea.Cmd {
	if chatSession := e.handler.stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		return e.handler.listenForChatEvents(chatSession.EventChannel)
	}
	return nil
}
