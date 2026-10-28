package agui

import (
	"bytes"
	"context"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"maps"
	"net"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"

	websocket "github.com/gorilla/websocket"

	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

// protocolVersion is the wire contract version the hello and its ack report.
// Version 2 replaces the CLI's CUSTOM events and app frames with the standard
// AG-UI ones: interrupts, run usage, the one state object, ACTIVITY_SNAPSHOT
// and RunAgentInput frames. The handshake is lenient: clients show an update
// state on a mismatch, and the binding never rejects a hello over its version.
const protocolVersion = 2

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

// loopbackHost is where the binding listens unless its config names a host.
const loopbackHost = "127.0.0.1"

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
	// ackAttrs are the other fields the ack declared to a dialing end, host
	// facts like its identity the binding itself never interprets.
	ackAttrs  map[string]string
	out       chan []byte
	mu        sync.Mutex
	done      chan struct{}
	closeOnce sync.Once
}

// newConn wraps one authenticated socket and starts its writer goroutine, so
// Deliver only queues frames and never waits on the socket.
func newConn(ws *websocket.Conn, kind string, helloAttrs, ackAttrs map[string]string) *Conn {
	c := &Conn{ws: ws, kind: kind, helloAttrs: helloAttrs, ackAttrs: ackAttrs, out: make(chan []byte, outboundQueue), done: make(chan struct{})}
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

// AckAttr is one other field the ack declared to this dialing end, empty when
// the ack omitted it or this is the listening end.
func (c *Conn) AckAttr(key string) string {
	return c.ackAttrs[key]
}

// PeerCertificates are the certificates the other end presented over TLS,
// nil on a plain connection.
func (c *Conn) PeerCertificates() []*x509.Certificate {
	tlsConn, ok := c.ws.NetConn().(*tls.Conn)
	if !ok {
		return nil
	}
	return tlsConn.ConnectionState().PeerCertificates
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

// BindingConfig configures the listening end. Host defaults to loopback. TLS
// serves the listener over TLS and, when it requires and verifies client
// certificates, stands in for the token. AckAttrs are the other fields the ack
// declares, host facts a dialing end reads back with AckAttr. AllowOrigin
// decides over requests that carry an Origin header, and without it only
// requests without one pass. Routes registers optional non-WebSocket HTTP
// handlers, e.g. static files, which serve under the same listener as /ws.
type BindingConfig struct {
	Host        string
	Port        int
	Token       string
	TLS         *tls.Config
	AckAttrs    map[string]any
	Handshake   Handshake
	AllowOrigin func(origin string) bool
	Routes      map[string]http.Handler
}

// requiresClientCert reports whether the TLS config authenticates every
// connection with a verified client certificate.
func (cfg BindingConfig) requiresClientCert() bool {
	return cfg.TLS != nil && cfg.TLS.ClientAuth == tls.RequireAndVerifyClientCert
}

// Binding hosts the AG-UI WebSocket binding. It owns the listener, the Origin
// check, the token handshake and the connections, and hands every frame to its
// Handler.
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

// Start listens on <host>:<port> and serves the /ws endpoint.
func (b *Binding) Start() error {
	if b.cfg.Token == "" && !b.cfg.requiresClientCert() {
		return errors.New("the binding needs a token or TLS client certificates")
	}

	host := b.cfg.Host
	if host == "" {
		host = loopbackHost
	}
	addr := net.JoinHostPort(host, strconv.Itoa(b.cfg.Port))
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("binding failed to listen on %s: %w", addr, err)
	}
	if b.cfg.TLS != nil {
		listener = tls.NewListener(listener, b.cfg.TLS)
	}

	b.addr = listener.Addr().String()
	mux := http.NewServeMux()
	for pattern, route := range b.cfg.Routes {
		if pattern != "" && route != nil {
			mux.Handle(pattern, route)
		}
	}
	mux.HandleFunc("/ws", b.handleWS)
	b.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ErrorLog:          log.New(quietProbes{next: log.Writer()}, log.Prefix(), log.Flags()),
	}
	go func() {
		if err := b.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Warn("binding server stopped", "error", err)
		}
	}()
	logger.Info("binding listening", "addr", b.addr)
	return nil
}

// quietProbes drops the handshake EOF a TCP readiness probe leaves on a TLS listener at
// every check, and passes every other server error through.
type quietProbes struct{ next io.Writer }

