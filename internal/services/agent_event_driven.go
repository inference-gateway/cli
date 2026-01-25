package services

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	sdk "github.com/inference-gateway/sdk"

	constants "github.com/inference-gateway/cli/internal/constants"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// AgentEvent represents an event in the event-driven agent system
type AgentEvent interface {
	EventType() string
}

// MessageReceivedEvent is triggered when a new message arrives
type MessageReceivedEvent struct {
	Message sdk.Message
}

func (e MessageReceivedEvent) EventType() string { return "MessageReceived" }

// StreamCompletedEvent is triggered when LLM streaming completes
type StreamCompletedEvent struct {
	Message            sdk.Message
	ToolCalls          []*sdk.ChatCompletionMessageToolCall
	Reasoning          string
	Usage              *sdk.CompletionUsage
	IterationStartTime time.Time
}

func (e StreamCompletedEvent) EventType() string { return "StreamCompleted" }

// ToolsCompletedEvent is triggered when all tools finish executing
type ToolsCompletedEvent struct {
	Results []domain.ConversationEntry
}

func (e ToolsCompletedEvent) EventType() string { return "ToolsCompleted" }

// CompletionRequestedEvent is triggered when the agent should complete
type CompletionRequestedEvent struct{}

func (e CompletionRequestedEvent) EventType() string { return "CompletionRequested" }

// StartStreamingEvent is triggered when the agent should start streaming
type StartStreamingEvent struct{}

func (e StartStreamingEvent) EventType() string { return "StartStreaming" }

// ProcessNextToolEvent is triggered to process the next tool in the queue
type ProcessNextToolEvent struct {
	ToolIndex int
}

func (e ProcessNextToolEvent) EventType() string { return "ProcessNextTool" }

// AllToolsProcessedEvent is triggered when all tools have been processed (approved/rejected/executed)
type AllToolsProcessedEvent struct{}

func (e AllToolsProcessedEvent) EventType() string { return "AllToolsProcessed" }

// ApprovalFailedEvent is triggered when approval fails (timeout, error, etc.)
type ApprovalFailedEvent struct {
	Error error
}

func (e ApprovalFailedEvent) EventType() string { return "ApprovalFailed" }

// EventDrivenAgent manages agent execution using event-driven state machine
type EventDrivenAgent struct {
	// Core dependencies
	service        *AgentServiceImpl
	stateMachine   domain.AgentStateMachine
	agentCtx       *domain.AgentContext
	eventPublisher *eventPublisher
	cancelChan     <-chan struct{}
	req            *domain.AgentRequest
	provider       string
	model          string
	taskTracker    domain.TaskTracker

	// Event channel
	events chan AgentEvent

	// State data
	currentMessage   sdk.Message
	currentToolCalls []*sdk.ChatCompletionMessageToolCall
	currentReasoning string
	availableTools   []sdk.ChatCompletionTool

	// Tool processing state (for sequential approval and execution)
	toolsNeedingApproval []sdk.ChatCompletionMessageToolCall
	currentToolIndex     int
	toolResults          []domain.ConversationEntry

	// Synchronization
	mu sync.Mutex
	wg sync.WaitGroup

	// Testability - can be overridden in tests
	toolExecutor func()
}

// NewEventDrivenAgent creates a new event-driven agent
func NewEventDrivenAgent(
	service *AgentServiceImpl,
	ctx context.Context,
	req *domain.AgentRequest,
	conversation *[]sdk.Message,
	eventPublisher *eventPublisher,
	cancelChan <-chan struct{},
	provider string,
	model string,
	taskTracker domain.TaskTracker,
) *EventDrivenAgent {
	stateMachine := NewAgentStateMachine(service.stateManager)

	agentCtx := &domain.AgentContext{
		RequestID:        req.RequestID,
		Conversation:     conversation,
		MessageQueue:     service.messageQueue,
		ConversationRepo: service.conversationRepo,
		ToolCalls:        nil,
		Turns:            0,
		MaxTurns:         service.config.GetAgentConfig().MaxTurns,
		HasToolResults:   false,
		ApprovalPolicy:   service.approvalPolicy,
		Ctx:              ctx,
		IsChatMode:       req.IsChatMode,
	}

	agent := &EventDrivenAgent{
		service:        service,
		stateMachine:   stateMachine,
		agentCtx:       agentCtx,
		eventPublisher: eventPublisher,
		cancelChan:     cancelChan,
		req:            req,
		provider:       provider,
		model:          model,
		taskTracker:    taskTracker,
		events:         make(chan AgentEvent, constants.EventChannelBufferSize),
	}

	agent.toolExecutor = agent.executeTools

	return agent
}

// Start begins the event-driven agent execution
func (a *EventDrivenAgent) Start() {
	logger.Info("🚀 Starting event-driven agent",
		"request_id", a.req.RequestID,
		"max_turns", a.agentCtx.MaxTurns)

	_ = a.stateMachine.Transition(a.agentCtx, domain.StateIdle)

	a.wg.Add(1)
	go a.processEvents()

	logger.Debug("📬 Triggering initial MessageReceivedEvent")
	a.events <- MessageReceivedEvent{}
}

// Wait waits for the agent to complete
func (a *EventDrivenAgent) Wait() {
	a.wg.Wait()
	close(a.events)
}

