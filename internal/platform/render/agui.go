package render

import (
	"encoding/json"
	"fmt"
	"io"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	uuid "github.com/google/uuid"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	ipc "github.com/inference-gateway/cli/internal/platform/ipc"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
)

// aguiEncoder serializes headless agent output as AG-UI protocol events. It
// tracks the open assistant message of the current turn so streamed text and
// reasoning arrive framed in the protocol's START/CONTENT/END triads.
type aguiEncoder struct {
	w        io.Writer
	threadID string
	runID    string

	msgID         string
	textOpen      bool
	reasoningOpen bool
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
	e.runID = uuid.New().String()
	e.emit(aguievents.NewRunStartedEvent(e.threadID, e.runID))
}

// emitRunFinished ends the run; result carries the session stats when the run
// made at least one LLM request (see docs/ag-ui-output.md).
func (e *aguiEncoder) emitRunFinished(result map[string]any) {
	opts := []aguievents.RunFinishedOption{aguievents.WithSuccessOutcome()}
	if len(result) > 0 {
		opts = append(opts, aguievents.WithResult(result))
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

// streamText emits a text delta, opening the turn's assistant message (and
// closing any open reasoning block, which precedes text) on the first delta.
func (e *aguiEncoder) streamText(delta string) {
	if e.msgID == "" {
		e.msgID = uuid.New().String()
	}
	if e.reasoningOpen {
		e.emit(aguievents.NewReasoningMessageEndEvent(e.msgID))
		e.reasoningOpen = false
	}
	if !e.textOpen {
		e.emit(aguievents.NewTextMessageStartEvent(e.msgID, aguievents.WithRole("assistant")))
		e.textOpen = true
	}
	e.emit(aguievents.NewTextMessageContentEvent(e.msgID, delta))
}

// streamReasoning emits a reasoning delta, opening the turn's reasoning
// message on the first delta. Reasoning always precedes text within one
// assistant message, so reasoning arriving while text is open means a new
// turn started without a completion event (a headless turn that continued
// after draining background notes): close the previous message first.
func (e *aguiEncoder) streamReasoning(delta string) {
	if e.textOpen {
		e.closeMessage()
	}
	if e.msgID == "" {
		e.msgID = uuid.New().String()
	}
	if !e.reasoningOpen {
		e.emit(aguievents.NewReasoningMessageStartEvent(e.msgID, "assistant"))
		e.reasoningOpen = true
	}
	e.emit(aguievents.NewReasoningMessageContentEvent(e.msgID, delta))
}

// emitUserMessage frames a complete user message (role "user") as its own
// AG-UI text message, closing anything the assistant had open first.
func (e *aguiEncoder) emitUserMessage(content string) {
	e.closeMessage()
	id := uuid.New().String()
	e.emit(aguievents.NewTextMessageStartEvent(id, aguievents.WithRole("user")))
	e.emit(aguievents.NewTextMessageContentEvent(id, content))
	e.emit(aguievents.NewTextMessageEndEvent(id))
}

// closeMessage ends any open text/reasoning framing and resets the per-turn
// message ID so the next delta starts a fresh AG-UI message. Safe to call
// when nothing is open.
func (e *aguiEncoder) closeMessage() {
	if e.reasoningOpen {
		e.emit(aguievents.NewReasoningMessageEndEvent(e.msgID))
		e.reasoningOpen = false
	}
	if e.textOpen {
		e.emit(aguievents.NewTextMessageEndEvent(e.msgID))
		e.textOpen = false
	}
	e.msgID = ""
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

func (e *aguiEncoder) emitToolResult(r *agentdomain.ToolExecutionResult) {
	content, _ := json.Marshal(r)
	e.emit(aguievents.NewToolCallResultEvent(uuid.New().String(), r.ToolCallID, string(content)))
}

func (e *aguiEncoder) emitTodos(todos []agentdomain.TodoItem) {
	e.emit(aguievents.NewStateSnapshotEvent(map[string]any{"todos": todos}))
}

// emitQueuedMessage surfaces a note the supervisor landed on the queue (a
// finished background job's result) that CheckingQueue drained into the
// conversation, so a client can render it as its own entry instead of only
// seeing the model's paraphrase.
func (e *aguiEncoder) emitQueuedMessage(content string) {
	e.closeMessage()
	e.emit(aguievents.NewCustomEvent("queued_message",
		aguievents.WithValue(map[string]string{"content": content})))
}

// emitBackgroundTasks publishes the supervisor's job snapshot so a client can
// show a running-task count and list, mirroring the chat TUI's task view.
func (e *aguiEncoder) emitBackgroundTasks(jobs []scheddomain.TrackedJob) {
	running := 0
	list := make([]map[string]any, 0, len(jobs))
	for _, j := range jobs {
		if j.Status == scheddomain.JobRunning {
			running++
		}
		list = append(list, map[string]any{
			"id": j.Meta.ID, "kind": string(j.Meta.Kind), "label": j.Meta.Label, "description": j.Meta.Description,
			"detail": j.Meta.Detail, "status": string(j.Status), "started_at": j.Meta.StartedAt,
		})
	}
	e.emit(aguievents.NewCustomEvent("background_tasks",
		aguievents.WithValue(map[string]any{"running": running, "jobs": list})))
}

// emitAgentStatus reports a local A2A agent's startup state (pulling image,
// starting, waiting for health, ready, failed) before the run's events begin.
func (e *aguiEncoder) emitAgentStatus(name, state, message string, done, total int) {
	e.emit(aguievents.NewCustomEvent("agent_status", aguievents.WithValue(map[string]any{
		"name": name, "state": state, "message": message, "done": done, "total": total,
	})))
}

func (e *aguiEncoder) emitApprovalRequest(req ipc.ApprovalRequest) {
	e.emit(aguievents.NewCustomEvent("approval_request", aguievents.WithValue(req)))
}

func (e *aguiEncoder) emitUserQuestionRequest(req ipc.UserQuestionRequest) {
	e.emit(aguievents.NewCustomEvent("user_question_request", aguievents.WithValue(req)))
}

// emitJudgeVerdict reports the LLM judge's decision for a gated tool call
// as a custom AG-UI event (see judge.yaml).
func (e *aguiEncoder) emitJudgeVerdict(ev agentdomain.JudgeVerdictChatEvent) {
	e.emit(aguievents.NewCustomEvent("judge_verdict", aguievents.WithValue(map[string]any{
		"tool": ev.Tool, "model": ev.Model, "decision": ev.Decision, "reason": ev.Reason, "turn": ev.Turn,
	})))
}

func (e *aguiEncoder) emitComputerUsePaused(reqID string) {
	e.emit(aguievents.NewCustomEvent("computer_use_paused",
		aguievents.WithValue(map[string]string{"request_id": reqID})))
}

func (e *aguiEncoder) emitComputerUseResumed(reqID string) {
	e.emit(aguievents.NewCustomEvent("computer_use_resumed",
		aguievents.WithValue(map[string]string{"request_id": reqID})))
}
