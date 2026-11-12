package headless

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	computer "github.com/inference-gateway/cli/internal/computer"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	ipc "github.com/inference-gateway/cli/internal/platform/ipc"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	models "github.com/inference-gateway/cli/internal/platform/models"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
)

// continuePrompt is the message a run input without new messages asks the agent
// to continue with, the run after one the client interrupted or paused.
const continuePrompt = "Please continue from where you left off."

// pendingInterrupt is one open interrupt's broker entry: why the run waits
// and which tool call raised it, empty for the plan and question kinds.
type pendingInterrupt struct {
	reason string
	tool   string
}

// headlessControl is the single reader of the headless process's stdin. It
// routes the run inputs a client sends into the message queue and, through
// their resume entries, into the approval, question and plan brokers the renderer
// blocks on, and browser results into the stdio browser. It mirrors what the
// chat approval coordinator does in the TUI. The channels close on stdin EOF.
type headlessControl struct {
	agentService agentdomain.AgentService
	messageQueue convdomain.MessageQueue
	refs         references
	sessionID    string
	approvals    chan ipc.ApprovalResponse
	questions    chan ipc.UserQuestionResponse
	plans        chan ipc.PlanApprovalResponse

	// pending holds the interrupts the runs raised, keyed by interrupt id
	// and carrying the reason and the tool call that raised it, so a resume
	// entry routes to its own broker and a take-over finds the Computer
	// calls whose approval box is open.
	pendingMu sync.Mutex
	pending   map[string]pendingInterrupt

	// directCancel stops the direct command the serve loop is running. The
	// stdin reader reads it while the serve loop sets it.
	directCancel atomic.Pointer[context.CancelFunc]

	// turnMu is held by the serve loop for each turn and by the stdin reader
	// for each rewind, so neither writes the conversation under the other.
	turnMu sync.Mutex

	// wake signals the serve loop that a run input landed on the queue.
	wake    chan struct{}
	browser *stdioBrowser
	panel   *Panel

	// drained signals a message that landed on the queue from elsewhere, such
	// as a finished background job's note. Unlike wake it never closes, since
	// a job can finish after stdin did.
	drained chan struct{}
	// deferred holds the messages after the serve loop's current turn, off the
	// queue so a running agent turn does not drain a direct task as text. Only
	// the serve loop writes it, under turnMu.
	deferred []convdomain.QueuedMessage
}

func newHeadlessControl(agentService agentdomain.AgentService, messageQueue convdomain.MessageQueue, refs references, sessionID string) *headlessControl {
	return &headlessControl{
		agentService: agentService,
		messageQueue: messageQueue,
		refs:         refs,
		sessionID:    sessionID,
		approvals:    make(chan ipc.ApprovalResponse, 4),
		questions:    make(chan ipc.UserQuestionResponse, 4),
		plans:        make(chan ipc.PlanApprovalResponse, 4),
		pending:      make(map[string]pendingInterrupt),
		wake:         make(chan struct{}, 1),
		drained:      make(chan struct{}, 1),
	}
}

// maxControlLineBytes accommodates ten 10 MiB attachments after base64 encoding.
const maxControlLineBytes = 160 << 20

