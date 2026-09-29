package infrastructure

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	aguimocks "github.com/inference-gateway/cli/tests/mocks/agui"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
)

// startHost hosts a binding that answers every browser_command with the
// browser_result carrying the same id and reports each command's id.
func startHost(t *testing.T) (client *ExtensionClient, host *aguimocks.FakeHandler, commands chan string) {
	t.Helper()
	commands = make(chan string, 8)
	host = &aguimocks.FakeHandler{}
	host.HandleCalls(func(conn *agui.Conn, frame []byte) {
		var cmd struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		}
		if json.Unmarshal(frame, &cmd) != nil || cmd.Type != frameBrowserCommand {
			return
		}
		commands <- cmd.ID
		raw, _ := json.Marshal(map[string]string{"type": frameBrowserResult, "id": cmd.ID, "title": "Example"})
		conn.Deliver(raw)
	})
	binding := agui.NewBinding(agui.BindingConfig{Token: "x", Handshake: BindingHandshake}, host)
	if err := binding.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(binding.Close)
	return NewExtensionClient(config.ExtensionConfig{Port: bindingPort(t, binding), Token: "x"}, agentdomain.NoopUINotifier{}, hostRunning), host, commands
}

func bindingPort(t *testing.T, binding *agui.Binding) int {
	t.Helper()
	_, port, err := net.SplitHostPort(binding.Addr())
	if err != nil {
		t.Fatalf("binding address: %v", err)
	}
	number, err := strconv.Atoi(port)
	if err != nil {
		t.Fatalf("binding port: %v", err)
	}
	return number
}

// hostRunning stands in for the EnsureHost hook when a host already listens.
func hostRunning(context.Context, int) error { return nil }

func TestExtensionClientRoutesCommandsByIdOverTheSocket(t *testing.T) {
	client, _, commands := startHost(t)
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

// TestExtensionClientReportsWhyNoHostStarted names the port and the boot
// failure in the error the Browser tools surface.
func TestExtensionClientReportsWhyNoHostStarted(t *testing.T) {
	noHost := func(context.Context, int) error {
		return errors.New("starting the infer daemon failed: permission denied")
	}
	client := NewExtensionClient(config.ExtensionConfig{Port: 52789}, agentdomain.NoopUINotifier{}, noHost)
	_, err := client.Request(t.Context(), "cmd-1", json.RawMessage(`{"type":"browser_command","id":"cmd-1","action":"tabs"}`))
	if err == nil || err.Error() != "no browser extension connected on port 52789 - starting the infer daemon failed: permission denied" {
		t.Fatalf("Request err = %v, want the no-extension wording naming the port and the cause", err)
	}
}

// TestExtensionClientSharesOneSocketAcrossConcurrentFirstCalls fires parallel
// Browser calls at a fresh client, as parallel tool calls do: all resolve over
// the one socket the first call dialed.
func TestExtensionClientSharesOneSocketAcrossConcurrentFirstCalls(t *testing.T) {
	client, host, _ := startHost(t)
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
	if got := host.AttachCallCount(); got != 1 {
		t.Fatalf("the client dialed %d sockets, want 1", got)
	}
}

// TestExtensionClientReachesTheExtensionThroughTheRelay runs both ends of the
// browser RPC over one binding: the client's command is driven through the
// extension connection the relay keeps.
func TestExtensionClientReachesTheExtensionThroughTheRelay(t *testing.T) {
	relay, _, binding := startRelay(t)
	go connectExtension(t, relay, binding).answer()

	ext := config.ExtensionConfig{Port: bindingPort(t, binding), Token: "test-token"}
	driver := NewExtensionDriver(relayConfig(), NewExtensionClient(ext, agentdomain.NoopUINotifier{}, hostRunning).Request)
	result, err := driver.Navigate(t.Context(), "https://example.com")
	if err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if result.URL != "https://example.com" || result.Title != "Example Domain" {
		t.Fatalf("Navigate = %+v, want the extension's answer", result)
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
