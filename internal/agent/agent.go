package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	constants "github.com/inference-gateway/cli/internal/constants"
	domain "github.com/inference-gateway/cli/internal/domain"
	formatting "github.com/inference-gateway/cli/internal/formatting"
	logger "github.com/inference-gateway/cli/internal/logger"
	services "github.com/inference-gateway/cli/internal/services"
	plugins "github.com/inference-gateway/cli/internal/services/plugins"
)

// AgentServiceImpl implements the AgentService interface with direct chat functionality
type AgentServiceImpl struct {
	client           sdk.Client
	toolService      domain.ToolService
	config           *config.Config
	conversationRepo domain.ConversationRepository
	a2aAgentService  domain.A2AAgentService
	skillsService    domain.SkillsService
	messageQueue     domain.MessageQueue
	stateManager     stateManager
	timeoutSeconds   int
	maxTokens        int
	optimizer        domain.ConversationOptimizer
	tokenizer        *services.TokenizerService
	approvalPolicy   domain.ApprovalPolicy
	bgRegistry       domain.BackgroundTaskRegistry
	reminderProvider domain.SystemReminderProvider
	hookProvider     domain.HookCommandProvider
	memoryBackend    domain.MemoryBackend

	// Reminder cadence is session-scoped, not per-request. sessionTurns counts
	// cumulative model turns across the whole chat session so an `interval`
	// reminder fires on every Nth conversational turn - the per-request
	// AgentContext.Turns resets to 1 on each user message, so keying interval
	// off it would essentially never fire in normal chat. firedReminders backs
	// the `once` trigger across the session. Both reset implicitly when a new
	// chat process builds a fresh AgentServiceImpl.
	sessionTurns   atomic.Int64
	firedReminders map[string]bool
	reminderMux    sync.Mutex

	// Session tracking: covers the full lifetime of a RunWithStream call.
	// Cancelling a session aborts streaming, tool execution, approval waits,
	// and the main event loop in one shot. Idempotent via sync.Once so
	// multiple Esc presses are safe.
	activeSessions map[string]*sessionCancel
	sessionMux     sync.RWMutex

	// Metrics tracking
	metrics    map[string]*domain.ChatMetrics
	metricsMux sync.RWMutex

	// Tool call accumulation
	toolCallsMap map[string]*sdk.ChatCompletionMessageToolCall
	toolCallsMux sync.RWMutex

	// Context caching
	gitContextCache    string
	gitContextTurn     int
	treeContextCache   string
	treeContextTurn    int
	memoryContextCache string
	memoryContextTurn  int
	contextCacheMux    sync.RWMutex

	// Mode-change tracking: the mode used on the previous streaming turn. When
	// the user cycles the mode mid-session (shift+tab), the next pre_stream
	// reminder query reports the change (modeChangeSinceLastStream) so the
	// on_mode_change reminder fires and the model adapts its behavior (e.g.
	// stops writing code in Plan mode). modeInitialized distinguishes "no
	// previous turn yet" from "previous turn was AgentModeStandard (zero value)".
	lastStreamedMode domain.AgentMode
	modeInitialized  bool
	modeMux          sync.Mutex
}

// sessionCancel bundles the two cancellation primitives for a single
// RunWithStream session: a context.CancelFunc that aborts in-flight
// streaming/tool/approval work, and a broadcast channel that wakes the
// agent's main event loop and any polling goroutines. sync.Once makes
// Cancel safe to call repeatedly so the UI can fire it on every Esc
// press without panicking on double-close.
type sessionCancel struct {
	cancelCtx  context.CancelFunc
	cancelChan chan struct{}
	once       sync.Once
}

