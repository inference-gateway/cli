package agui

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	uuid "github.com/google/uuid"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	ipc "github.com/inference-gateway/cli/internal/platform/ipc"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	models "github.com/inference-gateway/cli/internal/platform/models"
	render "github.com/inference-gateway/cli/internal/platform/render"
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

// emitTokenUsage publishes the session's cumulative stats after each LLM step
// so a client's usage readout (the desktop status bar) climbs during the run
// instead of jumping at RUN_FINISHED; value is sessionResult's object.
func (e *aguiEncoder) emitTokenUsage(value map[string]any) {
	e.emit(aguievents.NewCustomEvent("token_usage", aguievents.WithValue(value)))
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

// emitScreenRecording reports a RecordStart recording starting or its ffmpeg
// process ending, so a client can show a recording indicator.
func (e *aguiEncoder) emitScreenRecording(active bool) {
	e.emit(aguievents.NewCustomEvent("screen_recording",
		aguievents.WithValue(map[string]bool{"active": active})))
}

// RenderAGUI renders events as newline-delimited AG-UI protocol events. The
// run gets exactly one RUN_STARTED and one terminal event (RUN_FINISHED or
// RUN_ERROR); per-turn events in between carry deltas, tool calls, and results.
// When approvals is non-nil it acts as the IPC approval broker, same as
// RenderJSON. A ComputerUseResumedEvent clears any error carried over from
// the paused (cancelled) run, same as RenderJSON. After the channel closes the
// session stats from repo are attached to RUN_FINISHED's result, the AG-UI
// counterpart of RenderJSON's session_stats line. After each LLM step the same
// cumulative stats are also published as a token_usage CUSTOM event, so clients
// track usage mid-run (the desktop status bar) instead of only at the end.
// RenderAGUI streams the run as AG-UI events. jobs, when non-nil, is the
// supervisor's snapshot and is published as a background_tasks event after every
// tool result (a job may have been submitted) and every drained queue note (a
// job just finished). ponytail: intermediate job state changes are not
// published; bridge the UI notifier into the chat stream if a client needs them.
//
//nolint:gocyclo,cyclop // cohesive event switch; each case renders one ChatEvent variant
func RenderAGUI(events <-chan agentdomain.ChatEvent, w io.Writer, approvals <-chan ipc.ApprovalResponse, questions <-chan ipc.UserQuestionResponse, sessionID, model string, repo convdomain.ConversationRepository, jobs func() []scheddomain.TrackedJob) error {
	e := &aguiEncoder{w: w, threadID: sessionID}
	e.emitRunStarted(sessionID)
	snapshot := func() {
		if jobs != nil {
			e.emitBackgroundTasks(jobs())
		}
	}

	var runErr error
	for event := range events {
		switch ev := event.(type) {
		case agentdomain.ChatChunkEvent:
			if ev.ReasoningContent != "" {
				e.streamReasoning(ev.ReasoningContent)
			}
			if ev.Content != "" {
				e.streamText(ev.Content)
			}
		case agentdomain.ChatCompleteEvent:
			e.closeMessage()
			for _, tc := range ev.ToolCalls {
				e.emitToolCallStart(tc.ID, tc.Function.Name)
				e.emitToolCallArgs(tc.ID, tc.Function.Arguments)
				e.emitToolCallEnd(tc.ID)
			}
			if usage := sessionResult(model, repo); usage != nil {
				e.emitTokenUsage(usage)
			}
			if err := render.CompletionErr(ev); err != nil {
				runErr = err
			}
		case agentdomain.ChatErrorEvent:
			runErr = ev.Error
		case agentdomain.UserMessageChatEvent:
			e.emitUserMessage(ev.Content)
		case agentdomain.ToolExecutionCompletedEvent:
			for _, r := range ev.Results {
				if r != nil {
					e.emitToolResult(r)
				}
			}
			snapshot()
		case agentdomain.MessageQueuedEvent:
			if note, ok := render.QueuedNote(ev); ok {
				e.emitQueuedMessage(note)
			}
			snapshot()
		case agentdomain.TodoUpdateChatEvent:
			e.emitTodos(ev.Todos)
		case agentdomain.JudgeVerdictChatEvent:
			e.emitJudgeVerdict(ev)
			render.JudgeStderr(ev)
		case agentdomain.ToolApprovalRequestedEvent:
			e.emitApprovalRequest(ipc.ApprovalRequest{
				Type: "approval_request", ToolName: ev.ToolCall.Function.Name,
				ToolArgs: ev.ToolCall.Function.Arguments, ToolCallID: ev.ToolCall.ID,
			})
			render.AnswerApproval(ev, approvals)
		case agentdomain.UserQuestionRequestedEvent:
			e.emitUserQuestionRequest(render.UserQuestionRequest(ev))
			render.AnswerQuestions(ev, questions)
		case agentdomain.ComputerUsePausedEvent:
			e.emitComputerUsePaused(ev.RequestID)
		case agentdomain.ComputerUseResumedEvent:
			e.emitComputerUseResumed(ev.RequestID)
			runErr = nil
		case agentdomain.ScreenRecordingStatusEvent:
			e.emitScreenRecording(ev.Active)
		}
	}

	e.closeMessage()
	if runErr != nil {
		e.emitRunError(runErr.Error())
		return fmt.Errorf("agent error: %w", runErr)
	}
	e.emitRunFinished(sessionResult(model, repo))
	return nil
}

// sessionResult builds the per-session totals the desktop consumes from the
// per-step token_usage CUSTOM events and the terminal RUN_FINISHED event (see
// docs/ag-ui-output.md), mirroring the session_stats line of RenderJSON. nil
// when the run made no LLM requests (e.g. a shortcut answer), so no event
// carries a result.
func sessionResult(model string, repo convdomain.ConversationRepository) map[string]any {
	if repo == nil {
		return nil
	}
	tokens := repo.GetSessionTokens()
	if tokens.RequestCount <= 0 {
		return nil
	}
	result := map[string]any{
		"inputTokens":     tokens.TotalInputTokens,
		"outputTokens":    tokens.TotalOutputTokens,
		"cacheReadTokens": tokens.TotalCachedTokens,
		"totalToolCalls":  countToolCalls(repo.GetMessages()),
		"cost":            repo.GetSessionCostStats().TotalCost,
		"lastInputTokens": tokens.LastInputTokens,
	}
	if window, ok := models.LookupContextWindow(model); ok {
		result["contextWindow"] = window
	}
	return result
}

// countToolCalls sums the tool calls recorded on the session's assistant messages.
func countToolCalls(entries []convdomain.ConversationEntry) int {
	count := 0
	for _, e := range entries {
		if e.Message.ToolCalls != nil {
			count += len(*e.Message.ToolCalls)
		}
	}
	return count
}

// AgentStartupEmitter reports local A2A agent startup (image pull, container
// start, health check) as agent_status custom events before the run's events
// begin, so a client can show progress instead of silence while a multi-GB
// image pulls.
func AgentStartupEmitter(w io.Writer) func(name, state, message string, done, total int) {
	var mu sync.Mutex
	e := &aguiEncoder{w: w}
	return func(name, state, message string, done, total int) {
		mu.Lock()
		defer mu.Unlock()
		e.emitAgentStatus(name, state, message, done, total)
	}
}

// EmitRunError writes a RUN_ERROR event for a failure that happens before the
// run starts (gateway down, unknown model, ...) so stdout consumers see the
// failure instead of silence.
func EmitRunError(w io.Writer, message string) {
	(&aguiEncoder{w: w}).emitRunError(message)
}
