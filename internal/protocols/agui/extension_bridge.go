package agui

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	websocket "github.com/gorilla/websocket"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	sessionsdomain "github.com/inference-gateway/cli/internal/sessions/domain"
)

var errBrowserExtensionDisconnected = errors.New("the browser extension disconnected before it answered")

const (
	inboundBrowserHello     = "browser_hello"
	inboundBrowserResult    = "browser_result"
	outboundBrowserHelloAck = "browser_hello_ack"
)

const (
	clientExtension = "extension"
	clientDesktop   = "desktop"
)

// protocolVersion is the wire contract version the hello ack reports. The
// handshake is lenient: clients show an update state on a mismatch, and the
// bridge never rejects a hello over its version.
const protocolVersion = 1

// connWriteTimeout bounds one frame write, so a stalled client is dropped
// instead of stalling the thread that feeds it.
const connWriteTimeout = 10 * time.Second

// outboundQueue caps the frames a connection owes its client. A client must
// read its socket at least that fast, or it is closed for a reconnect.
const outboundQueue = 64

type extHello struct {
	Type             string          `json:"type"`
	Token            string          `json:"token"`
	Client           string          `json:"client"`
	ProtocolVersion  json.RawMessage `json:"protocol_version"`
	ExtensionVersion string          `json:"extension_version"`
}

type extHelloAck struct {
	Type            string `json:"type"`
	ProtocolVersion int    `json:"protocol_version"`
}

// Deps are the bridge's collaborators. Threads is set by infer daemon, which
// relays every client frame to the sessions context. It is nil in infer chat,
// where the bridge only carries browser commands.
type Deps struct {
	Extension    config.ExtensionConfig
	Notifier     agentdomain.UINotifier
	ArtifactsDir string
	Threads      sessionsdomain.Threads
}

// extConn is one authenticated client connection. It is the sessions Client its
// thread delivers worker lines to, one frame each.
type extConn struct {
	ws        *websocket.Conn
	client    string
	out       chan []byte
	mu        sync.Mutex
	done      chan struct{}
	closeOnce sync.Once
}

// newExtConn wraps one upgraded socket and starts its writer goroutine, so
// Deliver only queues frames and never waits on the socket.
func newExtConn(ws *websocket.Conn, client string) *extConn {
	c := &extConn{ws: ws, client: client, out: make(chan []byte, outboundQueue), done: make(chan struct{})}
	go c.writeLoop()
	return c
}

func (c *extConn) send(frame []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.ws.SetWriteDeadline(time.Now().Add(connWriteTimeout))
	return c.ws.WriteMessage(websocket.TextMessage, frame)
}

// Deliver queues one frame for the connection's writer, so a stalled client
// never holds back the thread's other clients. An overflow closes the
// connection, since it means the client stopped reading its socket, and it
// may reconnect for a fresh snapshot. A closed connection drops the frame.
func (c *extConn) Deliver(frame []byte) {
	select {
	case <-c.done:
		return
	default:
	}
	select {
	case c.out <- frame:
	default:
		logger.Warn("extension bridge closed a client whose outbound queue overflowed", "client", c.client)
		c.close()
	}
}

// writeLoop is the connection's dedicated writer: it drains the Deliver
// queue, each frame bounded by connWriteTimeout, and closes the connection
// when a write fails.
func (c *extConn) writeLoop() {
	for {
		select {
		case frame := <-c.out:
			if err := c.send(frame); err != nil {
				logger.Debug("extension bridge dropped a client after a failed write", "client", c.client, "error", err)
				c.close()
				return
			}
		case <-c.done:
			return
		}
	}
}

func (c *extConn) close() {
	c.closeOnce.Do(func() {
		close(c.done)
		_ = c.ws.Close()
	})
}

// ExtensionBridge hosts the localhost AG-UI WebSocket binding. It owns the
// listener, the Origin check and the token handshake. It keeps one extension
// connection, which Request drives as the browser driver's command/result RPC,
// and any number of desktop connections. Other frames go to Threads.
type ExtensionBridge struct {
	extension    config.ExtensionConfig
	notifier     agentdomain.UINotifier
	artifactsDir string
	threads      sessionsdomain.Threads

	server   *http.Server
	addr     string
	startErr error
	startMu  sync.Mutex
	mu       sync.Mutex
	ext      *extConn
	conns    map[*extConn]struct{}
	pending  map[string]chan json.RawMessage
}

