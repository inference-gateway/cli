package agent

import (
	"fmt"
	"sync"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	sdk "github.com/inference-gateway/sdk"
)

// AgentStateMachineImpl implements the AgentStateMachine interface.
//
// The state machine manages the agent's execution flow through the following states:
//
// State Flow:
//
//	Idle → CheckingQueue → StreamingLLM → PostStream → EvaluatingTools → ApprovingTools/ExecutingTools → PostToolExecution → CheckingQueue (loop) → Completing → Idle
//
// State Descriptions:
//   - Idle: Agent is not executing, waiting for work
//   - CheckingQueue: Checking if there are queued messages or if completion criteria are met
//   - StreamingLLM: Streaming responses from the LLM
//   - PostStream: Processing LLM response, checking for tool calls or completion
//   - EvaluatingTools: Determining if tool calls need approval
//   - ApprovingTools: Waiting for user approval of tool calls (only in chat mode)
//   - ExecutingTools: Executing approved or auto-approved tool calls
//   - PostToolExecution: Processing tool results, checking for completion or continuing
//   - Completing: Finalizing the agent execution
//   - Error: An error occurred during execution
//   - Cancelled: User cancelled the execution
//   - Stopped: Tool execution indicated stop (user rejection or error)
//
// Thread Safety:
//
//	All state transitions are protected by a read-write mutex to ensure thread-safe access.
type AgentStateMachineImpl struct {
	currentState  domain.AgentExecutionState
	previousState domain.AgentExecutionState
	stateManager  domain.StateManager
	mu            sync.RWMutex

	// State transition map: maps each state to its possible transitions with guards and actions
	transitions map[domain.AgentExecutionState][]StateTransition
}

// StateTransition represents a state transition with guard and action
type StateTransition struct {
	fromState domain.AgentExecutionState
	toState   domain.AgentExecutionState
	guard     domain.StateGuard
	action    domain.StateAction
}

// NewAgentStateMachine creates a new agent state machine
func NewAgentStateMachine(stateManager domain.StateManager) domain.AgentStateMachine {
	sm := &AgentStateMachineImpl{
		currentState: domain.StateIdle,
		stateManager: stateManager,
		transitions:  make(map[domain.AgentExecutionState][]StateTransition),
	}

	sm.registerTransitions()
	return sm
}

