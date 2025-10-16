package handlers

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
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
	var cmds []tea.Cmd

	if event.IsActive {
		cmds = append(cmds, func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    event.Message,
				Spinner:    true,
				StatusType: domain.StatusProcessing,
			}
		})
	} else {
		cmds = append(cmds, func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    event.Message,
				Spinner:    false,
				StatusType: domain.StatusDefault,
			}
		})
	}

	if chatSession := e.handler.stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, e.handler.listenForChatEvents(chatSession.EventChannel))
	}

	return tea.Batch(cmds...)
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

	backgroundTasks := e.handler.stateManager.GetBackgroundTasks(e.handler.toolService)
	hasBackgroundTasks := len(backgroundTasks) > 0

	if hasBackgroundTasks {
		statusMsg = fmt.Sprintf("Response complete - %d background task(s) running", len(backgroundTasks))
	}

	queuedMessages := e.handler.stateManager.GetQueuedMessages()
	logger.Info("Chat completed, checking queue",
		"queueSize", len(queuedMessages),
		"hasBackgroundTasks", hasBackgroundTasks)

	var queuedMsg *domain.QueuedMessage

	if len(queuedMessages) > 0 {
		queuedMsg = e.handler.stateManager.PopQueuedMessage()
		remainingInQueue := len(e.handler.stateManager.GetQueuedMessages())

		logger.Info("Popped queued message for processing",
			"requestID", queuedMsg.RequestID,
			"content", queuedMsg.Message.Content,
			"remainingInQueue", remainingInQueue)
	}

	if queuedMsg != nil && hasBackgroundTasks {
		statusMsg = fmt.Sprintf("Processing queued message - %d background task(s) running", len(backgroundTasks))
	} else if queuedMsg != nil {
		statusMsg = "Processing queued message..."
	}

	cmds = append(cmds, func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    statusMsg,
			Spinner:    hasBackgroundTasks || queuedMsg != nil,
			TokenUsage: tokenUsage,
			StatusType: domain.StatusDefault,
		}
	})

	if queuedMsg != nil {
		queuedMsgCopy := *queuedMsg

		cmds = append(cmds, func() tea.Msg {
			return domain.MessageQueuedEvent{
				RequestID: queuedMsgCopy.RequestID,
				Timestamp: time.Now(),
				Message:   queuedMsgCopy.Message,
			}
		})
	} else {
		chatSession := e.handler.stateManager.GetChatSession()
		if chatSession != nil && chatSession.EventChannel != nil {
			cmds = append(cmds, e.handler.listenForChatEvents(chatSession.EventChannel))
		} else if !hasBackgroundTasks {
			e.handler.stateManager.EndChatSession()
		}
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

	if chatSession := e.handler.stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, e.handler.listenForChatEvents(chatSession.EventChannel))
	}

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

	if chatSession := e.handler.stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, e.handler.listenForChatEvents(chatSession.EventChannel))
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

	if chatSession := e.handler.stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, e.handler.listenForChatEvents(chatSession.EventChannel))
	}

	return tea.Batch(cmds...)
}

func (e *ChatEventHandler) handleToolExecutionStarted(
	msg domain.ToolExecutionStartedEvent,
) tea.Cmd {
	var cmds []tea.Cmd

	cmds = append(cmds, func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    fmt.Sprintf("Starting tool execution (%d tools)", msg.TotalTools),
			Spinner:    true,
			StatusType: domain.StatusWorking,
		}
	})

	if chatSession := e.handler.stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, e.handler.listenForChatEvents(chatSession.EventChannel))
	}

	return tea.Batch(cmds...)
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