// processEvents is the main event processing loop
func (a *EventDrivenAgent) processEvents() {
	defer a.wg.Done()
	defer logger.Info("🏁 Agent event processing stopped", "request_id", a.req.RequestID)

	for {
		select {
		case <-a.cancelChan:
			logger.Warn("❌ Agent cancelled", "request_id", a.req.RequestID)
			_ = a.stateMachine.Transition(a.agentCtx, domain.StateCancelled)
			a.eventPublisher.publishChatComplete("", []sdk.ChatCompletionMessageToolCall{}, a.service.GetMetrics(a.req.RequestID))
			return

		case event, ok := <-a.events:
			if !ok {
				logger.Debug("📭 Event channel closed")
				return
			}

			logger.Debug("📨 Received event",
				"event_type", event.EventType(),
				"current_state", a.stateMachine.GetCurrentState(),
				"turn", a.agentCtx.Turns)

			a.handleEvent(event)

			currentState := a.stateMachine.GetCurrentState()
			if currentState == domain.StateIdle ||
				currentState == domain.StateStopped ||
				currentState == domain.StateCancelled ||
				currentState == domain.StateError {
				logger.Info("✅ Agent reached terminal state",
					"state", currentState,
					"total_turns", a.agentCtx.Turns)
				return
			}
		}
	}
}

// handleEvent processes a single event based on current state
func (a *EventDrivenAgent) handleEvent(event AgentEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()

	currentState := a.stateMachine.GetCurrentState()
	logger.Info("🎯 Handling event in state",
		"event", event.EventType(),
		"state", currentState,
		"turn", a.agentCtx.Turns,
		"queue_empty", a.agentCtx.MessageQueue.IsEmpty())

	switch currentState {
	case domain.StateIdle:
		a.handleIdleState(event)

	case domain.StateCheckingQueue:
		a.handleCheckingQueueState(event)

	case domain.StateStreamingLLM:
		a.handleStreamingState(event)

	case domain.StatePostStream:
		a.handlePostStreamState(event)

	case domain.StateEvaluatingTools:
		a.handleEvaluatingToolsState(event)

	case domain.StateApprovingTools:
		a.handleApprovingToolsState(event)

	case domain.StateExecutingTools:
		a.handleExecutingToolsState(event)

	case domain.StatePostToolExecution:
		a.handlePostToolExecutionState(event)

	case domain.StateCompleting:
		a.handleCompletingState(event)
	}

	logger.Debug("✓ Event handled",
		"event", event.EventType(),
		"new_state", a.stateMachine.GetCurrentState())
}

// handleIdleState handles events in Idle state
func (a *EventDrivenAgent) handleIdleState(event AgentEvent) {
	switch event.(type) {
	case MessageReceivedEvent:
		logger.Info("⏭️  STATE TRANSITION: Idle → CheckingQueue")
		if err := a.stateMachine.Transition(a.agentCtx, domain.StateCheckingQueue); err != nil {
			logger.Error("❌ Failed to transition to CheckingQueue", "error", err)
			return
		}
		logger.Debug("🔄 Re-emitting MessageReceivedEvent in CheckingQueue state")
		a.events <- event
	}
}

// handleCheckingQueueState handles events in CheckingQueue state
func (a *EventDrivenAgent) handleCheckingQueueState(event AgentEvent) {
	switch event.(type) {
	case MessageReceivedEvent:
		logger.Info("🔍 CheckingQueue state: evaluating conditions",
			"turns", a.agentCtx.Turns,
			"has_tool_results", a.agentCtx.HasToolResults,
			"queue_empty", a.agentCtx.MessageQueue.IsEmpty())

		if a.agentCtx.HasToolResults {
			logger.Info("🔧 Has tool results - MUST respond to tools before processing queue")
			logger.Info("⏭️  STATE TRANSITION: CheckingQueue → StreamingLLM (respond to tools)")
			if err := a.stateMachine.Transition(a.agentCtx, domain.StateStreamingLLM); err != nil {
				logger.Error("❌ Failed to transition to StreamingLLM", "error", err)
				return
			}

			a.events <- StartStreamingEvent{}
			return
		}

		if !a.agentCtx.MessageQueue.IsEmpty() {
			logger.Info("📥 Queue not empty, draining...")
			numBatched := a.service.batchDrainQueue(a.agentCtx.Conversation, a.eventPublisher)
			logger.Info("✅ Batched queued messages", "count", numBatched)
		}

		if a.taskTracker != nil && len(a.taskTracker.GetAllPollingTasks()) > 0 {
			numTasks := len(a.taskTracker.GetAllPollingTasks())
			logger.Info("⏳ Background tasks pending, waiting...", "tasks", numTasks)
			a.wg.Add(1)
			go func() {
				defer a.wg.Done()
				select {
				case <-time.After(constants.BackgroundTaskPollDelay):
					select {
					case a.events <- MessageReceivedEvent{}:
					case <-a.cancelChan:
						return
					}
				case <-a.cancelChan:
					return
				}
			}()
			return
		}

		if a.stateMachine.CanTransition(a.agentCtx, domain.StateCompleting) {
			logger.Info("⏭️  STATE TRANSITION: CheckingQueue → Completing (can complete)")
			logger.Debug("🤔 Completion conditions met",
				"turns", a.agentCtx.Turns,
				"has_tool_results", a.agentCtx.HasToolResults,
				"queue_empty", a.agentCtx.MessageQueue.IsEmpty(),
				"last_message_role", func() string {
					if len(*a.agentCtx.Conversation) > 0 {
						return string((*a.agentCtx.Conversation)[len(*a.agentCtx.Conversation)-1].Role)
					}
					return "none"
				}())

			if err := a.stateMachine.Transition(a.agentCtx, domain.StateCompleting); err != nil {
				logger.Error("❌ Failed to transition to Completing", "error", err)
				return
			}
			a.events <- CompletionRequestedEvent{}
			return
		}

		logger.Info("⏭️  STATE TRANSITION: CheckingQueue → StreamingLLM")
		logger.Debug("🔄 Cannot complete yet, continuing agent loop",
			"turns", a.agentCtx.Turns,
			"has_tool_results", a.agentCtx.HasToolResults,
			"queue_empty", a.agentCtx.MessageQueue.IsEmpty(),
			"last_message_role", func() string {
				if len(*a.agentCtx.Conversation) > 0 {
					return string((*a.agentCtx.Conversation)[len(*a.agentCtx.Conversation)-1].Role)
				}
				return "none"
			}())

		if err := a.stateMachine.Transition(a.agentCtx, domain.StateStreamingLLM); err != nil {
			logger.Error("❌ Failed to transition to StreamingLLM", "error", err)
			return
		}

		logger.Info("🌊 Emitting StartStreamingEvent", "turn", a.agentCtx.Turns+1)
		a.events <- StartStreamingEvent{}
	}
}

