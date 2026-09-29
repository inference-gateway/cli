package agui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	aguitypes "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	uuid "github.com/google/uuid"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	ipc "github.com/inference-gateway/cli/internal/platform/ipc"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	models "github.com/inference-gateway/cli/internal/platform/models"
	render "github.com/inference-gateway/cli/internal/platform/render"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
)

// RunEncoder renders one AG-UI run: RUN_STARTED at Start, AG-UI events per
// ChatEvent, and exactly one terminal RUN_FINISHED or RUN_ERROR at Finish.
// Every event is written with exactly one Write, so a transport can map one
// Write to one frame.
type RunEncoder struct {
	w     io.Writer
	model string

	repo convdomain.ConversationRepository
	jobs func() []scheddomain.TrackedJob

	// history, when non-empty, is the restored conversation Start emits as
	// MESSAGES_SNAPSHOT right after RUN_STARTED.
	history []convdomain.ConversationEntry

	approvals <-chan ipc.ApprovalResponse
	questions <-chan ipc.UserQuestionResponse

	threadID string
	runID    string

	msgID         string
	textOpen      bool
	reasoningOpen bool

	runErr error
}

// NewRunEncoder builds the encoder for one run over w. history, when non-empty,
// is emitted as MESSAGES_SNAPSHOT right after RUN_STARTED, so the run continues
// an existing conversation. approvals and questions broker IPC decisions for
// gated tool calls and questions (nil channels reject or dismiss immediately).
func NewRunEncoder(w io.Writer, model string, repo convdomain.ConversationRepository, history []convdomain.ConversationEntry, jobs func() []scheddomain.TrackedJob, approvals <-chan ipc.ApprovalResponse, questions <-chan ipc.UserQuestionResponse) *RunEncoder {
	return &RunEncoder{w: w, model: model, repo: repo, history: history, jobs: jobs, approvals: approvals, questions: questions}
}

func (r *RunEncoder) emit(ev aguievents.Event) {
	data, err := ev.ToJSON()
	if err != nil {
		logger.Error("failed to marshal AG-UI event", "error", err, "type", ev.Type())
		return
	}
	if _, err := r.w.Write(append(data, '\n')); err != nil {
		logger.Error("failed to write AG-UI event", "error", err)
	}
}

// Start frames the run: RUN_STARTED, then a MESSAGES_SNAPSHOT of the restored
// history when the run continues an existing conversation.
func (r *RunEncoder) Start(threadID, runID string) {
	r.threadID = threadID
	r.runID = runID
	r.emit(aguievents.NewRunStartedEvent(threadID, runID))
	if messages := snapshotMessages(r.history); len(messages) > 0 {
		r.emit(aguievents.NewMessagesSnapshotEvent(messages))
	}
}

