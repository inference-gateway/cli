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

// Publish maps a chat event to the run state or activity its owner publishes
// for it. It reports false for an event it does not own.
type Publish func(event agentdomain.ChatEvent) (agui.Published, bool)

// startupNotes buffers the local agents' boot progress until the first run of
// the process opens: the contract allows ACTIVITY_SNAPSHOT only inside a run,
// so the notes reported before one starts are held and drained into its start.
type startupNotes struct {
	mu    sync.Mutex
	notes []agui.Published
}

func newStartupNotes() *startupNotes { return &startupNotes{} }

// note records one agent's boot progress under its stable activity identity, so
// a client keeps one entry per agent and replaces it as the agent moves along.
func (s *startupNotes) note(name, state, message string, done, total int) {
	note := agui.Published{
		ActivityType: "agent_status",
		MessageID:    "agent:" + name,
		Content:      map[string]any{"name": name, "state": state, "message": message, "done": done, "total": total},
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notes = append(s.notes, note)
}

// drain hands over everything recorded so far and empties the buffer.
func (s *startupNotes) drain() []agui.Published {
	s.mu.Lock()
	defer s.mu.Unlock()
	notes := s.notes
	s.notes = nil
	return notes
}

// Interrupts is the registry a run's interrupts are noted under, so the worker
// answering a resume knows which broker each interrupt belongs to. A nil one
// belongs to an unattended run, where nothing answers the resume.
type Interrupts interface {
	Note(id, reason string)
	Forget(id string)
}

// answerSchema describes the resume payload of an input_required interrupt: the
// AskUserQuestion tool's answers, one entry per question it asked.
var answerSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"answers": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"header":         map[string]any{"type": "string"},
					"question":       map[string]any{"type": "string"},
					"selectedLabels": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"otherText":      map[string]any{"type": "string"},
				},
			},
		},
	},
}

// RunEncoder renders one agent run on the AG-UI protocol: it maps each
// ChatEvent to the AG-UI events of the run it writes, and an approval or a
// question suspends the run with the interrupt outcome and continues on the
// run the answer opens.
type RunEncoder struct {
	writer   io.Writer
	run      *agui.Run
	model    string
	threadID string

	repo convdomain.ConversationRepository
	jobs func() []scheddomain.TrackedJob

	// history, when non-empty, is the restored conversation Start emits as
	// MESSAGES_SNAPSHOT right after RUN_STARTED.
	history []convdomain.ConversationEntry

	approvals <-chan ipc.ApprovalResponse
	questions <-chan ipc.UserQuestionResponse
	startup   *startupNotes
	interrupt Interrupts
	publish   []Publish
}

// RunEncoderDeps are the collaborators of one run's encoder.
type RunEncoderDeps struct {
	Model     string
	Repo      convdomain.ConversationRepository
	History   []convdomain.ConversationEntry
	Jobs      func() []scheddomain.TrackedJob
	Approvals <-chan ipc.ApprovalResponse
	Questions <-chan ipc.UserQuestionResponse
	Startup   *startupNotes
	Interrupt Interrupts
	Publish   []Publish
}

// NewRunEncoder builds the encoder for one run over w. History, when non-empty,
// is emitted as MESSAGES_SNAPSHOT right after RUN_STARTED, so the run continues
// an existing conversation. Approvals and questions broker IPC decisions for
// gated tool calls and questions (nil channels reject or dismiss immediately).
// Startup drains the boot progress the run opens with, and Interrupt notes the
// interrupts the run raises so resume entries find their broker.
func NewRunEncoder(w io.Writer, deps RunEncoderDeps) *RunEncoder {
	return &RunEncoder{
		writer:    w,
		run:       agui.NewRun(w),
		model:     deps.Model,
		repo:      deps.Repo,
		history:   deps.History,
		jobs:      deps.Jobs,
		approvals: deps.Approvals,
		questions: deps.Questions,
		startup:   deps.Startup,
		interrupt: deps.Interrupt,
		publish:   deps.Publish,
	}
}

// Start frames the run: RUN_STARTED, the MESSAGES_SNAPSHOT of the restored
// history when the run continues an existing conversation, and the boot
// progress recorded before the run opened.
func (r *RunEncoder) Start(threadID, runID string) {
	r.threadID = threadID
	r.run.Start(threadID, runID)
	if messages := snapshotMessages(r.history); len(messages) > 0 {
		r.run.Snapshot(messages)
	}
	for _, note := range r.drainStartup() {
		r.run.Activity(note.MessageID, note.ActivityType, note.Content)
	}
}

