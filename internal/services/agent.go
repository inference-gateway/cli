package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/logger"
	sdk "github.com/inference-gateway/sdk"
)

// streamState manages the state of a streaming request (simplified)
type streamState struct {
	reqID            string
	startTime        time.Time
	contentBuilder   strings.Builder
	reasoningBuilder strings.Builder
	toolCallsMap     map[string]*sdk.ChatCompletionMessageToolCall
	toolCallsStarted bool
	usage            *sdk.CompletionUsage
}

// AgentServiceImpl implements the AgentService interface with direct chat functionality
type AgentServiceImpl struct {
	client         sdk.Client
	toolService    domain.ToolService
	config         domain.ConfigService
	timeoutSeconds int
	maxTokens      int
	optimizer      *ConversationOptimizer

	// Request tracking
	activeRequests map[string]context.CancelFunc
	requestsMux    sync.RWMutex

	// Metrics tracking
	metrics    map[string]*domain.ChatMetrics
	metricsMux sync.RWMutex
}

// NewAgentService creates a new agent service with pre-configured client
func NewAgentService(client sdk.Client, toolService domain.ToolService, config domain.ConfigService, timeoutSeconds int, maxTokens int, optimizer *ConversationOptimizer) *AgentServiceImpl {
	return &AgentServiceImpl{
		client:         client,
		toolService:    toolService,
		config:         config,
		timeoutSeconds: timeoutSeconds,
		maxTokens:      maxTokens,
		optimizer:      optimizer,
		activeRequests: make(map[string]context.CancelFunc),
		metrics:        make(map[string]*domain.ChatMetrics),
	}
}

// Run executes an agent task synchronously (for background/batch processing)
func (s *AgentServiceImpl) Run(ctx context.Context, req *domain.AgentRequest) (*domain.ChatSyncResponse, error) {
	if err := s.validateRequest(req); err != nil {
		return nil, err
	}

	optimizedMessages := req.Messages
	if s.optimizer != nil {
		optimizedMessages = s.optimizer.OptimizeMessages(req.Messages)
		logger.Debug("Message optimization applied",
			"original_count", len(req.Messages),
			"optimized_count", len(optimizedMessages))
	}

	messages := s.addSystemPrompt(optimizedMessages)

	logger.Info("LLM Request (Sync)",
		"request_id", req.RequestID,
		"model", req.Model,
		"messages", messages)

	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(s.timeoutSeconds)*time.Second)
	defer cancel()

	startTime := time.Now()

	response, err := s.generateContentSync(timeoutCtx, req.Model, messages)
	if err != nil {
		return nil, fmt.Errorf("failed to generate content: %w", err)
	}

	duration := time.Since(startTime)

	var content string
	var toolCalls []sdk.ChatCompletionMessageToolCall

	if len(response.Choices) > 0 {
		choice := response.Choices[0]
		content = choice.Message.Content

		if choice.Message.ToolCalls != nil {
			toolCalls = *choice.Message.ToolCalls
		}
	}

	return &domain.ChatSyncResponse{
		RequestID: req.RequestID,
		Content:   content,
		ToolCalls: toolCalls,
		Usage:     response.Usage,
		Duration:  duration,
	}, nil
}

// resetStreamState creates and returns a new stream state (simplified)
func (s *AgentServiceImpl) resetStreamState(reqID string, startTime time.Time) *streamState {
	return &streamState{
		reqID:            reqID,
		startTime:        startTime,
		toolCallsMap:     make(map[string]*sdk.ChatCompletionMessageToolCall),
		toolCallsStarted: false,
		usage:            nil,
	}
}

