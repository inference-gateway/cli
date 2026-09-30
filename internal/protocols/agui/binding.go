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
	"sync"
	"time"

	websocket "github.com/gorilla/websocket"

	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

// protocolVersion is the wire contract version the hello and its ack report.
// The handshake is lenient: clients show an update state on a mismatch, and the
// binding never rejects a hello over its version.
const protocolVersion = 1

// handshakeTimeout bounds the hello and its ack on a fresh socket.
const handshakeTimeout = 5 * time.Second

// connWriteTimeout bounds one frame write, so a stalled peer is dropped instead
// of stalling whoever feeds it.
const connWriteTimeout = 10 * time.Second

// pingInterval keeps an idle connection alive.
const pingInterval = 20 * time.Second

// outboundQueue caps the frames a connection owes its peer. A peer must read
// its socket at least that fast, or it is closed for a reconnect.
const outboundQueue = 64

// protocolVersionField is the hello field naming the wire contract version.
const protocolVersionField = "protocol_version"

// Handshake names the first frame a client sends and the frame that acks it.
type Handshake struct {
	Hello string
	Ack   string
}

type helloFrame struct {
	Type            string          `json:"type"`
	Token           string          `json:"token"`
	Client          string          `json:"client"`
	ProtocolVersion json.RawMessage `json:"protocol_version"`
}

type helloAckFrame struct {
	Type            string `json:"type"`
	ProtocolVersion int    `json:"protocol_version"`
}

// Handler receives what happens on a binding's connections. Attach runs once a
// connection authenticated and before its first frame, Handle once per frame,
// and Detach after the connection died.
type Handler interface {
	Attach(conn *Conn)
	Handle(conn *Conn, frame []byte)
	Detach(conn *Conn)
}

// Conn is one authenticated connection, on either end of the binding.
type Conn struct {
	ws   *websocket.Conn
	kind string
	// helloAttrs are the other fields the hello declared, client facts like a
	// version the binding itself never interprets. The token is never among
	// them.
	helloAttrs map[string]string
	out        chan []byte
	mu         sync.Mutex
	done       chan struct{}
	closeOnce  sync.Once
}

// newConn wraps one authenticated socket and starts its writer goroutine, so
// Deliver only queues frames and never waits on the socket.
func newConn(ws *websocket.Conn, kind string, helloAttrs map[string]string) *Conn {
	c := &Conn{ws: ws, kind: kind, helloAttrs: helloAttrs, out: make(chan []byte, outboundQueue), done: make(chan struct{})}
	go c.writeLoop()
	return c
}

// Kind is the client kind the hello declared. The binding never interprets it.
func (c *Conn) Kind() string {
	return c.kind
}

// HelloAttr is one other field the hello declared, empty when the hello
// omitted it.
func (c *Conn) HelloAttr(key string) string {
	return c.helloAttrs[key]
}

// Send writes one frame and reports the write's failure.
func (c *Conn) Send(frame []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.ws.SetWriteDeadline(time.Now().Add(connWriteTimeout))
	return c.ws.WriteMessage(websocket.TextMessage, frame)
}

// Deliver queues one frame for the connection's writer, so a stalled peer never
// holds back the others. An overflow closes the connection, since it means the
// peer stopped reading its socket. A closed connection drops the frame.
func (c *Conn) Deliver(frame []byte) {
	select {
	case <-c.done:
		return
	default:
	}
	select {
	case c.out <- frame:
	default:
		logger.Warn("binding closed a connection whose outbound queue overflowed", "client", c.kind)
		c.Close()
	}
}

func (c *Conn) writeLoop() {
	for {
		select {
		case frame := <-c.out:
			if err := c.Send(frame); err != nil {
				logger.Debug("binding dropped a connection after a failed write", "client", c.kind, "error", err)
				c.Close()
				return
			}
		case <-c.done:
			return
		}
	}
}

// Close closes the connection. Its read loop then ends and detaches it.
func (c *Conn) Close() {
	c.closeOnce.Do(func() {
		close(c.done)
		_ = c.ws.Close()
	})
}

// serve hands every frame to handler until the socket dies.
func (c *Conn) serve(handler Handler) {
	for {
		_, frame, err := c.ws.ReadMessage()
		if err != nil {
			return
		}
		handler.Handle(c, frame)
	}
}

// BindingConfig configures the listening end. AllowOrigin decides over requests
// that carry an Origin header, and without it only requests without one pass.
// Routes registers optional non-WebSocket HTTP handlers, e.g. static files,
// which serve under the same loopback listener as /ws.
type BindingConfig struct {
	Port        int
	Token       string
	Handshake   Handshake
	AllowOrigin func(origin string) bool
	Routes      map[string]http.Handler
}

// Binding hosts the localhost AG-UI WebSocket binding. It owns the listener,
// the Origin check, the token handshake and the connections, and hands every
// frame to its Handler.
type Binding struct {
	cfg      BindingConfig
	handler  Handler
	upgrader websocket.Upgrader

	server *http.Server
	addr   string
	mu     sync.Mutex
	conns  map[*Conn]struct{}
}

// NewBinding builds the binding over its handler.
func NewBinding(cfg BindingConfig, handler Handler) *Binding {
	return &Binding{
		cfg:     cfg,
		handler: handler,
		upgrader: websocket.Upgrader{CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			return origin == "" || (cfg.AllowOrigin != nil && cfg.AllowOrigin(origin))
		}},
		conns: make(map[*Conn]struct{}),
	}
}

