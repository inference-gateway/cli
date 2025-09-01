package services

import (
	"context"
	"encoding/json"
	"fmt"
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

	// Metrics tracking
	metrics    map[string]*domain.ChatMetrics
	metricsMux sync.RWMutex

	// Tool call accumulation
	toolCallsMap map[string]*sdk.ChatCompletionMessageToolCall
	toolCallsMux sync.RWMutex
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
		metrics:          make(map[string]*domain.ChatMetrics),
		toolCallsMap:     make(map[string]*sdk.ChatCompletionMessageToolCall),
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

	_ = time.Now()

	chatEvents := make(chan domain.ChatEvent, 100)

	systemPrompt := s.config.GetAgentConfig().SystemPrompt
	if systemPrompt == "" {
		systemPrompt = "You are an helpful assistant."
	}

	client := s.client.
		WithMiddlewareOptions(&sdk.MiddlewareOptions{
			SkipMCP: !s.config.ShouldSkipMCPToolOnClient(),
			SkipA2A: !s.config.ShouldSkipA2AToolOnClient(),
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
			s.clearToolCallsMap()

			if turns > 0 {
				time.Sleep(100 * time.Millisecond)
			}

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

			events, err := client.GenerateContentStream(ctx, sdk.Provider(provider), model, conversation)
			if err != nil {
				logger.Error("failed to create a stream, %w", err)
			}

			var allToolCallDeltas []sdk.ChatCompletionMessageToolCallChunk
			var message sdk.Message
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

			chatEvents <- domain.ChatCompleteEvent{
				RequestID: req.RequestID,
				Timestamp: time.Now(),
				ToolCalls: completeToolCalls,
			}

			for _, tc := range toolCalls {
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
					continue
				}

				lastMessage := messages[len(messages)-1]
				if lastMessage.Message.Role != sdk.Tool {
					continue
				}

				toolResult := sdk.Message{
					Role:       sdk.Tool,
					Content:    lastMessage.Message.Content,
					ToolCallId: &tc.Id,
				}
				conversation = append(conversation, toolResult)
			}

			// Step 8 - Save the token usage per iteration to the database

			if len(toolCalls) == 0 {
				// The agent after responding to the user intent doesn't want to call any tools - meaning it's finished processing

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
	cancel, exists := s.activeRequests[requestID]
	s.requestsMux.RUnlock()

	if !exists {
		return fmt.Errorf("request %s not found or already completed", requestID)
	}

	defer cancel()
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
				Role:    "assistant",
				Content: fmt.Sprintf("Tool call failed: %s - invalid arguments", tc.Function.Name),
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
				Role:    "assistant",
				Content: fmt.Sprintf("Tool validation failed: %s", tc.Function.Name),
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
				Role:    "assistant",
				Content: fmt.Sprintf("Tool execution failed: %s", tc.Function.Name),
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
