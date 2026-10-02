package domain

import (
	"context"
	"errors"

	sdk "github.com/inference-gateway/sdk"
)

// ErrMaxTurnsReached is returned when the agent reaches its maximum turn limit
// without completing the task. Callers should use errors.Is to check for it.
var ErrMaxTurnsReached = errors.New("max_turns_reached")

// AgentRequest represents a request to the agent service
type AgentRequest struct {
	RequestID                  string        `json:"request_id"`
	Model                      string        `json:"model"`
	Messages                   []sdk.Message `json:"messages"`
	IsChatMode                 bool          `json:"is_chat_mode"`
	ApprovalBrokerAttached     bool          `json:"approval_broker_attached"`
	UserQuestionBrokerAttached bool          `json:"user_question_broker_attached"`
	GroupKey                   string        `json:"group_key,omitempty"`
}

// AgentService runs agent tasks and streams their events
type AgentService interface {
	// RunWithStream executes an agent task with streaming (for interactive chat)
	RunWithStream(ctx context.Context, req *AgentRequest) (<-chan ChatEvent, error)

	// CancelRequest cancels an active request
	CancelRequest(requestID string) error

	// GetMetrics returns metrics for a completed request
	GetMetrics(requestID string) *ChatMetrics

	// BuildSystemPrompt returns the static system prompt sent as message[0],
	// byte-stable across turns; volatile context travels separately as a hidden
	// per-request message (see `infer debug agent system_prompt`).
	BuildSystemPrompt() string

	// SetReasoningEffort updates the reasoning effort applied to subsequent
	// requests. An empty string resets to the provider default.
	SetReasoningEffort(effort string) error

	// GetReasoningEffort returns the effort level currently applied to
	// requests ("" = provider default).
	GetReasoningEffort() string
}
