package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/inference-gateway/cli/internal/commands"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/ui"
	sdk "github.com/inference-gateway/sdk"
)

// ChatMessageHandler handles chat-related messages
type ChatMessageHandler struct {
	chatService      domain.ChatService
	conversationRepo domain.ConversationRepository
	modelService     domain.ModelService
	commandRegistry  *commands.Registry
}

// NewChatMessageHandler creates a new chat message handler
func NewChatMessageHandler(
	chatService domain.ChatService,
	conversationRepo domain.ConversationRepository,
	modelService domain.ModelService,
	commandRegistry *commands.Registry,
) *ChatMessageHandler {
	return &ChatMessageHandler{
		chatService:      chatService,
		conversationRepo: conversationRepo,
		modelService:     modelService,
		commandRegistry:  commandRegistry,
	}
}

func (h *ChatMessageHandler) GetPriority() int {
	return 100
}

func (h *ChatMessageHandler) CanHandle(msg tea.Msg) bool {
	switch msg.(type) {
	case ui.UserInputMsg:
		return true
	case ChatStreamStartedMsg:
		return true
	case ToolCallDetectedMsg:
		return true
	case domain.ChatStartEvent, domain.ChatChunkEvent, domain.ChatCompleteEvent, domain.ChatErrorEvent:
		return true
	default:
		return false
	}
}

func (h *ChatMessageHandler) Handle(msg tea.Msg, state *AppState) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ui.UserInputMsg:
		return h.handleUserInput(msg, state)

	case ChatStreamStartedMsg:
		return h.handleStreamStarted(msg, state)

	case ToolCallDetectedMsg:
		return h.handleToolCallDetected(msg, state)

	case domain.ChatStartEvent:
		return h.handleChatStart(msg, state)

	case domain.ChatChunkEvent:
		return h.handleChatChunk(msg, state)

	case domain.ChatCompleteEvent:
		return h.handleChatComplete(msg, state)

	case domain.ChatErrorEvent:
		return h.handleChatError(msg, state)
	}

	return nil, nil
}

func (h *ChatMessageHandler) handleUserInput(msg ui.UserInputMsg, state *AppState) (tea.Model, tea.Cmd) {
	if strings.HasPrefix(msg.Content, "/") {
		return h.handleCommand(msg.Content, state)
	}

	processedContent := h.processFileReferences(msg.Content)

	userEntry := domain.ConversationEntry{
		Message: sdk.Message{
			Role:    sdk.User,
			Content: processedContent,
		},
		Time: time.Now(),
	}

	if err := h.conversationRepo.AddMessage(userEntry); err != nil {
		return nil, func() tea.Msg {
			return ui.ShowErrorMsg{
				Error:  fmt.Sprintf("Failed to save message: %v", err),
				Sticky: false,
			}
		}
	}

	messages := h.conversationToSDKMessages()

	var cmds []tea.Cmd

	cmds = append(cmds, func() tea.Msg {
		return ui.UpdateHistoryMsg{
			History: h.conversationRepo.GetMessages(),
		}
	})

	cmds = append(cmds, h.startChatCompletion(messages))

	return nil, tea.Batch(cmds...)
}

func (h *ChatMessageHandler) handleCommand(commandText string, state *AppState) (tea.Model, tea.Cmd) {
	commandName := strings.TrimPrefix(commandText, "/")

	cmd, exists := h.commandRegistry.Get(commandName)
	if !exists || cmd == nil {
		return nil, func() tea.Msg {
			return ui.ShowErrorMsg{
				Error:  fmt.Sprintf("Command not found: %s", commandName),
				Sticky: false,
			}
		}
	}

	ctx := context.Background()
	result, err := cmd.Execute(ctx, []string{})

	if err != nil {
		return nil, func() tea.Msg {
			return ui.ShowErrorMsg{
				Error:  fmt.Sprintf("Command execution failed: %v", err),
				Sticky: false,
			}
		}
	}

	switch result.SideEffect {
	case commands.SideEffectExit:
		return nil, tea.Quit
	case commands.SideEffectClearConversation:
		return nil, tea.Batch(
			func() tea.Msg {
				return ui.UpdateHistoryMsg{
					History: []domain.ConversationEntry{},
				}
			},
			func() tea.Msg {
				return ui.SetStatusMsg{
					Message: result.Output,
					Spinner: false,
				}
			},
		)
	default:
		return nil, func() tea.Msg {
			return ui.SetStatusMsg{
				Message: result.Output,
				Spinner: false,
			}
		}
	}
}