// prepareStreamingContext sets up the streaming context
func (a *EventDrivenAgent) prepareStreamingContext() (context.Context, context.CancelFunc) {
	a.agentCtx.Turns++
	a.agentCtx.HasToolResults = false
	a.service.clearToolCallsMap()

	logger.Info("🎬 Starting streaming turn",
		"turn", a.agentCtx.Turns,
		"max_turns", a.agentCtx.MaxTurns,
		"conversation_length", len(*a.agentCtx.Conversation))

	if a.agentCtx.Turns > 1 {
		time.Sleep(constants.AgentIterationDelay)
	}

	requestCtx, requestCancel := context.WithTimeout(a.agentCtx.Ctx, time.Duration(a.service.timeoutSeconds)*time.Second)

	a.service.requestsMux.Lock()
	a.service.activeRequests[a.req.RequestID] = requestCancel
	a.service.requestsMux.Unlock()

	a.eventPublisher.publishChatStart()

	return requestCtx, requestCancel
}

// injectSystemReminderIfNeeded adds system reminder to conversation if needed
func (a *EventDrivenAgent) injectSystemReminderIfNeeded() {
	if !a.service.shouldInjectSystemReminder(a.agentCtx.Turns) {
		return
	}

	systemReminderMsg := a.service.getSystemReminderMessage()
	*a.agentCtx.Conversation = append(*a.agentCtx.Conversation, systemReminderMsg)

	reminderEntry := domain.ConversationEntry{
		Message: systemReminderMsg,
		Time:    time.Now(),
		Hidden:  true,
	}

	if err := a.service.conversationRepo.AddMessage(reminderEntry); err != nil {
		logger.Error("failed to store system reminder message", "error", err)
	}
}

// prepareStreamClient creates and configures the streaming client
func (a *EventDrivenAgent) prepareStreamClient() domain.SDKClient {
	mode := domain.AgentModeStandard
	if a.service.stateManager != nil {
		mode = a.service.stateManager.GetAgentMode()
	}

	a.availableTools = a.service.toolService.ListToolsForMode(mode)

	client := a.service.client.
		WithOptions(&sdk.CreateChatCompletionRequest{
			MaxTokens: &a.service.maxTokens,
			StreamOptions: &sdk.ChatCompletionStreamOptions{
				IncludeUsage: true,
			},
		}).
		WithMiddlewareOptions(&sdk.MiddlewareOptions{
			SkipMCP: true,
		})

	if len(a.availableTools) > 0 {
		client = client.WithTools(&a.availableTools)
	}

	return client
}

// startStreaming begins LLM streaming (runs in background)
func (a *EventDrivenAgent) startStreaming() {
	iterationStartTime := time.Now()

	requestCtx, requestCancel := a.prepareStreamingContext()
	defer requestCancel()

	defer func() {
		a.service.requestsMux.Lock()
		delete(a.service.activeRequests, a.req.RequestID)
		a.service.requestsMux.Unlock()
	}()

	a.injectSystemReminderIfNeeded()

	client := a.prepareStreamClient()

	events, err := client.GenerateContentStream(requestCtx, sdk.Provider(a.provider), a.model, *a.agentCtx.Conversation)
	if err != nil {
		logger.Error("Failed to create stream", "error", err, "turn", a.agentCtx.Turns)
		a.eventPublisher.chatEvents <- domain.ChatErrorEvent{
			RequestID: a.req.RequestID,
			Timestamp: time.Now(),
			Error:     err,
		}
		_ = a.stateMachine.Transition(a.agentCtx, domain.StateError)
		return
	}

	a.processStreamEvents(requestCtx, events, iterationStartTime)
}