func (r *RunEncoder) drainStartup() []agui.Published {
	if r.startup == nil {
		return nil
	}
	return r.startup.drain()
}

func (r *RunEncoder) emitToolResult(result *agentdomain.ToolExecutionResult) {
	content, _ := json.Marshal(result)
	r.run.ToolResult(result.ToolCallID, string(content))
}

// emitQueuedMessage surfaces a note the supervisor landed on the queue (a
// finished background job's result) that CheckingQueue drained into the
// conversation, so a client can render it as its own entry with the role the
// note carries in the conversation.
func (r *RunEncoder) emitQueuedMessage(ev agentdomain.MessageQueuedEvent, content string) {
	r.run.Message(agui.Role(ev.Message.Role), content)
}

// emitBackgroundTasks patches the supervisor's job snapshot into the state, so
// a client can show a running-task count and list, mirroring the chat TUI's
// task view.
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
	r.run.PatchState(agui.StateBackgroundTasks, map[string]any{"running": running, "jobs": list})
}

// snapshotJobs patches the backgroundTasks state so a client's task view stays
// current after a tool result or a drained queue note.
func (r *RunEncoder) snapshotJobs() {
	if r.jobs != nil {
		r.emitBackgroundTasks(r.jobs())
	}
}

// emitJudgeVerdict reports the LLM judge's decision for a gated tool call as
// the run's activity (see judge.yaml). The judge's own identity is the stable
// entry, so a repeated verdict replaces it instead of stacking.
func (r *RunEncoder) emitJudgeVerdict(ev agentdomain.JudgeVerdictChatEvent) {
	verdict := map[string]any{
		"tool": ev.Tool, "model": ev.Model, "decision": ev.Decision, "reason": ev.Reason, "turn": ev.Turn,
	}
	r.run.Activity("judge:"+ev.Tool+":"+fmt.Sprint(ev.Turn), "judge_verdict", verdict)
}

// suspendFor ends the open run on the one interrupt it waits on: the terminal
// RUN_FINISHED carries the interrupt for the client to answer with a resume
// entry, and the run this answers belongs to the caller's continuation.
func (r *RunEncoder) suspendFor(interrupt agui.Interrupt) {
	if r.interrupt != nil {
		r.interrupt.Note(interrupt.ID, interrupt.Reason)
	}
	r.run.Suspend([]agui.Interrupt{interrupt})
}

// continueRun swaps in the run that continues the suspended one: its state
// snapshot seeds what the interrupted run carried, so the answer lands in the
// next run with the same state.
func (r *RunEncoder) continueRun(interruptID string) {
	if r.interrupt != nil {
		r.interrupt.Forget(interruptID)
	}
	r.run = agui.NewContinuationRun(r.writer, r.run.State())
	r.run.Start(r.threadID, uuid.New().String())
}

// applyPublished renders one owner-published value in the run: a state key
// patch or an activity entry.
func (r *RunEncoder) applyPublished(published agui.Published) {
	switch {
	case published.StateKey != "":
		r.run.PatchState(published.StateKey, published.StateValue)
	case published.ActivityType != "":
		r.run.Activity(published.MessageID, published.ActivityType, published.Content)
	}
}

