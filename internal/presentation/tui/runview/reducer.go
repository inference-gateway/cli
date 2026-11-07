package runview

import (
	"cmp"
	"encoding/json"
	"errors"
	"strings"
	"time"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
)

// End is where a reduced frame leaves the run a Reducer follows.
type End int

const (
	Open End = iota
	Resumed
	Suspended
	Finished
	Cancelled
	Failed
)

// Prompt is one interrupt a suspended run waits on, as the event the chat TUI
// answers on its ResponseChan. Event is nil for an interrupt of an unknown
// reason, which the client can only cancel.
type Prompt struct {
	InterruptID string
	Event       agentdomain.ChatEvent
}

// SessionUsageEvent is a usage patch read back: the token totals of the run's
// session after the model step the patch closes. A chat whose agent runs
// elsewhere mirrors them into its repository, where the status bar reads them,
// so no handler renders it.
type SessionUsageEvent struct {
	agentdomain.BaseChatEvent
	Model        string
	InputTokens  int
	OutputTokens int
	CachedTokens int
}

// Reducer folds the AG-UI events of a thread's runs into the chat events the
// TUI handlers render: deltas become chunks, a model step its completion and
// an interrupt the prompt the user answers. It follows one client request
// across a suspended run and its continuations, and tolerates attaching mid-run.
type Reducer struct {
	requestID string
	started   bool

	roles map[string]string
	notes map[string]*strings.Builder

	text      strings.Builder
	reasoning strings.Builder
	calls     []*streamedCall
	known     map[string]*streamedCall
	results   []*agentdomain.ToolExecutionResult
	prompts   []Prompt
	// held is the completion of a step that handed no calls to tools. It
	// ends the client's turn, so it waits for the run to finish, unless a
	// queued message continues the run first.
	held *agentdomain.ChatCompleteEvent
}

// streamedCall is a tool call the run streamed: its name and its arguments.
type streamedCall struct {
	id   string
	name string
	args strings.Builder
}

// New builds the reducer for the client request requestID, the id every
// event it returns carries.
func New(requestID string) *Reducer {
	return &Reducer{
		requestID: requestID,
		roles:     make(map[string]string),
		notes:     make(map[string]*strings.Builder),
		known:     make(map[string]*streamedCall),
	}
}

// Decode reduces one frame into the events it stands for and reports where it
// leaves the run. A frame that is not an AG-UI event, or one outside a run's
// vocabulary, reduces to nothing.
func (r *Reducer) Decode(frame []byte) ([]agentdomain.ChatEvent, End) {
	event, err := agui.Decode(frame)
	if err != nil {
		return nil, Open
	}
	return r.Reduce(event)
}

// Reduce folds one typed event into the run and returns the chat events it
// completes, with where it leaves the run.
//
//nolint:gocyclo,cyclop // cohesive event switch; each case reduces one AG-UI event type
func (r *Reducer) Reduce(event agui.Event) ([]agentdomain.ChatEvent, End) {
	switch e := event.(type) {
	case *agui.RunStartedEvent:
		return r.runStarted()
	case *agui.TextMessageStartEvent:
		if e.Role != nil {
			r.roles[e.MessageID] = *e.Role
		}
	case *agui.TextMessageContentEvent:
		return r.textContent(e.MessageID, e.Delta), Open
	case *agui.TextMessageEndEvent:
		return r.textEnd(e.MessageID), Open
	case *agui.ReasoningMessageContentEvent:
		r.reasoning.WriteString(e.Delta)
		return r.withResults(agentdomain.ChatChunkEvent{RequestID: r.requestID, Timestamp: time.Now(), ReasoningContent: e.Delta}), Open
	case *agui.ToolCallStartEvent:
		events := r.withResults()
		r.call(e.ToolCallID).name = e.ToolCallName
		return events, Open
	case *agui.ToolCallArgsEvent:
		r.call(e.ToolCallID).args.WriteString(e.Delta)
	case *agui.ToolCallResultEvent:
		return r.toolResult(e.Content), Open
	case *agui.StateSnapshotEvent:
		var snapshot struct {
			Todos []agentdomain.TodoItem `json:"todos"`
		}
		if recode(e.Snapshot, &snapshot) && len(snapshot.Todos) > 0 {
			return []agentdomain.ChatEvent{r.todos(snapshot.Todos)}, Open
		}
	case *agui.StateDeltaEvent:
		return r.stateDelta(e), Open
	case *agui.ActivitySnapshotEvent:
		if activity := r.activity(e.ActivityType, e.Content); activity != nil {
			return append(r.closeStep(), r.withResults(activity)...), Open
		}
	case *agui.RunFinishedEvent:
		if e.Outcome == nil {
			return r.runFinished("", nil)
		}
		return r.runFinished(string(e.Outcome.Type), e.Outcome.Interrupts)
	case *agui.RunErrorEvent:
		events := r.releaseHeld(r.closeStep())
		return append(events, agentdomain.ChatErrorEvent{RequestID: r.requestID, Timestamp: time.Now(), Error: errors.New(e.Message)}), Failed
	}
	return nil, Open
}

