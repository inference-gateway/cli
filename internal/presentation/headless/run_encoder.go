package headless

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	uuid "github.com/google/uuid"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	ipc "github.com/inference-gateway/cli/internal/platform/ipc"
	models "github.com/inference-gateway/cli/internal/platform/models"
	render "github.com/inference-gateway/cli/internal/platform/render"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
)

// Publish maps a chat event to the CUSTOM event its owner publishes for it. It
// reports false for an event it does not own.
type Publish func(event agentdomain.ChatEvent) (agui.CustomEvent, bool)

// RunEncoder renders one agent run on the AG-UI protocol: it maps each
// ChatEvent to the AG-UI events of the run it writes.
type RunEncoder struct {
	run   *agui.Run
	model string

	repo convdomain.ConversationRepository
	jobs func() []scheddomain.TrackedJob

	// history, when non-empty, is the restored conversation Start emits as
	// MESSAGES_SNAPSHOT right after RUN_STARTED.
	history []convdomain.ConversationEntry

	approvals <-chan ipc.ApprovalResponse
	questions <-chan ipc.UserQuestionResponse
	publish   []Publish
}

// NewRunEncoder builds the encoder for one run over w. history, when non-empty,
// is emitted as MESSAGES_SNAPSHOT right after RUN_STARTED, so the run continues
// an existing conversation. approvals and questions broker IPC decisions for
// gated tool calls and questions (nil channels reject or dismiss immediately).
// publish renders the events the encoder does not map itself.
func NewRunEncoder(w io.Writer, model string, repo convdomain.ConversationRepository, history []convdomain.ConversationEntry, jobs func() []scheddomain.TrackedJob, approvals <-chan ipc.ApprovalResponse, questions <-chan ipc.UserQuestionResponse, publish ...Publish) *RunEncoder {
	return &RunEncoder{run: agui.NewRun(w), model: model, repo: repo, history: history, jobs: jobs, approvals: approvals, questions: questions, publish: publish}
}

// Start frames the run: RUN_STARTED, then a MESSAGES_SNAPSHOT of the restored
// history when the run continues an existing conversation.
func (r *RunEncoder) Start(threadID, runID string) {
	r.run.Start(threadID, runID)
	if messages := snapshotMessages(r.history); len(messages) > 0 {
		r.run.Snapshot(messages)
	}
}

func (r *RunEncoder) emitToolResult(result *agentdomain.ToolExecutionResult) {
	content, _ := json.Marshal(result)
	r.run.ToolResult(result.ToolCallID, string(content))
}

// emitQueuedMessage surfaces a note the supervisor landed on the queue (a
// finished background job's result) that CheckingQueue drained into the
// conversation, so a client can render it as its own entry instead of only
// seeing the model's paraphrase.
func (r *RunEncoder) emitQueuedMessage(content string) {
	r.run.CloseMessage()
	r.run.Custom("queued_message", map[string]string{"content": content})
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
	r.run.Custom("background_tasks", map[string]any{"running": running, "jobs": list})
}

// snapshotJobs publishes the background_tasks snapshot so a client's task view
// stays current after a tool result or a drained queue note.
func (r *RunEncoder) snapshotJobs() {
	if r.jobs != nil {
		r.emitBackgroundTasks(r.jobs())
	}
}

// emitJudgeVerdict reports the LLM judge's decision for a gated tool call
// as a custom AG-UI event (see judge.yaml).
func (r *RunEncoder) emitJudgeVerdict(ev agentdomain.JudgeVerdictChatEvent) {
	r.run.Custom("judge_verdict", map[string]any{
		"tool": ev.Tool, "model": ev.Model, "decision": ev.Decision, "reason": ev.Reason, "turn": ev.Turn,
	})
}

// emitPublished renders an event the encoder does not map itself as the CUSTOM
// event its owner publishes for it.
func (r *RunEncoder) emitPublished(event agentdomain.ChatEvent) {
	for _, publish := range r.publish {
		if custom, ok := publish(event); ok {
			r.run.Publish(custom)
			return
		}
	}
}

