package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/inference-gateway/cli/internal/commands"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/services"
	"github.com/inference-gateway/cli/internal/ui/shared"
	sdk "github.com/inference-gateway/sdk"
)

// ChatHandler handles chat-related messages using the new state management system
type ChatHandler struct {
	name             string
	chatService      domain.ChatService
	conversationRepo domain.ConversationRepository
	modelService     domain.ModelService
	configService    domain.ConfigService
	toolService      domain.ToolService
	fileService      domain.FileService
	commandRegistry  *commands.Registry
	toolOrchestrator *services.ToolExecutionOrchestrator
	debugService     *services.DebugService
}

// NewChatHandler creates a new chat handler
func NewChatHandler(
	chatService domain.ChatService,
	conversationRepo domain.ConversationRepository,
	modelService domain.ModelService,
	configService domain.ConfigService,
	toolService domain.ToolService,
	fileService domain.FileService,
	commandRegistry *commands.Registry,
	toolOrchestrator *services.ToolExecutionOrchestrator,
	debugService *services.DebugService,
) *ChatHandler {
	return &ChatHandler{
		name:             "ChatHandler",
		chatService:      chatService,
		conversationRepo: conversationRepo,
		modelService:     modelService,
		configService:    configService,
		toolService:      toolService,
		fileService:      fileService,
		commandRegistry:  commandRegistry,
		toolOrchestrator: toolOrchestrator,
		debugService:     debugService,
	}
}

// GetName returns the handler name
func (h *ChatHandler) GetName() string {
	return h.name
}

// GetPriority returns the handler priority
func (h *ChatHandler) GetPriority() int {
	return 100 // High priority for chat messages
}

// CanHandle determines if this handler can process the message
func (h *ChatHandler) CanHandle(msg tea.Msg) bool {
	switch msg.(type) {
	case shared.UserInputMsg:
		return true
	case shared.FileSelectionRequestMsg:
		return true
	case domain.ChatStartEvent, domain.ChatChunkEvent, domain.ChatCompleteEvent, domain.ChatErrorEvent:
		return true
	case domain.ToolCallStartEvent, domain.ToolCallEvent:
		return true
	case services.ToolExecutionStartedMsg, services.ToolExecutionProgressMsg, services.ToolExecutionCompletedMsg:
		return true
	case services.ToolApprovalRequestMsg, services.ToolApprovalResponseMsg:
		return true
	default:
		return false
	}
}

// Handle processes the message using the state manager
func (h *ChatHandler) Handle(
	msg tea.Msg,
	stateManager *services.StateManager,
	debugService *services.DebugService,
) (tea.Model, tea.Cmd) {

	switch msg := msg.(type) {
	case shared.UserInputMsg:
		return h.handleUserInput(msg, stateManager, debugService)

	case shared.FileSelectionRequestMsg:
		return h.handleFileSelectionRequest(msg, stateManager, debugService)

	case domain.ChatStartEvent:
		return h.handleChatStart(msg, stateManager, debugService)

	case domain.ChatChunkEvent:
		return h.handleChatChunk(msg, stateManager, debugService)

	case domain.ToolCallStartEvent:
		return h.handleToolCallStart(msg, stateManager, debugService)

	case domain.ToolCallEvent:
		return h.handleToolCall(msg, stateManager, debugService)

	case domain.ChatCompleteEvent:
		return h.handleChatComplete(msg, stateManager, debugService)

	case domain.ChatErrorEvent:
		return h.handleChatError(msg, stateManager, debugService)

	case services.ToolExecutionStartedMsg:
		return h.handleToolExecutionStarted(msg, stateManager, debugService)

	case services.ToolExecutionProgressMsg:
		return h.handleToolExecutionProgress(msg, stateManager, debugService)

	case services.ToolExecutionCompletedMsg:
		return h.handleToolExecutionCompleted(msg, stateManager, debugService)

	case services.ToolApprovalRequestMsg:
		return h.handleToolApprovalRequest(msg, stateManager, debugService)

	case services.ToolApprovalResponseMsg:
		return h.handleToolApprovalResponse(msg, stateManager, debugService)
	}

	return nil, nil
}

// handleUserInput processes user input messages
func (h *ChatHandler) handleUserInput(
	msg shared.UserInputMsg,
	stateManager *services.StateManager,
	debugService *services.DebugService,
) (tea.Model, tea.Cmd) {

	if debugService != nil {
		debugService.LogEvent(
			services.DebugEventTypeMessage,
			h.name,
			"Processing user input",
			map[string]any{
				"input_length": len(msg.Content),
				"has_prefix":   strings.HasPrefix(msg.Content, "/") || strings.HasPrefix(msg.Content, "!"),
			},
		)
	}

	if strings.HasPrefix(msg.Content, "/") {
		return h.handleCommand(msg.Content, stateManager, debugService)
	}

	if strings.HasPrefix(msg.Content, "!") {
		return h.handleBashCommand(msg.Content, stateManager, debugService)
	}

	// Expand @filename references before processing
	expandedContent, err := h.expandFileReferences(msg.Content, debugService)
	if err != nil {
		if debugService != nil {
			debugService.LogError(err, h.name, map[string]any{
				"operation": "expand_file_references",
			})
		}
		return nil, func() tea.Msg {
			return shared.ShowErrorMsg{
				Error:  fmt.Sprintf("Failed to expand file references: %v", err),
				Sticky: false,
			}
		}
	}

	return h.processChatMessage(expandedContent, stateManager, debugService)
}

