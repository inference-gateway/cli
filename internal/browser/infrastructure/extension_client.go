package infrastructure

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
)

// EnsureHost makes something host the binding on port before the client dials,
// starting a host when none listens.
type EnsureHost func(ctx context.Context, port int) error

// ExtensionClient reaches the extension through the host of the binding. It
// dials the binding as a browser client and resolves each browser_command with
// the browser_result carrying the same id.
type ExtensionClient struct {
	port       int
	token      string
	notifier   agentdomain.UINotifier
	ensureHost EnsureHost

	sendMu  sync.Mutex
	mu      sync.Mutex
	conn    *agui.Conn
	pending map[string]chan json.RawMessage
}

// NewExtensionClient builds the client over the binding's port and token.
// notifier drives the status bar's browser indicator, and ensureHost runs
// before each dial so the first Browser call finds a host.
func NewExtensionClient(ext config.ExtensionConfig, notifier agentdomain.UINotifier, ensureHost EnsureHost) *ExtensionClient {
	return &ExtensionClient{
		port:       ext.Port,
		token:      ext.Token,
		notifier:   notifier,
		ensureHost: ensureHost,
		pending:    make(map[string]chan json.RawMessage),
	}
}

// Request sends one browser_command frame and waits for the browser_result
// carrying id, the ExtensionRequest seam the extension driver calls. A
// connection whose socket died is redialed on the next call.
func (c *ExtensionClient) Request(ctx context.Context, id string, frame json.RawMessage) (json.RawMessage, error) {
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
// connection. sendMu keeps concurrent first calls on one connection.
func (c *ExtensionClient) send(ctx context.Context, id string, frame json.RawMessage, result chan json.RawMessage) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()

	c.mu.Lock()
	conn := c.conn
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

	if err := conn.Send(frame); err != nil {
		c.Detach(conn)
		return fmt.Errorf("failed to send the command to the browser extension: %w", err)
	}
	return nil
}

// register reserves id on the connection's reply queue. It reports false when
// the connection died since the caller picked it up.
func (c *ExtensionClient) register(conn *agui.Conn, id string, result chan json.RawMessage) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != conn {
		return false
	}
	c.pending[id] = result
	return true
}

func (c *ExtensionClient) forget(id string) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

// dial connects as a browser client, making sure a host listens first.
func (c *ExtensionClient) dial(ctx context.Context) (*agui.Conn, error) {
	if err := c.ensureHost(ctx, c.port); err != nil {
		return nil, fmt.Errorf("%s on port %d - %w", noExtensionConnected, c.port, err)
	}
	conn, err := agui.Dial(ctx, agui.DialConfig{
		Addr:      fmt.Sprintf("127.0.0.1:%d", c.port),
		Token:     c.token,
		Kind:      clientBrowser,
		Handshake: BindingHandshake,
	}, c)
	if err != nil {
		return nil, fmt.Errorf("%s on port %d - %w", noExtensionConnected, c.port, err)
	}
	return conn, nil
}

// Attach keeps the connection the dial produced.
func (c *ExtensionClient) Attach(conn *agui.Conn) {
	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()
}

// Handle resolves the pending Request whose id matches a browser_result.
func (c *ExtensionClient) Handle(_ *agui.Conn, frame []byte) {
	var msg struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	if json.Unmarshal(frame, &msg) != nil || msg.Type != frameBrowserResult {
		return
	}
	c.mu.Lock()
	ch, ok := c.pending[msg.ID]
	delete(c.pending, msg.ID)
	if ok {
		ch <- frame
	}
	c.mu.Unlock()
}

// Detach forgets a connection that died, so the next Request redials, and
// fails everything still waiting on it.
func (c *ExtensionClient) Detach(conn *agui.Conn) {
	conn.Close()
	c.mu.Lock()
	if c.conn != conn {
		c.mu.Unlock()
		return
	}
	c.conn = nil
	failed := c.pending
	c.pending = make(map[string]chan json.RawMessage)
	c.mu.Unlock()

	for _, ch := range failed {
		close(ch)
	}
}

// notifyConnected moves the status bar's browser indicator.
// ponytail: it only moves when a Browser tool runs. The host pushes
// browser_extension_status frames to browser clients; taking them here is the
// upgrade path when the indicator must be live.
func (c *ExtensionClient) notifyConnected(connected bool) {
	c.notifier.Notify(agentdomain.BrowserExtensionStatusEvent{Connected: connected})
}

// extensionAnswered reports whether a browser_result came from the extension,
// rather than from the host finding none connected.
func extensionAnswered(raw json.RawMessage) bool {
	var result struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(raw, &result)
	return !strings.HasPrefix(result.Error, noExtensionConnected) && result.Error != errBrowserExtensionDisconnected.Error()
}