// registerTransitions registers all valid state transitions with guards and actions.
//
// Each transition can have:
//   - guard: A function that must return true for the transition to be allowed
//   - action: A function executed when the transition occurs
//
// Transitions without guards are always allowed. Nil guards/actions are permitted.
func (sm *AgentStateMachineImpl) registerTransitions() {
	sm.addTransition(domain.StateIdle, domain.StateCheckingQueue, nil, nil)

	sm.addTransition(domain.StateCheckingQueue, domain.StateIdle,
		func(ctx *domain.AgentContext) bool {
			return sm.canComplete(ctx) && ctx.MessageQueue.IsEmpty()
		},
		nil)

	sm.addTransition(domain.StateCheckingQueue, domain.StateCompleting,
		func(ctx *domain.AgentContext) bool {
			return sm.canComplete(ctx)
		},
		nil)

	sm.addTransition(domain.StateCheckingQueue, domain.StateStreamingLLM,
		func(ctx *domain.AgentContext) bool {
			return !ctx.MessageQueue.IsEmpty() || len(*ctx.Conversation) > 0
		},
		nil)

	sm.addTransition(domain.StateStreamingLLM, domain.StatePostStream, nil, nil)

	sm.addTransition(domain.StatePostStream, domain.StateCheckingQueue,
		func(ctx *domain.AgentContext) bool {
			return !ctx.MessageQueue.IsEmpty()
		},
		nil)

	sm.addTransition(domain.StatePostStream, domain.StateEvaluatingTools,
		func(ctx *domain.AgentContext) bool {
			return len(ctx.ToolCalls) > 0
		},
		nil)

	sm.addTransition(domain.StatePostStream, domain.StateStreamingLLM,
		func(ctx *domain.AgentContext) bool {
			return len(ctx.ToolCalls) == 0 && !sm.canComplete(ctx) && ctx.MessageQueue.IsEmpty()
		},
		nil)

	sm.addTransition(domain.StatePostStream, domain.StateCompleting,
		func(ctx *domain.AgentContext) bool {
			return len(ctx.ToolCalls) == 0 && sm.canComplete(ctx)
		},
		nil)

	sm.addTransition(domain.StateEvaluatingTools, domain.StateApprovingTools,
		func(ctx *domain.AgentContext) bool {
			return sm.needsApproval(ctx)
		},
		nil)

	sm.addTransition(domain.StateEvaluatingTools, domain.StateExecutingTools,
		func(ctx *domain.AgentContext) bool {
			return !sm.needsApproval(ctx)
		},
		nil)

	sm.addTransition(domain.StateApprovingTools, domain.StateExecutingTools, nil, nil)

	sm.addTransition(domain.StateApprovingTools, domain.StatePostToolExecution, nil, nil)

	sm.addTransition(domain.StateApprovingTools, domain.StateCancelled, nil, nil)

	sm.addTransition(domain.StateExecutingTools, domain.StatePostToolExecution, nil, nil)

	sm.addTransition(domain.StatePostToolExecution, domain.StateCheckingQueue, nil, nil)

	sm.addTransition(domain.StatePostToolExecution, domain.StateCompleting,
		func(ctx *domain.AgentContext) bool {
			return sm.maxTurnsReached(ctx) || sm.canComplete(ctx)
		},
		nil)

	sm.addTransition(domain.StatePostToolExecution, domain.StateStreamingLLM,
		func(ctx *domain.AgentContext) bool {
			return !sm.maxTurnsReached(ctx) && !sm.canComplete(ctx) && ctx.MessageQueue.IsEmpty()
		},
		nil)

	sm.addTransition(domain.StateCompleting, domain.StateIdle, nil, nil)

	for state := domain.StateIdle; state <= domain.StateError; state++ {
		if state != domain.StateCancelled {
			sm.addTransition(state, domain.StateCancelled, nil, nil)
		}
	}

	for state := domain.StateIdle; state <= domain.StateError; state++ {
		if state != domain.StateError {
			sm.addTransition(state, domain.StateError, nil, nil)
		}
	}
}

// addTransition adds a state transition to the map
func (sm *AgentStateMachineImpl) addTransition(from, to domain.AgentExecutionState, guard domain.StateGuard, action domain.StateAction) {
	transition := StateTransition{
		fromState: from,
		toState:   to,
		guard:     guard,
		action:    action,
	}

	if sm.transitions[from] == nil {
		sm.transitions[from] = []StateTransition{}
	}

	sm.transitions[from] = append(sm.transitions[from], transition)
}

// Transition attempts to transition to the target state
func (sm *AgentStateMachineImpl) Transition(ctx *domain.AgentContext, targetState domain.AgentExecutionState) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	transition := sm.findTransition(sm.currentState, targetState)
	if transition == nil {
		return fmt.Errorf("invalid transition from %s to %s",
			sm.currentState, targetState)
	}

	if transition.guard != nil && !transition.guard(ctx) {
		return fmt.Errorf("guard failed for transition %s -> %s",
			sm.currentState, targetState)
	}

	if transition.action != nil {
		if err := transition.action(ctx); err != nil {
			return fmt.Errorf("action failed: %w", err)
		}
	}

	sm.previousState = sm.currentState
	sm.currentState = targetState

	var sessionID string
	if ctx.ConversationRepo != nil {
		sessionID = ctx.ConversationRepo.GetCurrentConversationID()
	}

	logger.Debug("State transition",
		"from", sm.previousState.String(),
		"to", sm.currentState.String(),
		"session_id", sessionID,
		"request_id", ctx.RequestID)

	if sm.stateManager != nil {
		sm.stateManager.BroadcastEvent(domain.StateTransitionEvent{
			BaseChatEvent: domain.BaseChatEvent{
				RequestID: ctx.RequestID,
				Timestamp: time.Now(),
			},
			FromState: sm.previousState,
			ToState:   sm.currentState,
		})
	}

	return nil
}

