package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/container"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/logger"
	"github.com/inference-gateway/sdk"
)

// AG-UI Protocol Event Types
// Ref: https://docs.ag-ui.com/concepts/events

// AGUIEvent is the base interface for all AG-UI events
type AGUIEvent struct {
	Type      string `json:"type"`
	Timestamp int64  `json:"timestamp,omitempty"`
}

// RunStarted signals the start of an agent run
type RunStarted struct {
	Type        string `json:"type"` // "RunStarted"
	ThreadID    string `json:"threadId"`
	RunID       string `json:"runId"`
	ParentRunID string `json:"parentRunId,omitempty"`
	Input       any    `json:"input,omitempty"`
	Timestamp   int64  `json:"timestamp,omitempty"`
}

// RunFinished signals the successful completion of an agent run
type RunFinished struct {
	Type      string `json:"type"` // "RunFinished"
	ThreadID  string `json:"threadId"`
	RunID     string `json:"runId"`
	Result    any    `json:"result,omitempty"`
	Outcome   string `json:"outcome,omitempty"`
	Timestamp int64  `json:"timestamp,omitempty"`
}

// RunError signals an error during an agent run
type RunError struct {
	Type      string `json:"type"` // "RunError"
	Message   string `json:"message"`
	Code      string `json:"code,omitempty"`
	Timestamp int64  `json:"timestamp,omitempty"`
}

// TextMessageStart initializes a new text message in the conversation
type TextMessageStart struct {
	Type      string `json:"type"` // "TextMessageStart"
	MessageID string `json:"messageId"`
	Role      string `json:"role"`
	Timestamp int64  `json:"timestamp,omitempty"`
}

// TextMessageContent delivers incremental parts of message text as available
type TextMessageContent struct {
	Type      string `json:"type"` // "TextMessageContent"
	MessageID string `json:"messageId"`
	Delta     string `json:"delta"`
	Timestamp int64  `json:"timestamp,omitempty"`
}

// TextMessageEnd marks the completion of a streaming text message
type TextMessageEnd struct {
	Type      string `json:"type"` // "TextMessageEnd"
	MessageID string `json:"messageId"`
	Timestamp int64  `json:"timestamp,omitempty"`
}

