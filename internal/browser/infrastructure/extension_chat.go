package infrastructure

import (
	"bytes"
	"sync"
	"sync/atomic"

	websocket "github.com/gorilla/websocket"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
)

// approvals holds the agent-approval cards the panel has not answered yet, so a
// resolved approval can clear them and a stale reply is ignored.
type approvals struct {
	write    frameWriter
	notifier agentdomain.UINotifier

	mu      sync.Mutex
	pending map[string]sdk.ChatCompletionMessageToolCall
}

func newApprovals(write frameWriter, notifier agentdomain.UINotifier) *approvals {
	return &approvals{
		write:    write,
		notifier: notifier,
		pending:  make(map[string]sdk.ChatCompletionMessageToolCall),
	}
}

// reset clears the approval cards of the previous connection.
func (a *approvals) reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pending = make(map[string]sdk.ChatCompletionMessageToolCall)
}

// request stashes the pending tool call and asks the panel to decide.
func (a *approvals) request(conn *websocket.Conn, req agentdomain.ToolApprovalRequestedEvent) {
	a.mu.Lock()
	a.pending[req.RequestID] = req.ToolCall
	a.mu.Unlock()
	a.write(conn, extApprovalRequest{
		Type:      outboundApprovalRequest,
		RequestID: req.RequestID,
		ToolName:  req.ToolCall.Function.Name,
		ToolArgs:  req.ToolCall.Function.Arguments,
	})
}

// resolvePending clears any outstanding approval cards on a
// ToolApprovalResolvedEvent. A duplicate approval_resolved is harmless - the
// panel ignores unknown ids, and the panel path already cleared the card.
func (a *approvals) resolvePending(conn *websocket.Conn) {
	a.mu.Lock()
	if len(a.pending) == 0 {
		a.mu.Unlock()
		return
	}
	ids := make([]string, 0, len(a.pending))
	for id := range a.pending {
		ids = append(ids, id)
	}
	a.pending = make(map[string]sdk.ChatCompletionMessageToolCall)
	a.mu.Unlock()
	for _, id := range ids {
		a.write(conn, extApprovalResolved{Type: outboundApprovalResolved, RequestID: id})
	}
}

// answer turns a panel approval_response into the same ToolApprovalResponseEvent
// the terminal emits, then confirms the card cleared.
func (a *approvals) answer(conn *websocket.Conn, requestID, action string) {
	a.mu.Lock()
	toolCall, ok := a.pending[requestID]
	delete(a.pending, requestID)
	a.mu.Unlock()
	if !ok {
		return
	}
	a.notifier.Notify(agentdomain.ToolApprovalResponseEvent{
		Action:   approvalAction(action),
		ToolCall: toolCall,
	})
	a.write(conn, extApprovalResolved{Type: outboundApprovalResolved, RequestID: requestID})
}

// approvalAction maps the wire action to a decision. Anything but "approve"
// (including unknown values) is treated as a reject, failing safe.
func approvalAction(action string) agentdomain.ApprovalAction {
	if action == approvalActionApprove {
		return agentdomain.ApprovalApprove
	}
	return agentdomain.ApprovalReject
}

// chatMirror mirrors the active turn to the panel: chat events as chat_event
// frames, approval events as the panel's approval handshake, and an interrupt
// frame cancels the turn the events belong to.
type chatMirror struct {
	write     frameWriter
	events    agentdomain.EventBridge
	repo      convdomain.ConversationRepository
	approvals *approvals
	agent     agentdomain.AgentService
	sessionID string

	activeRequestID atomic.Value
}

func newChatMirror(write frameWriter, deps Deps, approvals *approvals) *chatMirror {
	return &chatMirror{
		write:     write,
		events:    deps.Events,
		repo:      deps.Conversations,
		approvals: approvals,
		agent:     deps.Agent,
		sessionID: deps.SessionID,
	}
}

// run mirrors chat events to the extension until the connection dies, so the
// panel sees the same turn the TUI does.
func (m *chatMirror) run(conn *websocket.Conn, stop chan struct{}) {
	sub := m.events.SubscribeFuture()
	defer m.events.Unsubscribe(sub)

	filtered := make(chan agentdomain.ChatEvent, 100)
	go func() {
		defer close(filtered)
		for {
			select {
			case <-stop:
				return
			case ev, ok := <-sub:
				if !ok {
					return
				}
				forwarded, deliver := m.forward(conn, ev)
				if !deliver {
					continue
				}
				select {
				case filtered <- forwarded:
				case <-stop:
					return
				}
			}
		}
	}()

	writer := &chatEventWriter{write: m.write, conn: conn}
	if err := agui.Render(filtered, writer, nil, nil, m.sessionID, "", m.repo, nil); err != nil {
		logger.Debug("extension bridge chat pump ended", "error", err)
	}
}

// forward tracks the turn the event belongs to, answers the events the panel
// drives itself, and reports whether the event should still reach the panel.
func (m *chatMirror) forward(conn *websocket.Conn, ev agentdomain.ChatEvent) (agentdomain.ChatEvent, bool) {
	m.track(conn, ev)
	switch e := ev.(type) {
	case agentdomain.ToolApprovalRequestedEvent:
		m.approvals.request(conn, e)
		return nil, false
	case agentdomain.ToolApprovalResolvedEvent:
		m.approvals.resolvePending(conn)
		return nil, false
	case agentdomain.UserQuestionRequestedEvent:
		return displayOnlyQuestion(e), true
	}
	return ev, true
}

// track keeps the in-flight request id current so interrupt can cancel it, and
// tells the panel when a cancelled turn ends without a text message end.
func (m *chatMirror) track(conn *websocket.Conn, ev agentdomain.ChatEvent) {
	switch e := ev.(type) {
	case agentdomain.ChatStartEvent:
		m.activeRequestID.Store(e.RequestID)
	case agentdomain.ChatCompleteEvent:
		if len(e.ToolCalls) == 0 || e.Cancelled {
			m.activeRequestID.Store("")
		}
		if e.Cancelled {
			m.write(conn, extInterrupted{Type: outboundInterrupted})
		}
	}
}

// interrupt cancels the chat turn currently streaming, if any. It is idempotent,
// and a stale or unknown id is a no-op in AgentService.CancelRequest.
func (m *chatMirror) interrupt() {
	id, _ := m.activeRequestID.Load().(string)
	if id == "" {
		return
	}
	_ = m.agent.CancelRequest(id)
}

// displayOnlyQuestion strips the question's response channel so the panel can
// show it but only the TUI, which owns the channel, answers or dismisses it.
func displayOnlyQuestion(question agentdomain.UserQuestionRequestedEvent) agentdomain.UserQuestionRequestedEvent {
	question.ResponseChan = nil
	return question
}

// chatEventWriter adapts the AG-UI line stream to chat_event frames.
type chatEventWriter struct {
	write frameWriter
	conn  *websocket.Conn
	buf   []byte
}

func (w *chatEventWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		idx := bytes.IndexByte(w.buf, '\n')
		if idx < 0 {
			return len(p), nil
		}
		line := bytes.Clone(w.buf[:idx])
		w.buf = w.buf[idx+1:]
		if len(line) > 0 {
			w.write(w.conn, extChatEvent{Type: outboundChatEvent, Event: line})
		}
	}
}