func (c *headlessControl) readLines(in io.Reader) {
	reader := bufio.NewReaderSize(in, 64*1024)
	var line []byte
	oversized := false
	for {
		fragment, err := reader.ReadSlice('\n')
		if len(line)+len(fragment) > maxControlLineBytes {
			oversized = true
			line = nil
		}
		if !oversized {
			line = append(line, fragment...)
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if oversized {
			logger.Warn("discarding oversized headless control line", "max_bytes", maxControlLineBytes)
		} else if len(line) > 0 {
			c.dispatchLine(line)
		}
		line = nil
		oversized = false
		if err != nil {
			if err != io.EOF {
				logger.Warn("headless control stdin reader stopped", "error", err)
			}
			break
		}
	}
	close(c.approvals)
	close(c.questions)
	close(c.plans)
	close(c.wake)
	if c.browser != nil {
		c.browser.close()
	}
}

func (c *headlessControl) dispatchLine(line []byte) {
	var msg struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	if json.Unmarshal(line, &msg) != nil {
		return
	}
	if c.panel != nil && c.panel.Handle(line) {
		return
	}
	switch msg.Type {
	case "approval_response":
		var resp ipc.ApprovalResponse
		if json.Unmarshal(line, &resp) == nil {
			c.approvals <- resp
		}
	case "user_question_response":
		var resp ipc.UserQuestionResponse
		if json.Unmarshal(line, &resp) == nil {
			c.questions <- resp
		}
	case "plan_approval_response":
		var resp ipc.PlanApprovalResponse
		if json.Unmarshal(line, &resp) == nil {
			c.plans <- resp
		}
	case ipc.RunAgentInputFrameType:
		var frame runInputFrame
		if json.Unmarshal(line, &frame) != nil {
			return
		}
		c.enqueueRunInput(frame.Input)
	case "interrupt":
		_ = c.agentService.CancelRequest(c.sessionID)
		if cancel := c.directCancel.Load(); cancel != nil {
			(*cancel)()
		}
	case "browser_result":
		if c.browser != nil {
			c.browser.deliver(msg.ID, bytes.Clone(line))
		}
	}
}

// holdTurns keeps the serve loop from starting a turn until release runs. It
// reports false, holding nothing, while a turn is running or queued.
func (c *headlessControl) holdTurns() (release func(), idle bool) {
	if !c.turnMu.TryLock() {
		return nil, false
	}
	if c.hasTurn() {
		c.turnMu.Unlock()
		return nil, false
	}
	return c.turnMu.Unlock, true
}

// interruptible derives the context of a direct command an interrupt stops,
// until release runs.
func (c *headlessControl) interruptible(ctx context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(ctx)
	c.directCancel.Store(&cancel)
	return ctx, func() {
		c.directCancel.Store(nil)
		cancel()
	}
}

// runInputFrame is the frame that starts and continues runs: its messages are
// the new messages only and its resume entries answer the interrupts an
// interrupted run ended on.
type runInputFrame struct {
	Type  string             `json:"type"`
	Input agui.RunAgentInput `json:"input"`
}

// enqueueRunInput queues the input's new messages for the next turn and routes
// its resume entries to the brokers the open interrupts wait on: `@<path>` and
// `#N` tokens in the text expand, and attachments save to the project tmp dir
// as image parts or notes naming the saved path. A run input with no messages
// is the continue run the client asks for after stopping one, which the agent
// continues from the conversation it kept.
func (c *headlessControl) enqueueRunInput(input agui.RunAgentInput) {
	for _, msg := range input.Messages {
		text, attachments := messageParts(msg)
		text = c.refs.expand(text)
		images, notes := saveAttachments(attachments)
		content := strings.Join(append([]string{text}, notes...), "\n")
		if content == "" && len(images) == 0 {
			logger.Debug("dropping a run input message with neither text nor a usable attachment")
			continue
		}
		message, err := userMessage(content, images)
		if err != nil {
			logger.Warn("dropping a run input message with an unusable attachment", "error", err)
			continue
		}
		if err := c.messageQueue.Enqueue(context.Background(), message, convdomain.QueueSourceStdin, ipc.UserMessageRequestID); err != nil {
			logger.Error("failed to enqueue the run input message", "error", err)
			continue
		}
	}
	if len(input.Messages) == 0 && len(input.Resume) == 0 {
		if err := c.messageQueue.Enqueue(context.Background(), userMessageText(continuePrompt), convdomain.QueueSourceStdin, ipc.UserMessageRequestID); err != nil {
			logger.Error("failed to enqueue the continue prompt", "error", err)
		}
	}
	for _, entry := range input.Resume {
		c.answerResume(entry)
	}
	c.wakeTurns()
}

// references expands the tokens a run input's text carries the way the chat
// TUI's input does: `@<path>` files first, then `#N` issues.
type references struct {
	files  agentdomain.FileService
	images agentdomain.ImageService
	models convdomain.ModelService
	issues agentdomain.GitHubIssueService
}

func newReferences(svc Services) references {
	return references{
		files:  svc.GetFileService(),
		images: svc.GetImageService(),
		models: svc.GetModelService(),
		issues: svc.GetGitHubIssueService(),
	}
}

func (r references) expand(text string) string {
	supportsVision := r.models != nil && models.SupportsVision(r.models.GetCurrentModel())
	text = agentdomain.ExpandFileReferences(text, r.files, r.images, supportsVision)
	expanded, failures := agentdomain.ExpandIssueReferences(context.Background(), r.issues, text)
	for number, err := range failures {
		logger.Debug("issue expansion: GetIssue failed - leaving token in place", "number", number, "error", err)
	}
	return expanded
}

// userMessageText builds the plain user message a run input carries.
func userMessageText(content string) sdk.Message {
	return sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent(content)}
}

