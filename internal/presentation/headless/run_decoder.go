package headless

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

// RunEnd is where a decoded frame leaves the run a RunDecoder follows.
type RunEnd int

const (
	RunOpen RunEnd = iota
	RunResumed
	RunSuspended
	RunFinished
	RunCancelled
	RunFailed
)

// Prompt is one interrupt a suspended run waits on, as the event the chat TUI
// answers on its ResponseChan. Event is nil for an interrupt of an unknown
// reason, which the client can only cancel.
type Prompt struct {
	InterruptID string
	Event       agentdomain.ChatEvent
}

// RunDecoder turns the AG-UI frames of a thread's runs back into the agent
// ChatEvents the chat TUI renders, the inverse of RunEncoder. It follows one
// client request across a suspended run and its continuations, and tolerates
// attaching mid-run.
type RunDecoder struct {
	requestID string
	started   bool

	roles map[string]string
	notes map[string]*strings.Builder

	text      strings.Builder
	reasoning strings.Builder
	calls     []*decodedCall
	known     map[string]*decodedCall
	results   []*agentdomain.ToolExecutionResult
	prompts   []Prompt
	// held is the completion of a step that handed no calls to tools. It
	// ends the client's turn, so it waits for the run to finish, unless a
	// queued message continues the run first.
	held *agentdomain.ChatCompleteEvent
}

// decodedCall is a tool call the run streamed: its name and its arguments.
type decodedCall struct {
	id   string
	name string
	args strings.Builder
}

// decodedFrame is the slice of an AG-UI event the decoder reads.
type decodedFrame struct {
	Type         string          `json:"type"`
	MessageID    string          `json:"messageId"`
	Role         string          `json:"role"`
	Delta        json.RawMessage `json:"delta"`
	ToolCallID   string          `json:"toolCallId"`
	ToolCallName string          `json:"toolCallName"`
	Content      json.RawMessage `json:"content"`
	ActivityType string          `json:"activityType"`
	Message      string          `json:"message"`
	Snapshot     struct {
		Todos *[]agentdomain.TodoItem `json:"todos"`
	} `json:"snapshot"`
	Outcome struct {
		Type       string           `json:"type"`
		Interrupts []agui.Interrupt `json:"interrupts"`
	} `json:"outcome"`
}

// decodedPatch is one JSON Patch operation of a STATE_DELTA.
type decodedPatch struct {
	Path  string          `json:"path"`
	Value json.RawMessage `json:"value"`
}

// NewRunDecoder builds the decoder for the client request requestID, the id
// every decoded event carries.
func NewRunDecoder(requestID string) *RunDecoder {
	return &RunDecoder{
		requestID: requestID,
		roles:     make(map[string]string),
		notes:     make(map[string]*strings.Builder),
		known:     make(map[string]*decodedCall),
	}
}

// Decode turns one frame into the events it stands for and reports where it
// leaves the run. Frames outside a run's vocabulary decode to nothing.
//
//nolint:gocyclo,cyclop // cohesive event switch; each case decodes one AG-UI event type
func (d *RunDecoder) Decode(frame []byte) ([]agentdomain.ChatEvent, RunEnd) {
	var f decodedFrame
	if json.Unmarshal(frame, &f) != nil {
		return nil, RunOpen
	}
	switch f.Type {
	case "RUN_STARTED":
		return d.runStarted()
	case "TEXT_MESSAGE_START":
		d.roles[f.MessageID] = f.Role
	case "TEXT_MESSAGE_CONTENT":
		return d.textContent(f.MessageID, deltaText(f.Delta)), RunOpen
	case "TEXT_MESSAGE_END":
		return d.textEnd(f.MessageID), RunOpen
	case "REASONING_MESSAGE_CONTENT":
		delta := deltaText(f.Delta)
		d.reasoning.WriteString(delta)
		return d.withResults(agentdomain.ChatChunkEvent{RequestID: d.requestID, Timestamp: time.Now(), ReasoningContent: delta}), RunOpen
	case "TOOL_CALL_START":
		events := d.withResults()
		d.call(f.ToolCallID).name = f.ToolCallName
		return events, RunOpen
	case "TOOL_CALL_ARGS":
		d.call(f.ToolCallID).args.WriteString(deltaText(f.Delta))
	case "TOOL_CALL_RESULT":
		return d.toolResult(f.Content), RunOpen
	case "STATE_SNAPSHOT":
		if f.Snapshot.Todos != nil && len(*f.Snapshot.Todos) > 0 {
			return []agentdomain.ChatEvent{d.todos(*f.Snapshot.Todos)}, RunOpen
		}
	case "STATE_DELTA":
		return d.stateDelta(f.Delta), RunOpen
	case "ACTIVITY_SNAPSHOT":
		if event := d.activity(f.ActivityType, f.Content); event != nil {
			return d.withResults(event), RunOpen
		}
	case "RUN_FINISHED":
		return d.runFinished(f.Outcome.Type, f.Outcome.Interrupts)
	case "RUN_ERROR":
		events := d.releaseHeld(d.closeStep())
		return append(events, agentdomain.ChatErrorEvent{RequestID: d.requestID, Timestamp: time.Now(), Error: errors.New(f.Message)}), RunFailed
	}
	return nil, RunOpen
}

