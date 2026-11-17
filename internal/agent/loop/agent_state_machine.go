package loop

import (
	"fmt"
	"sync"
	"time"

	sdk "github.com/inference-gateway/sdk"

	states "github.com/inference-gateway/cli/internal/agent/loop/states"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

// stateMachine implements AgentStateMachine, guarding every transition with a
// read-write mutex. The flow is Idle → CheckingQueue → StreamingLLM →
// PostStream → EvaluatingTools → ApprovingTools, BlockingTools or ExecutingTools
// (⇄ InputRequired) → PostToolExecution → CheckingQueue → Completing → Idle,
// with Error, Cancelled and Stopped as exits.
type stateMachine struct {
	currentState  states.AgentExecutionState
	previousState states.AgentExecutionState
	mu            sync.RWMutex

	// State transition map: maps each state to its possible transitions with guards and actions
	transitions map[states.AgentExecutionState][]stateTransition
}

// stateTransition represents a state transition with guard and action
type stateTransition struct {
	toState states.AgentExecutionState
	guard   states.StateGuard
	action  states.StateAction
}

// newAgentStateMachine creates a new agent state machine
func newAgentStateMachine() states.AgentStateMachine {
	sm := &stateMachine{
		currentState: states.StateIdle,
		transitions:  make(map[states.AgentExecutionState][]stateTransition),
	}

	sm.registerTransitions()
	return sm
}

// registerTransitions registers every valid state transition. A transition's
// guard must return true for it to fire and its action runs when it does. Nil
// guards and actions are allowed, and a transition without a guard always fires.
func (sm *stateMachine) registerTransitions() {
	sm.addTransition(states.StateIdle, states.StateCheckingQueue, nil, nil)

	sm.addTransition(states.StateCheckingQueue, states.StateIdle,
		func(ctx *states.AgentContext) bool {
			return sm.canComplete(ctx) && ctx.MessageQueue.IsEmpty()
		},
		nil)

	sm.addTransition(states.StateCheckingQueue, states.StateCompleting,
		func(ctx *states.AgentContext) bool {
			return sm.canComplete(ctx)
		},
		nil)

	sm.addTransition(states.StateCheckingQueue, states.StateStreamingLLM,
		func(ctx *states.AgentContext) bool {
			return !ctx.MessageQueue.IsEmpty() || len(*ctx.Conversation) > 0
		},
		nil)

	sm.addTransition(states.StateStreamingLLM, states.StatePostStream, nil, nil)

	sm.addTransition(states.StatePostStream, states.StateCheckingQueue,
		func(ctx *states.AgentContext) bool {
			return !ctx.MessageQueue.IsEmpty()
		},
		nil)

	sm.addTransition(states.StatePostStream, states.StateEvaluatingTools,
		func(ctx *states.AgentContext) bool {
			return len(ctx.ToolCalls) > 0
		},
		nil)

	sm.addTransition(states.StatePostStream, states.StateStreamingLLM,
		func(ctx *states.AgentContext) bool {
			return len(ctx.ToolCalls) == 0 && !sm.canComplete(ctx) && ctx.MessageQueue.IsEmpty()
		},
		nil)

	sm.addTransition(states.StatePostStream, states.StateCompleting,
		func(ctx *states.AgentContext) bool {
			return len(ctx.ToolCalls) == 0 && sm.canComplete(ctx)
		},
		nil)

	sm.addTransition(states.StateEvaluatingTools, states.StateApprovingTools,
		func(ctx *states.AgentContext) bool {
			return sm.needsApproval(ctx)
		},
		nil)

	sm.addTransition(states.StateEvaluatingTools, states.StateBlockingTools,
		func(ctx *states.AgentContext) bool {
			return sm.needsApproval(ctx)
		},
		nil)

	sm.addTransition(states.StateEvaluatingTools, states.StateExecutingTools,
		func(ctx *states.AgentContext) bool {
			return !sm.needsApproval(ctx)
		},
		nil)

	sm.addTransition(states.StateApprovingTools, states.StateExecutingTools, nil, nil)

	sm.addTransition(states.StateApprovingTools, states.StatePostToolExecution, nil, nil)

	sm.addTransition(states.StateApprovingTools, states.StateCancelled, nil, nil)

	sm.addTransition(states.StateBlockingTools, states.StatePostToolExecution, nil, nil)

	sm.addTransition(states.StateExecutingTools, states.StatePostToolExecution, nil, nil)

	for _, state := range []states.AgentExecutionState{states.StateApprovingTools, states.StateExecutingTools} {
		sm.addTransition(state, states.StateInputRequired, nil, nil)
		sm.addTransition(states.StateInputRequired, state, nil, nil)
	}

	sm.addTransition(states.StatePostToolExecution, states.StateCheckingQueue, nil, nil)

	sm.addTransition(states.StatePostToolExecution, states.StateStopped, nil, nil)

	sm.addTransition(states.StatePostToolExecution, states.StateCompleting,
		func(ctx *states.AgentContext) bool {
			return sm.budgetReached(ctx) || sm.canComplete(ctx)
		},
		func(ctx *states.AgentContext) error {
			ctx.MaxTurnsExceeded = sm.maxTurnsReached(ctx) && !sm.canComplete(ctx)
			ctx.MaxDurationExceeded = sm.maxDurationReached(ctx) && !sm.canComplete(ctx)
			return nil
		})

	sm.addTransition(states.StatePostToolExecution, states.StateStreamingLLM,
		func(ctx *states.AgentContext) bool {
			return !sm.budgetReached(ctx) && !sm.canComplete(ctx) && ctx.MessageQueue.IsEmpty()
		},
		nil)

	sm.addTransition(states.StateCompleting, states.StateIdle, nil, nil)

	for state := states.StateIdle; state <= states.StateError; state++ {
		if state != states.StateCancelled {
			sm.addTransition(state, states.StateCancelled, nil, nil)
		}
	}

	for state := states.StateIdle; state <= states.StateError; state++ {
		if state != states.StateError {
			sm.addTransition(state, states.StateError, nil, nil)
		}
	}
}

// addTransition adds a state transition to the map
func (sm *stateMachine) addTransition(from, to states.AgentExecutionState, guard states.StateGuard, action states.StateAction) {
	transition := stateTransition{
		toState: to,
		guard:   guard,
		action:  action,
	}

	sm.transitions[from] = append(sm.transitions[from], transition)
}

// Transition attempts to transition to the target state
func (sm *stateMachine) Transition(ctx *states.AgentContext, targetState states.AgentExecutionState) error {
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
		trackWorkingTime(ctx.ConversationRepo, sm.previousState, sm.currentState)
	}

	logger.Info("state transition",
		"from", sm.previousState.String(),
		"to", sm.currentState.String(),
		"session_id", sessionID,
		"request_id", ctx.RequestID)

	return nil
}

