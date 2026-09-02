package agent

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentapp "github.com/inference-gateway/cli/internal/agent/application"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	conv "github.com/inference-gateway/cli/internal/conversation"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	constants "github.com/inference-gateway/cli/internal/platform/constants"
	formatting "github.com/inference-gateway/cli/internal/platform/formatting"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	memory "github.com/inference-gateway/cli/internal/platform/memory"
	models "github.com/inference-gateway/cli/internal/platform/models"
	streamevent "github.com/inference-gateway/cli/internal/platform/streamevent"
	telemetry "github.com/inference-gateway/cli/internal/platform/telemetry"
	utils "github.com/inference-gateway/cli/internal/platform/utils"
	plugins "github.com/inference-gateway/cli/internal/plugins"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
)

// AgentServiceImpl implements the AgentService interface with direct chat functionality
type AgentServiceImpl struct {
	client           sdk.Client
	toolService      agentdomain.ToolService
	config           *config.Config
	conversationRepo convdomain.ConversationRepository
	a2aAgentService  agentapp.A2AAgentService
	skillsService    agentdomain.SkillsService
	messageQueue     convdomain.MessageQueue
	stateManager     stateManager
	timeoutSeconds   int
	maxTokens        int
	optimizer        convdomain.ConversationOptimizer
	tokenizer        *conv.TokenizerService
	approvalPolicy   agentdomain.ApprovalPolicy
	judge            agentdomain.JudgeApprover
	currentModel     func() string
	escalations      *judgeEscalations

	bgRegistry       scheddomain.BackgroundTaskRegistry
	rolloverManager  *conv.SessionRolloverManager
	reminderProvider agentdomain.SystemReminderProvider
	hookProvider     agentdomain.HookCommandProvider
	memoryBackend    memory.MemoryBackend
	recorder         *telemetry.Recorder
	sessionTurns     atomic.Int64
	firedReminders   map[string]bool
	reminderMux      sync.Mutex
	stalledStrikes   int
	lastFinishReason string

	// (name+args), reset per key on success; backs the retry-loop breaker
	failedCalls    map[string]int
	failedCallsMux sync.Mutex

	// repeatedFailure holds the last tool-call key whose failure count
	// meets the on_repeated_failure threshold. Set by trackRepeatedFailure
	// during tool execution and consumed by injectDueReminders at the
	// post_tool hook. Cleared on the first post_tool dispatch read.
	repeatedFailureKey string
	repeatedFailureMux sync.Mutex

	// Session tracking: covers the full lifetime of a RunWithStream call.
	// Cancelling a session aborts streaming, tool execution, approval waits,
	// and the main event loop in one shot. Idempotent via sync.Once so
	// multiple Esc presses are safe.
	activeSessions     map[string]*sessionCancel
	sessionMux         sync.RWMutex
	reasoningEffort    string
	reasoningEffortMux sync.RWMutex

	// Metrics tracking
	metrics    map[string]*agentdomain.ChatMetrics
	metricsMux sync.RWMutex

	// Tool call accumulation
	toolCallsMap map[string]*sdk.ChatCompletionMessageToolCall
	toolCallsMux sync.RWMutex

	// Context caching
	gitContextCache    string
	gitContextTurn     int
	gitContextBranch   string
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
	lastStreamedMode agentdomain.AgentMode
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
	chatEvents chan<- agentdomain.ChatEvent
}

// newEventPublisher creates a new event publisher for the given request
func newEventPublisher(requestID string, chatEvents chan<- agentdomain.ChatEvent) *eventPublisher {
	return &eventPublisher{
		requestID:  requestID,
		chatEvents: chatEvents,
	}
}

// chatQuestionBroker implements agentdomain.UserQuestionBroker for the chat executor.
// It publishes a UserQuestionRequestedEvent onto the per-request chatEvents
// channel and blocks on the response channel, mirroring requestToolApproval.
// The agent loop is only blocked in the tool's Execute goroutine; the TUI keeps
// running and the answers arrive via the UI. When the user dismisses the form
// the UI closes the channel (ok=false); session cancellation unblocks ctx.Done.
type chatQuestionBroker struct {
	publisher *eventPublisher
}