// extractMarkdownSummary extracts the "## Summary" section from markdown content
func (h *ChatHandler) extractMarkdownSummary(content string) (string, bool) {
	lines := strings.Split(content, "\n")
	var summaryLines []string
	inSummary := false

	for _, line := range lines {
		if strings.TrimSpace(line) == "## Summary" {
			inSummary = true
			summaryLines = append(summaryLines, line)
			continue
		}

		if inSummary {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "## ") || trimmed == "---" {
				break
			}
			summaryLines = append(summaryLines, line)
		}
	}

	if len(summaryLines) > 1 {
		result := strings.Join(summaryLines, "\n")
		result = strings.TrimRight(result, " \t\n") + "\n"
		return result, true
	}

	return "", false
}

// expandFileReferences expands @filename references with file content
func (h *ChatHandler) expandFileReferences(content string, debugService *services.DebugService) (string, error) {
	re := regexp.MustCompile(`@([^\s]+)`)
	matches := re.FindAllStringSubmatch(content, -1)

	if len(matches) == 0 {
		return content, nil
	}

	expandedContent := content
	for _, match := range matches {
		fullMatch := match[0]
		filename := match[1]

		if err := h.fileService.ValidateFile(filename); err != nil {
			if debugService != nil && debugService.IsEnabled() {
				debugService.LogEvent(
					services.DebugEventTypeError,
					"ChatHandler",
					"Skipping invalid file reference",
					map[string]any{
						"filename": filename,
						"error":    err.Error(),
					},
				)
			}
			continue
		}

		fileContent, err := h.fileService.ReadFile(filename)
		if err != nil {
			if debugService != nil && debugService.IsEnabled() {
				debugService.LogEvent(
					services.DebugEventTypeError,
					"ChatHandler",
					"Failed to read file for expansion",
					map[string]any{
						"filename": filename,
						"error":    err.Error(),
					},
				)
			}
			continue
		}

		contentToInclude := fileContent
		if strings.HasSuffix(strings.ToLower(filename), ".md") {
			if summaryContent, hasSummary := h.extractMarkdownSummary(fileContent); hasSummary {
				contentToInclude = summaryContent
				if debugService != nil && debugService.IsEnabled() {
					debugService.LogEvent(
						services.DebugEventTypeMessage,
						"ChatHandler",
						"Extracted markdown summary section",
						map[string]any{
							"filename":      filename,
							"summary_size":  len(summaryContent),
							"original_size": len(fileContent),
						},
					)
				}
			}
		}

		fileBlock := fmt.Sprintf("File: %s\n```%s\n%s\n```\n", filename, filename, contentToInclude)
		expandedContent = strings.Replace(expandedContent, fullMatch, fileBlock, 1)

		if debugService != nil && debugService.IsEnabled() {
			debugService.LogEvent(
				services.DebugEventTypeMessage,
				"ChatHandler",
				"Expanded file reference",
				map[string]any{
					"filename":     filename,
					"content_size": len(fileContent),
				},
			)
		}
	}

	return expandedContent, nil
}

// processChatMessage processes a regular chat message
func (h *ChatHandler) processChatMessage(
	content string,
	stateManager *services.StateManager,
	debugService *services.DebugService,
) (tea.Model, tea.Cmd) {
	userEntry := domain.ConversationEntry{
		Message: sdk.Message{
			Role:    sdk.User,
			Content: content,
		},
		Time: time.Now(),
	}

	if err := h.conversationRepo.AddMessage(userEntry); err != nil {
		if debugService != nil {
			debugService.LogError(err, h.name, map[string]any{
				"operation": "add_user_message",
			})
		}
		return nil, func() tea.Msg {
			return shared.ShowErrorMsg{
				Error:  fmt.Sprintf("Failed to save message: %v", err),
				Sticky: false,
			}
		}
	}

	return nil, tea.Batch(
		func() tea.Msg {
			return shared.UpdateHistoryMsg{
				History: h.conversationRepo.GetMessages(),
			}
		},
		h.startChatCompletion(stateManager, debugService),
	)
}

// startChatCompletion initiates a chat completion request
func (h *ChatHandler) startChatCompletion(
	stateManager *services.StateManager,
	debugService *services.DebugService,
) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		currentModel := h.modelService.GetCurrentModel()
		if currentModel == "" {
			return domain.ChatErrorEvent{
				RequestID: "unknown",
				Timestamp: time.Now(),
				Error:     fmt.Errorf("no model selected"),
			}
		}

		entries := h.conversationRepo.GetMessages()
		messages := make([]sdk.Message, len(entries))
		for i, entry := range entries {
			messages[i] = entry.Message
		}

		requestID := generateRequestID()

		eventChan, err := h.chatService.SendMessage(ctx, requestID, currentModel, messages)
		if err != nil {
			if debugService != nil {
				debugService.LogError(err, h.name, map[string]any{
					"operation": "start_chat_completion",
					"model":     currentModel,
				})
			}
			return domain.ChatErrorEvent{
				RequestID: requestID,
				Timestamp: time.Now(),
				Error:     err,
			}
		}

		_ = stateManager.StartChatSession(requestID, currentModel, eventChan)

		if debugService != nil {
			debugService.LogEvent(
				services.DebugEventTypeSDKEvent,
				h.name,
				"Chat completion started",
				map[string]any{
					"request_id": requestID,
					"model":      currentModel,
				},
			)
		}

		return h.listenForChatEvents(eventChan)()
	}
}

// listenForChatEvents listens for chat events from the SDK
func (h *ChatHandler) listenForChatEvents(eventChan <-chan domain.ChatEvent) tea.Cmd {
	return func() tea.Msg {
		if event, ok := <-eventChan; ok {
			return event
		}
		return nil
	}
}

// handleChatStart processes chat start events
func (h *ChatHandler) handleChatStart(
	msg domain.ChatStartEvent,
	stateManager *services.StateManager,
	debugService *services.DebugService,
) (tea.Model, tea.Cmd) {
	_ = stateManager.UpdateChatStatus(domain.ChatStatusStarting)

	if debugService != nil {
		debugService.LogSDKEvent("ChatStart", msg.RequestID, msg)
	}

	var cmds []tea.Cmd
	cmds = append(cmds, func() tea.Msg {
		return shared.SetStatusMsg{
			Message:    "Starting response...",
			Spinner:    true,
			StatusType: shared.StatusGenerating,
		}
	})

	if chatSession := stateManager.GetChatSession(); chatSession != nil {
		cmds = append(cmds, h.listenForChatEvents(chatSession.EventChannel))
	}

	return nil, tea.Batch(cmds...)
}

