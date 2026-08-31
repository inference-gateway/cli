package domain

import (
	"slices"
	"time"
)

// HookPoint is one of the pre-defined points in the agent loop where actions
// can attach. The catalog is fully symmetric: every loop phase exposes a
// pre_/post_ pair. System reminders attach a text-injection
// action here today; executable command hooks attach a
// command-execution action at the same points later, both flowing through the
// single dispatchHooks(point) seam.
type HookPoint string

const (
	HookPreSession     HookPoint = "pre_session"      // run begins, before the first stream (turn 1)
	HookPostSession    HookPoint = "post_session"     // run finished ("agent finished generating")
	HookPreStream      HookPoint = "pre_stream"       // before each LLM streaming turn
	HookPostStream     HookPoint = "post_stream"      // after each LLM response, before tool evaluation
	HookPreTool        HookPoint = "pre_tool"         // before tool execution
	HookPostTool       HookPoint = "post_tool"        // after tool execution
	HookPreQueueDrain  HookPoint = "pre_queue_drain"  // before draining queued user messages
	HookPostQueueDrain HookPoint = "post_queue_drain" // after draining queued user messages
)

// HookPoints is the canonical catalog, used for config validation. Order is
// the loop order (a run flows top to bottom, looping the middle phases).
var HookPoints = []HookPoint{
	HookPreSession,
	HookPreStream,
	HookPostStream,
	HookPreTool,
	HookPostTool,
	HookPreQueueDrain,
	HookPostQueueDrain,
	HookPostSession,
}

// Valid reports whether h is one of the pre-defined hook points.
func (h HookPoint) Valid() bool { return slices.Contains(HookPoints, h) }

// SystemReminder is a resolved reminder ready to inject into the conversation.
// When AppendToToolResult is true, the Text is appended to the last tool-role
// message content (for tool_call/tool pairing) instead of inserted as a
// standalone user message. Set by the provider for on_repeated_failure reminders.
type SystemReminder struct {
	Name               string
	Text               string
	AppendToToolResult bool
}

// ReminderQuery carries the context a SystemReminderProvider needs to decide
// which reminders are due at a hook point.
//
// Turn and SessionTurn differ deliberately. Turn is the agent-loop turn within
// the CURRENT run (one user message in chat), used by the turns_before_max
// trigger relative to MaxTurns. SessionTurn is the cumulative model-turn count
// across the whole chat session - it does NOT reset when a new user message
// starts a fresh run, so the `interval` trigger fires on every Nth
// conversational turn as users expect (per-request Turns would reset to 1 each
// message and an interval reminder would essentially never fire in chat). In
// headless `infer headless` a single invocation IS the session, so the two are
// equal.
//
// Fired carries reminder names already emitted this session (consulted by the
// `once` trigger); the caller marks names fired after injecting.
//
// ToolFailed reports whether the tool batch that just completed had any failed
// call. It is meaningful only at the post_tool hook (set right before that
// dispatch) and drives the `on_failure` trigger.
//
// RepeatedFailures and FailedTool are set at the post_tool hook when the same
// tool call has failed threshold+ consecutive times; they drive the
// `on_repeated_failure` trigger. FailedTool is the function name, for
// templating.
//
// FinishReason carries the LLM response's finish_reason string. It drives
// the `on_truncation` trigger at the post_stream hook (firing when the value
// is "length").
//
// IncompleteTodos carries the remaining open todo items from the model's
// TodoWrite list; it drives the `on_stalled_todos` trigger at the post_stream
// hook (firing when non-empty and the response had no tool calls).
// StalledStrikes is the count of consecutive no-tool-call responses, gating
// that trigger's strike cap (threshold).
//
// ModeChanged reports whether the agent mode differs from the previous
// streaming turn; PrevMode/Mode carry the transition. They are meaningful only
// at the pre_stream hook (set right before that dispatch) and drive the
// `on_mode_change` trigger.
type ReminderQuery struct {
	Hook             HookPoint
	Turn             int
	SessionTurn      int
	MaxTurns         int
	Fired            map[string]bool
	ToolFailed       bool
	RepeatedFailures int
	FailedTool       string
	FinishReason     string
	IncompleteTodos  []TodoItem
	StalledStrikes   int
	TodoCount        int
	ModeChanged      bool
	PrevMode         AgentMode
	Mode             AgentMode
	// ModeGuidance carries per-mode adjustment instructions keyed by
	// AllowedlistKey (e.g. "plan"/"auto"), layered onto the on_mode_change
	// trigger from prompts.yaml (agent.mode_adjustment_plan/_auto). It is
	// consulted only when the reminder's own guidance for the mode is unset
	// or still the built-in default, so a user-edited reminders.yaml
	// guidance key keeps precedence.
	ModeGuidance map[string]string
}

// HookCommand is a resolved command hook ready to run at a hook point: a named
// shell command with a wall-clock timeout. It is the command-action sibling of
// SystemReminder (the text-injection action). The agent - not the provider -
// runs it, through the same bash allow-list a model-proposed command faces.
type HookCommand struct {
	Name    string
	Command string
	Timeout time.Duration
}
