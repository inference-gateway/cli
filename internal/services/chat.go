package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
)

// StreamingChatService implements ChatService using streaming SDK client
type StreamingChatService struct {
	baseURL        string
	apiKey         string
	timeoutSeconds int
	client         sdk.Client
	toolService    domain.ToolService

	// Request tracking
	activeRequests map[string]context.CancelFunc
	requestsMux    sync.RWMutex

	// Metrics tracking
	metrics    map[string]*domain.ChatMetrics
	metricsMux sync.RWMutex
}

// NewStreamingChatService creates a new streaming chat service
func NewStreamingChatService(baseURL, apiKey string, timeoutSeconds int, toolService domain.ToolService) *StreamingChatService {
	if !strings.HasSuffix(baseURL, "/v1") {
		baseURL = strings.TrimSuffix(baseURL, "/") + "/v1"
	}

	client := sdk.NewClient(&sdk.ClientOptions{
		BaseURL: baseURL,
		APIKey:  apiKey,
	})

	return &StreamingChatService{
		baseURL:        baseURL,
		apiKey:         apiKey,
		timeoutSeconds: timeoutSeconds,
		client:         client,
		toolService:    toolService,
		activeRequests: make(map[string]context.CancelFunc),
		metrics:        make(map[string]*domain.ChatMetrics),
	}
}

func (s *StreamingChatService) SendMessage(ctx context.Context, model string, messages []sdk.Message) (<-chan domain.ChatEvent, error) {
	if err := s.validateSendMessageParams(model, messages); err != nil {
		return nil, err
	}

	messages = s.addToolsIfAvailable(messages)
	requestID := generateRequestID()
	timeoutCtx, cancel := s.setupRequest(ctx, requestID)
	events := make(chan domain.ChatEvent, 100)

	go s.processStreamingRequest(timeoutCtx, cancel, requestID, model, messages, events)

	return events, nil
}

func (s *StreamingChatService) validateSendMessageParams(model string, messages []sdk.Message) error {
	if len(messages) == 0 {
		return fmt.Errorf("no messages provided")
	}
	if model == "" {
		return fmt.Errorf("no model specified")
	}
	return nil
}

func (s *StreamingChatService) addToolsIfAvailable(messages []sdk.Message) []sdk.Message {
	if s.toolService != nil {
		availableTools := s.toolService.ListTools()
		if len(availableTools) > 0 {
			toolsMessage := s.createToolsSystemMessage(availableTools)
			messages = append([]sdk.Message{toolsMessage}, messages...)
		}
	}
	return messages
}

func (s *StreamingChatService) setupRequest(ctx context.Context, requestID string) (context.Context, context.CancelFunc) {
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(s.timeoutSeconds)*time.Second)

	s.requestsMux.Lock()
	s.activeRequests[requestID] = cancel
	s.requestsMux.Unlock()

	return timeoutCtx, cancel
}

func (s *StreamingChatService) processStreamingRequest(timeoutCtx context.Context, cancel context.CancelFunc, requestID, model string, messages []sdk.Message, events chan<- domain.ChatEvent) {
	defer close(events)
	defer cancel()
	defer s.cleanupRequest(requestID)

	startTime := time.Now()
	s.sendStartEvent(events, requestID, startTime)
	s.initializeMetrics(requestID)

	stream, err := s.createContentStream(timeoutCtx, model, messages)
	if err != nil {
		s.sendErrorEvent(events, requestID, err)
		return
	}

	s.processEventStream(timeoutCtx, stream, events, requestID, startTime)
}

func (s *StreamingChatService) cleanupRequest(requestID string) {
	s.requestsMux.Lock()
	delete(s.activeRequests, requestID)
	s.requestsMux.Unlock()
}

func (s *StreamingChatService) sendStartEvent(events chan<- domain.ChatEvent, requestID string, startTime time.Time) {
	events <- domain.ChatStartEvent{
		RequestID: requestID,
		Timestamp: startTime,
	}
}

func (s *StreamingChatService) initializeMetrics(requestID string) {
	s.metricsMux.Lock()
	s.metrics[requestID] = &domain.ChatMetrics{
		Duration: 0,
		Usage:    nil,
	}
	s.metricsMux.Unlock()
}

