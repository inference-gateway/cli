package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/logger"
	sdk "github.com/inference-gateway/sdk"
)

// AgentServiceImpl implements the AgentService interface with direct chat functionality
type AgentServiceImpl struct {
	client           sdk.Client
	toolService      domain.ToolService
	config           domain.ConfigService
	conversationRepo domain.ConversationRepository
	timeoutSeconds   int
	maxTokens        int
	optimizer        *ConversationOptimizer

	// Request tracking
	activeRequests map[string]context.CancelFunc
	requestsMux    sync.RWMutex

	// Cancel channels for graceful stopping of agent event loops
	cancelChannels map[string]chan struct{}
	cancelMux      sync.RWMutex

	// Metrics tracking
	metrics    map[string]*domain.ChatMetrics
	metricsMux sync.RWMutex

	// Tool call accumulation
	toolCallsMap map[string]*sdk.ChatCompletionMessageToolCall
	toolCallsMux sync.RWMutex

	// A2A task tracking to prevent duplicate processing
	executedA2ATasks map[string]bool
	a2aTasksMux      sync.RWMutex
}

// NewAgentService creates a new agent service with pre-configured client
func NewAgentService(
	client sdk.Client,
	toolService domain.ToolService,
	config domain.ConfigService,
	conversationRepo domain.ConversationRepository,
	timeoutSeconds int,
	optimizer *ConversationOptimizer,
) *AgentServiceImpl {
	return &AgentServiceImpl{
		client:           client,
		toolService:      toolService,
		config:           config,
		conversationRepo: conversationRepo,
		timeoutSeconds:   timeoutSeconds,
		maxTokens:        config.GetAgentConfig().MaxTokens,
		optimizer:        optimizer,
		activeRequests:   make(map[string]context.CancelFunc),
		cancelChannels:   make(map[string]chan struct{}),
		metrics:          make(map[string]*domain.ChatMetrics),
		toolCallsMap:     make(map[string]*sdk.ChatCompletionMessageToolCall),
		executedA2ATasks: make(map[string]bool),
	}
}

// Run executes an agent task synchronously (for background/batch processing)
func (s *AgentServiceImpl) Run(ctx context.Context, req *domain.AgentRequest) (*domain.ChatSyncResponse, error) {
	if err := s.validateRequest(req); err != nil {
		return nil, err
	}

	optimizedMessages := req.Messages
	if s.optimizer != nil && s.config.GetAgentConfig().Optimization.Enabled {
		optimizedMessages = s.optimizer.OptimizeMessagesWithModel(req.Messages, req.Model)
	}

	messages := s.addSystemPrompt(optimizedMessages)

	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(s.timeoutSeconds)*time.Second)
	defer cancel()

	startTime := time.Now()

	response, err := func(timeoutCtx context.Context, model string, messages []sdk.Message) (*sdk.CreateChatCompletionResponse, error) {
		provider, modelName, err := s.parseProvider(model)
		if err != nil {
			return nil, fmt.Errorf("failed to parse provider from model '%s': %w", model, err)
		}

		providerType := sdk.Provider(provider)

		client := s.client.WithOptions(&sdk.CreateChatCompletionRequest{
			MaxTokens: &s.maxTokens,
		}).
			WithMiddlewareOptions(&sdk.MiddlewareOptions{
				SkipMCP: s.config.ShouldSkipMCPToolOnClient(),
				SkipA2A: s.config.ShouldSkipA2AToolOnClient(),
			})
		if s.toolService != nil { // nolint:nestif
			availableTools := s.toolService.ListTools()
			if len(availableTools) > 0 {
				client = s.client.WithTools(&availableTools)
			}
		}

		response, err := client.GenerateContent(timeoutCtx, providerType, modelName, messages)
		if err != nil {
			return nil, fmt.Errorf("failed to generate content: %w", err)
		}

		return response, nil
	}(timeoutCtx, req.Model, messages)
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