// findTransition finds a matching transition from current state to target state
func (sm *AgentStateMachineImpl) findTransition(from, to domain.AgentExecutionState) *StateTransition {
	transitions, exists := sm.transitions[from]
	if !exists {
		return nil
	}

	for _, transition := range transitions {
		if transition.toState == to {
			return &transition
		}
	}

	return nil
}

// GetCurrentState returns the current state (thread-safe)
func (sm *AgentStateMachineImpl) GetCurrentState() domain.AgentExecutionState {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.currentState
}

// GetPreviousState returns the previous state (thread-safe)
func (sm *AgentStateMachineImpl) GetPreviousState() domain.AgentExecutionState {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.previousState
}

// Guard functions
//
// Guard functions determine whether a state transition should be allowed.
// They return true if the transition can proceed, false otherwise.

// canComplete checks if the agent can complete (no more work to do).
//
// Completion criteria:
//   - At least one turn has been completed
//   - No pending tool results to process
//   - Message queue is empty
//   - Last message is not from the user (agent has responded)
//
// Returns true if all completion criteria are met.
func (sm *AgentStateMachineImpl) canComplete(ctx *domain.AgentContext) bool {

	if ctx.Turns == 0 {
		return false
	}

	if ctx.HasToolResults {
		return false
	}

	if !ctx.MessageQueue.IsEmpty() {
		return false
	}

	if len(*ctx.Conversation) > 0 {
		lastMessage := (*ctx.Conversation)[len(*ctx.Conversation)-1]
		if lastMessage.Role == sdk.User {
			return false
		}
	}

	return true
}

// needsApproval checks if any tool calls need user approval.
//
// Tool approval is required if:
//   - An approval policy is configured
//   - At least one tool call requires approval according to the policy
//   - The agent is running in chat mode (approval not needed in background mode)
//
// Returns true if user approval is needed before executing tools.
func (sm *AgentStateMachineImpl) needsApproval(ctx *domain.AgentContext) bool {
	if ctx.ApprovalPolicy == nil {
		return false
	}

	for _, toolCall := range ctx.ToolCalls {
		if ctx.ApprovalPolicy.ShouldRequireApproval(ctx.Ctx, toolCall, ctx.IsChatMode) {
			return true
		}
	}

	return false
}

// maxTurnsReached checks if the maximum number of turns has been reached.
//
// This prevents infinite loops by limiting the number of LLM-tool iterations.
// Returns true if the current turn count has reached or exceeded the maximum.
func (sm *AgentStateMachineImpl) maxTurnsReached(ctx *domain.AgentContext) bool {
	return ctx.Turns >= ctx.MaxTurns
}

// CanTransition checks if a transition from current state to target state is valid
// This is useful for checking before attempting a transition
func (sm *AgentStateMachineImpl) CanTransition(ctx *domain.AgentContext, targetState domain.AgentExecutionState) bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	transition := sm.findTransition(sm.currentState, targetState)
	if transition == nil {
		return false
	}

	if transition.guard != nil && !transition.guard(ctx) {
		return false
	}

	return true
}

// GetValidTransitions returns all valid transitions from the current state
func (sm *AgentStateMachineImpl) GetValidTransitions(ctx *domain.AgentContext) []domain.AgentExecutionState {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	transitions, exists := sm.transitions[sm.currentState]
	if !exists {
		return []domain.AgentExecutionState{}
	}

	validStates := []domain.AgentExecutionState{}
	for _, transition := range transitions {
		if transition.guard == nil || transition.guard(ctx) {
			validStates = append(validStates, transition.toState)
		}
	}

	return validStates
}

// Reset resets the state machine to idle
func (sm *AgentStateMachineImpl) Reset() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.previousState = sm.currentState
	sm.currentState = domain.StateIdle
}
