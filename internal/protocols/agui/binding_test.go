package agui

import (
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	websocket "github.com/gorilla/websocket"
)

var testHandshake = Handshake{Hello: "hello", Ack: "hello_ack"}

type handled struct {
	conn  *Conn
	frame string
}

// recorder is the Handler the tests read back.
type recorder struct {
	mu       sync.Mutex
	attached []*Conn
	handled  []handled
	detached []*Conn
}

func (r *recorder) Attach(conn *Conn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.attached = append(r.attached, conn)
}

func (r *recorder) Handle(conn *Conn, frame []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handled = append(r.handled, handled{conn: conn, frame: string(frame)})
}

func (r *recorder) Detach(conn *Conn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.detached = append(r.detached, conn)
}

func (r *recorder) snapshot() (attached []*Conn, frames []handled, detached []*Conn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*Conn(nil), r.attached...), append([]handled(nil), r.handled...), append([]*Conn(nil), r.detached...)
}

func startBinding(t *testing.T, cfg BindingConfig) (*Binding, *recorder) {
	t.Helper()
	handler := &recorder{}
	binding := NewBinding(cfg, handler)
	if err := binding.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(binding.Close)
	return binding, handler
}

func testBinding(t *testing.T) (*Binding, *recorder) {
	t.Helper()
	return startBinding(t, BindingConfig{Token: "test-token", Handshake: testHandshake})
}

func dial(t *testing.T, binding *Binding) *websocket.Conn {
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
		if ack["protocol_version"] != float64(protocolVersion) {
			t.Fatalf("client %q: ack protocol_version = %v, want %d", client, ack["protocol_version"], protocolVersion)
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
	binding := NewBinding(BindingConfig{Handshake: testHandshake}, &recorder{})
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
			binding, _ := startBinding(t, BindingConfig{Token: "test-token", Handshake: testHandshake, AllowOrigin: tt.allow})
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

func TestBindingHandsFramesToTheHandler(t *testing.T) {
	binding, handler := testBinding(t)
	conn := dial(t, binding)
	helloAs(t, conn, "test-token", "panel")

	frame := `{"type":"user_message","content":"hi"}`
	if err := conn.WriteMessage(websocket.TextMessage, []byte(frame)); err != nil {
		t.Fatalf("write: %v", err)
	}
	eventually(t, "Handle", func() bool { _, frames, _ := handler.snapshot(); return len(frames) == 1 })
	attached, frames, _ := handler.snapshot()
	if frames[0].frame != frame {
		t.Fatalf("Handle frame = %s, want %s", frames[0].frame, frame)
	}
	if len(attached) != 1 || attached[0] != frames[0].conn || attached[0].Kind() != "panel" {
		t.Fatalf("Attach got %v, want the one panel connection Handle saw", attached)
	}

	event := `{"type":"RUN_STARTED","threadId":"c1","runId":"r1"}`
	frames[0].conn.Deliver([]byte(event))
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
	eventually(t, "Attach", func() bool { attached, _, _ := handler.snapshot(); return len(attached) == 1 })

	_ = conn.Close()
	eventually(t, "Detach", func() bool { _, _, detached := handler.snapshot(); return len(detached) == 1 })
	attached, _, detached := handler.snapshot()
	if detached[0] != attached[0] {
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
	eventually(t, "Attach", func() bool { attached, _, _ := handler.snapshot(); return len(attached) == 1 })
	attached, _, _ := handler.snapshot()

	frame := []byte(`{"type":"TEXT_MESSAGE_CONTENT","delta":"` + strings.Repeat("x", 512*1024) + `"}`)
	for range 3*outboundQueue + 16 {
		attached[0].Deliver(frame)
	}

	eventually(t, "the stalled client to be detached", func() bool { _, _, detached := handler.snapshot(); return len(detached) == 1 })
}

func TestDialSpeaksToABinding(t *testing.T) {
	binding, host := testBinding(t)
	client := &recorder{}
	conn, err := Dial(t.Context(), DialConfig{Addr: binding.Addr(), Token: "test-token", Kind: "viewer", Handshake: testHandshake}, client)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(conn.Close)

	if err := conn.Send([]byte(`{"type":"ping"}`)); err != nil {
		t.Fatalf("Send: %v", err)
	}
	eventually(t, "the host to handle the frame", func() bool { _, frames, _ := host.snapshot(); return len(frames) == 1 })
	_, frames, _ := host.snapshot()
	if frames[0].conn.Kind() != "viewer" || frames[0].frame != `{"type":"ping"}` {
		t.Fatalf("host handled %+v, want the viewer's ping", frames[0])
	}

	frames[0].conn.Deliver([]byte(`{"type":"pong"}`))
	eventually(t, "the client to handle the answer", func() bool { _, frames, _ := client.snapshot(); return len(frames) == 1 })

	binding.Close()
	eventually(t, "the client to detach", func() bool { _, _, detached := client.snapshot(); return len(detached) == 1 })
}

func TestDialReportsARejectedHello(t *testing.T) {
	binding, _ := testBinding(t)
	_, err := Dial(t.Context(), DialConfig{Addr: binding.Addr(), Token: "wrong", Handshake: testHandshake}, &recorder{})
	if err == nil || !strings.Contains(err.Error(), "did not ack the hello") {
		t.Fatalf("Dial err = %v, want the missing ack", err)
	}
}