func (h *ChatMessageHandler) handleStreamStarted(msg ChatStreamStartedMsg, state *AppState) (tea.Model, tea.Cmd) {
	state.Data["eventChannel"] = msg.EventChannel
	if msg.RequestID != "" {
		state.Data["currentRequestID"] = msg.RequestID
	}
	return nil, h.listenForChatEvents(msg.EventChannel)
}

func (h *ChatMessageHandler) handleToolCallDetected(msg ToolCallDetectedMsg, state *AppState) (tea.Model, tea.Cmd) {
	state.Data["pendingToolCall"] = msg.ToolCall
	state.Data["toolCallResponse"] = msg.Response
	state.Data["approvalSelectedIndex"] = int(domain.ApprovalApprove)

	state.CurrentView = ViewApproval

	return nil, func() tea.Msg {
		return ui.SetStatusMsg{
			Message: fmt.Sprintf("SWITCHED TO APPROVAL VIEW: %s", msg.ToolCall.Name),
			Spinner: false,
		}
	}
}

func (h *ChatMessageHandler) handleChatStart(msg domain.ChatStartEvent, state *AppState) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	// Store the request ID for cancellation
	state.Data["currentRequestID"] = msg.RequestID

	cmds = append(cmds, func() tea.Msg {
		return ui.SetStatusMsg{
			Message: "Generating response...",
			Spinner: true,
		}
	})

	if eventChan, ok := state.Data["eventChannel"].(<-chan domain.ChatEvent); ok {
		cmds = append(cmds, h.listenForChatEvents(eventChan))
	}

	return nil, tea.Batch(cmds...)
}

func (h *ChatMessageHandler) handleChatChunk(msg domain.ChatChunkEvent, state *AppState) (tea.Model, tea.Cmd) {
	messages := h.conversationRepo.GetMessages()

	if len(messages) > 0 && messages[len(messages)-1].Message.Role == sdk.Assistant {
		existingContent := messages[len(messages)-1].Message.Content
		newContent := existingContent + msg.Content

		if err := h.conversationRepo.UpdateLastMessage(newContent); err != nil {
			return nil, func() tea.Msg {
				return ui.ShowErrorMsg{
					Error:  fmt.Sprintf("Failed to update assistant message: %v", err),
					Sticky: false,
				}
			}
		}
	} else {
		assistantEntry := domain.ConversationEntry{
			Message: sdk.Message{
				Role:    sdk.Assistant,
				Content: msg.Content,
			},
			Model: h.modelService.GetCurrentModel(),
			Time:  msg.Timestamp,
		}

		if err := h.conversationRepo.AddMessage(assistantEntry); err != nil {
			return nil, func() tea.Msg {
				return ui.ShowErrorMsg{
					Error:  fmt.Sprintf("Failed to save assistant message: %v", err),
					Sticky: false,
				}
			}
		}
	}

	var cmds []tea.Cmd

	cmds = append(cmds, func() tea.Msg {
		return ui.UpdateHistoryMsg{
			History: h.conversationRepo.GetMessages(),
		}
	})

	if eventChan, ok := state.Data["eventChannel"].(<-chan domain.ChatEvent); ok {
		cmds = append(cmds, h.listenForChatEvents(eventChan))
	}

	return nil, tea.Batch(cmds...)
}

