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

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	chn "github.com/inference-gateway/cli/internal/channels"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	ipc "github.com/inference-gateway/cli/internal/platform/ipc"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
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
	// stops. The render loop drains it; a full buffer drops the frame
	// rather than freezing the registry's pump for every follower.
	frames chan []byte

	// mu guards the idle bookkeeping: active is the time of the last
	// frame in either direction, and detouch detaches the chat on idle
	// so the registry may reap its worker.
	mu     sync.Mutex
	active time.Time
	detach *time.Timer

	// render state, owned by the render loop: the open assistant
	// message's deltas and its role, the tool calls rendered for the
	// next flush, and the tool call being streamed.
	message  strings.Builder
	role     string
	tools    []string
	toolName string
	toolArgs string
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
	}
	t.markActive()
	cm.threadChats.Store(senderKey, t)
	go t.renderLoop()
	return t
}

// threadFrame is one chat-built client frame for the registry: the
// resume_conversation that makes the chat follow its sender's thread, the
// user_message that starts its next turn with the images as attachments,
// and the interrupt answers. The thread options apply when the worker
// launches.
type threadFrame struct {
	Type        string                        `json:"type"`
	ID          string                        `json:"id,omitempty"`
	ProjectDir  string                        `json:"project_dir,omitempty"`
	Content     string                        `json:"content,omitempty"`
	Attachments []agentdomain.ImageAttachment `json:"attachments,omitempty"`
	sessionsdomain.ThreadOptions
}

const frameResume = "resume_conversation"

// frameBuffer is how many undelivered worker frames a busy chat's render
// loop may owe before further frames drop.
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
// per sender (idempotent on the registry), and the user_message frame
// carries the text with the images as attachments for the worker to turn
// into content parts. No subprocess is spawned per message.
func (t *threadChat) deliverUserMessage(ctx context.Context, msg chn.InboundMessage, projectDir string, opts sessionsdomain.ThreadOptions) error {
	frames := [][]byte{
		encodeThreadFrame(t, threadFrame{Type: frameResume, ID: t.sessionID, ProjectDir: projectDir, ThreadOptions: opts}),
		encodeThreadFrame(t, threadFrame{Type: "user_message", Content: msg.Content, Attachments: msg.Images}),
	}
	for _, frame := range frames {
		if err := t.sendFrame(ctx, frame); err != nil {
			return err
		}
	}
	return nil
}

// encodeThreadFrame marshals one frame; a thread frame carries flat scalar
// fields only, so a failure is a programming error logged and sent as the
// registry's rejection answer.
func encodeThreadFrame(t *threadChat, frame threadFrame) []byte {
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

// renderLoop renders the worker frames into channel messages until the
// daemon's context ends.
func (t *threadChat) renderLoop() {
	for {
		select {
		case <-t.ctx.Done():
			return
		case frame, open := <-t.frames:
			if !open {
				return
			}
			t.render(frame)
		}
	}
}

// render decodes one worker frame and answers the interrupts the way the
// other clients answer them: approvals with an approval_response, questions
// with a dismissal. Unknown frames are dropped, the way the desktop client
// skips what it cannot render.
func (t *threadChat) render(frame []byte) {
	var envelope struct {
		Type  string          `json:"type"`
		Name  string          `json:"name"`
		Value json.RawMessage `json:"value"`
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
	case "CUSTOM":
		switch envelope.Name {
		case "approval_request":
			t.renderApproval(envelope.Value)
		case "user_question_request":
			t.answerQuestion(envelope.Value)
		}
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

// renderApproval surfaces one approval interrupt to the chat's user the
// way the subprocess run did - the rich buttons when the channel has them,
// a text prompt otherwise, and the reply the user taps or types. With
// channels.require_approval off the chat answers cancelled without
// prompting, so gated tools stay blocked.
func (t *threadChat) renderApproval(value json.RawMessage) {
	var req ipc.ApprovalRequest
	if json.Unmarshal(value, &req) != nil || req.ToolCallID == "" {
		return
	}
	if !t.manager.cfg.RequireApproval {
		t.sendInterrupt(ipc.ApprovalResponse{
			Type:       "approval_response",
			ToolCallID: req.ToolCallID,
			Approved:   false,
		})
		return
	}
	resp := t.manager.resolveApproval(t.ctx, t.senderKey(), req, t.deliverText, t.channel)
	t.sendInterrupt(resp)
}

// answerQuestion dismisses one AskUserQuestion interrupt: a chat has no
// form to fill, so the tool takes its dismissed path, the answer the other
// clients use to close the form without running it.
func (t *threadChat) answerQuestion(value json.RawMessage) {
	var req struct {
		ToolCallID string `json:"tool_call_id"`
	}
	if json.Unmarshal(value, &req) != nil || req.ToolCallID == "" {
		return
	}
	t.sendInterrupt(ipc.UserQuestionResponse{
		Type:       "user_question_response",
		ToolCallID: req.ToolCallID,
		Cancelled:  true,
	})
}

// sendInterrupt routes one interrupt answer frame to the chat's thread.
func (t *threadChat) sendInterrupt(resp any) {
	frame, err := json.Marshal(resp)
	if err != nil {
		logger.Error("failed to marshal an interrupt answer", append(t.tags(), "recipient", t.recipient, "error", err)...)
		return
	}
	if err := t.sendFrame(t.ctx, frame); err != nil {
		logger.Error("failed to answer an interrupt", append(t.tags(), "recipient", t.recipient, "error", err)...)
	}
}
