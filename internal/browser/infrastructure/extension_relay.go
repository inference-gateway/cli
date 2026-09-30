package infrastructure

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
)

var errBrowserExtensionDisconnected = errors.New("the browser extension disconnected before it answered")

// noExtensionConnected opens the error a command gets when no extension is
// attached. A browser client reads it back to move its status indicator.
const noExtensionConnected = "no browser extension connected"

// extensionVersionAttr is the hello field naming the extension's version.
const extensionVersionAttr = "extension_version"

// ExtensionRelay is the host end of the browser RPC on a binding. It keeps the
// one extension connection and drives every browser_command through it, one at
// a time, answering each with the browser_result carrying its id.
type ExtensionRelay struct {
	port     int
	notifier agentdomain.UINotifier

	mu      sync.Mutex
	ext     *agui.Conn
	pending map[string]chan json.RawMessage
	// clients are the other connections on the binding, which the frame about
	// the extension's state goes to.
	clients map[*agui.Conn]struct{}
	// busy gates browser commands at one: every source the host routes drives
	// the one browser.
	// ponytail: a command runs to its own deadline even when its sender gave
	// up, holding the gate up to timeout_ms plus the margin. Add a cancel frame
	// if abandoned commands stall other threads.
	busy chan struct{}
}

// NewExtensionRelay builds the relay for the binding on the extension's port.
// notifier, when set, learns whether an extension is attached.
func NewExtensionRelay(ext config.ExtensionConfig, notifier agentdomain.UINotifier) *ExtensionRelay {
	if notifier == nil {
		notifier = agentdomain.NoopUINotifier{}
	}
	return &ExtensionRelay{
		port:     ext.Port,
		notifier: notifier,
		pending:  make(map[string]chan json.RawMessage),
		clients:  make(map[*agui.Conn]struct{}),
		busy:     make(chan struct{}, 1),
	}
}

// Attach takes a connection over. A new extension replaces the previous one,
// since MV3 service workers restart at will, and every other client joins the
// ones the state frames go to, which learn the current state at once.
func (r *ExtensionRelay) Attach(conn *agui.Conn) {
	if !isExtension(conn.Kind()) {
		r.attachClient(conn)
		return
	}
	logger.Info("browser extension attached", "extension_version", conn.HelloAttr(extensionVersionAttr))
	r.mu.Lock()
	replaced := r.ext
	r.ext = conn
	r.failPendingLocked()
	r.broadcastLocked(browserExtensionStatusFrame(true, conn.HelloAttr(extensionVersionAttr)))
	r.mu.Unlock()

	if replaced != nil {
		replaced.Close()
	}
	r.notifyConnected(true)
}

// attachClient welcomes one non-extension client with the frame naming the
// extension's current state.
func (r *ExtensionRelay) attachClient(conn *agui.Conn) {
	r.mu.Lock()
	r.clients[conn] = struct{}{}
	conn.Deliver(r.statusLocked())
	r.mu.Unlock()
}

// statusLocked builds the frame naming the relay's current extension state.
func (r *ExtensionRelay) statusLocked() []byte {
	if r.ext == nil {
		return browserExtensionStatusFrame(false, "")
	}
	return browserExtensionStatusFrame(true, r.ext.HelloAttr(extensionVersionAttr))
}

// broadcastLocked delivers one frame to every client, the extension's state
// being the thing they follow. Deliver only queues, so it stays under r.mu and
// every client's frames leave in the order the state changed.
func (r *ExtensionRelay) broadcastLocked(frame []byte) {
	for client := range r.clients {
		client.Deliver(frame)
	}
}

// Detach fails the commands still waiting on an extension connection that
// died, and stops framing the state to the connection when it was a client.
func (r *ExtensionRelay) Detach(conn *agui.Conn) {
	r.mu.Lock()
	wasExtension := r.ext == conn
	if wasExtension {
		r.ext = nil
		r.failPendingLocked()
		r.broadcastLocked(browserExtensionStatusFrame(false, ""))
	}
	delete(r.clients, conn)
	r.mu.Unlock()

	if wasExtension {
		logger.Info("browser extension detached", "extension_version", conn.HelloAttr(extensionVersionAttr))
		r.notifyConnected(false)
	}
}

// Handle claims the browser frames: a browser_result resolves the command
// waiting on it, and a browser_command a client posted is driven through the
// extension and answered on the posting connection. It reports false for every
// other frame. The extension never posts commands, being the other end.
func (r *ExtensionRelay) Handle(conn *agui.Conn, frame []byte) bool {
	var msg struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	if json.Unmarshal(frame, &msg) != nil {
		return false
	}
	switch {
	case msg.Type == frameBrowserResult:
		r.deliverResult(msg.ID, frame)
		return true
	case msg.Type == frameBrowserCommand && !isExtension(conn.Kind()):
		go func() { conn.Deliver(r.Relay(context.Background(), frame)) }()
		return true
	}
	return false
}