func (s *StreamingChatService) createContentStream(timeoutCtx context.Context, model string, messages []sdk.Message) (<-chan sdk.SSEvent, error) {
	provider, modelName, err := s.parseProvider(model)
	if err != nil {
		return nil, fmt.Errorf("failed to parse provider from model '%s': %w", model, err)
	}

	providerType := sdk.Provider(provider)
	stream, err := s.client.GenerateContentStream(timeoutCtx, providerType, modelName, messages)
	if err != nil {
		return nil, fmt.Errorf("failed to generate content stream: %w", err)
	}

	return stream, nil
}

func (s *StreamingChatService) sendErrorEvent(events chan<- domain.ChatEvent, requestID string, err error) {
	events <- domain.ChatErrorEvent{
		RequestID: requestID,
		Timestamp: time.Now(),
		Error:     err,
	}
}

func (s *StreamingChatService) processEventStream(timeoutCtx context.Context, stream <-chan sdk.SSEvent, events chan<- domain.ChatEvent, requestID string, startTime time.Time) {
	var fullMessage strings.Builder
	var toolCalls []sdk.ChatCompletionMessageToolCall
	var usage *sdk.CompletionUsage

	for {
		select {
		case <-timeoutCtx.Done():
			s.handleTimeout(events, requestID, timeoutCtx)
			return

		case event, ok := <-stream:
			if !ok {
				s.sendCompleteEvent(events, requestID, startTime, fullMessage.String(), toolCalls, usage)
				return
			}

			if event.Event == nil {
				continue
			}

			if s.handleStreamEvent(event, events, requestID, &fullMessage, &toolCalls, &usage) {
				return
			}
		}
	}
}

func (s *StreamingChatService) handleTimeout(events chan<- domain.ChatEvent, requestID string, timeoutCtx context.Context) {
	var errorMsg string
	if timeoutCtx.Err() == context.DeadlineExceeded {
		errorMsg = fmt.Sprintf("request timed out after %d seconds", s.timeoutSeconds)
	} else {
		errorMsg = "request cancelled by user"
	}

	events <- domain.ChatErrorEvent{
		RequestID: requestID,
		Timestamp: time.Now(),
		Error:     errors.New(errorMsg),
	}
}

func (s *StreamingChatService) sendCompleteEvent(events chan<- domain.ChatEvent, requestID string, startTime time.Time, message string, toolCalls []sdk.ChatCompletionMessageToolCall, usage *sdk.CompletionUsage) {
	duration := time.Since(startTime)

	s.metricsMux.Lock()
	if metrics, exists := s.metrics[requestID]; exists {
		metrics.Duration = duration
		metrics.Usage = usage
	}
	s.metricsMux.Unlock()

	events <- domain.ChatCompleteEvent{
		RequestID: requestID,
		Timestamp: time.Now(),
		Message:   message,
		ToolCalls: toolCalls,
		Metrics:   s.metrics[requestID],
	}
}

func (s *StreamingChatService) handleStreamEvent(event sdk.SSEvent, events chan<- domain.ChatEvent, requestID string, fullMessage *strings.Builder, toolCalls *[]sdk.ChatCompletionMessageToolCall, usage **sdk.CompletionUsage) bool {
	switch *event.Event {
	case sdk.ContentDelta:
		s.handleContentDelta(event, events, requestID, fullMessage, toolCalls, usage)
		return false

	case sdk.StreamEnd:
		return false

	case "error":
		s.handleStreamError(event, events, requestID)
		return true
	}

	return false
}

func (s *StreamingChatService) handleContentDelta(event sdk.SSEvent, events chan<- domain.ChatEvent, requestID string, fullMessage *strings.Builder, toolCalls *[]sdk.ChatCompletionMessageToolCall, usage **sdk.CompletionUsage) {
	chunk, toolCallsChunk, usageChunk := s.processContentDelta(event)
	if chunk != "" {
		fullMessage.WriteString(chunk)
		events <- domain.ChatChunkEvent{
			RequestID: requestID,
			Timestamp: time.Now(),
			Content:   chunk,
			ToolCalls: toolCallsChunk,
			Delta:     true,
		}
	}
	if len(toolCallsChunk) > 0 {
		*toolCalls = append(*toolCalls, toolCallsChunk...)
	}
	if usageChunk != nil {
		*usage = usageChunk
	}
}

func (s *StreamingChatService) handleStreamError(event sdk.SSEvent, events chan<- domain.ChatEvent, requestID string) {
	var errResp struct {
		Error string `json:"error"`
	}
	if event.Data != nil {
		_ = json.Unmarshal(*event.Data, &errResp)
	}
	events <- domain.ChatErrorEvent{
		RequestID: requestID,
		Timestamp: time.Now(),
		Error:     fmt.Errorf("stream error: %s", errResp.Error),
	}
}

