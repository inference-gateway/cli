package agui_test

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	aguimocks "github.com/inference-gateway/cli/tests/mocks/agui"

	websocket "github.com/gorilla/websocket"

	agui "github.com/inference-gateway/cli/internal/protocols/agui"
)

var testHandshake = agui.Handshake{Hello: "hello", Ack: "hello_ack"}

func startBinding(t *testing.T, cfg agui.BindingConfig) (*agui.Binding, *aguimocks.FakeHandler) {
	t.Helper()
	handler := &aguimocks.FakeHandler{}
	binding := agui.NewBinding(cfg, handler)
	if err := binding.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(binding.Close)
	return binding, handler
}

func testBinding(t *testing.T) (*agui.Binding, *aguimocks.FakeHandler) {
	t.Helper()
	return startBinding(t, agui.BindingConfig{Token: "test-token", Handshake: testHandshake})
}

func dial(t *testing.T, binding *agui.Binding) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial("ws://"+binding.Addr()+"/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// helloAs authenticates as the given client kind and returns the ack.
func helloAs(t *testing.T, conn *websocket.Conn, token, client string) map[string]any {
	t.Helper()
	if err := conn.WriteJSON(map[string]any{"type": "hello", "token": token, "client": client, "protocol_version": 1}); err != nil {
		t.Fatalf("hello: %v", err)
	}
	var ack map[string]any
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := conn.ReadJSON(&ack); err != nil {
		t.Fatalf("read ack: %v", err)
	}
	_ = conn.SetReadDeadline(time.Time{})
	if ack["type"] != "hello_ack" {
		t.Fatalf("expected hello_ack, got %v", ack)
	}
	return ack
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

func TestBindingAckCarriesProtocolVersion(t *testing.T) {
	binding, _ := testBinding(t)
	for _, client := range []string{"", "panel", "viewer"} {
		ack := helloAs(t, dial(t, binding), "test-token", client)
		if ack["protocol_version"] != float64(agui.ProtocolVersion) {
			t.Fatalf("client %q: ack protocol_version = %v, want %d", client, ack["protocol_version"], agui.ProtocolVersion)
		}
	}
}

func TestBindingAcceptsHelloWithoutProtocolVersion(t *testing.T) {
	binding, _ := testBinding(t)
	conn := dial(t, binding)
	if err := conn.WriteJSON(map[string]string{"type": "hello", "token": "test-token"}); err != nil {
		t.Fatalf("hello: %v", err)
	}
	var ack map[string]any
	if err := conn.ReadJSON(&ack); err != nil || ack["type"] != "hello_ack" {
		t.Fatalf("expected an ack for a versionless hello, got %v (%v)", ack, err)
	}
}

func TestConnAnswersTheHelloAttrsWithoutTheToken(t *testing.T) {
	binding, handler := testBinding(t)
	helloAs(t, dial(t, binding), "test-token", "panel")
	eventually(t, "the connection to attach", func() bool { return handler.AttachCallCount() == 1 })

	conn := handler.AttachArgsForCall(0)
	if got := conn.HelloAttr("protocol_version"); got != "1" {
		t.Fatalf("protocol_version attr = %q, want 1", got)
	}
	if got := conn.HelloAttr("token"); got != "" {
		t.Fatalf("the token is readable as a hello attr: %q", got)
	}
}

func TestBindingRejectsBadHello(t *testing.T) {
	tests := []struct {
		name  string
		hello map[string]string
	}{
		{"wrong token", map[string]string{"type": "hello", "token": "wrong"}},
		{"wrong frame", map[string]string{"type": "greeting", "token": "test-token"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			binding, _ := testBinding(t)
			conn := dial(t, binding)
			if err := conn.WriteJSON(tt.hello); err != nil {
				t.Fatalf("write: %v", err)
			}
			_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			var frame map[string]any
			if err := conn.ReadJSON(&frame); err == nil {
				t.Fatalf("expected connection close, got frame %v", frame)
			}
		})
	}
}

func TestBindingRefusesToStartWithoutToken(t *testing.T) {
	binding := agui.NewBinding(agui.BindingConfig{Handshake: testHandshake}, &aguimocks.FakeHandler{})
	if err := binding.Start(); err == nil || !strings.Contains(err.Error(), "token is empty") {
		t.Fatalf("expected token error, got %v", err)
	}
}

func TestBindingChecksTheOrigin(t *testing.T) {
	tests := []struct {
		name    string
		allow   func(string) bool
		origin  string
		wantErr bool
	}{
		{"no origin passes without a policy", nil, "", false},
		{"an origin fails without a policy", nil, "https://example.com", true},
		{"an allowed origin passes", func(o string) bool { return o == "app://panel" }, "app://panel", false},
		{"another origin fails", func(o string) bool { return o == "app://panel" }, "https://example.com", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			binding, _ := startBinding(t, agui.BindingConfig{Token: "test-token", Handshake: testHandshake, AllowOrigin: tt.allow})
			header := http.Header{}
			if tt.origin != "" {
				header.Set("Origin", tt.origin)
			}
			conn, _, err := websocket.DefaultDialer.Dial("ws://"+binding.Addr()+"/ws", header)
			if conn != nil {
				_ = conn.Close()
			}
			if (err != nil) != tt.wantErr {
				t.Fatalf("dial err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBindingServesRegisteredRoutes(t *testing.T) {
	route := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("route-bytes"))
	})
	binding, _ := startBinding(t, agui.BindingConfig{
		Token:     "test-token",
		Handshake: testHandshake,
		Routes:    map[string]http.Handler{"/artifacts/": route},
	})

	resp, err := http.Get("http://" + binding.Addr() + "/artifacts/proj/artifact.png")
	if err != nil {
		t.Fatalf("get the route: %v", err)
	}
	body, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		t.Fatalf("read the route body: %v", readErr)
	}
	if resp.StatusCode != http.StatusOK || string(body) != "route-bytes" {
		t.Fatalf("route status = %d body = %q, want 200 with route-bytes", resp.StatusCode, body)
	}

	missing, err := http.Get("http://" + binding.Addr() + "/somewhere-else")
	if err != nil {
		t.Fatalf("get an unregistered path: %v", err)
	}
	_ = missing.Body.Close()
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("unregistered path status = %d, want 404", missing.StatusCode)
	}
}

func TestBindingHandsFramesToTheHandler(t *testing.T) {
	binding, handler := testBinding(t)
	conn := dial(t, binding)
	helloAs(t, conn, "test-token", "panel")

	frame := `{"type":"user_message","content":"hi"}`
	if err := conn.WriteMessage(websocket.TextMessage, []byte(frame)); err != nil {
		t.Fatalf("write: %v", err)
	}
	eventually(t, "Handle", func() bool { return handler.HandleCallCount() == 1 })
	client, got := handler.HandleArgsForCall(0)
	if string(got) != frame {
		t.Fatalf("Handle frame = %s, want %s", got, frame)
	}
	if handler.AttachCallCount() != 1 || handler.AttachArgsForCall(0) != client || client.Kind() != "panel" {
		t.Fatal("Attach did not get the one panel connection Handle saw")
	}

	event := `{"type":"RUN_STARTED","threadId":"c1","runId":"r1"}`
	client.Deliver([]byte(event))
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, delivered, err := conn.ReadMessage()
	if err != nil || string(delivered) != event {
		t.Fatalf("expected the bare delivered frame, got %s (%v)", delivered, err)
	}
}

func TestBindingDetachesClosedClient(t *testing.T) {
	binding, handler := testBinding(t)
	conn := dial(t, binding)
	helloAs(t, conn, "test-token", "panel")
	eventually(t, "Attach", func() bool { return handler.AttachCallCount() == 1 })

	_ = conn.Close()
	eventually(t, "Detach", func() bool { return handler.DetachCallCount() == 1 })
	if handler.DetachArgsForCall(0) != handler.AttachArgsForCall(0) {
		t.Fatal("Detach got a different connection than Attach")
	}
}

// TestBindingClosesAStalledClientOnOverflow delivers frames past the outbound
// queue of a client that stopped reading its socket: it is detached fast,
// instead of parking the delivering goroutine in a write deadline.
func TestBindingClosesAStalledClientOnOverflow(t *testing.T) {
	binding, handler := testBinding(t)
	conn := dial(t, binding)
	helloAs(t, conn, "test-token", "panel")
	eventually(t, "Attach", func() bool { return handler.AttachCallCount() == 1 })
	client := handler.AttachArgsForCall(0)

	frame := []byte(`{"type":"TEXT_MESSAGE_CONTENT","delta":"` + strings.Repeat("x", 512*1024) + `"}`)
	for range 3*agui.OutboundQueue + 16 {
		client.Deliver(frame)
	}

	eventually(t, "the stalled client to be detached", func() bool { return handler.DetachCallCount() == 1 })
}

func TestDialSpeaksToABinding(t *testing.T) {
	binding, host := testBinding(t)
	client := &aguimocks.FakeHandler{}
	conn, err := agui.Dial(t.Context(), agui.DialConfig{Addr: binding.Addr(), Token: "test-token", Kind: "viewer", Handshake: testHandshake}, client)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(conn.Close)

	if err := conn.Send([]byte(`{"type":"ping"}`)); err != nil {
		t.Fatalf("Send: %v", err)
	}
	eventually(t, "the host to handle the frame", func() bool { return host.HandleCallCount() == 1 })
	viewer, frame := host.HandleArgsForCall(0)
	if viewer.Kind() != "viewer" || string(frame) != `{"type":"ping"}` {
		t.Fatalf("host handled %s from %q, want the viewer's ping", frame, viewer.Kind())
	}

	viewer.Deliver([]byte(`{"type":"pong"}`))
	eventually(t, "the client to handle the answer", func() bool { return client.HandleCallCount() == 1 })

	binding.Close()
	eventually(t, "the client to detach", func() bool { return client.DetachCallCount() == 1 })
}

func TestDialKeepsTheReservedHelloFieldsAheadOfHelloAttrs(t *testing.T) {
	binding, host := testBinding(t)
	attrs := map[string]string{"client": "impostor", "token": "wrong", "extension_version": "1.9.2"}
	conn, err := agui.Dial(t.Context(), agui.DialConfig{Addr: binding.Addr(), Token: "test-token", Kind: "viewer", HelloAttrs: attrs, Handshake: testHandshake}, &aguimocks.FakeHandler{})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(conn.Close)

	eventually(t, "the host to attach the viewer", func() bool { return host.AttachCallCount() == 1 })
	viewer := host.AttachArgsForCall(0)
	if viewer.Kind() != "viewer" || viewer.HelloAttr("extension_version") != "1.9.2" {
		t.Fatalf("host attached kind %q with extension_version %q, want the viewer declaring 1.9.2", viewer.Kind(), viewer.HelloAttr("extension_version"))
	}
}

func TestDialReportsARejectedHello(t *testing.T) {
	binding, _ := testBinding(t)
	_, err := agui.Dial(t.Context(), agui.DialConfig{Addr: binding.Addr(), Token: "wrong", Handshake: testHandshake}, &aguimocks.FakeHandler{})
	if err == nil || !strings.Contains(err.Error(), "did not ack the hello") {
		t.Fatalf("Dial err = %v, want the missing ack", err)
	}
}