// handleChatChunk processes chat chunk events
func (h *ChatHandler) handleChatChunk(
	msg domain.ChatChunkEvent,
	stateManager *services.StateManager,
	debugService *services.DebugService,
) (tea.Model, tea.Cmd) {
	chatSession := stateManager.GetChatSession()
	if chatSession == nil {
		return h.handleNoChatSession(msg)
	}

	if msg.Content == "" {
		return h.handleEmptyContent(chatSession)
	}

	h.updateConversationHistory(msg, chatSession, debugService)

	cmds := []tea.Cmd{
		func() tea.Msg {
			return shared.UpdateHistoryMsg{
				History: h.conversationRepo.GetMessages(),
			}
		},
	}

	statusCmds := h.handleStatusUpdate(msg, chatSession, stateManager)
	cmds = append(cmds, statusCmds...)

	if chatSession := stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, h.listenForChatEvents(chatSession.EventChannel))
	}

	return nil, tea.Batch(cmds...)
}

// handleNoChatSession handles the case when there's no active chat session
func (h *ChatHandler) handleNoChatSession(msg domain.ChatChunkEvent) (tea.Model, tea.Cmd) {
	if msg.ReasoningContent != "" {
		return nil, func() tea.Msg {
			return shared.SetStatusMsg{
				Message:    "Thinking...",
				Spinner:    true,
				StatusType: shared.StatusThinking,
			}
		}
	}
	return nil, nil
}

// handleEmptyContent handles the case when the message has no content
func (h *ChatHandler) handleEmptyContent(chatSession *domain.ChatSession) (tea.Model, tea.Cmd) {
	if chatSession != nil && chatSession.EventChannel != nil {
		return nil, h.listenForChatEvents(chatSession.EventChannel)
	}
	return nil, nil
}

// updateConversationHistory updates the conversation history with the new message
func (h *ChatHandler) updateConversationHistory(msg domain.ChatChunkEvent, chatSession *domain.ChatSession, debugService *services.DebugService) {
	messages := h.conversationRepo.GetMessages()
	shouldUpdateLast := h.shouldUpdateLastMessage(messages, chatSession)

	if shouldUpdateLast {
		h.updateLastMessage(messages, msg, chatSession, debugService)
	} else {
		h.addNewMessage(msg, chatSession, debugService)
	}
}

// shouldUpdateLastMessage determines if we should update the last message or add a new one
func (h *ChatHandler) shouldUpdateLastMessage(messages []domain.ConversationEntry, chatSession *domain.ChatSession) bool {
	return len(messages) > 0 &&
		messages[len(messages)-1].Message.Role == sdk.Assistant &&
		chatSession.RequestID != "" &&
		chatSession.IsFirstChunk
}

// updateLastMessage updates the existing last message with new content
func (h *ChatHandler) updateLastMessage(messages []domain.ConversationEntry, msg domain.ChatChunkEvent, chatSession *domain.ChatSession, debugService *services.DebugService) {
	existingContent := messages[len(messages)-1].Message.Content
	newContent := existingContent + msg.Content

	if err := h.conversationRepo.UpdateLastMessage(newContent); err != nil && debugService != nil {
		debugService.LogError(err, h.name, map[string]any{
			"operation":  "update_assistant_message",
			"request_id": chatSession.RequestID,
		})
	}
}

// addNewMessage adds a new assistant message to the conversation
func (h *ChatHandler) addNewMessage(msg domain.ChatChunkEvent, chatSession *domain.ChatSession, debugService *services.DebugService) {
	assistantEntry := domain.ConversationEntry{
		Message: sdk.Message{
			Role:    sdk.Assistant,
			Content: msg.Content,
		},
		Model: h.modelService.GetCurrentModel(),
		Time:  msg.Timestamp,
	}

	if err := h.conversationRepo.AddMessage(assistantEntry); err != nil && debugService != nil {
		debugService.LogError(err, h.name, map[string]any{
			"operation":  "add_assistant_message",
			"request_id": chatSession.RequestID,
		})
	}
}

// handleStatusUpdate handles updating the chat status and returns appropriate commands
func (h *ChatHandler) handleStatusUpdate(msg domain.ChatChunkEvent, chatSession *domain.ChatSession, stateManager *services.StateManager) []tea.Cmd {
	newStatus, shouldUpdateStatus := h.determineNewStatus(msg, chatSession.Status, chatSession.IsFirstChunk)

	if !shouldUpdateStatus || (newStatus == chatSession.Status && chatSession.IsFirstChunk) {
		return nil
	}

	_ = stateManager.UpdateChatStatus(newStatus)

	if !chatSession.IsFirstChunk {
		chatSession.IsFirstChunk = true
		return h.createFirstChunkStatusCmd(newStatus)
	}

	if newStatus != chatSession.Status {
		return h.createStatusUpdateCmd(newStatus)
	}

	return nil
}

// determineNewStatus determines what the new status should be based on message content
func (h *ChatHandler) determineNewStatus(msg domain.ChatChunkEvent, currentStatus domain.ChatStatus, isFirstChunk bool) (domain.ChatStatus, bool) {
	if msg.ReasoningContent != "" {
		return domain.ChatStatusThinking, true
	}

	if msg.Content != "" {
		if currentStatus != domain.ChatStatusThinking || !isFirstChunk {
			return domain.ChatStatusGenerating, true
		}
	}

	return currentStatus, false
}

