package services

import (
	"context"
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
func (s *AgentServiceImpl) RunWithStream(ctx context.Context, req *domain.AgentRequest) (<-chan domain.ChatEvent, error) {
	if err := s.validateRequest(req); err != nil {
		return nil, err
	}

	_ = time.Now()

	chatEvents := make(chan domain.ChatEvent, 100)

	// Step 1 - Add system prompt
	systemPrompt := s.config.GetAgentConfig().SystemPrompt
	if systemPrompt == "" {
		systemPrompt = "You are an helpful assistant."
	}

	// Step 2 - Create an SDK client and add tools if enabled
	client := sdk.NewClient(&sdk.ClientOptions{}).
		WithMiddlewareOptions(&sdk.MiddlewareOptions{
			SkipMCP: true,
			SkipA2A: true,
		})
	availableTools := s.toolService.ListTools()
	if len(availableTools) > 0 {
		client = client.WithTools(&availableTools)
	}
	// Step 3 - Prepare a request to the LLM with the user's intent
	conversation := []sdk.Message{
		{Role: "system", Content: systemPrompt},
	}
	conversation = append(conversation, req.Messages...)

	// Step 4 - Parse the provider and the model
	provider, model, err := s.parseProvider(req.Model)
	if err != nil {
		return nil, fmt.Errorf("failed to parse provider from model '%s': %w", model, err)
	}
	// Step 5 - Start agent event loop with max iteration from the config:
	turns := 0
	maxTurns := s.config.GetAgentConfig().MaxTurns
	toolcalls := []sdk.ChatCompletionMessageToolCall{}
	go func() {
		// Optimize conversations using the optimizer (based on the message count and the configurations)
		if s.optimizer != nil && s.config.GetAgentConfig().Optimization.Enabled {
			originalCount := len(conversation)

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
				logger.Debug("optimized conversation", "original_count", originalCount, "optimized_count", optimizedCount)
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

			// TODO 3 - store the optimized conversation in the database on a separate column - UI will stillWI see all the conversation related to the session but the optimized conversation will be sent to the LLM
		}
		//// EVENT LOOP START
		for maxTurns > turns {
			_, err := client.GenerateContentStream(ctx, sdk.Provider(provider), model, conversation)
			if err != nil {
				logger.Error("failed to create a stream, %w", err)
			}
			// Step 1 - Inject User's System reminder into the conversation as a hidden message and store it in the database
			// Step 2 - When there are tool calls, call tcs := accumulateToolCalls to collect the full definitions
			// Step 3 - Pass the return value from err := s.toolService.ExecuteTool(ctx, tc.ChatCompletionMessageToolCall)
			// Step 4 - Handle error for each tool call
			// Step 5 - Before processing a tool_call - store it to the conversations database and submit an event to the UI about tool call starting
			// Step 6 - When the tool call is complete successfully or with errors - store it to the conversations database and submit an event to the UI about tool call completed
			// Step 7 - When there is Reasoning or ReasoningContent - submit an event to the UI
			// Step 8 - When there is standard content delta and tool_calls == empty(we check at the beginning and continue if there are tool calls) - submit a content delta event to the UI and store the final message in the database
			// Step 9 - Save the token usage per iteration to the database
			if len(toolcalls) == 0 {
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
