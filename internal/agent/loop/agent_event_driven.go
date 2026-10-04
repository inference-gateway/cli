package loop

import (
	"context"
	"sync"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	states "github.com/inference-gateway/cli/internal/agent/loop/states"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	constants "github.com/inference-gateway/cli/internal/platform/constants"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

// eventDrivenAgent manages agent execution using event-driven state machine
type eventDrivenAgent struct {
	// Core dependencies
	service        *Agent
	cfg            *config.AgentConfig
	stateMachine   states.AgentStateMachine
	agentCtx       *states.AgentContext
	eventPublisher *eventPublisher
	cancelChan     <-chan struct{}
	req            *agentdomain.AgentRequest
	provider       string
	model          string

	// Event channel
	events chan states.AgentEvent

	// State handlers (registered on init)
	stateHandlers map[states.AgentExecutionState]states.StateHandler

	// State data
	currentMessage   sdk.Message
	currentToolCalls []*sdk.ChatCompletionMessageToolCall
	currentReasoning string
	finishReason     string
	availableTools   []sdk.ChatCompletionTool

	// Tool processing state (for sequential approval and execution)
	toolsNeedingApproval []sdk.ChatCompletionMessageToolCall
	currentToolIndex     int
	toolResults          []convdomain.ConversationEntry

	// Synchronization
	mu sync.Mutex
	wg sync.WaitGroup

	// Prompts waiting on the user and the state the first one suspended
	promptMu    sync.Mutex
	openPrompts int
	resumeState states.AgentExecutionState

	// Testability - can be overridden in tests
	toolExecutor func()
}

// newEventDrivenAgent creates a new event-driven agent
func newEventDrivenAgent(
	service *Agent,
	cfg *config.AgentConfig,
	ctx context.Context,
	req *agentdomain.AgentRequest,
	conversation *[]sdk.Message,
	eventPublisher *eventPublisher,
	cancelChan <-chan struct{},
	provider string,
	model string,
) *eventDrivenAgent {
	stateMachine := newAgentStateMachine()

	ctx = agentdomain.WithSandboxApprovalAvailable(ctx, req.IsChatMode || req.ApprovalBrokerAttached)
	ctx = agentdomain.WithUserQuestionsAvailable(ctx, req.IsChatMode || req.UserQuestionBrokerAttached)

	agentCtx := &states.AgentContext{
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

	agent := &eventDrivenAgent{
		service:        service,
		cfg:            cfg,
		stateMachine:   stateMachine,
		agentCtx:       agentCtx,
		eventPublisher: eventPublisher,
		cancelChan:     cancelChan,
		req:            req,
		provider:       provider,
		model:          model,
		events:         make(chan states.AgentEvent, constants.EventChannelBufferSize),
		stateHandlers:  make(map[states.AgentExecutionState]states.StateHandler),
	}

	agent.toolExecutor = agent.executeTools
	agent.registerStateHandlers()
	eventPublisher.inputGate = agent.awaitUser

	return agent
}

// registerStateHandlers creates and registers all state handlers for the event-driven agent.
// This method is called during agent initialization to set up the state handler registry.
func (a *eventDrivenAgent) registerStateHandlers() {
	ctx := &states.StateContext{
		StateMachine:         a.stateMachine,
		AgentCtx:             a.agentCtx,
		Events:               a.events,
		WaitGroup:            &a.wg,
		Mutex:                &a.mu,
		CurrentMessage:       &a.currentMessage,
		CurrentToolCalls:     &a.currentToolCalls,
		CurrentReasoning:     &a.currentReasoning,
		Tools:                a.service.toolService,
		ToolsNeedingApproval: &a.toolsNeedingApproval,
		CurrentToolIndex:     &a.currentToolIndex,
		ToolResults:          &a.toolResults,
		Request:              a.req,
		MaxConcurrentTools:   a.cfg.MaxConcurrentTools,
		ToolExecutor:         &a.toolExecutor,
		StartStreaming:       a.startStreaming,

		GetMetrics: a.service.GetMetrics,
		ShouldRequireApproval: func(toolCall *sdk.ChatCompletionMessageToolCall, isChatMode bool) bool {
			if a.service.approvalPolicy == nil {
				return false
			}
			return a.service.approvalPolicy.ShouldRequireApproval(a.agentCtx.Ctx, toolCall, isChatMode)
		},
		ApprovalDelivery: func(toolCall *sdk.ChatCompletionMessageToolCall) string {
			if a.service.config == nil {
				return config.ApprovalBehaviourPrompt
			}
			if a.service.stateManager != nil && a.service.stateManager.GetAgentMode() == agentdomain.AgentModeAutoWithJudge {
				return config.ApprovalBehaviourJudge
			}
			behaviour := a.service.config.ApprovalBehaviourFor(toolCall.Function.Name)
			return config.ResolveApprovalDelivery(behaviour, a.req.ApprovalBrokerAttached, a.req.IsChatMode)
		},
		AddMessage: a.service.conversationRepo.AddMessage,
		BatchDrainQueue: func() int {
			return a.service.batchDrainQueue(a.agentCtx.Conversation, a.eventPublisher)
		},
		RequestToolApproval: func(toolCall sdk.ChatCompletionMessageToolCall) (bool, string, error) {
			return a.service.requestToolApproval(a.agentCtx.Ctx, toolCall, a.eventPublisher)
		},
		ExecuteToolInternal: func(toolCall sdk.ChatCompletionMessageToolCall, isApproved bool) (entry convdomain.ConversationEntry) {
			defer a.recoverPanic()
			return a.service.executeToolInternal(a.agentCtx.Ctx, toolCall, a.eventPublisher, isApproved, time.Now())
		},
		GetAgentMode: func() agentdomain.AgentMode {
			if a.service.stateManager == nil {
				return agentdomain.AgentModeStandard
			}
			return a.service.stateManager.GetAgentMode()
		},
		PublishChatEvent: func(event agentdomain.ChatEvent) {
			a.eventPublisher.chatEvents <- event
		},
		PublishChatComplete: func(reasoning string, toolCalls []sdk.ChatCompletionMessageToolCall, metrics *agentdomain.ChatMetrics) {
			a.eventPublisher.publishChatComplete(reasoning, toolCalls, metrics)
		},
		PublishChatCancelled: func(metrics *agentdomain.ChatMetrics) {
			a.eventPublisher.publishChatCancelled(metrics)
		},
		PublishToolResults: func(results []convdomain.ConversationEntry) {
			a.eventPublisher.publishToolExecutionCompleted(results)
		},
		DispatchHooks: func(hook agentdomain.HookPoint) {
			a.service.dispatchHooks(a.agentCtx, hook)
		},
		WaitForBackgroundTasks: func() {
			a.service.waitForBackgroundTasks(a.agentCtx.Ctx)
		},
	}

	a.registerHandler(states.NewIdleState(ctx))
	a.registerHandler(states.NewCheckingQueueState(ctx))
	a.registerHandler(states.NewStreamingLLMState(ctx))
	a.registerHandler(states.NewPostStreamState(ctx))
	a.registerHandler(states.NewEvaluatingToolsState(ctx))
	a.registerHandler(states.NewApprovingToolsState(ctx))
	a.registerHandler(states.NewBlockingToolsState(ctx))
	a.registerHandler(states.NewExecutingToolsState(ctx))
	a.registerHandler(states.NewPostToolExecutionState(ctx))
	a.registerHandler(states.NewInputRequiredState(ctx))
	a.registerHandler(states.NewCompletingState(ctx))
	a.registerHandler(states.NewErrorState(ctx))
	a.registerHandler(states.NewCancelledState(ctx))
	a.registerHandler(states.NewStoppedState(ctx))
}

// awaitUser runs wait in the InputRequired state and resumes the state the
// loop left once the last open prompt is answered.
func (a *eventDrivenAgent) awaitUser(wait func()) {
	a.enterInputRequired()
	defer a.leaveInputRequired()
	wait()
}

func (a *eventDrivenAgent) enterInputRequired() {
	a.promptMu.Lock()
	defer a.promptMu.Unlock()

	a.openPrompts++
	if a.openPrompts > 1 {
		return
	}
	a.resumeState = a.stateMachine.GetCurrentState()
	if err := a.stateMachine.Transition(a.agentCtx, states.StateInputRequired); err != nil {
		logger.Debug("prompt opened outside a tool state", "error", err)
	}
}

// leaveInputRequired resumes the suspended state. A run that was cancelled or
// failed while the prompt was open stays where it is.
func (a *eventDrivenAgent) leaveInputRequired() {
	a.promptMu.Lock()
	defer a.promptMu.Unlock()

	a.openPrompts--
	if a.openPrompts > 0 || a.stateMachine.GetCurrentState() != states.StateInputRequired {
		return
	}
	if err := a.stateMachine.Transition(a.agentCtx, a.resumeState); err != nil {
		logger.Error("failed to resume after the user answered", "error", err)
	}
}

// registerHandler registers a single state handler
func (a *eventDrivenAgent) registerHandler(handler states.StateHandler) {
	a.stateHandlers[handler.Name()] = handler
}

// Start begins the event-driven agent execution. The state machine already
// begins in Idle, so seeding a MessageReceivedEvent drives the first transition
// (Idle -> CheckingQueue) via the Idle state handler.
func (a *eventDrivenAgent) Start() {
	a.wg.Add(1)
	go a.processEvents()

	a.events <- states.MessageReceivedEvent{}
}

// Wait waits for the agent to complete
func (a *eventDrivenAgent) Wait() {
	a.wg.Wait()
	close(a.events)
}

// processEvents is the main event processing loop. The double-select pattern
// (non-blocking probe before the real select) gives cancellation strict
// priority over pending events. Without the probe, Go's select chooses
// randomly when both channels are ready, so a flurry of in-flight events
// could mask the cancel signal and force the user to press Esc again.
func (a *eventDrivenAgent) processEvents() {
	defer a.wg.Done()
	defer a.recoverPanic()

	cancelAndExit := func() {
		if err := a.stateMachine.Transition(a.agentCtx, states.StateCancelled); err != nil {
			logger.Error("failed to transition to Cancelled state", "error", err)
		}
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
			if currentState == states.StateStopped ||
				currentState == states.StateCancelled ||
				currentState == states.StateError {
				return
			}

			if currentState == states.StateIdle {
				logger.Debug("agent reached Idle state - turn complete",
					"total_turns", a.service.sessionTurns.Load())
				return
			}
		}
	}
}

// handleEvent processes a single event based on current state using the state handler registry
func (a *eventDrivenAgent) handleEvent(event states.AgentEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()

	currentState := a.stateMachine.GetCurrentState()

	logger.Debug("dispatching event",
		"event", event.EventType(),
		"state", currentState.String(),
		"request_id", a.req.RequestID)

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