func (b *chatQuestionBroker) AskUserQuestions(ctx context.Context, questions []agentdomain.UserQuestion) ([]agentdomain.UserQuestionAnswer, bool, error) {
	responseChan := make(chan []agentdomain.UserQuestionAnswer, 1)

	b.publisher.chatEvents <- agentdomain.UserQuestionRequestedEvent{
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
	p.chatEvents <- agentdomain.ChatStartEvent{
		RequestID: p.requestID,
		Timestamp: time.Now(),
	}
}

// publishChatComplete publishes a ChatCompleteEvent for a normally-finished
// chat turn.
func (p *eventPublisher) publishChatComplete(reasoning string, toolCalls []sdk.ChatCompletionMessageToolCall, metrics *agentdomain.ChatMetrics) {
	p.chatEvents <- agentdomain.ChatCompleteEvent{
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
func (p *eventPublisher) publishChatCancelled(metrics *agentdomain.ChatMetrics) {
	p.chatEvents <- agentdomain.ChatCompleteEvent{
		RequestID: p.requestID,
		Timestamp: time.Now(),
		Metrics:   metrics,
		Cancelled: true,
	}
}

// publishChatChunk publishes a ChatChunkEvent
func (p *eventPublisher) publishChatChunk(content, reasoningContent string, toolCalls []sdk.ChatCompletionMessageToolCallChunk) {
	p.chatEvents <- agentdomain.ChatChunkEvent{
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
	p.chatEvents <- agentdomain.OptimizationStatusEvent{
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
		event := agentdomain.ToolExecutionProgressEvent{
			BaseChatEvent: agentdomain.BaseChatEvent{
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
func (p *eventPublisher) publishToolStatusChange(callID string, toolName string, status string, message string, images []agentdomain.ImageAttachment) {
	event := agentdomain.ToolExecutionProgressEvent{
		BaseChatEvent: agentdomain.BaseChatEvent{
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

// publishToolProgress sends a non-blocking running-status update so a lagging
// consumer cannot stall the long-running tool reporting it.
func (p *eventPublisher) publishToolProgress(callID string, toolName string, message string) {
	event := agentdomain.ToolExecutionProgressEvent{
		BaseChatEvent: agentdomain.BaseChatEvent{
			RequestID: p.requestID,
			Timestamp: time.Now(),
		},
		ToolCallID: callID,
		ToolName:   toolName,
		Status:     "running",
		Message:    message,
	}

	select {
	case p.chatEvents <- event:
	default:
		logger.Warn("tool progress update dropped - channel full")
	}
}

// publishBashOutputChunk publishes a BashOutputChunkEvent for streaming bash output
func (p *eventPublisher) publishBashOutputChunk(callID string, output string, isComplete bool) {
	event := agentdomain.BashOutputChunkEvent{
		BaseChatEvent: agentdomain.BaseChatEvent{
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
func (p *eventPublisher) publishTodoUpdate(todos []agentdomain.TodoItem) {
	event := agentdomain.TodoUpdateChatEvent{
		BaseChatEvent: agentdomain.BaseChatEvent{
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
func (p *eventPublisher) publishPlanApprovalRequest(planContent, planID string) {
	event := agentdomain.PlanApprovalRequestedEvent{
		RequestID:    p.requestID,
		Timestamp:    time.Now(),
		PlanContent:  planContent,
		PlanID:       planID,
		ResponseChan: nil,
	}

	select {
	case p.chatEvents <- event:
	default:
		logger.Warn("plan approval request event dropped - channel full")
	}
}

// publishToolExecutionCompleted publishes a ToolExecutionCompletedEvent after all tools finish
func (p *eventPublisher) publishToolExecutionCompleted(results []convdomain.ConversationEntry) {
	successCount := 0
	failureCount := 0
	toolResults := make([]*agentdomain.ToolExecutionResult, 0, len(results))

	for _, entry := range results {
		if entry.ToolExecution != nil {
			if entry.ToolExecution.ToolCallID == "" && entry.Message.ToolCallID != nil {
				entry.ToolExecution.ToolCallID = *entry.Message.ToolCallID
			}
			if entry.ToolExecution.Success {
				successCount++
			} else {
				failureCount++
			}
			toolResults = append(toolResults, entry.ToolExecution)
		}
	}

	event := agentdomain.ToolExecutionCompletedEvent{
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
// needs: the current agent mode, computer-use pause state, retry-status
// updates, and the session todo list (reminder gating). *statemanager.StateManager
// satisfies it.
type stateManager interface {
	agentdomain.AgentModeManager
	agentdomain.ComputerUsePauseManager
	agentdomain.ChatSessionManager
	agentdomain.TodoManager
}

func NewAgent(
	client sdk.Client,
	toolService agentdomain.ToolService,
	cfg *config.Config,
	conversationRepo convdomain.ConversationRepository,
	a2aAgentService agentapp.A2AAgentService,
	skillsService agentdomain.SkillsService,
	messageQueue convdomain.MessageQueue,
	stateManager stateManager,
	timeoutSeconds int,
	optimizer convdomain.ConversationOptimizer,
	bgRegistry scheddomain.BackgroundTaskRegistry,
	rolloverManager *conv.SessionRolloverManager,
) *AgentServiceImpl {
	tokenizer := conv.NewTokenizerService(conv.DefaultTokenizerConfig())

	approvalPolicy := NewStandardApprovalPolicy(cfg, stateManager)

	hookProvider := agentdomain.HookCommandProvider(cfg.Hooks)
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
		reasoningEffort:  cfg.GetAgentConfig().ReasoningEffort,
		optimizer:        optimizer,
		tokenizer:        tokenizer,
		approvalPolicy:   approvalPolicy,
		judge:            NewLLMJudge(client, cfg),
		escalations:      newJudgeEscalations(),
		bgRegistry:       bgRegistry,
		rolloverManager:  rolloverManager,
		reminderProvider: cfg.Reminders,
		hookProvider:     hookProvider,
		firedReminders:   make(map[string]bool),
		activeSessions:   make(map[string]*sessionCancel),
		metrics:          make(map[string]*agentdomain.ChatMetrics),
		toolCallsMap:     make(map[string]*sdk.ChatCompletionMessageToolCall),
	}
}

// SetReasoningEffort updates the reasoning effort applied to subsequent
// requests. An empty string resets to the provider default.
func (s *AgentServiceImpl) SetReasoningEffort(effort string) error {
	if effort != "" && !slices.Contains(config.ReasoningEffortLevels, effort) {
		return fmt.Errorf(
			"invalid reasoning effort %q: must be one of %s",
			effort, strings.Join(config.ReasoningEffortLevels, ", "),
		)
	}
	s.reasoningEffortMux.Lock()
	s.reasoningEffort = effort
	s.reasoningEffortMux.Unlock()
	return nil
}

// GetReasoningEffort returns the effort level currently applied to requests
// ("" = provider default).
func (s *AgentServiceImpl) GetReasoningEffort() string {
	s.reasoningEffortMux.RLock()
	defer s.reasoningEffortMux.RUnlock()
	return s.reasoningEffort
}

// reasoningEffortOptionFor maps the current effort level onto the optional
// chat-completions request field. Anthropic models get the raw value - the
// AnthropicMessages adapter reads it back from the options and translates it
// to output_config.effort (including minimal -> low). Every other provider
// clamps the Anthropic-only xhigh/max levels to high, the chat-completions
// ceiling.
func (s *AgentServiceImpl) reasoningEffortOptionFor(model string) *sdk.CreateChatCompletionRequestReasoningEffort {
	effort := s.GetReasoningEffort()
	if effort == "" {
		return nil
	}
	if provider, _, err := s.parseProvider(model); err != nil || sdk.Provider(provider) != sdk.Anthropic {
		switch effort {
		case "xhigh", "max":
			effort = "high"
		}
	}
	e := sdk.CreateChatCompletionRequestReasoningEffort(effort)
	return &e
}

// SetTelemetryRecorder wires the telemetry recorder so per-request token usage
// is tapped in storeIterationMetrics. A nil recorder disables recording.
func (s *AgentServiceImpl) SetTelemetryRecorder(rec *telemetry.Recorder) {
	s.recorder = rec
}

// SetMemoryBackend wires the memory sync backend so the chat agent pulls memory
// once at session start (SyncIn on HookPreSession). SyncOut is driven by the
// Memory tool on write/delete, not here - chat fires HookPostSession after every
// message, so pushing there would commit-storm. A nil backend disables sync.
func (s *AgentServiceImpl) SetMemoryBackend(backend memory.MemoryBackend) {
	s.memoryBackend = backend
}

// Run executes an agent task synchronously (for background/batch processing)
// turnOutput is the assembled result of a single model turn, produced by the
// sync or streaming executor passed to runTurn.
type turnOutput struct {
	content      string
	reasoning    string
	toolCalls    []sdk.ChatCompletionMessageToolCall
	finishReason string
	usage        *sdk.CompletionUsage
}

// turnExec issues the actual model call (sync or streaming) against a prepared
// client and returns the assembled output.
type turnExec func(ctx context.Context, client sdk.Client, provider sdk.Provider, model string, messages []sdk.Message) (turnOutput, error)

// runTurn wraps a single model turn with the shared preamble/postamble - message
// prep, timeout + span, client + tool construction, metrics, response assembly -
// and delegates the model call itself to exec. Run and RunStreaming differ only
// in exec (and whether streaming usage is requested).
// advertisedTools returns the tool definitions to send with a request. All
// mid-session modes advertise the same full list so a mode switch never
// invalidates the provider's prompt cache; restrictions apply at execution
// time. ReadOnly subagents keep their filtered list - their mode never
// changes mid-session, so there is no cache to break.
func (s *AgentServiceImpl) advertisedTools() []sdk.ChatCompletionTool {
	if s.toolService == nil {
		return nil
	}
	if s.stateManager != nil && s.stateManager.GetAgentMode() == agentdomain.AgentModeReadOnly {
		return s.toolService.ListToolsForMode(agentdomain.AgentModeReadOnly)
	}
	return s.toolService.ListTools()
}

func (s *AgentServiceImpl) runTurn(ctx context.Context, req *agentdomain.AgentRequest, stream bool, exec turnExec) (*agentdomain.ChatSyncResponse, error) {
	if err := s.validateRequest(req); err != nil {
		return nil, err
	}

	optimizedMessages := req.Messages
	if s.optimizer != nil {
		optimizedMessages = s.optimizer.OptimizeMessages(req.Messages, req.Model, false)
	}

	messages := s.addSystemPrompt(optimizedMessages)
	if tail, ok := s.volatileTailMessage(optimizedMessages, req.IsChatMode); ok && !conversationAwaitsToolResults(optimizedMessages) {
		messages = append(messages, tail)
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(s.timeoutSeconds)*time.Second)
	defer cancel()

	timeoutCtx, turnSpan := s.recorder.StartLLMTurnSpan(timeoutCtx, req.Model)
	defer turnSpan.End()

	startTime := time.Now()

	provider, modelName, err := s.parseProvider(req.Model)
	if err != nil {
		return nil, fmt.Errorf("failed to parse provider from model '%s': %w", req.Model, err)
	}

	opts := &sdk.CreateChatCompletionRequest{
		MaxTokens:       &s.maxTokens,
		ReasoningEffort: s.reasoningEffortOptionFor(req.Model),
	}
	if stream {
		opts.StreamOptions = &sdk.ChatCompletionStreamOptions{IncludeUsage: true}
	}
	client := s.client.WithOptions(opts).WithMiddlewareOptions(&sdk.MiddlewareOptions{SkipMCP: true})

	var availableTools []sdk.ChatCompletionTool
	if s.toolService != nil {
		availableTools = s.advertisedTools()
		if len(availableTools) > 0 {
			client = client.WithTools(&availableTools)
		}
	}

	out, err := exec(timeoutCtx, client, sdk.Provider(provider), modelName, messages)
	if err != nil {
		telemetry.SetSpanError(timeoutCtx, err)
		return nil, err
	}

	effectiveUsage := s.storeIterationMetrics(timeoutCtx, req.RequestID, req.Model, startTime, out.usage, &storeIterationMetricsInput{
		inputMessages:   messages,
		outputContent:   out.content,
		outputToolCalls: out.toolCalls,
		availableTools:  availableTools,
	})

	return &agentdomain.ChatSyncResponse{
		RequestID:        req.RequestID,
		Content:          out.content,
		ReasoningContent: out.reasoning,
		ToolCalls:        out.toolCalls,
		Usage:            effectiveUsage,
		Duration:         time.Since(startTime),
		FinishReason:     out.finishReason,
	}, nil
}

func (s *AgentServiceImpl) Run(ctx context.Context, req *agentdomain.AgentRequest) (*agentdomain.ChatSyncResponse, error) {
	return s.runTurn(ctx, req, false, func(ctx context.Context, client sdk.Client, provider sdk.Provider, model string, messages []sdk.Message) (turnOutput, error) {
		response, err := client.GenerateContent(ctx, provider, model, messages)
		if err != nil {
			return turnOutput{}, fmt.Errorf("failed to generate content: %w", err)
		}
		content, reasoning, toolCalls, finishReason := extractFirstChoice(response)
		return turnOutput{
			content:      content,
			reasoning:    reasoning,
			toolCalls:    toolCalls,
			finishReason: finishReason,
			usage:        response.Usage,
		}, nil
	})
}

// RunStreaming executes a single model turn with streaming, invoking onDelta for
// each content/reasoning/tool-call delta as it arrives, and returns the same
// assembled ChatSyncResponse as Run. It is the streaming counterpart of Run for
// callers that own their own agentic loop (the headless AG-UI agent) and want
// token-level output without adopting the full EventDrivenAgent. onDelta may be
// nil.
func (s *AgentServiceImpl) RunStreaming(
	ctx context.Context,
	req *agentdomain.AgentRequest,
	onDelta func(content, reasoning string, toolCalls []sdk.ChatCompletionMessageToolCallChunk),
) (*agentdomain.ChatSyncResponse, error) {
	return s.runTurn(ctx, req, true, func(ctx context.Context, client sdk.Client, provider sdk.Provider, model string, messages []sdk.Message) (turnOutput, error) {
		events, err := client.GenerateContentStream(ctx, provider, model, messages)
		if err != nil {
			return turnOutput{}, fmt.Errorf("failed to generate content stream: %w", err)
		}

		s.clearToolCallsMap()

		var acc streamAccumulator
		for streaming := true; streaming; {
			select {
			case <-ctx.Done():
				return turnOutput{}, fmt.Errorf("failed to generate content stream: %w", ctx.Err())
			case event, ok := <-events:
				if ok {
					acc.ingest(event, onDelta)
				} else {
					streaming = false
				}
			}
		}

		s.accumulateToolCalls(acc.toolDeltas)
		accumulated := s.getAccumulatedToolCalls()
		toolCalls := make([]sdk.ChatCompletionMessageToolCall, 0, len(accumulated))
		for _, tc := range accumulated {
			toolCalls = append(toolCalls, *tc)
		}

		return turnOutput{
			content:      acc.content.String(),
			reasoning:    acc.reasoning.String(),
			toolCalls:    toolCalls,
			finishReason: acc.finishReason,
			usage:        acc.usage,
		}, nil
	})
}

// streamAccumulator folds streaming SSE events into the assembled content,
// reasoning, tool-call deltas, usage, and finish reason for one model turn.
type streamAccumulator struct {
	content      strings.Builder
	reasoning    strings.Builder
	toolDeltas   []sdk.ChatCompletionMessageToolCallChunk
	usage        *sdk.CompletionUsage
	finishReason string
}

// ingest folds one SSE event in, invoking onDelta (may be nil) for each
// non-empty content/reasoning/tool-call delta.
func (a *streamAccumulator) ingest(
	event sdk.SSEvent,
	onDelta func(content, reasoning string, toolCalls []sdk.ChatCompletionMessageToolCallChunk),
) {
	if event.Event == nil || event.Data == nil {
		return
	}
	switch string(*event.Event) {
	case "message_stop", "system_init", "hook_event", "tool_failure", "result_metadata":
		return
	}
	var streamResponse sdk.CreateChatCompletionStreamResponse
	if err := json.Unmarshal(*event.Data, &streamResponse); err != nil {
		logger.Error("failed to unmarshal chat completion stream response", "error", err)
		return
	}
	if streamResponse.Usage != nil {
		a.usage = streamResponse.Usage
	}
	for _, choice := range streamResponse.Choices {
		a.ingestChoice(choice, onDelta)
	}
}

func (a *streamAccumulator) ingestChoice(
	choice sdk.ChatCompletionStreamChoice,
	onDelta func(content, reasoning string, toolCalls []sdk.ChatCompletionMessageToolCallChunk),
) {
	deltaContent := choice.Delta.Content
	if deltaContent != "" {
		a.content.WriteString(deltaContent)
	}
	reasoning := extractReasoningForEvent(choice.Delta)
	if reasoning != "" {
		a.reasoning.WriteString(reasoning)
	}
	var toolCalls []sdk.ChatCompletionMessageToolCallChunk
	if choice.Delta.ToolCalls != nil {
		toolCalls = *choice.Delta.ToolCalls
		a.toolDeltas = append(a.toolDeltas, toolCalls...)
	}
	if choice.FinishReason != "" {
		a.finishReason = string(choice.FinishReason)
	}
	if onDelta != nil && (deltaContent != "" || reasoning != "" || len(toolCalls) > 0) {
		onDelta(deltaContent, reasoning, toolCalls)
	}
}

// extractFirstChoice pulls content, reasoning, tool calls, and finish reason
// from the first choice of a non-streaming response. Reasoning preference
// matches the streaming path in agent_streaming.go.
func extractFirstChoice(response *sdk.CreateChatCompletionResponse) (string, string, []sdk.ChatCompletionMessageToolCall, string) {
	if len(response.Choices) == 0 {
		return "", "", nil, ""
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

	return content, reasoning, toolCalls, string(choice.FinishReason)
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

	repaired, synthetics := conv.EnsureToolCallsClosed(*conversation)
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
		entry := convdomain.ConversationEntry{
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
			publisher.chatEvents <- agentdomain.ToolCancelledEvent{
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

	messages := []convdomain.QueuedMessage{}

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

		entry := convdomain.ConversationEntry{
			Message: queuedMsg.Message,
			Time:    time.Now(),
		}
		if err := s.conversationRepo.AddMessage(entry); err != nil {
			logger.Error("failed to store batched message", "error", err)
		}

		eventPublisher.chatEvents <- agentdomain.MessageQueuedEvent{
			RequestID: queuedMsg.RequestID,
			Timestamp: time.Now(),
			Message:   queuedMsg.Message,
		}
	}

	return len(messages)
}

// RunWithStream executes an agent task with streaming (for interactive chat)
func (s *AgentServiceImpl) RunWithStream(ctx context.Context, req *agentdomain.AgentRequest) (<-chan agentdomain.ChatEvent, error) { // nolint:gocognit,gocyclo,cyclop,funlen
	if err := s.validateRequest(req); err != nil {
		return nil, err
	}

	if s.stateManager != nil && s.stateManager.IsComputerUsePaused() {
		logger.Info("execution is paused, waiting for resume")
		return nil, fmt.Errorf("execution is paused")
	}

	s.failedCallsMux.Lock()
	clear(s.failedCalls)
	s.failedCallsMux.Unlock()

	chatEvents := make(chan agentdomain.ChatEvent, 1000)
	eventPublisher := newEventPublisher(req.RequestID, chatEvents)

	sessionCtx, cancelCtx := context.WithCancel(ctx)
	sessionCtx = agentdomain.WithModel(sessionCtx, req.Model)
	sessionCtx = s.recorder.SpanContext(sessionCtx)
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
		defer func() {
			if r := recover(); r != nil {
				logger.Error("agent panic recovered", "panic", r, "stack", string(debug.Stack()))
				eventPublisher.chatEvents <- agentdomain.ChatErrorEvent{
					RequestID: req.RequestID,
					Timestamp: time.Now(),
					Error:     fmt.Errorf("agent panic: %v", r),
				}
			}
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
func (s *AgentServiceImpl) GetMetrics(requestID string) *agentdomain.ChatMetrics {
	s.metricsMux.RLock()
	defer s.metricsMux.RUnlock()

	if metrics, exists := s.metrics[requestID]; exists {
		return &agentdomain.ChatMetrics{
			Duration: metrics.Duration,
			Usage:    metrics.Usage,
		}
	}
	return nil
}

// storeIterationMetricsInput holds the data needed for token usage polyfill calculation
// cacheCreationTokenSource is implemented by clients that report Anthropic
// cache-creation (cache-write) tokens out of band, since the OpenAI-shaped
// usage struct has no field for them. The /v1/messages adapter
// (internal/platform/adapters.AnthropicMessages) is the one implementation;
// the interface lives here because this package is its consumer.
type cacheCreationTokenSource interface {
	TakeCacheCreationTokens() int
}

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
	ctx context.Context,
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

	metrics := &agentdomain.ChatMetrics{
		Duration: time.Since(startTime),
		Usage:    effectiveUsage,
	}

	s.metricsMux.Lock()
	s.metrics[requestID] = metrics
	s.metricsMux.Unlock()

	cached := 0
	if details := effectiveUsage.PromptTokensDetails; details != nil && details.CachedTokens != nil && *details.CachedTokens > 0 {
		cached = int(*details.CachedTokens)
		s.conversationRepo.AddCachedTokens(cached)
	}

	cacheWrite := 0
	if src, ok := s.client.(cacheCreationTokenSource); ok {
		cacheWrite = src.TakeCacheCreationTokens()
	}

	if err := s.conversationRepo.AddTokenUsage(
		model,
		int(effectiveUsage.PromptTokens),
		int(effectiveUsage.CompletionTokens),
		int(effectiveUsage.TotalTokens),
		cached,
		cacheWrite,
	); err != nil {
		logger.Error("failed to add token usage to session", "error", err)
	}

	if s.recorder != nil {
		s.recorder.RecordUsage(model, int(effectiveUsage.PromptTokens), int(effectiveUsage.CompletionTokens), cached, cacheWrite)
	}
	telemetry.SetSpanUsage(ctx, int(effectiveUsage.PromptTokens), int(effectiveUsage.CompletionTokens))

	return effectiveUsage
}

func (s *AgentServiceImpl) optimizeConversation(_ context.Context, req *agentdomain.AgentRequest, conversation []sdk.Message, eventPublisher *eventPublisher) []sdk.Message {
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
	Result convdomain.ConversationEntry
}

// executeToolCallsParallel runs a batch that needs no approval. Approval is
// decided upstream by states.EvaluatingToolsState: batches with a tool that
// requires approval go to ApprovingTools/BlockingTools and never reach here.
func (s *AgentServiceImpl) executeToolCallsParallel(
	ctx context.Context,
	toolCalls []*sdk.ChatCompletionMessageToolCall,
	eventPublisher *eventPublisher,
) []convdomain.ConversationEntry {

	if len(toolCalls) == 0 {
		return []convdomain.ConversationEntry{}
	}

	eventPublisher.publishToolsQueued(func() []sdk.ChatCompletionMessageToolCall {
		calls := make([]sdk.ChatCompletionMessageToolCall, len(toolCalls))
		for i, tc := range toolCalls {
			calls[i] = *tc
		}
		return calls
	}())

	time.Sleep(constants.AgentToolExecutionDelay)

	results := make([]convdomain.ConversationEntry, len(toolCalls))

	resultsChan := make(chan IndexedToolResult, len(toolCalls))
	semaphore := make(chan struct{}, s.config.GetAgentConfig().MaxConcurrentTools)
	panicked := make(chan any, 1)

	var wg sync.WaitGroup
	for i, tc := range toolCalls {
		wg.Add(1)
		go func(index int, toolCall *sdk.ChatCompletionMessageToolCall) {
			defer func() {
				wg.Done()
			}()
			defer func() {
				if r := recover(); r != nil {
					logger.Error("tool goroutine panic", "tool", toolCall.Function.Name, "panic", r, "stack", string(debug.Stack()))
					select {
					case panicked <- r:
					default:
					}
				}
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

			result := s.executeTool(ctx, *toolCall, eventPublisher)

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

	for res := range resultsChan {
		results[res.Index] = res.Result
	}

	select {
	case r := <-panicked:
		panic(r)
	default:
	}

	if err := s.batchSaveToolResults(results); err != nil {
		logger.Error("failed to batch save tool results", "error", err)
	}

	eventPublisher.publishToolExecutionCompleted(results)

	return results
}

func (s *AgentServiceImpl) executeTool(
	ctx context.Context,
	tc sdk.ChatCompletionMessageToolCall,
	eventPublisher *eventPublisher,
) convdomain.ConversationEntry {
	wasApproved := s.stateManager != nil && s.stateManager.GetAgentMode() == agentdomain.AgentModeAutoAccept
	return s.executeToolInternal(ctx, tc, eventPublisher, wasApproved, time.Now())
}

// executeToolInternal runs the tool once and, when the failure is a sandbox
// denial and a user can answer prompts (chat TUI or IPC broker), asks them to
// grant the denied directory and retries. Used by both executeTool() (no
// approval needed) and processNextTool() (approval already obtained).
func (s *AgentServiceImpl) executeToolInternal(
	ctx context.Context,
	tc sdk.ChatCompletionMessageToolCall,
	eventPublisher *eventPublisher,
	wasApproved bool,
	startTime time.Time,
) convdomain.ConversationEntry {
	entry := s.executeToolOnce(ctx, tc, eventPublisher, wasApproved, startTime)
	lastGrant := ""
	for entry.ToolExecution != nil && !entry.ToolExecution.Success && agentdomain.SandboxApprovalAvailable(ctx) {
		path, denied := config.SandboxDeniedPath(entry.ToolExecution.Error)
		if !denied {
			break
		}
		dir := sandboxGrantDir(path)
		if dir == lastGrant {
			break
		}
		allow, always := s.requestSandboxApproval(ctx, tc, eventPublisher, dir)
		if !allow {
			break
		}
		lastGrant = dir
		config.AddSandboxDirectory(dir)
		logger.Info("sandbox extended by user approval", "dir", dir, "tool", tc.Function.Name, "persisted", always)
		if always {
			if err := utils.PersistSandboxDirectory(dir, s.config.Tools.Sandbox.Directories); err != nil {
				logger.Error("failed to persist sandbox directory", "dir", dir, "error", err)
			}
		}
		entry = s.executeToolOnce(ctx, tc, eventPublisher, wasApproved, startTime)
	}
	return entry
}

// sandboxGrantDir maps a denied path to the directory worth granting: the path
// itself when it is an existing directory, otherwise its parent.
func sandboxGrantDir(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if info, err := os.Stat(abs); err == nil && info.IsDir() {
		return abs
	}
	return filepath.Dir(abs)
}

// requestSandboxApproval asks the user - through the standard approval
// pipeline, as a synthetic SandboxAccess tool call - to allow dir outside the
// sandbox. Approve grants it for this session; auto-accept ("always") also
// persists it to the userspace config.
func (s *AgentServiceImpl) requestSandboxApproval(
	ctx context.Context,
	tc sdk.ChatCompletionMessageToolCall,
	eventPublisher *eventPublisher,
	dir string,
) (allow, always bool) {
	args, err := json.Marshal(map[string]string{"path": dir, "tool": tc.Function.Name})
	if err != nil {
		return false, false
	}

	responseChan := make(chan agentdomain.ApprovalAction, 1)
	eventPublisher.chatEvents <- agentdomain.ToolApprovalRequestedEvent{
		RequestID: eventPublisher.requestID,
		Timestamp: time.Now(),
		ToolCall: sdk.ChatCompletionMessageToolCall{
			ID:   tc.ID + "-sandbox",
			Type: tc.Type,
			Function: sdk.ChatCompletionMessageToolCallFunction{
				Name:      "SandboxAccess",
				Arguments: string(args),
			},
		},
		ResponseChan: responseChan,
	}

	select {
	case response := <-responseChan:
		allow = response == agentdomain.ApprovalApprove || response == agentdomain.ApprovalAutoAccept
		return allow, response == agentdomain.ApprovalAutoAccept
	case <-ctx.Done():
	case <-time.After(constants.ApprovalTimeout):
	}
	return false, false
}

// executeToolOnce performs the actual tool execution without approval checks
//
//nolint:funlen,gocyclo,cyclop // Tool execution requires comprehensive error handling and status updates
func (s *AgentServiceImpl) executeToolOnce(
	ctx context.Context,
	tc sdk.ChatCompletionMessageToolCall,
	eventPublisher *eventPublisher,
	wasApproved bool,
	startTime time.Time,
) (finalEntry convdomain.ConversationEntry) {
	eventPublisher.publishToolStatusChange(tc.ID, tc.Function.Name, "running", "Executing...", nil)

	defer func() {
		s.trackRepeatedFailure(tc, finalEntry)
	}()

	defer func() {
		status, message := "completed", "Completed successfully"
		var images []agentdomain.ImageAttachment
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
		execCtx = agentdomain.WithAgentMode(execCtx, s.stateManager.GetAgentMode())
	}

	if agentdomain.GetSessionID(execCtx) == "" && s.conversationRepo != nil {
		if convID := s.conversationRepo.GetCurrentConversationID(); convID != "" {
			execCtx = agentdomain.WithSessionID(execCtx, convID)
		}
	}
	if wasApproved {
		execCtx = agentdomain.WithToolApproved(execCtx)
	}
	execCtx = agentdomain.WithToolCallID(execCtx, tc.ID)
	execCtx = agentdomain.WithToolProgressCallback(execCtx, func(message string) {
		eventPublisher.publishToolProgress(tc.ID, tc.Function.Name, message)
	})

	if tc.Function.Name == "Bash" {
		bashCallback := func(line string) {
			eventPublisher.publishBashOutputChunk(tc.ID, line, false)
		}
		execCtx = agentdomain.WithBashOutputCallback(execCtx, bashCallback)

		detachChan := make(chan struct{}, 1)
		if chatHandler := agentdomain.GetChatHandler(ctx); chatHandler != nil {
			chatHandler.SetBashDetachChan(detachChan)

			defer func() {
				chatHandler.ClearBashDetachChan()
			}()
		}
		execCtx = agentdomain.WithBashDetachChannel(execCtx, detachChan)
	}

	if tc.Function.Name == "AskUserQuestion" && agentdomain.GetChatHandler(ctx) != nil {
		execCtx = agentdomain.WithUserQuestionBroker(execCtx, &chatQuestionBroker{publisher: eventPublisher})
	}

	if tc.Function.Name == "RequestApproval" && agentdomain.GetChatHandler(ctx) != nil {
		execCtx = agentdomain.WithApprovalEscalation(execCtx, &approvalEscalator{svc: s, publisher: eventPublisher})
	}

	resultChan := make(chan struct {
		result *agentdomain.ToolExecutionResult
		err    error
	}, 1)

	go func() {
		result, err := s.toolService.ExecuteTool(execCtx, tc.Function)
		resultChan <- struct {
			result *agentdomain.ToolExecutionResult
			err    error
		}{result, err}
	}()

	ticker := time.NewTicker(constants.AgentStatusTickerInterval)
	defer ticker.Stop()

	var result *agentdomain.ToolExecutionResult
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

	toolExecutionResult := &agentdomain.ToolExecutionResult{
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
		if todoResult, ok := result.Data.(*agentdomain.TodoWriteToolResult); ok && todoResult != nil {
			if s.stateManager != nil {
				s.stateManager.SetTodos(todoResult.Todos)
			}
			eventPublisher.publishTodoUpdate(todoResult.Todos)
		}
	}

	if result.ToolName == "RequestPlanApproval" && result.Success {
		if extractPlanContent(result) == "" {
			logger.Warn("requestPlanApproval succeeded but plan content is empty")
		}
	}

	formattedContent := s.conversationRepo.FormatToolResultForLLM(toolExecutionResult)

	entry := convdomain.ConversationEntry{
		Message: sdk.Message{
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

// handleToolResults appends tool results to the conversation and returns true
// when the agent should stop (a plan is awaiting approval). Rejections cannot
// occur on this route; the state machine's ApprovingToolsState is the only
// place a rejection ends the turn.
func (s *AgentServiceImpl) handleToolResults(
	toolResults []convdomain.ConversationEntry,
	conversation *[]sdk.Message,
	eventPublisher *eventPublisher,
	req *agentdomain.AgentRequest,
) bool {
	planContent, planID := s.checkPlanApproval(toolResults)

	s.addToolResultsToConversation(toolResults, conversation, req.Model)

	if planContent != "" {
		s.createPlanMessage(planContent, planID, conversation, eventPublisher, req)
		return true
	}

	return false
}

// checkPlanApproval returns the plan content and ID when a RequestPlanApproval
// tool succeeded in the batch.
func (s *AgentServiceImpl) checkPlanApproval(toolResults []convdomain.ConversationEntry) (planContent, planID string) {
	for _, entry := range toolResults {
		if entry.ToolExecution == nil || entry.ToolExecution.ToolName != "RequestPlanApproval" || !entry.ToolExecution.Success {
			continue
		}
		planContent = extractPlanContent(entry.ToolExecution)
		planID = extractPlanID(entry.ToolExecution)
		logger.Info("requestPlanApproval tool executed - stopping agent loop to wait for user approval", "planLength", len(planContent))
	}
	return planContent, planID
}

// addToolResultsToConversation adds tool results and images to the conversation
func (s *AgentServiceImpl) addToolResultsToConversation(toolResults []convdomain.ConversationEntry, conversation *[]sdk.Message, model string) {
	for _, entry := range toolResults {
		toolResult := sdk.Message{
			Role:       sdk.Tool,
			Content:    entry.Message.Content,
			ToolCallID: entry.Message.ToolCallID,
		}
		*conversation = append(*conversation, toolResult)
	}

	s.addImageMessageFromToolResults(toolResults, conversation, model)
}

// createPlanMessage creates and stores a plan message for approval
func (s *AgentServiceImpl) createPlanMessage(
	planContent string,
	planID string,
	conversation *[]sdk.Message,
	eventPublisher *eventPublisher,
	req *agentdomain.AgentRequest,
) {
	planMessage := sdk.Message{
		Role:    sdk.Assistant,
		Content: sdk.NewMessageContent(planContent),
	}
	*conversation = append(*conversation, planMessage)

	planEntry := convdomain.ConversationEntry{
		Message:            planMessage,
		Time:               time.Now(),
		IsPlan:             true,
		PlanApprovalStatus: convdomain.PlanApprovalPending,
	}
	if err := s.conversationRepo.AddMessage(planEntry); err != nil {
		logger.Error("failed to store plan message", "error", err)
	}

	eventPublisher.publishPlanApprovalRequest(planContent, planID)

	logger.Info("plan approval requested - stopping agent loop")
	eventPublisher.publishChatComplete("", []sdk.ChatCompletionMessageToolCall{}, s.GetMetrics(req.RequestID))
}

// extractPlanContent extracts plan content from RequestPlanApproval tool result
func extractPlanContent(result *agentdomain.ToolExecutionResult) string {
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

// extractPlanID extracts the stored plan ID from a RequestPlanApproval tool
// result. The ID lets the post-approval continuation prompt point the agent
// back at the stored plan (via `infer plans show <id>`) after the planning
// context is compacted away.
func extractPlanID(result *agentdomain.ToolExecutionResult) string {
	if result == nil || result.Data == nil {
		return ""
	}

	data, ok := result.Data.(map[string]any)
	if !ok {
		return ""
	}

	id, ok := data["plan_id"].(string)
	if !ok {
		return ""
	}

	return id
}

// addImageMessageFromToolResults adds images from tool results as a separate hidden user message
// This ensures compatibility with all providers (Anthropic requires tool messages to be text-only)
func (s *AgentServiceImpl) addImageMessageFromToolResults(toolResults []convdomain.ConversationEntry, conversation *[]sdk.Message, model string) {
	imageMessage := s.createImageMessageFromToolResults(toolResults, model)
	if imageMessage == nil {
		return
	}

	*conversation = append(*conversation, *imageMessage)

	imageEntry := convdomain.ConversationEntry{
		Message: *imageMessage,
		Time:    time.Now(),
		Hidden:  true,
	}
	if err := s.conversationRepo.AddMessage(imageEntry); err != nil {
		logger.Error("failed to add image message from tool results", "error", err)
	}
}

// createImageMessageFromToolResults creates a hidden user message containing images from tool results.
// Non-vision models get text path notes (pointing at ImageDecode) instead of raw
// image parts, which they reject. Returns nil if no images are present.
func (s *AgentServiceImpl) createImageMessageFromToolResults(toolResults []convdomain.ConversationEntry, model string) *sdk.Message {
	var allImages []agentdomain.ImageAttachment

	for _, result := range toolResults {
		if result.ToolExecution != nil && len(result.ToolExecution.Images) > 0 {
			allImages = append(allImages, result.ToolExecution.Images...)
		}
	}

	if len(allImages) == 0 {
		return nil
	}

	supportsVision := models.SupportsVision(model)

	var contentParts []sdk.ContentPart
	textPart, err := sdk.NewTextContentPart(fmt.Sprintf("Tool execution returned %d image(s) for analysis:", len(allImages)))
	if err == nil {
		contentParts = append(contentParts, textPart)
	}

	for i, img := range allImages {
		if !supportsVision {
			if note := agentdomain.ImagePathNote(img); note != "" {
				if notePart, err := sdk.NewTextContentPart(note); err == nil {
					contentParts = append(contentParts, notePart)
				}
			}
			continue
		}
		dataURL := fmt.Sprintf("data:%s;base64,%s", img.MimeType, img.Data)
		imagePart, err := sdk.NewImageContentPart(dataURL, nil)
		if err != nil {
			logger.Warn("failed to create image content part", "index", i, "filename", img.Filename, "error", err)
			continue
		}
		contentParts = append(contentParts, imagePart)
	}

	if len(contentParts) <= 1 {
		logger.Warn("no content parts created for image message from tool results")
		return nil
	}

	return &sdk.Message{
		Role:    sdk.User,
		Content: sdk.NewMessageContent(contentParts),
	}
}

// requestToolApproval requests approval for a tool and waits for the response.
// The auto-with-judge mode (or approval_behaviour "judge") hands the decision
// to the LLM judge instead of a human; it returns (false, judgeReason, nil)
// so the rejection tool result can carry the judge's reasoning and, unlike a
// human rejection, not end the turn. Human decisions return an empty reason.
func (s *AgentServiceImpl) requestToolApproval(
	ctx context.Context,
	tc sdk.ChatCompletionMessageToolCall,
	eventPublisher *eventPublisher,
) (bool, string, error) {
	if s.judgeDecides(tc) {
		return s.requestJudgeApproval(ctx, tc, eventPublisher)
	}

	return s.requestHumanApproval(ctx, tc, eventPublisher, "")
}

// requestHumanApproval publishes the approval prompt for tc and waits for the
// human decision. note is optional context rendered above the call (the
// RequestApproval escalation uses it for the judge's reason and the agent's
// justification). A closed response channel counts as a rejection.
func (s *AgentServiceImpl) requestHumanApproval(
	ctx context.Context,
	tc sdk.ChatCompletionMessageToolCall,
	eventPublisher *eventPublisher,
	note string,
) (bool, string, error) {
	responseChan := make(chan agentdomain.ApprovalAction, 1)
	eventPublisher.chatEvents <- agentdomain.ToolApprovalRequestedEvent{
		RequestID:    eventPublisher.requestID,
		Timestamp:    time.Now(),
		ToolCall:     tc,
		Context:      note,
		ResponseChan: responseChan,
	}

	var approved bool
	var err error

	select {
	case response, open := <-responseChan:
		if !open {
			break
		}
		if response == agentdomain.ApprovalAutoAccept {
			logger.Info("switching to auto-accept mode from approval response")
			s.stateManager.SetAgentMode(agentdomain.AgentModeAutoAccept)
		}
		approved = response == agentdomain.ApprovalApprove || response == agentdomain.ApprovalAutoAccept
	case <-ctx.Done():
		err = fmt.Errorf("approval request cancelled: %w", ctx.Err())
	case <-time.After(constants.ApprovalTimeout):
		err = fmt.Errorf("approval request timed out")
	}

	if err != nil || !approved {
		s.conversationRepo.RemovePendingToolCallByID(tc.ID)
	}

	return approved, "", err
}

// judgeDecides reports whether the LLM judge answers this approval instead
// of a human: the auto-with-judge mode forces the judge, or the config
// selects approval_behaviour "judge". The judge is always reachable, so
// broker/chat delivery details never downgrade it.
func (s *AgentServiceImpl) judgeDecides(tc sdk.ChatCompletionMessageToolCall) bool {
	if s.stateManager != nil && s.stateManager.GetAgentMode() == agentdomain.AgentModeAutoWithJudge {
		return true
	}
	return s.config != nil && s.config.ApprovalBehaviourFor(tc.Function.Name) == config.ApprovalBehaviourJudge
}

// SetCurrentModelFn wires the session's current-model accessor so the judge
// follows a model picked at runtime, not only the configured agent.model.
func (s *AgentServiceImpl) SetCurrentModelFn(fn func() string) {
	s.currentModel = fn
}

// judgeModel resolves who judges: judge.model, else the session's current
// model, else agent.model.
func (s *AgentServiceImpl) judgeModel() string {
	current := ""
	if s.currentModel != nil {
		current = s.currentModel()
	}
	return s.config.Judge.ResolveModel(cmp.Or(current, s.config.Agent.Model))
}

// requestJudgeApproval asks the judge to decide one pending tool call against
// the user's latest request, publishes the verdict as a chat event (mirrored
// on the hidden debug channel), and maps a rejection onto the standard
// refusal path so the driver sees the judge's reason and adjusts.
func (s *AgentServiceImpl) requestJudgeApproval(
	ctx context.Context,
	tc sdk.ChatCompletionMessageToolCall,
	eventPublisher *eventPublisher,
) (bool, string, error) {
	if s.consumeJudgeBypass(tc) {
		return true, "", nil
	}

	model := s.judgeModel()
	root, latest := userIntents(s.conversationRepo)
	verdict, err := s.judge.Judge(ctx, agentdomain.JudgeInput{Model: model, RootIntent: root, Intent: latest, Action: judgeActionInput(tc)})
	if err != nil {
		verdict = agentdomain.JudgeVerdict{Decision: agentdomain.JudgeDecisionRejected, Reason: "judge unavailable: " + err.Error()}
	}
	if !verdict.Approved() {
		s.conversationRepo.RemovePendingToolCallByID(tc.ID)
		s.escalations.record(tc.Function.Name, tc.Function.Arguments, verdict.Reason)
	}
	s.recordJudgeUsage(verdict.Usage)

	eventPublisher.chatEvents <- agentdomain.JudgeVerdictChatEvent{
		BaseChatEvent: agentdomain.BaseChatEvent{RequestID: eventPublisher.requestID, Timestamp: time.Now()},
		Tool:          tc.Function.Name,
		Model:         model,
		Decision:      verdict.Decision,
		Reason:        verdict.Reason,
		Turn:          int(s.sessionTurns.Load()),
	}
	streamevent.EmitDebugEvent("judge_verdict", map[string]any{
		"tool": tc.Function.Name, "model": model, "decision": verdict.Decision, "reason": verdict.Reason, "turn": s.sessionTurns.Load(),
	})
	if !verdict.Approved() {
		logger.Warn("judge rejected tool call", "tool", tc.Function.Name, "model", model, "reason", verdict.Reason)
	}

	// The tool-result reason names the judge so the driver and the user see who decided.
	// Rejections carry the escalation hint so the driver learns it can ask the
	// user to override (issue #1156).
	if verdict.Approved() {
		return true, "", nil
	}
	return false, model + ": " + verdict.Reason + judgeEscalationHint, nil
}

// recordJudgeUsage adds the judge call's tokens to the session totals and the
// telemetry recorder so the status bar and cost reports include judge spend.
func (s *AgentServiceImpl) recordJudgeUsage(usage *sdk.CompletionUsage) {
	if usage == nil {
		return
	}
	model := s.config.Judge.ResolveModel(s.config.Agent.Model)
	if err := s.conversationRepo.AddTokenUsage(model, int(usage.PromptTokens), int(usage.CompletionTokens), int(usage.TotalTokens), 0, 0); err != nil {
		logger.Error("failed to add judge token usage to session", "error", err)
	}
	if s.recorder != nil {
		s.recorder.RecordUsage(model, int(usage.PromptTokens), int(usage.CompletionTokens), 0, 0)
	}
}

// trackRepeatedFailure counts identical failing tool calls (same name and
// arguments) and, from the third failure on, stores the key so
// injectDueReminders can deliver the on_repeated_failure reminder via the
// reminders pipeline. A success with the same arguments resets the counter.
func (s *AgentServiceImpl) trackRepeatedFailure(tc sdk.ChatCompletionMessageToolCall, entry convdomain.ConversationEntry) {
	key := tc.Function.Name + "\x00" + tc.Function.Arguments
	s.failedCallsMux.Lock()
	defer s.failedCallsMux.Unlock()

	if s.failedCalls == nil {
		s.failedCalls = make(map[string]int)
	}

	if entry.ToolExecution == nil || entry.ToolExecution.Success {
		delete(s.failedCalls, key)
		return
	}

	s.failedCalls[key]++
	if s.failedCalls[key] >= 3 {
		s.repeatedFailureMux.Lock()
		s.repeatedFailureKey = key
		s.repeatedFailureMux.Unlock()
	}
}

// takeRepeatedFailure reads and clears the repeated-failure key stored by
// trackRepeatedFailure, returning the tool name and failure count. Returns ("", 0)
// when no failure met the threshold. Called by injectDueReminders at post_tool.
func (s *AgentServiceImpl) takeRepeatedFailure() (string, int) {
	s.repeatedFailureMux.Lock()
	defer s.repeatedFailureMux.Unlock()
	key := s.repeatedFailureKey
	s.repeatedFailureKey = ""

	if key == "" {
		return "", 0
	}
	s.failedCallsMux.Lock()
	n := s.failedCalls[key]
	s.failedCallsMux.Unlock()

	if i := strings.IndexByte(key, '\x00'); i >= 0 {
		return key[:i], n
	}
	return key, n
}

func (s *AgentServiceImpl) createErrorEntry(tc sdk.ChatCompletionMessageToolCall, err error, startTime time.Time) convdomain.ConversationEntry {
	return convdomain.ConversationEntry{
		Message: sdk.Message{
			Role:       sdk.Tool,
			Content:    sdk.NewMessageContent(fmt.Sprintf("Tool execution failed: %s - %s", tc.Function.Name, err.Error())),
			ToolCallID: &tc.ID,
		},
		Time: time.Now(),
		ToolExecution: &agentdomain.ToolExecutionResult{
			ToolName:  tc.Function.Name,
			Arguments: make(map[string]any),
			Success:   false,
			Duration:  time.Since(startTime),
			Error:     err.Error(),
		},
	}
}

func (s *AgentServiceImpl) batchSaveToolResults(entries []convdomain.ConversationEntry) error {
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
