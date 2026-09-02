// Shared value types of the agent kernel: modes, tool-call lifecycle,
// approval actions, chat/agent status enums.

package domain

import (
	"strings"
	"time"
)

// AgentMode represents the operational mode of the agent
type AgentMode int

const (
	// AgentModeStandard is the default mode with all configured tools and approval checks
	AgentModeStandard AgentMode = iota
	// AgentModePlan is a read-only mode for planning without execution
	AgentModePlan
	// AgentModeAutoAccept bypasses all approval checks (YOLO mode)
	AgentModeAutoAccept
	// AgentModeAutoWithJudge keeps the standard approval rules but hands every
	// gated call to an LLM judge (see judge.yaml) instead of a human: allow-listed
	// commands still pass for free, anything off-list is decided by one judge
	// call. The no-human approver for headless/CI runs.
	AgentModeAutoWithJudge
	// AgentModeReadOnly is an Explore-like capability for subagents: only
	// read/search tools are offered and approval is bypassed (the toolset is
	// read-only by construction). It is a subagent capability selected by the
	// Agent tool's `type` parameter, not a human shift+tab mode.
	AgentModeReadOnly
)

// ModeKey returns the canonical mode key - the inverse of ParseAgentMode:
// "standard", "plan", "auto", "auto-with-judge", "readonly". It identifies the
// mode itself (reminder guidance, telemetry, subagent env, extension bridge);
// the bash allow-list bucket is resolved from the AgentMode inside config.
func (m AgentMode) ModeKey() string {
	switch m {
	case AgentModePlan:
		return "plan"
	case AgentModeAutoAccept:
		return "auto"
	case AgentModeAutoWithJudge:
		return "auto-with-judge"
	case AgentModeReadOnly:
		return "readonly"
	default:
		return "standard"
	}
}

// ParseAgentMode is the inverse of ModeKey: it maps a mode key
// ("standard"/"plan"/"auto"/"auto-with-judge"/"readonly") back to an AgentMode. Matching
// is case-insensitive and tolerant of surrounding whitespace. ok is false for
// an empty or unrecognized key, in which case callers should keep
// AgentModeStandard.
func ParseAgentMode(s string) (AgentMode, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "standard":
		return AgentModeStandard, true
	case "plan":
		return AgentModePlan, true
	case "auto":
		return AgentModeAutoAccept, true
	case "auto-with-judge":
		return AgentModeAutoWithJudge, true
	case "readonly":
		return AgentModeReadOnly, true
	default:
		return AgentModeStandard, false
	}
}

func (m AgentMode) String() string {
	switch m {
	case AgentModeStandard:
		return "Standard"
	case AgentModePlan:
		return "Plan"
	case AgentModeAutoAccept:
		return "AutoAccept"
	case AgentModeAutoWithJudge:
		return "AutoWithJudge"
	case AgentModeReadOnly:
		return "ReadOnly"
	default:
		return "Unknown"
	}
}

// DisplayName returns a user-friendly display name for the mode
func (m AgentMode) DisplayName() string {
	switch m {
	case AgentModeStandard:
		return "Standard"
	case AgentModePlan:
		return "Plan Mode"
	case AgentModeAutoAccept:
		return "Auto-Accept"
	case AgentModeAutoWithJudge:
		return "Auto+Judge"
	case AgentModeReadOnly:
		return "Read-Only"
	default:
		return "Unknown"
	}
}

// RetryStatus tracks the current retry state for reconnection attempts.
// A nil *RetryStatus means no retry is in progress.
type RetryStatus struct {
	Attempt     int
	MaxAttempts int
}

// ChatStatus represents the current chat operation status
type ChatStatus int

const (
	ChatStatusIdle ChatStatus = iota
	ChatStatusStarting
	ChatStatusThinking
	ChatStatusGenerating
	ChatStatusReceivingTools
	ChatStatusWaitingTools
	ChatStatusCompleted
	ChatStatusError
	ChatStatusCancelled
)

func (c ChatStatus) String() string {
	switch c {
	case ChatStatusIdle:
		return "Idle"
	case ChatStatusStarting:
		return "Starting"
	case ChatStatusThinking:
		return "Thinking"
	case ChatStatusGenerating:
		return "Generating"
	case ChatStatusReceivingTools:
		return "ReceivingTools"
	case ChatStatusWaitingTools:
		return "WaitingTools"
	case ChatStatusCompleted:
		return "Completed"
	case ChatStatusError:
		return "Error"
	case ChatStatusCancelled:
		return "Cancelled"
	default:
		return "Unknown"
	}
}

