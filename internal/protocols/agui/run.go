package agui

import (
	"context"
	"errors"
	"io"
	"strings"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	aguitypes "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	uuid "github.com/google/uuid"

	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

// The messages of a MESSAGES_SNAPSHOT and the run input and interrupt types a
// caller bridges between the CLI and the wire.
type (
	Message       = aguitypes.Message
	Role          = aguitypes.Role
	ToolCall      = aguitypes.ToolCall
	FunctionCall  = aguitypes.FunctionCall
	Interrupt     = aguitypes.Interrupt
	ResumeEntry   = aguitypes.ResumeEntry
	RunAgentInput = aguitypes.RunAgentInput
	TokenUsage    = aguievents.TokenUsage
)

// TokenCount returns a pointer to a count, for populating a TokenUsage.
func TokenCount(count int64) *int64 { return aguievents.TokenCount(count) }

// OutcomeCancelled is the cancelled variant of the 1.0 outcome set: a run its
// caller stopped before it completed, with nothing waited for. The Go SDK
// names success and interrupt; this names the third variant the spec defines.
const OutcomeCancelled = aguievents.RunFinishedOutcomeType("cancelled")

// The interrupt reasons this CLI raises. AG-UI leaves reason open ended.
const (
	InterruptToolCall      = "tool_call"
	InterruptInputRequired = "input_required"
)

// The state keys a run publishes. The state is one object: Start seeds every
// key, so a STATE_SNAPSHOT always carries all of them, and a change patches
// one key through STATE_DELTA instead of replacing the others.
const (
	StateTodos           = "todos"
	StateUsage           = "usage"
	StateBackgroundTasks = "backgroundTasks"
	StateScreenRecording = "screenRecording"
)

// The state's zero values, per key.
func seedState() map[string]any {
	return map[string]any{
		StateTodos:           []any{},
		StateUsage:           []any{},
		StateBackgroundTasks: map[string]any{"running": 0, "jobs": []any{}},
		StateScreenRecording: map[string]any{"active": false},
	}
}

// Published is what an owning context renders into the run for one of its own
// chat events: a key patch of the run's state object or an ACTIVITY_SNAPSHOT
// entry. An event that deserves neither reports false on the publish seam and
// the run drops it.
type Published struct {
	StateKey     string
	StateValue   any
	ActivityType string
	MessageID    string
	Content      any
}

// WriteCustom writes one CUSTOM event outside any run - the extension point
// for what the protocol has no event for, today the approval prompt of a
// panel-initiated tool request. Run-scoped events never take this path.
func WriteCustom(w io.Writer, name string, value any) {
	write(w, aguievents.NewCustomEvent(name, aguievents.WithValue(value)))
}

// WriteRunError writes one RUN_ERROR for a failure outside any run: a refused
// frame or a process failure before the run starts. No run exists to carry a
// run id, so the event carries none.
func WriteRunError(w io.Writer, message string) {
	write(w, aguievents.NewRunErrorEvent(message))
}

func write(w io.Writer, ev aguievents.Event) {
	data, err := ev.ToJSON()
	if err != nil {
		logger.Error("failed to marshal AG-UI event", "error", err, "type", ev.Type())
		return
	}
	if _, err := w.Write(append(data, '\n')); err != nil {
		logger.Error("failed to write AG-UI event", "error", err)
	}
}

// Run writes one AG-UI run as newline-delimited events: RUN_STARTED at Start,
// the events its methods name, and exactly one terminal RUN_FINISHED or
// RUN_ERROR at Finish. Every event lands within the run, one Write per event,
// so a transport can map one Write to one frame.
type Run struct {
	w io.Writer

	threadID string
	runID    string

	msgID         string
	textOpen      bool
	reasoningOpen bool

	// state is the run's one state object, seeded at Start. A nil state means
	// the run has not started yet, which is why emit drops events before it.
	state map[string]any

	// carried is the state a continuation run inherits from the run it
	// follows, whose interrupt a resume answered.
	carried map[string]any

	// ended marks the terminal event written. Every event after it is
	// dropped, so a run never writes outside its own frame.
	ended bool
	err   error
}

// NewRun builds the writer of one run over w.
func NewRun(w io.Writer) *Run {
	return &Run{w: w}
}

// NewContinuationRun builds the writer of a run that continues a suspended
// one: its STATE_SNAPSHOT seeds the state the suspended run left.
func NewContinuationRun(w io.Writer, carried map[string]any) *Run {
	return &Run{w: w, carried: carried}
}

func (r *Run) emit(ev aguievents.Event) {
	if r.ended {
		logger.Debug("dropped an AG-UI event for an already ended run", "type", ev.Type())
		return
	}
	if r.state == nil && ev.Type() != aguievents.EventTypeRunError {
		logger.Debug("dropped an AG-UI event for a run that has not started", "type", ev.Type())
		return
	}
	write(r.w, ev)
}

// Start writes RUN_STARTED and seeds the run's state object, so the first
// STATE_SNAPSHOT of every run carries all state keys.
func (r *Run) Start(threadID, runID string) {
	r.threadID = threadID
	r.runID = runID
	r.state = seedState()
	for key, value := range r.carried {
		r.state[key] = value
	}
	r.emit(aguievents.NewRunStartedEvent(threadID, runID))
	r.emit(aguievents.NewStateSnapshotEvent(r.state))
}

// State returns the run's one state object, live, or nil before Start.
func (r *Run) State() map[string]any {
	return r.state
}

// Snapshot writes MESSAGES_SNAPSHOT.
func (r *Run) Snapshot(messages []Message) {
	r.emit(aguievents.NewMessagesSnapshotEvent(messages))
}

// Text writes a text delta, opening the turn's assistant message on the first
// one. It closes an open reasoning block, which precedes text.
func (r *Run) Text(delta string) {
	if r.msgID == "" {
		r.msgID = uuid.New().String()
	}
	if r.reasoningOpen {
		r.emit(aguievents.NewReasoningMessageEndEvent(r.msgID))
		r.reasoningOpen = false
	}
	if !r.textOpen {
		r.emit(aguievents.NewTextMessageStartEvent(r.msgID, aguievents.WithRole("assistant")))
		r.textOpen = true
	}
	r.emit(aguievents.NewTextMessageContentEvent(r.msgID, delta))
}

// Reasoning writes a reasoning delta, opening the turn's reasoning message on
// the first one. Reasoning precedes text within one assistant message, so
// reasoning arriving while text is open closes the previous message first.
func (r *Run) Reasoning(delta string) {
	if r.textOpen {
		r.CloseMessage()
	}
	if r.msgID == "" {
		r.msgID = uuid.New().String()
	}
	if !r.reasoningOpen {
		r.emit(aguievents.NewReasoningMessageStartEvent(r.msgID, "assistant"))
		r.reasoningOpen = true
	}
	r.emit(aguievents.NewReasoningMessageContentEvent(r.msgID, delta))
}

// Message writes a complete message with the role it has in the conversation:
// a note the agent queued or a user message rendered over the run.
func (r *Run) Message(role Role, content string) {
	r.CloseMessage()
	id := uuid.New().String()
	r.emit(aguievents.NewTextMessageStartEvent(id, aguievents.WithRole(string(role))))
	r.emit(aguievents.NewTextMessageContentEvent(id, content))
	r.emit(aguievents.NewTextMessageEndEvent(id))
}

// CloseMessage ends any open text or reasoning framing, so the next delta
// starts a fresh message. Safe to call when nothing is open.
func (r *Run) CloseMessage() {
	if r.reasoningOpen {
		r.emit(aguievents.NewReasoningMessageEndEvent(r.msgID))
		r.reasoningOpen = false
	}
	if r.textOpen {
		r.emit(aguievents.NewTextMessageEndEvent(r.msgID))
		r.textOpen = false
	}
	r.msgID = ""
}

// ToolCall writes one complete tool call: its start, its arguments when it has
// any, and its end.
func (r *Run) ToolCall(id, name, args string) {
	r.emit(aguievents.NewToolCallStartEvent(id, name))
	if args != "" {
		r.emit(aguievents.NewToolCallArgsEvent(id, args))
	}
	r.emit(aguievents.NewToolCallEndEvent(id))
}

// ToolResult writes the result of the tool call toolCallID.
func (r *Run) ToolResult(toolCallID, content string) {
	r.emit(aguievents.NewToolCallResultEvent(uuid.New().String(), toolCallID, content))
}

// PatchState updates one key of the run's state object and writes STATE_DELTA
// patching exactly that key, so state changes never replace the siblings.
func (r *Run) PatchState(key string, value any) {
	if r.state == nil {
		return
	}
	r.state[key] = value
	r.emit(aguievents.NewStateDeltaEvent([]aguievents.JSONPatchOperation{{Op: "add", Path: stateKeyPointer(key), Value: value}}))
}

// Activity writes ACTIVITY_SNAPSHOT, the progress or notice of one item of
// work a client keeps as its own entry: a local agent starting up or a judge
// verdict. messageId carries the stable identity that replaces the entry.
func (r *Run) Activity(messageID, activityType string, content any) {
	r.emit(aguievents.NewActivitySnapshotEvent(messageID, activityType, content))
}

// Suspend ends the run the AG-UI interrupt way: one terminal RUN_FINISHED
// whose outcome carries the interrupts the run is waiting on. The caller
// answers with resume entries on the next run, which continues where this
// one left off.
func (r *Run) Suspend(interrupts []Interrupt) {
	if r.state == nil {
		logger.Debug("cannot suspend a run that has not started")
		return
	}
	r.CloseMessage()
	r.emit(aguievents.NewRunFinishedEventWithOptions(r.threadID, r.runID, aguievents.WithInterruptOutcome(interrupts)))
	r.ended = true
}

// Fail records the failure the run ends with. A later failure replaces it.
func (r *Run) Fail(err error) {
	r.err = err
}

// Finish writes the run's one terminal event and returns the failure: a
// cancelled run ends with outcome cancelled, a failed one with RUN_ERROR,
// both carrying usage, and a clean run with outcome success carrying the
// result. Suspend already wrote the terminal, so an interrupted run writes
// nothing here. A run that failed before it started writes only a RUN_ERROR
// without a run id, the one event a run may carry before RUN_STARTED.
func (r *Run) Finish(result map[string]any, usage []TokenUsage) error {
	if r.ended {
		return r.err
	}
	if r.state == nil {
		if r.err != nil {
			r.emit(aguievents.NewRunErrorEvent(r.err.Error()))
		}
		return r.err
	}
	r.CloseMessage()
	switch {
	case errors.Is(r.err, context.Canceled):
		opts := []aguievents.RunFinishedOption{
			aguievents.WithOutcome(aguievents.RunFinishedOutcome{Type: OutcomeCancelled}),
		}
		if len(usage) > 0 {
			opts = append(opts, aguievents.WithUsage(usage))
		}
		r.emit(aguievents.NewRunFinishedEventWithOptions(r.threadID, r.runID, opts...))
	case r.err != nil:
		r.emitRunError(r.err.Error(), usage)
	default:
		opts := []aguievents.RunFinishedOption{aguievents.WithSuccessOutcome()}
		if len(result) > 0 {
			opts = append(opts, aguievents.WithResult(result))
		}
		if len(usage) > 0 {
			opts = append(opts, aguievents.WithUsage(usage))
		}
		r.emit(aguievents.NewRunFinishedEventWithOptions(r.threadID, r.runID, opts...))
	}
	return r.err
}

// emitRunError carries no run id for a run that fails before it started.
func (r *Run) emitRunError(message string, usage []TokenUsage) {
	opts := []aguievents.RunErrorOption{}
	if r.runID != "" {
		opts = append(opts, aguievents.WithRunID(r.runID))
	}
	if len(usage) > 0 {
		opts = append(opts, aguievents.WithErrorUsage(usage))
	}
	r.emit(aguievents.NewRunErrorEvent(message, opts...))
}

// stateKeyPointer renders a state key as a JSON Pointer the patch applies to.
func stateKeyPointer(key string) string {
	pointer := strings.ReplaceAll(key, "~", "~0")
	return "/" + strings.ReplaceAll(pointer, "/", "~1")
}