// RunWithStream executes an agent task with streaming (for interactive chat)
func (s *AgentServiceImpl) RunWithStream(ctx context.Context, req *domain.AgentRequest) (<-chan domain.ChatEvent, error) { // nolint:gocognit,gocyclo,cyclop,funlen
	if err := s.validateRequest(req); err != nil {
		return nil, err
	}

	chatEvents := make(chan domain.ChatEvent, 100)

	cancelChan := make(chan struct{}, 1)
	s.cancelMux.Lock()
	s.cancelChannels[req.RequestID] = cancelChan
	s.cancelMux.Unlock()

	defer func() {
		s.cancelMux.Lock()
		delete(s.cancelChannels, req.RequestID)
		s.cancelMux.Unlock()
	}()

	systemPrompt := s.config.GetAgentConfig().SystemPrompt
	if systemPrompt == "" {
		systemPrompt = "You are an helpful assistant."
	}

	client := s.client.
		WithMiddlewareOptions(&sdk.MiddlewareOptions{
			SkipMCP: s.config.ShouldSkipMCPToolOnClient(),
			SkipA2A: s.config.ShouldSkipA2AToolOnClient(),
		})
	availableTools := s.toolService.ListTools()
	if len(availableTools) > 0 {
		client = client.WithTools(&availableTools)
	}

	conversation := []sdk.Message{
		{Role: "system", Content: systemPrompt},
	}
	conversation = append(conversation, req.Messages...)

	provider, model, err := s.parseProvider(req.Model)
	if err != nil {
		return nil, fmt.Errorf("failed to parse provider from model '%s': %w", model, err)
	}

	turns := 0
	maxTurns := s.config.GetAgentConfig().MaxTurns
	go func() {
		conversation = s.optimizeConversation(ctx, req, conversation, chatEvents)

		//// EVENT LOOP START
		for maxTurns > turns {
			select {
			case <-cancelChan:
				chatEvents <- domain.ChatCompleteEvent{
					RequestID: req.RequestID,
					Timestamp: time.Now(),
					ToolCalls: []sdk.ChatCompletionMessageToolCall{},
					Metrics:   s.GetMetrics(req.RequestID),
				}
				close(chatEvents)
				return
			default:
			}

			s.clearToolCallsMap()
			iterationStartTime := time.Now()

			if turns > 0 {
				time.Sleep(100 * time.Millisecond)
			}

			requestCtx, requestCancel := context.WithCancel(ctx)

			s.requestsMux.Lock()
			s.activeRequests[req.RequestID] = requestCancel
			s.requestsMux.Unlock()

			defer func() {
				s.requestsMux.Lock()
				delete(s.activeRequests, req.RequestID)
				s.requestsMux.Unlock()
			}()

			chatEvents <- domain.ChatStartEvent{
				RequestID: req.RequestID,
				Timestamp: time.Now(),
			}

			if s.shouldInjectSystemReminder(turns) {
				systemReminderMsg := s.getSystemReminderMessage()
				conversation = append(conversation, systemReminderMsg)

				reminderEntry := domain.ConversationEntry{
					Message: systemReminderMsg,
					Time:    time.Now(),
					Hidden:  true,
				}

				if err := s.conversationRepo.AddMessage(reminderEntry); err != nil {
					logger.Error("failed to store system reminder message", "error", err)
				}
			}

			events, err := client.GenerateContentStream(requestCtx, sdk.Provider(provider), model, conversation)
			if err != nil {
				logger.Error("failed to create a stream, %w", err)
			}

			var allToolCallDeltas []sdk.ChatCompletionMessageToolCallChunk
			var message sdk.Message
			var streamUsage *sdk.CompletionUsage
			////// STREAM ITERATION START
			for event := range events {
				if event.Event == nil {
					logger.Error("event is nil")
					continue
				}

				if event.Data == nil {
					continue
				}

				var streamResponse sdk.CreateChatCompletionStreamResponse
				if err := json.Unmarshal(*event.Data, &streamResponse); err != nil {
					logger.Error("failed to unmarshal chat completion steam response")
					continue
				}

				for _, choice := range streamResponse.Choices {
					if choice.Delta.Reasoning != nil && *choice.Delta.Reasoning != "" {
						if message.Reasoning == nil {
							message.Reasoning = new(string)
						}
						*message.Reasoning += *choice.Delta.Reasoning
					}
					if choice.Delta.ReasoningContent != nil && *choice.Delta.ReasoningContent != "" {
						if message.ReasoningContent == nil {
							message.ReasoningContent = new(string)
						}
						*message.ReasoningContent += *choice.Delta.ReasoningContent
					}
					deltaContent := choice.Delta.Content
					if deltaContent != "" {
						message.Content += deltaContent
					}

					reasoning := ""
					if message.Reasoning != nil && *message.Reasoning != "" {
						reasoning = *message.Reasoning
					} else if message.ReasoningContent != nil && *message.ReasoningContent != "" {
						reasoning = *message.ReasoningContent
					}

					if len(choice.Delta.ToolCalls) > 0 {
						allToolCallDeltas = append(allToolCallDeltas, choice.Delta.ToolCalls...)
					}

					if deltaContent != "" || reasoning != "" || len(choice.Delta.ToolCalls) > 0 {
						event := domain.ChatChunkEvent{
							RequestID:        req.RequestID,
							Timestamp:        time.Now(),
							ReasoningContent: reasoning,
							Content:          deltaContent,
							Delta:            true,
						}

						if len(choice.Delta.ToolCalls) > 0 {
							event.ToolCalls = choice.Delta.ToolCalls
						}

						chatEvents <- event
					}

					if streamResponse.Usage != nil {
						streamUsage = streamResponse.Usage
					}
				}
			}
			////// STREAM ITERATION FINISHED

			s.accumulateToolCalls(allToolCallDeltas)
			toolCalls := s.getAccumulatedToolCalls()

			assistantMessage := sdk.Message{
				Role:    sdk.Assistant,
				Content: message.Content,
			}

			if len(toolCalls) > 0 {
				assistantToolCalls := make([]sdk.ChatCompletionMessageToolCall, 0, len(toolCalls))
				for _, tc := range toolCalls {
					assistantToolCalls = append(assistantToolCalls, *tc)
				}
				assistantMessage.ToolCalls = &assistantToolCalls
			}

			conversation = append(conversation, assistantMessage)

			assistantEntry := domain.ConversationEntry{
				Message: assistantMessage,
				Time:    time.Now(),
			}

			if err := s.conversationRepo.AddMessage(assistantEntry); err != nil {
				logger.Error("failed to store assistant message", "error", err)
			}

			var completeToolCalls []sdk.ChatCompletionMessageToolCall
			if len(toolCalls) > 0 {
				completeToolCalls = make([]sdk.ChatCompletionMessageToolCall, 0, len(toolCalls))
				for _, tc := range toolCalls {
					completeToolCalls = append(completeToolCalls, *tc)
				}
			}

			for _, tc := range toolCalls {
				if s.isA2ATool(tc.Function.Name) {
					s.a2aTasksMux.Lock()
					alreadyExecuted := s.executedA2ATasks[tc.Id]
					if !alreadyExecuted {
						s.executedA2ATasks[tc.Id] = true
						s.a2aTasksMux.Unlock()
						s.handleA2AToolCall(*tc, req.RequestID, chatEvents)
					} else {
						s.a2aTasksMux.Unlock()
					}

					toolResult := sdk.Message{
						Role:       sdk.Tool,
						Content:    fmt.Sprintf("%s executed on Gateway successfully", tc.Function.Name),
						ToolCallId: &tc.Id,
					}
					conversation = append(conversation, toolResult)
					continue
				}

				err := s.executeToolCall(ctx, *tc, req.RequestID, chatEvents)
				if err != nil {
					logger.Error("failed to execute tool: %w", err)
					errorResult := sdk.Message{
						Role:       sdk.Tool,
						Content:    fmt.Sprintf("Tool execution failed: %s", err.Error()),
						ToolCallId: &tc.Id,
					}
					conversation = append(conversation, errorResult)
					continue
				}

				messages := s.conversationRepo.GetMessages()
				if len(messages) == 0 {
					errorResult := sdk.Message{
						Role:       sdk.Tool,
						Content:    "Tool execution completed but no result was stored",
						ToolCallId: &tc.Id,
					}
					conversation = append(conversation, errorResult)
					continue
				}

				lastMessage := messages[len(messages)-1]
				if lastMessage.Message.Role != sdk.Tool {
					errorResult := sdk.Message{
						Role:       sdk.Tool,
						Content:    "Tool execution completed but result format is unexpected",
						ToolCallId: &tc.Id,
					}
					conversation = append(conversation, errorResult)
					continue
				}

				toolResult := sdk.Message{
					Role:       sdk.Tool,
					Content:    lastMessage.Message.Content,
					ToolCallId: &tc.Id,
				}
				conversation = append(conversation, toolResult)
			}

			s.storeIterationMetrics(req.RequestID, iterationStartTime, streamUsage)

			chatEvents <- domain.ChatCompleteEvent{
				RequestID: req.RequestID,
				Timestamp: time.Now(),
				ToolCalls: completeToolCalls,
				Metrics:   s.GetMetrics(req.RequestID),
			}

			if len(toolCalls) == 0 {
				// TODO - implement retries to ensure the agent is done - inject final message and continue until max configured retries
				break
			}
			turns++
		}
		//// EVENT LOOP FINISHED
		close(chatEvents)
	}()

	return chatEvents, nil
}

