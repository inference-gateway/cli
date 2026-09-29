package agui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sync"
	"time"

	websocket "github.com/gorilla/websocket"

	config "github.com/inference-gateway/cli/config"
)

var errDaemonClientClosed = errors.New("the infer daemon client is shutting down")

// daemonBootWait bounds how long a Request waits for a daemon this process
// just started to bind the binding port.
const daemonBootWait = 15 * time.Second

// daemonHello is the first frame the client sends. A browser client only
// speaks browser frames, so it never claims the extension's connection slot.
type daemonHello struct {
	Type            string `json:"type"`
	Token           string `json:"token"`
	Client          string `json:"client"`
	ProtocolVersion int    `json:"protocol_version"`
}

// DaemonClient reaches the browser through the infer daemon's extension
// bridge. It dials ws://127.0.0.1:<port>/ws as a browser client - the daemon
// hosts the socket and owns the extension connection - writes each
// browser_command frame and resolves it with the browser_result carrying the
// same id. When nothing listens it starts the daemon, which is how `infer
// chat` and a standalone headless boot their daemon on the first Browser call.
type DaemonClient struct {
	port  int
	token string

	mu      sync.Mutex
	ws      *websocket.Conn
	pending map[string]chan json.RawMessage
	closed  bool
	spawned bool
}

// NewDaemonClient builds the client the container injects into the extension
// driver for infer chat and a standalone headless run, over the binding's
// port and token.
func NewDaemonClient(ext config.ExtensionConfig) *DaemonClient {
	return &DaemonClient{port: ext.Port, token: ext.Token, pending: make(map[string]chan json.RawMessage)}
}

