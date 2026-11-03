package infrastructure

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	aguimocks "github.com/inference-gateway/cli/tests/mocks/agui"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// fakePeerA2A is the a2a peer protocol in miniature: its stream announces the
// peer's attrs and streams every frame it is told to emit, and every
// message/send rides onto sent. It serves on loopback without TLS.
type fakePeerA2A struct {
	*httptest.Server

	attrs map[string]string
	sent  chan string
	emit  chan string
}

func newFakePeerA2A(t *testing.T, attrs map[string]string) *fakePeerA2A {
	t.Helper()
	fake := &fakePeerA2A{attrs: attrs, sent: make(chan string, 16), emit: make(chan string, 16)}
	fake.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := make([]byte, 0, 4096)
		buffer := make([]byte, 4096)
		for {
			n, err := r.Body.Read(buffer)
			raw = append(raw, buffer[:n]...)
			if err != nil {
				break
			}
		}
		id, method, frame, caller, ok := ParsePeerA2ARequest(raw)
		if !ok || (method != peerA2AMethodSend && method != peerA2AMethodStream) || caller != "a" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if method == peerA2AMethodSend {
			fake.sent <- string(frame)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(PeerA2AAck(id))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write(sseData(PeerA2AEvent(nil, fake.attrs)))
		if flusher != nil {
			flusher.Flush()
		}
		for frame := range fake.emit {
			_, _ = w.Write(sseData(PeerA2AEvent([]byte(frame), nil)))
			if flusher != nil {
				flusher.Flush()
			}
		}
		<-r.Context().Done()
	}))
	t.Cleanup(func() {
		close(fake.emit)
		fake.Close()
	})
	return fake
}

// sseData renders one event as a server-sent events data line.
func sseData(event []byte) []byte {
	return append(append([]byte("data: "), event...), '\n', '\n')
}

// endpointAt is the dial endpoint a test server's URL reaches, without TLS.
func endpointAt(serverURL string) agentdomain.Endpoint {
	parsed, err := url.Parse(serverURL)
	if err != nil {
		return agentdomain.Endpoint{}
	}
	return agentdomain.Endpoint{Addr: parsed.Host}
}

// awaitFrames collects the frames the handler saw, when it saw enough.
func awaitFrames(t *testing.T, handler *aguimocks.FakeHandler, want int) [][]byte {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if handler.HandleCallCount() >= want {
			frames := make([][]byte, handler.HandleCallCount())
			for i := range handler.HandleCallCount() {
				_, frame := handler.HandleArgsForCall(i)
				frames[i] = frame
			}
			return frames
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the handler never saw %d frames", want)
	return nil
}

func TestDialPeerA2AExchangesTheVocabulary(t *testing.T) {
	fake := newFakePeerA2A(t, map[string]string{AttrWorkspace: "b", AttrVersion: "0.1.0", AttrStorageID: "s-1"})
	handler := &aguimocks.FakeHandler{}

	conn, info, err := dialPeerA2A(t.Context(), endpointAt(fake.URL), handler, "a")
	if err != nil {
		t.Fatalf("dialPeerA2A() = %v", err)
	}
	if info.Workspace != "b" || info.Version != "0.1.0" || info.StorageID != "s-1" {
		t.Fatalf("PeerInfo = %+v", info)
	}
	if conn.Kind() != PeerClientKind {
		t.Fatalf("Kind() = %q", conn.Kind())
	}
	if conn.AckAttr(AttrWorkspace) != "b" || conn.AckAttr(AttrVersion) != "0.1.0" {
		t.Fatalf("AckAttr = %q / %q", conn.AckAttr(AttrWorkspace), conn.AckAttr(AttrVersion))
	}

	frame := []byte(`{"type":"teleport_chunk","teleport_id":"t1"}`)
	if err := conn.Send(frame); err != nil {
		t.Fatalf("Send() = %v", err)
	}
	select {
	case sent := <-fake.sent:
		if sent != string(frame) {
			t.Fatalf("the peer received %q", sent)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the peer never received the frame")
	}

	fake.emit <- `{"type":"teleport_imported","teleport_id":"t1","ok":true}`
	frames := awaitFrames(t, handler, 1)
	if string(frames[0]) != `{"type":"teleport_imported","teleport_id":"t1","ok":true}` {
		t.Fatalf("the handler saw %q", frames[0])
	}

	conn.Close()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && handler.DetachCallCount() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if handler.DetachCallCount() == 0 {
		t.Fatal("closing the exchange must detach the handler")
	}
	if err := conn.Send([]byte(`{}`)); err == nil {
		t.Fatal("Send on a closed exchange must fail")
	}
}

func TestDialPeerA2AFailsWhenNothingServesA2A(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(plain.Close)

	_, _, err := dialPeerA2A(t.Context(), endpointAt(plain.URL), &aguimocks.FakeHandler{}, "a")
	if err == nil || !strings.Contains(err.Error(), "does not serve the a2a protocol") {
		t.Fatalf("dialPeerA2A() = %v", err)
	}
}