// NewExtensionBridge builds the bridge. artifactsDir, when non-empty, is served
// read-only at /artifacts/ so the panel can display generated images the agent
// saved locally.
func NewExtensionBridge(deps Deps) *ExtensionBridge {
	notifier := deps.Notifier
	if notifier == nil {
		notifier = agentdomain.NoopUINotifier{}
	}
	return &ExtensionBridge{
		extension:    deps.Extension,
		notifier:     notifier,
		artifactsDir: deps.ArtifactsDir,
		threads:      deps.Threads,
		conns:        make(map[*extConn]struct{}),
		pending:      make(map[string]chan json.RawMessage),
	}
}

// Start listens on 127.0.0.1:<port> and serves the /ws endpoint. Errors are
// also stored so Request surfaces them instead of a silent no-op.
func (b *ExtensionBridge) Start() error {
	if b.extension.Token == "" {
		b.startErr = errors.New("browser_use.extension.token is empty - set a shared secret in browser_use.yaml and in the opentask extension options")
		return b.startErr
	}

	addr := fmt.Sprintf("127.0.0.1:%d", b.extension.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		b.startErr = fmt.Errorf("extension bridge failed to listen on %s: %w", addr, err)
		return b.startErr
	}

	b.addr = listener.Addr().String()
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", b.handleWS)
	if b.artifactsDir != "" {
		mux.Handle("/artifacts/", http.StripPrefix("/artifacts/", http.FileServer(http.Dir(b.artifactsDir))))
	}
	b.server = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := b.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Warn("extension bridge server stopped", "error", err)
		}
	}()
	logger.Info("extension bridge listening", "addr", addr)
	return nil
}

// retryStart re-runs Start when the previous attempt failed and returns the
// resulting start error, nil once the bridge is listening.
func (b *ExtensionBridge) retryStart() error {
	b.startMu.Lock()
	defer b.startMu.Unlock()
	if b.startErr == nil {
		return nil
	}
	b.startErr = nil
	return b.Start()
}

// Addr returns the actual listen address (useful with port 0 in tests).
func (b *ExtensionBridge) Addr() string {
	return b.addr
}

var extUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		return origin == "" ||
			strings.HasPrefix(origin, "chrome-extension://") ||
			strings.HasPrefix(origin, "moz-extension://") ||
			strings.HasPrefix(origin, "safari-web-extension://")
	},
}

func (b *ExtensionBridge) handleWS(w http.ResponseWriter, r *http.Request) {
	ws, err := extUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	var hello extHello
	if err := ws.ReadJSON(&hello); err != nil || hello.Type != inboundBrowserHello ||
		subtle.ConstantTimeCompare([]byte(hello.Token), []byte(b.extension.Token)) != 1 {
		logger.Warn("extension bridge rejected a connection with a bad or missing hello")
		_ = ws.Close()
		return
	}
	_ = ws.SetReadDeadline(time.Time{})

	client := clientKind(hello.Client)
	if len(hello.ProtocolVersion) == 0 {
		logger.Warn("extension bridge accepted a hello without protocol_version", "client", client, "version", hello.ExtensionVersion)
	}
	if err := ws.WriteJSON(extHelloAck{Type: outboundBrowserHelloAck, ProtocolVersion: protocolVersion}); err != nil {
		_ = ws.Close()
		return
	}
	logger.Info("extension bridge client connected", "client", client, "version", hello.ExtensionVersion, "protocol_version", string(hello.ProtocolVersion))
	b.adopt(newExtConn(ws, client))
}

// clientKind maps the hello's client field to a known kind. Anything but a
// desktop is the extension, which is what hellos without the field come from.
func clientKind(client string) string {
	if client == clientDesktop {
		return clientDesktop
	}
	return clientExtension
}

// adopt registers c and starts its pump goroutines. A new extension replaces
// the previous one (MV3 service workers restart at will), while desktop
// connections accumulate.
func (b *ExtensionBridge) adopt(c *extConn) {
	isExtension := c.client == clientExtension
	b.mu.Lock()
	b.conns[c] = struct{}{}
	replaced := b.ext
	if isExtension {
		b.ext = c
		b.failPendingLocked()
	}
	b.mu.Unlock()

	if isExtension {
		if replaced != nil {
			replaced.close()
		}
		b.notifyConnected(true)
	}
	go b.readLoop(c)
	go b.pingLoop(c)
}