// CancelRequest cancels an active request
func (s *AgentServiceImpl) CancelRequest(requestID string) error {
	s.requestsMux.RLock()
	cancel, contextExists := s.activeRequests[requestID]
	s.requestsMux.RUnlock()

	s.cancelMux.RLock()
	cancelChan, chanExists := s.cancelChannels[requestID]
	s.cancelMux.RUnlock()

	if !contextExists && !chanExists {
		return fmt.Errorf("request %s not found or already completed", requestID)
	}

	if contextExists {
		cancel()
	}

	if chanExists {
		select {
		case cancelChan <- struct{}{}:
		default:
		}
	}

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

// storeIterationMetrics stores metrics for the current iteration and accumulates session tokens
func (s *AgentServiceImpl) storeIterationMetrics(requestID string, startTime time.Time, usage *sdk.CompletionUsage) {
	if usage == nil {
		return
	}

	metrics := &domain.ChatMetrics{
		Duration: time.Since(startTime),
		Usage:    usage,
	}

	s.metricsMux.Lock()
	s.metrics[requestID] = metrics
	s.metricsMux.Unlock()

	if err := s.conversationRepo.AddTokenUsage(
		int(usage.PromptTokens),
		int(usage.CompletionTokens),
		int(usage.TotalTokens),
	); err != nil {
		logger.Error("failed to add token usage to session", "error", err)
	}
}

func (s *AgentServiceImpl) optimizeConversation(ctx context.Context, req *domain.AgentRequest, conversation []sdk.Message, chatEvents chan<- domain.ChatEvent) []sdk.Message {
	if s.optimizer == nil || !s.config.GetAgentConfig().Optimization.Enabled {
		return conversation
	}

	originalCount := len(conversation)

	persistentRepo, isPersistent := s.conversationRepo.(*PersistentConversationRepository)
	if isPersistent {
		if cachedMessages := persistentRepo.GetOptimizedMessages(); len(cachedMessages) > 0 {
			if len(conversation) <= len(cachedMessages) {
				return cachedMessages
			}
			conversation = append(cachedMessages, conversation[len(cachedMessages):]...)
		}
	}

	chatEvents <- domain.OptimizationStatusEvent{
		RequestID:      req.RequestID,
		Timestamp:      time.Now(),
		Message:        "Optimizing conversation history...",
		IsActive:       true,
		OriginalCount:  originalCount,
		OptimizedCount: originalCount,
	}

	conversation = s.optimizer.OptimizeMessagesWithModel(conversation, req.Model)
	optimizedCount := len(conversation)

	var message string
	if originalCount != optimizedCount {
		message = fmt.Sprintf("Conversation optimized (%d → %d messages)", originalCount, optimizedCount)
	} else {
		message = "Conversation optimization completed"
	}

	chatEvents <- domain.OptimizationStatusEvent{
		RequestID:      req.RequestID,
		Timestamp:      time.Now(),
		Message:        message,
		IsActive:       false,
		OriginalCount:  originalCount,
		OptimizedCount: optimizedCount,
	}

	if isPersistent {
		if err := persistentRepo.SetOptimizedMessages(ctx, conversation); err != nil {
			logger.Error("Failed to save optimized conversation", "error", err)
		}
	}

	return conversation
}

func (s *AgentServiceImpl) isA2ATool(toolName string) bool {
	return strings.HasPrefix(toolName, "a2a_")
}

func (s *AgentServiceImpl) handleA2AToolCall(tc sdk.ChatCompletionMessageToolCall, requestID string, chatEvents chan<- domain.ChatEvent) {
	startTime := time.Now()

	// Enhanced status indicator for A2A tool calls
	chatEvents <- domain.ToolCallStartEvent{
		RequestID:     requestID,
		Timestamp:     startTime,
		ToolName:      tc.Function.Name,
		ToolArguments: tc.Function.Arguments,
	}

	// Show intermediate status that agent is processing
	chatEvents <- domain.ToolCallUpdateEvent{
		RequestID:  requestID,
		Timestamp:  time.Now(),
		ToolCallID: tc.Id,
		ToolName:   tc.Function.Name,
		Arguments:  tc.Function.Arguments,
		Status:     domain.ToolCallStreamStatusStreaming,
	}

	chatEvents <- domain.A2AToolCallExecutedEvent{
		RequestID:         requestID,
		Timestamp:         time.Now(),
		ToolCallID:        tc.Id,
		ToolName:          tc.Function.Name,
		Arguments:         tc.Function.Arguments,
		ExecutedOnGateway: true,
		TaskID:            tc.Id,
	}

	a2aEntry := domain.ConversationEntry{
		Message: domain.Message{
			Role:       "tool",
			Content:    fmt.Sprintf("%s executed on Gateway", tc.Function.Name),
			ToolCallId: &tc.Id,
		},
		Time: time.Now(),
		ToolExecution: &domain.ToolExecutionResult{
			ToolName:  tc.Function.Name,
			Arguments: make(map[string]any),
			Success:   true,
			Duration:  time.Since(startTime),
			Metadata: map[string]string{
				"executed_on_gateway": "true",
			},
		},
	}

	if err := s.conversationRepo.AddMessage(a2aEntry); err != nil {
		logger.Error("failed to store A2A tool execution in conversation", "error", err)
	}

	chatEvents <- domain.ToolCallCompleteEvent{
		RequestID:  requestID,
		Timestamp:  time.Now(),
		Success:    true,
		ToolCallID: tc.Id,
		ToolName:   tc.Function.Name,
		Result:     "executed on gateway",
	}
}

func (s *AgentServiceImpl) executeToolCall(ctx context.Context, tc sdk.ChatCompletionMessageToolCall, requestID string, chatEvents chan<- domain.ChatEvent) error {
	startTime := time.Now()

	chatEvents <- domain.ToolCallStartEvent{
		RequestID:     requestID,
		Timestamp:     startTime,
		ToolName:      tc.Function.Name,
		ToolArguments: tc.Function.Arguments,
	}

	var args map[string]any
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		logger.Error("failed to parse tool arguments", "tool", tc.Function.Name, "error", err)

		errorEntry := domain.ConversationEntry{
			Message: domain.Message{
				Role:       "tool",
				Content:    fmt.Sprintf("Tool call failed: %s - invalid arguments: %v", tc.Function.Name, err),
				ToolCallId: &tc.Id,
			},
			Time: time.Now(),
			ToolExecution: &domain.ToolExecutionResult{
				ToolName:  tc.Function.Name,
				Arguments: args,
				Success:   false,
				Duration:  time.Since(startTime),
				Error:     fmt.Sprintf("invalid tool arguments: %v", err),
			},
		}
		if err := s.conversationRepo.AddMessage(errorEntry); err != nil {
			logger.Error("failed to store tool error in conversation", "error", err)
		}

		chatEvents <- domain.ToolCallErrorEvent{
			RequestID:  requestID,
			Timestamp:  time.Now(),
			ToolCallID: tc.Id,
			ToolName:   tc.Function.Name,
			Error:      fmt.Errorf("invalid tool arguments: %w", err),
		}
		return fmt.Errorf("failed to parse tool arguments: %w", err)
	}

	if err := s.toolService.ValidateTool(tc.Function.Name, args); err != nil {
		logger.Error("tool validation failed", "tool", tc.Function.Name, "error", err)

		errorEntry := domain.ConversationEntry{
			Message: domain.Message{
				Role:       "tool",
				Content:    fmt.Sprintf("Tool validation failed: %s - %s", tc.Function.Name, err.Error()),
				ToolCallId: &tc.Id,
			},
			Time: time.Now(),
			ToolExecution: &domain.ToolExecutionResult{
				ToolName:  tc.Function.Name,
				Arguments: args,
				Success:   false,
				Duration:  time.Since(startTime),
				Error:     err.Error(),
			},
		}
		if err := s.conversationRepo.AddMessage(errorEntry); err != nil {
			logger.Error("failed to store tool validation error in conversation", "error", err)
		}

		chatEvents <- domain.ToolCallErrorEvent{
			RequestID:  requestID,
			Timestamp:  time.Now(),
			ToolCallID: tc.Id,
			ToolName:   tc.Function.Name,
			Error:      err,
		}
		return fmt.Errorf("tool validation failed: %w", err)
	}

	tcResult, err := s.toolService.ExecuteTool(ctx, tc.Function)
	if err != nil {
		logger.Error("failed to execute %s with %s", tc.Function.Name, tc.Function.Arguments)

		errorEntry := domain.ConversationEntry{
			Message: domain.Message{
				Role:       "tool",
				Content:    fmt.Sprintf("Tool execution failed: %s - %s", tc.Function.Name, err.Error()),
				ToolCallId: &tc.Id,
			},
			Time: time.Now(),
			ToolExecution: &domain.ToolExecutionResult{
				ToolName:  tc.Function.Name,
				Arguments: args,
				Success:   false,
				Duration:  time.Since(startTime),
				Error:     err.Error(),
			},
		}
		if err := s.conversationRepo.AddMessage(errorEntry); err != nil {
			logger.Error("failed to store tool execution error in conversation", "error", err)
		}

		chatEvents <- domain.ToolCallErrorEvent{
			RequestID:  requestID,
			Timestamp:  time.Now(),
			ToolCallID: tc.Id,
			ToolName:   tc.Function.Name,
			Error:      err,
		}
		return err
	}

	toolExecutionResult := &domain.ToolExecutionResult{
		ToolName:  tcResult.ToolName,
		Arguments: args,
		Success:   tcResult.Success,
		Duration:  time.Since(startTime),
		Data:      tcResult.Data,
		Metadata:  tcResult.Metadata,
		Diff:      tcResult.Diff,
	}

	formattedContent := s.conversationRepo.FormatToolResultForLLM(toolExecutionResult)

	successEntry := domain.ConversationEntry{
		Message: domain.Message{
			Role:       "tool",
			Content:    formattedContent,
			ToolCallId: &tc.Id,
		},
		Time: time.Now(),
		ToolExecution: &domain.ToolExecutionResult{
			ToolName:  tcResult.ToolName,
			Arguments: args,
			Success:   tcResult.Success,
			Duration:  time.Since(startTime),
			Data:      tcResult.Data,
			Metadata:  tcResult.Metadata,
			Diff:      tcResult.Diff,
		},
	}
	if err := s.conversationRepo.AddMessage(successEntry); err != nil {
		logger.Error("failed to store tool execution success in conversation", "error", err)
	}

	chatEvents <- domain.ToolCallCompleteEvent{
		RequestID:  requestID,
		Timestamp:  time.Now(),
		Success:    tcResult.Success,
		ToolCallID: tc.Id,
		ToolName:   tcResult.ToolName,
		Result:     tcResult.Data,
	}

	return nil
}