// processStreamChoice processes a single stream choice delta
func (a *EventDrivenAgent) processStreamChoice(
	choice sdk.ChatCompletionStreamChoice,
	message *sdk.Message,
	allToolCallDeltas *[]sdk.ChatCompletionMessageToolCallChunk,
) {
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
	if choice.Delta.Reasoning != nil && *choice.Delta.Reasoning != "" {
		reasoning = *choice.Delta.Reasoning
	} else if choice.Delta.ReasoningContent != nil && *choice.Delta.ReasoningContent != "" {
		reasoning = *choice.Delta.ReasoningContent
	}

	if len(choice.Delta.ToolCalls) > 0 {
		*allToolCallDeltas = append(*allToolCallDeltas, choice.Delta.ToolCalls...)
	}

	if deltaContent != "" || reasoning != "" || len(choice.Delta.ToolCalls) > 0 {
		a.eventPublisher.publishChatChunk(deltaContent, reasoning, choice.Delta.ToolCalls)
	}
}

// processStreamEvents processes incoming stream events
func (a *EventDrivenAgent) processStreamEvents(requestCtx context.Context, events <-chan sdk.SSEvent, iterationStartTime time.Time) {
	var allToolCallDeltas []sdk.ChatCompletionMessageToolCallChunk
	var message sdk.Message
	var streamUsage *sdk.CompletionUsage

	for event := range events {
		select {
		case <-requestCtx.Done():
			a.handleStreamTimeout(requestCtx)
			return
		default:
		}

		if event.Event == nil || event.Data == nil {
			continue
		}

		var streamResponse sdk.CreateChatCompletionStreamResponse
		if err := json.Unmarshal(*event.Data, &streamResponse); err != nil {
			logger.Error("failed to unmarshal stream response")
			continue
		}

		for _, choice := range streamResponse.Choices {
			a.processStreamChoice(choice, &message, &allToolCallDeltas)
		}

		if streamResponse.Usage != nil {
			streamUsage = streamResponse.Usage
		}
	}

	a.finalizeStreamCompletion(message, allToolCallDeltas, streamUsage, iterationStartTime)
}

// handleStreamTimeout handles stream timeout errors
func (a *EventDrivenAgent) handleStreamTimeout(requestCtx context.Context) {
	if requestCtx.Err() == context.DeadlineExceeded {
		logger.Error("stream timeout", "error", requestCtx.Err())
		a.eventPublisher.chatEvents <- domain.ChatErrorEvent{
			RequestID: a.req.RequestID,
			Timestamp: time.Now(),
			Error:     fmt.Errorf("stream timed out after %d seconds", a.service.timeoutSeconds),
		}
	}
	_ = a.stateMachine.Transition(a.agentCtx, domain.StateError)
}

// finalizeStreamCompletion finalizes the stream and emits completion event
func (a *EventDrivenAgent) finalizeStreamCompletion(
	message sdk.Message,
	allToolCallDeltas []sdk.ChatCompletionMessageToolCallChunk,
	streamUsage *sdk.CompletionUsage,
	iterationStartTime time.Time,
) {
	a.service.accumulateToolCalls(allToolCallDeltas)
	toolCalls := a.service.getAccumulatedToolCalls()

	reasoning := ""
	if message.Reasoning != nil && *message.Reasoning != "" {
		reasoning = *message.Reasoning
	} else if message.ReasoningContent != nil && *message.ReasoningContent != "" {
		reasoning = *message.ReasoningContent
	}

	a.events <- StreamCompletedEvent{
		Message:            message,
		ToolCalls:          toolCalls,
		Reasoning:          reasoning,
		Usage:              streamUsage,
		IterationStartTime: iterationStartTime,
	}
}

// handleStreamingState handles events in StreamingLLM state
func (a *EventDrivenAgent) handleStreamingState(event AgentEvent) {
	switch e := event.(type) {
	case StartStreamingEvent:
		logger.Info("🚀 Starting LLM streaming (background goroutine)", "turn", a.agentCtx.Turns+1)
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			a.startStreaming()
		}()

	case StreamCompletedEvent:
		logger.Info("🎉 Stream completed",
			"turn", a.agentCtx.Turns,
			"tool_calls", len(e.ToolCalls),
			"has_reasoning", e.Reasoning != "")

		a.currentMessage = e.Message
		a.currentToolCalls = e.ToolCalls
		a.currentReasoning = e.Reasoning
		a.agentCtx.ToolCalls = e.ToolCalls

		contentStr, _ := e.Message.Content.AsMessageContent0()
		logger.Debug("💾 Storing stream data",
			"content_length", len(contentStr),
			"reasoning_length", len(e.Reasoning))

		assistantContent := e.Message.Content
		if _, err := assistantContent.AsMessageContent0(); err != nil {
			assistantContent = sdk.NewMessageContent("")
		}
		outputContent, _ := assistantContent.AsMessageContent0()

		var completeToolCalls []sdk.ChatCompletionMessageToolCall
		if len(e.ToolCalls) > 0 {
			completeToolCalls = make([]sdk.ChatCompletionMessageToolCall, 0, len(e.ToolCalls))
			for _, tc := range e.ToolCalls {
				completeToolCalls = append(completeToolCalls, *tc)
			}
		}

		polyfillInput := &storeIterationMetricsInput{
			inputMessages:   (*a.agentCtx.Conversation)[:len(*a.agentCtx.Conversation)],
			outputContent:   outputContent,
			outputToolCalls: completeToolCalls,
			availableTools:  a.availableTools,
		}

		a.service.storeIterationMetrics(a.req.RequestID, a.req.Model, e.IterationStartTime, e.Usage, polyfillInput)

		logger.Info("⏭️  STATE TRANSITION: StreamingLLM → PostStream")
		if err := a.stateMachine.Transition(a.agentCtx, domain.StatePostStream); err != nil {
			logger.Error("❌ Failed to transition to PostStream", "error", err)
			return
		}

		a.events <- MessageReceivedEvent{}
	}
}

