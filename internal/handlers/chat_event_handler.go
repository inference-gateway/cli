package handlers

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	services "github.com/inference-gateway/cli/internal/services"
	tools "github.com/inference-gateway/cli/internal/services/tools"
	sdk "github.com/inference-gateway/sdk"
)

type ChatEventHandler struct {
	handler          *ChatHandler
	activeToolCallID string
}

func NewChatEventHandler(handler *ChatHandler) *ChatEventHandler {
	return &ChatEventHandler{
		handler:          handler,
		activeToolCallID: "",
	}
}

func (e *ChatEventHandler) handleChatStart(
	_ /* event */ domain.ChatStartEvent,
) tea.Cmd {
	_ = e.handler.stateManager.UpdateChatStatus(domain.ChatStatusStarting)
	e.activeToolCallID = ""

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
				Model:     chatSession.Model,
			}
		},
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
	e.restorePendingModel()

	if len(msg.ToolCalls) == 0 {
		_ = e.handler.stateManager.UpdateChatStatus(domain.ChatStatusCompleted)
	}

	var cmds []tea.Cmd

	cmds = append(cmds, func() tea.Msg {
		return domain.UpdateHistoryEvent{
			History: e.handler.conversationRepo.GetMessages(),
		}
	})

	cmds = append(cmds, func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    "Response complete",
			Spinner:    false,
			StatusType: domain.StatusDefault,
		}
	})

	chatSession := e.handler.stateManager.GetChatSession()
	if chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, e.handler.listenForChatEvents(chatSession.EventChannel))
	}

	return tea.Batch(cmds...)
}

// restorePendingModel restores the original model if a temporary model switch is pending
func (e *ChatEventHandler) restorePendingModel() {
	if e.handler.pendingModelRestoration == "" {
		return
	}

	originalModel := e.handler.pendingModelRestoration
	e.handler.pendingModelRestoration = ""

	if err := e.handler.modelService.SelectModel(originalModel); err != nil {
		logger.Error("Failed to restore original model", "model", originalModel, "error", err)
		e.addModelRestorationWarning(originalModel)
		return
	}

	logger.Debug("Successfully restored original model", "model", originalModel)
}

// addModelRestorationWarning adds a warning message when model restoration fails
func (e *ChatEventHandler) addModelRestorationWarning(originalModel string) {
	warningEntry := domain.ConversationEntry{
		Message: sdk.Message{
			Role:    sdk.Assistant,
			Content: sdk.NewMessageContent(fmt.Sprintf("[Warning: Failed to restore model to %s]", originalModel)),
		},
		Time: time.Now(),
	}

	if err := e.handler.conversationRepo.AddMessage(warningEntry); err != nil {
		logger.Error("Failed to add model restoration warning message", "error", err)
	}
}

func (e *ChatEventHandler) handleChatError(
	msg domain.ChatErrorEvent,
) tea.Cmd {
	_ = e.handler.stateManager.UpdateChatStatus(domain.ChatStatusError)
	e.handler.stateManager.EndChatSession()
	e.handler.stateManager.EndToolExecution()
	e.activeToolCallID = ""

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
				ToolName:   msg.ToolName,
			}
		})
	default:
		cmds = append(cmds, func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    statusMsg,
				Spinner:    false,
				StatusType: domain.StatusWorking,
				ToolName:   msg.ToolName,
			}
		})
	}

	if chatSession := e.handler.stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, e.handler.listenForChatEvents(chatSession.EventChannel))
	}

	return tea.Batch(cmds...)
}

