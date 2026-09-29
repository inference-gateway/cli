package infrastructure

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	config "github.com/inference-gateway/cli/config"
	browserdomain "github.com/inference-gateway/cli/internal/browser/domain"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
)

const navigateCommand = `{"type":"browser_command","id":"cmd-1","action":"navigate","url":"https://example.com","timeout_ms":2000}`

func relayConfig() *config.BrowserUseConfig {
	cfg := config.DefaultBrowserUseConfig()
	cfg.Enabled = true
	cfg.Backend = config.BrowserBackendExtension
	cfg.Extension.Port = 0
	cfg.Extension.Token = "test-token"
	cfg.Browser.TimeoutSeconds = 2
	return cfg
}

// relayHost hosts the relay behind a binding the way the daemon does: the
// relay claims the browser frames, and the rest is recorded.
type relayHost struct {
	relay *ExtensionRelay

	mu        sync.Mutex
	unclaimed []string
}

func (h *relayHost) Attach(conn *agui.Conn) { h.relay.Attach(conn) }

func (h *relayHost) Handle(conn *agui.Conn, frame []byte) {
	if h.relay.Handle(conn, frame) {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.unclaimed = append(h.unclaimed, string(frame))
}

func (h *relayHost) Detach(conn *agui.Conn) { h.relay.Detach(conn) }

func (h *relayHost) passedOn() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.unclaimed...)
}

func startRelay(t *testing.T) (*ExtensionRelay, *relayHost, *agui.Binding) {
	t.Helper()
	cfg := relayConfig()
	host := &relayHost{relay: NewExtensionRelay(cfg.Extension, nil)}
	binding := agui.NewBinding(agui.BindingConfig{
		Port:        cfg.Extension.Port,
		Token:       cfg.Extension.Token,
		Handshake:   BindingHandshake,
		AllowOrigin: AllowExtensionOrigin,
	}, host)
	if err := binding.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(binding.Close)
	return host.relay, host, binding
}

// peer is one client of the binding under test. It collects the frames the
// binding sends it.
type peer struct {
	conn   *agui.Conn
	frames chan map[string]any
	closed chan struct{}
}

func (p *peer) Attach(conn *agui.Conn) { p.conn = conn }

func (p *peer) Handle(_ *agui.Conn, frame []byte) {
	var decoded map[string]any
	if json.Unmarshal(frame, &decoded) == nil {
		p.frames <- decoded
	}
}

func (p *peer) Detach(*agui.Conn) { close(p.closed) }

func (p *peer) send(t *testing.T, frame any) {
	t.Helper()
	raw, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := p.conn.Send(raw); err != nil {
		t.Fatalf("send: %v", err)
	}
}

func (p *peer) next(t *testing.T) map[string]any {
	t.Helper()
	select {
	case frame := <-p.frames:
		return frame
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for a frame")
		return nil
	}
}

func (p *peer) quiet(t *testing.T, what string) {
	t.Helper()
	select {
	case frame := <-p.frames:
		t.Fatalf("%s: %v", what, frame)
	case <-time.After(300 * time.Millisecond):
	}
}

// answer plays the extension: it answers every browser_command with a result
// that echoes the command's url.
func (p *peer) answer() {
	for {
		select {
		case cmd := <-p.frames:
			if cmd["type"] != frameBrowserCommand {
				continue
			}
			raw, _ := json.Marshal(map[string]any{"type": frameBrowserResult, "id": cmd["id"], "url": cmd["url"], "title": "Example Domain"})
			_ = p.conn.Send(raw)
		case <-p.closed:
			return
		}
	}
}

func connect(t *testing.T, binding *agui.Binding, kind string) *peer {
	t.Helper()
	p := &peer{frames: make(chan map[string]any, 8), closed: make(chan struct{})}
	conn, err := agui.Dial(t.Context(), agui.DialConfig{Addr: binding.Addr(), Token: "test-token", Kind: kind, Handshake: BindingHandshake}, p)
	if err != nil {
		t.Fatalf("Dial as %q: %v", kind, err)
	}
	t.Cleanup(conn.Close)
	return p
}