// trackWorkingTime runs the session stopwatch while the loop is in a working
// state and stops it when the loop rests, ends or waits on the user.
func trackWorkingTime(repo convdomain.ConversationRepository, from, to states.AgentExecutionState) {
	if from.IsWorking() == to.IsWorking() {
		return
	}
	if to.IsWorking() {
		repo.StartWorking()
		return
	}
	repo.StopWorking()
}

// findTransition finds a matching transition from current state to target state
func (sm *stateMachine) findTransition(from, to states.AgentExecutionState) *stateTransition {
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
func (sm *stateMachine) GetCurrentState() states.AgentExecutionState {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.currentState
}

// Guard functions
//
// Guard functions determine whether a state transition should be allowed.
// They return true if the transition can proceed, false otherwise.

// canComplete reports whether the agent has no more work: at least one turn
// ran, no tool results are pending, the queue is empty and the last message is
// not from the user.
func (sm *stateMachine) canComplete(ctx *states.AgentContext) bool {

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

// needsApproval reports whether an approval policy is configured and gates at
// least one of the pending tool calls.
func (sm *stateMachine) needsApproval(ctx *states.AgentContext) bool {
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
func (sm *stateMachine) maxTurnsReached(ctx *states.AgentContext) bool {
	return ctx.Turns >= ctx.MaxTurns
}

// maxDurationReached reports whether the run has spent its time budget. A zero
// MaxDuration means no budget.
func (sm *stateMachine) maxDurationReached(ctx *states.AgentContext) bool {
	return ctx.MaxDuration > 0 && time.Since(ctx.StartedAt) >= ctx.MaxDuration
}

// budgetReached reports whether either limit, turns or wall clock, ends the run
// after the current tool call.
func (sm *stateMachine) budgetReached(ctx *states.AgentContext) bool {
	return sm.maxTurnsReached(ctx) || sm.maxDurationReached(ctx)
}

// CanTransition checks if a transition from current state to target state is valid
// This is useful for checking before attempting a transition
func (sm *stateMachine) CanTransition(ctx *states.AgentContext, targetState states.AgentExecutionState) bool {
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

// Reset resets the state machine to idle
func (sm *stateMachine) Reset() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.previousState = sm.currentState
	sm.currentState = states.StateIdle
}