// Request sends one browser_command frame and waits for the browser_result
// carrying id, the ExtensionRequest seam the extension driver calls. A
// connection whose socket died is redialed on the next call.
func (c *DaemonClient) Request(ctx context.Context, id string, frame json.RawMessage) (json.RawMessage, error) {
	c.mu.Lock()
	conn, closed := c.ws, c.closed
	c.mu.Unlock()
	if closed {
		return nil, errDaemonClientClosed
	}
	if conn == nil {
		var err error
		if conn, err = c.dial(ctx); err != nil {
			return nil, err
		}
	}

	result := make(chan json.RawMessage, 1)
	defer c.forget(id)
	if err := c.register(conn, id, result); err != nil {
		return nil, err
	}

	if err := conn.SetWriteDeadline(time.Now().Add(connWriteTimeout)); err != nil {
		c.drop(conn)
		return nil, fmt.Errorf("failed to send the command to the browser extension: %w", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, frame); err != nil {
		c.drop(conn)
		return nil, fmt.Errorf("failed to send the command to the browser extension: %w", err)
	}

	select {
	case raw, ok := <-result:
		if !ok {
			return nil, errBrowserExtensionDisconnected
		}
		return raw, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// register reserves id on the connection's reply queue. A nil result means the
// client was closed or the connection was already replaced by another
// Request's Drop, and the caller redials.
func (c *DaemonClient) register(conn *websocket.Conn, id string, result chan json.RawMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.ws != conn {
		return errDaemonClientClosed
	}
	c.pending[id] = result
	return nil
}

func (c *DaemonClient) forget(id string) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

// dial connects and authenticates, starting the daemon first when nothing
// listens on the binding port.
func (c *DaemonClient) dial(ctx context.Context) (*websocket.Conn, error) {
	if err := c.ensureDaemon(ctx); err != nil {
		return nil, err
	}
	conn, _, err := (&websocket.Dialer{HandshakeTimeout: 5 * time.Second}).
		DialContext(ctx, fmt.Sprintf("ws://127.0.0.1:%d/ws", c.port), nil)
	if err != nil {
		return nil, fmt.Errorf("no browser extension connected on port %d - the infer daemon did not accept the connection: %w", c.port, err)
	}
	if err := c.hello(conn); err != nil {
		_ = conn.Close()
		return nil, err
	}

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		_ = conn.Close()
		return nil, errDaemonClientClosed
	}
	c.ws = conn
	c.mu.Unlock()
	go c.read(conn)
	return conn, nil
}

// hello performs the browser_hello handshake and consumes the ack.
func (c *DaemonClient) hello(conn *websocket.Conn) error {
	if err := conn.WriteJSON(daemonHello{
		Type:            inboundBrowserHello,
		Token:           c.token,
		Client:          clientBrowser,
		ProtocolVersion: protocolVersion,
	}); err != nil {
		_ = conn.Close()
		return fmt.Errorf("the infer daemon did not take the browser hello: %w", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var ack extHelloAck
	if err := conn.ReadJSON(&ack); err != nil || ack.Type != outboundBrowserHelloAck {
		_ = conn.Close()
		return errors.New("the infer daemon did not ack the browser hello")
	}
	_ = conn.SetReadDeadline(time.Time{})
	return nil
}

// read resolves the pending Request whose id matches each browser_result. The
// loop dies with the connection, which drops everything still pending.
func (c *DaemonClient) read(conn *websocket.Conn) {
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			c.drop(conn)
			return
		}
		var msg struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		}
		if json.Unmarshal(raw, &msg) != nil || msg.Type != inboundBrowserResult {
			continue
		}
		c.mu.Lock()
		ch, ok := c.pending[msg.ID]
		delete(c.pending, msg.ID)
		if ok {
			ch <- raw
		}
		c.mu.Unlock()
	}
}

// drop forgets a connection whose socket died, so the next Request redials,
// and fails everything still waiting on it.
func (c *DaemonClient) drop(conn *websocket.Conn) {
	c.mu.Lock()
	if c.ws != conn {
		c.mu.Unlock()
		return
	}
	c.ws = nil
	failed := c.pending
	c.pending = make(map[string]chan json.RawMessage)
	c.mu.Unlock()

	for _, ch := range failed {
		close(ch)
	}
}

// Close fails every pending Request and closes the socket.
func (c *DaemonClient) Close() {
	c.mu.Lock()
	c.closed = true
	ws := c.ws
	c.ws = nil
	failed := c.pending
	c.pending = make(map[string]chan json.RawMessage)
	c.mu.Unlock()

	for _, ch := range failed {
		close(ch)
	}
	if ws != nil {
		_ = ws.Close()
	}
}

// ensureDaemon makes some daemon listen on the port. Nothing there starts
// one, whose own pid lock makes a redundant start exit right away, and the
// caller waits for the port once it may already be on its way.
func (c *DaemonClient) ensureDaemon(ctx context.Context) error {
	if daemonReachable(c.port) {
		return nil
	}
	if err := c.spawn(); err != nil {
		return fmt.Errorf("no browser extension connected on port %d - starting the infer daemon failed: %w", c.port, err)
	}
	deadline := time.Now().Add(daemonBootWait)
	for {
		if daemonReachable(c.port) {
			return nil
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("no browser extension connected on port %d - the infer daemon did not bind its binding port in time", c.port)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// spawn runs `infer daemon` detached once per client, so the daemon outlives
// this process. Ignoring a redundant start is safe: the daemon's own lock
// makes the second one exit immediately. A failed spawn may be retried, so a
// later Request tries again.
func (c *DaemonClient) spawn() error {
	c.mu.Lock()
	if c.spawned {
		c.mu.Unlock()
		return nil
	}
	c.spawned = true
	c.mu.Unlock()

	if err := startDaemonProcess(); err != nil {
		c.mu.Lock()
		c.spawned = false
		c.mu.Unlock()
		return err
	}
	return nil
}

// startDaemonProcess runs the infer binary as a detached daemon. A package
// var, so tests stub the boot instead of spawning the runner binary.
// ponytail: the daemon inherits this process's env, which carries the same
// browser_use config; route overrides as flags if a worker ever needs its own.
var startDaemonProcess = func() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "daemon")
	detachDaemon(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// daemonReachable reports whether something accepts TCP on the binding port.
func daemonReachable(port int) bool {
	conn, err := (&net.Dialer{Timeout: 500 * time.Millisecond}).
		Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