// messageParts splits one run input message into its text and its attachments,
// the content parts of the user message: text parts join into the text, image
// parts become model-readable attachments.
func messageParts(msg agui.Message) (string, []agentdomain.ImageAttachment) {
	switch content := msg.Content.(type) {
	case string:
		return content, nil
	case []any:
		var texts []string
		var attachments []agentdomain.ImageAttachment
		for _, part := range inputParts(content) {
			switch part.Type {
			case agui.InputContentTypeText:
				texts = append(texts, part.Text)
			case agui.InputContentTypeImage:
				data := part.Data
				if data == "" && part.Source != nil {
					data = part.Source.Value
				}
				attachments = append(attachments, agentdomain.ImageAttachment{
					Data: data, MimeType: part.MimeType, Filename: part.Filename,
				})
			}
		}
		return strings.Join(texts, "\n"), attachments
	}
	return "", nil
}

// inputParts re-reads the message's decoded content array as input parts, so a
// part and its shape survive the round trip the frame's JSON makes it take.
func inputParts(content []any) []agui.InputContent {
	parts := make([]agui.InputContent, 0, len(content))
	for _, raw := range content {
		data, err := json.Marshal(raw)
		if err != nil {
			continue
		}
		var part agui.InputContent
		if json.Unmarshal(data, &part) != nil {
			continue
		}
		parts = append(parts, part)
	}
	return parts
}

// answerResume routes one resume entry to the broker of the interrupt it
// answers, skipping one that answers nothing open: a late resume for an
// interrupt the run already let go of must not decide the next one.
func (c *headlessControl) answerResume(entry agui.ResumeEntry) {
	c.pendingMu.Lock()
	note, open := c.pending[entry.InterruptID]
	c.pendingMu.Unlock()
	if !open {
		return
	}
	switch note.reason {
	case agui.InterruptInputRequired:
		c.questions <- ipc.UserQuestionResponse{
			Type: "user_question_response", ToolCallID: entry.InterruptID,
			Answers: questionAnswers(entry.Payload), Cancelled: entry.Status != agui.ResumeStatusResolved,
		}
		return
	case agui.InterruptPlanApproval:
		c.plans <- ipc.PlanApprovalResponse{
			Type: "plan_approval_response", PlanID: entry.InterruptID, Action: planDecision(entry),
		}
		return
	}
	c.approvals <- ipc.ApprovalResponse{
		Type: "approval_response", ToolCallID: entry.InterruptID,
		Approved: entry.Status == agui.ResumeStatusResolved,
	}
}

// questionAnswers is the answers array of a question's resume payload, given
// as {"answers": [...]} the way the interrupt's response schema asks, or bare.
func questionAnswers(payload any) json.RawMessage {
	raw, _ := json.Marshal(payload)
	var wrapped struct {
		Answers json.RawMessage `json:"answers"`
	}
	if json.Unmarshal(raw, &wrapped) == nil && wrapped.Answers != nil {
		return wrapped.Answers
	}
	return raw
}

