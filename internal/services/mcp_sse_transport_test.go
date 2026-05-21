package services

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	transport "github.com/metoro-io/mcp-golang/transport"
)

func newRPCRequest(id int64, method string) *transport.BaseJsonRpcMessage {
	return transport.NewBaseMessageRequest(&transport.BaseJSONRPCRequest{
		Id:      transport.RequestId(id),
		Jsonrpc: "2.0",
		Method:  method,
	})
}

func TestSSEHTTPClientTransport_CapturesAndReplaysSessionID(t *testing.T) {
	const issuedSessionID = "test-session-123"

	var (
		mu                sync.Mutex
		requestsSeen      int
		secondRequestHdrs http.Header
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestsSeen++
		current := requestsSeen
		if current == 2 {
			secondRequestHdrs = r.Header.Clone()
		}
		mu.Unlock()

		if current == 1 {
			w.Header().Set("Mcp-Session-Id", issuedSessionID)
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":{}}`)
			return
		}

		if r.Header.Get("Mcp-Session-Id") == "" {
			http.Error(w, `{"jsonrpc":"2.0","error":{"code":-32600,"message":"Missing session ID"}}`, http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":2,"result":{}}`)
	}))
	defer srv.Close()

	tr := NewSSEHTTPClientTransport(srv.URL)
	tr.SetMessageHandler(func(context.Context, *transport.BaseJsonRpcMessage) {})

	if err := tr.Send(context.Background(), newRPCRequest(1, "initialize")); err != nil {
		t.Fatalf("initialize send failed: %v", err)
	}

	if err := tr.Send(context.Background(), newRPCRequest(2, "ping")); err != nil {
		t.Fatalf("follow-up send failed (expected session ID to be replayed): %v", err)
	}

	mu.Lock()
	got := secondRequestHdrs.Get("Mcp-Session-Id")
	mu.Unlock()
	if got != issuedSessionID {
		t.Fatalf("second request Mcp-Session-Id = %q, want %q", got, issuedSessionID)
	}
}

func TestSSEHTTPClientTransport_NoSessionWhenServerOmitsIt(t *testing.T) {
	var (
		mu                 sync.Mutex
		secondRequestHdrs  http.Header
		secondRequestCount int
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		secondRequestCount++
		current := secondRequestCount
		if current == 2 {
			secondRequestHdrs = r.Header.Clone()
		}
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer srv.Close()

	tr := NewSSEHTTPClientTransport(srv.URL)
	tr.SetMessageHandler(func(context.Context, *transport.BaseJsonRpcMessage) {})

	if err := tr.Send(context.Background(), newRPCRequest(1, "initialize")); err != nil {
		t.Fatalf("first send failed: %v", err)
	}
	if err := tr.Send(context.Background(), newRPCRequest(2, "ping")); err != nil {
		t.Fatalf("second send failed: %v", err)
	}

	mu.Lock()
	got := secondRequestHdrs.Get("Mcp-Session-Id")
	mu.Unlock()
	if strings.TrimSpace(got) != "" {
		t.Fatalf("expected no Mcp-Session-Id header when server never issued one, got %q", got)
	}
}