// ToolCall represents a tool call with proper typing
type ToolCall struct {
	ID        string               `json:"id"`
	Name      string               `json:"name"`
	Arguments map[string]any       `json:"arguments"`
	Status    ToolCallStatus       `json:"status"`
	Result    *ToolExecutionResult `json:"result,omitempty"`
	StartTime time.Time            `json:"start_time"`
	EndTime   *time.Time           `json:"end_time,omitempty"`
}

// ToolCallStatus represents the status of an individual tool call
type ToolCallStatus int

const (
	ToolCallStatusPending ToolCallStatus = iota
	ToolCallStatusWaitingApproval
	ToolCallStatusExecuting
	ToolCallStatusCompleted
	ToolCallStatusFailed
	ToolCallStatusCancelled
	ToolCallStatusDenied
)

func (t ToolCallStatus) String() string {
	switch t {
	case ToolCallStatusPending:
		return "Pending"
	case ToolCallStatusWaitingApproval:
		return "WaitingApproval"
	case ToolCallStatusExecuting:
		return "Executing"
	case ToolCallStatusCompleted:
		return "Completed"
	case ToolCallStatusFailed:
		return "Failed"
	case ToolCallStatusCancelled:
		return "Cancelled"
	case ToolCallStatusDenied:
		return "Denied"
	default:
		return "Unknown"
	}
}

// ToolExecutionStatus represents the overall tool execution session status
type ToolExecutionStatus int

const (
	ToolExecutionStatusIdle ToolExecutionStatus = iota
	ToolExecutionStatusProcessing
	ToolExecutionStatusExecuting
	ToolExecutionStatusCompleted
	ToolExecutionStatusFailed
)

func (t ToolExecutionStatus) String() string {
	switch t {
	case ToolExecutionStatusIdle:
		return "Idle"
	case ToolExecutionStatusProcessing:
		return "Processing"
	case ToolExecutionStatusExecuting:
		return "Executing"
	case ToolExecutionStatusCompleted:
		return "Completed"
	case ToolExecutionStatusFailed:
		return "Failed"
	default:
		return "Unknown"
	}
}

// ApprovalAction represents the user's choice for tool approval
type ApprovalAction int

const (
	ApprovalApprove ApprovalAction = iota
	ApprovalReject
	ApprovalAutoAccept
)

func (a ApprovalAction) String() string {
	switch a {
	case ApprovalApprove:
		return "Approve"
	case ApprovalReject:
		return "Reject"
	case ApprovalAutoAccept:
		return "Auto-Accept"
	default:
		return "Unknown"
	}
}

// PlanApprovalAction represents the user's choice for plan approval
type PlanApprovalAction int

const (
	PlanApprovalAccept PlanApprovalAction = iota
	PlanApprovalReject
	PlanApprovalAcceptStandard
)

func (a PlanApprovalAction) String() string {
	switch a {
	case PlanApprovalAccept:
		return "Accept"
	case PlanApprovalReject:
		return "Reject"
	case PlanApprovalAcceptStandard:
		return "Approve Each Step"
	default:
		return "Unknown"
	}
}

// AgentState represents the current state of an agent
type AgentState int

const (
	AgentStateUnknown AgentState = iota
	AgentStatePullingImage
	AgentStateStarting
	AgentStateWaitingReady
	AgentStateReady
	AgentStateFailed
)

func (a AgentState) String() string {
	switch a {
	case AgentStateUnknown:
		return "Unknown"
	case AgentStatePullingImage:
		return "PullingImage"
	case AgentStateStarting:
		return "Starting"
	case AgentStateWaitingReady:
		return "WaitingReady"
	case AgentStateReady:
		return "Ready"
	case AgentStateFailed:
		return "Failed"
	default:
		return "Unknown"
	}
}

// DisplayName returns a user-friendly display name for the agent state
func (a AgentState) DisplayName() string {
	switch a {
	case AgentStateUnknown:
		return "unknown"
	case AgentStatePullingImage:
		return "pulling image"
	case AgentStateStarting:
		return "starting"
	case AgentStateWaitingReady:
		return "waiting"
	case AgentStateReady:
		return "ready"
	case AgentStateFailed:
		return "failed"
	default:
		return "unknown"
	}
}