// planDecision is the action a plan's resume entry decides. A cancelled entry
// or a payload without a known action keeps planning.
func planDecision(entry agui.ResumeEntry) string {
	if entry.Status != agui.ResumeStatusResolved {
		return ipc.PlanReject
	}
	raw, _ := json.Marshal(entry.Payload)
	var payload struct {
		Action string `json:"action"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return ipc.PlanReject
	}
	return payload.Action
}

// Note records the interrupt a run just raised and the tool call a tool-call
// interrupt names, so the resume entry that answers it routes to its broker
// and a screen take-over finds the open Computer approvals.
func (c *headlessControl) Note(id, reason, tool string) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	c.pending[id] = pendingInterrupt{reason: reason, tool: tool}
}

// Forget drops the interrupt whose answer the run just handled.
func (c *headlessControl) Forget(id string) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	delete(c.pending, id)
}

// rejectComputerApprovals answers every open approval interrupt of a Computer
// tool call as rejected, the decision a take-over makes for the agent: the
// person now decides what happens on that screen. The broker's wait matches
// by tool call id. A full broker drops the answer, the way Notify drops.
func (c *headlessControl) rejectComputerApprovals() {
	c.pendingMu.Lock()
	var ids []string
	for id, note := range c.pending {
		if note.reason == agui.InterruptToolCall && note.tool == computer.ToolComputer {
			ids = append(ids, id)
		}
	}
	c.pendingMu.Unlock()
	for _, id := range ids {
		select {
		case c.approvals <- ipc.ApprovalResponse{Type: "approval_response", ToolCallID: id, Approved: false}:
		default:
		}
	}
}

// wakeTurns nudges the serve loop that a run input landed on the queue, the
// nudge a hand-back's continue message arrives beside.
func (c *headlessControl) wakeTurns() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// awaitTurn blocks until the message queue holds the next turn's input, and
// reports false once stdin closed with nothing left to run. A frame that
// arrives between turns has no open run to answer, so the reader drops it and
// never blocks on a channel nobody drains.
func (c *headlessControl) awaitTurn() bool {
	for !c.hasTurn() {
		var open bool
		select {
		case <-c.drained:
			continue
		case _, open = <-c.wake:
		case _, open = <-c.approvals:
		case _, open = <-c.questions:
		case _, open = <-c.plans:
		}
		if !open {
			return c.hasTurn()
		}
	}
	return true
}

func (c *headlessControl) hasTurn() bool {
	return len(c.deferred) > 0 || !c.messageQueue.IsEmpty()
}

// nextTurn takes the serve loop's next turn: a direct task alone, or the
// prompts before the next one, so a `!cmd` queued next to anything else still
// runs instead of reaching the model as text.
func (c *headlessControl) nextTurn(ctx context.Context) []convdomain.QueuedMessage {
	pending := append(c.deferred, drainQueue(ctx, c.messageQueue)...)
	end := len(pending)
	for i, message := range pending {
		if isDirectMessage(message) {
			end = max(i, 1)
			break
		}
	}
	c.deferred = pending[end:]
	return pending[:end]
}

// drainWaker wakes the serve loop for a message that landed on the queue
// outside a run input, which runs as a turn of its own the way the chat TUI
// drains it. Every other notification goes on to the client.
type drainWaker struct {
	agentdomain.UINotifier
	drained chan<- struct{}
}

func (n drainWaker) Notify(event any) {
	if _, drain := event.(agentdomain.DrainQueueEvent); !drain {
		n.UINotifier.Notify(event)
		return
	}
	select {
	case n.drained <- struct{}{}:
	default:
	}
}

// uiBridge is the headless UINotifier: it forwards the UI notifications a
// headless client renders (screen recording status, computer-use actions,
// background job changes) into the rendered event stream. Notify never blocks the producer; a full buffer
// drops the event.
type uiBridge chan agentdomain.ChatEvent

func (b uiBridge) Notify(event any) {
	var ev agentdomain.ChatEvent
	switch n := event.(type) {
	case agentdomain.ScreenRecordingStatusEvent:
		n.Timestamp = time.Now()
		ev = n
	case agentdomain.ComputerUseActionEvent:
		n.Timestamp = time.Now()
		ev = n
	case agentdomain.ScreenTakenOverEvent:
		n.Timestamp = time.Now()
		ev = n
	case agentdomain.ScreenHandedBackEvent:
		n.Timestamp = time.Now()
		ev = n
	case agentdomain.BackgroundTasksChangedEvent:
		n.Timestamp = time.Now()
		ev = n
	default:
		return
	}
	select {
	case b <- ev:
	default:
	}
}

// merge interleaves bridged notifications into events and closes when events
// does; notifications arriving after that are dropped.
func (b uiBridge) merge(events <-chan agentdomain.ChatEvent) <-chan agentdomain.ChatEvent {
	merged := make(chan agentdomain.ChatEvent)
	go func() {
		defer close(merged)
		for {
			select {
			case ev, ok := <-events:
				if !ok {
					return
				}
				merged <- ev
			case ev := <-b:
				merged <- ev
			}
		}
	}()
	return merged
}
