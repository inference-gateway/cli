// The channel-side driving adapter: one threadChat per chat follows its
// sender's thread on the daemon's session registry, hands every inbound
// message over as ipc frames, and renders the worker's AG-UI events back
// through the channel, the interrupt answers included.

package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	uuid "github.com/google/uuid"

	chn "github.com/inference-gateway/cli/internal/channels"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	ipc "github.com/inference-gateway/cli/internal/platform/ipc"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
	sessionsdomain "github.com/inference-gateway/cli/internal/sessions/domain"
)

// threadChat is the per-chat driving adapter onto the daemon's thread
// registry: it is the worker's Client of one chat, follows the chat's
// deterministic session id on the daemon's working dir, delivers the
// worker's AG-UI renders as channel messages, and answers the interrupts
// (tool approvals, user questions) the way the other clients do.
type threadChat struct {
	ctx        context.Context
	manager    *ChannelManagerService
	channel    chn.Channel
	recipient  string
	sessionID  string
	projectDir string

	// frames carries the frames the registry delivers until the daemon
	// stops. handleFrames drains it; a full buffer drops the frame rather
	// than freezing the registry's pump for every follower.
	frames chan []byte

	// mu guards the idle bookkeeping: active is the time of the last
	// frame in either direction, and detouch detaches the chat on idle
	// so the registry may reap its worker.
	mu     sync.Mutex
	active time.Time
	detach *time.Timer

	// render state, owned by handleFrames: the open assistant
	// message's deltas and its role, the tool calls rendered for the
	// next flush, the tool call being streamed, and the calls of the run
	// by id, which the interrupt that suspends the run names.
	message  strings.Builder
	role     string
	tools    []string
	toolName string
	toolArgs string
	calls    map[string]ipc.ApprovalRequest
}

// threadChatFor returns the sender's render adapter, building and starting
// its render loop on first use.
func (cm *ChannelManagerService) threadChatFor(ctx context.Context, senderKey string, ch chn.Channel, senderID, projectDir string) *threadChat {
	if cached, ok := cm.threadChats.Load(senderKey); ok {
		return cached.(*threadChat)
	}
	t := &threadChat{
		ctx:        ctx,
		manager:    cm,
		channel:    ch,
		recipient:  senderID,
		sessionID:  convdomain.FormatChannelSessionID(ch.Name(), senderID),
		projectDir: projectDir,
		frames:     make(chan []byte, frameBuffer),
		active:     time.Now(),
		calls:      make(map[string]ipc.ApprovalRequest),
	}
	t.markActive()
	cm.threadChats.Store(senderKey, t)
	go t.handleFrames()
	return t
}

// threadFrame is the resume_conversation frame that makes the chat follow
// its sender's thread. The thread options apply when the worker launches.
type threadFrame struct {
	Type       string `json:"type"`
	ID         string `json:"id,omitempty"`
	ProjectDir string `json:"project_dir,omitempty"`
	sessionsdomain.ThreadOptions
}

// runInputFrame starts the chat's next run with its new message, or answers
// the interrupts a suspended run waits on with resume entries.
type runInputFrame struct {
	Type  string             `json:"type"`
	Input agui.RunAgentInput `json:"input"`
}

const (
	frameResume   = "resume_conversation"
	frameRunInput = "run_agent_input"
)

// frameBuffer is how many undelivered worker frames a busy chat's render
// loop may owe before further frames drop. ponytail: resolveApproval blocks
// the render loop while the user decides and the continuation run's frames
// queue here, answer approvals off the loop if chats start dropping frames.
const frameBuffer = 64

// Deliver accepts one frame from the thread registry. A full buffer drops
// the frame: the chat render is best effort, and a blocking Deliver would
// freeze the registry's pump for every follower of the thread.
func (t *threadChat) Deliver(frame []byte) {
	t.markActive()
	select {
	case t.frames <- frame:
	default:
		logger.Debug("dropping a worker frame the chat is too busy to render", append(t.tags(), "recipient", t.recipient)...)
	}
}