// ToolCallStart indicates the agent is invoking a tool
type ToolCallStart struct {
	Type            string         `json:"type"`
	ToolCallID      string         `json:"toolCallId"`
	ToolCallName    string         `json:"toolCallName"`
	Arguments       string         `json:"arguments,omitempty"`
	ParentMessageID string         `json:"parentMessageId,omitempty"`
	Timestamp       int64          `json:"timestamp,omitempty"`
	Status          string         `json:"status,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
}

// ToolCallArgs delivers incremental parts of tool argument data
type ToolCallArgs struct {
	Type       string `json:"type"`
	ToolCallID string `json:"toolCallId"`
	Delta      string `json:"delta"`
	Timestamp  int64  `json:"timestamp,omitempty"`
}

// ToolCallEnd marks the completion of a tool call specification
type ToolCallEnd struct {
	Type       string `json:"type"` // "ToolCallEnd"
	ToolCallID string `json:"toolCallId"`
	Timestamp  int64  `json:"timestamp,omitempty"`
}

// ToolCallProgress delivers intermediate progress updates during tool execution
type ToolCallProgress struct {
	Type       string         `json:"type"` // "ToolCallProgress"
	ToolCallID string         `json:"toolCallId"`
	Status     string         `json:"status"`           // Current execution status
	Message    string         `json:"message"`          // Human-readable status message
	Output     string         `json:"output,omitempty"` // Streaming output (for Bash)
	Metadata   map[string]any `json:"metadata,omitempty"`
	Timestamp  int64          `json:"timestamp,omitempty"`
}

// ToolCallResult provides the output/result from executed tool
type ToolCallResult struct {
	Type       string         `json:"type"` // "ToolCallResult"
	MessageID  string         `json:"messageId"`
	ToolCallID string         `json:"toolCallId"`
	Content    any            `json:"content"`
	Role       string         `json:"role,omitempty"`
	Timestamp  int64          `json:"timestamp,omitempty"`
	Status     string         `json:"status,omitempty"`   // Final status: "complete" or "failed"
	Duration   float64        `json:"duration,omitempty"` // Execution time in seconds
	Metadata   map[string]any `json:"metadata,omitempty"` // Additional metadata (e.g., exit code, error details)
}

// ParallelToolsMetadata provides summary information for parallel tool execution
type ParallelToolsMetadata struct {
	Type          string  `json:"type"` // "ParallelToolsMetadata"
	TotalCount    int     `json:"totalCount"`
	SuccessCount  int     `json:"successCount,omitempty"`
	FailureCount  int     `json:"failureCount,omitempty"`
	TotalDuration float64 `json:"totalDuration,omitempty"` // Total execution time in seconds
	Timestamp     int64   `json:"timestamp,omitempty"`
}

// HeadlessInput represents JSON input from UI
type HeadlessInput struct {
	Type    string                   `json:"type"`            // "message", "interrupt", "shutdown"
	Content string                   `json:"content"`         // User message content
	Images  []domain.ImageAttachment `json:"images"`          // Image attachments
	Model   string                   `json:"model,omitempty"` // Optional model to use for this message
}

// toolExecutionState tracks the execution state of a single tool call
type toolExecutionState struct {
	CallID       string
	ToolName     string
	StartTime    time.Time
	Status       string
	OutputBuffer []string
}

// HeadlessHandler handles headless mode communication via stdin/stdout
// Implements AG-UI protocol: https://docs.ag-ui.com
type HeadlessHandler struct {
	sessionID      string
	conversationID string
	services       *container.ServiceContainer
	config         *config.Config
	stdin          io.Reader
	stdout         io.Writer
	ctx            context.Context
	cancel         context.CancelFunc
	currentRunID   string
	toolStates     map[string]*toolExecutionState
	toolStatesMux  sync.RWMutex
}

// NewHeadlessHandler creates a new headless handler
func NewHeadlessHandler(sessionID string, conversationID string, services *container.ServiceContainer, cfg *config.Config) *HeadlessHandler {
	if sessionID == "" {
		sessionID = uuid.New().String()
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &HeadlessHandler{
		sessionID:      sessionID,
		conversationID: conversationID,
		services:       services,
		config:         cfg,
		stdin:          os.Stdin,
		stdout:         os.Stdout,
		ctx:            ctx,
		cancel:         cancel,
	}
}

// Start begins the headless session
func (h *HeadlessHandler) Start() error {
	if err := h.services.GetGatewayManager().EnsureStarted(); err != nil {
		logger.Error("Failed to start gateway", "error", err)
		h.emitRunError(fmt.Sprintf("failed to start gateway: %v", err), "GATEWAY_START_FAILED")
		return err
	}

	ctx, cancel := context.WithTimeout(h.ctx, time.Duration(h.config.Gateway.Timeout)*time.Second)
	defer cancel()

	models, err := h.services.GetModelService().ListModels(ctx)
	if err != nil {
		h.emitRunError(fmt.Sprintf("inference gateway is not available: %v", err), "GATEWAY_UNAVAILABLE")
		return fmt.Errorf("inference gateway is not available: %w", err)
	}

	if len(models) == 0 {
		h.emitRunError("no models available from inference gateway", "NO_MODELS")
		return fmt.Errorf("no models available from inference gateway")
	}

	defaultModel := h.config.Agent.Model
	if defaultModel == "" || !contains(models, defaultModel) {
		defaultModel = models[0]
	}

	if err := h.services.GetModelService().SelectModel(defaultModel); err != nil {
		h.emitRunError(fmt.Sprintf("failed to set model: %v", err), "MODEL_SELECT_FAILED")
		return fmt.Errorf("failed to set model: %w", err)
	}

	conversationRepo := h.services.GetConversationRepository()
	h.loadExistingConversation(conversationRepo)

	h.emitEvent(RunStarted{
		Type:      "RunStarted",
		ThreadID:  h.conversationID,
		RunID:     h.sessionID,
		Timestamp: time.Now().UnixMilli(),
	})

	return h.readLoop()
}

// readLoop continuously reads JSON input from stdin
func (h *HeadlessHandler) readLoop() error {
	scanner := bufio.NewScanner(h.stdin)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024) // 1MB initial, 10MB max for large images

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var input HeadlessInput
		if err := json.Unmarshal(line, &input); err != nil {
			logger.Error("Failed to parse JSON input", "error", err, "line", string(line))
			h.emitRunError(fmt.Sprintf("invalid JSON input: %v", err), "INVALID_INPUT")
			continue
		}

		if err := h.processInput(input); err != nil {
			if err == io.EOF {
				return nil
			}
			logger.Error("Failed to process input", "error", err, "input", input)
			h.emitRunError(fmt.Sprintf("failed to process input: %v", err), "PROCESSING_FAILED")
		}
	}

	if err := scanner.Err(); err != nil {
		logger.Error("Error reading from stdin", "error", err)
		return fmt.Errorf("error reading from stdin: %w", err)
	}

	return nil
}

// processInput handles a single input message
func (h *HeadlessHandler) processInput(input HeadlessInput) error {
	switch input.Type {
	case "message":
		return h.handleMessage(input.Content, input.Images, input.Model)
	case "interrupt":
		return h.handleInterrupt()
	case "shutdown":
		h.cancel()
		return io.EOF // Signal clean shutdown
	default:
		return fmt.Errorf("unknown input type: %s", input.Type)
	}
}

// handleMessage processes a user message
func (h *HeadlessHandler) handleMessage(content string, images []domain.ImageAttachment, model string) error {
	if content == "" {
		return fmt.Errorf("empty message content")
	}

	if model != "" {
		modelService := h.services.GetModelService()
		if err := modelService.SelectModel(model); err != nil {
			h.emitRunError(fmt.Sprintf("Failed to select model '%s': %v", model, err), "MODEL_SELECT_FAILED")
			return fmt.Errorf("failed to select model: %w", err)
		}
		logger.Info("Model selected for headless session", "model", model, "session", h.sessionID)
	}

	h.currentRunID = uuid.New().String()

	userMessageID := uuid.New().String()
	h.emitEvent(TextMessageStart{
		Type:      "TextMessageStart",
		MessageID: userMessageID,
		Role:      "user",
		Timestamp: time.Now().UnixMilli(),
	})
	h.emitEvent(TextMessageContent{
		Type:      "TextMessageContent",
		MessageID: userMessageID,
		Delta:     content,
		Timestamp: time.Now().UnixMilli(),
	})
	h.emitEvent(TextMessageEnd{
		Type:      "TextMessageEnd",
		MessageID: userMessageID,
		Timestamp: time.Now().UnixMilli(),
	})

	h.emitEvent(RunStarted{
		Type:     "RunStarted",
		ThreadID: h.conversationID,
		RunID:    h.currentRunID,
		Input: map[string]any{
			"content": content,
			"images":  len(images),
		},
		Timestamp: time.Now().UnixMilli(),
	})

	var userMessage sdk.Message

	if len(images) > 0 {
		contentParts := []sdk.ContentPart{}

		var textPart sdk.ContentPart
		if err := textPart.FromTextContentPart(sdk.TextContentPart{
			Type: sdk.Text,
			Text: content,
		}); err != nil {
			h.emitRunError(fmt.Sprintf("failed to create text content part: %v", err), "CONTENT_PART_FAILED")
			return fmt.Errorf("failed to create text content part: %w", err)
		}
		contentParts = append(contentParts, textPart)

		for _, img := range images {
			dataURL := fmt.Sprintf("data:%s;base64,%s", img.MimeType, img.Data)
			imagePart, err := sdk.NewImageContentPart(dataURL, nil)
			if err != nil {
				h.emitRunError(fmt.Sprintf("failed to create image content part: %v", err), "IMAGE_PART_FAILED")
				return fmt.Errorf("failed to create image content part: %w", err)
			}
			contentParts = append(contentParts, imagePart)
		}

		userMessage = sdk.Message{
			Role:    sdk.User,
			Content: sdk.NewMessageContent(contentParts),
		}
	} else {
		userMessage = sdk.Message{
			Role:    sdk.User,
			Content: sdk.NewMessageContent(content),
		}
	}

	conversationRepo := h.services.GetConversationRepository()

	wasNewConversation := h.conversationID == ""

	if err := conversationRepo.AddMessage(domain.ConversationEntry{
		Message: userMessage,
		Time:    time.Now(),
	}); err != nil {
		logger.Warn("Failed to add user message to conversation", "error", err)
	}

	if wasNewConversation {
		if persistentRepo, ok := conversationRepo.(interface {
			GetCurrentConversationID() string
		}); ok {
			h.conversationID = persistentRepo.GetCurrentConversationID()
			if h.conversationID != "" {
				h.emitEvent(map[string]any{
					"type":            "ConversationCreated",
					"conversation_id": h.conversationID,
					"timestamp":       time.Now().UnixMilli(),
				})
			}
		}
	}

	entries := conversationRepo.GetMessages()
	messages := make([]sdk.Message, len(entries))
	for i, entry := range entries {
		messages[i] = entry.Message
	}

	req := &domain.AgentRequest{
		RequestID: fmt.Sprintf("req_%d", time.Now().UnixNano()),
		Model:     h.services.GetModelService().GetCurrentModel(),
		Messages:  messages,
	}

	ctx, cancel := context.WithCancel(h.ctx)
	defer cancel()

	agentService := h.services.GetAgentService()
	events, err := agentService.RunWithStream(ctx, req)
	if err != nil {
		h.emitRunError(fmt.Sprintf("failed to start chat: %v", err), "CHAT_START_FAILED")
		return fmt.Errorf("failed to start chat: %w", err)
	}

	return h.processStreamingEvents(events)
}

// processStreamingEvents handles streaming chat events
func (h *HeadlessHandler) processStreamingEvents(events <-chan domain.ChatEvent) error {
	var messageID string
	var tokenStats map[string]int

	for {
		select {
		case <-h.ctx.Done():
			return h.ctx.Err()
		case event, ok := <-events:
			if !ok {
				return h.handleStreamComplete(messageID, tokenStats)
			}

			var err error
			messageID, tokenStats, err = h.handleEvent(event, messageID, tokenStats)
			if err != nil {
				return err
			}
		}
	}
}

func (h *HeadlessHandler) handleStreamComplete(messageID string, tokenStats map[string]int) error {
	h.emitEvent(RunFinished{
		Type:     "RunFinished",
		ThreadID: h.conversationID,
		RunID:    h.currentRunID,
		Result: map[string]any{
			"message_id": messageID,
			"tokens":     tokenStats,
		},
		Outcome:   "success",
		Timestamp: time.Now().UnixMilli(),
	})
	return nil
}

func (h *HeadlessHandler) handleEvent(event domain.ChatEvent, messageID string, tokenStats map[string]int) (string, map[string]int, error) {
	switch e := event.(type) {
	case domain.ChatChunkEvent:
		return h.handleChatChunk(e, messageID), tokenStats, nil
	case domain.ChatCompleteEvent:
		return h.handleChatComplete(e, messageID)
	case domain.ChatErrorEvent:
		return messageID, tokenStats, h.handleChatError(e)
	case domain.ToolCallReadyEvent:
		return h.handleToolCallReady(e, messageID), tokenStats, nil
	case domain.ParallelToolsStartEvent:
		return h.handleParallelToolsStart(e, messageID), tokenStats, nil
	case domain.ToolExecutionProgressEvent:
		h.handleToolExecutionProgress(e)
		return messageID, tokenStats, nil
	case domain.BashOutputChunkEvent:
		h.handleBashOutputChunk(e)
		return messageID, tokenStats, nil
	case domain.ParallelToolsCompleteEvent:
		h.handleParallelToolsComplete(e)
		return messageID, tokenStats, nil
	case domain.ToolApprovalRequestedEvent:
		h.handleToolApprovalRequested(e, messageID)
		return messageID, tokenStats, nil
	default:
		return messageID, tokenStats, nil
	}
}

func (h *HeadlessHandler) handleChatChunk(e domain.ChatChunkEvent, messageID string) string {
	if e.Content == "" {
		return messageID
	}

	if messageID == "" {
		messageID = uuid.New().String()
		h.emitEvent(TextMessageStart{
			Type:      "TextMessageStart",
			MessageID: messageID,
			Role:      "assistant",
			Timestamp: time.Now().UnixMilli(),
		})
	}

	h.emitEvent(TextMessageContent{
		Type:      "TextMessageContent",
		MessageID: messageID,
		Delta:     e.Content,
		Timestamp: time.Now().UnixMilli(),
	})

	return messageID
}

func (h *HeadlessHandler) handleChatComplete(e domain.ChatCompleteEvent, messageID string) (string, map[string]int, error) {
	if messageID != "" {
		h.emitEvent(TextMessageEnd{
			Type:      "TextMessageEnd",
			MessageID: messageID,
			Timestamp: time.Now().UnixMilli(),
		})
		messageID = ""
	}

	var tokenStats map[string]int
	if e.Metrics != nil && e.Metrics.Usage != nil {
		tokenStats = map[string]int{
			"input_tokens":  int(e.Metrics.Usage.PromptTokens),
			"output_tokens": int(e.Metrics.Usage.CompletionTokens),
			"total_tokens":  int(e.Metrics.Usage.PromptTokens + e.Metrics.Usage.CompletionTokens),
		}
	}

	return messageID, tokenStats, nil
}

func (h *HeadlessHandler) handleChatError(e domain.ChatErrorEvent) error {
	h.emitRunError(fmt.Sprintf("chat error: %v", e.Error), "CHAT_ERROR")
	return fmt.Errorf("chat error: %v", e.Error)
}

func (h *HeadlessHandler) handleToolCallReady(e domain.ToolCallReadyEvent, messageID string) string {
	if messageID != "" {
		h.emitEvent(TextMessageEnd{
			Type:      "TextMessageEnd",
			MessageID: messageID,
			Timestamp: time.Now().UnixMilli(),
		})
		messageID = ""
	}

	for _, toolCall := range e.ToolCalls {
		h.emitToolCallEvents(toolCall.Id, toolCall.Function.Name, toolCall.Function.Arguments, messageID)
	}

	return messageID
}

func (h *HeadlessHandler) handleParallelToolsStart(e domain.ParallelToolsStartEvent, messageID string) string {
	if messageID != "" {
		h.emitEvent(TextMessageEnd{
			Type:      "TextMessageEnd",
			MessageID: messageID,
			Timestamp: time.Now().UnixMilli(),
		})
		messageID = ""
	}

	for _, tool := range e.Tools {
		h.trackToolExecution(tool.CallID, tool.Name)
		h.emitEvent(ToolCallStart{
			Type:            "ToolCallStart",
			ToolCallID:      tool.CallID,
			ToolCallName:    tool.Name,
			Arguments:       tool.Arguments,
			ParentMessageID: messageID,
			Status:          "queued",
			Timestamp:       time.Now().UnixMilli(),
		})
	}

	return messageID
}

func (h *HeadlessHandler) handleToolExecutionProgress(e domain.ToolExecutionProgressEvent) {
	state := h.updateToolStatus(e.ToolCallID, e.Status)
	aguiStatus := mapStatusToAGUI(e.Status)

	h.emitEvent(ToolCallProgress{
		Type:       "ToolCallProgress",
		ToolCallID: e.ToolCallID,
		Status:     aguiStatus,
		Message:    e.Message,
		Timestamp:  time.Now().UnixMilli(),
	})

	if e.Status == "complete" || e.Status == "failed" {
		h.emitToolResult(e, state, aguiStatus)
	}
}

func (h *HeadlessHandler) emitToolResult(e domain.ToolExecutionProgressEvent, state *toolExecutionState, aguiStatus string) {
	if state == nil {
		return
	}

	duration := time.Since(state.StartTime).Seconds()
	content := e.Result
	if content == "" {
		content = e.Message
	}

	h.emitEvent(ToolCallResult{
		Type:       "ToolCallResult",
		MessageID:  uuid.New().String(),
		ToolCallID: e.ToolCallID,
		Content:    content,
		Role:       "tool",
		Status:     aguiStatus,
		Duration:   duration,
		Timestamp:  time.Now().UnixMilli(),
	})

	h.removeToolState(e.ToolCallID)
}

func (h *HeadlessHandler) handleBashOutputChunk(e domain.BashOutputChunkEvent) {
	h.emitEvent(ToolCallProgress{
		Type:       "ToolCallProgress",
		ToolCallID: e.ToolCallID,
		Status:     "running",
		Message:    "Streaming output...",
		Output:     e.Output,
		Metadata: map[string]any{
			"isComplete": e.IsComplete,
		},
		Timestamp: time.Now().UnixMilli(),
	})
}

func (h *HeadlessHandler) handleParallelToolsComplete(e domain.ParallelToolsCompleteEvent) {
	h.emitEvent(ParallelToolsMetadata{
		Type:          "ParallelToolsMetadata",
		TotalCount:    e.TotalExecuted,
		SuccessCount:  e.SuccessCount,
		FailureCount:  e.FailureCount,
		TotalDuration: e.Duration.Seconds(),
		Timestamp:     time.Now().UnixMilli(),
	})
}

func (h *HeadlessHandler) handleToolApprovalRequested(e domain.ToolApprovalRequestedEvent, messageID string) {
	h.emitEvent(ToolCallStart{
		Type:            "ToolCallStart",
		ToolCallID:      e.ToolCall.Id,
		ToolCallName:    e.ToolCall.Function.Name,
		Arguments:       e.ToolCall.Function.Arguments,
		ParentMessageID: messageID,
		Timestamp:       time.Now().UnixMilli(),
	})
}

func (h *HeadlessHandler) emitToolCallEvents(toolCallID, toolName, arguments, messageID string) {
	h.trackToolExecution(toolCallID, toolName)

	h.emitEvent(ToolCallStart{
		Type:            "ToolCallStart",
		ToolCallID:      toolCallID,
		ToolCallName:    toolName,
		Arguments:       arguments,
		ParentMessageID: messageID,
		Status:          "queued",
		Timestamp:       time.Now().UnixMilli(),
	})

	h.emitEvent(ToolCallArgs{
		Type:       "ToolCallArgs",
		ToolCallID: toolCallID,
		Delta:      arguments,
		Timestamp:  time.Now().UnixMilli(),
	})

	h.emitEvent(ToolCallEnd{
		Type:       "ToolCallEnd",
		ToolCallID: toolCallID,
		Timestamp:  time.Now().UnixMilli(),
	})
}

// handleInterrupt handles interruption signal
func (h *HeadlessHandler) handleInterrupt() error {
	h.cancel()
	h.emitRunError("Chat interrupted by user", "INTERRUPTED")
	return nil
}

// emitEvent sends an AG-UI protocol event to stdout
func (h *HeadlessHandler) emitEvent(event any) {
	jsonData, err := json.Marshal(event)
	if err != nil {
		logger.Error("Failed to marshal event", "error", err, "event", event)
		return
	}

	if _, err := fmt.Fprintf(h.stdout, "%s\n", jsonData); err != nil {
		logger.Error("Failed to write to stdout", "error", err)
	}
}

// emitRunError sends a RunError event
func (h *HeadlessHandler) emitRunError(message string, code string) {
	h.emitEvent(RunError{
		Type:      "RunError",
		Message:   message,
		Code:      code,
		Timestamp: time.Now().UnixMilli(),
	})
}

// Shutdown cleanly shuts down the headless handler
func (h *HeadlessHandler) Shutdown() error {
	h.cancel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return h.services.Shutdown(ctx)
}

// contains checks if a slice contains a string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// loadExistingConversation attempts to load a conversation from storage if conversationID is set
func (h *HeadlessHandler) loadExistingConversation(conversationRepo domain.ConversationRepository) {
	if h.conversationID == "" {
		return
	}

	ctx, cancel := context.WithTimeout(h.ctx, 30*time.Second)
	defer cancel()

	persistentRepo, ok := conversationRepo.(interface {
		LoadConversation(ctx context.Context, conversationID string) error
	})
	if !ok {
		logger.Warn("ConversationRepository does not support LoadConversation, will create new conversation on first message")
		h.conversationID = ""
		return
	}

	logger.Info("💾 Attempting to load conversation from storage...")
	if err := persistentRepo.LoadConversation(ctx, h.conversationID); err != nil {
		logger.Warn("Failed to load conversation, will create new one on first message", "conversation_id", h.conversationID, "error", err)
		h.conversationID = ""
	}
}

// trackToolExecution initializes state tracking for a tool call
func (h *HeadlessHandler) trackToolExecution(callID, toolName string) {
	h.toolStatesMux.Lock()
	defer h.toolStatesMux.Unlock()

	if h.toolStates == nil {
		h.toolStates = make(map[string]*toolExecutionState)
	}

	h.toolStates[callID] = &toolExecutionState{
		CallID:       callID,
		ToolName:     toolName,
		StartTime:    time.Now(),
		Status:       "queued",
		OutputBuffer: []string{},
	}
}

// getToolState retrieves tool execution state (thread-safe read)
func (h *HeadlessHandler) getToolState(callID string) *toolExecutionState {
	h.toolStatesMux.RLock()
	defer h.toolStatesMux.RUnlock()
	return h.toolStates[callID]
}

// updateToolStatus updates tool status and returns updated state
func (h *HeadlessHandler) updateToolStatus(callID, status string) *toolExecutionState {
	h.toolStatesMux.Lock()
	defer h.toolStatesMux.Unlock()

	if state, exists := h.toolStates[callID]; exists {
		state.Status = status
		return state
	}
	return nil
}

// removeToolState cleans up tool state after completion
func (h *HeadlessHandler) removeToolState(callID string) {
	h.toolStatesMux.Lock()
	defer h.toolStatesMux.Unlock()
	delete(h.toolStates, callID)
}

// mapStatusToAGUI maps TUI status values to AG-UI protocol status
func mapStatusToAGUI(status string) string {
	switch status {
	case "queued", "ready":
		return "queued"
	case "running", "starting", "saving", "executing", "streaming":
		return "running"
	case "complete", "completed", "executed":
		return "complete"
	case "error", "failed":
		return "failed"
	default:
		return status
	}
}