// Prompts are the interrupts the run suspended on, valid after Decode reported
// Suspended.
func (r *Reducer) Prompts() []Prompt {
	return r.prompts
}

// runStarted opens the request on its first run. A later run is the
// continuation of a suspended one, whose prompts someone answered.
func (r *Reducer) runStarted() ([]agentdomain.ChatEvent, End) {
	r.prompts = nil
	r.held = nil
	if r.started {
		return nil, Resumed
	}
	r.started = true
	return []agentdomain.ChatEvent{agentdomain.ChatStartEvent{RequestID: r.requestID, Timestamp: time.Now()}}, Open
}

// textContent streams an assistant delta, or keeps the text of a message
// with another role until it ends. A delta whose message never started, as
// when the client attached mid-message, is the assistant's.
func (r *Reducer) textContent(messageID, delta string) []agentdomain.ChatEvent {
	if role := r.roles[messageID]; role != "" && role != string(sdk.Assistant) {
		note := r.notes[messageID]
		if note == nil {
			note = &strings.Builder{}
			r.notes[messageID] = note
		}
		note.WriteString(delta)
		return nil
	}
	r.text.WriteString(delta)
	return r.withResults(agentdomain.ChatChunkEvent{RequestID: r.requestID, Timestamp: time.Now(), Content: delta})
}

// textEnd turns a finished message with a role other than the assistant's,
// a queued user message or a background job's note, into the queued event the
// chat TUI renders it from.
func (r *Reducer) textEnd(messageID string) []agentdomain.ChatEvent {
	role := r.roles[messageID]
	note := r.notes[messageID]
	delete(r.roles, messageID)
	delete(r.notes, messageID)
	if note == nil {
		return nil
	}
	message := sdk.Message{Role: sdk.MessageRole(role), Content: sdk.NewMessageContent(note.String())}
	return r.withResults(agentdomain.MessageQueuedEvent{RequestID: r.requestID, Timestamp: time.Now(), Message: message})
}

// call is the tool call with id, buffered for the step it belongs to.
func (r *Reducer) call(id string) *streamedCall {
	if c := r.known[id]; c != nil {
		return c
	}
	c := &streamedCall{id: id}
	r.known[id] = c
	r.calls = append(r.calls, c)
	return c
}

// toolResult keeps one executed call's result for the completion event the
// step's results go out as. A result closes the model's step if it is open.
func (r *Reducer) toolResult(content string) []agentdomain.ChatEvent {
	var result agentdomain.ToolExecutionResult
	if json.Unmarshal([]byte(content), &result) != nil {
		return nil
	}
	events := r.closeStep()
	r.results = append(r.results, &result)
	return events
}

// stateDelta reduces the state patches the chat TUI renders: the usage patch
// that closes a model step, the todos and the background job patch that
// follows a step's tool results.
func (r *Reducer) stateDelta(delta *agui.StateDeltaEvent) []agentdomain.ChatEvent {
	var events []agentdomain.ChatEvent
	for _, patch := range delta.Delta {
		switch patch.Path {
		case "/" + agui.StateUsage:
			events = append(events, r.closeStep()...)
			events = append(events, r.sessionUsage(patch.Value)...)
		case "/" + agui.StateBackgroundTasks:
			events = append(events, r.withResults()...)
		case "/" + agui.StateTodos:
			var todos []agentdomain.TodoItem
			if recode(patch.Value, &todos) {
				events = append(events, r.todos(todos))
			}
		}
	}
	return events
}

// sessionUsage reads a usage patch, whose one entry holds the session's totals.
func (r *Reducer) sessionUsage(value any) []agentdomain.ChatEvent {
	var usage []agui.TokenUsage
	if !recode(value, &usage) || len(usage) == 0 {
		return nil
	}
	return []agentdomain.ChatEvent{SessionUsageEvent{
		BaseChatEvent: r.base(),
		Model:         usage[0].Model,
		InputTokens:   tokenCount(usage[0].InputTokens),
		OutputTokens:  tokenCount(usage[0].OutputTokens),
		CachedTokens:  tokenCount(usage[0].CachedInputTokens),
	}}
}

