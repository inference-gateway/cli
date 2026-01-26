package agent

import (
	"context"
	"sync"
	"time"

	sdk "github.com/inference-gateway/sdk"

	states "github.com/inference-gateway/cli/internal/agent/states"
	constants "github.com/inference-gateway/cli/internal/constants"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

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
	events chan domain.AgentEvent

	// State handlers (registered on init)
	stateHandlers map[domain.AgentExecutionState]domain.StateHandler

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
		events:         make(chan domain.AgentEvent, constants.EventChannelBufferSize),
		stateHandlers:  make(map[domain.AgentExecutionState]domain.StateHandler),
	}

	agent.toolExecutor = agent.executeTools
	agent.registerStateHandlers()

	return agent
}

// registerStateHandlers creates and registers all state handlers for the event-driven agent.
// This method is called during agent initialization to set up the state handler registry.
func (a *EventDrivenAgent) registerStateHandlers() {
	ctx := &domain.StateContext{
		StateMachine:         a.stateMachine,
		AgentCtx:             a.agentCtx,
		Events:               a.events,
		WaitGroup:            &a.wg,
		CancelChan:           a.cancelChan,
		Mutex:                &a.mu,
		CurrentMessage:       &a.currentMessage,
		CurrentToolCalls:     &a.currentToolCalls,
		CurrentReasoning:     &a.currentReasoning,
		AvailableTools:       &a.availableTools,
		ToolsNeedingApproval: &a.toolsNeedingApproval,
		CurrentToolIndex:     &a.currentToolIndex,
		ToolResults:          &a.toolResults,
		Request:              a.req,
		TaskTracker:          a.taskTracker,
		Provider:             a.provider,
		Model:                a.model,
		ToolExecutor:         &a.toolExecutor,
		StartStreaming:       a.startStreaming,

		GetMetrics: a.service.GetMetrics,
		ShouldRequireApproval: func(toolCall *sdk.ChatCompletionMessageToolCall, isChatMode bool) bool {
			if a.service.approvalPolicy == nil {
				return false
			}
			return a.service.approvalPolicy.ShouldRequireApproval(a.agentCtx.Ctx, toolCall, isChatMode)
		},
		AddMessage: a.service.conversationRepo.AddMessage,
		BatchDrainQueue: func() int {
			return a.service.batchDrainQueue(a.agentCtx.Conversation, a.eventPublisher)
		},
		RequestToolApproval: func(toolCall sdk.ChatCompletionMessageToolCall) (bool, error) {
			return a.service.requestToolApproval(a.agentCtx.Ctx, toolCall, a.eventPublisher)
		},
		ExecuteToolInternal: func(toolCall sdk.ChatCompletionMessageToolCall, isApproved bool) domain.ConversationEntry {
			return a.service.executeToolInternal(a.agentCtx.Ctx, toolCall, a.eventPublisher, isApproved, time.Now())
		},
		GetAgentMode: func() domain.AgentMode {
			if a.service.stateManager == nil {
				return domain.AgentModeStandard
			}
			return a.service.stateManager.GetAgentMode()
		},
		PublishChatEvent: func(event domain.ChatEvent) {
			a.eventPublisher.chatEvents <- event
		},
		PublishChatComplete: func(reasoning string, toolCalls []sdk.ChatCompletionMessageToolCall, metrics *domain.ChatMetrics) {
			a.eventPublisher.publishChatComplete(reasoning, toolCalls, metrics)
		},
	}

	a.registerHandler(states.NewIdleState(ctx))
	a.registerHandler(states.NewCheckingQueueState(ctx))
	a.registerHandler(states.NewStreamingLLMState(ctx))
	a.registerHandler(states.NewPostStreamState(ctx))
	a.registerHandler(states.NewEvaluatingToolsState(ctx))
	a.registerHandler(states.NewApprovingToolsState(ctx))
	a.registerHandler(states.NewExecutingToolsState(ctx))
	a.registerHandler(states.NewPostToolExecutionState(ctx))
	a.registerHandler(states.NewCompletingState(ctx))
	a.registerHandler(states.NewErrorState(ctx))
	a.registerHandler(states.NewCancelledState(ctx))
	a.registerHandler(states.NewStoppedState(ctx))
}

