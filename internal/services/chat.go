package services

import (
	"context"
	"encoding/json"
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
	// Prepare base URL for SDK
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
	if len(messages) == 0 {
		return nil, fmt.Errorf("no messages provided")
	}

	if model == "" {
		return nil, fmt.Errorf("no model specified")
	}

	// Add tools system message if tools are available
	if s.toolService != nil {
		availableTools := s.toolService.ListTools()
		if len(availableTools) > 0 {
			toolsMessage := s.createToolsSystemMessage(availableTools)
			// Prepend tools message to the messages
			messages = append([]sdk.Message{toolsMessage}, messages...)
		}
	}

	// Generate request ID
	requestID := generateRequestID()

	// Create context with timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(s.timeoutSeconds)*time.Second)

	// Track active request
	s.requestsMux.Lock()
	s.activeRequests[requestID] = cancel
	s.requestsMux.Unlock()

	// Clean up tracking when done
	defer func() {
		s.requestsMux.Lock()
		delete(s.activeRequests, requestID)
		s.requestsMux.Unlock()
	}()

	// Create output channel
	events := make(chan domain.ChatEvent, 100)

	// Start async processing
	go func() {
		defer close(events)
		defer cancel()

		startTime := time.Now()

		// Send start event
		events <- domain.ChatStartEvent{
			RequestID: requestID,
			Timestamp: startTime,
		}

		// Initialize metrics
		s.metricsMux.Lock()
		s.metrics[requestID] = &domain.ChatMetrics{
			Duration: 0,
			Usage:    nil,
		}
		s.metricsMux.Unlock()

		// Use the provided model

		provider, modelName, err := s.parseProvider(model)
		if err != nil {
			events <- domain.ChatErrorEvent{
				RequestID: requestID,
				Timestamp: time.Now(),
				Error:     fmt.Errorf("failed to parse provider from model '%s': %w", model, err),
			}
			return
		}

		// Start streaming - convert string provider to SDK provider type
		providerType := sdk.Provider(provider)
		stream, err := s.client.GenerateContentStream(timeoutCtx, providerType, modelName, messages)
		if err != nil {
			events <- domain.ChatErrorEvent{
				RequestID: requestID,
				Timestamp: time.Now(),
				Error:     fmt.Errorf("failed to generate content stream: %w", err),
			}
			return
		}

		// Process stream
		var fullMessage strings.Builder
		var toolCalls []sdk.ChatCompletionMessageToolCall
		var usage *sdk.CompletionUsage

		for {
			select {
			case <-timeoutCtx.Done():
				events <- domain.ChatErrorEvent{
					RequestID: requestID,
					Timestamp: time.Now(),
					Error:     fmt.Errorf("request timed out after %d seconds", s.timeoutSeconds),
				}
				return

			case event, ok := <-stream:
				if !ok {
					// Stream closed - send completion event
					duration := time.Since(startTime)

					// Update metrics
					s.metricsMux.Lock()
					if metrics, exists := s.metrics[requestID]; exists {
						metrics.Duration = duration
						metrics.Usage = usage
					}
					s.metricsMux.Unlock()

					events <- domain.ChatCompleteEvent{
						RequestID: requestID,
						Timestamp: time.Now(),
						Message:   fullMessage.String(),
						ToolCalls: toolCalls,
						Metrics:   s.metrics[requestID],
					}
					return
				}

				if event.Event == nil {
					continue
				}

				switch *event.Event {
				case sdk.ContentDelta:
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
						toolCalls = append(toolCalls, toolCallsChunk...)
					}
					if usageChunk != nil {
						usage = usageChunk
					}

				case sdk.StreamEnd:
					// Stream ended normally
					continue

				case "error":
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
					return
				}
			}
		}
	}()

	return events, nil
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
		// Return copy to prevent external modification
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

		// Process tool calls
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
		description := fmt.Sprintf("\n- **%s**: %s", tool.Name, tool.Description)

		// Add parameter information if available
		if tool.Parameters != nil {
			if paramMap, ok := tool.Parameters.(map[string]interface{}); ok {
				if props, ok := paramMap["properties"].(map[string]interface{}); ok {
					description += "\n  Parameters:"
					for paramName, paramInfo := range props {
						if paramInfoMap, ok := paramInfo.(map[string]interface{}); ok {
							if desc, ok := paramInfoMap["description"].(string); ok {
								description += fmt.Sprintf("\n    - %s: %s", paramName, desc)
							}
						}
					}
				}
			}
		}

		toolDescriptions = append(toolDescriptions, description)
	}

	toolDescriptions = append(toolDescriptions, "\nTo use a tool, format your response as: `TOOL_CALL: {\"name\": \"tool_name\", \"arguments\": {\"param\": \"value\"}}`")

	return sdk.Message{
		Role:    sdk.System,
		Content: strings.Join(toolDescriptions, ""),
	}
}

// generateRequestID generates a unique request ID
func generateRequestID() string {
	return fmt.Sprintf("req_%d", time.Now().UnixNano())
}
