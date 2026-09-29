package agui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	websocket "github.com/gorilla/websocket"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

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

// DaemonClient reaches the browser through the infer daemon, which hosts the
// binding and owns the extension connection. It dials the binding as a browser
// client and resolves each browser_command with the browser_result carrying
// the same id. When nothing listens it starts the daemon, so infer chat and a
// standalone headless boot one on their first Browser call.
type DaemonClient struct {
	port     int
	token    string
	notifier agentdomain.UINotifier

	sendMu  sync.Mutex
	mu      sync.Mutex
	ws      *websocket.Conn
	pending map[string]chan json.RawMessage
}

// NewDaemonClient builds the client the container injects into the extension
// driver for infer chat and a standalone headless run, over the binding's
// port and token. notifier drives the status bar's browser indicator.
func NewDaemonClient(ext config.ExtensionConfig, notifier agentdomain.UINotifier) *DaemonClient {
	return &DaemonClient{port: ext.Port, token: ext.Token, notifier: notifier, pending: make(map[string]chan json.RawMessage)}
}

// Request sends one browser_command frame and waits for the browser_result
// carrying id, the ExtensionRequest seam the extension driver calls. A
// connection whose socket died is redialed on the next call.
func (c *DaemonClient) Request(ctx context.Context, id string, frame json.RawMessage) (json.RawMessage, error) {
	result := make(chan json.RawMessage, 1)
	defer c.forget(id)
	if err := c.send(ctx, id, frame, result); err != nil {
		return nil, err
	}

	select {
	case raw, ok := <-result:
		if !ok {
			return nil, errBrowserExtensionDisconnected
		}
		c.notifyConnected(extensionAnswered(raw))
		return raw, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// send registers id and writes the frame, dialing first when there is no
// socket. sendMu keeps concurrent first calls on one socket and the writes to
// the single writer gorilla allows.
func (c *DaemonClient) send(ctx context.Context, id string, frame json.RawMessage, result chan json.RawMessage) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()

	c.mu.Lock()
	conn := c.ws
	c.mu.Unlock()
	if conn == nil {
		var err error
		if conn, err = c.dial(ctx); err != nil {
			c.notifyConnected(false)
			return err
		}
	}
	if !c.register(conn, id, result) {
		return errBrowserExtensionDisconnected
	}

	_ = conn.SetWriteDeadline(time.Now().Add(connWriteTimeout))
	if err := conn.WriteMessage(websocket.TextMessage, frame); err != nil {
		c.drop(conn)
		return fmt.Errorf("failed to send the command to the browser extension: %w", err)
	}
	return nil
}

// register reserves id on the connection's reply queue. It reports false when
// the socket died since the caller picked it up.
func (c *DaemonClient) register(conn *websocket.Conn, id string, result chan json.RawMessage) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ws != conn {
		return false
	}
	c.pending[id] = result
	return true
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
		return nil, err
	}

	c.mu.Lock()
	c.ws = conn
	c.mu.Unlock()
	go c.read(conn)
	return conn, nil
}

// hello performs the browser_hello handshake and consumes the ack. It closes
// the connection when the handshake fails.
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

// drop closes a connection whose socket died, so the next Request redials,
// and fails everything still waiting on it.
func (c *DaemonClient) drop(conn *websocket.Conn) {
	_ = conn.Close()
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

// notifyConnected moves the status bar's browser indicator.
// ponytail: it only moves when a Browser tool runs. Have the daemon push
// extension status frames to browser clients if it must be live.
func (c *DaemonClient) notifyConnected(connected bool) {
	c.notifier.Notify(agentdomain.BrowserExtensionStatusEvent{Connected: connected})
}

// extensionAnswered reports whether a browser_result came from the extension,
// rather than from the daemon finding none connected.
func extensionAnswered(raw json.RawMessage) bool {
	var result struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(raw, &result)
	return !strings.HasPrefix(result.Error, noExtensionConnected) && result.Error != errBrowserExtensionDisconnected.Error()
}

// ensureDaemon makes some daemon listen on the port. Nothing there starts
// one, whose own pid lock makes a redundant start exit right away, and the
// caller waits for the port once it may already be on its way.
func (c *DaemonClient) ensureDaemon(ctx context.Context) error {
	if daemonReachable(c.port) {
		return nil
	}
	if err := startDaemonProcess(); err != nil {
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

// startDaemonProcess runs the infer binary as a detached daemon that outlives
// this process. A package var, so tests stub the boot instead of spawning the
// runner binary.
// ponytail: the daemon inherits this process's env and working directory, so
// it loads the same config. Route overrides as flags if it ever needs its own.
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
	logger.Info("started the infer daemon for browser-use", "pid", cmd.Process.Pid)
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
