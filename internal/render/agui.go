package render

import (
	"encoding/json"
	"fmt"
	"io"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	guuid "github.com/google/uuid"

	domain "github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/logger"
)

// aguiEncoder serializes headless agent output as AG-UI protocol events.
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

func (e *aguiEncoder) emitRunStarted(sessionID string) {
	e.threadID = sessionID
	e.runID = guuid.New().String()
	e.emit(aguievents.NewRunStartedEvent(e.threadID, e.runID))
}

func (e *aguiEncoder) emitRunFinished() {
	opts := []aguievents.RunFinishedOption{aguievents.WithSuccessOutcome()}
	if e.result != nil {
		opts = append(opts, aguievents.WithResult(e.result))
	}
	e.emit(aguievents.NewRunFinishedEventWithOptions(e.threadID, e.runID, opts...))
}

func (e *aguiEncoder) emitRunError(message string) {
	opts := []aguievents.RunErrorOption{}
	if e.runID != "" {
		opts = append(opts, aguievents.WithRunID(e.runID))
	}
	e.emit(aguievents.NewRunErrorEvent(message, opts...))
}

func (e *aguiEncoder) emitTextDelta(messageID, delta string) {
	e.emit(aguievents.NewTextMessageContentEvent(messageID, delta))
}

func (e *aguiEncoder) emitReasoningDelta(messageID, delta string) {
	e.emit(aguievents.NewReasoningMessageContentEvent(messageID, delta))
}

func (e *aguiEncoder) emitToolCallStart(id, name string) {
	e.emit(aguievents.NewToolCallStartEvent(id, name))
}

func (e *aguiEncoder) emitToolCallArgs(id, args string) {
	if args != "" {
		e.emit(aguievents.NewToolCallArgsEvent(id, args))
	}
}

func (e *aguiEncoder) emitToolCallEnd(id string) {
	e.emit(aguievents.NewToolCallEndEvent(id))
}

func (e *aguiEncoder) emitToolResult(r *domain.ToolExecutionResult) {
	content, _ := json.Marshal(r)
	e.emit(aguievents.NewToolCallResultEvent(guuid.New().String(), r.ToolName, string(content)))
}

func (e *aguiEncoder) emitTodos(todos []domain.TodoItem) {
	e.emit(aguievents.NewStateSnapshotEvent(map[string]any{"todos": todos}))
}

func (e *aguiEncoder) emitApprovalRequest(req domain.ApprovalRequest) {
	e.emit(aguievents.NewCustomEvent("approval_request", aguievents.WithValue(req)))
}
