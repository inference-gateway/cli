package web_test

import (
	"net/http/httptest"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	websocket "github.com/gorilla/websocket"

	web "github.com/inference-gateway/cli/internal/presentation/web"
)

func dial(t *testing.T, server *httptest.Server, token string) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(strings.Replace(server.URL, "http", "ws", 1)+web.Path+"ws", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := conn.WriteJSON(map[string]any{"type": "init", "token": token, "cols": 80, "rows": 24}); err != nil {
		t.Fatalf("init: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	return conn
}

func TestTerminalRunsCommandForTokenHolders(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no PTY on windows")
	}
	server := httptest.NewServer(&web.Terminal{
		Token:   "secret",
		Command: func(string) *exec.Cmd { return exec.Command("sh", "-c", "echo hello; cat") },
	})
	defer server.Close()

	t.Run("token", func(t *testing.T) {
		conn := dial(t, server, "secret")
		var init struct {
			Type      string `json:"type"`
			SessionID string `json:"session_id"`
		}
		if err := conn.ReadJSON(&init); err != nil || init.Type != "init_response" || init.SessionID == "" {
			t.Fatalf("want an init_response with the tab's session id, got %+v err %v", init, err)
		}
		kind, data, err := conn.ReadMessage()
		if err != nil || kind != websocket.BinaryMessage || !strings.Contains(string(data), "hello") {
			t.Fatalf("want binary hello, got kind %d %q err %v", kind, data, err)
		}
	})

	t.Run("wrong token", func(t *testing.T) {
		conn := dial(t, server, "nope")
		if _, _, err := conn.ReadMessage(); err == nil {
			t.Fatal("want the socket closed")
		}
	})

	t.Run("page", func(t *testing.T) {
		resp, err := server.Client().Get(server.URL + web.Path)
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("page: %v %v", resp, err)
		}
		_ = resp.Body.Close()
	})
}
