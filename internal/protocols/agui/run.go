package agui

import (
	"context"
	"errors"
	"io"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	aguitypes "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	uuid "github.com/google/uuid"

	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

// The messages of a MESSAGES_SNAPSHOT.
type (
	Message      = aguitypes.Message
	Role         = aguitypes.Role
	ToolCall     = aguitypes.ToolCall
	FunctionCall = aguitypes.FunctionCall
)

// CustomEvent is one AG-UI CUSTOM event. Resumes marks a run that carries on
// after an interruption, which discards the failure the interruption left.
type CustomEvent struct {
	Name    string
	Value   any
	Resumes bool
}

// Run writes one AG-UI run as newline-delimited events: RUN_STARTED at Start,
// the events its methods name, and exactly one terminal RUN_FINISHED or
// RUN_ERROR at Finish. Every event is written with exactly one Write, so a
// transport can map one Write to one frame.
type Run struct {
	w io.Writer

	threadID string
	runID    string

	msgID         string
	textOpen      bool
	reasoningOpen bool

	err error
}

// NewRun builds the writer of one run over w. The methods that need no run,
// such as Custom and Snapshot, also work on a run that never starts.
func NewRun(w io.Writer) *Run {
	return &Run{w: w}
}

func (r *Run) emit(ev aguievents.Event) {
	data, err := ev.ToJSON()
	if err != nil {
		logger.Error("failed to marshal AG-UI event", "error", err, "type", ev.Type())
		return
	}
	if _, err := r.w.Write(append(data, '\n')); err != nil {
		logger.Error("failed to write AG-UI event", "error", err)
	}
}

// Start writes RUN_STARTED.
func (r *Run) Start(threadID, runID string) {
	r.threadID = threadID
	r.runID = runID
	r.emit(aguievents.NewRunStartedEvent(threadID, runID))
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

// UserMessage writes a complete user message as its own text message, closing
// anything the assistant had open first.
func (r *Run) UserMessage(content string) {
	r.CloseMessage()
	id := uuid.New().String()
	r.emit(aguievents.NewTextMessageStartEvent(id, aguievents.WithRole("user")))
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

// State writes STATE_SNAPSHOT.
func (r *Run) State(snapshot any) {
	r.emit(aguievents.NewStateSnapshotEvent(snapshot))
}

// Custom writes one CUSTOM event.
func (r *Run) Custom(name string, value any) {
	r.emit(aguievents.NewCustomEvent(name, aguievents.WithValue(value)))
}

// Publish writes a CUSTOM event and lets it resume the run.
func (r *Run) Publish(event CustomEvent) {
	r.Custom(event.Name, event.Value)
	if event.Resumes {
		r.err = nil
	}
}

// Fail records the failure the run ends with. A later failure replaces it.
func (r *Run) Fail(err error) {
	r.err = err
}

// Finish closes open framing and writes the run's one terminal event:
// RUN_FINISHED carrying result, RUN_FINISHED with outcome cancelled for a
// cancelled run, or RUN_ERROR for a failed one. It returns the failure.
func (r *Run) Finish(result map[string]any) error {
	r.CloseMessage()
	switch {
	case errors.Is(r.err, context.Canceled):
		r.emit(aguievents.NewRunFinishedEventWithOptions(r.threadID, r.runID,
			aguievents.WithOutcome(aguievents.RunFinishedOutcome{Type: "cancelled"})))
	case r.err != nil:
		r.emitRunError(r.err.Error())
	default:
		opts := []aguievents.RunFinishedOption{aguievents.WithSuccessOutcome()}
		if len(result) > 0 {
			opts = append(opts, aguievents.WithResult(result))
		}
		r.emit(aguievents.NewRunFinishedEventWithOptions(r.threadID, r.runID, opts...))
	}
	return r.err
}

// emitRunError carries no run id for a run that fails before it started.
func (r *Run) emitRunError(message string) {
	opts := []aguievents.RunErrorOption{}
	if r.runID != "" {
		opts = append(opts, aguievents.WithRunID(r.runID))
	}
	r.emit(aguievents.NewRunErrorEvent(message, opts...))
}