// handlePostStreamState handles events in PostStream state
func (a *EventDrivenAgent) handlePostStreamState(_ AgentEvent) {
	logger.Info("🔄 PostStream state: evaluating next action",
		"turn", a.agentCtx.Turns,
		"tool_calls", len(a.currentToolCalls),
		"queue_empty", a.agentCtx.MessageQueue.IsEmpty())

	logger.Debug("💾 Storing assistant message FIRST (before queue check)",
		"has_tool_calls", len(a.currentToolCalls) > 0,
		"has_reasoning", a.currentReasoning != "")

	assistantContent := a.currentMessage.Content
	if _, err := assistantContent.AsMessageContent0(); err != nil {
		assistantContent = sdk.NewMessageContent("")
	}

	assistantMessage := sdk.Message{
		Role:    sdk.Assistant,
		Content: assistantContent,
	}

	if len(a.currentToolCalls) > 0 {
		assistantToolCalls := make([]sdk.ChatCompletionMessageToolCall, 0, len(a.currentToolCalls))
		for _, tc := range a.currentToolCalls {
			assistantToolCalls = append(assistantToolCalls, *tc)
		}
		assistantMessage.ToolCalls = &assistantToolCalls

		if a.currentReasoning != "" {
			assistantMessage.Reasoning = &a.currentReasoning
			assistantMessage.ReasoningContent = &a.currentReasoning
		}
	}

	*a.agentCtx.Conversation = append(*a.agentCtx.Conversation, assistantMessage)

	assistantEntry := domain.ConversationEntry{
		Message:          assistantMessage,
		ReasoningContent: a.currentReasoning,
		Model:            a.req.Model,
		Time:             time.Now(),
	}

	if err := a.service.conversationRepo.AddMessage(assistantEntry); err != nil {
		logger.Error("failed to store assistant message", "error", err)
	}

	logger.Debug("✅ Assistant message stored in conversation and repository")

	if !a.agentCtx.MessageQueue.IsEmpty() {
		logger.Info("📬 Messages queued during stream, returning to CheckingQueue")
		logger.Info("⏭️  STATE TRANSITION: PostStream → CheckingQueue (queue not empty)")
		if err := a.stateMachine.Transition(a.agentCtx, domain.StateCheckingQueue); err != nil {
			logger.Error("❌ Failed to transition to CheckingQueue", "error", err)
			return
		}
		a.events <- MessageReceivedEvent{}
		return
	}

	if len(a.currentToolCalls) > 0 {
		a.transitionToEvaluatingTools()
		return
	}

	a.handleNoToolCallsScenario()
}

// transitionToEvaluatingTools transitions to tool evaluation state
func (a *EventDrivenAgent) transitionToEvaluatingTools() {
	logger.Info("🔧 Has tool calls, evaluating tools", "count", len(a.currentToolCalls))
	logger.Info("⏭️  STATE TRANSITION: PostStream → EvaluatingTools")
	if err := a.stateMachine.Transition(a.agentCtx, domain.StateEvaluatingTools); err != nil {
		logger.Error("❌ Failed to transition to EvaluatingTools", "error", err)
		return
	}
	a.events <- MessageReceivedEvent{}
}

// handleNoToolCallsScenario handles the scenario when there are no tool calls
func (a *EventDrivenAgent) handleNoToolCallsScenario() {
	a.agentCtx.HasToolResults = false
	logger.Debug("❌ No tool calls in response")

	canComplete := a.agentCtx.Turns > 0 && a.agentCtx.MessageQueue.IsEmpty()
	if canComplete {
		a.transitionToCompleting()
		return
	}

	a.transitionToCheckingQueue()
}

// transitionToCompleting transitions to completing state
func (a *EventDrivenAgent) transitionToCompleting() {
	logger.Info("✅ Agent can complete (no tools, turns > 0, queue empty)")

	var completeToolCalls []sdk.ChatCompletionMessageToolCall
	a.eventPublisher.publishChatComplete(a.currentReasoning, completeToolCalls, a.service.GetMetrics(a.req.RequestID))

	logger.Info("⏭️  STATE TRANSITION: PostStream → Completing")
	if err := a.stateMachine.Transition(a.agentCtx, domain.StateCompleting); err != nil {
		logger.Error("❌ Failed to transition to Completing", "error", err)
		return
	}
	a.events <- CompletionRequestedEvent{}
}