// createFirstChunkStatusCmd creates status command for the first chunk
func (h *ChatHandler) createFirstChunkStatusCmd(status domain.ChatStatus) []tea.Cmd {
	switch status {
	case domain.ChatStatusThinking:
		return []tea.Cmd{func() tea.Msg {
			return shared.SetStatusMsg{
				Message:    "Thinking...",
				Spinner:    true,
				StatusType: shared.StatusThinking,
			}
		}}
	case domain.ChatStatusGenerating:
		return []tea.Cmd{func() tea.Msg {
			return shared.SetStatusMsg{
				Message:    "Generating response...",
				Spinner:    true,
				StatusType: shared.StatusGenerating,
			}
		}}
	}
	return nil
}

// createStatusUpdateCmd creates status update command for status changes
func (h *ChatHandler) createStatusUpdateCmd(status domain.ChatStatus) []tea.Cmd {
	switch status {
	case domain.ChatStatusThinking:
		return []tea.Cmd{func() tea.Msg {
			return shared.UpdateStatusMsg{
				Message:    "Thinking...",
				StatusType: shared.StatusThinking,
			}
		}}
	case domain.ChatStatusGenerating:
		return []tea.Cmd{func() tea.Msg {
			return shared.UpdateStatusMsg{
				Message:    "Generating response...",
				StatusType: shared.StatusGenerating,
			}
		}}
	}
	return nil
}

// handleChatComplete processes chat completion events
func (h *ChatHandler) handleChatComplete(
	msg domain.ChatCompleteEvent,
	stateManager *services.StateManager,
	debugService *services.DebugService,
) (tea.Model, tea.Cmd) {

	if debugService != nil {
		debugService.LogSDKEvent("ChatComplete", msg.RequestID, map[string]any{
			"tool_calls_count": len(msg.ToolCalls),
			"has_metrics":      msg.Metrics != nil,
		})
	}

	_ = stateManager.UpdateChatStatus(domain.ChatStatusCompleted)

	stateManager.EndChatSession()

	if len(msg.ToolCalls) > 0 {
		sessionID, cmd := h.toolOrchestrator.StartToolExecution(msg.RequestID, msg.ToolCalls)

		if debugService != nil {
			debugService.LogToolExecution("orchestrator", "session_started", map[string]any{
				"session_id":       sessionID,
				"tool_calls_count": len(msg.ToolCalls),
			})
		}

		return nil, tea.Batch(
			func() tea.Msg {
				return shared.UpdateHistoryMsg{
					History: h.conversationRepo.GetMessages(),
				}
			},
			cmd,
		)
	}

	statusMsg := "Response complete"
	tokenUsage := ""
	if msg.Metrics != nil {
		h.addTokenUsageToSession(msg.Metrics, debugService)
		tokenUsage = h.formatMetrics(msg.Metrics)
	}

	return nil, tea.Batch(
		func() tea.Msg {
			return shared.UpdateHistoryMsg{
				History: h.conversationRepo.GetMessages(),
			}
		},
		func() tea.Msg {
			return shared.SetStatusMsg{
				Message:    statusMsg,
				Spinner:    false,
				TokenUsage: tokenUsage,
				StatusType: shared.StatusDefault,
			}
		},
	)
}

// handleChatError processes chat error events
func (h *ChatHandler) handleChatError(
	msg domain.ChatErrorEvent,
	stateManager *services.StateManager,
	debugService *services.DebugService,
) (tea.Model, tea.Cmd) {

	if debugService != nil {
		debugService.LogError(msg.Error, h.name, map[string]any{
			"request_id": msg.RequestID,
		})
	}

	_ = stateManager.UpdateChatStatus(domain.ChatStatusError)
	stateManager.EndChatSession()
	stateManager.EndToolExecution()

	_ = stateManager.TransitionToView(domain.ViewStateChat)

	errorMsg := fmt.Sprintf("Chat error: %v", msg.Error)
	if strings.Contains(msg.Error.Error(), "timed out") {
		errorMsg = fmt.Sprintf("⏰ %v\n\nSuggestions:\n• Try breaking your request into smaller parts\n• Check if the server is overloaded\n• Verify your network connection", msg.Error)
	}

	return nil, func() tea.Msg {
		return shared.ShowErrorMsg{
			Error:  errorMsg,
			Sticky: true,
		}
	}
}

// handleToolCallStart processes tool call start events
func (h *ChatHandler) handleToolCallStart(
	msg domain.ToolCallStartEvent,
	stateManager *services.StateManager,
	debugService *services.DebugService,
) (tea.Model, tea.Cmd) {

	if debugService != nil {
		debugService.LogSDKEvent("ToolCallStart", msg.RequestID, map[string]any{
			"timestamp": msg.Timestamp,
		})
	}

	var cmds []tea.Cmd

	cmds = append(cmds, func() tea.Msg {
		return shared.SetStatusMsg{
			Message:    "Working...",
			Spinner:    true,
			StatusType: shared.StatusWorking,
		}
	})

	if chatSession := stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, h.listenForChatEvents(chatSession.EventChannel))
	}

	return nil, tea.Batch(cmds...)
}

