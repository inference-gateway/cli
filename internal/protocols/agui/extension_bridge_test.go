package agui

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sessionsmocks "github.com/inference-gateway/cli/tests/mocks/sessions"

	websocket "github.com/gorilla/websocket"

	config "github.com/inference-gateway/cli/config"
	browserdomain "github.com/inference-gateway/cli/internal/browser/domain"
	browserinfra "github.com/inference-gateway/cli/internal/browser/infrastructure"
	sessionsdomain "github.com/inference-gateway/cli/internal/sessions/domain"
)

var _ sessionsdomain.Client = (*extConn)(nil)

func bridgeConfig() *config.BrowserUseConfig {
	cfg := config.DefaultBrowserUseConfig()
	cfg.Enabled = true
	cfg.Backend = config.BrowserBackendExtension
	cfg.Extension.Port = 0
	cfg.Extension.Token = "test-token"
	cfg.Browser.TimeoutSeconds = 2
	return cfg
}

func testDeps(cfg *config.BrowserUseConfig) Deps {
	return Deps{Extension: cfg.Extension}
}

func startBridge(t *testing.T, deps Deps) *ExtensionBridge {
	t.Helper()
	bridge := NewExtensionBridge(deps)
	if err := bridge.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(bridge.Close)
	return bridge
}

func startRelay(t *testing.T) (*ExtensionBridge, *sessionsmocks.FakeThreads) {
	t.Helper()
	threads := &sessionsmocks.FakeThreads{}
	deps := testDeps(bridgeConfig())
	deps.Threads = threads
	return startBridge(t, deps), threads
}