// streamText emits a text delta, opening the turn's assistant message (and
// closing any open reasoning block, which precedes text) on the first delta.
func (r *RunEncoder) streamText(delta string) {
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

// streamReasoning emits a reasoning delta, opening the turn's reasoning
// message on the first delta. Reasoning always precedes text within one
// assistant message, so reasoning arriving while text is open means a new
// turn started without a completion event (a headless turn that continued
// after draining background notes): close the previous message first.
func (r *RunEncoder) streamReasoning(delta string) {
	if r.textOpen {
		r.closeMessage()
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

// emitUserMessage frames a complete user message (role "user") as its own
// AG-UI text message, closing anything the assistant had open first.
func (r *RunEncoder) emitUserMessage(content string) {
	r.closeMessage()
	id := uuid.New().String()
	r.emit(aguievents.NewTextMessageStartEvent(id, aguievents.WithRole("user")))
	r.emit(aguievents.NewTextMessageContentEvent(id, content))
	r.emit(aguievents.NewTextMessageEndEvent(id))
}

// closeMessage ends any open text/reasoning framing and resets the per-turn
// message ID so the next delta starts a fresh AG-UI message. Safe to call
// when nothing is open.
func (r *RunEncoder) closeMessage() {
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

func (r *RunEncoder) emitToolCallStart(id, name string) {
	r.emit(aguievents.NewToolCallStartEvent(id, name))
}

func (r *RunEncoder) emitToolCallArgs(id, args string) {
	if args != "" {
		r.emit(aguievents.NewToolCallArgsEvent(id, args))
	}
}

func (r *RunEncoder) emitToolCallEnd(id string) {
	r.emit(aguievents.NewToolCallEndEvent(id))
}

func (r *RunEncoder) emitToolResult(result *agentdomain.ToolExecutionResult) {
	content, _ := json.Marshal(result)
	r.emit(aguievents.NewToolCallResultEvent(uuid.New().String(), result.ToolCallID, string(content)))
}

func (r *RunEncoder) emitTodos(todos []agentdomain.TodoItem) {
	r.emit(aguievents.NewStateSnapshotEvent(map[string]any{"todos": todos}))
}

// emitQueuedMessage surfaces a note the supervisor landed on the queue (a
// finished background job's result) that CheckingQueue drained into the
// conversation, so a client can render it as its own entry instead of only
// seeing the model's paraphrase.
func (r *RunEncoder) emitQueuedMessage(content string) {
	r.closeMessage()
	r.emit(aguievents.NewCustomEvent("queued_message",
		aguievents.WithValue(map[string]string{"content": content})))
}

// emitBackgroundTasks publishes the supervisor's job snapshot so a client can
// show a running-task count and list, mirroring the chat TUI's task view.
func (r *RunEncoder) emitBackgroundTasks(jobs []scheddomain.TrackedJob) {
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
	r.emit(aguievents.NewCustomEvent("background_tasks",
		aguievents.WithValue(map[string]any{"running": running, "jobs": list})))
}

// snapshotJobs publishes the background_tasks snapshot so a client's task view
// stays current after a tool result or a drained queue note.
func (r *RunEncoder) snapshotJobs() {
	if r.jobs != nil {
		r.emitBackgroundTasks(r.jobs())
	}
}

// emitTokenUsage publishes the session's cumulative stats after each LLM step
// so a client's usage readout (the desktop status bar) climbs during the run
// instead of jumping at RUN_FINISHED. The value is sessionResult's object.
func (r *RunEncoder) emitTokenUsage(value map[string]any) {
	r.emit(aguievents.NewCustomEvent("token_usage", aguievents.WithValue(value)))
}

// emitAgentStatus reports a local A2A agent's startup state (pulling image,
// starting, waiting for health, ready, failed) before the run's events begin.
func (r *RunEncoder) emitAgentStatus(name, state, message string, done, total int) {
	r.emit(aguievents.NewCustomEvent("agent_status", aguievents.WithValue(map[string]any{
		"name": name, "state": state, "message": message, "done": done, "total": total,
	})))
}

func (r *RunEncoder) emitApprovalRequest(req ipc.ApprovalRequest) {
	r.emit(aguievents.NewCustomEvent("approval_request", aguievents.WithValue(req)))
}

func (r *RunEncoder) emitUserQuestionRequest(req ipc.UserQuestionRequest) {
	r.emit(aguievents.NewCustomEvent("user_question_request", aguievents.WithValue(req)))
}

// emitJudgeVerdict reports the LLM judge's decision for a gated tool call
// as a custom AG-UI event (see judge.yaml).
func (r *RunEncoder) emitJudgeVerdict(ev agentdomain.JudgeVerdictChatEvent) {
	r.emit(aguievents.NewCustomEvent("judge_verdict", aguievents.WithValue(map[string]any{
		"tool": ev.Tool, "model": ev.Model, "decision": ev.Decision, "reason": ev.Reason, "turn": ev.Turn,
	})))
}

func (r *RunEncoder) emitComputerUsePaused(reqID string) {
	r.emit(aguievents.NewCustomEvent("computer_use_paused",
		aguievents.WithValue(map[string]string{"request_id": reqID})))
}

func (r *RunEncoder) emitComputerUseResumed(reqID string) {
	r.emit(aguievents.NewCustomEvent("computer_use_resumed",
		aguievents.WithValue(map[string]string{"request_id": reqID})))
}

// emitScreenRecording reports a RecordStart recording starting or its ffmpeg
// process ending, so a client can show a recording indicator.
func (r *RunEncoder) emitScreenRecording(active bool) {
	r.emit(aguievents.NewCustomEvent("screen_recording",
		aguievents.WithValue(map[string]bool{"active": active})))
}

// emitRunError ends a failed run. runID stays empty for a failure before the
// run started (see EmitRunError), so the event carries no run id then.
func (r *RunEncoder) emitRunError(message string) {
	opts := []aguievents.RunErrorOption{}
	if r.runID != "" {
		opts = append(opts, aguievents.WithRunID(r.runID))
	}
	r.emit(aguievents.NewRunErrorEvent(message, opts...))
}

// Handle renders one agent ChatEvent into the run's AG-UI stream.
//
//nolint:gocyclo,cyclop // cohesive event switch; each case renders one ChatEvent variant
func (r *RunEncoder) Handle(event agentdomain.ChatEvent) {
	switch ev := event.(type) {
	case agentdomain.ChatChunkEvent:
		if ev.ReasoningContent != "" {
			r.streamReasoning(ev.ReasoningContent)
		}
		if ev.Content != "" {
			r.streamText(ev.Content)
		}
	case agentdomain.ChatCompleteEvent:
		r.closeMessage()
		for _, tc := range ev.ToolCalls {
			r.emitToolCallStart(tc.ID, tc.Function.Name)
			r.emitToolCallArgs(tc.ID, tc.Function.Arguments)
			r.emitToolCallEnd(tc.ID)
		}
		if usage := sessionResult(r.model, r.repo); usage != nil {
			r.emitTokenUsage(usage)
		}
		if err := render.CompletionErr(ev); err != nil {
			r.runErr = err
		}
	case agentdomain.ChatErrorEvent:
		r.runErr = ev.Error
	case agentdomain.UserMessageChatEvent:
		r.emitUserMessage(ev.Content)
	case agentdomain.ToolExecutionCompletedEvent:
		for _, result := range ev.Results {
			if result != nil {
				r.emitToolResult(result)
			}
		}
		r.snapshotJobs()
	case agentdomain.MessageQueuedEvent:
		if note, ok := render.QueuedNote(ev); ok {
			r.emitQueuedMessage(note)
		}
		r.snapshotJobs()
	case agentdomain.TodoUpdateChatEvent:
		r.emitTodos(ev.Todos)
	case agentdomain.JudgeVerdictChatEvent:
		r.emitJudgeVerdict(ev)
		render.JudgeStderr(ev)
	case agentdomain.ToolApprovalRequestedEvent:
		r.emitApprovalRequest(ipc.ApprovalRequest{
			Type: "approval_request", ToolName: ev.ToolCall.Function.Name,
			ToolArgs: ev.ToolCall.Function.Arguments, ToolCallID: ev.ToolCall.ID,
		})
		render.AnswerApproval(ev, r.approvals)
	case agentdomain.UserQuestionRequestedEvent:
		r.emitUserQuestionRequest(render.UserQuestionRequest(ev))
		render.AnswerQuestions(ev, r.questions)
	case agentdomain.ComputerUsePausedEvent:
		r.emitComputerUsePaused(ev.RequestID)
	case agentdomain.ComputerUseResumedEvent:
		r.emitComputerUseResumed(ev.RequestID)
		r.runErr = nil
	case agentdomain.ScreenRecordingStatusEvent:
		r.emitScreenRecording(ev.Active)
	}
}

// Finish closes open framing and writes the run's one terminal event: RUN_FINISHED
// with outcome success, or outcome cancelled for a stopped turn, or RUN_ERROR when
// the run failed. It returns the run error for the caller's exit code and telemetry.
func (r *RunEncoder) Finish() error {
	r.closeMessage()
	switch {
	case errors.Is(r.runErr, context.Canceled):
		r.emit(aguievents.NewRunFinishedEventWithOptions(r.threadID, r.runID,
			aguievents.WithOutcome(aguievents.RunFinishedOutcome{Type: "cancelled"})))
	case r.runErr != nil:
		r.emitRunError(r.runErr.Error())
	default:
		opts := []aguievents.RunFinishedOption{aguievents.WithSuccessOutcome()}
		if result := sessionResult(r.model, r.repo); len(result) > 0 {
			opts = append(opts, aguievents.WithResult(result))
		}
		r.emit(aguievents.NewRunFinishedEventWithOptions(r.threadID, r.runID, opts...))
	}
	if err := r.runErr; err != nil {
		return fmt.Errorf("agent error: %w", err)
	}
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

// snapshotMessages maps the conversation entries restored for the run to the
// AG-UI messages of a MESSAGES_SNAPSHOT: id, role, text content, assistant
// toolCalls, tool toolCallId, and error when the tool execution failed.
func snapshotMessages(entries []convdomain.ConversationEntry) []aguitypes.Message {
	messages := make([]aguitypes.Message, 0, len(entries))
	for _, entry := range entries {
		if entry.Hidden {
			continue
		}
		msg := aguitypes.Message{ID: uuid.New().String(), Role: aguitypes.Role(entry.Message.Role)}
		if text, err := entry.Message.Content.AsMessageContent0(); err == nil && text != "" {
			msg.Content = text
		}
		if toolCalls := entry.Message.ToolCalls; toolCalls != nil {
			calls := make([]aguitypes.ToolCall, 0, len(*toolCalls))
			for _, tc := range *toolCalls {
				calls = append(calls, aguitypes.ToolCall{
					ID:       tc.ID,
					Type:     string(tc.Type),
					Function: aguitypes.FunctionCall{Name: tc.Function.Name, Arguments: tc.Function.Arguments},
				})
			}
			msg.ToolCalls = calls
		}
		if entry.Message.ToolCallID != nil {
			msg.ToolCallID = *entry.Message.ToolCallID
			if entry.ToolExecution != nil && !entry.ToolExecution.Success {
				msg.Error = entry.ToolExecution.Error
			}
		}
		messages = append(messages, msg)
	}
	return messages
}

// AgentStartupEmitter reports local A2A agent startup (image pull, container
// start, health check) as agent_status custom events before the run's events
// begin, so a client can show progress instead of silence while a multi-GB
// image pulls.
func AgentStartupEmitter(w io.Writer) func(name, state, message string, done, total int) {
	var mu sync.Mutex
	e := &RunEncoder{w: w}
	return func(name, state, message string, done, total int) {
		mu.Lock()
		defer mu.Unlock()
		e.emitAgentStatus(name, state, message, done, total)
	}
}

// EmitRunError writes a RUN_ERROR event for a failure that happens before the
// run starts (gateway down, unknown model, ...) so stdout consumers see the
// failure instead of silence.
func EmitRunError(w io.Writer, err error) {
	(&RunEncoder{w: w}).emitRunError(render.Truncate(err.Error(), 3500))
}

// Render renders the whole stream as one run of newline-delimited AG-UI events:
// RUN_STARTED, per-turn deltas, tool calls and stats, then Finish's one terminal
// event. approvals and questions broker IPC answers and jobs publishes
// background_tasks snapshots. ponytail: intermediate job state changes are not
// published, bridge the UI notifier in if a client needs them.
func Render(events <-chan agentdomain.ChatEvent, w io.Writer, approvals <-chan ipc.ApprovalResponse, questions <-chan ipc.UserQuestionResponse, sessionID, model string, repo convdomain.ConversationRepository, jobs func() []scheddomain.TrackedJob) error {
	r := NewRunEncoder(w, model, repo, nil, jobs, approvals, questions)
	r.Start(sessionID, uuid.New().String())
	for event := range events {
		r.Handle(event)
	}
	return r.Finish()
}