// deliverResult hands a browser_result to the goroutine waiting for that
// command id. The send stays under r.mu, which keeps it atomic with
// failPendingLocked closing the remaining queues.
func (r *ExtensionRelay) deliverResult(id string, frame []byte) {
	r.mu.Lock()
	ch, ok := r.pending[id]
	delete(r.pending, id)
	if ok {
		ch <- frame
	}
	r.mu.Unlock()
}

func (r *ExtensionRelay) notifyConnected(connected bool) {
	r.notifier.Notify(agentdomain.BrowserExtensionStatusEvent{Connected: connected})
}

// failPendingLocked releases every Relay still waiting on a connection that
// is gone, so callers get an immediate error instead of waiting out their
// deadline. The caller holds r.mu, which keeps it atomic with the conn swap.
func (r *ExtensionRelay) failPendingLocked() {
	for id, ch := range r.pending {
		close(ch)
		delete(r.pending, id)
	}
}

// browserCommandMeta is the part of a browser_command the relay needs: the id
// it waits on, the action the timeout wording names, and the per-action budget.
type browserCommandMeta struct {
	ID        string `json:"id"`
	Action    string `json:"action"`
	TimeoutMs int    `json:"timeout_ms"`
}

func parseBrowserCommand(frame []byte) browserCommandMeta {
	var meta browserCommandMeta
	_ = json.Unmarshal(frame, &meta)
	return meta
}

// commandDeadline is how long the relay waits for a command's answer: the
// frame's timeout_ms, or the driver's default, plus the reply margin.
func commandDeadline(meta browserCommandMeta) time.Duration {
	if meta.TimeoutMs <= 0 {
		return defaultActionTimeoutSeconds*time.Second + commandReplyMargin
	}
	return time.Duration(meta.TimeoutMs)*time.Millisecond + commandReplyMargin
}

// Relay drives one browser_command through the extension and always answers
// with the browser_result carrying its id. A failure becomes the result's
// error, because the sender waits for the answer by id.
func (r *ExtensionRelay) Relay(ctx context.Context, frame []byte) []byte {
	result, err := r.relay(ctx, frame)
	if err != nil {
		return browserResultError(parseBrowserCommand(frame).ID, err)
	}
	return result
}

// relay forwards the frame to the connected extension behind the one-browser
// gate. A context without a deadline gets the command's own. Errors keep the
// wording the ExtensionDriver surfaces.
func (r *ExtensionRelay) relay(ctx context.Context, frame []byte) (json.RawMessage, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	meta := parseBrowserCommand(frame)
	if _, ok := ctx.Deadline(); !ok {
		derived, cancel := context.WithTimeout(ctx, commandDeadline(meta))
		defer cancel()
		ctx = derived
	}

	select {
	case r.busy <- struct{}{}:
		defer func() { <-r.busy }()
	case <-ctx.Done():
		return nil, commandTimedOut(meta.Action, ctx.Err())
	}

	r.mu.Lock()
	ext := r.ext
	if ext == nil {
		r.mu.Unlock()
		logger.Warn("browser command has no extension to route through", "port", r.port, "action", meta.Action)
		return nil, fmt.Errorf("%s on port %d - install the opentask extension and set its bridge port/token to match browser_use.yaml", noExtensionConnected, r.port)
	}
	ch := make(chan json.RawMessage, 1)
	r.pending[meta.ID] = ch
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		delete(r.pending, meta.ID)
		r.mu.Unlock()
	}()

	if err := ext.Send(frame); err != nil {
		return nil, fmt.Errorf("failed to send the command to the browser extension: %w", err)
	}

	select {
	case result, ok := <-ch:
		if !ok {
			return nil, errBrowserExtensionDisconnected
		}
		return result, nil
	case <-ctx.Done():
		return nil, commandTimedOut(meta.Action, ctx.Err())
	}
}

// commandTimedOut words a deadline the way the Browser tools report it.
func commandTimedOut(action string, err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("timed out waiting for the browser extension to %s - is the opentask extension still running?", action)
	}
	return err
}

// browserResultError builds the browser_result a failed relay reports, keeping
// the command's id so the sender's pending request resolves with the error.
func browserResultError(id string, relayErr error) []byte {
	data, _ := json.Marshal(map[string]string{
		"type":  frameBrowserResult,
		"id":    id,
		"error": relayErr.Error(),
	})
	return data
}