func (e *ChatEventHandler) handleA2ATaskCompleted(
	msg domain.A2ATaskCompletedEvent,
) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	backgroundTasks := e.handler.stateManager.GetBackgroundTasks(e.handler.toolService)
	hasBackgroundTasks := len(backgroundTasks) > 0

	queuedMessages := e.handler.stateManager.GetQueuedMessages()
	hasQueuedMessages := len(queuedMessages) > 0

	chatSession := e.handler.stateManager.GetChatSession()
	hasActiveSession := chatSession != nil

	logger.Info("A2A task completed (success) handler called",
		"requestID", msg.RequestID,
		"taskID", msg.TaskID,
		"hasBackgroundTasks", hasBackgroundTasks,
		"queueSize", len(queuedMessages),
		"hasActiveSession", hasActiveSession)

	statusMessage := "A2A task completed"
	if hasBackgroundTasks {
		statusMessage = fmt.Sprintf("A2A task completed - %d background task(s) remaining", len(backgroundTasks))
	}

	if !hasBackgroundTasks && hasActiveSession {
		logger.Info("Last background task completed - ending chat session")
		e.handler.stateManager.EndChatSession()
	}

	if !hasBackgroundTasks && !hasQueuedMessages {
		logger.Info("No background tasks or queued messages - triggering chat completion for A2A results")

		cmds = append(cmds, func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    "Processing A2A task results...",
				Spinner:    true,
				StatusType: domain.StatusPreparing,
			}
		})

		cmds = append(cmds, e.handler.startChatCompletion())
	} else {
		cmds = append(cmds, func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    statusMessage,
				Spinner:    hasBackgroundTasks,
				StatusType: domain.StatusDefault,
			}
		})

		if chatSession := e.handler.stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
			cmds = append(cmds, e.handler.listenForChatEvents(chatSession.EventChannel))
		}
	}

	return nil, tea.Batch(cmds...)
}

func (e *ChatEventHandler) handleA2ATaskFailed(
	msg domain.A2ATaskFailedEvent,
) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	backgroundTasks := e.handler.stateManager.GetBackgroundTasks(e.handler.toolService)
	hasBackgroundTasks := len(backgroundTasks) > 0

	queuedMessages := e.handler.stateManager.GetQueuedMessages()
	hasQueuedMessages := len(queuedMessages) > 0

	chatSession := e.handler.stateManager.GetChatSession()
	hasActiveSession := chatSession != nil

	logger.Info("A2A task failed handler called",
		"requestID", msg.RequestID,
		"taskID", msg.TaskID,
		"error", msg.Error,
		"hasBackgroundTasks", hasBackgroundTasks,
		"queueSize", len(queuedMessages),
		"hasActiveSession", hasActiveSession)

	statusMessage := fmt.Sprintf("A2A task failed: %s", msg.Error)
	if hasBackgroundTasks {
		statusMessage = fmt.Sprintf("A2A task failed - %d background task(s) remaining", len(backgroundTasks))
	}

	if !hasBackgroundTasks && hasActiveSession {
		logger.Info("Last background task failed - ending chat session")
		e.handler.stateManager.EndChatSession()
	}

	if !hasBackgroundTasks && !hasQueuedMessages {
		logger.Info("A2A task failed - triggering chat completion for agent to reason about failure")

		cmds = append(cmds, func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    "Processing A2A task failure...",
				Spinner:    true,
				StatusType: domain.StatusPreparing,
			}
		})

		cmds = append(cmds, e.handler.startChatCompletion())
	} else {
		cmds = append(cmds, func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    statusMessage,
				Spinner:    hasBackgroundTasks,
				StatusType: domain.StatusDefault,
			}
		})

		if chatSession := e.handler.stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
			cmds = append(cmds, e.handler.listenForChatEvents(chatSession.EventChannel))
		}
	}

	return nil, tea.Batch(cmds...)
}

func (e *ChatEventHandler) handleA2ATaskStatusUpdate(
	msg domain.A2ATaskStatusUpdateEvent,
) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	statusMessage := fmt.Sprintf("A2A task %s: %s", msg.Status, msg.Message)
	cmds = append(cmds, func() tea.Msg {
		return domain.UpdateStatusEvent{
			Message:    statusMessage,
			StatusType: domain.StatusWorking,
		}
	})

	if chatSession := e.handler.stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, e.handler.listenForChatEvents(chatSession.EventChannel))
	}

	return nil, tea.Batch(cmds...)
}