// processToolCallDelta efficiently processes tool call deltas with real-time UI updates
func (s *AgentServiceImpl) processToolCallDelta(deltaToolCall sdk.ChatCompletionMessageToolCallChunk, state *streamState, events chan domain.ChatEvent) {
	key := fmt.Sprintf("%d", deltaToolCall.Index)

	toolCall, exists := state.toolCallsMap[key]
	wasNew := false
	if !exists {
		wasNew = true
		toolCall = &sdk.ChatCompletionMessageToolCall{
			Id:   deltaToolCall.ID,
			Type: sdk.Function,
			Function: sdk.ChatCompletionMessageToolCallFunction{
				Name:      "",
				Arguments: "",
			},
		}
		state.toolCallsMap[key] = toolCall
	}

	nameChanged := false
	argsChanged := false
	idChanged := false

	if deltaToolCall.ID != "" && toolCall.Id != deltaToolCall.ID {
		toolCall.Id = deltaToolCall.ID
		idChanged = true
	}
	if deltaToolCall.Function.Name != "" {
		toolCall.Function.Name += deltaToolCall.Function.Name
		nameChanged = true
	}
	if deltaToolCall.Function.Arguments != "" {
		toolCall.Function.Arguments += deltaToolCall.Function.Arguments
		argsChanged = true
	}

	args := strings.TrimSpace(toolCall.Function.Arguments)
	funcName := strings.TrimSpace(toolCall.Function.Name)

	if (strings.HasPrefix(funcName, "a2a_") && s.config.ShouldSkipA2AToolOnClient()) ||
		(strings.HasPrefix(funcName, "mcp_") && s.config.ShouldSkipMCPToolOnClient()) {
		return
	}

	if wasNew && funcName != "" {
		logger.Debug("Sending ToolCallPreviewEvent",
			"tool_name", funcName,
			"tool_call_id", toolCall.Id)
		events <- domain.ToolCallPreviewEvent{
			RequestID:  state.reqID,
			Timestamp:  time.Now(),
			ToolCallID: toolCall.Id,
			ToolName:   funcName,
			Arguments:  args,
			Status:     domain.ToolCallStreamStatusStreaming,
			IsComplete: false,
		}
		return
	}

	if (nameChanged || argsChanged || idChanged) && funcName != "" { //nolint:nestif
		status := domain.ToolCallStreamStatusStreaming

		if args != "" && strings.HasSuffix(args, "}") {
			var temp any
			if json.Unmarshal([]byte(args), &temp) == nil {
				status = domain.ToolCallStreamStatusComplete
				logger.Debug("Tool call JSON complete",
					"tool_name", funcName,
					"args", args)
			}
		}

		events <- domain.ToolCallUpdateEvent{
			RequestID:  state.reqID,
			Timestamp:  time.Now(),
			ToolCallID: toolCall.Id,
			ToolName:   funcName,
			Arguments:  args,
			Status:     status,
		}
	} else {
		logger.Debug("Skipping ToolCallUpdateEvent",
			"nameChanged", nameChanged,
			"argsChanged", argsChanged,
			"idChanged", idChanged,
			"funcName", funcName)
	}
}