// readLoop handles frames from c until the connection dies or is replaced.
func (b *ExtensionBridge) readLoop(c *extConn) {
	defer b.dropConn(c)
	for {
		_, raw, err := c.ws.ReadMessage()
		if err != nil {
			return
		}
		b.route(c, raw)
	}
}

// route hands browser_result frames to the Request waiting on them, with their
// raw bytes intact, and every other frame to Threads when the bridge relays.
func (b *ExtensionBridge) route(c *extConn, raw []byte) {
	var msg struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil {
		logger.Debug("extension bridge dropped an undecodable frame", "error", err)
		return
	}
	if msg.Type == inboundBrowserResult {
		b.deliverBrowserResult(msg.ID, raw)
		return
	}
	if b.threads != nil {
		b.threads.Handle(c, raw)
	}
}

// deliverBrowserResult hands a browser_result frame to the goroutine waiting
// for that command id.
func (b *ExtensionBridge) deliverBrowserResult(id string, raw []byte) {
	b.mu.Lock()
	ch, ok := b.pending[id]
	delete(b.pending, id)
	b.mu.Unlock()
	if ok {
		ch <- raw
	}
}

func (b *ExtensionBridge) pingLoop(c *extConn) {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			if err := c.ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
				return
			}
		}
	}
}

// dropConn forgets c, closes it and detaches it from its thread. Pending
// browser requests fail when c was the extension.
func (b *ExtensionBridge) dropConn(c *extConn) {
	b.mu.Lock()
	delete(b.conns, c)
	wasExtension := b.ext == c
	if wasExtension {
		b.ext = nil
		b.failPendingLocked()
	}
	b.mu.Unlock()

	c.close()
	if b.threads != nil {
		b.threads.Detach(c)
	}
	if wasExtension {
		b.notifyConnected(false)
	}
}

// notifyConnected tells the TUI status bar whether an extension is attached.
func (b *ExtensionBridge) notifyConnected(connected bool) {
	b.notifier.Notify(agentdomain.BrowserExtensionStatusEvent{Connected: connected})
}

// failPendingLocked releases every Request still waiting on a connection that
// is gone, so callers get an immediate error instead of waiting out their
// deadline. The caller holds b.mu, which keeps it atomic with the conn swap.
func (b *ExtensionBridge) failPendingLocked() {
	for id, ch := range b.pending {
		close(ch)
		delete(b.pending, id)
	}
}

// Request writes one extension frame and waits for the browser_result carrying
// id, returning that frame's raw JSON. A failed listen is retried first, so a
// process that lost the port to another infer takes it over once that one
// exits. The caller owns the id, the frame (including its timeout_ms), and the
// deadline via ctx.
func (b *ExtensionBridge) Request(ctx context.Context, id string, frame json.RawMessage) (json.RawMessage, error) {
	if err := b.retryStart(); err != nil {
		return nil, err
	}

	b.mu.Lock()
	ext := b.ext
	if ext == nil {
		b.mu.Unlock()
		return nil, fmt.Errorf("no browser extension connected on port %d - install the opentask extension and set its bridge port/token to match browser_use.yaml", b.extension.Port)
	}
	ch := make(chan json.RawMessage, 1)
	b.pending[id] = ch
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		delete(b.pending, id)
		b.mu.Unlock()
	}()

	if err := ext.send(frame); err != nil {
		return nil, fmt.Errorf("failed to send the command to the browser extension: %w", err)
	}

	select {
	case result, ok := <-ch:
		if !ok {
			return nil, errBrowserExtensionDisconnected
		}
		return result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Close shuts the server and every connection down. Each read loop then ends
// and detaches its connection.
func (b *ExtensionBridge) Close() {
	if b.server != nil {
		_ = b.server.Close()
	}
	b.mu.Lock()
	conns := slices.Collect(maps.Keys(b.conns))
	b.ext = nil
	b.failPendingLocked()
	b.mu.Unlock()
	for _, c := range conns {
		c.close()
	}
}