// emitPublished renders an event the encoder does not map itself as the value
// its owner publishes for it.
func (r *RunEncoder) emitPublished(event agentdomain.ChatEvent) {
	for _, publish := range r.publish {
		if published, ok := publish(event); ok {
			r.applyPublished(published)
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
		// the usage state climbs after each LLM step, so a client's readout
		// does not jump at the terminal event.
		if usage := sessionUsage(r.model, r.repo); usage != nil {
			r.run.PatchState(agui.StateUsage, usage)
		}
		if err := render.CompletionErr(ev); err != nil {
			r.run.Fail(err)
		}
	case agentdomain.ChatErrorEvent:
		r.run.Fail(ev.Error)
	case agentdomain.UserMessageChatEvent:
		r.run.Message("user", ev.Content)
	case agentdomain.ToolExecutionCompletedEvent:
		for _, result := range ev.Results {
			if result != nil {
				r.emitToolResult(result)
			}
		}
		r.snapshotJobs()
	case agentdomain.MessageQueuedEvent:
		if note, ok := render.QueuedNote(ev); ok {
			r.emitQueuedMessage(ev, note)
		}
		r.snapshotJobs()
	case agentdomain.TodoUpdateChatEvent:
		r.run.PatchState(agui.StateTodos, ev.Todos)
	case agentdomain.JudgeVerdictChatEvent:
		r.emitJudgeVerdict(ev)
		render.JudgeStderr(ev)
	case agentdomain.ToolApprovalRequestedEvent:
		r.suspendFor(agui.Interrupt{ID: ev.ToolCall.ID, Reason: agui.InterruptToolCall, ToolCallID: ev.ToolCall.ID})
		render.AnswerApproval(ev, r.approvals)
		r.continueRun(ev.ToolCall.ID)
	case agentdomain.UserQuestionRequestedEvent:
		r.suspendFor(agui.Interrupt{ID: ev.ToolCallID, Reason: agui.InterruptInputRequired, ResponseSchema: answerSchema})
		render.AnswerQuestions(ev, r.questions)
		r.continueRun(ev.ToolCallID)
	default:
		r.emitPublished(event)
	}
}

// Finish writes the current run's one terminal event, RUN_FINISHED carrying the
// usage and the session extras or the failure the run ended with. It returns
// the run error for the caller's exit code and telemetry.
func (r *RunEncoder) Finish() error {
	if err := r.run.Finish(sessionExtras(r.model, r.repo), sessionUsage(r.model, r.repo)); err != nil {
		return fmt.Errorf("agent error: %w", err)
	}
	return nil
}

// sessionUsage builds the per-session token totals as the usage list the
// terminal event carries, nil when the run made no LLM requests (e.g. a
// shortcut answer), so no terminal event carries usage.
func sessionUsage(model string, repo convdomain.ConversationRepository) []agui.TokenUsage {
	tokens, ok := sessionTokens(repo)
	if !ok {
		return nil
	}
	usage := agui.TokenUsage{
		Model:        model,
		InputTokens:  agui.TokenCount(int64(tokens.TotalInputTokens)),
		OutputTokens: agui.TokenCount(int64(tokens.TotalOutputTokens)),
	}
	if tokens.TotalCachedTokens > 0 {
		usage.CachedInputTokens = agui.TokenCount(int64(tokens.TotalCachedTokens))
	}
	return []agui.TokenUsage{usage}
}

// sessionExtras builds what the terminal event's result keeps on top of usage:
// the session cost, the model's context window and its total tool calls. nil
// when the run made no LLM requests, so the terminal event carries no result.
func sessionExtras(model string, repo convdomain.ConversationRepository) map[string]any {
	_, ok := sessionTokens(repo)
	if !ok {
		return nil
	}
	result := map[string]any{
		"totalToolCalls": countToolCalls(repo.GetMessages()),
		"cost":           repo.GetSessionCostStats().TotalCost,
	}
	if window, ok := models.LookupContextWindow(model); ok {
		result["contextWindow"] = window
	}
	return result
}

func sessionTokens(repo convdomain.ConversationRepository) (convdomain.SessionTokenStats, bool) {
	if repo == nil {
		return convdomain.SessionTokenStats{}, false
	}
	tokens := repo.GetSessionTokens()
	return tokens, tokens.RequestCount > 0
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

// startupEmitter reports local A2A agent startup (image pull, container start,
// health check) as the run's agent_status activity entries, buffered until the
// first run opens, so a client can show progress instead of silence while a
// multi-GB image pulls.
func startupEmitter(notes *startupNotes) func(name, state, message string, done, total int) {
	return notes.note
}

// emitAGUIRunError writes a RUN_ERROR event for a failure that happens before
// the run starts (gateway down, unknown model, ...) so stdout consumers see the
// failure instead of silence.
func emitAGUIRunError(w io.Writer, err error) {
	r := agui.NewRun(w)
	r.Fail(errors.New(render.Truncate(err.Error(), 3500)))
	_ = r.Finish(nil, nil)
}

// renderAGUI renders the whole stream as one run of newline-delimited AG-UI
// events: RUN_STARTED, per-turn deltas, tool calls and stats, then Finish's one
// terminal event. ponytail: intermediate job state changes are not published,
// bridge the UI notifier in if a client needs them.
func renderAGUI(events <-chan agentdomain.ChatEvent, w io.Writer, approvals <-chan ipc.ApprovalResponse, questions <-chan ipc.UserQuestionResponse, sessionID, model string, repo convdomain.ConversationRepository, history []convdomain.ConversationEntry, jobs func() []scheddomain.TrackedJob, startup *startupNotes, publish ...Publish) error {
	r := NewRunEncoder(w, RunEncoderDeps{
		Model: model, Repo: repo, History: history, Jobs: jobs,
		Approvals: approvals, Questions: questions, Startup: startup, Publish: publish,
	})
	r.Start(sessionID, uuid.New().String())
	for event := range events {
		r.Handle(event)
	}
	return r.Finish()
}
