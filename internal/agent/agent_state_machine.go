package agent

import (
	"fmt"
	"sync"

	sdk "github.com/inference-gateway/sdk"

	states "github.com/inference-gateway/cli/internal/agent/states"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

// AgentStateMachineImpl implements the AgentStateMachine interface.
//
// The state machine manages the agent's execution flow through the following states:
//
// State Flow:
//
//	Idle → CheckingQueue → StreamingLLM → PostStream → EvaluatingTools → ApprovingTools/BlockingTools/ExecutingTools → PostToolExecution → CheckingQueue (loop) → Completing → Idle
//
// State Descriptions:
//   - Idle: Agent is not executing, waiting for work
//   - CheckingQueue: Checking if there are queued messages or if completion criteria are met
//   - StreamingLLM: Streaming responses from the LLM
//   - PostStream: Processing LLM response, checking for tool calls or completion
//   - EvaluatingTools: Determining if tool calls need approval
//   - ApprovingTools: Waiting for user approval of tool calls (only in chat mode)
//   - BlockingTools: Approval required but undeliverable (approval_behaviour=block); gated tools are rejected with a reason
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
	currentState  states.AgentExecutionState
	previousState states.AgentExecutionState
	mu            sync.RWMutex

	// State transition map: maps each state to its possible transitions with guards and actions
	transitions map[states.AgentExecutionState][]StateTransition
}

// StateTransition represents a state transition with guard and action
type StateTransition struct {
	fromState states.AgentExecutionState
	toState   states.AgentExecutionState
	guard     states.StateGuard
	action    states.StateAction
}

// NewAgentStateMachine creates a new agent state machine
func NewAgentStateMachine() states.AgentStateMachine {
	sm := &AgentStateMachineImpl{
		currentState: states.StateIdle,
		transitions:  make(map[states.AgentExecutionState][]StateTransition),
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

	sm.addTransition(states.StateExecutingTools, states.StateStopped, nil, nil)

	sm.addTransition(states.StatePostToolExecution, states.StateCheckingQueue, nil, nil)

	sm.addTransition(states.StatePostToolExecution, states.StateCompleting,
		func(ctx *states.AgentContext) bool {
			return sm.maxTurnsReached(ctx) || sm.canComplete(ctx)
		},
		func(ctx *states.AgentContext) error {
			ctx.MaxTurnsExceeded = sm.maxTurnsReached(ctx) && !sm.canComplete(ctx)
			return nil
		})

	sm.addTransition(states.StatePostToolExecution, states.StateStreamingLLM,
		func(ctx *states.AgentContext) bool {
			return !sm.maxTurnsReached(ctx) && !sm.canComplete(ctx) && ctx.MessageQueue.IsEmpty()
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
func (sm *AgentStateMachineImpl) addTransition(from, to states.AgentExecutionState, guard states.StateGuard, action states.StateAction) {
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
func (sm *AgentStateMachineImpl) Transition(ctx *states.AgentContext, targetState states.AgentExecutionState) error {
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

	logger.Info("state transition",
		"from", sm.previousState.String(),
		"to", sm.currentState.String(),
		"session_id", sessionID,
		"request_id", ctx.RequestID)

	return nil
}

// findTransition finds a matching transition from current state to target state
func (sm *AgentStateMachineImpl) findTransition(from, to states.AgentExecutionState) *StateTransition {
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
func (sm *AgentStateMachineImpl) GetCurrentState() states.AgentExecutionState {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.currentState
}

// GetPreviousState returns the previous state (thread-safe)
func (sm *AgentStateMachineImpl) GetPreviousState() states.AgentExecutionState {
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
func (sm *AgentStateMachineImpl) canComplete(ctx *states.AgentContext) bool {

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
func (sm *AgentStateMachineImpl) needsApproval(ctx *states.AgentContext) bool {
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
func (sm *AgentStateMachineImpl) maxTurnsReached(ctx *states.AgentContext) bool {
	return ctx.Turns >= ctx.MaxTurns
}

// CanTransition checks if a transition from current state to target state is valid
// This is useful for checking before attempting a transition
func (sm *AgentStateMachineImpl) CanTransition(ctx *states.AgentContext, targetState states.AgentExecutionState) bool {
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
func (sm *AgentStateMachineImpl) GetValidTransitions(ctx *states.AgentContext) []states.AgentExecutionState {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	transitions, exists := sm.transitions[sm.currentState]
	if !exists {
		return []states.AgentExecutionState{}
	}

	validStates := []states.AgentExecutionState{}
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
	sm.currentState = states.StateIdle
}