func (e *ChatEventHandler) handleToolCallReady(
	_ /* msg */ domain.ToolCallReadyEvent,

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

func (e *ChatEventHandler) handleToolApprovalRequested(
	msg domain.ToolApprovalRequestedEvent,
) tea.Cmd {
	if inMemRepo, ok := e.handler.conversationRepo.(*services.InMemoryConversationRepository); ok {
		if err := inMemRepo.AddPendingToolCall(msg.ToolCall, msg.ResponseChan); err != nil {
			logger.Error("Failed to add pending tool call", "error", err)
		}
	} else if persistentRepo, ok := e.handler.conversationRepo.(*services.PersistentConversationRepository); ok {
		if err := persistentRepo.AddPendingToolCall(msg.ToolCall, msg.ResponseChan); err != nil {
			logger.Error("Failed to add pending tool call", "error", err)
		}
	}

	e.handler.stateManager.SetupApprovalUIState(&msg.ToolCall, msg.ResponseChan)

	var cmds []tea.Cmd

	cmds = append(cmds, func() tea.Msg {
		return domain.UpdateHistoryEvent{
			History: e.handler.conversationRepo.GetMessages(),
		}
	})

	cmds = append(cmds, func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    fmt.Sprintf("Tool approval required: %s", msg.ToolCall.Function.Name),
			Spinner:    false,
			StatusType: domain.StatusDefault,
		}
	})

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

	switch msg.Status {
	case "starting":
		e.activeToolCallID = msg.ToolCallID
		cmds = append(cmds, func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    msg.Message,
				Spinner:    true,
				StatusType: domain.StatusWorking,
				ToolName:   msg.ToolName,
			}
		})
	case "running":
		if e.activeToolCallID == msg.ToolCallID {
			cmds = append(cmds, func() tea.Msg {
				return domain.UpdateStatusEvent{
					Message:    msg.Message,
					StatusType: domain.StatusWorking,
					ToolName:   msg.ToolName,
				}
			})
		} else {
			e.activeToolCallID = msg.ToolCallID
			cmds = append(cmds, func() tea.Msg {
				return domain.SetStatusEvent{
					Message:    msg.Message,
					Spinner:    true,
					StatusType: domain.StatusWorking,
					ToolName:   msg.ToolName,
				}
			})
		}
	case "complete", "failed":
		e.activeToolCallID = ""
		cmds = append(cmds, func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    msg.Message,
				Spinner:    false,
				StatusType: domain.StatusDefault,
				ToolName:   "",
			}
		})
	case "saving":
		cmds = append(cmds, func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    msg.Message,
				Spinner:    true,
				StatusType: domain.StatusDefault,
				ToolName:   "",
			}
		})
	}

	e.handler.commandHandler.toolEventChannelMu.RLock()
	toolEventChan := e.handler.commandHandler.toolEventChannel
	e.handler.commandHandler.toolEventChannelMu.RUnlock()

	if toolEventChan != nil {
		cmds = append(cmds, e.handler.commandHandler.listenToToolEvents(toolEventChan))
		return tea.Batch(cmds...)
	}

	e.handler.commandHandler.bashEventChannelMu.RLock()
	bashEventChan := e.handler.commandHandler.bashEventChannel
	e.handler.commandHandler.bashEventChannelMu.RUnlock()

	if bashEventChan != nil {
		cmds = append(cmds, e.handler.commandHandler.listenToBashEvents(bashEventChan))
		return tea.Batch(cmds...)
	}

	if chatSession := e.handler.stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, e.handler.listenForChatEvents(chatSession.EventChannel))
	}

	if len(cmds) > 0 {
		return tea.Batch(cmds...)
	}
	return nil
}

func (e *ChatEventHandler) handleBashOutputChunk(
	_ domain.BashOutputChunkEvent,
) tea.Cmd {
	e.handler.commandHandler.bashEventChannelMu.RLock()
	bashEventChan := e.handler.commandHandler.bashEventChannel
	e.handler.commandHandler.bashEventChannelMu.RUnlock()

	if bashEventChan != nil {
		return e.handler.commandHandler.listenToBashEvents(bashEventChan)
	}

	if chatSession := e.handler.stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		return e.handler.listenForChatEvents(chatSession.EventChannel)
	}

	return nil
}

func (e *ChatEventHandler) handleToolExecutionCompleted(
	msg domain.ToolExecutionCompletedEvent,

) tea.Cmd {
	e.activeToolCallID = ""

	cmds := []tea.Cmd{
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
	}

	todoUpdateCmd := e.extractTodoUpdateCmd(msg.Results)
	if todoUpdateCmd != nil {
		cmds = append(cmds, todoUpdateCmd)
	}

	return tea.Batch(cmds...)
}

// extractTodoUpdateCmd checks tool results for TodoWrite and returns a command to update todos
func (e *ChatEventHandler) extractTodoUpdateCmd(results []*domain.ToolExecutionResult) tea.Cmd {
	for _, result := range results {
		if result == nil || result.ToolName != "TodoWrite" || !result.Success {
			continue
		}

		todoResult, ok := result.Data.(*domain.TodoWriteToolResult)
		if !ok || todoResult == nil {
			continue
		}

		todos := todoResult.Todos
		return func() tea.Msg {
			return domain.TodoUpdateEvent{
				Todos: todos,
			}
		}
	}
	return nil
}