// registerHandler registers a single state handler
func (a *EventDrivenAgent) registerHandler(handler domain.StateHandler) {
	a.stateHandlers[handler.Name()] = handler
}

// Start begins the event-driven agent execution
func (a *EventDrivenAgent) Start() {
	logger.Debug("starting event-driven agent",
		"request_id", a.req.RequestID,
		"max_turns", a.agentCtx.MaxTurns)

	_ = a.stateMachine.Transition(a.agentCtx, domain.StateIdle)

	a.wg.Add(1)
	go a.processEvents()

	logger.Debug("triggering initial message received event")
	a.events <- domain.MessageReceivedEvent{}
}

// Wait waits for the agent to complete
func (a *EventDrivenAgent) Wait() {
	a.wg.Wait()
	close(a.events)
}

// GetEventChannel returns the agent's internal event channel for external components
// to send wake-up events (e.g., when A2A tasks complete)
func (a *EventDrivenAgent) GetEventChannel() chan<- domain.AgentEvent {
	return a.events
}

// processEvents is the main event processing loop
func (a *EventDrivenAgent) processEvents() {
	defer a.wg.Done()
	defer logger.Debug("agent event processing stopped", "request_id", a.req.RequestID)

	for {
		select {
		case <-a.cancelChan:
			logger.Debug("agent cancelled", "request_id", a.req.RequestID)
			_ = a.stateMachine.Transition(a.agentCtx, domain.StateCancelled)
			a.eventPublisher.publishChatComplete("", []sdk.ChatCompletionMessageToolCall{}, a.service.GetMetrics(a.req.RequestID))
			return

		case event, ok := <-a.events:
			if !ok {
				logger.Debug("event channel closed")
				return
			}

			logger.Debug("received event",
				"event_type", event.EventType(),
				"current_state", a.stateMachine.GetCurrentState(),
				"turn", a.agentCtx.Turns)

			a.handleEvent(event)

			currentState := a.stateMachine.GetCurrentState()
			// Check if we should exit the event loop
			if currentState == domain.StateStopped ||
				currentState == domain.StateCancelled ||
				currentState == domain.StateError {
				logger.Debug("agent reached terminal state",
					"state", currentState,
					"total_turns", a.agentCtx.Turns)
				return
			}

			// For Idle state, only exit if there are no pending A2A tasks
			if currentState == domain.StateIdle {
				hasPendingTasks := a.taskTracker != nil && len(a.taskTracker.GetAllPollingTasks()) > 0
				if hasPendingTasks {
					logger.Debug("agent in Idle state but has pending A2A tasks, staying alive",
						"pending_tasks", len(a.taskTracker.GetAllPollingTasks()))
					// Don't return - stay in the event loop waiting for task completion
				} else {
					logger.Debug("agent reached Idle state with no pending tasks",
						"total_turns", a.agentCtx.Turns)
					return
				}
			}
		}
	}
}

// handleEvent processes a single event based on current state using the state handler registry
func (a *EventDrivenAgent) handleEvent(event domain.AgentEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()

	currentState := a.stateMachine.GetCurrentState()
	logger.Debug("handling event in state",
		"event", event.EventType(),
		"state", currentState,
		"turn", a.agentCtx.Turns,
		"queue_empty", a.agentCtx.MessageQueue.IsEmpty())

	handler, exists := a.stateHandlers[currentState]
	if !exists {
		logger.Error("no handler for state", "state", currentState.String())
		return
	}

	if err := handler.Handle(event); err != nil {
		logger.Error("state handler error",
			"state", currentState.String(),
			"error", err)
	}

	logger.Debug("event handled",
		"event", event.EventType(),
		"new_state", a.stateMachine.GetCurrentState())
}