func (s *StreamingChatService) CancelRequest(requestID string) error {
	s.requestsMux.RLock()
	cancel, exists := s.activeRequests[requestID]
	s.requestsMux.RUnlock()

	if !exists {
		return fmt.Errorf("request %s not found or already completed", requestID)
	}

	cancel()
	return nil
}

func (s *StreamingChatService) GetMetrics(requestID string) *domain.ChatMetrics {
	s.metricsMux.RLock()
	defer s.metricsMux.RUnlock()

	if metrics, exists := s.metrics[requestID]; exists {
		return &domain.ChatMetrics{
			Duration: metrics.Duration,
			Usage:    metrics.Usage,
		}
	}
	return nil
}

// parseProvider parses provider and model name from model string
func (s *StreamingChatService) parseProvider(model string) (string, string, error) {
	parts := strings.SplitN(model, "/", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid model format, expected 'provider/model'")
	}
	return parts[0], parts[1], nil
}

// processContentDelta processes a content delta event
func (s *StreamingChatService) processContentDelta(event sdk.SSEvent) (string, []sdk.ChatCompletionMessageToolCall, *sdk.CompletionUsage) {
	if event.Data == nil {
		return "", nil, nil
	}

	var streamResponse sdk.CreateChatCompletionStreamResponse
	if err := json.Unmarshal(*event.Data, &streamResponse); err != nil {
		return "", nil, nil
	}

	var content string
	var toolCalls []sdk.ChatCompletionMessageToolCall

	for _, choice := range streamResponse.Choices {
		if choice.Delta.Content != "" {
			content += choice.Delta.Content
		}

		for _, deltaToolCall := range choice.Delta.ToolCalls {
			toolCall := sdk.ChatCompletionMessageToolCall{
				Id:   deltaToolCall.ID,
				Type: sdk.ChatCompletionToolType(deltaToolCall.Type),
				Function: sdk.ChatCompletionMessageToolCallFunction{
					Name:      deltaToolCall.Function.Name,
					Arguments: deltaToolCall.Function.Arguments,
				},
			}
			toolCalls = append(toolCalls, toolCall)
		}
	}

	return content, toolCalls, streamResponse.Usage
}

// createToolsSystemMessage creates a system message describing available tools
func (s *StreamingChatService) createToolsSystemMessage(tools []domain.ToolDefinition) sdk.Message {
	var toolDescriptions []string

	toolDescriptions = append(toolDescriptions, "You have access to the following tools:")

	for _, tool := range tools {
		description := s.formatToolDescription(tool)
		toolDescriptions = append(toolDescriptions, description)
	}

	toolDescriptions = append(toolDescriptions, "\nTo use a tool, format your response as: `TOOL_CALL: {\"name\": \"tool_name\", \"arguments\": {\"param\": \"value\"}}`")

	return sdk.Message{
		Role:    sdk.System,
		Content: strings.Join(toolDescriptions, ""),
	}
}

func (s *StreamingChatService) formatToolDescription(tool domain.ToolDefinition) string {
	description := fmt.Sprintf("\n- **%s**: %s", tool.Name, tool.Description)

	if tool.Parameters != nil {
		parameterDescription := s.formatToolParameters(tool.Parameters)
		if parameterDescription != "" {
			description += parameterDescription
		}
	}

	return description
}

func (s *StreamingChatService) formatToolParameters(parameters interface{}) string {
	paramMap, ok := parameters.(map[string]interface{})
	if !ok {
		return ""
	}

	props, ok := paramMap["properties"].(map[string]interface{})
	if !ok {
		return ""
	}

	description := "\n  Parameters:"
	for paramName, paramInfo := range props {
		paramDesc := s.extractParameterDescription(paramInfo)
		if paramDesc != "" {
			description += fmt.Sprintf("\n    - %s: %s", paramName, paramDesc)
		}
	}

	return description
}

func (s *StreamingChatService) extractParameterDescription(paramInfo interface{}) string {
	paramInfoMap, ok := paramInfo.(map[string]interface{})
	if !ok {
		return ""
	}

	desc, ok := paramInfoMap["description"].(string)
	if !ok {
		return ""
	}

	return desc
}

// generateRequestID generates a unique request ID
func generateRequestID() string {
	return fmt.Sprintf("req_%d", time.Now().UnixNano())
}
