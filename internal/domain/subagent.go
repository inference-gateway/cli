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

// EnvSubagentAgentMode names the environment variable the Agent tool sets to
// carry the parent chat's coding mode (the AgentMode.AllowedlistKey form -
// "standard"/"plan"/"auto") to a spawned subagent, so it starts in the same
// mode as its parent. It is absent for top-level `infer chat`/`infer agent`
// runs, which therefore stay Standard-by-default.
const EnvSubagentAgentMode = "INFER_SUBAGENT_AGENT_MODE"

// EnvSubagentResultFile names the environment variable the Agent tool sets on an
// interactive subagent's `infer chat` so it writes its last assistant message
// (as a SubagentResultFile JSON) to that path on each completed turn. The parent
// reads it to deliver the subagent's real answer - not the tmux pane's chrome -
// when the subagent finishes. Unset for normal `infer chat`, which writes nothing.
const EnvSubagentResultFile = "INFER_SUBAGENT_RESULT_FILE"

// EnvSubagentApprovalFile names the environment variable the Agent tool sets on
// an interactive subagent's `infer chat` so it writes a SubagentApprovalFile JSON
// to that path whenever it blocks on a tool-approval prompt (and removes it when
// the prompt resolves). The parent watches this file to surface "subagent is
// awaiting approval" to the user and relay the decision (ApproveSubagent). Unset
// for normal `infer chat`, which writes nothing.
const EnvSubagentApprovalFile = "INFER_SUBAGENT_APPROVAL_FILE"

// EnvSubagentHistoryName names the environment variable the Agent tool sets on
// an interactive subagent's `infer chat` so it uses its own history file
// (<configDir>/history/history-<name>) instead of the main agent's history.
// When unset or empty, the subagent falls back to the main history file; the
// reserved value SubagentHistoryMemoryOnly selects in-memory-only history.
const EnvSubagentHistoryName = "INFER_SUBAGENT_HISTORY_NAME"

// SubagentHistoryMemoryOnly is the reserved EnvSubagentHistoryName value telling an
// interactive subagent to keep its input history in memory only (no file). The Agent
// tool sets it for subagents without a usable label so they don't create a new
// single-use history file per spawn. sanitizeSlug never yields this value (it contains
// ':'), so it can't collide with a real slug.
const SubagentHistoryMemoryOnly = ":memory:"

// SubagentApprovalFile is the JSON an interactive subagent's chat writes while it
// is blocked on a tool-approval prompt. It is an authoritative signal (written
// the moment the chat blocks, removed when it resolves) so the parent does not
// have to scrape the pane's TUI to detect a pending approval.
type SubagentApprovalFile struct {
	Awaiting bool   `json:"awaiting"`
	Summary  string `json:"summary,omitempty"`
}

// SubagentState is the data record for one local subagent (an `infer agent`
// subprocess or tmux pane spawned by the Agent tool) that the subagent control
// tools (ListSubagents, CloseSubagent, ...) read. Monitoring is owned by the job
// supervisor (headlessSubagentJob / interactiveSubagentJob), not this struct.
type SubagentState struct {
	ID          string
	Label       string
	Description string
	Model       string
	Mode        string // SubagentModeHeadless | SubagentModeInteractive
	SessionID   string
	PaneID      string
	Status      SubagentStatus
	StartedAt   time.Time
	CancelFunc  context.CancelFunc
	Silent      bool
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

// PaneObservation is one probe of an interactive subagent's tmux pane, produced
// by a pane inspector and consumed by the interactive subagent monitor
// (interactiveSubagentJob) to decide when a turn completed or an approval is
// pending.
type PaneObservation struct {
	// Harvested is the subagent chat's real last assistant message (from its
	// result file); "" until its turn completes. The ONLY content ever delivered -
	// the pane is never scraped for content (its TUI chrome is noise).
	Harvested string
	// Screen is a snapshot of the pane's current tail, used by the poller to detect
	// idleness by stability: while the subagent works the chat's elapsed-time
	// spinner changes this every poll; at idle it is frozen. The input-box
	// placeholder ("Type your message") is NOT a usable idle signal - it is drawn
	// even mid-turn - so the stability of the whole tail is used instead.
	Screen string
	// Gone means the pane no longer exists (closed).
	Gone bool
	// Dead means the pane's process exited (the pane is kept open by remain-on-exit).
	Dead bool
	// AwaitingApproval means the subagent is blocked on a tool-approval prompt.
	AwaitingApproval bool
	// ApprovalSummary describes the pending tool call (name + args) when awaiting.
	ApprovalSummary string
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

	// SetSubagentStatus atomically updates a subagent's status under the
	// tracker's lock. Returns an error if the ID is not tracked.
	SetSubagentStatus(id string, status SubagentStatus) error
}