func (q quietProbes) Write(line []byte) (int, error) {
	if bytes.Contains(line, []byte("TLS handshake error")) && bytes.HasSuffix(bytes.TrimSpace(line), []byte(": EOF")) {
		return len(line), nil
	}
	return q.next.Write(line)
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
	var hello helloFrame
	if ws.ReadJSON(&helloRaw) != nil || json.Unmarshal(helloRaw, &hello) != nil || hello.Type != b.cfg.Handshake.Hello ||
		!b.authenticated(r, hello.Token) {
		logger.Warn("binding rejected a connection with a bad or missing hello")
		_ = ws.Close()
		return
	}
	_ = ws.SetReadDeadline(time.Time{})

	if len(hello.ProtocolVersion) == 0 {
		logger.Warn("binding accepted a hello without protocol_version", "client", hello.Client)
	}
	if err := ws.WriteJSON(b.ackFrame()); err != nil {
		_ = ws.Close()
		return
	}
	c := newConn(ws, hello.Client, declaredHelloAttrs(helloRaw), nil)
	logger.Info("binding client connected", "client", c.kind, "protocol_version", c.HelloAttr(protocolVersionField))
	b.adopt(c)
}

// authenticated accepts the configured token, or a client certificate the TLS
// listener already verified when the config requires one.
func (b *Binding) authenticated(r *http.Request, token string) bool {
	if b.cfg.Token != "" && subtle.ConstantTimeCompare([]byte(token), []byte(b.cfg.Token)) == 1 {
		return true
	}
	return b.cfg.requiresClientCert() && r.TLS != nil && len(r.TLS.PeerCertificates) > 0
}

// ackFrame is the hello's answer: the ack type, the protocol version and the
// host facts the config declares.
func (b *Binding) ackFrame() map[string]any {
	ack := make(map[string]any, len(b.cfg.AckAttrs)+2)
	for key, value := range b.cfg.AckAttrs {
		ack[key] = value
	}
	ack["type"] = b.cfg.Handshake.Ack
	ack[protocolVersionField] = protocolVersion
	return ack
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
// declares, and HelloAttrs the other fields it declares, e.g. a version the
// host reports on. TLS dials wss:// with that config.
type DialConfig struct {
	Addr       string
	Token      string
	Kind       string
	HelloAttrs map[string]string
	Handshake  Handshake
	TLS        *tls.Config
}

// Dial connects to a binding, performs the handshake and hands every frame the
// binding sends to handler until the connection dies.
func Dial(ctx context.Context, cfg DialConfig, handler Handler) (*Conn, error) {
	scheme := "ws"
	if cfg.TLS != nil {
		scheme = "wss"
	}
	ws, _, err := (&websocket.Dialer{HandshakeTimeout: handshakeTimeout, TLSClientConfig: cfg.TLS}).
		DialContext(ctx, fmt.Sprintf("%s://%s/ws", scheme, cfg.Addr), nil)
	if err != nil {
		return nil, fmt.Errorf("nothing accepted the connection on %s: %w", cfg.Addr, err)
	}
	ackAttrs, err := sayHello(ws, cfg)
	if err != nil {
		_ = ws.Close()
		return nil, err
	}

	c := newConn(ws, cfg.Kind, cfg.HelloAttrs, ackAttrs)
	handler.Attach(c)
	go func() {
		c.serve(handler)
		c.Close()
		handler.Detach(c)
	}()
	return c, nil
}

// sayHello sends the hello and returns the other fields the ack declared.
func sayHello(ws *websocket.Conn, cfg DialConfig) (map[string]string, error) {
	hello := make(map[string]any, len(cfg.HelloAttrs)+4)
	for key, value := range cfg.HelloAttrs {
		hello[key] = value
	}
	hello["type"] = cfg.Handshake.Hello
	hello["token"] = cfg.Token
	hello["client"] = cfg.Kind
	hello[protocolVersionField] = protocolVersion
	raw, _ := json.Marshal(hello)
	if err := ws.WriteMessage(websocket.TextMessage, raw); err != nil {
		return nil, fmt.Errorf("the binding on %s did not take the hello: %w", cfg.Addr, err)
	}
	_ = ws.SetReadDeadline(time.Now().Add(handshakeTimeout))
	var ackRaw json.RawMessage
	var ack struct {
		Type string `json:"type"`
	}
	if err := ws.ReadJSON(&ackRaw); err != nil || json.Unmarshal(ackRaw, &ack) != nil || ack.Type != cfg.Handshake.Ack {
		return nil, fmt.Errorf("the binding on %s did not ack the hello", cfg.Addr)
	}
	_ = ws.SetReadDeadline(time.Time{})
	return declaredHelloAttrs(ackRaw), nil
}
