package cmd

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	aguitypes "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	uuid "github.com/google/uuid"

	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// agui is the process-wide AG-UI encoder, set when `infer agent` runs with
// --output-format ag-ui and nil otherwise. When set, the legacy JSON emit
// points in agent.go delegate to it instead of printing their own lines, so
// stdout carries exclusively newline-delimited AG-UI protocol events.
var agui *aguiEncoder

// aguiEncoder serializes the headless agent's emit points as AG-UI protocol
// events (github.com/ag-ui-protocol/ag-ui), one JSON object per line.
type aguiEncoder struct {
	w        io.Writer
	threadID string
	runID    string
	result   any
}

func (e *aguiEncoder) emit(ev aguievents.Event) {
	data, err := ev.ToJSON()
	if err != nil {
		logger.Error("failed to marshal AG-UI event", "error", err, "type", ev.Type())
		return
	}
	if _, err := fmt.Fprintf(e.w, "%s\n", data); err != nil {
		logger.Error("failed to write AG-UI event", "error", err)
	}
}

// emitRunStarted brackets the run: threadID is the (resolved) session id,
// runID a fresh per-invocation id.
func (e *aguiEncoder) emitRunStarted(sessionID string) {
	e.threadID = sessionID
	e.runID = uuid.New().String()
	e.emit(aguievents.NewRunStartedEvent(e.threadID, e.runID))
}

// emitMessagesSnapshot emits the history restored by --session-id so a
// resuming client can render the prior conversation.
func (e *aguiEncoder) emitMessagesSnapshot(conversation []ConversationMessage) {
	messages := make([]aguitypes.Message, 0, len(conversation))
	for _, msg := range conversation {
		if msg.Role == "system" || msg.Internal {
			continue
		}
		m := aguitypes.Message{
			ID:         uuid.New().String(),
			Role:       aguitypes.Role(msg.Role),
			Content:    msg.Content,
			ToolCallID: msg.ToolCallID,
		}
		if msg.ToolCalls != nil {
			for _, tc := range *msg.ToolCalls {
				m.ToolCalls = append(m.ToolCalls, aguitypes.ToolCall{
					ID:       tc.ID,
					Type:     string(tc.Type),
					Function: aguitypes.FunctionCall{Name: tc.Function.Name, Arguments: tc.Function.Arguments},
				})
			}
		}
		messages = append(messages, m)
	}
	e.emit(aguievents.NewMessagesSnapshotEvent(messages))
}

// emitMessage maps one conversation message onto AG-UI events: tool results
// become TOOL_CALL_RESULT carrying the raw execution result, text becomes a
// TEXT_MESSAGE_START/CONTENT/END triad, and assistant tool calls become
// TOOL_CALL_START/ARGS/END triads.
func (e *aguiEncoder) emitMessage(msg ConversationMessage) {
	if msg.Role == "tool" {
		e.emit(aguievents.NewToolCallResultEvent(uuid.New().String(), msg.ToolCallID, aguiToolResultContent(msg)))
		return
	}

	messageID := cmp.Or(msg.RequestID, uuid.New().String())
	if msg.Content != "" {
		e.emit(aguievents.NewTextMessageStartEvent(messageID, aguievents.WithRole(msg.Role)))
		e.emit(aguievents.NewTextMessageContentEvent(messageID, msg.Content))
		e.emit(aguievents.NewTextMessageEndEvent(messageID))
	}

	if msg.ToolCalls == nil {
		return
	}
	for _, tc := range *msg.ToolCalls {
		opts := []aguievents.ToolCallStartOption{}
		if msg.Content != "" {
			opts = append(opts, aguievents.WithParentMessageID(messageID))
		}
		e.emit(aguievents.NewToolCallStartEvent(tc.ID, tc.Function.Name, opts...))
		if tc.Function.Arguments != "" {
			e.emit(aguievents.NewToolCallArgsEvent(tc.ID, tc.Function.Arguments))
		}
		e.emit(aguievents.NewToolCallEndEvent(tc.ID))
	}
}