// deliverUserMessage routes one inbound message to the chat's thread: the
// resume_conversation frame keeps it following the deterministic session id
// per sender (idempotent on the registry), and the run input carries the new
// user message with the images as content parts. No subprocess is spawned
// per message.
func (t *threadChat) deliverUserMessage(ctx context.Context, msg chn.InboundMessage, projectDir string, opts sessionsdomain.ThreadOptions) error {
	frames := [][]byte{
		encodeThreadFrame(t, threadFrame{Type: frameResume, ID: t.sessionID, ProjectDir: projectDir, ThreadOptions: opts}),
		encodeThreadFrame(t, t.runInput([]agui.Message{userMessage(msg)}, nil)),
	}
	for _, frame := range frames {
		if err := t.sendFrame(ctx, frame); err != nil {
			return err
		}
	}
	return nil
}

// runInput builds the run_agent_input frame of one run on the chat's thread.
func (t *threadChat) runInput(messages []agui.Message, resume []agui.ResumeEntry) runInputFrame {
	return runInputFrame{Type: frameRunInput, Input: agui.RunAgentInput{
		ThreadID: t.sessionID, RunID: uuid.NewString(), Messages: messages, Resume: resume,
	}}
}

// userMessage renders one inbound chat message as the run input's user
// message: plain text, or text and image parts when it carries images.
func userMessage(msg chn.InboundMessage) agui.Message {
	message := agui.Message{ID: uuid.NewString(), Role: "user", Content: msg.Content}
	if len(msg.Images) == 0 {
		return message
	}
	parts := []agui.InputContent{{Type: agui.InputContentTypeText, Text: msg.Content}}
	for _, image := range msg.Images {
		parts = append(parts, agui.InputContent{Type: agui.InputContentTypeImage, MimeType: image.MimeType, Data: image.Data, Filename: image.Filename})
	}
	message.Content = parts
	return message
}

// encodeThreadFrame marshals one frame. A frame carries plain data, so a
// failure is a programming error logged and sent as the registry's
// rejection answer.
func encodeThreadFrame(t *threadChat, frame any) []byte {
	data, err := json.Marshal(frame)
	if err != nil {
		logger.Error("failed to marshal a chat frame", append(t.tags(), "recipient", t.recipient, "error", err)...)
	}
	return data
}

// sendFrame hands one frame to the registry, waking the chat first so the
// idle detach can never race a frame that is about to route.
func (t *threadChat) sendFrame(ctx context.Context, frame []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	t.markActive()
	if t.manager.router == nil {
		return fmt.Errorf("the thread registry is not running - enable the AG-UI binding (daemon.binding) in .infer/")
	}
	t.manager.router.Handle(t, frame)
	return nil
}

// markActive notes a frame in either direction and re-arms the idle
// detach, so a live conversation never detaches under its own traffic.
func (t *threadChat) markActive() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.active = time.Now()
	if t.detach == nil {
		t.detach = time.AfterFunc(sessionsdomain.IdleTimeout, t.detachIdle)
	} else {
		t.detach.Reset(sessionsdomain.IdleTimeout)
	}
}

// detachIdle stops following the thread once the chat sat idle a whole
// grace, letting the registry reap its idle worker. Re-arms for the
// remaining grace when traffic raced the expiry.
func (t *threadChat) detachIdle() {
	t.mu.Lock()
	idle := time.Since(t.active)
	if idle < sessionsdomain.IdleTimeout {
		t.detach.Reset(sessionsdomain.IdleTimeout - idle)
		t.mu.Unlock()
		return
	}
	t.detach.Stop()
	t.detach = nil
	t.mu.Unlock()

	if t.manager.router != nil {
		t.manager.router.Detach(t)
	}
	logger.Debug("detached an idle chat thread", append(t.tags(), "recipient", t.recipient)...)
}

// DetachAll detaches every chat from its thread and stops the idle timers
// when the daemon's channels stop.
func (cm *ChannelManagerService) DetachThreads() {
	cm.threadChats.Range(func(key, value any) bool {
		t := value.(*threadChat)
		t.mu.Lock()
		if t.detach != nil {
			t.detach.Stop()
			t.detach = nil
		}
		t.mu.Unlock()
		if cm.router != nil {
			cm.router.Detach(t)
		}
		return true
	})
}