func tokenCount(count *int64) int {
	if count == nil {
		return 0
	}
	return int(*count)
}

func (r *Reducer) todos(todos []agentdomain.TodoItem) agentdomain.ChatEvent {
	return agentdomain.TodoUpdateChatEvent{BaseChatEvent: r.base(), Todos: todos}
}

// activity reduces the activities the chat TUI renders as its own events:
// judge verdicts, tool progress, a cancelled call, conversation optimization
// and streamed bash output.
func (r *Reducer) activity(activityType string, content any) agentdomain.ChatEvent {
	var c struct {
		ToolCallID string                    `json:"toolCallId"`
		ToolName   string                    `json:"toolName"`
		Status     string                    `json:"status"`
		Message    string                    `json:"message"`
		Output     string                    `json:"output"`
		Complete   bool                      `json:"complete"`
		Tool       string                    `json:"tool"`
		Model      string                    `json:"model"`
		Decision   agentdomain.JudgeDecision `json:"decision"`
		Reason     string                    `json:"reason"`
		Turn       int                       `json:"turn"`
		Original   int                       `json:"originalCount"`
		Optimized  int                       `json:"optimizedCount"`
	}
	if !recode(content, &c) {
		return nil
	}
	switch activityType {
	case "tool_progress":
		if c.Status == "cancelled" {
			return agentdomain.ToolCancelledEvent{RequestID: r.requestID, Timestamp: time.Now(), ToolCallID: c.ToolCallID, ToolName: c.ToolName}
		}
		return agentdomain.ToolExecutionProgressEvent{BaseChatEvent: r.base(), ToolCallID: c.ToolCallID, ToolName: c.ToolName, Status: c.Status, Message: c.Message}
	case "bash_output":
		return agentdomain.BashOutputChunkEvent{BaseChatEvent: r.base(), ToolCallID: c.ToolCallID, Output: c.Output, IsComplete: c.Complete}
	case "optimization_status":
		return agentdomain.OptimizationStatusEvent{RequestID: r.requestID, Timestamp: time.Now(), Message: c.Message, OriginalCount: c.Original, OptimizedCount: c.Optimized}
	case "judge_verdict":
		return agentdomain.JudgeVerdictChatEvent{BaseChatEvent: r.base(), Tool: c.Tool, Model: c.Model, Decision: c.Decision, Reason: c.Reason, Turn: c.Turn}
	}
	return nil
}

// runFinished closes the run: a suspended run hands out its prompts, a
// cancelled one completes as cancelled, and a finished one completes with its
// last step, or with an empty one when no step completed it: a direct `!cmd`
// run stops on its tool result, and the run that records a plan decision has
// no step at all.
func (r *Reducer) runFinished(outcome string, interrupts []agui.Interrupt) ([]agentdomain.ChatEvent, End) {
	events := r.closeStep()
	if outcome != "interrupt" && outcome != string(agui.OutcomeCancelled) && r.held == nil {
		r.held = &agentdomain.ChatCompleteEvent{RequestID: r.requestID, Timestamp: time.Now()}
	}
	events = r.releaseHeld(events)
	switch outcome {
	case "interrupt":
		r.prompts = make([]Prompt, 0, len(interrupts))
		for _, interrupt := range interrupts {
			prompt := r.prompt(interrupt)
			r.prompts = append(r.prompts, prompt)
			if prompt.Event != nil {
				events = append(events, prompt.Event)
			}
		}
		return events, Suspended
	case string(agui.OutcomeCancelled):
		return append(events, agentdomain.ChatCompleteEvent{RequestID: r.requestID, Timestamp: time.Now(), Cancelled: true}), Cancelled
	}
	return events, Finished
}

// prompt is the event the chat TUI answers one interrupt with. The interrupt
// names what it waits on, and the calls the run streamed fill in what an
// older worker's interrupt leaves out.
func (r *Reducer) prompt(interrupt agui.Interrupt) Prompt {
	base := Prompt{InterruptID: interrupt.ID}
	switch interrupt.Reason {
	case agui.InterruptToolCall:
		id := cmp.Or(interrupt.ToolCallID, interrupt.ID)
		name, args := r.callOf(id, interrupt.Metadata)
		base.Event = agentdomain.ToolApprovalRequestedEvent{
			RequestID: r.requestID, Timestamp: time.Now(), Context: interrupt.Message,
			ToolCall:     sdk.ChatCompletionMessageToolCall{ID: id, Type: "function", Function: sdk.ChatCompletionMessageToolCallFunction{Name: name, Arguments: args}},
			ResponseChan: make(chan agentdomain.ApprovalAction, 1),
		}
	case agui.InterruptInputRequired:
		base.Event = agentdomain.UserQuestionRequestedEvent{
			RequestID: r.requestID, Timestamp: time.Now(), ToolCallID: interrupt.ID,
			Questions: r.questionsOf(interrupt.ID, interrupt.Metadata), ResponseChan: make(chan []agentdomain.UserQuestionAnswer, 1),
		}
	case agui.InterruptPlanApproval:
		planID, _ := interrupt.Metadata["planId"].(string)
		plan, _ := interrupt.Metadata["plan"].(string)
		base.Event = agentdomain.PlanApprovalRequestedEvent{
			RequestID: r.requestID, Timestamp: time.Now(), PlanID: planID, PlanContent: plan,
			ResponseChan: make(chan agentdomain.PlanApprovalAction, 1),
		}
	}
	return base
}