// handleToolCall processes individual tool call events and executes tools immediately when JSON is complete
func (h *ChatHandler) handleToolCall(
	msg domain.ToolCallEvent,
	stateManager *services.StateManager,
	debugService *services.DebugService,
) (tea.Model, tea.Cmd) {

	if debugService != nil {
		debugService.LogSDKEvent("ToolCall", msg.RequestID, map[string]any{
			"tool_name": msg.ToolName,
			"args":      msg.Args,
		})
	}

	args := strings.TrimSpace(msg.Args)
	toolName := strings.TrimSpace(msg.ToolName)

	if args != "" && toolName != "" && strings.HasSuffix(args, "}") {
		var temp any
		if json.Unmarshal([]byte(args), &temp) == nil {
			if debugService != nil {
				debugService.LogEvent(
					services.DebugEventTypeToolExecution,
					h.name,
					fmt.Sprintf("Executing tool immediately: %s", toolName),
					map[string]any{
						"request_id": msg.RequestID,
						"tool_name":  toolName,
						"args":       args,
					},
				)
			}

			return nil, tea.Batch(
				func() tea.Msg {
					return shared.SetStatusMsg{
						Message:    fmt.Sprintf("Executing tool: %s", toolName),
						Spinner:    true,
						StatusType: shared.StatusWorking,
					}
				},
				h.executeToolCall(msg.RequestID, msg.ToolCallID, toolName, args, stateManager, debugService),
			)
		}
	}

	return nil, func() tea.Msg {
		return shared.SetStatusMsg{
			Message:    fmt.Sprintf("Receiving tool call: %s", toolName),
			Spinner:    true,
			StatusType: shared.StatusWorking,
		}
	}
}

func (h *ChatHandler) handleToolExecutionStarted(
	msg services.ToolExecutionStartedMsg,
	_ *services.StateManager,
	_ *services.DebugService,
) (tea.Model, tea.Cmd) {

	return nil, func() tea.Msg {
		return shared.SetStatusMsg{
			Message:    fmt.Sprintf("Starting tool execution (%d tools)", msg.TotalTools),
			Spinner:    true,
			StatusType: shared.StatusWorking,
		}
	}
}

func (h *ChatHandler) handleToolExecutionProgress(
	msg services.ToolExecutionProgressMsg,
	stateManager *services.StateManager,
	_ *services.DebugService,
) (tea.Model, tea.Cmd) {

	if msg.RequiresApproval {
		_ = stateManager.SetToolApprovalRequired(true)
		stateManager.SetupApprovalUI()
		_ = stateManager.TransitionToView(domain.ViewStateToolApproval)
	}

	return nil, func() tea.Msg {
		return shared.SetStatusMsg{
			Message: fmt.Sprintf("Tool %d/%d: %s (%s)",
				msg.CurrentTool, msg.TotalTools, msg.ToolName, msg.Status),
			Spinner:    true,
			StatusType: shared.StatusWorking,
		}
	}
}

func (h *ChatHandler) handleToolExecutionCompleted(
	msg services.ToolExecutionCompletedMsg,
	stateManager *services.StateManager,
	debugService *services.DebugService,
) (tea.Model, tea.Cmd) {
	return nil, tea.Batch(
		func() tea.Msg {
			return shared.SetStatusMsg{
				Message: fmt.Sprintf("Tools completed (%d/%d successful) - preparing response...",
					msg.SuccessCount, msg.TotalExecuted),
				Spinner:    true,
				StatusType: shared.StatusPreparing,
			}
		},
		h.startChatCompletion(stateManager, debugService),
	)
}

func (h *ChatHandler) handleToolApprovalRequest(
	msg services.ToolApprovalRequestMsg,
	_ *services.StateManager,
	debugService *services.DebugService,
) (tea.Model, tea.Cmd) {
	if debugService != nil {
		debugService.LogToolExecution(msg.ToolCall.Function.Name, "approval_requested", map[string]any{
			"session_id":  msg.SessionID,
			"tool_index":  msg.ToolIndex,
			"total_tools": msg.TotalTools,
		})
	}

	return nil, nil
}

func (h *ChatHandler) handleToolApprovalResponse(
	msg services.ToolApprovalResponseMsg,
	_ *services.StateManager,
	_ *services.DebugService,
) (tea.Model, tea.Cmd) {
	return nil, h.toolOrchestrator.HandleApprovalResponse(msg.Approved, msg.ToolIndex)
}