// handleFrames handles the worker frames until the daemon's context ends.
func (t *threadChat) handleFrames() {
	for {
		select {
		case <-t.ctx.Done():
			return
		case frame, open := <-t.frames:
			if !open {
				return
			}
			t.handle(frame)
		}
	}
}

// handle decodes one worker frame: it renders the conversation frames as
// channel messages and answers the interrupts a suspended run ends on the
// way the other clients do, approvals with the user's decision and questions
// with a dismissal. Unknown frames are dropped, the way the desktop client
// skips what it cannot render.
func (t *threadChat) handle(frame []byte) {
	var envelope struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(frame, &envelope) != nil {
		logger.Debug("dropping an undecodable worker frame", append(t.tags(), "recipient", t.recipient)...)
		return
	}

	switch envelope.Type {
	case "TEXT_MESSAGE_START":
		t.onTextStart(frame)
	case "TEXT_MESSAGE_CONTENT":
		t.onTextDelta(frame)
	case "TEXT_MESSAGE_END":
		t.onTextEnd(frame)
	case "TOOL_CALL_START":
		t.onToolStart(frame)
	case "TOOL_CALL_ARGS":
		t.onToolArgs(frame)
	case "TOOL_CALL_END":
		t.onToolEnd()
	case "TOOL_CALL_RESULT":
		t.onToolResult(frame)
	case "RUN_ERROR":
		t.onRunError(frame)
	case "RUN_FINISHED":
		t.onRunFinished(frame)
	}
}

// deliverText pushes one rendered message out through the chat's channel.
func (t *threadChat) deliverText(content string) {
	if content == "" {
		return
	}
	out := chn.OutboundMessage{
		ChannelName: t.channel.Name(),
		RecipientID: t.recipient,
		Content:     content,
		Timestamp:   time.Now(),
	}
	if err := t.channel.Send(t.ctx, out); err != nil {
		logger.Error("failed to send the chat render", append(t.tags(), "channel", t.channel.Name(), "error", err)...)
	}
}

// senderKey is the key the channel manager tracks the chat's interrupts
// under: the channel name and the recipient.
func (t *threadChat) senderKey() string {
	return fmt.Sprintf("%s-%s", t.channel.Name(), t.recipient)
}

// tags are the log tags carrying the chat thread's identity.
func (t *threadChat) tags() []any {
	return []any{"project_dir", t.projectDir, "conversation_id", t.sessionID}
}

// onRunFinished answers the interrupts a suspended run ends on with one run
// input of resume entries, so the continuation run carries the decisions.
func (t *threadChat) onRunFinished(frame []byte) {
	var ev struct {
		Outcome struct {
			Type       string           `json:"type"`
			Interrupts []agui.Interrupt `json:"interrupts"`
		} `json:"outcome"`
	}
	if json.Unmarshal(frame, &ev) != nil || ev.Outcome.Type != "interrupt" {
		return
	}
	resume := make([]agui.ResumeEntry, 0, len(ev.Outcome.Interrupts))
	for _, interrupt := range ev.Outcome.Interrupts {
		resume = append(resume, agui.ResumeEntry{InterruptID: interrupt.ID, Status: t.answer(interrupt)})
	}
	if err := t.sendFrame(t.ctx, encodeThreadFrame(t, t.runInput(nil, resume))); err != nil {
		logger.Error("failed to answer an interrupt", append(t.tags(), "recipient", t.recipient, "error", err)...)
	}
}

// answer decides one interrupt. A tool approval goes to the chat's user the
// way the subprocess run did - the rich buttons when the channel has them, a
// text prompt otherwise - unless channels.require_approval is off, which
// cancels without prompting so gated tools stay blocked. A question is
// cancelled: a chat has no form to fill, so the tool takes its dismissed path.
func (t *threadChat) answer(interrupt agui.Interrupt) agui.ResumeStatus {
	req, known := t.calls[interrupt.ToolCallID]
	if interrupt.Reason != agui.InterruptToolCall || !known || !t.manager.cfg.RequireApproval {
		return agui.ResumeStatusCancelled
	}
	if t.manager.resolveApproval(t.ctx, t.senderKey(), req, t.deliverText, t.channel).Approved {
		return agui.ResumeStatusResolved
	}
	return agui.ResumeStatusCancelled
}