// Prompts are the interrupts the run suspended on, valid after Decode reported
// RunSuspended.
func (d *RunDecoder) Prompts() []Prompt {
	return d.prompts
}

// runStarted opens the request on its first run. A later run is the
// continuation of a suspended one, whose prompts someone answered.
func (d *RunDecoder) runStarted() ([]agentdomain.ChatEvent, RunEnd) {
	d.prompts = nil
	d.held = nil
	if d.started {
		return nil, RunResumed
	}
	d.started = true
	return []agentdomain.ChatEvent{agentdomain.ChatStartEvent{RequestID: d.requestID, Timestamp: time.Now()}}, RunOpen
}

// textContent streams an assistant delta, or keeps the text of a message
// with another role until it ends. A delta whose message never started, as
// when the client attached mid-message, is the assistant's.
func (d *RunDecoder) textContent(messageID, delta string) []agentdomain.ChatEvent {
	if role := d.roles[messageID]; role != "" && role != string(sdk.Assistant) {
		note := d.notes[messageID]
		if note == nil {
			note = &strings.Builder{}
			d.notes[messageID] = note
		}
		note.WriteString(delta)
		return nil
	}
	d.text.WriteString(delta)
	return d.withResults(agentdomain.ChatChunkEvent{RequestID: d.requestID, Timestamp: time.Now(), Content: delta})
}

// textEnd turns a finished message with a role other than the assistant's,
// a queued user message or a background job's note, into the queued event the
// chat TUI renders it from.
func (d *RunDecoder) textEnd(messageID string) []agentdomain.ChatEvent {
	role := d.roles[messageID]
	note := d.notes[messageID]
	delete(d.roles, messageID)
	delete(d.notes, messageID)
	if note == nil {
		return nil
	}
	message := sdk.Message{Role: sdk.MessageRole(role), Content: sdk.NewMessageContent(note.String())}
	return d.withResults(agentdomain.MessageQueuedEvent{RequestID: d.requestID, Timestamp: time.Now(), Message: message})
}

// call is the tool call with id, buffered for the step it belongs to.
func (d *RunDecoder) call(id string) *decodedCall {
	if c := d.known[id]; c != nil {
		return c
	}
	c := &decodedCall{id: id}
	d.known[id] = c
	d.calls = append(d.calls, c)
	return c
}

// toolResult keeps one executed call's result for the completion event the
// step's results go out as. A result closes the model's step if it is open.
func (d *RunDecoder) toolResult(content json.RawMessage) []agentdomain.ChatEvent {
	var raw string
	if json.Unmarshal(content, &raw) != nil {
		return nil
	}
	var result agentdomain.ToolExecutionResult
	if json.Unmarshal([]byte(raw), &result) != nil {
		return nil
	}
	events := d.closeStep()
	d.results = append(d.results, &result)
	return events
}

// stateDelta decodes the state patches the chat TUI renders: the usage patch
// that closes a model step, the todos and the background job patch that
// follows a step's tool results.
func (d *RunDecoder) stateDelta(delta json.RawMessage) []agentdomain.ChatEvent {
	var patches []decodedPatch
	if json.Unmarshal(delta, &patches) != nil {
		return nil
	}
	var events []agentdomain.ChatEvent
	for _, patch := range patches {
		switch patch.Path {
		case "/" + agui.StateUsage:
			events = append(events, d.closeStep()...)
		case "/" + agui.StateBackgroundTasks:
			events = append(events, d.withResults()...)
		case "/" + agui.StateTodos:
			var todos []agentdomain.TodoItem
			if json.Unmarshal(patch.Value, &todos) == nil {
				events = append(events, d.todos(todos))
			}
		}
	}
	return events
}