// RunWithStream executes an agent task with streaming (for interactive chat)
func (s *AgentServiceImpl) RunWithStream(ctx context.Context, req *domain.AgentRequest) (<-chan domain.ChatEvent, error) { //nolint:funlen,gocognit,gocyclo,cyclop
	if err := s.validateRequest(req); err != nil {
		return nil, err
	}

	optimizedMessages := req.Messages
	if s.optimizer != nil {
		optimizedMessages = s.optimizer.OptimizeMessages(req.Messages)
		logger.Debug("Message optimization applied",
			"original_count", len(req.Messages),
			"optimized_count", len(optimizedMessages))
	}

	messages := s.addSystemPrompt(optimizedMessages)

	logger.Info("LLM Request",
		"request_id", req.RequestID,
		"model", req.Model,
		"messages", messages)

	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(s.timeoutSeconds)*time.Second)

	s.requestsMux.Lock()
	s.activeRequests[req.RequestID] = cancel
	s.requestsMux.Unlock()

	channelSize := 200
	if s.toolService != nil {
		toolCount := len(s.toolService.ListTools())
		channelSize = max(200, toolCount*10)
	}
	events := make(chan domain.ChatEvent, channelSize)

	go func() {
		defer close(events)
		defer cancel()
		defer func() {
			s.requestsMux.Lock()
			delete(s.activeRequests, req.RequestID)
			s.requestsMux.Unlock()
		}()

		startTime := time.Now()
		state := s.resetStreamState(req.RequestID, startTime)

		events <- domain.ChatStartEvent{
			RequestID: req.RequestID,
			Timestamp: startTime,
			Model:     req.Model,
		}

		s.metricsMux.Lock()
		s.metrics[req.RequestID] = &domain.ChatMetrics{
			Duration: 0,
			Usage:    nil,
		}
		s.metricsMux.Unlock()

		slashIndex := strings.Index(req.Model, "/")
		if slashIndex == -1 {
			s.sendErrorEvent(events, req.RequestID, fmt.Errorf("invalid model format, expected 'provider/model'"))
			return
		}

		provider := req.Model[:slashIndex]
		modelName := req.Model[slashIndex+1:]
		providerType := sdk.Provider(provider)

		clientWithTools := s.client
		if s.toolService != nil { //nolint:nestif
			availableTools := s.toolService.ListTools()
			if len(availableTools) > 0 {
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
				clientWithTools = s.client.WithTools(&sdkTools)
			}
		}

		options := &sdk.CreateChatCompletionRequest{
			MaxTokens: &s.maxTokens,
		}

		middlewareOptions := &sdk.MiddlewareOptions{
			SkipMCP: s.config.ShouldSkipMCPToolOnClient(),
			SkipA2A: s.config.ShouldSkipA2AToolOnClient(),
		}

		currentMessages := make([]sdk.Message, len(messages))
		copy(currentMessages, messages)

		for {

			select {
			case <-timeoutCtx.Done():
				var errorMsg string
				if timeoutCtx.Err() == context.DeadlineExceeded {
					errorMsg = fmt.Sprintf("request timed out after %d seconds", s.timeoutSeconds)
				} else {
					errorMsg = "request cancelled by user"
				}
				events <- domain.ChatErrorEvent{
					RequestID: req.RequestID,
					Timestamp: time.Now(),
					Error:     fmt.Errorf("%s", errorMsg),
				}
				return
			default:
			}

			stream, err := clientWithTools.
				WithOptions(options).
				WithMiddlewareOptions(middlewareOptions).
				GenerateContentStream(timeoutCtx, providerType, modelName, currentMessages)
			if err != nil {
				events <- domain.ChatErrorEvent{
					RequestID: req.RequestID,
					Timestamp: time.Now(),
					Error:     fmt.Errorf("failed to generate content stream: %w", err),
				}
				return
			}

			var iterationBuilder strings.Builder
			iterationToolCallsMap := make(map[string]*sdk.ChatCompletionMessageToolCall)
			hasToolCalls := false
			streamComplete := false

			for !streamComplete {
				select {
				case <-timeoutCtx.Done():
					var errorMsg string
					if timeoutCtx.Err() == context.DeadlineExceeded {
						errorMsg = fmt.Sprintf("request timed out after %d seconds", s.timeoutSeconds)
					} else {
						errorMsg = "request cancelled by user"
					}
					events <- domain.ChatErrorEvent{
						RequestID: req.RequestID,
						Timestamp: time.Now(),
						Error:     fmt.Errorf("%s", errorMsg),
					}
					return

				case event, ok := <-stream:
					if !ok {
						streamComplete = true
						break
					}

					if event.Event == nil {
						continue
					}

					switch *event.Event {
					case sdk.ContentDelta:
						if event.Data == nil {
							continue
						}

						var streamResponse sdk.CreateChatCompletionStreamResponse
						if err := json.Unmarshal(*event.Data, &streamResponse); err != nil {
							continue
						}

						for _, choice := range streamResponse.Choices {
							if choice.Delta.Content != "" {
								iterationBuilder.WriteString(choice.Delta.Content)
								state.contentBuilder.WriteString(choice.Delta.Content)
								events <- domain.ChatChunkEvent{
									RequestID:        req.RequestID,
									Timestamp:        time.Now(),
									Content:          choice.Delta.Content,
									ReasoningContent: "",
									ToolCalls:        nil,
									Delta:            true,
								}
							}

							var extractedReasoning string
							if choice.Delta.ReasoningContent != nil && *choice.Delta.ReasoningContent != "" {
								extractedReasoning = *choice.Delta.ReasoningContent
							}
							if choice.Delta.Reasoning != nil && *choice.Delta.Reasoning != "" {
								extractedReasoning += *choice.Delta.Reasoning
							}

							if extractedReasoning != "" {
								state.reasoningBuilder.WriteString(extractedReasoning)
								events <- domain.ChatChunkEvent{
									RequestID:        req.RequestID,
									Timestamp:        time.Now(),
									Content:          "",
									ReasoningContent: extractedReasoning,
									ToolCalls:        nil,
									Delta:            true,
								}
							}

							if len(choice.Delta.ToolCalls) > 0 { //nolint:nestif
								hasToolCalls = true
								if !state.toolCallsStarted {
									state.toolCallsStarted = true
									events <- domain.ToolCallStartEvent{
										RequestID: req.RequestID,
										Timestamp: time.Now(),
									}
								}

								for _, deltaToolCall := range choice.Delta.ToolCalls {
									key := fmt.Sprintf("%d", deltaToolCall.Index)
									if iterationToolCallsMap[key] == nil {
										iterationToolCallsMap[key] = &sdk.ChatCompletionMessageToolCall{
											Id:   deltaToolCall.ID,
											Type: sdk.Function,
											Function: sdk.ChatCompletionMessageToolCallFunction{
												Name:      "",
												Arguments: "",
											},
										}
									}
									if deltaToolCall.ID != "" {
										iterationToolCallsMap[key].Id = deltaToolCall.ID
									}
									if deltaToolCall.Function.Name != "" {
										iterationToolCallsMap[key].Function.Name += deltaToolCall.Function.Name
									}
									if deltaToolCall.Function.Arguments != "" {
										iterationToolCallsMap[key].Function.Arguments += deltaToolCall.Function.Arguments
									}

									s.processToolCallDelta(deltaToolCall, state, events)
								}
							}
						}

						if streamResponse.Usage != nil {
							state.usage = streamResponse.Usage
						}

					case sdk.StreamEnd:
						if event.Data != nil { //nolint:nestif
							dataStr := string(*event.Data)
							if dataStr == "[DONE]" {
								streamComplete = true
								break
							}
							var streamResponse sdk.CreateChatCompletionStreamResponse
							if json.Unmarshal(*event.Data, &streamResponse) == nil {
								for _, choice := range streamResponse.Choices {
									if choice.FinishReason == "tool_calls" || choice.FinishReason == "stop" {
										streamComplete = true
										break
									}
								}
							}
						}

					case "error":
						var errResp struct {
							Error string `json:"error"`
						}
						if event.Data != nil {
							_ = json.Unmarshal(*event.Data, &errResp)
						}
						events <- domain.ChatErrorEvent{
							RequestID: req.RequestID,
							Timestamp: time.Now(),
							Error:     fmt.Errorf("stream error: %s", errResp.Error),
						}
						return
					}
				}
			}

			hasGlobalToolCalls := len(state.toolCallsMap) > 0

			logger.Debug("Stream iteration complete",
				"hasToolCalls", hasToolCalls,
				"hasGlobalToolCalls", hasGlobalToolCalls,
				"iterationToolCallsCount", len(iterationToolCallsMap),
				"globalToolCallsCount", len(state.toolCallsMap))

			if !hasToolCalls && !hasGlobalToolCalls {
				finalToolCalls := make([]sdk.ChatCompletionMessageToolCall, 0, len(state.toolCallsMap))
				for _, tc := range state.toolCallsMap {
					finalToolCalls = append(finalToolCalls, *tc)
				}

				duration := time.Since(startTime)

				s.metricsMux.Lock()
				if metrics, exists := s.metrics[req.RequestID]; exists {
					metrics.Duration = duration
					metrics.Usage = state.usage
				}
				s.metricsMux.Unlock()

				events <- domain.ChatCompleteEvent{
					RequestID: req.RequestID,
					Timestamp: time.Now(),
					Message:   state.contentBuilder.String(),
					ToolCalls: finalToolCalls,
					Metrics:   s.metrics[req.RequestID],
				}
				return
			}

			finalToolCalls := make([]sdk.ChatCompletionMessageToolCall, 0, len(state.toolCallsMap))
			for _, tc := range state.toolCallsMap {
				finalToolCalls = append(finalToolCalls, *tc)
			}

			logger.Debug("Sending ToolCallReadyEvent",
				"toolCallsCount", len(finalToolCalls))

			events <- domain.ToolCallReadyEvent{
				RequestID: req.RequestID,
				Timestamp: time.Now(),
				ToolCalls: finalToolCalls,
			}

			break
		}

		finalToolCalls := make([]sdk.ChatCompletionMessageToolCall, 0, len(state.toolCallsMap))
		for _, tc := range state.toolCallsMap {
			finalToolCalls = append(finalToolCalls, *tc)
		}

		duration := time.Since(startTime)

		s.metricsMux.Lock()
		if metrics, exists := s.metrics[req.RequestID]; exists {
			metrics.Duration = duration
			metrics.Usage = state.usage
		}
		s.metricsMux.Unlock()

		events <- domain.ChatCompleteEvent{
			RequestID: req.RequestID,
			Timestamp: time.Now(),
			Message:   state.contentBuilder.String(),
			ToolCalls: finalToolCalls,
			Metrics:   s.metrics[req.RequestID],
		}
	}()

	return events, nil
}

// CancelRequest cancels an active request
func (s *AgentServiceImpl) CancelRequest(requestID string) error {
	s.requestsMux.RLock()
	cancel, exists := s.activeRequests[requestID]
	s.requestsMux.RUnlock()

	if !exists {
		return fmt.Errorf("request %s not found or already completed", requestID)
	}

	cancel()
	return nil
}

// GetMetrics returns metrics for a completed request
func (s *AgentServiceImpl) GetMetrics(requestID string) *domain.ChatMetrics {
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