func dial(t *testing.T, bridge *ExtensionBridge) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial("ws://"+bridge.Addr()+"/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// helloAs authenticates as the given client kind and returns the ack.
func helloAs(t *testing.T, conn *websocket.Conn, token, client string) map[string]any {
	t.Helper()
	if err := conn.WriteJSON(map[string]any{"type": "browser_hello", "token": token, "client": client, "protocol_version": 1}); err != nil {
		t.Fatalf("hello: %v", err)
	}
	var ack map[string]any
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := conn.ReadJSON(&ack); err != nil {
		t.Fatalf("read ack: %v", err)
	}
	_ = conn.SetReadDeadline(time.Time{})
	if ack["type"] != "browser_hello_ack" {
		t.Fatalf("expected browser_hello_ack, got %v", ack)
	}
	return ack
}

// extensionReq adapts the bridge's relay to the ExtensionRequest seam the
// driver calls, where the id travels separately from the frame.
func extensionReq(bridge *ExtensionBridge) browserinfra.ExtensionRequest {
	return func(ctx context.Context, _ string, frame json.RawMessage) (json.RawMessage, error) {
		return bridge.relay(ctx, frame)
	}
}

func hello(t *testing.T, conn *websocket.Conn, token string) {
	t.Helper()
	helloAs(t, conn, token, "")
}

// untilConnected retries try while the bridge has not adopted the dialed
// connection yet, since adopt runs after the client reads browser_hello_ack.
func untilConnected(t *testing.T, try func() error) error {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := try()
		if err == nil || !strings.Contains(err.Error(), "no browser extension connected") || time.Now().After(deadline) {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
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

// answerNavigate plays the extension: it answers the first browser_command.
func answerNavigate(conn *websocket.Conn) {
	for {
		var cmd map[string]any
		if err := conn.ReadJSON(&cmd); err != nil {
			return
		}
		if cmd["type"] == "browser_command" {
			_ = conn.WriteJSON(map[string]any{"type": "browser_result", "id": cmd["id"], "url": cmd["url"], "title": "Example Domain"})
			return
		}
	}
}

func TestExtensionBridgeAckCarriesProtocolVersion(t *testing.T) {
	bridge := startBridge(t, testDeps(bridgeConfig()))
	for _, client := range []string{"", "extension", "desktop", "browser"} {
		ack := helloAs(t, dial(t, bridge), "test-token", client)
		if ack["protocol_version"] != float64(protocolVersion) {
			t.Fatalf("client %q: ack protocol_version = %v, want %d", client, ack["protocol_version"], protocolVersion)
		}
	}
}

func TestExtensionBridgeAcceptsHelloWithoutProtocolVersion(t *testing.T) {
	bridge := startBridge(t, testDeps(bridgeConfig()))
	conn := dial(t, bridge)
	if err := conn.WriteJSON(map[string]string{"type": "browser_hello", "token": "test-token"}); err != nil {
		t.Fatalf("hello: %v", err)
	}
	var ack map[string]any
	if err := conn.ReadJSON(&ack); err != nil || ack["type"] != "browser_hello_ack" {
		t.Fatalf("expected an ack for a versionless hello, got %v (%v)", ack, err)
	}
}

func TestExtensionBridgeRelaysFramesToThreads(t *testing.T) {
	bridge, threads := startRelay(t)
	conn := dial(t, bridge)
	helloAs(t, conn, "test-token", "desktop")

	frame := `{"type":"user_message","content":"hi"}`
	if err := conn.WriteMessage(websocket.TextMessage, []byte(frame)); err != nil {
		t.Fatalf("write: %v", err)
	}
	eventually(t, "Threads.Handle", func() bool { return threads.HandleCallCount() == 1 })
	client, got := threads.HandleArgsForCall(0)
	if string(got) != frame {
		t.Fatalf("Handle frame = %s, want %s", got, frame)
	}

	event := `{"type":"RUN_STARTED","threadId":"c1","runId":"r1"}`
	client.Deliver([]byte(event))
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, delivered, err := conn.ReadMessage()
	if err != nil || string(delivered) != event {
		t.Fatalf("expected the bare delivered frame, got %s (%v)", delivered, err)
	}
}

func TestExtensionBridgeKeepsBrowserResultsFromThreads(t *testing.T) {
	bridge, threads := startRelay(t)
	conn := dial(t, bridge)
	hello(t, conn, "test-token")

	if err := conn.WriteJSON(map[string]string{"type": "browser_result", "id": "orphan"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := conn.WriteJSON(map[string]string{"type": "list_skills"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	eventually(t, "the list_skills frame", func() bool { return threads.HandleCallCount() == 1 })
	if _, got := threads.HandleArgsForCall(0); !strings.Contains(string(got), "list_skills") {
		t.Fatalf("browser_result leaked to Threads: %s", got)
	}
}

func TestExtensionBridgeDetachesClosedClient(t *testing.T) {
	bridge, threads := startRelay(t)
	conn := dial(t, bridge)
	helloAs(t, conn, "test-token", "desktop")
	if err := conn.WriteJSON(map[string]string{"type": "list_skills"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	eventually(t, "Threads.Handle", func() bool { return threads.HandleCallCount() == 1 })

	_ = conn.Close()
	eventually(t, "Threads.Detach", func() bool { return threads.DetachCallCount() == 1 })
	handled, _ := threads.HandleArgsForCall(0)
	if detached := threads.DetachArgsForCall(0); detached != handled {
		t.Fatal("Detach got a different client than Handle")
	}
}

// TestExtensionBridgeClosesAStalledClientOnOverflow delivers frames past the
// outbound queue of a client that stopped reading its socket: it is detached
// fast, instead of parking the relaying goroutine in a write deadline.
func TestExtensionBridgeClosesAStalledClientOnOverflow(t *testing.T) {
	bridge, threads := startRelay(t)
	conn := dial(t, bridge)
	helloAs(t, conn, "test-token", "desktop")

	if err := conn.WriteJSON(map[string]string{"type": "list_skills"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	eventually(t, "Threads.Handle", func() bool { return threads.HandleCallCount() == 1 })
	client, _ := threads.HandleArgsForCall(0)

	frame := []byte(`{"type":"TEXT_MESSAGE_CONTENT","delta":"` + strings.Repeat("x", 512*1024) + `"}`)
	for range 3*outboundQueue + 16 {
		client.Deliver(frame)
	}

	eventually(t, "the stalled client to be detached", func() bool { return threads.DetachCallCount() == 1 })
}

func TestExtensionBridgeDesktopsDoNotReplaceTheExtension(t *testing.T) {
	bridge, threads := startRelay(t)
	extension := dial(t, bridge)
	helloAs(t, extension, "test-token", "extension")

	for range 2 {
		desktop := dial(t, bridge)
		helloAs(t, desktop, "test-token", "desktop")
		if err := desktop.WriteJSON(map[string]string{"type": "list_skills"}); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	eventually(t, "both desktops relaying", func() bool { return threads.HandleCallCount() == 2 })
	first, _ := threads.HandleArgsForCall(0)
	second, _ := threads.HandleArgsForCall(1)
	if first == second {
		t.Fatal("two desktop connections share one client")
	}

	go answerNavigate(extension)
	err := untilConnected(t, func() error {
		_, err := bridge.relay(context.Background(), json.RawMessage(`{"type":"browser_command","id":"cmd-1","action":"navigate","url":"https://example.com","timeout_ms":2000}`))
		return err
	})
	if err != nil {
		t.Fatalf("Relay with desktops attached: %v", err)
	}
}

func TestExtensionBridgeWithoutThreadsIgnoresPanelFrames(t *testing.T) {
	bridge := startBridge(t, testDeps(bridgeConfig()))
	conn := dial(t, bridge)
	hello(t, conn, "test-token")
	if err := conn.WriteJSON(map[string]string{"type": "list_skills"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if _, frame, err := conn.ReadMessage(); err == nil {
		t.Fatalf("expected no reply without Threads, got %s", frame)
	}
}

func TestExtensionBridgeNavigateRoundTrip(t *testing.T) {
	bridge := startBridge(t, testDeps(bridgeConfig()))
	conn := dial(t, bridge)
	hello(t, conn, "test-token")

	go func() {
		for {
			var cmd map[string]any
			if err := conn.ReadJSON(&cmd); err != nil {
				return
			}
			if cmd["type"] == "browser_command" {
				_ = conn.WriteJSON(map[string]any{
					"type":  "browser_result",
					"id":    cmd["id"],
					"url":   cmd["url"],
					"title": "Example Domain",
				})
				return
			}
		}
	}()

	var result json.RawMessage
	err := untilConnected(t, func() (err error) {
		result, err = bridge.relay(context.Background(), json.RawMessage(`{"type":"browser_command","id":"cmd-1","action":"navigate","url":"https://example.com","timeout_ms":2000}`))
		return err
	})
	if err != nil {
		t.Fatalf("Relay: %v", err)
	}
	if !strings.Contains(string(result), `"url":"https://example.com"`) || !strings.Contains(string(result), `"title":"Example Domain"`) {
		t.Fatalf("unexpected result: %s", result)
	}
}

func TestExtensionBridgeReplacesConnection(t *testing.T) {
	bridge := startBridge(t, testDeps(bridgeConfig()))

	first := dial(t, bridge)
	hello(t, first, "test-token")

	second := dial(t, bridge)
	hello(t, second, "test-token")

	_ = first.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		if _, _, err := first.ReadMessage(); err != nil {
			break
		}
	}

	go func() {
		for {
			var cmd map[string]any
			if err := second.ReadJSON(&cmd); err != nil {
				return
			}
			if cmd["type"] == "browser_command" {
				_ = second.WriteJSON(map[string]any{"type": "browser_result", "id": cmd["id"], "url": cmd["url"]})
				return
			}
		}
	}()

	err := untilConnected(t, func() error {
		_, err := bridge.relay(context.Background(), json.RawMessage(`{"type":"browser_command","id":"cmd-1","action":"navigate","url":"https://example.com","timeout_ms":2000}`))
		return err
	})
	if err != nil {
		t.Fatalf("Relay after replacement: %v", err)
	}
}

func TestExtensionBridgeServesArtifacts(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cat.png"), []byte("PNGDATA"), 0o600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	deps := testDeps(bridgeConfig())
	deps.ArtifactsDir = dir
	bridge := NewExtensionBridge(deps)
	if err := bridge.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(bridge.Close)

	body, status := httpGet(t, "http://"+bridge.Addr()+"/artifacts/cat.png")
	if status != http.StatusOK || body != "PNGDATA" {
		t.Fatalf("serve artifact: status=%d body=%q", status, body)
	}

	_, status = httpGet(t, "http://"+bridge.Addr()+"/artifacts/../extension_bridge.go")
	if status == http.StatusOK {
		t.Fatalf("path traversal was not blocked (status %d)", status)
	}
}

func TestExtensionBridgeWithoutArtifactsDirHasNoRoute(t *testing.T) {
	bridge := startBridge(t, testDeps(bridgeConfig()))
	if _, status := httpGet(t, "http://"+bridge.Addr()+"/artifacts/cat.png"); status == http.StatusOK {
		t.Fatalf("expected no /artifacts/ route, got status %d", status)
	}
}

func httpGet(t *testing.T, url string) (string, int) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(body), resp.StatusCode
}

func TestExtensionBridgeDisconnectFailsPendingRequest(t *testing.T) {
	bridge := startBridge(t, testDeps(bridgeConfig()))
	conn := dial(t, bridge)
	hello(t, conn, "test-token")

	go func() {
		var cmd map[string]any
		if err := conn.ReadJSON(&cmd); err == nil {
			_ = conn.Close()
		}
	}()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	err := untilConnected(t, func() error {
		_, err := bridge.relay(ctx, json.RawMessage(`{"type":"browser_command","id":"cmd-1","action":"tabs"}`))
		return err
	})
	if !errors.Is(err, errBrowserExtensionDisconnected) {
		t.Fatalf("expected disconnect error, got %v", err)
	}
}

func TestExtensionDriverOverBridgeRoundTrip(t *testing.T) {
	cfg := bridgeConfig()
	bridge := startBridge(t, testDeps(cfg))
	conn := dial(t, bridge)
	hello(t, conn, "test-token")

	go func() {
		for {
			var cmd map[string]any
			if err := conn.ReadJSON(&cmd); err != nil {
				return
			}
			if cmd["type"] == "browser_command" && cmd["action"] == "navigate" {
				_ = conn.WriteJSON(map[string]any{
					"type":  "browser_result",
					"id":    cmd["id"],
					"url":   cmd["url"],
					"title": "Example Domain",
				})
				return
			}
		}
	}()

	driver := browserinfra.NewExtensionDriver(cfg, extensionReq(bridge))
	var result browserdomain.BrowserToolResult
	err := untilConnected(t, func() (err error) {
		result, err = driver.Navigate(context.Background(), "https://example.com")
		return err
	})
	if err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if result.URL != "https://example.com" || result.Title != "Example Domain" {
		t.Fatalf("driver did not understand the bridge frames: %+v", result)
	}
}

func TestExtensionBridgeRejectsBadToken(t *testing.T) {
	bridge := startBridge(t, testDeps(bridgeConfig()))
	conn := dial(t, bridge)

	if err := conn.WriteJSON(map[string]string{"type": "browser_hello", "token": "wrong"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var frame map[string]any
	if err := conn.ReadJSON(&frame); err == nil {
		t.Fatalf("expected connection close, got frame %v", frame)
	}
}

func TestExtensionBridgeFailsFastWithoutConnection(t *testing.T) {
	bridge := startBridge(t, testDeps(bridgeConfig()))

	_, err := bridge.relay(context.Background(), json.RawMessage(`{"type":"browser_command","id":"cmd-1","action":"tabs"}`))
	if err == nil || !strings.Contains(err.Error(), "no browser extension connected") {
		t.Fatalf("expected no-extension error, got %v", err)
	}
}

func TestExtensionBridgeRefusesToStartWithoutToken(t *testing.T) {
	cfg := bridgeConfig()
	cfg.Extension.Token = ""
	bridge := NewExtensionBridge(testDeps(cfg))
	if err := bridge.Start(); err == nil || !strings.Contains(err.Error(), "token is empty") {
		t.Fatalf("expected token error, got %v", err)
	}
	if _, err := bridge.relay(context.Background(), json.RawMessage(`{"type":"browser_command"}`)); err == nil || !strings.Contains(err.Error(), "token is empty") {
		t.Fatalf("expected stored start error from relay, got %v", err)
	}
}

// adoptedExtension waits until the bridge has taken the extension connection
// over, the gap the ack read leaves before adopt runs.
func adoptedExtension(t *testing.T, bridge *ExtensionBridge) {
	t.Helper()
	eventually(t, "the extension connection", func() bool {
		bridge.mu.Lock()
		defer bridge.mu.Unlock()
		return bridge.ext != nil
	})
}

// TestExtensionBridgeRoutesClientCommandsToTheExtension hands a browser
// command a browser client posts on the socket to the extension connection,
// and answers the posting client with the browser_result carrying the
// command's id. Nobody else receives it.
func TestExtensionBridgeRoutesClientCommandsToTheExtension(t *testing.T) {
	bridge := startBridge(t, testDeps(bridgeConfig()))
	extension := dial(t, bridge)
	helloAs(t, extension, "test-token", "extension")
	adoptedExtension(t, bridge)
	first := dial(t, bridge)
	helloAs(t, first, "test-token", "browser")
	second := dial(t, bridge)
	helloAs(t, second, "test-token", "browser")

	go answerNavigate(extension)

	if err := first.WriteJSON(map[string]any{"type": "browser_command", "id": "cmd-1", "action": "navigate", "url": "https://example.com", "timeout_ms": 2000}); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = first.SetReadDeadline(time.Now().Add(3 * time.Second))
	var result map[string]any
	if err := first.ReadJSON(&result); err != nil {
		t.Fatalf("read result: %v", err)
	}
	if result["type"] != "browser_result" || result["id"] != "cmd-1" || result["url"] != "https://example.com" {
		t.Fatalf("first client got %v, want cmd-1's browser_result", result)
	}

	_ = first.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if err := first.ReadJSON(&result); err == nil {
		t.Fatalf("the answered frame was replayed: %v", result)
	}
	_ = second.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if err := second.ReadJSON(&result); err == nil {
		t.Fatalf("another client received cmd-1's result: %v", result)
	}
}

// TestExtensionBridgeSerializesClientCommands holds the first command's answer
// back: the second client's command waits through the one-browser gate and
// reaches the extension only after the first resolved.
func TestExtensionBridgeSerializesClientCommands(t *testing.T) {
	bridge := startBridge(t, testDeps(bridgeConfig()))
	extension := dial(t, bridge)
	helloAs(t, extension, "test-token", "extension")
	adoptedExtension(t, bridge)
	first := dial(t, bridge)
	helloAs(t, first, "test-token", "browser")
	second := dial(t, bridge)
	helloAs(t, second, "test-token", "browser")

	saw := make(chan map[string]any, 8)
	proceed := make(chan struct{}, 4)
	go func() {
		for {
			var cmd map[string]any
			if err := extension.ReadJSON(&cmd); err != nil || cmd["type"] != "browser_command" {
				return
			}
			saw <- cmd
			<-proceed
			_ = extension.WriteJSON(map[string]any{"type": "browser_result", "id": cmd["id"], "url": "ok"})
		}
	}()

	if err := first.WriteJSON(map[string]any{"type": "browser_command", "id": "cmd-1", "action": "tabs"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	select {
	case cmd := <-saw:
		if cmd["id"] != "cmd-1" {
			t.Fatalf("the extension saw %v first", cmd)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the extension never saw cmd-1")
	}

	if err := second.WriteJSON(map[string]any{"type": "browser_command", "id": "cmd-2", "action": "tabs"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	select {
	case cmd := <-saw:
		t.Fatalf("%v reached the extension while cmd-1 was still open", cmd)
	case <-time.After(300 * time.Millisecond):
	}

	proceed <- struct{}{}
	_ = first.SetReadDeadline(time.Now().Add(3 * time.Second))
	var result map[string]any
	if err := first.ReadJSON(&result); err != nil || result["id"] != "cmd-1" {
		t.Fatalf("first client answered with %v (%v)", result, err)
	}

	select {
	case cmd := <-saw:
		if cmd["id"] != "cmd-2" {
			t.Fatalf("next in line was %v, want cmd-2", cmd)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cmd-2 never reached the extension after cmd-1 resolved")
	}
	proceed <- struct{}{}
	_ = second.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := second.ReadJSON(&result); err != nil || result["id"] != "cmd-2" {
		t.Fatalf("second client answered with %v (%v)", result, err)
	}
}

// TestExtensionBridgeAnswersNoExtensionToAClient answers a browser client whose
// command arrives while no extension is connected with a browser_result that
// carries the no-extension wording the Browser tools report.
func TestExtensionBridgeAnswersNoExtensionToAClient(t *testing.T) {
	bridge := startBridge(t, testDeps(bridgeConfig()))
	conn := dial(t, bridge)
	helloAs(t, conn, "test-token", "browser")

	if err := conn.WriteJSON(map[string]any{"type": "browser_command", "id": "cmd-1", "action": "tabs"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var result map[string]any
	if err := conn.ReadJSON(&result); err != nil {
		t.Fatalf("read result: %v", err)
	}
	if result["type"] != "browser_result" || result["id"] != "cmd-1" {
		t.Fatalf("got %v, want a browser_result for cmd-1", result)
	}
	if message, _ := result["error"].(string); !strings.Contains(message, "no browser extension connected") {
		t.Fatalf("result error = %q, want the no-extension wording", message)
	}
}
