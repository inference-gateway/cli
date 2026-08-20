// State-machine contracts consumed by the agent run loop and the per-state
// executors in this package.

package states

import (
	"context"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	domain "github.com/inference-gateway/cli/internal/domain"
)

// AgentContext represents the execution context for the agent state machine
type AgentContext struct {
	RequestID        string
	Conversation     *[]sdk.Message
	MessageQueue     domain.MessageQueue
	ConversationRepo domain.ConversationRepository
	ToolCalls        []*sdk.ChatCompletionMessageToolCall
	Turns            int
	MaxTurns         int
	HasToolResults   bool
	LastToolFailed   bool
	ApprovalPolicy   agentdomain.ApprovalPolicy
	Ctx              context.Context
	IsChatMode       bool
	// MaxTurnsExceeded is set by the state machine when the run is forced into
	// Completing because the turn limit was hit before the task could complete.
	MaxTurnsExceeded bool
}

// AnyToolFailed reports whether any entry in a completed tool batch failed
// (executed with a non-success result). It backs the post_tool `on_failure`
// reminder trigger; callers set AgentContext.LastToolFailed from it when a batch
// completes.
func AnyToolFailed(results []domain.ConversationEntry) bool {
	for _, entry := range results {
		if entry.ToolExecution != nil && !entry.ToolExecution.Success {
			return true
		}
	}
	return false
}

// AnyToolRejected reports whether any entry in a completed tool batch was
// rejected by the user. A rejection ends the agent turn instead of feeding the
// results back for another LLM response.
func AnyToolRejected(results []domain.ConversationEntry) bool {
	for _, entry := range results {
		if entry.ToolExecution != nil && entry.ToolExecution.Rejected {
			return true
		}
	}
	return false
}

// StateGuard is a function that determines if a state transition should occur
type StateGuard func(ctx *AgentContext) bool

// StateAction is a function executed on state transitions
type StateAction func(ctx *AgentContext) error

// AgentStateMachine manages agent execution state transitions
type AgentStateMachine interface {
	// Transition attempts to transition to the target state
	Transition(ctx *AgentContext, targetState AgentExecutionState) error

	// GetCurrentState returns the current state (thread-safe)
	GetCurrentState() AgentExecutionState

	// GetPreviousState returns the previous state (thread-safe)
	GetPreviousState() AgentExecutionState

	// CanTransition checks if a transition is valid without executing it
	CanTransition(ctx *AgentContext, targetState AgentExecutionState) bool

	// GetValidTransitions returns all valid transitions from current state
	GetValidTransitions(ctx *AgentContext) []AgentExecutionState

	// Reset resets the state machine to idle
	Reset()
}

// AgentExecutionState represents the state of the agent execution loop
// This is a more granular state than ChatStatus and is used for the state machine
type AgentExecutionState int

const (
	// StateIdle indicates no active work
	StateIdle AgentExecutionState = iota
	// StateCheckingQueue indicates examining message queue
	StateCheckingQueue
	// StateStreamingLLM indicates waiting for LLM response
	StateStreamingLLM
	// StatePostStream indicates after stream, before tool evaluation
	StatePostStream
	// StateEvaluatingTools indicates categorizing tool calls
	StateEvaluatingTools
	// StateApprovingTools indicates waiting for user approvals (sequential)
	StateApprovingTools
	// StateBlockingTools indicates approval is required but no approver is
	// reachable (approval_behaviour resolves to block), so the gated tool calls
	// are rejected with a reason instead of being prompted or executed.
	StateBlockingTools
	// StateExecutingTools indicates running tools (parallel)
	StateExecutingTools
	// StatePostToolExecution indicates after all tools complete
	StatePostToolExecution
	// StateCompleting indicates finalizing loop
	StateCompleting
	// StateStopped indicates loop terminated
	StateStopped
	// StateCancelled indicates user cancelled
	StateCancelled
	// StateError indicates error occurred
	StateError
)

func (s AgentExecutionState) String() string {
	switch s {
	case StateIdle:
		return "Idle"
	case StateCheckingQueue:
		return "CheckingQueue"
	case StateStreamingLLM:
		return "StreamingLLM"
	case StatePostStream:
		return "PostStream"
	case StateEvaluatingTools:
		return "EvaluatingTools"
	case StateApprovingTools:
		return "ApprovingTools"
	case StateBlockingTools:
		return "BlockingTools"
	case StateExecutingTools:
		return "ExecutingTools"
	case StatePostToolExecution:
		return "PostToolExecution"
	case StateCompleting:
		return "Completing"
	case StateStopped:
		return "Stopped"
	case StateCancelled:
		return "Cancelled"
	case StateError:
		return "Error"
	default:
		return "Unknown"
	}
}