func (h *ChatMessageHandler) handleChatComplete(msg domain.ChatCompleteEvent, state *AppState) (tea.Model, tea.Cmd) {
	statusMsg := ui.FormatSuccess("Response complete")
	if msg.Metrics != nil {
		statusMsg = ui.FormatSuccess(fmt.Sprintf("Complete - %s", h.formatMetrics(msg.Metrics)))
	}

	delete(state.Data, "eventChannel")
	delete(state.Data, "currentRequestID")

	// Check for structured tool calls first (preferred method)
	if len(msg.ToolCalls) > 0 {
		// Update the last assistant message to include the tool calls
		h.updateAssistantMessageWithToolCalls(msg.ToolCalls)

		toolCall := msg.ToolCalls[0] // Handle first tool call

		// Parse arguments from JSON string to map
		args := make(map[string]interface{})
		if toolCall.Function.Arguments != "" {
			if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err == nil {
				toolCallRequest := ToolCallRequest{
					ID:        toolCall.Id,
					Name:      toolCall.Function.Name,
					Arguments: args,
				}

				return nil, func() tea.Msg {
					return ToolCallDetectedMsg{
						ToolCall: toolCallRequest,
						Response: msg.Message,
					}
				}
			}
		}
	}

	// Fallback to text parsing for backward compatibility
	if toolCall := h.parseToolCall(msg.Message); toolCall != nil {
		// Generate a unique tool call ID for text-based tool calls
		toolCallID := h.generateToolCallID()
		toolCall.ID = toolCallID

		// Create structured tool call for the assistant message
		structuredToolCall := sdk.ChatCompletionMessageToolCall{
			Id:   toolCallID,
			Type: "function",
			Function: sdk.ChatCompletionMessageToolCallFunction{
				Name:      toolCall.Name,
				Arguments: h.marshalToolArguments(toolCall.Arguments),
			},
		}

		// Update the assistant message with structured tool calls
		h.updateAssistantMessageWithToolCalls([]sdk.ChatCompletionMessageToolCall{structuredToolCall})

		return nil, func() tea.Msg {
			return ToolCallDetectedMsg{
				ToolCall: *toolCall,
				Response: msg.Message,
			}
		}
	}

	return nil, func() tea.Msg {
		return ui.SetStatusMsg{
			Message: statusMsg,
			Spinner: false,
		}
	}
}

func (h *ChatMessageHandler) handleChatError(msg domain.ChatErrorEvent, state *AppState) (tea.Model, tea.Cmd) {
	errorMsg := ui.FormatError(msg.Error.Error())

	if contains(msg.Error.Error(), "timed out") {
		errorMsg = fmt.Sprintf("⏰ %v\n\nSuggestions:\n• Try breaking your request into smaller parts\n• Check if the server is overloaded\n• Verify your network connection", msg.Error)
	}

	delete(state.Data, "eventChannel")
	delete(state.Data, "currentRequestID")

	return nil, func() tea.Msg {
		return ui.ShowErrorMsg{
			Error:  errorMsg,
			Sticky: true,
		}
	}
}

func (h *ChatMessageHandler) conversationToSDKMessages() []sdk.Message {
	entries := h.conversationRepo.GetMessages()
	messages := make([]sdk.Message, len(entries))

	for i, entry := range entries {
		messages[i] = entry.Message
	}

	return messages
}

func (h *ChatMessageHandler) startChatCompletion(messages []sdk.Message) tea.Cmd {
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

		eventChan, err := h.chatService.SendMessage(ctx, currentModel, messages)
		if err != nil {
			return domain.ChatErrorEvent{
				RequestID: "unknown",
				Timestamp: time.Now(),
				Error:     err,
			}
		}

		requestID := "unknown"

		return ChatStreamStartedMsg{
			EventChannel: eventChan,
			RequestID:    requestID,
		}
	}
}

// ChatStreamStartedMsg wraps the event channel for stream processing
type ChatStreamStartedMsg struct {
	EventChannel <-chan domain.ChatEvent
	RequestID    string
}

// ToolCallRequest represents a parsed tool call from LLM response
type ToolCallRequest struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// ToolCallDetectedMsg indicates a tool call was found in the response
type ToolCallDetectedMsg struct {
	ToolCall ToolCallRequest
	Response string
}

// listenForChatEvents creates a command that listens for the next chat event
func (h *ChatMessageHandler) listenForChatEvents(eventChan <-chan domain.ChatEvent) tea.Cmd {
	return func() tea.Msg {
		if event, ok := <-eventChan; ok {
			return event
		}
		return nil
	}
}

func (h *ChatMessageHandler) formatMetrics(metrics *domain.ChatMetrics) string {
	if metrics == nil {
		return ""
	}

	var parts []string

	duration := metrics.Duration.Round(time.Millisecond)
	parts = append(parts, fmt.Sprintf("Time: %v", duration))

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

	return joinStrings(parts, " | ")
}

