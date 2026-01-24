package services

import (
	"fmt"
	"sync"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
)

// AgentStateMachineImpl implements the AgentStateMachine interface
type AgentStateMachineImpl struct {
	currentState  domain.AgentExecutionState
	previousState domain.AgentExecutionState
	stateManager  domain.StateManager
	mu            sync.RWMutex

	// State transition map
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

// registerTransitions registers all valid state transitions with guards and actions
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

	if sm.stateManager != nil {
		sm.stateManager.BroadcastEvent(domain.StateTransitionEvent{
			BaseChatEvent: domain.BaseChatEvent{
				RequestID: "",
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

// canComplete checks if the agent can complete (no more work to do)
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

// needsApproval checks if any tool calls need user approval
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

// maxTurnsReached checks if max turns have been reached
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
