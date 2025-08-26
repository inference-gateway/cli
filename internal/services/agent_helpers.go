package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
)

// validateRequest validates the agent request
func (s *AgentServiceImpl) validateRequest(req *domain.AgentRequest) error {
	if len(req.Messages) == 0 {
		return fmt.Errorf("no messages provided")
	}
	if req.Model == "" {
		return fmt.Errorf("no model specified")
	}
	return nil
}

// addToolsIfAvailable adds system prompt and returns messages
func (s *AgentServiceImpl) addToolsIfAvailable(messages []sdk.Message) []sdk.Message {
	var systemMessages []sdk.Message

	if s.systemPrompt != "" {
		currentTime := time.Now().Format("Monday, January 2, 2006 at 3:04 PM MST")
		systemPromptWithTime := fmt.Sprintf("%s\n\nCurrent date and time: %s", s.systemPrompt, currentTime)

		systemMessages = append(systemMessages, sdk.Message{
			Role:    sdk.System,
			Content: systemPromptWithTime,
		})
	}

	if len(systemMessages) > 0 {
		messages = append(systemMessages, messages...)
	}
	return messages
}

// convertToSDKTools converts tool service tools to SDK tools
func (s *AgentServiceImpl) convertToSDKTools() *[]sdk.ChatCompletionTool {
	if s.toolService == nil {
		return nil
	}

	availableTools := s.toolService.ListTools()
	if len(availableTools) == 0 {
		return nil
	}

	sdkTools := make([]sdk.ChatCompletionTool, len(availableTools))
	for i, tool := range availableTools {
		description := tool.Description

		var parameters *sdk.FunctionParameters
		if tool.Parameters != nil {
			if paramMap, ok := tool.Parameters.(map[string]any); ok {
				fp := sdk.FunctionParameters(paramMap)
				parameters = &fp
			}
		}

		sdkTools[i] = sdk.ChatCompletionTool{
			Type: sdk.Function,
			Function: sdk.FunctionObject{
				Name:        tool.Name,
				Description: &description,
				Parameters:  parameters,
			},
		}
	}

	return &sdkTools
}

// setupRequest sets up request tracking with timeout
func (s *AgentServiceImpl) setupRequest(ctx context.Context, requestID string) (context.Context, context.CancelFunc) {
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(s.timeoutSeconds)*time.Second)

	s.requestsMux.Lock()
	s.activeRequests[requestID] = cancel
	s.requestsMux.Unlock()

	return timeoutCtx, cancel
}

// generateContentSync generates content synchronously
func (s *AgentServiceImpl) generateContentSync(timeoutCtx context.Context, model string, messages []sdk.Message) (*sdk.CreateChatCompletionResponse, error) {
	provider, modelName, err := s.parseProvider(model)
	if err != nil {
		return nil, fmt.Errorf("failed to parse provider from model '%s': %w", model, err)
	}

	providerType := sdk.Provider(provider)

	clientWithTools := s.client
	if tools := s.convertToSDKTools(); tools != nil {
		clientWithTools = s.client.WithTools(tools)
	}

	options := &sdk.CreateChatCompletionRequest{
		MaxTokens: &s.maxTokens,
	}

	response, err := clientWithTools.
		WithOptions(options).
		WithMiddlewareOptions(&sdk.MiddlewareOptions{
			SkipMCP: true,
			SkipA2A: true,
		}).
		GenerateContent(timeoutCtx, providerType, modelName, messages)
	if err != nil {
		return nil, fmt.Errorf("failed to generate content: %w", err)
	}

	return response, nil
}

// parseProvider parses provider and model name from model string
func (s *AgentServiceImpl) parseProvider(model string) (string, string, error) {
	parts := strings.SplitN(model, "/", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid model format, expected 'provider/model'")
	}

	provider := parts[0]
	modelName := parts[1]

	// Handle Google's model format: google/models/gemini-2.5-flash -> google, gemini-2.5-flash
	if provider == "google" && strings.HasPrefix(modelName, "models/") {
		modelName = strings.TrimPrefix(modelName, "models/")
	}

	return provider, modelName, nil
}

