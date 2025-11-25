package services

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
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
	messageQueue     domain.MessageQueue
	stateManager     domain.StateManager
	timeoutSeconds   int
	maxTokens        int
	optimizer        *ConversationOptimizer
	tokenizer        *TokenizerService

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

// publishBashOutputChunk publishes a BashOutputChunkEvent for streaming bash output
func (p *eventPublisher) publishBashOutputChunk(callID string, output string, isComplete bool) {
	event := domain.BashOutputChunkEvent{
		BaseChatEvent: domain.BaseChatEvent{
			RequestID: p.requestID,
			Timestamp: time.Now(),
		},
		ToolCallID: callID,
		Output:     output,
		IsComplete: isComplete,
	}

	select {
	case p.chatEvents <- event:
	default:
		logger.Warn("bash output chunk dropped - channel full")
	}
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

// publishTodoUpdate publishes a TodoUpdateChatEvent when TodoWrite tool executes
func (p *eventPublisher) publishTodoUpdate(todos []domain.TodoItem) {
	event := domain.TodoUpdateChatEvent{
		BaseChatEvent: domain.BaseChatEvent{
			RequestID: p.requestID,
			Timestamp: time.Now(),
		},
		Todos: todos,
	}

	select {
	case p.chatEvents <- event:
	default:
		logger.Warn("todo update event dropped - channel full")
	}
}

// NewAgentService creates a new agent service with pre-configured client
func NewAgentService(
	client sdk.Client,
	toolService domain.ToolService,
	config domain.ConfigService,
	conversationRepo domain.ConversationRepository,
	a2aAgentService domain.A2AAgentService,
	messageQueue domain.MessageQueue,
	stateManager domain.StateManager,
	timeoutSeconds int,
	optimizer *ConversationOptimizer,
) *AgentServiceImpl {
	tokenizer := NewTokenizerService(DefaultTokenizerConfig())

	return &AgentServiceImpl{
		client:           client,
		toolService:      toolService,
		config:           config,
		conversationRepo: conversationRepo,
		a2aAgentService:  a2aAgentService,
		messageQueue:     messageQueue,
		stateManager:     stateManager,
		timeoutSeconds:   timeoutSeconds,
		maxTokens:        config.GetAgentConfig().MaxTokens,
		optimizer:        optimizer,
		tokenizer:        tokenizer,
		activeRequests:   make(map[string]context.CancelFunc),
		cancelChannels:   make(map[string]chan struct{}),
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
				SkipMCP: true,
			})
		if s.toolService != nil {
			mode := domain.AgentModeStandard
			if s.stateManager != nil {
				mode = s.stateManager.GetAgentMode()
			}
			availableTools := s.toolService.ListToolsForMode(mode)
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
		contentStr, err := choice.Message.Content.AsMessageContent0()
		if err != nil {
			contentStr = ""
		}
		content = contentStr

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

// handleIdleState processes queued messages and background tasks when the agent is idle
func (s *AgentServiceImpl) handleIdleState(
	eventPublisher *eventPublisher,
	taskTracker domain.TaskTracker,
	conversation *[]sdk.Message,
) (shouldContinue bool, shouldReturn bool) {
	hasQueuedMessage := s.messageQueue != nil && !s.messageQueue.IsEmpty()
	hasBackgroundTasks := taskTracker != nil && len(taskTracker.GetAllPollingTasks()) > 0

	switch {
	case hasQueuedMessage:
		queuedMsg := s.messageQueue.Dequeue()
		if queuedMsg != nil {
			*conversation = append(*conversation, queuedMsg.Message)
			entry := domain.ConversationEntry{
				Message: queuedMsg.Message,
				Time:    time.Now(),
			}
			if err := s.conversationRepo.AddMessage(entry); err != nil {
				logger.Error("failed to store queued message", "error", err)
			}
			eventPublisher.chatEvents <- domain.MessageQueuedEvent{
				RequestID: queuedMsg.RequestID,
				Timestamp: time.Now(),
				Message:   queuedMsg.Message,
			}
			return true, false
		}
	case hasBackgroundTasks:
		time.Sleep(500 * time.Millisecond)
		return true, false
	default:
		eventPublisher.publishChatComplete([]sdk.ChatCompletionMessageToolCall{}, s.GetMetrics(eventPublisher.requestID))

		time.Sleep(100 * time.Millisecond)
		if s.messageQueue != nil && !s.messageQueue.IsEmpty() {
			logger.Info("Detected queued message after chat complete, continuing")
			return true, false
		}

		return false, true
	}
	return false, false
}

// RunWithStream executes an agent task with streaming (for interactive chat)
func (s *AgentServiceImpl) RunWithStream(ctx context.Context, req *domain.AgentRequest) (<-chan domain.ChatEvent, error) { // nolint:gocognit,gocyclo,cyclop,funlen
	if err := s.validateRequest(req); err != nil {
		return nil, err
	}

	chatEvents := make(chan domain.ChatEvent, 1000)
	eventPublisher := newEventPublisher(req.RequestID, chatEvents)

	cancelChan := make(chan struct{}, 1)
	s.cancelMux.Lock()
	s.cancelChannels[req.RequestID] = cancelChan
	s.cancelMux.Unlock()

	defer func() {
		s.cancelMux.Lock()
		delete(s.cancelChannels, req.RequestID)
		s.cancelMux.Unlock()
	}()

	conversation := s.addSystemPrompt(req.Messages)

	provider, model, err := s.parseProvider(req.Model)
	if err != nil {
		return nil, fmt.Errorf("failed to parse provider from model '%s': %w", model, err)
	}

	taskTracker := s.toolService.GetTaskTracker()
	var monitor *A2APollingMonitor

	if taskTracker != nil {
		monitor = NewA2APollingMonitor(taskTracker, chatEvents, s.messageQueue, req.RequestID, s.conversationRepo)
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

			if !hasToolResults && turns > 0 {
				shouldContinue, shouldReturn := s.handleIdleState(eventPublisher, taskTracker, &conversation)
				if shouldReturn {
					return
				}
				if shouldContinue {
					hasToolResults = true
					continue
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

			mode := domain.AgentModeStandard
			if s.stateManager != nil {
				mode = s.stateManager.GetAgentMode()
			}

			availableTools := s.toolService.ListToolsForMode(mode)

			client := s.client.
				WithOptions(&sdk.CreateChatCompletionRequest{
					MaxTokens: &s.maxTokens,
					StreamOptions: &sdk.ChatCompletionStreamOptions{
						IncludeUsage: true,
					},
				}).
				WithMiddlewareOptions(&sdk.MiddlewareOptions{
					SkipMCP: true,
				})
			if len(availableTools) > 0 {
				client = client.WithTools(&availableTools)
			}

			events, err := client.GenerateContentStream(requestCtx, sdk.Provider(provider), model, conversation)
			if err != nil {
				logger.Error("Failed to create stream",
					"error", err,
					"turn", turns,
					"conversationLength", len(conversation),
					"provider", provider)
				eventPublisher.chatEvents <- domain.ChatErrorEvent{
					RequestID: req.RequestID,
					Timestamp: time.Now(),
					Error:     err,
				}
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
						currentContent, err := message.Content.AsMessageContent0()
						if err != nil {
							currentContent = ""
						}
						message.Content = sdk.NewMessageContent(currentContent + deltaContent)
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

			assistantContent := message.Content
			if _, err := assistantContent.AsMessageContent0(); err != nil {
				assistantContent = sdk.NewMessageContent("")
			}

			assistantMessage := sdk.Message{
				Role:    sdk.Assistant,
				Content: assistantContent,
			}

			if len(toolCalls) > 0 {
				indices := make([]int, 0, len(toolCalls))
				for key := range toolCalls {
					var idx int
					_, _ = fmt.Sscanf(key, "%d", &idx)
					indices = append(indices, idx)
				}
				sort.Ints(indices)

				assistantToolCalls := make([]sdk.ChatCompletionMessageToolCall, 0, len(toolCalls))
				for _, idx := range indices {
					key := fmt.Sprintf("%d", idx)
					if tc, ok := toolCalls[key]; ok {
						assistantToolCalls = append(assistantToolCalls, *tc)
					}
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

			outputContent, _ := assistantContent.AsMessageContent0()

			polyfillInput := &storeIterationMetricsInput{
				inputMessages:   conversation[:len(conversation)-1],
				outputContent:   outputContent,
				outputToolCalls: completeToolCalls,
				availableTools:  availableTools,
			}

			s.storeIterationMetrics(req.RequestID, iterationStartTime, streamUsage, polyfillInput)

			if len(toolCalls) > 0 {
				toolCallsSlice := make([]*sdk.ChatCompletionMessageToolCall, 0, len(toolCalls))
				for _, tc := range toolCalls {
					toolCallsSlice = append(toolCallsSlice, tc)
				}

				toolResults := s.executeToolCallsParallel(ctx, toolCallsSlice, eventPublisher, req.IsChatMode)

				hasRejection := false
				for _, entry := range toolResults {
					if entry.ToolExecution != nil && entry.ToolExecution.Rejected {
						hasRejection = true
						break
					}
				}

				for _, entry := range toolResults {
					toolResult := sdk.Message{
						Role:       sdk.Tool,
						Content:    entry.Message.Content,
						ToolCallId: entry.Message.ToolCallId,
					}
					conversation = append(conversation, toolResult)
				}

				if hasRejection {
					eventPublisher.publishChatComplete([]sdk.ChatCompletionMessageToolCall{}, s.GetMetrics(req.RequestID))
					return
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

// storeIterationMetricsInput holds the data needed for token usage polyfill calculation
type storeIterationMetricsInput struct {
	inputMessages   []sdk.Message
	outputContent   string
	outputToolCalls []sdk.ChatCompletionMessageToolCall
	availableTools  []sdk.ChatCompletionTool
}

// storeIterationMetrics stores metrics for the current iteration and accumulates session tokens.
// If the provider doesn't return usage metrics, it uses the tokenizer polyfill to estimate them.
func (s *AgentServiceImpl) storeIterationMetrics(
	requestID string,
	startTime time.Time,
	usage *sdk.CompletionUsage,
	polyfillInput *storeIterationMetricsInput,
) {
	effectiveUsage := usage

	if s.tokenizer != nil && s.tokenizer.ShouldUsePolyfill(usage) && polyfillInput != nil {
		effectiveUsage = s.tokenizer.CalculateUsagePolyfill(
			polyfillInput.inputMessages,
			polyfillInput.outputContent,
			polyfillInput.outputToolCalls,
			polyfillInput.availableTools,
		)
	}

	if effectiveUsage == nil {
		return
	}

	metrics := &domain.ChatMetrics{
		Duration: time.Since(startTime),
		Usage:    effectiveUsage,
	}

	s.metricsMux.Lock()
	s.metrics[requestID] = metrics
	s.metricsMux.Unlock()

	if err := s.conversationRepo.AddTokenUsage(
		int(effectiveUsage.PromptTokens),
		int(effectiveUsage.CompletionTokens),
		int(effectiveUsage.TotalTokens),
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
	isChatMode bool,
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

	var approvalTools []struct {
		index int
		tool  *sdk.ChatCompletionMessageToolCall
	}
	var parallelTools []struct {
		index int
		tool  *sdk.ChatCompletionMessageToolCall
	}

	for i, tc := range toolCalls {
		requiresApproval := s.shouldRequireApproval(tc, isChatMode)
		if requiresApproval {
			approvalTools = append(approvalTools, struct {
				index int
				tool  *sdk.ChatCompletionMessageToolCall
			}{i, tc})
		} else {
			parallelTools = append(parallelTools, struct {
				index int
				tool  *sdk.ChatCompletionMessageToolCall
			}{i, tc})
		}
	}

	for _, at := range approvalTools {
		eventPublisher.publishToolStatusChange(
			at.tool.Id,
			"starting",
			fmt.Sprintf("Initializing %s...", at.tool.Function.Name),
		)

		time.Sleep(constants.AgentToolExecutionDelay)

		result := s.executeToolWithFlashingUI(ctx, *at.tool, eventPublisher, isChatMode)

		status := "complete"
		message := "Completed successfully"
		if result.ToolExecution != nil && !result.ToolExecution.Success {
			status = "failed"
			message = "Execution failed"
		}

		eventPublisher.publishToolStatusChange(at.tool.Id, status, message)
		results[at.index] = result
	}

	if len(parallelTools) > 0 {
		resultsChan := make(chan IndexedToolResult, len(parallelTools))
		semaphore := make(chan struct{}, s.config.GetAgentConfig().MaxConcurrentTools)

		var wg sync.WaitGroup
		for _, pt := range parallelTools {
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

				result := s.executeToolWithFlashingUI(ctx, *toolCall, eventPublisher, isChatMode)

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
			}(pt.index, pt.tool)
		}

		go func() {
			wg.Wait()
			close(resultsChan)
		}()

		for res := range resultsChan {
			results[res.Index] = res.Result
		}
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
	isChatMode bool,
) domain.ConversationEntry {

	startTime := time.Now()

	requiresApproval := s.shouldRequireApproval(&tc, isChatMode)
	wasApproved := false
	isAutoAcceptMode := s.stateManager != nil && s.stateManager.GetAgentMode() == domain.AgentModeAutoAccept
	if isAutoAcceptMode {
		wasApproved = true
	} else if requiresApproval {
		approved, err := s.requestToolApproval(ctx, tc, eventPublisher)
		if err != nil {
			logger.Error("failed to request tool approval", "tool", tc.Function.Name, "error", err)
			s.conversationRepo.RemovePendingToolCallByID(tc.Id)
			return s.createErrorEntry(tc, err, startTime)
		}
		if !approved {
			s.conversationRepo.RemovePendingToolCallByID(tc.Id)
			return s.createRejectionEntry(tc, startTime)
		}
		wasApproved = true
	}

	eventPublisher.publishToolStatusChange(tc.Id, "running", "Executing...")

	time.Sleep(constants.AgentToolExecutionDelay)

	if !isCompleteJSON(tc.Function.Arguments) {
		incompleteErr := fmt.Errorf(
			"TOOL FAILED: %s - content was truncated due to output token limits (received %d chars of incomplete JSON). %s",
			tc.Function.Name, len(tc.Function.Arguments), getTruncationRecoveryGuidance(tc.Function.Name),
		)
		logger.Error("incomplete JSON in tool arguments",
			"tool", tc.Function.Name,
			"args_length", len(tc.Function.Arguments),
			"args_preview", truncateString(tc.Function.Arguments, 200),
		)
		return s.createErrorEntry(tc, incompleteErr, startTime)
	}

	var args map[string]any
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		logger.Error("failed to parse tool arguments", "tool", tc.Function.Name, "error", err)
		return s.createErrorEntry(tc, err, startTime)
	}

	if !wasApproved {
		if err := s.toolService.ValidateTool(tc.Function.Name, args); err != nil {
			logger.Error("tool validation failed", "tool", tc.Function.Name, "error", err)
			return s.createErrorEntry(tc, err, startTime)
		}
	}

	execCtx := ctx
	if wasApproved {
		execCtx = context.WithValue(ctx, domain.ToolApprovedKey, true)
	}

	if tc.Function.Name == "Bash" {
		bashCallback := func(line string) {
			eventPublisher.publishBashOutputChunk(tc.Id, line, false)
		}
		execCtx = context.WithValue(execCtx, domain.BashOutputCallbackKey, domain.BashOutputCallback(bashCallback))
	}

	resultChan := make(chan struct {
		result *domain.ToolExecutionResult
		err    error
	}, 1)

	go func() {
		result, err := s.toolService.ExecuteTool(execCtx, tc.Function)
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

	if result.ToolName == "TodoWrite" && result.Success {
		if todoResult, ok := result.Data.(*domain.TodoWriteToolResult); ok && todoResult != nil {
			eventPublisher.publishTodoUpdate(todoResult.Todos)
		}
	}

	formattedContent := s.conversationRepo.FormatToolResultForLLM(toolExecutionResult)

	entry := domain.ConversationEntry{
		Message: domain.Message{
			Role:       sdk.Tool,
			Content:    sdk.NewMessageContent(formattedContent),
			ToolCallId: &tc.Id,
		},
		Time:          time.Now(),
		ToolExecution: toolExecutionResult,
	}

	if requiresApproval {
		s.conversationRepo.RemovePendingToolCallByID(tc.Id)
	}

	return entry
}

// requestToolApproval requests user approval for a tool and waits for response
func (s *AgentServiceImpl) requestToolApproval(
	ctx context.Context,
	tc sdk.ChatCompletionMessageToolCall,
	eventPublisher *eventPublisher,
) (bool, error) {
	responseChan := make(chan domain.ApprovalAction, 1)

	eventPublisher.chatEvents <- domain.ToolApprovalRequestedEvent{
		RequestID:    eventPublisher.requestID,
		Timestamp:    time.Now(),
		ToolCall:     tc,
		ResponseChan: responseChan,
	}

	select {
	case response := <-responseChan:
		return response == domain.ApprovalApprove, nil
	case <-ctx.Done():
		return false, fmt.Errorf("approval request cancelled: %w", ctx.Err())
	case <-time.After(5 * time.Minute):
		return false, fmt.Errorf("approval request timed out")
	}
}

// isBashCommandWhitelisted checks if a Bash tool command is whitelisted
func (s *AgentServiceImpl) isBashCommandWhitelisted(tc *sdk.ChatCompletionMessageToolCall) bool {
	var args map[string]any
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		logger.Debug("Tool approval required - failed to parse bash arguments", "tool", tc.Function.Name, "error", err)
		return false
	}

	command, ok := args["command"].(string)
	if !ok {
		logger.Debug("Tool approval required - command not found in bash arguments", "tool", tc.Function.Name)
		return false
	}

	isWhitelisted := s.config.IsBashCommandWhitelisted(command)
	if isWhitelisted {
		logger.Debug("Tool approval not required - bash command is whitelisted", "tool", tc.Function.Name, "command", command)
	} else {
		logger.Debug("Tool approval required - bash command not whitelisted", "tool", tc.Function.Name, "command", command)
	}
	return isWhitelisted
}

// shouldRequireApproval determines if a tool execution requires user approval
// For Bash tool specifically, it checks if the command is whitelisted
func (s *AgentServiceImpl) shouldRequireApproval(tc *sdk.ChatCompletionMessageToolCall, isChatMode bool) bool {
	if s.stateManager != nil && s.stateManager.GetAgentMode() == domain.AgentModeAutoAccept {
		logger.Debug("Tool approval not required - auto-accept mode enabled", "tool", tc.Function.Name)
		return false
	}

	if !isChatMode {
		logger.Debug("Tool approval not required - not in chat mode", "tool", tc.Function.Name)
		return false
	}

	if tc.Function.Name == "Bash" {
		return !s.isBashCommandWhitelisted(tc)
	}

	requiresApproval := s.config.IsApprovalRequired(tc.Function.Name)
	if requiresApproval {
		logger.Debug("Tool approval required - tool configured to require approval", "tool", tc.Function.Name)
	} else {
		logger.Debug("Tool approval not required - tool configured to not require approval", "tool", tc.Function.Name)
	}
	return requiresApproval
}

func (s *AgentServiceImpl) createErrorEntry(tc sdk.ChatCompletionMessageToolCall, err error, startTime time.Time) domain.ConversationEntry {
	return domain.ConversationEntry{
		Message: domain.Message{
			Role:       sdk.Tool,
			Content:    sdk.NewMessageContent(fmt.Sprintf("Tool execution failed: %s - %s", tc.Function.Name, err.Error())),
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

func (s *AgentServiceImpl) createRejectionEntry(tc sdk.ChatCompletionMessageToolCall, startTime time.Time) domain.ConversationEntry {
	var args map[string]any
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		args = make(map[string]any)
	}

	rejectionMessage := fmt.Sprintf(
		"Tool call rejected by user: %s\n\nYou can provide alternative instructions or ask me to proceed differently.",
		tc.Function.Name,
	)

	return domain.ConversationEntry{
		Message: domain.Message{
			Role:       sdk.Tool,
			Content:    sdk.NewMessageContent(rejectionMessage),
			ToolCallId: &tc.Id,
		},
		Time: time.Now(),
		ToolExecution: &domain.ToolExecutionResult{
			ToolName:  tc.Function.Name,
			Arguments: args,
			Success:   false,
			Duration:  time.Since(startTime),
			Error:     "rejected by user",
			Rejected:  true,
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