// callOf names the call an approval waits on, from the interrupt's metadata
// or else from the call the run streamed.
func (r *Reducer) callOf(id string, metadata map[string]any) (string, string) {
	name, _ := metadata["toolName"].(string)
	args, _ := metadata["arguments"].(string)
	if c := r.known[id]; c != nil {
		name, args = cmp.Or(name, c.name), cmp.Or(args, c.args.String())
	}
	return name, args
}

// questionsOf lists what a question interrupt asks, from its metadata or else
// from the AskUserQuestion call's arguments.
func (r *Reducer) questionsOf(id string, metadata map[string]any) []agentdomain.UserQuestion {
	var asked struct {
		Questions []agentdomain.UserQuestion `json:"questions"`
	}
	if raw, err := json.Marshal(metadata); err == nil && json.Unmarshal(raw, &asked) == nil && len(asked.Questions) > 0 {
		return asked.Questions
	}
	if c := r.known[id]; c != nil {
		_ = json.Unmarshal([]byte(c.args.String()), &asked)
	}
	return asked.Questions
}

// closeStep completes the model's open step with its text, reasoning and tool
// calls, after the results of the step before it.
func (r *Reducer) closeStep() []agentdomain.ChatEvent {
	events := r.toolResults()
	if r.text.Len() == 0 && r.reasoning.Len() == 0 && len(r.calls) == 0 {
		return events
	}
	calls := make([]sdk.ChatCompletionMessageToolCall, 0, len(r.calls))
	for _, c := range r.calls {
		calls = append(calls, sdk.ChatCompletionMessageToolCall{ID: c.id, Type: "function", Function: sdk.ChatCompletionMessageToolCallFunction{Name: c.name, Arguments: c.args.String()}})
	}
	complete := agentdomain.ChatCompleteEvent{
		RequestID: r.requestID, Timestamp: time.Now(), Message: r.text.String(), ReasoningContent: r.reasoning.String(), ToolCalls: calls,
	}
	r.text.Reset()
	r.reasoning.Reset()
	r.calls = nil
	if len(calls) == 0 {
		r.held = &complete
		return events
	}
	return append(events, complete)
}

// releaseHeld appends the held completion of the last step to events.
func (r *Reducer) releaseHeld(events []agentdomain.ChatEvent) []agentdomain.ChatEvent {
	if r.held == nil {
		return events
	}
	events = append(events, *r.held)
	r.held = nil
	return events
}

// withResults prepends the held completion and the completion of the tool
// results decoded so far to events, since whatever follows them starts the
// next step.
func (r *Reducer) withResults(events ...agentdomain.ChatEvent) []agentdomain.ChatEvent {
	events = r.toolResults(events...)
	if r.held != nil {
		events = append([]agentdomain.ChatEvent{*r.held}, events...)
		r.held = nil
	}
	return events
}

// toolResults prepends the completion of the tool results decoded so far to
// events.
func (r *Reducer) toolResults(events ...agentdomain.ChatEvent) []agentdomain.ChatEvent {
	if len(r.results) == 0 {
		return events
	}
	completed := agentdomain.ToolExecutionCompletedEvent{RequestID: r.requestID, Timestamp: time.Now(), TotalExecuted: len(r.results), Results: r.results}
	for _, result := range r.results {
		if result.Success {
			completed.SuccessCount++
		} else {
			completed.FailureCount++
		}
	}
	r.results = nil
	return append([]agentdomain.ChatEvent{completed}, events...)
}

func (r *Reducer) base() agentdomain.BaseChatEvent {
	return agentdomain.BaseChatEvent{RequestID: r.requestID, Timestamp: time.Now()}
}

// recode reads a value the event carried as decoded JSON into out.
func recode(value, out any) bool {
	raw, err := json.Marshal(value)
	return err == nil && json.Unmarshal(raw, out) == nil
}