func (d *RunDecoder) todos(todos []agentdomain.TodoItem) agentdomain.ChatEvent {
	return agentdomain.TodoUpdateChatEvent{BaseChatEvent: d.base(), Todos: todos}
}

// activity decodes the activities the chat TUI renders as its own events:
// judge verdicts, tool progress and streamed bash output.
func (d *RunDecoder) activity(activityType string, content json.RawMessage) agentdomain.ChatEvent {
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
	}
	if json.Unmarshal(content, &c) != nil {
		return nil
	}
	switch activityType {
	case "tool_progress":
		return agentdomain.ToolExecutionProgressEvent{BaseChatEvent: d.base(), ToolCallID: c.ToolCallID, ToolName: c.ToolName, Status: c.Status, Message: c.Message}
	case "bash_output":
		return agentdomain.BashOutputChunkEvent{BaseChatEvent: d.base(), ToolCallID: c.ToolCallID, Output: c.Output, IsComplete: c.Complete}
	case "judge_verdict":
		return agentdomain.JudgeVerdictChatEvent{BaseChatEvent: d.base(), Tool: c.Tool, Model: c.Model, Decision: c.Decision, Reason: c.Reason, Turn: c.Turn}
	}
	return nil
}

// runFinished closes the run: a suspended run hands out its prompts, a
// cancelled one completes as cancelled, and a finished one completes with its
// last step, or with an empty one when no step completed it: a direct `!cmd`
// run stops on its tool result, and the run that records a plan decision has
// no step at all.
func (d *RunDecoder) runFinished(outcome string, interrupts []agui.Interrupt) ([]agentdomain.ChatEvent, RunEnd) {
	events := d.closeStep()
	if outcome != "interrupt" && outcome != string(agui.OutcomeCancelled) && d.held == nil {
		d.held = &agentdomain.ChatCompleteEvent{RequestID: d.requestID, Timestamp: time.Now()}
	}
	events = d.releaseHeld(events)
	switch outcome {
	case "interrupt":
		d.prompts = make([]Prompt, 0, len(interrupts))
		for _, interrupt := range interrupts {
			prompt := d.prompt(interrupt)
			d.prompts = append(d.prompts, prompt)
			if prompt.Event != nil {
				events = append(events, prompt.Event)
			}
		}
		return events, RunSuspended
	case string(agui.OutcomeCancelled):
		return append(events, agentdomain.ChatCompleteEvent{RequestID: d.requestID, Timestamp: time.Now(), Cancelled: true}), RunCancelled
	}
	return events, RunFinished
}

// prompt is the event the chat TUI answers one interrupt with. The interrupt
// names what it waits on, and the calls the run streamed fill in what an
// older worker's interrupt leaves out.
func (d *RunDecoder) prompt(interrupt agui.Interrupt) Prompt {
	base := Prompt{InterruptID: interrupt.ID}
	switch interrupt.Reason {
	case agui.InterruptToolCall:
		id := cmp.Or(interrupt.ToolCallID, interrupt.ID)
		name, args := d.callOf(id, interrupt.Metadata)
		base.Event = agentdomain.ToolApprovalRequestedEvent{
			RequestID: d.requestID, Timestamp: time.Now(), Context: interrupt.Message,
			ToolCall:     sdk.ChatCompletionMessageToolCall{ID: id, Type: "function", Function: sdk.ChatCompletionMessageToolCallFunction{Name: name, Arguments: args}},
			ResponseChan: make(chan agentdomain.ApprovalAction, 1),
		}
	case agui.InterruptInputRequired:
		base.Event = agentdomain.UserQuestionRequestedEvent{
			RequestID: d.requestID, Timestamp: time.Now(), ToolCallID: interrupt.ID,
			Questions: d.questionsOf(interrupt.ID, interrupt.Metadata), ResponseChan: make(chan []agentdomain.UserQuestionAnswer, 1),
		}
	case interruptPlanApproval:
		planID, _ := interrupt.Metadata["planId"].(string)
		plan, _ := interrupt.Metadata["plan"].(string)
		base.Event = agentdomain.PlanApprovalRequestedEvent{
			RequestID: d.requestID, Timestamp: time.Now(), PlanID: planID, PlanContent: plan,
			ResponseChan: make(chan agentdomain.PlanApprovalAction, 1),
		}
	}
	return base
}

