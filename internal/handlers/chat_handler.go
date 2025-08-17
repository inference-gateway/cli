package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/commands"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/ui"
	"github.com/inference-gateway/cli/internal/ui/shared"
	sdk "github.com/inference-gateway/sdk"
)

// ChatMessageHandler handles chat-related messages
type ChatMessageHandler struct {
	chatService      domain.ChatService
	conversationRepo domain.ConversationRepository
	modelService     domain.ModelService
	commandRegistry  *commands.Registry
	config           *config.Config
}

// NewChatMessageHandler creates a new chat message handler
func NewChatMessageHandler(
	chatService domain.ChatService,
	conversationRepo domain.ConversationRepository,
	modelService domain.ModelService,
	commandRegistry *commands.Registry,
	config *config.Config,
) *ChatMessageHandler {
	return &ChatMessageHandler{
		chatService:      chatService,
		conversationRepo: conversationRepo,
		modelService:     modelService,
		commandRegistry:  commandRegistry,
		config:           config,
	}
}

func (h *ChatMessageHandler) GetPriority() int {
	return 100
}

func (h *ChatMessageHandler) CanHandle(msg tea.Msg) bool {
	switch msg.(type) {
	case shared.UserInputMsg:
		return true
	case ChatStreamStartedMsg:
		return true
	case ToolCallDetectedMsg:
		return true
	case ToolAutoApproveMsg:
		return true
	case StoreRemainingToolCallsMsg:
		return true
	case ProcessNextToolCallMsg:
		return true
	case TriggerFollowUpLLMCallMsg:
		return true
	case domain.ChatStartEvent, domain.ChatChunkEvent, domain.ChatCompleteEvent, domain.ChatErrorEvent:
		return true
	default:
		return false
	}
}

func (h *ChatMessageHandler) Handle(msg tea.Msg, state *AppState) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case shared.UserInputMsg:
		return h.handleUserInput(msg, state)

	case ChatStreamStartedMsg:
		return h.handleStreamStarted(msg, state)

	case ToolCallDetectedMsg:
		return h.handleToolCallDetected(msg, state)

	case ToolAutoApproveMsg:
		return nil, nil

	case StoreRemainingToolCallsMsg:
		return h.handleStoreRemainingToolCalls(msg, state)

	case ProcessNextToolCallMsg:
		return h.handleProcessNextToolCall(msg, state)

	case TriggerFollowUpLLMCallMsg:
		return h.handleTriggerFollowUpLLMCall(msg, state)

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

func (h *ChatMessageHandler) handleUserInput(msg shared.UserInputMsg, state *AppState) (tea.Model, tea.Cmd) {
	if strings.HasPrefix(msg.Content, "/") {
		return h.handleCommand(msg.Content, state)
	}

	if strings.HasPrefix(msg.Content, "!") {
		return h.handleBashCommand(msg.Content, state)
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
			return shared.ShowErrorMsg{
				Error:  fmt.Sprintf("Failed to save message: %v", err),
				Sticky: false,
			}
		}
	}

	messages := h.conversationToSDKMessages()

	var cmds []tea.Cmd

	cmds = append(cmds, func() tea.Msg {
		return shared.UpdateHistoryMsg{
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
			return shared.ShowErrorMsg{
				Error:  fmt.Sprintf("Command not found: %s", commandName),
				Sticky: false,
			}
		}
	}

	ctx := context.Background()
	result, err := cmd.Execute(ctx, []string{})

	if err != nil {
		return nil, func() tea.Msg {
			return shared.ShowErrorMsg{
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
				return shared.UpdateHistoryMsg{
					History: []domain.ConversationEntry{},
				}
			},
			func() tea.Msg {
				return shared.SetStatusMsg{
					Message: result.Output,
					Spinner: false,
				}
			},
		)
	case commands.SideEffectExportConversation:
		return nil, tea.Batch(
			func() tea.Msg {
				return shared.SetStatusMsg{
					Message: result.Output,
					Spinner: true,
				}
			},
			h.performExport(cmd, result.Data),
		)
	case commands.SideEffectSwitchModel:
		return nil, func() tea.Msg {
			return SwitchModelMsg{}
		}
	default:
		return nil, func() tea.Msg {
			return shared.SetStatusMsg{
				Message: result.Output,
				Spinner: false,
			}
		}
	}
}