// parseToolCall parses tool calls from LLM response text
func (h *ChatMessageHandler) parseToolCall(response string) *ToolCallRequest {
	toolCallIndex := strings.Index(response, "TOOL_CALL:")
	if toolCallIndex == -1 {
		return nil
	}

	jsonStart := toolCallIndex + len("TOOL_CALL:")
	if remainingIdx := strings.Index(response[jsonStart:], "{"); remainingIdx != -1 {
		jsonStart = jsonStart + remainingIdx
	} else {
		return nil
	}

	braceCount := 0
	jsonEnd := jsonStart
	for i := jsonStart; i < len(response); i++ {
		if response[i] == '{' {
			braceCount++
		} else if response[i] == '}' {
			braceCount--
			if braceCount == 0 {
				jsonEnd = i + 1
				break
			}
		}
	}

	if braceCount != 0 {
		return nil
	}

	jsonStr := response[jsonStart:jsonEnd]
	var toolCall ToolCallRequest
	if err := json.Unmarshal([]byte(jsonStr), &toolCall); err != nil {
		return nil
	}

	return &toolCall
}

// Helper functions
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		(len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
			containsAt(s, substr, 1))))
}

func containsAt(s, substr string, start int) bool {
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	if len(strs) == 1 {
		return strs[0]
	}

	result := strs[0]
	for _, str := range strs[1:] {
		result += sep + str
	}
	return result
}

// updateAssistantMessageWithToolCalls updates the last assistant message to include tool calls
func (h *ChatMessageHandler) updateAssistantMessageWithToolCalls(toolCalls []sdk.ChatCompletionMessageToolCall) {
	_ = h.conversationRepo.UpdateLastMessageToolCalls(&toolCalls)
	// Ignore error - the tool call will still work without proper tool_calls structure in the conversation
}

// marshalToolArguments converts tool arguments map to JSON string
func (h *ChatMessageHandler) marshalToolArguments(args map[string]interface{}) string {
	if args == nil {
		return "{}"
	}

	jsonBytes, err := json.Marshal(args)
	if err != nil {
		return "{}"
	}

	return string(jsonBytes)
}

// generateToolCallID generates a unique tool call ID for text-based tool calls
func (h *ChatMessageHandler) generateToolCallID() string {
	return fmt.Sprintf("call_%d", time.Now().UnixNano())
}

// processFileReferences processes @file references in user input and embeds file contents
func (h *ChatMessageHandler) processFileReferences(content string) string {
	fileRefRegex := regexp.MustCompile(`@([a-zA-Z0-9._/\\-]+\.[a-zA-Z0-9]+)`)

	return fileRefRegex.ReplaceAllStringFunc(content, func(match string) string {
		filename := match[1:]

		var fullPath string
		if filepath.IsAbs(filename) {
			fullPath = filename
		} else {
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Sprintf("\n[Error: Could not determine working directory for file %s: %v]\n", filename, err)
			}
			fullPath = filepath.Join(cwd, filename)
		}

		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			return fmt.Sprintf("\n[Error: File not found: %s]\n", filename)
		}

		content, err := os.ReadFile(fullPath)
		if err != nil {
			return fmt.Sprintf("\n[Error: Could not read file %s: %v]\n", filename, err)
		}

		const maxFileSize = 50 * 1024 // 50KB
		if len(content) > maxFileSize {
			return fmt.Sprintf("\n[Error: File %s is too large (%d bytes). Maximum size is %d bytes.]\n", filename, len(content), maxFileSize)
		}

		ext := strings.ToLower(filepath.Ext(filename))
		var language string
		switch ext {
		case ".go":
			language = "go"
		case ".js", ".jsx":
			language = "javascript"
		case ".ts", ".tsx":
			language = "typescript"
		case ".py":
			language = "python"
		case ".java":
			language = "java"
		case ".c", ".h":
			language = "c"
		case ".cpp", ".cc", ".cxx", ".hpp":
			language = "cpp"
		case ".rs":
			language = "rust"
		case ".rb":
			language = "ruby"
		case ".php":
			language = "php"
		case ".sh":
			language = "bash"
		case ".sql":
			language = "sql"
		case ".html", ".htm":
			language = "html"
		case ".css":
			language = "css"
		case ".xml":
			language = "xml"
		case ".json":
			language = "json"
		case ".yaml", ".yml":
			language = "yaml"
		case ".md":
			language = "markdown"
		default:
			language = "text"
		}

		return fmt.Sprintf("\n\n📁 **File: %s**\n```%s\n%s\n```\n", filename, language, string(content))
	})
}