// executeToolCall executes a single tool call and adds the result to conversation history
func (h *ChatHandler) executeToolCall(
	requestID string,
	toolCallID string,
	toolName string,
	arguments string,
	_ *services.StateManager,
	debugService *services.DebugService,
) tea.Cmd {
	return func() tea.Msg {
		startTime := time.Now()

		// Parse arguments into map
		var argsMap map[string]any
		if err := json.Unmarshal([]byte(arguments), &argsMap); err != nil {
			if debugService != nil {
				debugService.LogError(err, h.name, map[string]any{
					"operation":  "parse_tool_arguments",
					"request_id": requestID,
					"tool_name":  toolName,
					"args":       arguments,
				})
			}

			return shared.ShowErrorMsg{
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
			Model: h.modelService.GetCurrentModel(),
			Time:  time.Now(),
		}

		if err := h.conversationRepo.AddMessage(assistantEntry); err != nil {
			if debugService != nil {
				debugService.LogError(err, h.name, map[string]any{
					"operation":  "add_tool_call_message",
					"request_id": requestID,
					"tool_name":  toolName,
				})
			}
		}

		toolCalls := []sdk.ChatCompletionMessageToolCall{toolCall}

		sessionID, cmd := h.toolOrchestrator.StartToolExecution(requestID, toolCalls)

		if debugService != nil {
			debugService.LogToolExecution(toolName, "immediate_execution_started", map[string]any{
				"request_id": requestID,
				"session_id": sessionID,
				"tool_name":  toolName,
				"duration":   time.Since(startTime).String(),
			})
		}

		return tea.Batch(
			func() tea.Msg {
				return shared.UpdateHistoryMsg{
					History: h.conversationRepo.GetMessages(),
				}
			},
			cmd,
		)()
	}
}

func (h *ChatHandler) handleCommand(
	commandText string,
	stateManager *services.StateManager,
	debugService *services.DebugService,
) (tea.Model, tea.Cmd) {
	command := strings.TrimSpace(strings.TrimPrefix(commandText, "/"))

	if command == "" {
		return nil, func() tea.Msg {
			return shared.ShowErrorMsg{
				Error:  "No command provided. Use: /<command>",
				Sticky: false,
			}
		}
	}

	if debugService != nil {
		debugService.LogEvent(
			services.DebugEventTypeCommand,
			h.name,
			"Processing command",
			map[string]any{
				"command": command,
			},
		)
	}

	parts := strings.Fields(command)
	if len(parts) == 0 {
		return nil, func() tea.Msg {
			return shared.ShowErrorMsg{
				Error:  "Invalid command format",
				Sticky: false,
			}
		}
	}

	mainCommand := parts[0]
	args := parts[1:]

	return nil, h.executeCommand(mainCommand, args, stateManager, debugService)
}

// executeCommand executes the specific command based on the command type
// Commands are processed silently without being added to chat history
func (h *ChatHandler) executeCommand(
	command string,
	args []string,
	stateManager *services.StateManager,
	debugService *services.DebugService,
) tea.Cmd {
	return func() tea.Msg {
		if registryResult := h.tryExecuteFromRegistry(command, args, stateManager); registryResult != nil {
			return registryResult
		}

		switch command {
		case "clear", "cls":
			if err := h.conversationRepo.Clear(); err != nil {
				if debugService != nil {
					debugService.LogError(err, h.name, map[string]any{
						"operation": "clear_history",
					})
				}
				return shared.SetStatusMsg{
					Message:    fmt.Sprintf("Failed to clear conversation: %v", err),
					Spinner:    false,
					StatusType: shared.StatusDefault,
				}
			}
			return tea.Batch(
				func() tea.Msg {
					return shared.UpdateHistoryMsg{
						History: h.conversationRepo.GetMessages(),
					}
				},
				func() tea.Msg {
					return shared.SetStatusMsg{
						Message:    "Conversation cleared",
						Spinner:    false,
						StatusType: shared.StatusDefault,
					}
				},
			)()

		default:
			return shared.SetStatusMsg{
				Message:    fmt.Sprintf("Unknown command: %s", command),
				Spinner:    false,
				StatusType: shared.StatusDefault,
			}
		}
	}
}

// tryExecuteFromRegistry attempts to execute command from the command registry
func (h *ChatHandler) tryExecuteFromRegistry(command string, args []string, stateManager *services.StateManager) tea.Msg {
	if h.commandRegistry == nil {
		return nil
	}

	cmd, exists := h.commandRegistry.Get(command)
	if !exists {
		return nil
	}

	if !cmd.CanExecute(args) {
		return shared.SetStatusMsg{
			Message:    fmt.Sprintf("Invalid usage. Usage: %s", cmd.GetUsage()),
			Spinner:    false,
			StatusType: shared.StatusDefault,
		}
	}

	return h.executeRegistryCommand(cmd, args, stateManager)
}

// executeRegistryCommand executes a command from the registry and handles results
func (h *ChatHandler) executeRegistryCommand(cmd commands.Command, args []string, stateManager *services.StateManager) tea.Msg {
	ctx := context.Background()
	result, err := cmd.Execute(ctx, args)
	if err != nil {
		return shared.SetStatusMsg{
			Message:    fmt.Sprintf("Command failed: %v", err),
			Spinner:    false,
			StatusType: shared.StatusDefault,
		}
	}

	return h.handleCommandSideEffect(result.SideEffect, stateManager)
}

// handleCommandSideEffect handles side effects from command execution
func (h *ChatHandler) handleCommandSideEffect(sideEffect commands.SideEffectType, stateManager *services.StateManager) tea.Msg {
	switch sideEffect {
	case commands.SideEffectSwitchModel:
		return h.handleSwitchModelSideEffect(stateManager)
	case commands.SideEffectClearConversation:
		return h.handleClearConversationSideEffect()
	case commands.SideEffectExportConversation:
		return h.handleExportConversationSideEffect()
	case commands.SideEffectExit:
		return tea.Quit()
	default:
		return shared.SetStatusMsg{
			Message:    "Command completed",
			Spinner:    false,
			StatusType: shared.StatusDefault,
		}
	}
}

// handleSwitchModelSideEffect handles model switching side effect
func (h *ChatHandler) handleSwitchModelSideEffect(stateManager *services.StateManager) tea.Msg {
	_ = stateManager.TransitionToView(domain.ViewStateModelSelection)
	return shared.SetStatusMsg{
		Message:    "Select a model from the dropdown",
		Spinner:    false,
		StatusType: shared.StatusDefault,
	}
}

// handleClearConversationSideEffect handles conversation clearing side effect
func (h *ChatHandler) handleClearConversationSideEffect() tea.Msg {
	if err := h.conversationRepo.Clear(); err != nil {
		return shared.SetStatusMsg{
			Message:    fmt.Sprintf("Failed to clear conversation: %v", err),
			Spinner:    false,
			StatusType: shared.StatusDefault,
		}
	}

	return tea.Batch(
		func() tea.Msg {
			return shared.UpdateHistoryMsg{
				History: h.conversationRepo.GetMessages(),
			}
		},
		func() tea.Msg {
			return shared.SetStatusMsg{
				Message:    "Conversation cleared",
				Spinner:    false,
				StatusType: shared.StatusDefault,
			}
		},
	)()
}

// handleExportConversationSideEffect handles conversation export side effect
func (h *ChatHandler) handleExportConversationSideEffect() tea.Msg {
	return tea.Batch(
		func() tea.Msg {
			return shared.SetStatusMsg{
				Message:    "📝 Generating summary and exporting conversation...",
				Spinner:    true,
				StatusType: shared.StatusWorking,
			}
		},
		h.performExportAsync(),
	)()
}

// performExportAsync performs the export operation asynchronously
func (h *ChatHandler) performExportAsync() tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		cmd, exists := h.commandRegistry.Get("compact")
		if !exists {
			return shared.SetStatusMsg{
				Message:    "Export command not found",
				Spinner:    false,
				StatusType: shared.StatusDefault,
			}
		}

		exportCmd, ok := cmd.(*commands.ExportCommand)
		if !ok {
			return shared.SetStatusMsg{
				Message:    "Invalid export command type",
				Spinner:    false,
				StatusType: shared.StatusDefault,
			}
		}

		filePath, err := exportCmd.PerformExport(ctx)
		if err != nil {
			return shared.SetStatusMsg{
				Message:    fmt.Sprintf("Export failed: %v", err),
				Spinner:    false,
				StatusType: shared.StatusDefault,
			}
		}

		return shared.SetStatusMsg{
			Message:    fmt.Sprintf("📝 Conversation exported to: %s", filePath),
			Spinner:    false,
			StatusType: shared.StatusDefault,
		}
	}
}

