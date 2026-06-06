package agent

import (
	"context"
	"sync"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	states "github.com/inference-gateway/cli/internal/agent/states"
	constants "github.com/inference-gateway/cli/internal/constants"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// EventDrivenAgent manages agent execution using event-driven state machine
type EventDrivenAgent struct {
	// Core dependencies
	service        *AgentServiceImpl
	cfg            *config.AgentConfig
	stateMachine   domain.AgentStateMachine
	agentCtx       *domain.AgentContext
	eventPublisher *eventPublisher
	cancelChan     <-chan struct{}
	req            *domain.AgentRequest
	provider       string
	model          string
	registry       domain.BackgroundTaskRegistry

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
	cfg *config.AgentConfig,
	ctx context.Context,
	req *domain.AgentRequest,
	conversation *[]sdk.Message,
	eventPublisher *eventPublisher,
	cancelChan <-chan struct{},
	provider string,
	model string,
	registry domain.BackgroundTaskRegistry,
) *EventDrivenAgent {
	stateMachine := NewAgentStateMachine(service.stateManager)

	agentCtx := &domain.AgentContext{
		RequestID:        req.RequestID,
		Conversation:     conversation,
		MessageQueue:     service.messageQueue,
		ConversationRepo: service.conversationRepo,
		ToolCalls:        nil,
		Turns:            0,
		MaxTurns:         cfg.MaxTurns,
		HasToolResults:   false,
		ApprovalPolicy:   service.approvalPolicy,
		Ctx:              ctx,
		IsChatMode:       req.IsChatMode,
	}

	agent := &EventDrivenAgent{
		service:        service,
		cfg:            cfg,
		stateMachine:   stateMachine,
		agentCtx:       agentCtx,
		eventPublisher: eventPublisher,
		cancelChan:     cancelChan,
		req:            req,
		provider:       provider,
		model:          model,
		registry:       registry,
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
		StateMachine:           a.stateMachine,
		AgentCtx:               a.agentCtx,
		Events:                 a.events,
		WaitGroup:              &a.wg,
		CancelChan:             a.cancelChan,
		Mutex:                  &a.mu,
		CurrentMessage:         &a.currentMessage,
		CurrentToolCalls:       &a.currentToolCalls,
		CurrentReasoning:       &a.currentReasoning,
		AvailableTools:         &a.availableTools,
		ToolsNeedingApproval:   &a.toolsNeedingApproval,
		CurrentToolIndex:       &a.currentToolIndex,
		ToolResults:            &a.toolResults,
		Request:                a.req,
		BackgroundTaskRegistry: a.registry,
		Provider:               a.provider,
		Model:                  a.model,
		MaxConcurrentTools:     a.cfg.MaxConcurrentTools,
		ToolExecutor:           &a.toolExecutor,
		StartStreaming:         a.startStreaming,

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
		PublishChatCancelled: func(metrics *domain.ChatMetrics) {
			a.eventPublisher.publishChatCancelled(metrics)
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

// Events returns a send-only handle on the agent's internal event channel
// so external producers (e.g. the A2A task poller) can wake the loop when
// background work completes while the agent is idling.
func (a *EventDrivenAgent) Events() chan<- domain.AgentEvent {
	return a.events
}

// Start begins the event-driven agent execution
func (a *EventDrivenAgent) Start() {
	_ = a.stateMachine.Transition(a.agentCtx, domain.StateIdle)

	a.wg.Add(1)
	go a.processEvents()

	a.events <- domain.MessageReceivedEvent{}
}

// Wait waits for the agent to complete
func (a *EventDrivenAgent) Wait() {
	a.wg.Wait()
	close(a.events)
}

// processEvents is the main event processing loop. The double-select pattern
// (non-blocking probe before the real select) gives cancellation strict
// priority over pending events. Without the probe, Go's select chooses
// randomly when both channels are ready, so a flurry of in-flight events
// could mask the cancel signal and force the user to press Esc again.
func (a *EventDrivenAgent) processEvents() {
	defer a.wg.Done()

	cancelAndExit := func() {
		_ = a.stateMachine.Transition(a.agentCtx, domain.StateCancelled)
		a.eventPublisher.publishChatCancelled(a.service.GetMetrics(a.req.RequestID))
	}

	for {
		select {
		case <-a.cancelChan:
			cancelAndExit()
			return
		default:
		}

		select {
		case <-a.cancelChan:
			cancelAndExit()
			return

		case event, ok := <-a.events:
			if !ok {
				return
			}

			a.handleEvent(event)

			currentState := a.stateMachine.GetCurrentState()
			if currentState == domain.StateStopped ||
				currentState == domain.StateCancelled ||
				currentState == domain.StateError {
				return
			}

			if currentState == domain.StateIdle {
				hasPendingTasks := a.registry != nil && a.registry.HasPending()
				if hasPendingTasks {
					logger.Debug("agent in Idle state but has pending background tasks, staying alive")
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
}