// aguiToolResultContent returns the raw tool execution result as JSON, without
// the legacy "Result of tool call: ..." prefix the json format wraps it in.
func aguiToolResultContent(msg ConversationMessage) string {
	if msg.ToolExecution == nil {
		return msg.Content
	}
	data, err := json.Marshal(msg.ToolExecution)
	if err != nil {
		return msg.Content
	}
	return string(data)
}

// emitTodos surfaces the agent's todo list as the AG-UI state channel.
func (e *aguiEncoder) emitTodos(todos []domain.TodoItem) {
	e.emit(aguievents.NewStateSnapshotEvent(map[string]any{"todos": todos}))
}

// emitApprovalRequest surfaces the bespoke stdin/stdout approval handshake as
// a documented CUSTOM event; replies still arrive as approval_response lines
// on stdin. A future phase maps this onto an interrupt outcome.
func (e *aguiEncoder) emitApprovalRequest(req domain.ApprovalRequest) {
	e.emit(aguievents.NewCustomEvent("approval_request", aguievents.WithValue(req)))
}

// emitRunFinished terminates a successful run, carrying the session stats
// captured from emitSessionStats as the run result.
func (e *aguiEncoder) emitRunFinished() {
	opts := []aguievents.RunFinishedOption{aguievents.WithSuccessOutcome()}
	if e.result != nil {
		opts = append(opts, aguievents.WithResult(e.result))
	}
	e.emit(aguievents.NewRunFinishedEventWithOptions(e.threadID, e.runID, opts...))
}

// emitRunError terminates a failed run (error return or panic path).
func (e *aguiEncoder) emitRunError(message string) {
	opts := []aguievents.RunErrorOption{}
	if e.runID != "" {
		opts = append(opts, aguievents.WithRunID(e.runID))
	}
	e.emit(aguievents.NewRunErrorEvent(message, opts...))
}

// --- Streaming emit points ---
//
// When `infer agent --output-format ag-ui` streams a model turn (see
// executeStreamingTurn in agent.go), text and reasoning arrive token by token.
// These helpers emit the AG-UI START/CONTENT/END triads incrementally so a
// client renders the answer and the thinking as they are generated, instead of
// one full message per turn via emitMessage.

func (e *aguiEncoder) emitTextStart(messageID string) {
	e.emit(aguievents.NewTextMessageStartEvent(messageID, aguievents.WithRole("assistant")))
}

func (e *aguiEncoder) emitTextDelta(messageID, delta string) {
	e.emit(aguievents.NewTextMessageContentEvent(messageID, delta))
}

func (e *aguiEncoder) emitTextEnd(messageID string) {
	e.emit(aguievents.NewTextMessageEndEvent(messageID))
}

func (e *aguiEncoder) emitReasoningStart(messageID string) {
	e.emit(aguievents.NewReasoningMessageStartEvent(messageID, "assistant"))
}

func (e *aguiEncoder) emitReasoningDelta(messageID, delta string) {
	e.emit(aguievents.NewReasoningMessageContentEvent(messageID, delta))
}

func (e *aguiEncoder) emitReasoningEnd(messageID string) {
	e.emit(aguievents.NewReasoningMessageEndEvent(messageID))
}

// emitToolCalls emits only the tool-call triads of an assistant message. The
// streaming path uses it after the turn: text and reasoning were already
// streamed as deltas, but the turn's tool calls still need to reach the client.
// ponytail: tool-call arguments are emitted whole, not token-streamed - args
// are JSON with no rendering benefit from streaming. Stream them per-delta if a
// client ever needs live arg preview.
func (e *aguiEncoder) emitToolCalls(msg ConversationMessage) {
	if msg.ToolCalls == nil {
		return
	}
	for _, tc := range *msg.ToolCalls {
		e.emit(aguievents.NewToolCallStartEvent(tc.ID, tc.Function.Name))
		if tc.Function.Arguments != "" {
			e.emit(aguievents.NewToolCallArgsEvent(tc.ID, tc.Function.Arguments))
		}
		e.emit(aguievents.NewToolCallEndEvent(tc.ID))
	}
}
