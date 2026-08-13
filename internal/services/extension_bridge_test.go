package services

import (
	"context"
	"strings"
	"testing"
	"time"

	websocket "github.com/gorilla/websocket"

	config "github.com/inference-gateway/cli/config"
	macos "github.com/inference-gateway/cli/internal/display/macos"
	domain "github.com/inference-gateway/cli/internal/domain"
)

// recordingNotifier lives in mcp_manager_test.go; all returns a snapshot of
// its collected events.
func (n *recordingNotifier) all() []any {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]any(nil), n.events...)
}

func bridgeConfig() *config.BrowserUseConfig {
	cfg := config.DefaultBrowserUseConfig()
	cfg.Enabled = true
	cfg.Backend = config.BrowserBackendExtension
	cfg.Extension.Port = 0
	cfg.Extension.Token = "test-token"
	cfg.Browser.TimeoutSeconds = 2
	return cfg
}

func startBridge(t *testing.T, cfg *config.BrowserUseConfig, notifier domain.UINotifier, events domain.EventBridge) *ExtensionBridge {
	t.Helper()
	bridge := NewExtensionBridge(cfg, notifier, nil, events, "test-session")
	if err := bridge.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(bridge.Close)
	return bridge
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

// hello authenticates and consumes the ack and conversation snapshot frames.
func hello(t *testing.T, conn *websocket.Conn, token string) {
	t.Helper()
	if err := conn.WriteJSON(map[string]string{"type": "browser_hello", "token": token}); err != nil {
		t.Fatalf("hello: %v", err)
	}
	var ack map[string]any
	if err := conn.ReadJSON(&ack); err != nil {
		t.Fatalf("read ack: %v", err)
	}
	if ack["type"] != "browser_hello_ack" {
		t.Fatalf("expected browser_hello_ack, got %v", ack)
	}
}

func TestExtensionBridgeRejectsBadToken(t *testing.T) {
	bridge := startBridge(t, bridgeConfig(), nil, nil)
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
	bridge := startBridge(t, bridgeConfig(), nil, nil)

	_, err := bridge.Navigate(context.Background(), "https://example.com")
	if err == nil || !strings.Contains(err.Error(), "no browser extension connected") {
		t.Fatalf("expected no-extension error, got %v", err)
	}
}

func TestExtensionBridgeRefusesToStartWithoutToken(t *testing.T) {
	cfg := bridgeConfig()
	cfg.Extension.Token = ""
	bridge := NewExtensionBridge(cfg, nil, nil, nil, "s")
	if err := bridge.Start(); err == nil || !strings.Contains(err.Error(), "token is empty") {
		t.Fatalf("expected token error, got %v", err)
	}
	if _, err := bridge.Navigate(context.Background(), "https://example.com"); err == nil || !strings.Contains(err.Error(), "token is empty") {
		t.Fatalf("expected stored start error from verb, got %v", err)
	}
}

func TestExtensionBridgeNavigateRoundTrip(t *testing.T) {
	bridge := startBridge(t, bridgeConfig(), nil, nil)
	conn := dial(t, bridge)
	hello(t, conn, "test-token")

	// Fake extension: answer the first browser_command.
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

	// The connection is adopted asynchronously after the ack; retry briefly.
	var result domain.BrowserToolResult
	var err error
	deadline := time.Now().Add(2 * time.Second)
	for {
		result, err = bridge.Navigate(context.Background(), "https://example.com")
		if err == nil || !strings.Contains(err.Error(), "no browser extension connected") || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if result.URL != "https://example.com" || result.Title != "Example Domain" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestExtensionBridgeUserMessageReachesNotifier(t *testing.T) {
	notifier := &recordingNotifier{}
	bridge := startBridge(t, bridgeConfig(), notifier, nil)
	conn := dial(t, bridge)
	hello(t, conn, "test-token")

	if err := conn.WriteJSON(map[string]string{"type": "user_message", "content": "hello from the browser"}); err != nil {
		t.Fatalf("write: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, ev := range notifier.all() {
			if input, ok := ev.(domain.UserInputEvent); ok {
				if input.Content != "hello from the browser" {
					t.Fatalf("unexpected content: %q", input.Content)
				}
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("notifier never received the user message")
}

func TestExtensionBridgeMirrorsChatEvents(t *testing.T) {
	events := macos.NewEventBridge()
	bridge := startBridge(t, bridgeConfig(), nil, events)
	conn := dial(t, bridge)
	hello(t, conn, "test-token")

	// Give the chat pump a moment to subscribe, then publish a chunk.
	time.Sleep(50 * time.Millisecond)
	events.Publish(domain.ChatChunkEvent{Content: "streamed text"})

	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		var frame map[string]any
		if err := conn.ReadJSON(&frame); err != nil {
			t.Fatalf("read: %v", err)
		}
		if frame["type"] == "chat_event" {
			return
		}
	}
}

func TestExtensionBridgeReplacesConnection(t *testing.T) {
	bridge := startBridge(t, bridgeConfig(), nil, nil)

	first := dial(t, bridge)
	hello(t, first, "test-token")

	second := dial(t, bridge)
	hello(t, second, "test-token")

	// The first connection should be closed by the replacement.
	_ = first.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		if _, _, err := first.ReadMessage(); err != nil {
			break
		}
	}

	// Commands go to the second connection.
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

	var err error
	deadline := time.Now().Add(2 * time.Second)
	for {
		_, err = bridge.Read(context.Background(), "")
		if err == nil || !strings.Contains(err.Error(), "no browser extension connected") || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("Read after replacement: %v", err)
	}
}