// Cancel triggers both primitives exactly once.
func (sc *sessionCancel) Cancel() {
	sc.once.Do(func() {
		sc.cancelCtx()
		close(sc.cancelChan)
	})
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

// chatQuestionBroker implements domain.UserQuestionBroker for the chat executor.
// It publishes a UserQuestionRequestedEvent onto the per-request chatEvents
// channel and blocks on the response channel, mirroring requestToolApproval.
// The agent loop is only blocked in the tool's Execute goroutine; the TUI keeps
// running and the answers arrive via the UI. When the user dismisses the form
// the UI closes the channel (ok=false); session cancellation unblocks ctx.Done.
type chatQuestionBroker struct {
	publisher *eventPublisher
}

func (b *chatQuestionBroker) AskUserQuestions(ctx context.Context, questions []domain.UserQuestion) ([]domain.UserQuestionAnswer, bool, error) {
	responseChan := make(chan []domain.UserQuestionAnswer, 1)

	b.publisher.chatEvents <- domain.UserQuestionRequestedEvent{
		RequestID:    b.publisher.requestID,
		Timestamp:    time.Now(),
		Questions:    questions,
		ResponseChan: responseChan,
	}

	select {
	case answers, open := <-responseChan:
		if !open {
			return nil, false, nil
		}
		return answers, true, nil
	case <-ctx.Done():
		return nil, false, ctx.Err()
	}
}

// publishChatStart publishes a ChatStartEvent
func (p *eventPublisher) publishChatStart() {
	p.chatEvents <- domain.ChatStartEvent{
		RequestID: p.requestID,
		Timestamp: time.Now(),
	}
}

// publishChatComplete publishes a ChatCompleteEvent for a normally-finished
// chat turn.
func (p *eventPublisher) publishChatComplete(reasoning string, toolCalls []sdk.ChatCompletionMessageToolCall, metrics *domain.ChatMetrics) {
	p.chatEvents <- domain.ChatCompleteEvent{
		RequestID:        p.requestID,
		Timestamp:        time.Now(),
		ReasoningContent: reasoning,
		ToolCalls:        toolCalls,
		Metrics:          metrics,
	}
}

// publishChatCancelled publishes a ChatCompleteEvent flagged as cancelled so
// the UI shows "User interrupted" instead of "Response complete". The event
// reuses ChatCompleteEvent (rather than a new type) so existing listeners
// keep handling lifecycle bookkeeping uniformly.
func (p *eventPublisher) publishChatCancelled(metrics *domain.ChatMetrics) {
	p.chatEvents <- domain.ChatCompleteEvent{
		RequestID: p.requestID,
		Timestamp: time.Now(),
		Metrics:   metrics,
		Cancelled: true,
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
			ToolCallID: tc.ID,
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
func (p *eventPublisher) publishPlanApprovalRequest(planContent, planPath string) {
	event := domain.PlanApprovalRequestedEvent{
		RequestID:    p.requestID,
		Timestamp:    time.Now(),
		PlanContent:  planContent,
		PlanPath:     planPath,
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
// stateManager is the narrow slice of the app state manager the agent core
// needs: the current agent mode, computer-use pause state, and retry-status
// updates. *services.StateManager satisfies it.
type stateManager interface {
	domain.AgentModeManager
	domain.ComputerUsePauseManager
	domain.ChatSessionManager
}

func NewAgent(
	client sdk.Client,
	toolService domain.ToolService,
	cfg *config.Config,
	conversationRepo domain.ConversationRepository,
	a2aAgentService domain.A2AAgentService,
	skillsService domain.SkillsService,
	messageQueue domain.MessageQueue,
	stateManager stateManager,
	timeoutSeconds int,
	optimizer domain.ConversationOptimizer,
	bgRegistry domain.BackgroundTaskRegistry,
) *AgentServiceImpl {
	tokenizer := services.NewTokenizerService(services.DefaultTokenizerConfig())

	approvalPolicy := services.NewStandardApprovalPolicy(cfg, stateManager)

	hookProvider := domain.HookCommandProvider(cfg.Hooks)
	if pluginProvider := plugins.NewPluginHookCommandProvider(cfg); pluginProvider != nil {
		hookProvider = pluginProvider
	}

	return &AgentServiceImpl{
		client:           client,
		toolService:      toolService,
		config:           cfg,
		conversationRepo: conversationRepo,
		a2aAgentService:  a2aAgentService,
		skillsService:    skillsService,
		messageQueue:     messageQueue,
		stateManager:     stateManager,
		timeoutSeconds:   timeoutSeconds,
		maxTokens:        cfg.GetAgentConfig().MaxTokens,
		optimizer:        optimizer,
		tokenizer:        tokenizer,
		approvalPolicy:   approvalPolicy,
		bgRegistry:       bgRegistry,
		reminderProvider: cfg.Reminders,
		hookProvider:     hookProvider,
		firedReminders:   make(map[string]bool),
		activeSessions:   make(map[string]*sessionCancel),
		metrics:          make(map[string]*domain.ChatMetrics),
		toolCallsMap:     make(map[string]*sdk.ChatCompletionMessageToolCall),
	}
}

// SetMemoryBackend wires the memory sync backend so the chat agent pulls memory
// once at session start (SyncIn on HookPreSession). SyncOut is driven by the
// Memory tool on write/delete, not here - chat fires HookPostSession after every
// message, so pushing there would commit-storm. A nil backend disables sync.
func (s *AgentServiceImpl) SetMemoryBackend(backend domain.MemoryBackend) {
	s.memoryBackend = backend
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

	var availableTools []sdk.ChatCompletionTool

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
			availableTools = s.toolService.ListToolsForMode(mode)
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

	content, reasoningContent, toolCalls := extractFirstChoice(response)

	effectiveUsage := s.storeIterationMetrics(req.RequestID, req.Model, startTime, response.Usage, &storeIterationMetricsInput{
		inputMessages:   messages,
		outputContent:   content,
		outputToolCalls: toolCalls,
		availableTools:  availableTools,
	})

	syncResponse := &domain.ChatSyncResponse{
		RequestID:        req.RequestID,
		Content:          content,
		ReasoningContent: reasoningContent,
		ToolCalls:        toolCalls,
		Usage:            effectiveUsage,
		Duration:         duration,
	}

	return syncResponse, nil
}

// extractFirstChoice pulls content, reasoning, and tool calls from the first
// choice of a non-streaming response. Reasoning preference matches the
// streaming path in agent_streaming.go.
func extractFirstChoice(response *sdk.CreateChatCompletionResponse) (string, string, []sdk.ChatCompletionMessageToolCall) {
	if len(response.Choices) == 0 {
		return "", "", nil
	}

	choice := response.Choices[0]

	content, err := choice.Message.Content.AsMessageContent0()
	if err != nil {
		content = ""
	}

	reasoning := ""
	switch {
	case choice.Message.Reasoning != nil && *choice.Message.Reasoning != "":
		reasoning = *choice.Message.Reasoning
	case choice.Message.ReasoningContent != nil && *choice.Message.ReasoningContent != "":
		reasoning = *choice.Message.ReasoningContent
	}

	var toolCalls []sdk.ChatCompletionMessageToolCall
	if choice.Message.ToolCalls != nil {
		toolCalls = *choice.Message.ToolCalls
	}

	return content, reasoning, toolCalls
}

// ensureConversationIntegrity enforces the OpenAI tool_call/response
// invariant by inserting a synthetic Tool-role message for every
// orphan tool_call_id in the current conversation. Returns the number
// of synthetics inserted.
//
// persistSynthetics:
//   - true at the drain-time chokepoint (real corruption point - JSONL
//     append order matches logical order, so repo state stays valid).
//   - false at defensive call sites (e.g. before sending to the
//     gateway) where the orphan may have come from a pre-existing
//     disk state we cannot retroactively repair without rewriting the
//     JSONL.
//
// Idempotent: re-running on an already-repaired conversation is a
// no-op (returns 0).
func (s *AgentServiceImpl) ensureConversationIntegrity(
	conversation *[]sdk.Message,
	publisher *eventPublisher,
	requestID string,
	persistSynthetics bool,
) int {
	if conversation == nil || len(*conversation) == 0 {
		return 0
	}

	repaired, synthetics := services.EnsureToolCallsClosed(*conversation)
	if len(synthetics) == 0 {
		return 0
	}

	*conversation = repaired

	logger.Info("synthesized cancelled tool responses for orphan tool_calls",
		"count", len(synthetics),
		"persisted", persistSynthetics)

	if !persistSynthetics {
		return len(synthetics)
	}

	for _, syn := range synthetics {
		entry := domain.ConversationEntry{
			Message: syn.Message,
			Time:    time.Now(),
		}
		if s.conversationRepo != nil {
			if err := s.conversationRepo.AddMessage(entry); err != nil {
				logger.Error("failed to persist synthetic cancelled tool response",
					"tool_call_id", syn.ToolCallID, "error", err)
			}
		}
		if publisher != nil {
			publisher.chatEvents <- domain.ToolCancelledEvent{
				RequestID:  requestID,
				Timestamp:  time.Now(),
				ToolCallID: syn.ToolCallID,
				ToolName:   syn.ToolName,
			}
		}
	}
	return len(synthetics)
}

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

	for !s.messageQueue.IsEmpty() {
		msg := s.messageQueue.Dequeue()
		if msg != nil {
			messages = append(messages, *msg)
		}
	}

	if len(messages) == 0 {
		return 0
	}

	s.ensureConversationIntegrity(conversation, eventPublisher, messages[0].RequestID, true)

	logger.Info("batching queued messages into conversation",
		"count", len(messages),
		"oldest", messages[0].QueuedAt,
		"newest", messages[len(messages)-1].QueuedAt)

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
		logger.Info("execution is paused, waiting for resume")
		return nil, fmt.Errorf("execution is paused")
	}

	chatEvents := make(chan domain.ChatEvent, 1000)
	eventPublisher := newEventPublisher(req.RequestID, chatEvents)

	sessionCtx, cancelCtx := context.WithCancel(ctx)
	sessionCtx = domain.WithModel(sessionCtx, req.Model)
	sc := &sessionCancel{
		cancelCtx:  cancelCtx,
		cancelChan: make(chan struct{}),
	}
	s.registerSession(req.RequestID, sc)
	context.AfterFunc(sessionCtx, sc.Cancel)

	conversation := s.addSystemPrompt(req.Messages)

	provider, model, err := s.parseProvider(req.Model)
	if err != nil {
		sc.Cancel()
		s.deregisterSession(req.RequestID)
		return nil, fmt.Errorf("failed to parse provider from model '%s': %w", model, err)
	}

	go func() {
		defer func() {
			close(chatEvents)
			s.deregisterSession(req.RequestID)
			sc.Cancel()
		}()

		conversation = s.optimizeConversation(sessionCtx, req, conversation, eventPublisher)

		agent := NewEventDrivenAgent(
			s,
			s.config.GetAgentConfig(),
			sessionCtx,
			req,
			&conversation,
			eventPublisher,
			sc.cancelChan,
			provider,
			model,
			s.bgRegistry,
		)

		agent.Start()
		agent.Wait()
	}()

	return chatEvents, nil
}

// CancelRequest cancels an active request. Safe to call multiple times for
// the same requestID - subsequent calls are no-ops via sync.Once on the
// underlying sessionCancel. Returns nil even when the request is unknown,
// so the UI can fire it on every Esc press without surfacing spurious
// errors after the session has already torn down. The agent loop publishes
// ChatCompleteEvent{Cancelled:true} as the single cancel-completion signal;
// no separate CancelledEvent broadcast is needed.
func (s *AgentServiceImpl) CancelRequest(requestID string) error {
	s.sessionMux.RLock()
	sc, sessionExists := s.activeSessions[requestID]
	s.sessionMux.RUnlock()

	if sessionExists {
		sc.Cancel()
	}

	return nil
}

func (s *AgentServiceImpl) registerSession(requestID string, sc *sessionCancel) {
	s.sessionMux.Lock()
	defer s.sessionMux.Unlock()
	s.activeSessions[requestID] = sc
}

func (s *AgentServiceImpl) deregisterSession(requestID string) {
	s.sessionMux.Lock()
	defer s.sessionMux.Unlock()
	delete(s.activeSessions, requestID)
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
// It returns the effective (possibly polyfilled) usage that was accumulated, or nil when there
// was nothing to record. Both the streaming path and the sync Run path funnel through here so
// chat and headless token accounting stay identical (issue #835).
func (s *AgentServiceImpl) storeIterationMetrics(
	requestID string,
	model string,
	startTime time.Time,
	usage *sdk.CompletionUsage,
	polyfillInput *storeIterationMetricsInput,
) *sdk.CompletionUsage {
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
		return nil
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

	return effectiveUsage
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
		requiresApproval := s.approvalPolicy.ShouldRequireApproval(ctx, tc, isChatMode)
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
					toolCall.ID,
					toolCall.Function.Name, "starting",
					fmt.Sprintf("Initializing %s...", toolCall.Function.Name),
					nil,
				)

				time.Sleep(constants.AgentToolExecutionDelay)

				result := s.executeTool(ctx, *toolCall, eventPublisher, isChatMode)

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

	requiresApproval := s.approvalPolicy.ShouldRequireApproval(ctx, &tc, isChatMode)
	wasApproved := false
	isAutoAcceptMode := s.stateManager != nil && s.stateManager.GetAgentMode() == domain.AgentModeAutoAccept
	if isAutoAcceptMode {
		wasApproved = true
	} else if requiresApproval {
		approved, err := s.requestToolApproval(ctx, tc, eventPublisher)
		if err != nil {
			logger.Error("failed to request tool approval", "tool", tc.Function.Name, "error", err)
			return s.createErrorEntry(tc, err, startTime)
		}
		if !approved {
			eventPublisher.publishToolStatusChange(tc.ID, tc.Function.Name, "failed", "rejected", nil)
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
) (finalEntry domain.ConversationEntry) {
	eventPublisher.publishToolStatusChange(tc.ID, tc.Function.Name, "running", "Executing...", nil)

	defer func() {
		status, message := "completed", "Completed successfully"
		var images []domain.ImageAttachment
		if finalEntry.ToolExecution != nil {
			if !finalEntry.ToolExecution.Success {
				status, message = "failed", "Execution failed"
			}
			images = finalEntry.ToolExecution.Images
		}
		eventPublisher.publishToolStatusChange(tc.ID, tc.Function.Name, status, message, images)
	}()

	time.Sleep(constants.AgentToolExecutionDelay)

	if !isCompleteJSON(tc.Function.Arguments) {
		incompleteErr := fmt.Errorf(
			"TOOL FAILED: %s - content was truncated due to output token limits (received %d chars of incomplete JSON). %s",
			tc.Function.Name, len(tc.Function.Arguments), getTruncationRecoveryGuidance(tc.Function.Name),
		)
		logger.Error("incomplete JSON in tool arguments",
			"tool", tc.Function.Name,
			"args_length", len(tc.Function.Arguments),
			"args_preview", formatting.TruncateText(tc.Function.Arguments, 200),
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
	if s.stateManager != nil {
		execCtx = domain.WithAgentMode(execCtx, s.stateManager.GetAgentMode())
	}

	if domain.GetSessionID(execCtx) == "" && s.conversationRepo != nil {
		if convID := s.conversationRepo.GetCurrentConversationID(); convID != "" {
			execCtx = domain.WithSessionID(execCtx, convID)
		}
	}
	if wasApproved {
		execCtx = domain.WithToolApproved(execCtx)
	}

	if tc.Function.Name == "Bash" {
		bashCallback := func(line string) {
			eventPublisher.publishBashOutputChunk(tc.ID, line, false)
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

	if tc.Function.Name == "AskUserQuestion" && domain.GetChatHandler(ctx) != nil {
		execCtx = domain.WithUserQuestionBroker(execCtx, &chatQuestionBroker{publisher: eventPublisher})
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
			eventPublisher.publishToolStatusChange(tc.ID, tc.Function.Name, "running", "Processing...", nil)
		case <-ctx.Done():
			logger.Error("tool execution cancelled", "tool", tc.Function.Name)
			return s.createErrorEntry(tc, ctx.Err(), startTime)
		}
	}

	if err != nil {
		logger.Error("failed to execute tool", "tool", tc.Function.Name, "error", err)
		return s.createErrorEntry(tc, err, startTime)
	}

	eventPublisher.publishToolStatusChange(tc.ID, tc.Function.Name, "saving", "Saving results...", nil)

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
		if extractPlanContent(result) == "" {
			logger.Warn("requestPlanApproval succeeded but plan content is empty")
		}
	}

	formattedContent := s.conversationRepo.FormatToolResultForLLM(toolExecutionResult)

	entry := domain.ConversationEntry{
		Message: domain.Message{
			Role:       sdk.Tool,
			Content:    sdk.NewMessageContent(formattedContent),
			ToolCallID: &tc.ID,
		},
		Time:          time.Now(),
		ToolExecution: toolExecutionResult,
	}

	if wasApproved {
		s.conversationRepo.RemovePendingToolCallByID(tc.ID)
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
	hasRejection, planContent, planPath := s.checkToolResultsStatus(toolResults)

	s.addToolResultsToConversation(toolResults, conversation)

	if hasRejection {
		logger.Info("tool was rejected - stopping agent loop")
		eventPublisher.publishChatComplete("", []sdk.ChatCompletionMessageToolCall{}, s.GetMetrics(req.RequestID))
		return true
	}

	if planContent != "" {
		s.createPlanMessage(planContent, planPath, conversation, eventPublisher, req)
		return true
	}

	return false
}

// checkToolResultsStatus checks for rejections and plan approval in tool results
func (s *AgentServiceImpl) checkToolResultsStatus(toolResults []domain.ConversationEntry) (hasRejection bool, planContent, planPath string) {
	for _, entry := range toolResults {
		if entry.ToolExecution != nil {
			if entry.ToolExecution.Rejected {
				return true, "", ""
			}
			if entry.ToolExecution.ToolName == "RequestPlanApproval" && entry.ToolExecution.Success {
				planContent = extractPlanContent(entry.ToolExecution)
				planPath = extractPlanPath(entry.ToolExecution)
				logger.Info("requestPlanApproval tool executed - stopping agent loop to wait for user approval", "planLength", len(planContent))
			}
		}
	}
	return false, planContent, planPath
}

// addToolResultsToConversation adds tool results and images to the conversation
func (s *AgentServiceImpl) addToolResultsToConversation(toolResults []domain.ConversationEntry, conversation *[]sdk.Message) {
	for _, entry := range toolResults {
		toolResult := sdk.Message{
			Role:       sdk.Tool,
			Content:    entry.Message.Content,
			ToolCallID: entry.Message.ToolCallID,
		}
		*conversation = append(*conversation, toolResult)
	}

	s.addImageMessageFromToolResults(toolResults, conversation)
}

// createPlanMessage creates and stores a plan message for approval
func (s *AgentServiceImpl) createPlanMessage(
	planContent string,
	planPath string,
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

	eventPublisher.publishPlanApprovalRequest(planContent, planPath)

	logger.Info("plan approval requested - stopping agent loop")
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

// extractPlanPath extracts the saved plan file path from a RequestPlanApproval
// tool result. The path lets the post-approval continuation prompt point the
// agent back at the plan on disk after the planning context is compacted away.
func extractPlanPath(result *domain.ToolExecutionResult) string {
	if result == nil || result.Data == nil {
		return ""
	}

	data, ok := result.Data.(map[string]any)
	if !ok {
		return ""
	}

	path, ok := data["path"].(string)
	if !ok {
		return ""
	}

	return path
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
			logger.Warn("failed to create image content part", "index", i, "filename", img.Filename, "error", err)
			continue
		}
		contentParts = append(contentParts, imagePart)
	}

	if len(contentParts) == 0 {
		logger.Warn("no content parts created for image message from tool results")
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
			logger.Info("switching to auto-accept mode from floating window")
			s.stateManager.SetAgentMode(domain.AgentModeAutoAccept)
		}
		approved = response == domain.ApprovalApprove || response == domain.ApprovalAutoAccept
	case <-ctx.Done():
		err = fmt.Errorf("approval request cancelled: %w", ctx.Err())
	case <-time.After(constants.ApprovalTimeout):
		err = fmt.Errorf("approval request timed out")
	}

	if err != nil || !approved {
		s.conversationRepo.RemovePendingToolCallByID(tc.ID)
	}

	return approved, err
}

func (s *AgentServiceImpl) createErrorEntry(tc sdk.ChatCompletionMessageToolCall, err error, startTime time.Time) domain.ConversationEntry {
	return domain.ConversationEntry{
		Message: domain.Message{
			Role:       sdk.Tool,
			Content:    sdk.NewMessageContent(fmt.Sprintf("Tool execution failed: %s - %s", tc.Function.Name, err.Error())),
			ToolCallID: &tc.ID,
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
			ToolCallID: &tc.ID,
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
