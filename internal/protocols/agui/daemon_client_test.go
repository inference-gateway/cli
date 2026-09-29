package agui

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	websocket "github.com/gorilla/websocket"

	config "github.com/inference-gateway/cli/config"
)

// startFakeBridge is a fake daemon binding: browser clients authenticate and
// every browser_command is answered with the browser_result carrying the same
// id, the command's id reported on commands.
func startFakeBridge(t *testing.T) (*DaemonClient, chan string) {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	commands := make(chan string, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		var hello daemonHello
		if err := conn.ReadJSON(&hello); err != nil || hello.Type != inboundBrowserHello {
			return
		}
		if err := conn.WriteJSON(extHelloAck{Type: outboundBrowserHelloAck, ProtocolVersion: protocolVersion}); err != nil {
			return
		}
		for {
			var cmd struct {
				Type string `json:"type"`
				ID   string `json:"id"`
			}
			if err := conn.ReadJSON(&cmd); err != nil {
				return
			}
			if cmd.Type != inboundBrowserCommand {
				continue
			}
			commands <- cmd.ID
			if err := conn.WriteJSON(map[string]string{
				"type":  inboundBrowserResult,
				"id":    cmd.ID,
				"title": "Example",
			}); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	client := NewDaemonClient(config.ExtensionConfig{Port: port, Token: "x"})
	t.Cleanup(client.Close)
	return client, commands
}

func TestDaemonClientRoutesCommandsByIdOverTheSocket(t *testing.T) {
	client, commands := startFakeBridge(t)
	for _, id := range []string{"cmd-1", "cmd-2"} {
		raw, err := client.Request(t.Context(), id, json.RawMessage(`{"type":"browser_command","id":"`+id+`","action":"tabs"}`))
		if err != nil {
			t.Fatalf("Request(%s): %v", id, err)
		}
		if !strings.Contains(string(raw), id) {
			t.Fatalf("Request(%s) = %s, want its own browser_result", id, raw)
		}
	}
	if got := <-commands; got != "cmd-1" {
		t.Fatalf("first command relayed was %q, want cmd-1", got)
	}
	if got := <-commands; got != "cmd-2" {
		t.Fatalf("second command relayed was %q, want cmd-2", got)
	}
}

// TestDaemonClientReportsWhyNoDaemonStarted names the port and the boot failure
// in the error the Browser tools surface.
func TestDaemonClientReportsWhyNoDaemonStarted(t *testing.T) {
	previous := startDaemonProcess
	startDaemonProcess = func() error { return errors.New("permission denied") }
	t.Cleanup(func() { startDaemonProcess = previous })

	client := NewDaemonClient(config.ExtensionConfig{Port: 52789})
	_, err := client.Request(t.Context(), "cmd-1", json.RawMessage(`{"type":"browser_command","id":"cmd-1","action":"tabs"}`))
	if err == nil || !strings.Contains(err.Error(), "no browser extension connected on port 52789") {
		t.Fatalf("Request err = %v, want the no-extension wording naming the port", err)
	}
}