func (e *ChatEventHandler) handleMessageQueued(
	msg domain.MessageQueuedEvent,
) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	queueSize := len(e.handler.stateManager.GetQueuedMessages())
	logger.Info("Processing queued message",
		"requestID", msg.RequestID,
		"content", msg.Message.Content,
		"remainingInQueue", queueSize)

	userEntry := domain.ConversationEntry{
		Message: msg.Message,
		Time:    time.Now(),
	}

	if err := e.handler.conversationRepo.AddMessage(userEntry); err != nil {
		logger.Error("Failed to add queued message to conversation",
			"error", err,
			"requestID", msg.RequestID)
		cmds = append(cmds, func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Failed to save queued message: %v", err),
				Sticky: false,
			}
		})
		return nil, tea.Batch(cmds...)
	}

	logger.Info("Queued message added to conversation, starting chat completion",
		"requestID", msg.RequestID,
		"totalMessages", len(e.handler.conversationRepo.GetMessages()))

	cmds = append(cmds, func() tea.Msg {
		return domain.UpdateHistoryEvent{
			History: e.handler.conversationRepo.GetMessages(),
		}
	})

	cmds = append(cmds, func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    "Processing queued message...",
			Spinner:    true,
			StatusType: domain.StatusProcessing,
		}
	})

	cmds = append(cmds, e.handler.startChatCompletion())

	return nil, tea.Batch(cmds...)
}

func (e *ChatEventHandler) handleA2ATaskInputRequired(
	msg domain.A2ATaskInputRequiredEvent,
	stateManager domain.StateManager,
) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	statusMessage := fmt.Sprintf("⚠️  A2A task requires input: %s", msg.Message)
	cmds = append(cmds, func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    statusMessage,
			Spinner:    false,
			StatusType: domain.StatusDefault,
		}
	})

	if chatSession := stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, e.handler.listenForChatEvents(chatSession.EventChannel))
	}

	return nil, tea.Batch(cmds...)
}

func (e *ChatEventHandler) handleCancelled(
	msg domain.CancelledEvent,
) tea.Cmd {
	_ = e.handler.stateManager.UpdateChatStatus(domain.ChatStatusCancelled)
	e.handler.stateManager.EndChatSession()
	e.handler.stateManager.EndToolExecution()

	return func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    fmt.Sprintf("Request cancelled: %s", msg.Reason),
			Spinner:    false,
			StatusType: domain.StatusDefault,
		}
	}
}

func (e *ChatEventHandler) handleA2AToolCallExecuted(
	msg domain.A2AToolCallExecutedEvent,
) tea.Cmd {
	var cmds []tea.Cmd

	statusMessage := fmt.Sprintf("A2A tool %s executed on gateway", msg.ToolName)
	cmds = append(cmds, func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    statusMessage,
			Spinner:    true,
			StatusType: domain.StatusWorking,
		}
	})

	if chatSession := e.handler.stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, e.handler.listenForChatEvents(chatSession.EventChannel))
	}

	return tea.Batch(cmds...)
}

func (e *ChatEventHandler) handleA2ATaskSubmitted(
	msg domain.A2ATaskSubmittedEvent,
) tea.Cmd {
	var cmds []tea.Cmd

	statusMessage := fmt.Sprintf("A2A task submitted to %s", msg.AgentName)
	cmds = append(cmds, func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    statusMessage,
			Spinner:    true,
			StatusType: domain.StatusWorking,
		}
	})

	if chatSession := e.handler.stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, e.handler.listenForChatEvents(chatSession.EventChannel))
	}

	return tea.Batch(cmds...)
}
