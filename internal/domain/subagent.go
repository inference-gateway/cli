package domain

import (
	"context"
	"time"
)

// SubagentStatus represents the lifecycle state of a local subagent.
type SubagentStatus string

const (
	SubagentRunning   SubagentStatus = "running"
	SubagentCompleted SubagentStatus = "completed"
	SubagentFailed    SubagentStatus = "failed"
)

// SubagentMode selects how a subagent is surfaced while it runs.
const (
	SubagentModeHeadless    = "headless"
	SubagentModeInteractive = "interactive"
)

// SubagentState tracks one in-flight local subagent (an `infer agent`
// subprocess spawned by the Agent tool). It is the subagent analogue of
// TaskPollingState: the SubagentPoller selects on ResultChan/ErrorChan to
// deliver the outcome back onto the conversation.
type SubagentState struct {
	ID          string
	Label       string
	Description string
	Model       string
	Mode        string // SubagentModeHeadless | SubagentModeInteractive
	SessionID   string
	Status      SubagentStatus
	StartedAt   time.Time
	CancelFunc  context.CancelFunc
	ResultChan  chan *ToolExecutionResult
	ErrorChan   chan error
	Silent bool
}

// SubagentResultFile is the JSON written by `infer agent --result-file` on exit
// and read back by the Agent tool to harvest a subagent's outcome from a
// detached (tmux) run whose stdout the parent does not own.
type SubagentResultFile struct {
	FinalAssistant string `json:"final_assistant"`
	Success        bool   `json:"success"`
	Error          string `json:"error,omitempty"`
	SessionID      string `json:"session_id,omitempty"`
}

// SubagentTracker tracks local subagents spawned by the Agent tool. It is the
// third projection of BackgroundTaskRegistry (alongside A2ATaskTracker and
// ShellTracker); methods are suffixed with "Subagent" to avoid colliding with
// the shell tracker's same-named surface when embedded together.
type SubagentTracker interface {
	// AddSubagent registers a running subagent. Returns an error if the ID
	// is already tracked.
	AddSubagent(state *SubagentState) error

	// GetSubagent returns a subagent by ID, or nil if not tracked.
	GetSubagent(id string) *SubagentState

	// GetAllSubagents returns all tracked subagents.
	GetAllSubagents() []*SubagentState

	// RemoveSubagent removes a subagent from tracking.
	RemoveSubagent(id string) error

	// CountRunningSubagents returns the number of subagents in the running state.
	CountRunningSubagents() int
}
