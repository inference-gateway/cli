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
	client           domain.SDKClient
	toolService      domain.ToolService
	config           domain.ConfigService
	conversationRepo domain.ConversationRepository
	a2aAgentService  domain.A2AAgentService
	messageQueue     domain.MessageQueue
	stateManager     domain.StateManager
	timeoutSeconds   int
	maxTokens        int
	optimizer        domain.ConversationOptimizer
	tokenizer        *TokenizerService
	approvalPolicy   domain.ApprovalPolicy

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

	// Context caching
	gitContextCache string
	gitContextTurn  int
	contextCacheMux sync.RWMutex
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
func (p *eventPublisher) publishChatComplete(reasoning string, toolCalls []sdk.ChatCompletionMessageToolCall, metrics *domain.ChatMetrics) {
	p.chatEvents <- domain.ChatCompleteEvent{
		RequestID:        p.requestID,
		Timestamp:        time.Now(),
		ReasoningContent: reasoning,
		ToolCalls:        toolCalls,
		Metrics:          metrics,
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

// publishToolsQueued publishes individual ToolExecutionProgressEvent for each queued tool
func (p *eventPublisher) publishToolsQueued(toolCalls []sdk.ChatCompletionMessageToolCall) {
	for _, tc := range toolCalls {
		event := domain.ToolExecutionProgressEvent{
			BaseChatEvent: domain.BaseChatEvent{
				RequestID: p.requestID,
				Timestamp: time.Now(),
			},
			ToolCallID: tc.Id,
			ToolName:   tc.Function.Name,
			Arguments:  tc.Function.Arguments,
			Status:     "queued",
			Message:    "",
		}
		p.chatEvents <- event
	}
}

// publishToolStatusChange publishes a ToolExecutionProgressEvent
func (p *eventPublisher) publishToolStatusChange(callID string, toolName string, status string, message string, images []domain.ImageAttachment) {
	event := domain.ToolExecutionProgressEvent{
		BaseChatEvent: domain.BaseChatEvent{
			RequestID: p.requestID,
			Timestamp: time.Now(),
		},
		ToolCallID: callID,
		ToolName:   toolName,
		Status:     status,
		Message:    message,
		Images:     images,
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

// publishPlanApprovalRequest publishes a PlanApprovalRequestedEvent when RequestPlanApproval tool executes
func (p *eventPublisher) publishPlanApprovalRequest(planContent string) {
	event := domain.PlanApprovalRequestedEvent{
		RequestID:    p.requestID,
		Timestamp:    time.Now(),
		PlanContent:  planContent,
		ResponseChan: nil,
	}

	select {
	case p.chatEvents <- event:
	default:
		logger.Warn("plan approval request event dropped - channel full")
	}
}

// publishToolExecutionCompleted publishes a ToolExecutionCompletedEvent after all tools finish
func (p *eventPublisher) publishToolExecutionCompleted(results []domain.ConversationEntry) {
	successCount := 0
	failureCount := 0
	toolResults := make([]*domain.ToolExecutionResult, 0, len(results))

	for _, entry := range results {
		if entry.ToolExecution != nil {
			if entry.ToolExecution.Success {
				successCount++
			} else {
				failureCount++
			}
			toolResults = append(toolResults, entry.ToolExecution)
		}
	}

	event := domain.ToolExecutionCompletedEvent{
		SessionID:     p.requestID,
		RequestID:     p.requestID,
		Timestamp:     time.Now(),
		TotalExecuted: len(results),
		SuccessCount:  successCount,
		FailureCount:  failureCount,
		Results:       toolResults,
	}

	select {
	case p.chatEvents <- event:
	default:
		logger.Warn("tool execution completed event dropped - channel full")
	}
}

// NewAgentService creates a new agent service with pre-configured client
func NewAgentService(
	client domain.SDKClient,
	toolService domain.ToolService,
	config domain.ConfigService,
	conversationRepo domain.ConversationRepository,
	a2aAgentService domain.A2AAgentService,
	messageQueue domain.MessageQueue,
	stateManager domain.StateManager,
	timeoutSeconds int,
	optimizer domain.ConversationOptimizer,
) *AgentServiceImpl {
	tokenizer := NewTokenizerService(DefaultTokenizerConfig())

	approvalPolicy := NewStandardApprovalPolicy(config.GetConfig(), stateManager)

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
		approvalPolicy:   approvalPolicy,
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
	if s.optimizer != nil {
		optimizedMessages = s.optimizer.OptimizeMessages(req.Messages, req.Model, false)
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
// batchDrainQueue drains all queued messages and adds them to conversation
// Returns the number of messages drained
func (s *AgentServiceImpl) batchDrainQueue(
	conversation *[]sdk.Message,
	eventPublisher *eventPublisher,
) int {
	if s.messageQueue == nil {
		return 0
	}

	messages := []domain.QueuedMessage{}

	// Drain entire queue
	for !s.messageQueue.IsEmpty() {
		msg := s.messageQueue.Dequeue()
		if msg != nil {
			messages = append(messages, *msg)
		}
	}

	if len(messages) == 0 {
		return 0
	}

	logger.Info("Batching queued messages into conversation",
		"count", len(messages),
		"oldest", messages[0].QueuedAt,
		"newest", messages[len(messages)-1].QueuedAt)

	// Add all messages to conversation
	for _, queuedMsg := range messages {
		*conversation = append(*conversation, queuedMsg.Message)

		entry := domain.ConversationEntry{
			Message: queuedMsg.Message,
			Time:    time.Now(),
		}
		if err := s.conversationRepo.AddMessage(entry); err != nil {
			logger.Error("failed to store batched message", "error", err)
		}

		eventPublisher.chatEvents <- domain.MessageQueuedEvent{
			RequestID: queuedMsg.RequestID,
			Timestamp: time.Now(),
			Message:   queuedMsg.Message,
		}
	}

	return len(messages)
}

// RunWithStream executes an agent task with streaming (for interactive chat)
func (s *AgentServiceImpl) RunWithStream(ctx context.Context, req *domain.AgentRequest) (<-chan domain.ChatEvent, error) { // nolint:gocognit,gocyclo,cyclop,funlen
	if err := s.validateRequest(req); err != nil {
		return nil, err
	}

	if s.stateManager != nil && s.stateManager.IsComputerUsePaused() {
		logger.Info("Execution is paused, waiting for resume")
		return nil, fmt.Errorf("execution is paused")
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

		agent := NewEventDrivenAgent(
			s,
			ctx,
			req,
			&conversation,
			eventPublisher,
			cancelChan,
			provider,
			model,
			taskTracker,
		)

		agent.Start()
		agent.Wait()
	}()

	return chatEvents, nil
}

// CancelRequest cancels an active request
func (s *AgentServiceImpl) CancelRequest(requestID string) error {
	s.requestsMux.Lock()
	cancel, contextExists := s.activeRequests[requestID]
	if contextExists {
		delete(s.activeRequests, requestID)
	}
	s.requestsMux.Unlock()

	s.cancelMux.Lock()
	cancelChan, chanExists := s.cancelChannels[requestID]
	if chanExists {
		delete(s.cancelChannels, requestID)
	}
	s.cancelMux.Unlock()

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

	if s.stateManager != nil {
		cancelEvent := domain.CancelledEvent{
			RequestID: requestID,
			Timestamp: time.Now(),
			Reason:    "user cancelled",
		}
		s.stateManager.BroadcastEvent(cancelEvent)
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
	model string,
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
		model,
		int(effectiveUsage.PromptTokens),
		int(effectiveUsage.CompletionTokens),
		int(effectiveUsage.TotalTokens),
	); err != nil {
		logger.Error("failed to add token usage to session", "error", err)
	}
}

func (s *AgentServiceImpl) optimizeConversation(_ context.Context, req *domain.AgentRequest, conversation []sdk.Message, eventPublisher *eventPublisher) []sdk.Message {
	if s.optimizer == nil {
		return conversation
	}

	originalCount := len(conversation)

	conversation = s.optimizer.OptimizeMessages(conversation, req.Model, false)
	optimizedCount := len(conversation)

	if originalCount != optimizedCount {
		eventPublisher.publishOptimizationStatus(fmt.Sprintf("Conversation optimized (%d → %d messages)", originalCount, optimizedCount), false, originalCount, optimizedCount)
	}

	return conversation
}

type IndexedToolResult struct {
	Index  int
	Result domain.ConversationEntry
}

func (s *AgentServiceImpl) executeToolCallsParallel( // nolint:funlen
	ctx context.Context,
	toolCalls []*sdk.ChatCompletionMessageToolCall,
	eventPublisher *eventPublisher,
	isChatMode bool,
) []domain.ConversationEntry {

	if len(toolCalls) == 0 {
		return []domain.ConversationEntry{}
	}

	eventPublisher.publishToolsQueued(func() []sdk.ChatCompletionMessageToolCall {
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
		requiresApproval := s.approvalPolicy.ShouldRequireApproval(context.Background(), tc, isChatMode)
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

		time.Sleep(constants.AgentToolExecutionDelay)

		result := s.executeTool(ctx, *at.tool, eventPublisher, isChatMode)

		status := "completed"
		message := "Completed successfully"
		var images []domain.ImageAttachment
		if result.ToolExecution != nil {
			if !result.ToolExecution.Success {
				status = "failed"
				message = "Execution failed"
			}
			images = result.ToolExecution.Images

			if at.tool.Function.Name == "GetLatestScreenshot" && len(images) > 0 {
				logger.Info("Publishing GetLatestScreenshot completion with images",
					"imageCount", len(images),
					"status", status)
			}
		}

		eventPublisher.publishToolStatusChange(at.tool.Id, at.tool.Function.Name, status, message, images)
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
					toolCall.Function.Name, "starting",
					fmt.Sprintf("Initializing %s...", toolCall.Function.Name),
					nil,
				)

				time.Sleep(constants.AgentToolExecutionDelay)

				result := s.executeTool(ctx, *toolCall, eventPublisher, isChatMode)

				status := "completed"
				message := "Completed successfully"
				var images []domain.ImageAttachment
				if result.ToolExecution != nil {
					if !result.ToolExecution.Success {
						status = "failed"
						message = "Execution failed"
					}
					images = result.ToolExecution.Images
				}

				eventPublisher.publishToolStatusChange(toolCall.Id, toolCall.Function.Name, status, message, images)

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

	if err := s.batchSaveToolResults(results); err != nil {
		logger.Error("failed to batch save tool results", "error", err)
	}

	eventPublisher.publishToolExecutionCompleted(results)

	return results
}

//nolint:funlen,gocyclo,cyclop // Tool execution requires comprehensive error handling and status updates
func (s *AgentServiceImpl) executeTool(
	ctx context.Context,
	tc sdk.ChatCompletionMessageToolCall,
	eventPublisher *eventPublisher,
	isChatMode bool,
) domain.ConversationEntry {
	startTime := time.Now()

	requiresApproval := s.approvalPolicy.ShouldRequireApproval(context.Background(), &tc, isChatMode)
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

	return s.executeToolInternal(ctx, tc, eventPublisher, wasApproved, startTime)
}

// executeToolInternal performs the actual tool execution without approval checks
// This is used by both executeTool() (after approval) and processNextTool() (approval already obtained)
//
//nolint:funlen,gocyclo,cyclop // Tool execution requires comprehensive error handling and status updates
func (s *AgentServiceImpl) executeToolInternal(
	ctx context.Context,
	tc sdk.ChatCompletionMessageToolCall,
	eventPublisher *eventPublisher,
	wasApproved bool,
	startTime time.Time,
) domain.ConversationEntry {
	eventPublisher.publishToolStatusChange(tc.Id, tc.Function.Name, "running", "Executing...", nil)

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
		execCtx = domain.WithToolApproved(execCtx)
	}

	if tc.Function.Name == "Bash" {
		bashCallback := func(line string) {
			eventPublisher.publishBashOutputChunk(tc.Id, line, false)
		}
		execCtx = domain.WithBashOutputCallback(execCtx, bashCallback)

		detachChan := make(chan struct{}, 1)
		if chatHandler := domain.GetChatHandler(ctx); chatHandler != nil {
			chatHandler.SetBashDetachChan(detachChan)

			defer func() {
				chatHandler.ClearBashDetachChan()
			}()
		}
		execCtx = domain.WithBashDetachChannel(execCtx, detachChan)
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

	resultReceived := false
	for !resultReceived {
		select {
		case res := <-resultChan:
			result = res.result
			err = res.err
			ticker.Stop()
			resultReceived = true
		case <-ticker.C:
			eventPublisher.publishToolStatusChange(tc.Id, tc.Function.Name, "running", "Processing...", nil)
		case <-ctx.Done():
			logger.Error("tool execution cancelled", "tool", tc.Function.Name)
			return s.createErrorEntry(tc, ctx.Err(), startTime)
		}
	}

	if err != nil {
		logger.Error("failed to execute tool", "tool", tc.Function.Name, "error", err)
		return s.createErrorEntry(tc, err, startTime)
	}

	eventPublisher.publishToolStatusChange(tc.Id, tc.Function.Name, "saving", "Saving results...", nil)

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
		Images:    result.Images,
	}

	if result.ToolName == "TodoWrite" && result.Success {
		if todoResult, ok := result.Data.(*domain.TodoWriteToolResult); ok && todoResult != nil {
			eventPublisher.publishTodoUpdate(todoResult.Todos)
		}
	}

	if result.ToolName == "RequestPlanApproval" && result.Success {
		planContent := extractPlanContent(result)
		if planContent != "" {
			eventPublisher.publishPlanApprovalRequest(planContent)
		} else {
			logger.Warn("RequestPlanApproval succeeded but plan content is empty")
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

	if wasApproved {
		s.conversationRepo.RemovePendingToolCallByID(tc.Id)
	}

	return entry
}

// handleToolResults processes tool execution results and returns true if agent should stop
func (s *AgentServiceImpl) handleToolResults(
	toolResults []domain.ConversationEntry,
	conversation *[]sdk.Message,
	eventPublisher *eventPublisher,
	req *domain.AgentRequest,
) bool {
	hasRejection, planContent := s.checkToolResultsStatus(toolResults)

	s.addToolResultsToConversation(toolResults, conversation)

	if hasRejection {
		logger.Info("Tool was rejected - stopping agent loop")
		eventPublisher.publishChatComplete("", []sdk.ChatCompletionMessageToolCall{}, s.GetMetrics(req.RequestID))
		return true
	}

	if planContent != "" {
		s.createPlanMessage(planContent, conversation, eventPublisher, req)
		return true
	}

	return false
}

// checkToolResultsStatus checks for rejections and plan approval in tool results
func (s *AgentServiceImpl) checkToolResultsStatus(toolResults []domain.ConversationEntry) (hasRejection bool, planContent string) {
	for _, entry := range toolResults {
		if entry.ToolExecution != nil {
			if entry.ToolExecution.Rejected {
				return true, ""
			}
			if entry.ToolExecution.ToolName == "RequestPlanApproval" && entry.ToolExecution.Success {
				planContent = extractPlanContent(entry.ToolExecution)
				logger.Info("RequestPlanApproval tool executed - stopping agent loop to wait for user approval", "planLength", len(planContent))
			}
		}
	}
	return false, planContent
}

// addToolResultsToConversation adds tool results and images to the conversation
func (s *AgentServiceImpl) addToolResultsToConversation(toolResults []domain.ConversationEntry, conversation *[]sdk.Message) {
	for _, entry := range toolResults {
		toolResult := sdk.Message{
			Role:       sdk.Tool,
			Content:    entry.Message.Content,
			ToolCallId: entry.Message.ToolCallId,
		}
		*conversation = append(*conversation, toolResult)
	}

	s.addImageMessageFromToolResults(toolResults, conversation)
}

// createPlanMessage creates and stores a plan message for approval
func (s *AgentServiceImpl) createPlanMessage(
	planContent string,
	conversation *[]sdk.Message,
	eventPublisher *eventPublisher,
	req *domain.AgentRequest,
) {
	planMessage := sdk.Message{
		Role:    sdk.Assistant,
		Content: sdk.NewMessageContent(planContent),
	}
	*conversation = append(*conversation, planMessage)

	planEntry := domain.ConversationEntry{
		Message:            planMessage,
		Time:               time.Now(),
		IsPlan:             true,
		PlanApprovalStatus: domain.PlanApprovalPending,
	}
	if err := s.conversationRepo.AddMessage(planEntry); err != nil {
		logger.Error("failed to store plan message", "error", err)
	}

	logger.Info("Plan approval requested - stopping agent loop")
	eventPublisher.publishChatComplete("", []sdk.ChatCompletionMessageToolCall{}, s.GetMetrics(req.RequestID))
}

// extractPlanContent extracts plan content from RequestPlanApproval tool result
func extractPlanContent(result *domain.ToolExecutionResult) string {
	if result == nil || result.Data == nil {
		return ""
	}

	data, ok := result.Data.(map[string]any)
	if !ok {
		return ""
	}

	plan, ok := data["plan"].(string)
	if !ok {
		return ""
	}

	return plan
}

// addImageMessageFromToolResults adds images from tool results as a separate hidden user message
// This ensures compatibility with all providers (Anthropic requires tool messages to be text-only)
func (s *AgentServiceImpl) addImageMessageFromToolResults(toolResults []domain.ConversationEntry, conversation *[]sdk.Message) {
	imageMessage := s.createImageMessageFromToolResults(toolResults)
	if imageMessage == nil {
		return
	}

	*conversation = append(*conversation, *imageMessage)

	imageEntry := domain.ConversationEntry{
		Message: *imageMessage,
		Time:    time.Now(),
		Hidden:  true,
	}
	if err := s.conversationRepo.AddMessage(imageEntry); err != nil {
		logger.Error("failed to add image message from tool results", "error", err)
	}
}

// createImageMessageFromToolResults creates a hidden user message containing images from tool results
// Returns nil if no images are present
func (s *AgentServiceImpl) createImageMessageFromToolResults(toolResults []domain.ConversationEntry) *sdk.Message {
	var allImages []domain.ImageAttachment

	for _, result := range toolResults {
		if result.ToolExecution != nil && len(result.ToolExecution.Images) > 0 {
			allImages = append(allImages, result.ToolExecution.Images...)
		}
	}

	if len(allImages) == 0 {
		return nil
	}

	var contentParts []sdk.ContentPart
	textPart, err := sdk.NewTextContentPart(fmt.Sprintf("Tool execution returned %d image(s) for analysis:", len(allImages)))
	if err == nil {
		contentParts = append(contentParts, textPart)
	}

	for i, img := range allImages {
		dataURL := fmt.Sprintf("data:%s;base64,%s", img.MimeType, img.Data)
		imagePart, err := sdk.NewImageContentPart(dataURL, nil)
		if err != nil {
			logger.Warn("Failed to create image content part", "index", i, "filename", img.Filename, "error", err)
			continue
		}
		contentParts = append(contentParts, imagePart)
	}

	if len(contentParts) == 0 {
		logger.Warn("No content parts created for image message from tool results")
		return nil
	}

	return &sdk.Message{
		Role:    sdk.User,
		Content: sdk.NewMessageContent(contentParts),
	}
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

	var approved bool
	var err error

	select {
	case response := <-responseChan:
		if response == domain.ApprovalAutoAccept {
			logger.Info("Switching to auto-accept mode from floating window")
			s.stateManager.SetAgentMode(domain.AgentModeAutoAccept)
		}
		approved = response == domain.ApprovalApprove || response == domain.ApprovalAutoAccept
	case <-ctx.Done():
		err = fmt.Errorf("approval request cancelled: %w", ctx.Err())
	case <-time.After(5 * time.Minute):
		err = fmt.Errorf("approval request timed out")
	}

	return approved, err
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