// processStreamingRequest processes a streaming request
func (s *AgentServiceImpl) processStreamingRequest(timeoutCtx context.Context, cancel context.CancelFunc, requestID, model string, messages []sdk.Message, events chan<- domain.ChatEvent) {
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

// cleanupRequest removes request from tracking
func (s *AgentServiceImpl) cleanupRequest(requestID string) {
	s.requestsMux.Lock()
	delete(s.activeRequests, requestID)
	s.requestsMux.Unlock()
}

// sendStartEvent sends a chat start event
func (s *AgentServiceImpl) sendStartEvent(events chan<- domain.ChatEvent, requestID string, startTime time.Time) {
	events <- domain.ChatStartEvent{
		RequestID: requestID,
		Timestamp: startTime,
	}
}

// initializeMetrics initializes metrics for a request
func (s *AgentServiceImpl) initializeMetrics(requestID string) {
	s.metricsMux.Lock()
	s.metrics[requestID] = &domain.ChatMetrics{
		Duration: 0,
		Usage:    nil,
	}
	s.metricsMux.Unlock()
}

// createContentStream creates a streaming content channel
func (s *AgentServiceImpl) createContentStream(timeoutCtx context.Context, model string, messages []sdk.Message) (<-chan sdk.SSEvent, error) {
	provider, modelName, err := s.parseProvider(model)
	if err != nil {
		return nil, fmt.Errorf("failed to parse provider from model '%s': %w", model, err)
	}

	providerType := sdk.Provider(provider)

	clientWithTools := s.client
	if tools := s.convertToSDKTools(); tools != nil {
		clientWithTools = s.client.WithTools(tools)
	}

	options := &sdk.CreateChatCompletionRequest{
		MaxTokens: &s.maxTokens,
	}

	stream, err := clientWithTools.
		WithOptions(options).
		WithMiddlewareOptions(&sdk.MiddlewareOptions{
			SkipMCP: true,
			SkipA2A: true,
		}).
		GenerateContentStream(timeoutCtx, providerType, modelName, messages)
	if err != nil {
		return nil, fmt.Errorf("failed to generate content stream: %w", err)
	}

	return stream, nil
}

// sendErrorEvent sends an error event
func (s *AgentServiceImpl) sendErrorEvent(events chan<- domain.ChatEvent, requestID string, err error) {
	events <- domain.ChatErrorEvent{
		RequestID: requestID,
		Timestamp: time.Now(),
		Error:     err,
	}
}

// processEventStream processes the streaming events
func (s *AgentServiceImpl) processEventStream(timeoutCtx context.Context, stream <-chan sdk.SSEvent, events chan<- domain.ChatEvent, requestID string, startTime time.Time) {
	var fullMessage strings.Builder
	var toolCalls []sdk.ChatCompletionMessageToolCall
	toolCallsMap := make(map[string]*sdk.ChatCompletionMessageToolCall)
	var usage *sdk.CompletionUsage
	toolCallsStarted := false

	for {
		select {
		case <-timeoutCtx.Done():
			s.handleTimeout(events, requestID, timeoutCtx)
			return

		case event, ok := <-stream:
			if !ok {
				finalToolCalls := make([]sdk.ChatCompletionMessageToolCall, 0, len(toolCallsMap))
				for _, tc := range toolCallsMap {
					finalToolCalls = append(finalToolCalls, *tc)
				}
				s.sendCompleteEvent(events, requestID, startTime, fullMessage.String(), finalToolCalls, usage)
				return
			}

			if event.Event == nil {
				continue
			}

			if s.handleStreamEvent(event, events, requestID, &fullMessage, &toolCalls, &usage, toolCallsMap, &toolCallsStarted) {
				finalToolCalls := make([]sdk.ChatCompletionMessageToolCall, 0, len(toolCallsMap))
				for _, tc := range toolCallsMap {
					finalToolCalls = append(finalToolCalls, *tc)
				}
				s.sendCompleteEvent(events, requestID, startTime, fullMessage.String(), finalToolCalls, usage)
				return
			}
		}
	}
}

// handleTimeout handles request timeout
func (s *AgentServiceImpl) handleTimeout(events chan<- domain.ChatEvent, requestID string, timeoutCtx context.Context) {
	var errorMsg string
	if timeoutCtx.Err() == context.DeadlineExceeded {
		errorMsg = fmt.Sprintf("request timed out after %d seconds", s.timeoutSeconds)
	} else {
		errorMsg = "request cancelled by user"
	}

	events <- domain.ChatErrorEvent{
		RequestID: requestID,
		Timestamp: time.Now(),
		Error:     fmt.Errorf("%s", errorMsg),
	}
}

// sendCompleteEvent sends a completion event
func (s *AgentServiceImpl) sendCompleteEvent(events chan<- domain.ChatEvent, requestID string, startTime time.Time, message string, toolCalls []sdk.ChatCompletionMessageToolCall, usage *sdk.CompletionUsage) {
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

// handleStreamEvent handles individual stream events
func (s *AgentServiceImpl) handleStreamEvent(event sdk.SSEvent, events chan<- domain.ChatEvent, requestID string, fullMessage *strings.Builder, toolCalls *[]sdk.ChatCompletionMessageToolCall, usage **sdk.CompletionUsage, toolCallsMap map[string]*sdk.ChatCompletionMessageToolCall, toolCallsStarted *bool) bool {
	switch *event.Event {
	case sdk.ContentDelta:
		s.handleContentDelta(event, events, requestID, fullMessage, toolCalls, usage, toolCallsMap, toolCallsStarted)
		return false

	case sdk.StreamEnd:
		return true

	case "error":
		s.handleStreamError(event, events, requestID)
		return true
	}

	return false
}

// handleContentDelta handles content delta events
func (s *AgentServiceImpl) handleContentDelta(event sdk.SSEvent, events chan<- domain.ChatEvent, requestID string, fullMessage *strings.Builder, _ *[]sdk.ChatCompletionMessageToolCall, usage **sdk.CompletionUsage, toolCallsMap map[string]*sdk.ChatCompletionMessageToolCall, toolCallsStarted *bool) {
	chunk, reasoningChunk, usageChunk, hasToolCalls := s.processContentDelta(event, toolCallsMap, events, requestID)

	if hasToolCalls && !*toolCallsStarted {
		*toolCallsStarted = true
		events <- domain.ToolCallStartEvent{
			RequestID: requestID,
			Timestamp: time.Now(),
		}
	}

	if chunk != "" || reasoningChunk != "" {
		if chunk != "" {
			fullMessage.WriteString(chunk)
		}
		events <- domain.ChatChunkEvent{
			RequestID:        requestID,
			Timestamp:        time.Now(),
			Content:          chunk,
			ReasoningContent: reasoningChunk,
			ToolCalls:        nil,
			Delta:            true,
		}
	}
	if usageChunk != nil {
		*usage = usageChunk
	}
}

// handleStreamError handles stream error events
func (s *AgentServiceImpl) handleStreamError(event sdk.SSEvent, events chan<- domain.ChatEvent, requestID string) {
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

// processContentDelta processes a content delta event and accumulates tool calls
func (s *AgentServiceImpl) processContentDelta(event sdk.SSEvent, toolCallsMap map[string]*sdk.ChatCompletionMessageToolCall, events chan<- domain.ChatEvent, requestID string) (string, string, *sdk.CompletionUsage, bool) {
	if event.Data == nil {
		return "", "", nil, false
	}

	var streamResponse sdk.CreateChatCompletionStreamResponse
	if err := json.Unmarshal(*event.Data, &streamResponse); err != nil {
		return "", "", nil, false
	}

	var content, reasoningContent string
	hasToolCalls := false

	for _, choice := range streamResponse.Choices {
		content += choice.Delta.Content
		extractedReasoning := s.extractReasoningContent((*json.RawMessage)(event.Data), choice)
		reasoningContent += extractedReasoning
		hasToolCalls = s.processToolCalls(choice.Delta.ToolCalls, toolCallsMap, events, requestID) || hasToolCalls
	}

	return content, reasoningContent, streamResponse.Usage, hasToolCalls
}

// extractReasoningContent extracts reasoning content from choice delta
func (s *AgentServiceImpl) extractReasoningContent(eventData *json.RawMessage, choice sdk.ChatCompletionStreamChoice) string {
	var reasoningContent string

	reasoningContent += s.extractReasoningFromRawData(eventData)
	reasoningContent += s.extractReasoningFromChoice(choice)

	return reasoningContent
}

// extractReasoningFromRawData extracts reasoning content from raw event data
func (s *AgentServiceImpl) extractReasoningFromRawData(eventData *json.RawMessage) string {
	var rawData map[string]any
	if json.Unmarshal(*eventData, &rawData) != nil {
		return ""
	}

	choices, ok := rawData["choices"].([]any)
	if !ok || len(choices) == 0 {
		return ""
	}

	choice, ok := choices[0].(map[string]any)
	if !ok {
		return ""
	}

	delta, ok := choice["delta"].(map[string]any)
	if !ok {
		return ""
	}

	reasoning, ok := delta["reasoning_content"].(string)
	if ok && reasoning != "" {
		return reasoning
	}

	return ""
}

// extractReasoningFromChoice extracts reasoning content from choice delta
func (s *AgentServiceImpl) extractReasoningFromChoice(choice sdk.ChatCompletionStreamChoice) string {
	var reasoningContent string

	if choice.Delta.ReasoningContent != nil && *choice.Delta.ReasoningContent != "" {
		reasoningContent += *choice.Delta.ReasoningContent
	}
	if choice.Delta.Reasoning != nil && *choice.Delta.Reasoning != "" {
		reasoningContent += *choice.Delta.Reasoning
	}

	return reasoningContent
}

// processToolCalls processes delta tool calls and returns true if any tool calls were processed
func (s *AgentServiceImpl) processToolCalls(deltaToolCalls []sdk.ChatCompletionMessageToolCallChunk, toolCallsMap map[string]*sdk.ChatCompletionMessageToolCall, events chan<- domain.ChatEvent, requestID string) bool {
	if len(deltaToolCalls) == 0 {
		return false
	}

	for _, deltaToolCall := range deltaToolCalls {
		s.processSingleToolCall(deltaToolCall, toolCallsMap, events, requestID)
	}

	return true
}

// processSingleToolCall processes a single delta tool call
func (s *AgentServiceImpl) processSingleToolCall(deltaToolCall sdk.ChatCompletionMessageToolCallChunk, toolCallsMap map[string]*sdk.ChatCompletionMessageToolCall, events chan<- domain.ChatEvent, requestID string) {
	key := fmt.Sprintf("%d", deltaToolCall.Index)

	s.initializeToolCall(key, deltaToolCall, toolCallsMap)
	s.updateToolCall(key, deltaToolCall, toolCallsMap)
	s.emitToolCallEventIfComplete(key, toolCallsMap, events, requestID)
}

// initializeToolCall creates a new tool call entry if it doesn't exist
func (s *AgentServiceImpl) initializeToolCall(key string, deltaToolCall sdk.ChatCompletionMessageToolCallChunk, toolCallsMap map[string]*sdk.ChatCompletionMessageToolCall) {
	if toolCallsMap[key] != nil {
		return
	}

	toolCallsMap[key] = &sdk.ChatCompletionMessageToolCall{
		Id:   deltaToolCall.ID,
		Type: sdk.Function,
		Function: sdk.ChatCompletionMessageToolCallFunction{
			Name:      "",
			Arguments: "",
		},
	}
}

// updateToolCall updates the tool call with delta information
func (s *AgentServiceImpl) updateToolCall(key string, deltaToolCall sdk.ChatCompletionMessageToolCallChunk, toolCallsMap map[string]*sdk.ChatCompletionMessageToolCall) {
	if deltaToolCall.ID != "" {
		toolCallsMap[key].Id = deltaToolCall.ID
	}

	if deltaToolCall.Function.Name != "" {
		toolCallsMap[key].Function.Name += deltaToolCall.Function.Name
	}
	if deltaToolCall.Function.Arguments != "" {
		toolCallsMap[key].Function.Arguments += deltaToolCall.Function.Arguments
	}
}

// emitToolCallEventIfComplete emits a tool call event if the tool call is complete
func (s *AgentServiceImpl) emitToolCallEventIfComplete(key string, toolCallsMap map[string]*sdk.ChatCompletionMessageToolCall, events chan<- domain.ChatEvent, requestID string) {
	args := strings.TrimSpace(toolCallsMap[key].Function.Arguments)
	funcName := strings.TrimSpace(toolCallsMap[key].Function.Name)

	if !s.isToolCallComplete(args, funcName) {
		return
	}

	events <- domain.ToolCallEvent{
		RequestID:  requestID,
		Timestamp:  time.Now(),
		ToolCallID: toolCallsMap[key].Id,
		ToolName:   funcName,
		Args:       args,
	}
}

// isToolCallComplete checks if a tool call is complete and valid
func (s *AgentServiceImpl) isToolCallComplete(args, funcName string) bool {
	if args == "" || funcName == "" || !strings.HasSuffix(args, "}") {
		return false
	}

	var temp any
	return json.Unmarshal([]byte(args), &temp) == nil
}