// transitionToCheckingQueue transitions back to checking queue state
func (a *EventDrivenAgent) transitionToCheckingQueue() {
	logger.Info("🔁 Continuing agent loop (need more turns)")
	logger.Info("⏭️  STATE TRANSITION: PostStream → CheckingQueue")
	if err := a.stateMachine.Transition(a.agentCtx, domain.StateCheckingQueue); err != nil {
		logger.Error("❌ Failed to transition to CheckingQueue", "error", err)
		return
	}
	a.events <- MessageReceivedEvent{}
}

// handleEvaluatingToolsState handles events in EvaluatingTools state
func (a *EventDrivenAgent) handleEvaluatingToolsState(_ AgentEvent) {
	logger.Info("🔍 Evaluating tools", "tool_count", len(a.currentToolCalls))

	var completeToolCalls []sdk.ChatCompletionMessageToolCall
	if len(a.currentToolCalls) > 0 {
		completeToolCalls = make([]sdk.ChatCompletionMessageToolCall, 0, len(a.currentToolCalls))
		for _, tc := range a.currentToolCalls {
			completeToolCalls = append(completeToolCalls, *tc)
			logger.Debug("🔧 Tool call", "tool", tc.Function.Name, "id", tc.Id)
		}
	}
	a.eventPublisher.publishChatComplete(a.currentReasoning, completeToolCalls, a.service.GetMetrics(a.req.RequestID))

	needsApproval := false
	if a.service.approvalPolicy != nil {
		for _, toolCall := range a.currentToolCalls {
			if a.service.approvalPolicy.ShouldRequireApproval(a.agentCtx.Ctx, toolCall, a.req.IsChatMode) {
				needsApproval = true
				logger.Debug("🔐 Tool requires approval", "tool", toolCall.Function.Name)
				break
			}
		}
	}

	if needsApproval {
		logger.Info("⏭️  STATE TRANSITION: EvaluatingTools → ApprovingTools (needs approval)")
		if err := a.stateMachine.Transition(a.agentCtx, domain.StateApprovingTools); err != nil {
			logger.Error("❌ Failed to transition to ApprovingTools", "error", err)
			return
		}
		a.events <- MessageReceivedEvent{}
	} else {
		logger.Info("✅ No approval needed, executing tools")
		logger.Info("⏭️  STATE TRANSITION: EvaluatingTools → ExecutingTools")
		if err := a.stateMachine.Transition(a.agentCtx, domain.StateExecutingTools); err != nil {
			logger.Error("❌ Failed to transition to ExecutingTools", "error", err)
			return
		}

		a.wg.Add(1)
		go a.toolExecutor()
	}
}

// handleApprovingToolsState handles events in ApprovingTools state
func (a *EventDrivenAgent) handleApprovingToolsState(event AgentEvent) {
	switch e := event.(type) {
	case MessageReceivedEvent:
		logger.Info("🔐 ApprovingTools state: initializing tool processing queue")

		a.toolsNeedingApproval = make([]sdk.ChatCompletionMessageToolCall, 0, len(a.currentToolCalls))
		for _, tc := range a.currentToolCalls {
			a.toolsNeedingApproval = append(a.toolsNeedingApproval, *tc)
		}
		a.currentToolIndex = 0
		a.toolResults = []domain.ConversationEntry{}

		logger.Info("📋 Starting sequential tool approval", "total_tools", len(a.toolsNeedingApproval))

		a.wg.Add(1)
		go a.processNextTool()

	case AllToolsProcessedEvent:
		logger.Info("✅ All tools processed", "results", len(a.toolResults))

		logger.Info("⏭️  STATE TRANSITION: ApprovingTools → PostToolExecution")
		if err := a.stateMachine.Transition(a.agentCtx, domain.StatePostToolExecution); err != nil {
			logger.Error("❌ Failed to transition to PostToolExecution", "error", err)
			return
		}

		a.events <- MessageReceivedEvent{}

	case ApprovalFailedEvent:
		logger.Error("❌ Approval failed", "error", e.Error)
		a.handleApprovalFailure(e.Error)
	}
}

// getNextToolForProcessing returns the next tool that needs processing
// Returns nil if all tools have been processed
func (a *EventDrivenAgent) getNextToolForProcessing() *sdk.ChatCompletionMessageToolCall {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.currentToolIndex >= len(a.toolsNeedingApproval) {
		return nil
	}

	tc := &a.toolsNeedingApproval[a.currentToolIndex]
	a.currentToolIndex++
	return tc
}

// handleToolRejection handles when a user rejects a tool call
func (a *EventDrivenAgent) handleToolRejection(tc sdk.ChatCompletionMessageToolCall) {
	logger.Info("❌ Tool rejected by user", "tool", tc.Function.Name)

	rejectionMessage := sdk.Message{
		Role:       sdk.Tool,
		Content:    sdk.NewMessageContent(fmt.Sprintf("Tool execution rejected by user: %s", tc.Function.Name)),
		ToolCallId: &tc.Id,
	}

	*a.agentCtx.Conversation = append(*a.agentCtx.Conversation, rejectionMessage)

	rejectionEntry := domain.ConversationEntry{
		Message: rejectionMessage,
		Time:    time.Now(),
	}

	if err := a.service.conversationRepo.AddMessage(rejectionEntry); err != nil {
		logger.Error("failed to store tool rejection message", "error", err)
	}

	a.eventPublisher.chatEvents <- domain.ToolRejectedEvent{
		RequestID: a.eventPublisher.requestID,
		Timestamp: time.Now(),
		ToolCall:  tc,
	}

	a.mu.Lock()
	a.agentCtx.HasToolResults = true
	a.mu.Unlock()
}

