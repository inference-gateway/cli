package services

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	constants "github.com/inference-gateway/cli/internal/constants"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	sdk "github.com/inference-gateway/sdk"
)

// AgentServiceImpl implements the AgentService interface with direct chat functionality
type AgentServiceImpl struct {
	client           sdk.Client
	toolService      domain.ToolService
	config           domain.ConfigService
	conversationRepo domain.ConversationRepository
	a2aAgentService  domain.A2AAgentService
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

	// Input message queue for all conversation inputs
	messageQueues map[string]chan sdk.Message
	queuesMux     sync.RWMutex
}

// eventPublisher provides a utility for publishing chat events
type eventPublisher struct {
	requestID  string
	chatEvents chan<- domain.ChatEvent
}

// newEventPublisher creates a new event publisher for the given request
func newEventPublisher(requestID string, chatEvents chan<- domain.ChatEvent) *eventPublisher {
	return &eventPublisher{
		requestID:  requestID,
		chatEvents: chatEvents,
	}
}

// publishChatStart publishes a ChatStartEvent
func (p *eventPublisher) publishChatStart() {
	p.chatEvents <- domain.ChatStartEvent{
		RequestID: p.requestID,
		Timestamp: time.Now(),
	}
}

// publishChatComplete publishes a ChatCompleteEvent
func (p *eventPublisher) publishChatComplete(toolCalls []sdk.ChatCompletionMessageToolCall, metrics *domain.ChatMetrics) {
	p.chatEvents <- domain.ChatCompleteEvent{
		RequestID: p.requestID,
		Timestamp: time.Now(),
		ToolCalls: toolCalls,
		Metrics:   metrics,
	}
}

// publishChatChunk publishes a ChatChunkEvent
func (p *eventPublisher) publishChatChunk(content, reasoningContent string, toolCalls []sdk.ChatCompletionMessageToolCallChunk) {
	p.chatEvents <- domain.ChatChunkEvent{
		RequestID:        p.requestID,
		Timestamp:        time.Now(),
		ReasoningContent: reasoningContent,
		Content:          content,
		Delta:            true,
		ToolCalls:        toolCalls,
	}
}

// publishOptimizationStatus publishes an OptimizationStatusEvent
func (p *eventPublisher) publishOptimizationStatus(message string, isActive bool, originalCount, optimizedCount int) {
	p.chatEvents <- domain.OptimizationStatusEvent{
		RequestID:      p.requestID,
		Timestamp:      time.Now(),
		Message:        message,
		IsActive:       isActive,
		OriginalCount:  originalCount,
		OptimizedCount: optimizedCount,
	}
}

// publishParallelToolsStart publishes a ParallelToolsStartEvent
func (p *eventPublisher) publishParallelToolsStart(toolCalls []sdk.ChatCompletionMessageToolCall) {
	tools := make([]domain.ToolInfo, len(toolCalls))
	for i, tc := range toolCalls {
		tools[i] = domain.ToolInfo{
			CallID: tc.Id,
			Name:   tc.Function.Name,
			Status: "queued",
		}
	}

	event := domain.ParallelToolsStartEvent{
		BaseChatEvent: domain.BaseChatEvent{
			RequestID: p.requestID,
			Timestamp: time.Now(),
		},
		Tools: tools,
	}

	p.chatEvents <- event
}

// publishToolStatusChange publishes a ToolExecutionProgressEvent
func (p *eventPublisher) publishToolStatusChange(callID string, status string, message string) {
	event := domain.ToolExecutionProgressEvent{
		BaseChatEvent: domain.BaseChatEvent{
			RequestID: p.requestID,
			Timestamp: time.Now(),
		},
		ToolCallID: callID,
		Status:     status,
		Message:    message,
	}

	p.chatEvents <- event
}

// publishParallelToolsComplete publishes a ParallelToolsCompleteEvent
func (p *eventPublisher) publishParallelToolsComplete(totalExecuted, successCount, failureCount int, duration time.Duration) {
	event := domain.ParallelToolsCompleteEvent{
		BaseChatEvent: domain.BaseChatEvent{
			RequestID: p.requestID,
			Timestamp: time.Now(),
		},
		TotalExecuted: totalExecuted,
		SuccessCount:  successCount,
		FailureCount:  failureCount,
		Duration:      duration,
	}

	p.chatEvents <- event
}

