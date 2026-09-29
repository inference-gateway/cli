package agui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	websocket "github.com/gorilla/websocket"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// startFakeBridge is a fake daemon binding: browser clients authenticate and
// every browser_command is answered with the browser_result carrying the same
// id, the command's id reported on commands and each accepted socket counted.
func startFakeBridge(t *testing.T) (*DaemonClient, chan string, *atomic.Int32) {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	commands := make(chan string, 8)
	var sockets atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		sockets.Add(1)
		defer func() { _ = conn.Close() }()
		defer context.AfterFunc(t.Context(), func() { _ = conn.Close() })()
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
	return NewDaemonClient(config.ExtensionConfig{Port: port, Token: "x"}, agentdomain.NoopUINotifier{}), commands, &sockets
}

func TestDaemonClientRoutesCommandsByIdOverTheSocket(t *testing.T) {
	client, commands, _ := startFakeBridge(t)
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

	port := freePort(t)
	client := NewDaemonClient(config.ExtensionConfig{Port: port}, agentdomain.NoopUINotifier{})
	_, err := client.Request(t.Context(), "cmd-1", json.RawMessage(`{"type":"browser_command","id":"cmd-1","action":"tabs"}`))
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("no browser extension connected on port %d", port)) {
		t.Fatalf("Request err = %v, want the no-extension wording naming the port", err)
	}
}

// TestDaemonClientSharesOneSocketAcrossConcurrentFirstCalls fires parallel
// Browser calls at a fresh client, as parallel tool calls do: all resolve over
// the one socket the first call dialed.
func TestDaemonClientSharesOneSocketAcrossConcurrentFirstCalls(t *testing.T) {
	client, _, sockets := startFakeBridge(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for i := range 4 {
		id := fmt.Sprintf("cmd-%d", i)
		wg.Go(func() {
			raw, err := client.Request(ctx, id, json.RawMessage(`{"type":"browser_command","id":"`+id+`","action":"tabs"}`))
			if err != nil || !strings.Contains(string(raw), id) {
				t.Errorf("Request(%s) = %s, %v, want its own browser_result", id, raw, err)
			}
		})
	}
	wg.Wait()
	if got := sockets.Load(); got != 1 {
		t.Fatalf("the client dialed %d sockets, want 1", got)
	}
}

func TestExtensionAnswered(t *testing.T) {
	tests := []struct {
		name   string
		result string
		want   bool
	}{
		{"result", `{"type":"browser_result","id":"a","title":"Example"}`, true},
		{"action failure", `{"type":"browser_result","id":"a","error":"element not found"}`, true},
		{"no extension", `{"type":"browser_result","id":"a","error":"no browser extension connected on port 52789 - install"}`, false},
		{"extension dropped", `{"type":"browser_result","id":"a","error":"the browser extension disconnected before it answered"}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extensionAnswered(json.RawMessage(tt.result)); got != tt.want {
				t.Fatalf("extensionAnswered(%s) = %v, want %v", tt.result, got, tt.want)
			}
		})
	}
}

// freePort returns a loopback port nothing listens on, so a daemon running on
// the machine cannot answer the test.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}