// Handle renders one agent ChatEvent into the run's AG-UI stream.
//
//nolint:gocyclo,cyclop // cohesive event switch; each case renders one ChatEvent variant
func (r *RunEncoder) Handle(event agentdomain.ChatEvent) {
	switch ev := event.(type) {
	case agentdomain.ChatChunkEvent:
		if ev.ReasoningContent != "" {
			r.run.Reasoning(ev.ReasoningContent)
		}
		if ev.Content != "" {
			r.run.Text(ev.Content)
		}
	case agentdomain.ChatCompleteEvent:
		r.run.CloseMessage()
		for _, tc := range ev.ToolCalls {
			r.run.ToolCall(tc.ID, tc.Function.Name, tc.Function.Arguments)
		}
		// token_usage climbs after each LLM step, so a client's usage readout
		// does not jump at RUN_FINISHED.
		if usage := sessionResult(r.model, r.repo); usage != nil {
			r.run.Custom("token_usage", usage)
		}
		if err := render.CompletionErr(ev); err != nil {
			r.run.Fail(err)
		}
	case agentdomain.ChatErrorEvent:
		r.run.Fail(ev.Error)
	case agentdomain.UserMessageChatEvent:
		r.run.UserMessage(ev.Content)
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
		r.run.State(map[string]any{"todos": ev.Todos})
	case agentdomain.JudgeVerdictChatEvent:
		r.emitJudgeVerdict(ev)
		render.JudgeStderr(ev)
	case agentdomain.ToolApprovalRequestedEvent:
		r.run.Custom("approval_request", ipc.ApprovalRequest{
			Type: "approval_request", ToolName: ev.ToolCall.Function.Name,
			ToolArgs: ev.ToolCall.Function.Arguments, ToolCallID: ev.ToolCall.ID,
		})
		render.AnswerApproval(ev, r.approvals)
	case agentdomain.UserQuestionRequestedEvent:
		r.run.Custom("user_question_request", render.UserQuestionRequest(ev))
		render.AnswerQuestions(ev, r.questions)
	default:
		r.emitPublished(event)
	}
}

// Finish writes the run's one terminal event, RUN_FINISHED carrying the session
// totals or the failure the run ended with. It returns the run error for the
// caller's exit code and telemetry.
func (r *RunEncoder) Finish() error {
	if err := r.run.Finish(sessionResult(r.model, r.repo)); err != nil {
		return fmt.Errorf("agent error: %w", err)
	}
	return nil
}

// sessionResult builds the per-session totals a client consumes from the
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
func snapshotMessages(entries []convdomain.ConversationEntry) []agui.Message {
	messages := make([]agui.Message, 0, len(entries))
	for _, entry := range entries {
		if entry.Hidden {
			continue
		}
		msg := agui.Message{ID: uuid.New().String(), Role: agui.Role(entry.Message.Role)}
		if text, err := entry.Message.Content.AsMessageContent0(); err == nil && text != "" {
			msg.Content = text
		}
		if toolCalls := entry.Message.ToolCalls; toolCalls != nil {
			calls := make([]agui.ToolCall, 0, len(*toolCalls))
			for _, tc := range *toolCalls {
				calls = append(calls, agui.ToolCall{
					ID:       tc.ID,
					Type:     string(tc.Type),
					Function: agui.FunctionCall{Name: tc.Function.Name, Arguments: tc.Function.Arguments},
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

// aguiStartupEmitter reports local A2A agent startup (image pull, container
// start, health check) as agent_status custom events before the run's events
// begin, so a client can show progress instead of silence while a multi-GB
// image pulls.
func aguiStartupEmitter(w io.Writer) func(name, state, message string, done, total int) {
	var mu sync.Mutex
	run := agui.NewRun(w)
	return func(name, state, message string, done, total int) {
		mu.Lock()
		defer mu.Unlock()
		run.Custom("agent_status", map[string]any{
			"name": name, "state": state, "message": message, "done": done, "total": total,
		})
	}
}

// emitAGUIRunError writes a RUN_ERROR event for a failure that happens before
// the run starts (gateway down, unknown model, ...) so stdout consumers see the
// failure instead of silence.
func emitAGUIRunError(w io.Writer, err error) {
	run := agui.NewRun(w)
	run.Fail(errors.New(render.Truncate(err.Error(), 3500)))
	_ = run.Finish(nil)
}

// renderAGUI renders the whole stream as one run of newline-delimited AG-UI
// events: RUN_STARTED, per-turn deltas, tool calls and stats, then Finish's one
// terminal event. ponytail: intermediate job state changes are not published,
// bridge the UI notifier in if a client needs them.
func renderAGUI(events <-chan agentdomain.ChatEvent, w io.Writer, approvals <-chan ipc.ApprovalResponse, questions <-chan ipc.UserQuestionResponse, sessionID, model string, repo convdomain.ConversationRepository, history []convdomain.ConversationEntry, jobs func() []scheddomain.TrackedJob, publish ...Publish) error {
	r := NewRunEncoder(w, model, repo, history, jobs, approvals, questions, publish...)
	r.Start(sessionID, uuid.New().String())
	for event := range events {
		r.Handle(event)
	}
	return r.Finish()
}