// shouldAutoApproveRemaining checks if auto-accept mode is enabled
func (a *EventDrivenAgent) shouldAutoApproveRemaining() bool {
	return a.service.stateManager.GetAgentMode() == domain.AgentModeAutoAccept
}

// executeAllRemainingTools executes the current tool and all remaining tools in auto-accept mode
func (a *EventDrivenAgent) executeAllRemainingTools(tc sdk.ChatCompletionMessageToolCall) {
	logger.Info("✅ Auto-accept mode enabled, auto-approving all remaining tools")

	a.mu.Lock()
	remainingTools := a.toolsNeedingApproval[a.currentToolIndex:]
	a.mu.Unlock()

	for _, remainingTool := range remainingTools {
		logger.Info("✅ Auto-approving tool", "tool", remainingTool.Function.Name)

		a.eventPublisher.chatEvents <- domain.ToolApprovedEvent{
			RequestID: a.eventPublisher.requestID,
			Timestamp: time.Now(),
			ToolCall:  remainingTool,
		}

		result := a.service.executeToolInternal(
			a.agentCtx.Ctx,
			remainingTool,
			a.eventPublisher,
			true,
			time.Now(),
		)

		a.appendToolResult(result)
	}

	result := a.service.executeToolInternal(
		a.agentCtx.Ctx,
		tc,
		a.eventPublisher,
		true,
		time.Now(),
	)

	a.appendToolResult(result)

	a.mu.Lock()
	a.currentToolIndex = len(a.toolsNeedingApproval)
	a.mu.Unlock()
}

// executeSingleApprovedTool executes a single approved tool
func (a *EventDrivenAgent) executeSingleApprovedTool(tc sdk.ChatCompletionMessageToolCall) {
	logger.Info("🚀 Executing approved tool", "tool", tc.Function.Name)

	result := a.service.executeToolInternal(
		a.agentCtx.Ctx,
		tc,
		a.eventPublisher,
		true,
		time.Now(),
	)

	a.appendToolResult(result)
}

// appendToolResult appends a tool execution result to the conversation and storage
func (a *EventDrivenAgent) appendToolResult(result domain.ConversationEntry) {
	a.mu.Lock()
	a.toolResults = append(a.toolResults, result)
	a.mu.Unlock()

	*a.agentCtx.Conversation = append(*a.agentCtx.Conversation, result.Message)

	if err := a.service.conversationRepo.AddMessage(result); err != nil {
		logger.Error("failed to store tool result", "error", err)
	}

	a.mu.Lock()
	a.agentCtx.HasToolResults = true
	a.mu.Unlock()
}

// processNextTool handles approval and execution of ONE tool sequentially
func (a *EventDrivenAgent) processNextTool() {
	defer a.wg.Done()

	tc := a.getNextToolForProcessing()
	if tc == nil {
		logger.Info("✅ All tools processed", "approved", len(a.toolResults))
		a.events <- AllToolsProcessedEvent{}
		return
	}

	logger.Info("🔐 Requesting approval for tool", "tool", tc.Function.Name)

	approved, err := a.service.requestToolApproval(a.agentCtx.Ctx, *tc, a.eventPublisher)
	if err != nil {
		logger.Error("❌ Approval request failed", "tool", tc.Function.Name, "error", err)
		a.events <- ApprovalFailedEvent{Error: err}
		return
	}

	if !approved {
		a.handleToolRejection(*tc)
		a.wg.Add(1)
		go a.processNextTool()
		return
	}

	logger.Info("✅ Tool approved", "tool", tc.Function.Name)
	a.eventPublisher.chatEvents <- domain.ToolApprovedEvent{
		RequestID: a.eventPublisher.requestID,
		Timestamp: time.Now(),
		ToolCall:  *tc,
	}

	if a.shouldAutoApproveRemaining() {
		a.executeAllRemainingTools(*tc)
		a.events <- AllToolsProcessedEvent{}
		return
	}

	a.executeSingleApprovedTool(*tc)

	a.wg.Add(1)
	go a.processNextTool()
}

// handleToolRejection handles when a user rejects a tool
// handleApprovalFailure handles when approval fails (timeout, error, etc.)
func (a *EventDrivenAgent) handleApprovalFailure(err error) {
	logger.Error("🛑 Handling approval failure", "error", err)

	a.eventPublisher.chatEvents <- domain.ChatErrorEvent{
		RequestID: a.req.RequestID,
		Timestamp: time.Now(),
		Error:     fmt.Errorf("approval failed: %w", err),
	}

	logger.Info("⏭️  STATE TRANSITION: ApprovingTools → Error")
	_ = a.stateMachine.Transition(a.agentCtx, domain.StateError)
}