// connectExtension connects an extension and waits until the relay has taken
// it over, the gap the ack read leaves before the binding attaches it.
func connectExtension(t *testing.T, relay *ExtensionRelay, binding *agui.Binding) *peer {
	t.Helper()
	relay.mu.Lock()
	previous := relay.ext
	relay.mu.Unlock()

	extension := connect(t, binding, "extension")
	eventually(t, "the extension connection", func() bool {
		relay.mu.Lock()
		defer relay.mu.Unlock()
		return relay.ext != nil && relay.ext != previous
	})
	return extension
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestExtensionRelayNavigateRoundTrip(t *testing.T) {
	relay, _, binding := startRelay(t)
	go connectExtension(t, relay, binding).answer()

	result, err := relay.relay(context.Background(), json.RawMessage(navigateCommand))
	if err != nil {
		t.Fatalf("relay: %v", err)
	}
	if !strings.Contains(string(result), `"url":"https://example.com"`) || !strings.Contains(string(result), `"title":"Example Domain"`) {
		t.Fatalf("unexpected result: %s", result)
	}
}

func TestExtensionRelayTakesAHelloWithoutAKindForTheExtension(t *testing.T) {
	relay, _, binding := startRelay(t)
	connect(t, binding, "")
	eventually(t, "the extension connection", func() bool {
		relay.mu.Lock()
		defer relay.mu.Unlock()
		return relay.ext != nil
	})
}

func TestExtensionRelayReplacesConnection(t *testing.T) {
	relay, _, binding := startRelay(t)
	first := connectExtension(t, relay, binding)
	second := connectExtension(t, relay, binding)

	select {
	case <-first.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("the replaced extension connection stayed open")
	}

	go second.answer()
	if _, err := relay.relay(context.Background(), json.RawMessage(navigateCommand)); err != nil {
		t.Fatalf("relay after replacement: %v", err)
	}
}

func TestExtensionRelayDisconnectFailsPendingRequest(t *testing.T) {
	relay, _, binding := startRelay(t)
	extension := connectExtension(t, relay, binding)
	go func() {
		<-extension.frames
		extension.conn.Close()
	}()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	_, err := relay.relay(ctx, json.RawMessage(`{"type":"browser_command","id":"cmd-1","action":"tabs"}`))
	if !errors.Is(err, errBrowserExtensionDisconnected) {
		t.Fatalf("expected disconnect error, got %v", err)
	}
}

func TestExtensionRelayFailsFastWithoutConnection(t *testing.T) {
	relay, _, _ := startRelay(t)

	_, err := relay.relay(context.Background(), json.RawMessage(`{"type":"browser_command","id":"cmd-1","action":"tabs"}`))
	if err == nil || !strings.Contains(err.Error(), "no browser extension connected") {
		t.Fatalf("expected no-extension error, got %v", err)
	}
}

func TestExtensionDriverOverRelayRoundTrip(t *testing.T) {
	relay, _, binding := startRelay(t)
	go connectExtension(t, relay, binding).answer()

	driver := NewExtensionDriver(relayConfig(), func(ctx context.Context, _ string, frame json.RawMessage) (json.RawMessage, error) {
		return relay.relay(ctx, frame)
	})
	var result browserdomain.BrowserToolResult
	result, err := driver.Navigate(context.Background(), "https://example.com")
	if err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if result.URL != "https://example.com" || result.Title != "Example Domain" {
		t.Fatalf("driver did not understand the relay frames: %+v", result)
	}
}

func TestExtensionRelayLeavesOtherFramesToTheHost(t *testing.T) {
	relay, host, binding := startRelay(t)
	extension := connectExtension(t, relay, binding)

	extension.send(t, map[string]string{"type": frameBrowserResult, "id": "orphan"})
	extension.send(t, map[string]string{"type": "list_skills"})

	eventually(t, "the list_skills frame", func() bool { return len(host.passedOn()) == 1 })
	if got := host.passedOn()[0]; !strings.Contains(got, "list_skills") {
		t.Fatalf("browser_result leaked to the host: %s", got)
	}
}

func TestExtensionRelayDesktopsDoNotReplaceTheExtension(t *testing.T) {
	relay, host, binding := startRelay(t)
	extension := connectExtension(t, relay, binding)

	for range 2 {
		connect(t, binding, "desktop").send(t, map[string]string{"type": "list_skills"})
	}
	eventually(t, "both desktops relaying", func() bool { return len(host.passedOn()) == 2 })

	go extension.answer()
	if _, err := relay.relay(context.Background(), json.RawMessage(navigateCommand)); err != nil {
		t.Fatalf("relay with desktops attached: %v", err)
	}
}

// TestExtensionRelayRoutesClientCommandsToTheExtension hands a browser command
// a browser client posts on the socket to the extension connection, and answers
// the posting client with the browser_result carrying the command's id. Nobody
// else receives it.
func TestExtensionRelayRoutesClientCommandsToTheExtension(t *testing.T) {
	relay, _, binding := startRelay(t)
	go connectExtension(t, relay, binding).answer()
	first := connect(t, binding, "browser")
	second := connect(t, binding, "browser")

	first.send(t, json.RawMessage(navigateCommand))
	result := first.next(t)
	if result["type"] != "browser_result" || result["id"] != "cmd-1" || result["url"] != "https://example.com" {
		t.Fatalf("first client got %v, want cmd-1's browser_result", result)
	}

	first.quiet(t, "the answered frame was replayed")
	second.quiet(t, "another client received cmd-1's result")
}

// TestExtensionRelaySerializesClientCommands holds the first command's answer
// back: the second client's command waits through the one-browser gate and
// reaches the extension only after the first resolved.
func TestExtensionRelaySerializesClientCommands(t *testing.T) {
	relay, _, binding := startRelay(t)
	extension := connectExtension(t, relay, binding)
	first := connect(t, binding, "browser")
	second := connect(t, binding, "browser")

	resolve := func(cmd map[string]any) {
		extension.send(t, map[string]any{"type": "browser_result", "id": cmd["id"], "url": "ok"})
	}

	first.send(t, map[string]any{"type": "browser_command", "id": "cmd-1", "action": "tabs"})
	opened := extension.next(t)
	if opened["id"] != "cmd-1" {
		t.Fatalf("the extension saw %v first", opened)
	}

	second.send(t, map[string]any{"type": "browser_command", "id": "cmd-2", "action": "tabs"})
	extension.quiet(t, "a command reached the extension while cmd-1 was still open")

	resolve(opened)
	if result := first.next(t); result["id"] != "cmd-1" {
		t.Fatalf("first client answered with %v", result)
	}

	queued := extension.next(t)
	if queued["id"] != "cmd-2" {
		t.Fatalf("next in line was %v, want cmd-2", queued)
	}
	resolve(queued)
	if result := second.next(t); result["id"] != "cmd-2" {
		t.Fatalf("second client answered with %v", result)
	}
}

// TestExtensionRelayAnswersNoExtensionToAClient answers a browser client whose
// command arrives while no extension is connected with a browser_result that
// carries the no-extension wording the Browser tools report.
func TestExtensionRelayAnswersNoExtensionToAClient(t *testing.T) {
	_, _, binding := startRelay(t)
	client := connect(t, binding, "browser")

	client.send(t, map[string]any{"type": "browser_command", "id": "cmd-1", "action": "tabs"})
	result := client.next(t)
	if result["type"] != "browser_result" || result["id"] != "cmd-1" {
		t.Fatalf("got %v, want a browser_result for cmd-1", result)
	}
	if message, _ := result["error"].(string); !strings.Contains(message, "no browser extension connected") {
		t.Fatalf("result error = %q, want the no-extension wording", message)
	}
}

func TestAllowExtensionOrigin(t *testing.T) {
	tests := []struct {
		origin string
		want   bool
	}{
		{"chrome-extension://abc", true},
		{"moz-extension://abc", true},
		{"safari-web-extension://abc", true},
		{"https://example.com", false},
	}
	for _, tt := range tests {
		t.Run(tt.origin, func(t *testing.T) {
			if got := AllowExtensionOrigin(tt.origin); got != tt.want {
				t.Fatalf("AllowExtensionOrigin(%q) = %v, want %v", tt.origin, got, tt.want)
			}
		})
	}
}
