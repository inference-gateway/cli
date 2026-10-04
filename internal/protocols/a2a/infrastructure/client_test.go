package infrastructure

import (
	"net/http"
	"net/http/httptest"
	"testing"

	adk "github.com/inference-gateway/adk/types"
)

// TestNewClient_ActivatesUsageExtension: every request the client sends lists
// the usage extension, or remote agents leave the usage out of their tasks.
func TestNewClient_ActivatesUsageExtension(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("A2A-Extensions")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"1","result":{"id":"t1"}}`))
	}))
	defer srv.Close()

	if _, err := NewClient(srv.URL).GetTask(t.Context(), adk.GetTaskRequest{ID: "t1"}); err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got != adk.UsageExtensionURI {
		t.Fatalf("A2A-Extensions = %q, want %q", got, adk.UsageExtensionURI)
	}
}