// executeTools executes all tools (runs in background)
func (a *EventDrivenAgent) executeTools() {
	defer a.wg.Done()

	logger.Info("⚙️  Executing tools", "tool_count", len(a.currentToolCalls))

	toolCallsSlice := make([]*sdk.ChatCompletionMessageToolCall, 0, len(a.currentToolCalls))
	for _, tc := range a.currentToolCalls {
		toolCallsSlice = append(toolCallsSlice, tc)
		logger.Debug("🔧 Executing tool", "tool", tc.Function.Name, "id", tc.Id)
	}

	logger.Info("🔄 Running tools in parallel...")
	toolResults := a.service.executeToolCallsParallel(a.agentCtx.Ctx, toolCallsSlice, a.eventPublisher, a.req.IsChatMode)
	logger.Info("✅ Tool execution completed", "result_count", len(toolResults))

	if a.service.handleToolResults(toolResults, a.agentCtx.Conversation, a.eventPublisher, a.req) {
		logger.Warn("🛑 Tool results indicated stop (user rejection or error)")
		_ = a.stateMachine.Transition(a.agentCtx, domain.StateStopped)
		return
	}

	a.mu.Lock()
	a.agentCtx.HasToolResults = true
	a.mu.Unlock()

	logger.Debug("📤 Emitting ToolsCompletedEvent")
	a.events <- ToolsCompletedEvent{Results: toolResults}
}

// handleExecutingToolsState handles events in ExecutingTools state
func (a *EventDrivenAgent) handleExecutingToolsState(event AgentEvent) {
	switch event.(type) {
	case ToolsCompletedEvent:
		logger.Info("🎉 Tools execution completed")
		logger.Info("⏭️  STATE TRANSITION: ExecutingTools → PostToolExecution")

		if err := a.stateMachine.Transition(a.agentCtx, domain.StatePostToolExecution); err != nil {
			logger.Error("❌ Failed to transition to PostToolExecution", "error", err)
			return
		}
		a.events <- MessageReceivedEvent{}
	}
}

// handlePostToolExecutionState handles events in PostToolExecution state
func (a *EventDrivenAgent) handlePostToolExecutionState(_ AgentEvent) {
	logger.Info("🔄 PostToolExecution state: evaluating next action",
		"turn", a.agentCtx.Turns,
		"max_turns", a.agentCtx.MaxTurns,
		"queue_empty", a.agentCtx.MessageQueue.IsEmpty())

	if !a.agentCtx.MessageQueue.IsEmpty() {
		logger.Info("📬 Messages queued during tool execution, draining queue")
		numBatched := a.service.batchDrainQueue(a.agentCtx.Conversation, a.eventPublisher)
		logger.Info("✅ Batched messages after tool execution", "count", numBatched)
		logger.Info("⏭️  STATE TRANSITION: PostToolExecution → CheckingQueue (queue not empty)")
		if err := a.stateMachine.Transition(a.agentCtx, domain.StateCheckingQueue); err != nil {
			logger.Error("❌ Failed to transition to CheckingQueue", "error", err)
			return
		}
		a.events <- MessageReceivedEvent{}
		return
	}

	if a.stateMachine.CanTransition(a.agentCtx, domain.StateCompleting) {
		logger.Info("🛑 Can complete after tool execution")
		logger.Info("⏭️  STATE TRANSITION: PostToolExecution → Completing")
		if err := a.stateMachine.Transition(a.agentCtx, domain.StateCompleting); err != nil {
			logger.Error("❌ Failed to transition to Completing", "error", err)
			return
		}
		a.events <- CompletionRequestedEvent{}
	} else {
		logger.Info("🔁 Continuing to next turn", "current_turn", a.agentCtx.Turns, "max", a.agentCtx.MaxTurns)
		logger.Info("⏭️  STATE TRANSITION: PostToolExecution → StreamingLLM (via CheckingQueue)")
		if err := a.stateMachine.Transition(a.agentCtx, domain.StateCheckingQueue); err != nil {
			logger.Error("❌ Failed to transition to CheckingQueue", "error", err)
			return
		}
		a.events <- MessageReceivedEvent{}
	}
}

// handleCompletingState handles events in Completing state
func (a *EventDrivenAgent) handleCompletingState(_ AgentEvent) {
	logger.Info("🏁 Completing state: finalizing agent execution",
		"total_turns", a.agentCtx.Turns,
		"queue_empty", a.agentCtx.MessageQueue.IsEmpty())

	logger.Debug("⏸️  Sleeping 100ms for final queue check...")
	time.Sleep(100 * time.Millisecond)

	if !a.agentCtx.MessageQueue.IsEmpty() {
		logger.Info("📬 Messages queued after completion, restarting agent")
		logger.Info("⏭️  STATE TRANSITION: Completing → CheckingQueue (messages queued)")
		if err := a.stateMachine.Transition(a.agentCtx, domain.StateCheckingQueue); err != nil {
			logger.Error("❌ Failed to transition to CheckingQueue", "error", err)
			return
		}
		a.events <- MessageReceivedEvent{}
		return
	}

	logger.Info("✅ No queued messages, completing agent execution")

	logger.Debug("📤 Publishing final chat completion event")
	a.eventPublisher.publishChatComplete("", []sdk.ChatCompletionMessageToolCall{}, a.service.GetMetrics(a.req.RequestID))

	logger.Info("⏭️  STATE TRANSITION: Completing → Idle (agent done)")
	_ = a.stateMachine.Transition(a.agentCtx, domain.StateIdle)
	logger.Info("🎉 Agent execution completed successfully", "total_turns", a.agentCtx.Turns)
}