// callOf names the call an approval waits on, from the interrupt's metadata
// or else from the call the run streamed.
func (d *RunDecoder) callOf(id string, metadata map[string]any) (string, string) {
	name, _ := metadata["toolName"].(string)
	args, _ := metadata["arguments"].(string)
	if c := d.known[id]; c != nil {
		name, args = cmp.Or(name, c.name), cmp.Or(args, c.args.String())
	}
	return name, args
}

// questionsOf lists what a question interrupt asks, from its metadata or else
// from the AskUserQuestion call's arguments.
func (d *RunDecoder) questionsOf(id string, metadata map[string]any) []agentdomain.UserQuestion {
	var asked struct {
		Questions []agentdomain.UserQuestion `json:"questions"`
	}
	if raw, err := json.Marshal(metadata); err == nil && json.Unmarshal(raw, &asked) == nil && len(asked.Questions) > 0 {
		return asked.Questions
	}
	if c := d.known[id]; c != nil {
		_ = json.Unmarshal([]byte(c.args.String()), &asked)
	}
	return asked.Questions
}

// closeStep completes the model's open step with its text, reasoning and tool
// calls, after the results of the step before it.
func (d *RunDecoder) closeStep() []agentdomain.ChatEvent {
	events := d.toolResults()
	if d.text.Len() == 0 && d.reasoning.Len() == 0 && len(d.calls) == 0 {
		return events
	}
	calls := make([]sdk.ChatCompletionMessageToolCall, 0, len(d.calls))
	for _, c := range d.calls {
		calls = append(calls, sdk.ChatCompletionMessageToolCall{ID: c.id, Type: "function", Function: sdk.ChatCompletionMessageToolCallFunction{Name: c.name, Arguments: c.args.String()}})
	}
	complete := agentdomain.ChatCompleteEvent{
		RequestID: d.requestID, Timestamp: time.Now(), Message: d.text.String(), ReasoningContent: d.reasoning.String(), ToolCalls: calls,
	}
	d.text.Reset()
	d.reasoning.Reset()
	d.calls = nil
	if len(calls) == 0 {
		d.held = &complete
		return events
	}
	return append(events, complete)
}

// releaseHeld appends the held completion of the last step to events.
func (d *RunDecoder) releaseHeld(events []agentdomain.ChatEvent) []agentdomain.ChatEvent {
	if d.held == nil {
		return events
	}
	events = append(events, *d.held)
	d.held = nil
	return events
}

// withResults prepends the held completion and the completion of the tool
// results decoded so far to events, since whatever follows them starts the
// next step.
func (d *RunDecoder) withResults(events ...agentdomain.ChatEvent) []agentdomain.ChatEvent {
	events = d.toolResults(events...)
	if d.held != nil {
		events = append([]agentdomain.ChatEvent{*d.held}, events...)
		d.held = nil
	}
	return events
}

// toolResults prepends the completion of the tool results decoded so far to
// events.
func (d *RunDecoder) toolResults(events ...agentdomain.ChatEvent) []agentdomain.ChatEvent {
	if len(d.results) == 0 {
		return events
	}
	completed := agentdomain.ToolExecutionCompletedEvent{RequestID: d.requestID, Timestamp: time.Now(), TotalExecuted: len(d.results), Results: d.results}
	for _, result := range d.results {
		if result.Success {
			completed.SuccessCount++
		} else {
			completed.FailureCount++
		}
	}
	d.results = nil
	return append([]agentdomain.ChatEvent{completed}, events...)
}

func (d *RunDecoder) base() agentdomain.BaseChatEvent {
	return agentdomain.BaseChatEvent{RequestID: d.requestID, Timestamp: time.Now()}
}

// deltaText is a content or arguments delta, which travels as a JSON string.
func deltaText(delta json.RawMessage) string {
	var text string
	_ = json.Unmarshal(delta, &text)
	return text
}