func (h *ChatMessageHandler) handleBashCommand(commandText string, state *AppState) (tea.Model, tea.Cmd) {
	bashCommand := strings.TrimPrefix(commandText, "!")
	bashCommand = strings.TrimSpace(bashCommand)

	if bashCommand == "" {
		return nil, func() tea.Msg {
			return shared.ShowErrorMsg{
				Error:  "No command provided after '!'",
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
		return nil, func() tea.Msg {
			return shared.ShowErrorMsg{
				Error:  fmt.Sprintf("Failed to save message: %v", err),
				Sticky: false,
			}
		}
	}

	updateHistoryCmd := func() tea.Msg {
		return shared.UpdateHistoryMsg{
			History: h.conversationRepo.GetMessages(),
		}
	}

	executeBashCmd := func() tea.Msg {
		return h.executeBashCommand(bashCommand)
	}

	return nil, tea.Batch(updateHistoryCmd, executeBashCmd)
}

func (h *ChatMessageHandler) executeBashCommand(command string) tea.Msg {
	cmd := exec.Command("sh", "-c", command)

	output, err := cmd.CombinedOutput()

	var content string

	if err != nil {
		content = fmt.Sprintf("Bash Command: `%s`\n\n❌ **Command failed:**\n```\n%s\n```\n\n**Error:** %v", command, string(output), err)
	} else {
		if len(output) == 0 {
			content = fmt.Sprintf("Bash Command: `%s`\n\n✅ **Command executed successfully** (no output)", command)
		} else {
			content = fmt.Sprintf("Bash Command: `%s`\n\n✅ **Output:**\n```\n%s\n```", command, string(output))
		}
	}

	bashResultEntry := domain.ConversationEntry{
		Message: sdk.Message{
			Role:    sdk.User,
			Content: content,
		},
		Time: time.Now(),
	}

	if saveErr := h.conversationRepo.AddMessage(bashResultEntry); saveErr != nil {
		return shared.ShowErrorMsg{
			Error:  fmt.Sprintf("Failed to save bash result: %v", saveErr),
			Sticky: false,
		}
	}

	return shared.UpdateHistoryMsg{
		History: h.conversationRepo.GetMessages(),
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
	if !h.config.IsApprovalRequired(msg.ToolCall.Name) {
		return nil, func() tea.Msg {
			return ToolAutoApproveMsg(msg)
		}
	}

	state.Data["pendingToolCall"] = msg.ToolCall
	state.Data["toolCallResponse"] = msg.Response
	state.Data["approvalSelectedIndex"] = int(domain.ApprovalApprove)

	state.CurrentView = ViewApproval

	return nil, func() tea.Msg {
		return shared.SetStatusMsg{
			Message: fmt.Sprintf("SWITCHED TO APPROVAL VIEW: %s", msg.ToolCall.String()),
			Spinner: false,
		}
	}
}

func (h *ChatMessageHandler) handleChatStart(msg domain.ChatStartEvent, state *AppState) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	state.Data["currentRequestID"] = msg.RequestID

	cmds = append(cmds, func() tea.Msg {
		return shared.SetStatusMsg{
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
				return shared.ShowErrorMsg{
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
				return shared.ShowErrorMsg{
					Error:  fmt.Sprintf("Failed to save assistant message: %v", err),
					Sticky: false,
				}
			}
		}
	}

	var cmds []tea.Cmd

	cmds = append(cmds, func() tea.Msg {
		return shared.UpdateHistoryMsg{
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
	tokenUsage := ""
	if msg.Metrics != nil {
		statusMsg = ui.FormatSuccess("Response complete")
		tokenUsage = h.formatMetrics(msg.Metrics)
	}

	delete(state.Data, "eventChannel")
	delete(state.Data, "currentRequestID")

	if len(msg.ToolCalls) > 0 {
		return h.handleToolCalls(msg, statusMsg, tokenUsage)
	}

	return nil, func() tea.Msg {
		return shared.SetStatusMsg{
			Message:    statusMsg,
			Spinner:    false,
			TokenUsage: tokenUsage,
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

	delete(state.Data, "remainingToolCalls")
	delete(state.Data, "pendingToolCall")
	delete(state.Data, "toolCallResponse")
	delete(state.Data, "approvalSelectedIndex")

	state.CurrentView = ViewChat

	return nil, func() tea.Msg {
		return shared.ShowErrorMsg{
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

// String returns a formatted representation of the tool call
func (t ToolCallRequest) String() string {
	return ui.FormatToolCall(t.Name, t.Arguments)
}

// ToolCallDetectedMsg indicates a tool call was found in the response
type ToolCallDetectedMsg struct {
	ToolCall ToolCallRequest
	Response string
}

// ToolAutoApproveMsg indicates a tool should be auto-executed without approval
type ToolAutoApproveMsg struct {
	ToolCall ToolCallRequest
	Response string
}

// StoreRemainingToolCallsMsg stores remaining tool calls for sequential processing
type StoreRemainingToolCallsMsg struct {
	RemainingCalls []sdk.ChatCompletionMessageToolCall
}

// ProcessNextToolCallMsg triggers processing of the next tool call in the queue
type ProcessNextToolCallMsg struct{}

// TriggerFollowUpLLMCallMsg triggers the follow-up LLM call after all tools are executed
type TriggerFollowUpLLMCallMsg struct{}

// SwitchModelMsg indicates that model selection view should be shown
type SwitchModelMsg struct{}

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

// processFileReferences processes @file references in user input and embeds file contents
func (h *ChatMessageHandler) processFileReferences(content string) string {
	fileRefRegex := regexp.MustCompile(`@([a-zA-Z0-9._/\\-]+\.[a-zA-Z0-9]+)`)

	return fileRefRegex.ReplaceAllStringFunc(content, func(match string) string {
		filename := match[1:]
		return h.processFileReference(filename)
	})
}

func (h *ChatMessageHandler) processFileReference(filename string) string {
	fullPath, err := h.resolveFilePath(filename)
	if err != nil {
		return fmt.Sprintf("\n[Error: Could not determine working directory for file %s: %v]\n", filename, err)
	}

	content, err := h.readAndValidateFile(fullPath, filename)
	if err != nil {
		return fmt.Sprintf("\n[Error: %v]\n", err)
	}

	language := h.getLanguageFromExtension(filename)
	return fmt.Sprintf("\n\n📁 **File: %s**\n```%s\n%s\n```\n", filename, language, string(content))
}

func (h *ChatMessageHandler) resolveFilePath(filename string) (string, error) {
	if filepath.IsAbs(filename) {
		return filename, nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	return filepath.Join(cwd, filename), nil
}

func (h *ChatMessageHandler) readAndValidateFile(fullPath, filename string) ([]byte, error) {
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("file not found: %s", filename)
	}

	content, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, fmt.Errorf("could not read file %s: %v", filename, err)
	}

	const maxFileSize = 50 * 1024 // 50KB
	if len(content) > maxFileSize {
		return nil, fmt.Errorf("file %s is too large (%d bytes). Maximum size is %d bytes", filename, len(content), maxFileSize)
	}

	return content, nil
}

func (h *ChatMessageHandler) getLanguageFromExtension(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))

	languageMap := map[string]string{
		".go":   "go",
		".js":   "javascript",
		".jsx":  "javascript",
		".ts":   "typescript",
		".tsx":  "typescript",
		".py":   "python",
		".java": "java",
		".c":    "c",
		".h":    "c",
		".cpp":  "cpp",
		".cc":   "cpp",
		".cxx":  "cpp",
		".hpp":  "cpp",
		".rs":   "rust",
		".rb":   "ruby",
		".php":  "php",
		".sh":   "bash",
		".sql":  "sql",
		".html": "html",
		".htm":  "html",
		".css":  "css",
		".xml":  "xml",
		".json": "json",
		".yaml": "yaml",
		".yml":  "yaml",
		".md":   "markdown",
	}

	if language, exists := languageMap[ext]; exists {
		return language
	}

	return "text"
}

// performExport performs the export operation in background and returns the result
func (h *ChatMessageHandler) performExport(cmd commands.Command, data interface{}) tea.Cmd {
	return func() tea.Msg {
		exportCmd, ok := cmd.(*commands.ExportCommand)
		if !ok {
			return shared.ShowErrorMsg{
				Error:  "Invalid export command type",
				Sticky: false,
			}
		}

		ctx, ok := data.(context.Context)
		if !ok {
			ctx = context.Background()
		}

		filePath, err := exportCmd.PerformExport(ctx)
		if err != nil {
			return shared.ShowErrorMsg{
				Error:  fmt.Sprintf("Export failed: %v", err),
				Sticky: true,
			}
		}

		return shared.SetStatusMsg{
			Message: fmt.Sprintf("📝 Chat exported to %s", filePath),
			Spinner: false,
		}
	}
}

// handleToolCalls processes tool calls and returns appropriate commands
func (h *ChatMessageHandler) handleToolCalls(msg domain.ChatCompleteEvent, statusMsg, tokenUsage string) (tea.Model, tea.Cmd) {
	if err := h.conversationRepo.UpdateLastMessageToolCalls(&msg.ToolCalls); err != nil {
		if err := h.handleToolCallsUpdateError(msg); err != nil {
			return nil, func() tea.Msg {
				return shared.ShowErrorMsg{
					Error:  fmt.Sprintf("Failed to save assistant message with tool calls: %v", err),
					Sticky: false,
				}
			}
		}
	}

	return h.processToolCallsSequentially(msg.ToolCalls, statusMsg, tokenUsage)
}

// processToolCallsSequentially handles multiple tool calls one by one
func (h *ChatMessageHandler) processToolCallsSequentially(toolCalls []sdk.ChatCompletionMessageToolCall, statusMsg, tokenUsage string) (tea.Model, tea.Cmd) {
	if len(toolCalls) == 0 {
		return nil, func() tea.Msg {
			return shared.SetStatusMsg{
				Message:    statusMsg,
				Spinner:    false,
				TokenUsage: tokenUsage,
			}
		}
	}

	toolCall := toolCalls[0]
	args := h.parseToolArguments(toolCall.Function.Arguments)

	toolCallRequest := ToolCallRequest{
		ID:        toolCall.Id,
		Name:      toolCall.Function.Name,
		Arguments: args,
	}

	return nil, tea.Batch(
		func() tea.Msg {
			return shared.SetStatusMsg{
				Message:    statusMsg,
				Spinner:    false,
				TokenUsage: tokenUsage,
			}
		},
		func() tea.Msg {
			return ToolCallDetectedMsg{
				ToolCall: toolCallRequest,
				Response: fmt.Sprintf("Processing tool call 1 of %d", len(toolCalls)),
			}
		},
		func() tea.Msg {
			return StoreRemainingToolCallsMsg{
				RemainingCalls: toolCalls[1:],
			}
		},
	)
}

// handleToolCallsUpdateError handles the case when updating tool calls fails
func (h *ChatMessageHandler) handleToolCallsUpdateError(msg domain.ChatCompleteEvent) error {
	messages := h.conversationRepo.GetMessages()

	if len(messages) == 0 || messages[len(messages)-1].Message.Role != sdk.Assistant {
		assistantEntry := domain.ConversationEntry{
			Message: sdk.Message{
				Role:      sdk.Assistant,
				Content:   msg.Message,
				ToolCalls: &msg.ToolCalls,
			},
			Model: h.modelService.GetCurrentModel(),
			Time:  time.Now(),
		}

		return h.conversationRepo.AddMessage(assistantEntry)
	}

	return nil
}

// parseToolArguments parses tool function arguments from JSON string
func (h *ChatMessageHandler) parseToolArguments(arguments string) map[string]interface{} {
	args := make(map[string]interface{})
	if arguments != "" {
		if err := json.Unmarshal([]byte(arguments), &args); err != nil {
			args = make(map[string]interface{})
		}
	}
	return args
}

// handleStoreRemainingToolCalls stores remaining tool calls for sequential processing
func (h *ChatMessageHandler) handleStoreRemainingToolCalls(msg StoreRemainingToolCallsMsg, state *AppState) (tea.Model, tea.Cmd) {
	state.Data["remainingToolCalls"] = msg.RemainingCalls
	return nil, nil
}

// handleProcessNextToolCall processes the next tool call in the queue
func (h *ChatMessageHandler) handleProcessNextToolCall(msg ProcessNextToolCallMsg, state *AppState) (tea.Model, tea.Cmd) {
	remainingCalls, ok := state.Data["remainingToolCalls"].([]sdk.ChatCompletionMessageToolCall)
	if !ok || len(remainingCalls) == 0 {
		delete(state.Data, "remainingToolCalls")

		return nil, func() tea.Msg {
			return shared.SetStatusMsg{
				Message: "All tool calls completed, preparing follow-up request...",
				Spinner: true,
			}
		}
	}

	nextCall := remainingCalls[0]
	state.Data["remainingToolCalls"] = remainingCalls[1:]

	args := h.parseToolArguments(nextCall.Function.Arguments)
	toolCallRequest := ToolCallRequest{
		ID:        nextCall.Id,
		Name:      nextCall.Function.Name,
		Arguments: args,
	}

	remainingCount := len(remainingCalls) - 1
	statusMessage := fmt.Sprintf("Processing next tool call (%d remaining)", remainingCount)

	return nil, tea.Batch(
		func() tea.Msg {
			return shared.SetStatusMsg{
				Message: statusMessage,
				Spinner: false,
			}
		},
		func() tea.Msg {
			return ToolCallDetectedMsg{
				ToolCall: toolCallRequest,
				Response: statusMessage,
			}
		},
	)
}

// triggerFollowUpLLMCall sends the conversation with tool results back to the LLM for reasoning
func (h *ChatMessageHandler) triggerFollowUpLLMCall() tea.Cmd {
	return func() tea.Msg {
		messages := h.conversationToSDKMessages()

		return h.startChatCompletion(messages)()
	}
}

// handleTriggerFollowUpLLMCall handles the trigger for follow-up LLM call
func (h *ChatMessageHandler) handleTriggerFollowUpLLMCall(msg TriggerFollowUpLLMCallMsg, state *AppState) (tea.Model, tea.Cmd) {
	return nil, h.triggerFollowUpLLMCall()
}