// Start listens on 127.0.0.1:<port> and serves the /ws endpoint.
func (b *Binding) Start() error {
	if b.cfg.Token == "" {
		return errors.New("the binding token is empty")
	}

	addr := fmt.Sprintf("127.0.0.1:%d", b.cfg.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("binding failed to listen on %s: %w", addr, err)
	}

	b.addr = listener.Addr().String()
	mux := http.NewServeMux()
	for pattern, route := range b.cfg.Routes {
		if pattern != "" && route != nil {
			mux.Handle(pattern, route)
		}
	}
	mux.HandleFunc("/ws", b.handleWS)
	b.server = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := b.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Warn("binding server stopped", "error", err)
		}
	}()
	logger.Info("binding listening", "addr", b.addr)
	return nil
}

// Addr returns the actual listen address (useful with port 0 in tests).
func (b *Binding) Addr() string {
	return b.addr
}

func (b *Binding) handleWS(w http.ResponseWriter, r *http.Request) {
	ws, err := b.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	_ = ws.SetReadDeadline(time.Now().Add(handshakeTimeout))
	var helloRaw json.RawMessage
	if err := ws.ReadJSON(&helloRaw); err != nil {
		logger.Warn("binding rejected a connection with a bad or missing hello")
		_ = ws.Close()
		return
	}
	var hello helloFrame
	if err := json.Unmarshal(helloRaw, &hello); err != nil || hello.Type != b.cfg.Handshake.Hello ||
		subtle.ConstantTimeCompare([]byte(hello.Token), []byte(b.cfg.Token)) != 1 {
		logger.Warn("binding rejected a connection with a bad or missing hello")
		_ = ws.Close()
		return
	}
	_ = ws.SetReadDeadline(time.Time{})

	if len(hello.ProtocolVersion) == 0 {
		logger.Warn("binding accepted a hello without protocol_version", "client", hello.Client)
	}
	if err := ws.WriteJSON(helloAckFrame{Type: b.cfg.Handshake.Ack, ProtocolVersion: protocolVersion}); err != nil {
		_ = ws.Close()
		return
	}
	logger.Info("binding client connected", "client", hello.Client, "protocol_version", string(hello.ProtocolVersion))
	b.adopt(newConn(ws, hello.Client, declaredHelloAttrs(helloRaw)))
}

func (b *Binding) adopt(c *Conn) {
	b.mu.Lock()
	b.conns[c] = struct{}{}
	b.mu.Unlock()

	b.handler.Attach(c)
	go func() {
		defer b.drop(c)
		c.serve(b.handler)
	}()
	go c.pingLoop()
}

func (c *Conn) pingLoop() {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			if err := c.ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(handshakeTimeout)); err != nil {
				return
			}
		}
	}
}

func (b *Binding) drop(c *Conn) {
	b.mu.Lock()
	delete(b.conns, c)
	b.mu.Unlock()

	c.Close()
	logger.Info("binding client disconnected", "client", c.kind, "protocol_version", c.HelloAttr(protocolVersionField))
	b.handler.Detach(c)
}

// declaredHelloAttrs flattens the fields a hello declared that the binding
// itself never interprets, so handlers can answer what the client declared.
func declaredHelloAttrs(hello json.RawMessage) map[string]string {
	var declared map[string]json.RawMessage
	if json.Unmarshal(hello, &declared) != nil {
		return nil
	}
	attrs := make(map[string]string, len(declared))
	for key, value := range declared {
		if key == "type" || key == "token" {
			continue
		}
		var text string
		if json.Unmarshal(value, &text) != nil {
			text = string(value)
		}
		attrs[key] = text
	}
	return attrs
}

// Close shuts the server and every connection down. Each read loop then ends
// and detaches its connection.
func (b *Binding) Close() {
	if b.server != nil {
		_ = b.server.Close()
	}
	b.mu.Lock()
	conns := slices.Collect(maps.Keys(b.conns))
	b.mu.Unlock()
	for _, c := range conns {
		c.Close()
	}
}

// DialConfig configures the dialing end. Kind is the client kind the hello
// declares.
type DialConfig struct {
	Addr      string
	Token     string
	Kind      string
	Handshake Handshake
}

// Dial connects to a binding, performs the handshake and hands every frame the
// binding sends to handler until the connection dies.
func Dial(ctx context.Context, cfg DialConfig, handler Handler) (*Conn, error) {
	ws, _, err := (&websocket.Dialer{HandshakeTimeout: handshakeTimeout}).
		DialContext(ctx, fmt.Sprintf("ws://%s/ws", cfg.Addr), nil)
	if err != nil {
		return nil, fmt.Errorf("nothing accepted the connection on %s: %w", cfg.Addr, err)
	}
	if err := sayHello(ws, cfg); err != nil {
		_ = ws.Close()
		return nil, err
	}

	c := newConn(ws, cfg.Kind, nil)
	handler.Attach(c)
	go func() {
		c.serve(handler)
		c.Close()
		handler.Detach(c)
	}()
	return c, nil
}

func sayHello(ws *websocket.Conn, cfg DialConfig) error {
	version, _ := json.Marshal(protocolVersion)
	if err := ws.WriteJSON(helloFrame{Type: cfg.Handshake.Hello, Token: cfg.Token, Client: cfg.Kind, ProtocolVersion: version}); err != nil {
		return fmt.Errorf("the binding on %s did not take the hello: %w", cfg.Addr, err)
	}
	_ = ws.SetReadDeadline(time.Now().Add(handshakeTimeout))
	var ack helloAckFrame
	if err := ws.ReadJSON(&ack); err != nil || ack.Type != cfg.Handshake.Ack {
		return fmt.Errorf("the binding on %s did not ack the hello", cfg.Addr)
	}
	_ = ws.SetReadDeadline(time.Time{})
	return nil
}