// NewAgentService creates a new agent service with pre-configured client
func NewAgentService(
	client sdk.Client,
	toolService domain.ToolService,
	config domain.ConfigService,
	conversationRepo domain.ConversationRepository,
	a2aAgentService domain.A2AAgentService,
	timeoutSeconds int,
	optimizer *ConversationOptimizer,
) *AgentServiceImpl {
	return &AgentServiceImpl{
		client:           client,
		toolService:      toolService,
		config:           config,
		conversationRepo: conversationRepo,
		a2aAgentService:  a2aAgentService,
		timeoutSeconds:   timeoutSeconds,
		maxTokens:        config.GetAgentConfig().MaxTokens,
		optimizer:        optimizer,
		activeRequests:   make(map[string]context.CancelFunc),
		cancelChannels:   make(map[string]chan struct{}),
		metrics:          make(map[string]*domain.ChatMetrics),
		toolCallsMap:     make(map[string]*sdk.ChatCompletionMessageToolCall),
		messageQueues:    make(map[string]chan sdk.Message),
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
				SkipMCP: true,
				SkipA2A: true,
			})
		if s.toolService != nil {
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

	chatEvents := make(chan domain.ChatEvent, 1000)
	eventPublisher := newEventPublisher(req.RequestID, chatEvents)

	messageQueue := make(chan sdk.Message, 10)
	s.queuesMux.Lock()
	s.messageQueues[req.RequestID] = messageQueue
	s.queuesMux.Unlock()

	cancelChan := make(chan struct{}, 1)
	s.cancelMux.Lock()
	s.cancelChannels[req.RequestID] = cancelChan
	s.cancelMux.Unlock()

	defer func() {
		s.cancelMux.Lock()
		delete(s.cancelChannels, req.RequestID)
		s.cancelMux.Unlock()

		s.queuesMux.Lock()
		delete(s.messageQueues, req.RequestID)
		s.queuesMux.Unlock()
	}()

	client := s.client.
		WithMiddlewareOptions(&sdk.MiddlewareOptions{
			SkipMCP: true,
			SkipA2A: true,
		})
	availableTools := s.toolService.ListTools()
	if len(availableTools) > 0 {
		client = client.WithTools(&availableTools)
	}

	conversation := s.addSystemPrompt(req.Messages)

	provider, model, err := s.parseProvider(req.Model)
	if err != nil {
		return nil, fmt.Errorf("failed to parse provider from model '%s': %w", model, err)
	}

	taskTracker := s.toolService.GetTaskTracker()
	var monitor *A2APollingMonitor

	if taskTracker != nil {
		monitor = NewA2APollingMonitor(taskTracker, chatEvents, messageQueue, req.RequestID)
		go monitor.Start(ctx)
	}

	go func() {
		defer func() {
			if monitor != nil {
				monitor.Stop()
			}
			close(chatEvents)
		}()

		conversation = s.optimizeConversation(ctx, req, conversation, eventPublisher)

		turns := 0
		maxTurns := s.config.GetAgentConfig().MaxTurns
		hasToolResults := false

		//// EVENT LOOP START
		for maxTurns > turns {
			select {
			case <-cancelChan:
				eventPublisher.publishChatComplete([]sdk.ChatCompletionMessageToolCall{}, s.GetMetrics(req.RequestID))
				return
			default:
			}

			hasActivePolling := taskTracker != nil && len(taskTracker.GetAllPollingTasks()) > 0

			if !hasToolResults && turns > 0 {
				if !hasActivePolling {
					eventPublisher.publishChatComplete([]sdk.ChatCompletionMessageToolCall{}, s.GetMetrics(req.RequestID))
					return
				}

				maxWaitTime := 5 * time.Minute
				waitTimeout := time.NewTimer(maxWaitTime)

				select {
				case msg := <-messageQueue:
					conversation = append(conversation, msg)

					entry := domain.ConversationEntry{
						Message: msg,
						Time:    time.Now(),
					}
					if err := s.conversationRepo.AddMessage(entry); err != nil {
						logger.Error("failed to store queued message", "error", err)
					}

					eventPublisher.chatEvents <- domain.MessageQueuedEvent{
						RequestID: req.RequestID,
						Timestamp: time.Now(),
						Message:   msg,
					}

					waitTimeout.Stop()

				case <-waitTimeout.C:
					return

				case <-cancelChan:
					waitTimeout.Stop()
					eventPublisher.publishChatComplete([]sdk.ChatCompletionMessageToolCall{}, s.GetMetrics(req.RequestID))
					return
				}
			}

			hasToolResults = false
			turns++
			s.clearToolCallsMap()
			iterationStartTime := time.Now()

			if turns > 1 {
				time.Sleep(constants.AgentIterationDelay)
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

			eventPublisher.publishChatStart()

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
				logger.Error("Failed to create stream",
					"error", err,
					"turn", turns,
					"conversationLength", len(conversation),
					"provider", provider)
				return
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
						eventPublisher.publishChatChunk(deltaContent, reasoning, choice.Delta.ToolCalls)
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

			s.storeIterationMetrics(req.RequestID, iterationStartTime, streamUsage)

			if len(toolCalls) > 0 {
				toolCallsSlice := make([]*sdk.ChatCompletionMessageToolCall, 0, len(toolCalls))
				for _, tc := range toolCalls {
					toolCallsSlice = append(toolCallsSlice, tc)
				}

				toolResults := s.executeToolCallsParallel(ctx, toolCallsSlice, eventPublisher)

				for _, entry := range toolResults {
					toolResult := sdk.Message{
						Role:       sdk.Tool,
						Content:    entry.Message.Content,
						ToolCallId: entry.Message.ToolCallId,
					}
					conversation = append(conversation, toolResult)
				}
				hasToolResults = true
			}

			eventPublisher.publishChatComplete(completeToolCalls, s.GetMetrics(req.RequestID))
		}
		//// EVENT LOOP FINISHED
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

func (s *AgentServiceImpl) optimizeConversation(ctx context.Context, req *domain.AgentRequest, conversation []sdk.Message, eventPublisher *eventPublisher) []sdk.Message {
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

	eventPublisher.publishOptimizationStatus("Optimizing conversation history...", true, originalCount, originalCount)

	conversation = s.optimizer.OptimizeMessagesWithModel(conversation, req.Model)
	optimizedCount := len(conversation)

	var message string
	if originalCount != optimizedCount {
		message = fmt.Sprintf("Conversation optimized (%d → %d messages)", originalCount, optimizedCount)
	} else {
		message = "Conversation optimization completed"
	}

	eventPublisher.publishOptimizationStatus(message, false, originalCount, optimizedCount)

	if isPersistent {
		if err := persistentRepo.SetOptimizedMessages(ctx, conversation); err != nil {
			logger.Error("Failed to save optimized conversation", "error", err)
		}
	}

	return conversation
}

type IndexedToolResult struct {
	Index  int
	Result domain.ConversationEntry
}

func (s *AgentServiceImpl) executeToolCallsParallel(
	ctx context.Context,
	toolCalls []*sdk.ChatCompletionMessageToolCall,
	eventPublisher *eventPublisher,
) []domain.ConversationEntry {

	if len(toolCalls) == 0 {
		return []domain.ConversationEntry{}
	}

	startTime := time.Now()

	eventPublisher.publishParallelToolsStart(func() []sdk.ChatCompletionMessageToolCall {
		calls := make([]sdk.ChatCompletionMessageToolCall, len(toolCalls))
		for i, tc := range toolCalls {
			calls[i] = *tc
		}
		return calls
	}())

	time.Sleep(constants.AgentToolExecutionDelay)

	results := make([]domain.ConversationEntry, len(toolCalls))
	resultsChan := make(chan IndexedToolResult, len(toolCalls))

	semaphore := make(chan struct{}, s.config.GetAgentConfig().MaxConcurrentTools)

	var wg sync.WaitGroup
	for i, tc := range toolCalls {
		wg.Add(1)
		go func(index int, toolCall *sdk.ChatCompletionMessageToolCall) {
			defer func() {
				wg.Done()
			}()

			semaphore <- struct{}{}
			defer func() {
				<-semaphore
			}()

			eventPublisher.publishToolStatusChange(
				toolCall.Id,
				"starting",
				fmt.Sprintf("Initializing %s...", toolCall.Function.Name),
			)

			time.Sleep(constants.AgentToolExecutionDelay)

			result := s.executeToolWithFlashingUI(ctx, *toolCall, eventPublisher)

			status := "complete"
			message := "Completed successfully"
			if result.ToolExecution != nil && !result.ToolExecution.Success {
				status = "failed"
				message = "Execution failed"
			}

			eventPublisher.publishToolStatusChange(toolCall.Id, status, message)

			resultsChan <- IndexedToolResult{
				Index:  index,
				Result: result,
			}
		}(i, tc)
	}

	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	resultCount := 0
	for res := range resultsChan {
		resultCount++
		results[res.Index] = res.Result
	}

	duration := time.Since(startTime)
	successCount := 0
	failureCount := 0

	for _, result := range results {
		if result.ToolExecution != nil && result.ToolExecution.Success {
			successCount++
		} else {
			failureCount++
		}
	}

	eventPublisher.publishParallelToolsComplete(len(toolCalls), successCount, failureCount, duration)

	if err := s.batchSaveToolResults(results); err != nil {
		logger.Error("failed to batch save tool results", "error", err)
	}

	return results
}

func (s *AgentServiceImpl) executeToolWithFlashingUI(
	ctx context.Context,
	tc sdk.ChatCompletionMessageToolCall,
	eventPublisher *eventPublisher,
) domain.ConversationEntry {

	startTime := time.Now()

	eventPublisher.publishToolStatusChange(tc.Id, "running", "Executing...")

	time.Sleep(constants.AgentToolExecutionDelay)

	var args map[string]any
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		logger.Error("failed to parse tool arguments", "tool", tc.Function.Name, "error", err)
		return s.createErrorEntry(tc, err, startTime)
	}

	if err := s.toolService.ValidateTool(tc.Function.Name, args); err != nil {
		logger.Error("tool validation failed", "tool", tc.Function.Name, "error", err)
		return s.createErrorEntry(tc, err, startTime)
	}

	resultChan := make(chan struct {
		result *domain.ToolExecutionResult
		err    error
	}, 1)

	go func() {
		result, err := s.toolService.ExecuteTool(ctx, tc.Function)
		resultChan <- struct {
			result *domain.ToolExecutionResult
			err    error
		}{result, err}
	}()

	ticker := time.NewTicker(constants.AgentStatusTickerInterval)
	defer ticker.Stop()

	var result *domain.ToolExecutionResult
	var err error

	for {
		select {
		case res := <-resultChan:
			result = res.result
			err = res.err
			ticker.Stop()
			goto done
		case <-ticker.C:
			eventPublisher.publishToolStatusChange(tc.Id, "running", "Processing...")
		case <-ctx.Done():
			logger.Error("tool execution cancelled", "tool", tc.Function.Name)
			return s.createErrorEntry(tc, ctx.Err(), startTime)
		}
	}

done:
	if err != nil {
		logger.Error("failed to execute tool", "tool", tc.Function.Name, "error", err)
		return s.createErrorEntry(tc, err, startTime)
	}

	eventPublisher.publishToolStatusChange(tc.Id, "saving", "Saving results...")

	time.Sleep(constants.AgentToolExecutionDelay)

	toolExecutionResult := &domain.ToolExecutionResult{
		ToolName:  result.ToolName,
		Arguments: args,
		Success:   result.Success,
		Duration:  time.Since(startTime),
		Data:      result.Data,
		Metadata:  result.Metadata,
		Diff:      result.Diff,
		Error:     result.Error,
	}

	formattedContent := s.conversationRepo.FormatToolResultForLLM(toolExecutionResult)

	entry := domain.ConversationEntry{
		Message: domain.Message{
			Role:       sdk.Tool,
			Content:    formattedContent,
			ToolCallId: &tc.Id,
		},
		Time:          time.Now(),
		ToolExecution: toolExecutionResult,
	}

	return entry
}

func (s *AgentServiceImpl) createErrorEntry(tc sdk.ChatCompletionMessageToolCall, err error, startTime time.Time) domain.ConversationEntry {
	return domain.ConversationEntry{
		Message: domain.Message{
			Role:       sdk.Tool,
			Content:    fmt.Sprintf("Tool execution failed: %s - %s", tc.Function.Name, err.Error()),
			ToolCallId: &tc.Id,
		},
		Time: time.Now(),
		ToolExecution: &domain.ToolExecutionResult{
			ToolName:  tc.Function.Name,
			Arguments: make(map[string]any),
			Success:   false,
			Duration:  time.Since(startTime),
			Error:     err.Error(),
		},
	}
}

func (s *AgentServiceImpl) batchSaveToolResults(entries []domain.ConversationEntry) error {
	savedCount := 0
	for _, entry := range entries {
		if err := s.conversationRepo.AddMessage(entry); err != nil {
			logger.Error("failed to save tool result",
				"tool", entry.ToolExecution.ToolName,
				"error", err,
			)
			return err
		}
		savedCount++
	}

	return nil
}
