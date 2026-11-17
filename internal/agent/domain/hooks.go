package domain

import (
	"slices"
	"time"
)

// HookPoint is one of the pre-defined points in the agent loop where actions
// attach. Every loop phase exposes a pre_/post_ pair. System reminders and
// command hooks both attach here and flow through dispatchHooks(point).
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

// ReminderQuery is what a SystemReminderProvider needs to pick due reminders.
// Turn counts within the current run, while SessionTurn counts across the chat
// so interval reminders fire. Fired lists names already emitted. The remaining
// fields drive one trigger each at the hook that sets them: post_tool
// failures, post_stream truncation, stalled todos and empty replies, pre_stream mode changes.
type ReminderQuery struct {
	Hook             HookPoint
	Turn             int
	SessionTurn      int
	MaxTurns         int
	Elapsed          time.Duration
	Fired            map[string]bool
	ToolFailed       bool
	RepeatedFailures int
	FailedTool       string
	FinishReason     string
	IncompleteTodos  []TodoItem
	StalledStrikes   int
	EmptyResponse    bool
	TodoCount        int
	ModeChanged      bool
	PrevMode         AgentMode
	Mode             AgentMode
	ModeGuidance     map[string]string
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