func (h *ChatHandler) handleBashCommand(
	commandText string,
	stateManager *services.StateManager,
	debugService *services.DebugService,
) (tea.Model, tea.Cmd) {
	command := strings.TrimSpace(strings.TrimPrefix(commandText, "!"))

	if command == "" {
		return nil, func() tea.Msg {
			return shared.ShowErrorMsg{
				Error:  "No bash command provided. Use: !<command>",
				Sticky: false,
			}
		}
	}

	if debugService != nil {
		debugService.LogEvent(
			services.DebugEventTypeCommand,
			h.name,
			"Processing bash command",
			map[string]any{
				"command": command,
			},
		)
	}

	if !h.toolService.IsToolEnabled("Bash") {
		return nil, func() tea.Msg {
			return shared.ShowErrorMsg{
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

	if err := h.conversationRepo.AddMessage(userEntry); err != nil {
		if debugService != nil {
			debugService.LogError(err, h.name, map[string]any{
				"operation": "add_bash_command_message",
			})
		}
		return nil, func() tea.Msg {
			return shared.ShowErrorMsg{
				Error:  fmt.Sprintf("Failed to save message: %v", err),
				Sticky: false,
			}
		}
	}

	return nil, tea.Batch(
		func() tea.Msg {
			return shared.UpdateHistoryMsg{
				History: h.conversationRepo.GetMessages(),
			}
		},
		func() tea.Msg {
			return shared.SetStatusMsg{
				Message:    fmt.Sprintf("Executing: %s", command),
				Spinner:    true,
				StatusType: shared.StatusWorking,
			}
		},
		h.executeBashCommand(command, stateManager, debugService),
	)
}

// executeBashCommand executes a bash command using the tool service
func (h *ChatHandler) executeBashCommand(
	command string,
	_ *services.StateManager,
	debugService *services.DebugService,
) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		startTime := time.Now()

		h.logBashStart(debugService, command)

		args := map[string]any{
			"command": command,
			"format":  "text",
		}

		if err := h.toolService.ValidateTool("Bash", args); err != nil {
			return h.handleBashValidationError(debugService, command, err)
		}

		result, err := h.toolService.ExecuteTool(ctx, "Bash", args)
		duration := time.Since(startTime)

		if err != nil {
			return h.handleBashExecutionError(debugService, command, duration, err)
		}

		h.logBashComplete(debugService, command, result, duration)

		responseContent := h.formatBashResponse(result)
		h.addBashResponseToHistory(debugService, responseContent)

		return h.createBashUIUpdate(result.Success)
	}
}

func (h *ChatHandler) logBashStart(debugService *services.DebugService, command string) {
	if debugService != nil {
		debugService.LogToolExecution("Bash", "execution_started", map[string]any{
			"command": command,
		})
	}
}

func (h *ChatHandler) handleBashValidationError(debugService *services.DebugService, command string, err error) tea.Msg {
	if debugService != nil {
		debugService.LogError(err, h.name, map[string]any{
			"operation": "validate_bash_command",
			"command":   command,
		})
	}

	errorEntry := domain.ConversationEntry{
		Message: sdk.Message{
			Role:    sdk.Assistant,
			Content: fmt.Sprintf("❌ Error: %v", err),
		},
		Model: h.modelService.GetCurrentModel(),
		Time:  time.Now(),
	}

	if addErr := h.conversationRepo.AddMessage(errorEntry); addErr != nil && debugService != nil {
		debugService.LogError(addErr, h.name, map[string]any{
			"operation": "add_error_message",
		})
	}

	return tea.Batch(
		func() tea.Msg {
			return shared.UpdateHistoryMsg{
				History: h.conversationRepo.GetMessages(),
			}
		},
		func() tea.Msg {
			return shared.ShowErrorMsg{
				Error:  fmt.Sprintf("Command validation failed: %v", err),
				Sticky: false,
			}
		},
	)()
}

func (h *ChatHandler) handleBashExecutionError(debugService *services.DebugService, command string, duration time.Duration, err error) tea.Msg {
	if debugService != nil {
		debugService.LogError(err, h.name, map[string]any{
			"operation": "execute_bash_command",
			"command":   command,
			"duration":  duration.String(),
		})
	}

	errorEntry := domain.ConversationEntry{
		Message: sdk.Message{
			Role:    sdk.Assistant,
			Content: fmt.Sprintf("❌ Execution failed: %v", err),
		},
		Model: h.modelService.GetCurrentModel(),
		Time:  time.Now(),
	}

	if addErr := h.conversationRepo.AddMessage(errorEntry); addErr != nil && debugService != nil {
		debugService.LogError(addErr, h.name, map[string]any{
			"operation": "add_error_message",
		})
	}

	return tea.Batch(
		func() tea.Msg {
			return shared.UpdateHistoryMsg{
				History: h.conversationRepo.GetMessages(),
			}
		},
		func() tea.Msg {
			return shared.ShowErrorMsg{
				Error:  fmt.Sprintf("Command execution failed: %v", err),
				Sticky: false,
			}
		},
	)()
}

func (h *ChatHandler) logBashComplete(debugService *services.DebugService, command string, result *domain.ToolExecutionResult, duration time.Duration) {
	if debugService != nil {
		debugService.LogToolExecution("Bash", "execution_completed", map[string]any{
			"command":  command,
			"success":  result.Success,
			"duration": duration.String(),
		})
	}
}

func (h *ChatHandler) formatBashResponse(result *domain.ToolExecutionResult) string {
	if result.Success {
		return h.formatSuccessfulBashResponse(result)
	}
	return h.formatFailedBashResponse(result)
}

func (h *ChatHandler) formatSuccessfulBashResponse(result *domain.ToolExecutionResult) string {
	if bashResult, ok := result.Data.(*domain.BashToolResult); ok {
		responseContent := fmt.Sprintf("✅ Command executed successfully:\n\n```bash\n$ %s\n```\n\n", bashResult.Command)
		if bashResult.Output != "" {
			responseContent += fmt.Sprintf("**Output:**\n```\n%s\n```", strings.TrimSpace(bashResult.Output))
		}
		if bashResult.Duration != "" {
			responseContent += fmt.Sprintf("\n\n*Execution time: %s*", bashResult.Duration)
		}
		return responseContent
	}
	return "✅ Command executed successfully (no output)"
}

func (h *ChatHandler) formatFailedBashResponse(result *domain.ToolExecutionResult) string {
	if bashResult, ok := result.Data.(*domain.BashToolResult); ok {
		responseContent := fmt.Sprintf("❌ Command failed with exit code %d:\n\n```bash\n$ %s\n```\n\n", bashResult.ExitCode, bashResult.Command)
		if bashResult.Output != "" {
			responseContent += fmt.Sprintf("**Output:**\n```\n%s\n```", strings.TrimSpace(bashResult.Output))
		}
		if bashResult.Error != "" {
			responseContent += fmt.Sprintf("\n\n**Error:** %s", bashResult.Error)
		}
		return responseContent
	} else if result.Error != "" {
		return fmt.Sprintf("❌ Command failed: %s", result.Error)
	}
	return "❌ Command failed for unknown reason"
}

func (h *ChatHandler) addBashResponseToHistory(debugService *services.DebugService, responseContent string) {
	assistantEntry := domain.ConversationEntry{
		Message: sdk.Message{
			Role:    sdk.Assistant,
			Content: responseContent,
		},
		Model: h.modelService.GetCurrentModel(),
		Time:  time.Now(),
	}

	if err := h.conversationRepo.AddMessage(assistantEntry); err != nil && debugService != nil {
		debugService.LogError(err, h.name, map[string]any{
			"operation": "add_bash_response_message",
		})
	}
}

func (h *ChatHandler) createBashUIUpdate(success bool) tea.Msg {
	statusMsg := "Command completed"
	if !success {
		statusMsg = "Command failed"
	}

	return tea.Batch(
		func() tea.Msg {
			return shared.UpdateHistoryMsg{
				History: h.conversationRepo.GetMessages(),
			}
		},
		func() tea.Msg {
			return shared.SetStatusMsg{
				Message:    statusMsg,
				Spinner:    false,
				StatusType: shared.StatusDefault,
			}
		},
	)()
}

func (h *ChatHandler) formatMetrics(metrics *domain.ChatMetrics) string {
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

	sessionStats := h.conversationRepo.GetSessionTokens()
	if sessionStats.TotalInputTokens > 0 {
		parts = append(parts, fmt.Sprintf("Session Input: %d tokens", sessionStats.TotalInputTokens))
	}

	return strings.Join(parts, " | ")
}

// addTokenUsageToSession accumulates token usage to session totals
func (h *ChatHandler) addTokenUsageToSession(metrics *domain.ChatMetrics, debugService *services.DebugService) {
	if metrics == nil || metrics.Usage == nil {
		return
	}

	if err := h.conversationRepo.AddTokenUsage(
		int(metrics.Usage.PromptTokens),
		int(metrics.Usage.CompletionTokens),
		int(metrics.Usage.TotalTokens),
	); err != nil && debugService != nil {
		debugService.LogError(err, h.name, map[string]any{
			"operation": "add_token_usage",
		})
	}
}

func generateRequestID() string {
	return fmt.Sprintf("req_%d", time.Now().UnixNano())
}

// handleFileSelectionRequest handles the file selection request triggered by "@" key
func (h *ChatHandler) handleFileSelectionRequest(
	_ shared.FileSelectionRequestMsg,
	stateManager *services.StateManager,
	debugService *services.DebugService,
) (tea.Model, tea.Cmd) {
	files, err := h.fileService.ListProjectFiles()
	if err != nil {
		if debugService != nil && debugService.IsEnabled() {
			debugService.LogEvent(
				services.DebugEventTypeError,
				"ChatHandler",
				"Failed to list project files",
				map[string]any{
					"error": err.Error(),
				},
			)
		}
		return nil, func() tea.Msg {
			return shared.ShowErrorMsg{
				Error:  fmt.Sprintf("Failed to load files: %v", err),
				Sticky: false,
			}
		}
	}

	if len(files) == 0 {
		return nil, func() tea.Msg {
			return shared.ShowErrorMsg{
				Error:  "No files found in the current directory",
				Sticky: false,
			}
		}
	}

	// Transition to file selection view
	if err := stateManager.TransitionToView(domain.ViewStateFileSelection); err != nil {
		if debugService != nil && debugService.IsEnabled() {
			debugService.LogEvent(
				services.DebugEventTypeError,
				"ChatHandler",
				"Failed to transition to file selection view",
				map[string]any{
					"error": err.Error(),
				},
			)
		}
		return nil, func() tea.Msg {
			return shared.ShowErrorMsg{
				Error:  "Failed to open file selection",
				Sticky: false,
			}
		}
	}

	return nil, func() tea.Msg {
		return shared.SetupFileSelectionMsg{
			Files: files,
		}
	}
}