func (e *ChatEventHandler) handleParallelToolsStart(
	_ domain.ParallelToolsStartEvent,

) tea.Cmd {
	e.handler.commandHandler.bashEventChannelMu.RLock()
	bashEventChan := e.handler.commandHandler.bashEventChannel
	e.handler.commandHandler.bashEventChannelMu.RUnlock()

	if bashEventChan != nil {
		return e.handler.commandHandler.listenToBashEvents(bashEventChan)
	}

	if chatSession := e.handler.stateManager.GetChatSession(); chatSession != nil {
		return e.handler.listenForChatEvents(chatSession.EventChannel)
	}

	return nil
}

func (e *ChatEventHandler) handleParallelToolsComplete(
	_ domain.ParallelToolsCompleteEvent,

) tea.Cmd {
	var cmds []tea.Cmd

	cmds = append(cmds, func() tea.Msg {
		return domain.UpdateHistoryEvent{
			History: e.handler.conversationRepo.GetMessages(),
		}
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

	return strings.Join(parts, " | ")
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

	var taskResult string
	if msg.Result.Data != nil {
		if submitResult, ok := msg.Result.Data.(tools.A2ASubmitTaskResult); ok {
			taskResult = submitResult.TaskResult

			if submitResult.Task != nil && e.handler.taskRetentionService != nil {
				retainedTask := domain.TaskInfo{
					Task:        *submitResult.Task,
					AgentURL:    submitResult.AgentURL,
					StartedAt:   time.Now().Add(-msg.Result.Duration),
					CompletedAt: time.Now(),
				}
				e.handler.taskRetentionService.AddTask(retainedTask)
			}
		}
	}

	if taskResult == "" {
		taskResult = e.handler.conversationRepo.FormatToolResultForLLM(&msg.Result)
	}

	cmds = append(cmds, func() tea.Msg {
		return domain.StreamingContentEvent{
			RequestID: msg.RequestID,
			Content:   taskResult,
			Delta:     false,
		}
	})

	cmds = append(cmds, func() tea.Msg {
		return domain.UpdateHistoryEvent{
			History: e.handler.conversationRepo.GetMessages(),
		}
	})

	chatSession := e.handler.stateManager.GetChatSession()

	cmds = append(cmds, func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    "A2A task completed",
			Spinner:    false,
			StatusType: domain.StatusDefault,
		}
	})

	if chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, e.handler.listenForChatEvents(chatSession.EventChannel))
	}

	return nil, tea.Batch(cmds...)
}

func (e *ChatEventHandler) handleA2ATaskFailed(
	msg domain.A2ATaskFailedEvent,
) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	var taskResult string
	if msg.Result.Data != nil {
		if submitResult, ok := msg.Result.Data.(tools.A2ASubmitTaskResult); ok {
			taskResult = submitResult.TaskResult

			if submitResult.Task != nil && e.handler.taskRetentionService != nil {
				retainedTask := domain.TaskInfo{
					Task:        *submitResult.Task,
					AgentURL:    submitResult.AgentURL,
					StartedAt:   time.Now().Add(-msg.Result.Duration),
					CompletedAt: time.Now(),
				}
				e.handler.taskRetentionService.AddTask(retainedTask)
			}
		}
	}

	var errorContent string
	if taskResult != "" {
		errorContent = fmt.Sprintf("[A2A Task Failed]\n\n%s", taskResult)
	} else {
		formattedResult := e.handler.conversationRepo.FormatToolResultForLLM(&msg.Result)
		errorContent = fmt.Sprintf("[A2A Task Failed]\n\nError: %s\n\n%s", msg.Error, formattedResult)
	}

	cmds = append(cmds, func() tea.Msg {
		return domain.StreamingContentEvent{
			RequestID: msg.RequestID,
			Content:   errorContent,
			Delta:     false,
		}
	})

	cmds = append(cmds, func() tea.Msg {
		return domain.UpdateHistoryEvent{
			History: e.handler.conversationRepo.GetMessages(),
		}
	})

	chatSession := e.handler.stateManager.GetChatSession()

	cmds = append(cmds, func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    fmt.Sprintf("A2A task failed: %s", msg.Error),
			Spinner:    false,
			StatusType: domain.StatusDefault,
		}
	})

	if chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, e.handler.listenForChatEvents(chatSession.EventChannel))
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
	_ domain.MessageQueuedEvent,
) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

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

	chatSession := e.handler.stateManager.GetChatSession()
	if chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, e.handler.listenForChatEvents(chatSession.EventChannel))
	}

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
	e.activeToolCallID = ""

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
