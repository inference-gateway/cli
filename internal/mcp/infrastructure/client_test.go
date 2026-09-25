package infrastructure

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// testRequest is what the stub decodes from every request.
type testRequest struct {
	Method string `json:"method"`
	Params struct {
		Meta      map[string]any  `json:"_meta"`
		Cursor    string          `json:"cursor"`
		Name      string          `json:"name"`
		Arguments *map[string]any `json:"arguments"`
	} `json:"params"`
}

// stub serves handle behind an httptest server and returns a client for it.
func stub(t *testing.T, handle func(w http.ResponseWriter, r *http.Request, req testRequest)) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req testRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decoding request: %v", err)
			return
		}
		handle(w, r, req)
	}))
	t.Cleanup(srv.Close)
	return NewClient(srv.URL)
}

func writeResult(w http.ResponseWriter, result any) {
	w.Header().Set("Content-Type", contentTypeJSON)
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": result})
}

func writeError(w http.ResponseWriter, status, code int, message string, data any) {
	w.Header().Set("Content-Type", contentTypeJSON)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "error": map[string]any{"code": code, "message": message, "data": data}})
}

// Every request is a stateless 2026-07-28 request: version, client info and
// capabilities in _meta, version and method mirrored into headers, a
// non-ASCII tool name base64-encoded in Mcp-Name, and no session.
func TestClient_RequestMetadata(t *testing.T) {
	const tool = "größe"
	client := stub(t, func(w http.ResponseWriter, r *http.Request, req testRequest) {
		if req.Method == "initialize" || r.Header.Get("Mcp-Session-Id") != "" {
			t.Errorf("%s: sent an initialize handshake or a session", req.Method)
		}
		if got := r.Header.Get(headerProtocolVersion); got != ProtocolVersion {
			t.Errorf("%s: %s = %q", req.Method, headerProtocolVersion, got)
		}
		if got := r.Header.Get(headerMethod); got != req.Method {
			t.Errorf("%s: %s = %q", req.Method, headerMethod, got)
		}
		for _, key := range []string{"protocolVersion", "clientInfo", "clientCapabilities"} {
			if _, ok := req.Params.Meta["io.modelcontextprotocol/"+key]; !ok {
				t.Errorf("%s: _meta lacks %s", req.Method, key)
			}
		}

		switch req.Method {
		case "server/discover":
			writeResult(w, map[string]any{"supportedVersions": []string{ProtocolVersion}})
		case "tools/list":
			writeResult(w, map[string]any{"tools": []any{}})
		case "tools/call":
			want := "=?base64?" + base64.StdEncoding.EncodeToString([]byte(tool)) + "?="
			if got := r.Header.Get(headerName); got != want {
				t.Errorf("%s = %q, want %q", headerName, got, want)
			}
			if req.Params.Name != tool || req.Params.Arguments == nil {
				t.Errorf("tools/call params: name %q, arguments %v", req.Params.Name, req.Params.Arguments)
			}
			writeResult(w, map[string]any{"content": []any{}})
		}
	})

	ctx := context.Background()
	if err := client.Discover(ctx); err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if _, err := client.ListTools(ctx); err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if _, err := client.CallTool(ctx, tool, nil); err != nil {
		t.Fatalf("CallTool: %v", err)
	}
}

func TestClient_ListTools_FollowsCursor(t *testing.T) {
	client := stub(t, func(w http.ResponseWriter, _ *http.Request, req testRequest) {
		if req.Params.Cursor == "" {
			writeResult(w, map[string]any{"tools": []any{map[string]any{"name": "a", "inputSchema": map[string]any{"type": "object"}}}, "nextCursor": "p2"})
			return
		}
		writeResult(w, map[string]any{"tools": []any{map[string]any{"name": "b", "description": "second"}}})
	})

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 2 || tools[0].Name != "a" || tools[0].InputSchema["type"] != "object" || tools[1].Description != "second" {
		t.Errorf("unexpected tools: %+v", tools)
	}
}

func TestClient_CallTool(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		wantContent string
		wantIsError bool
		wantErr     string
	}{
		{
			name:        "content blocks flatten to text",
			contentType: contentTypeJSON,
			body: `{"jsonrpc":"2.0","id":1,"result":{"content":[` +
				`{"type":"text","text":"hello"},{"type":"image","data":"AA==","mimeType":"image/png"},` +
				`{"type":"resource","resource":{"uri":"file:///a","text":"file body"}},` +
				`{"type":"resource_link","uri":"file:///b","name":"b"}]}}`,
			wantContent: "hello\n[Image content]\nfile body\nfile:///b",
		},
		{
			name:        "sse result after a notification",
			contentType: contentTypeSSE,
			body: "event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\",\"params\":{}}\n\n" +
				"event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"streamed\"}]}}\n\n",
			wantContent: "streamed",
		},
		{
			name:        "tool reported error",
			contentType: contentTypeJSON,
			body:        `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"no such file"}],"isError":true}}`,
			wantContent: "no such file",
			wantIsError: true,
		},
		{
			name:        "input required is refused",
			contentType: contentTypeJSON,
			body:        `{"jsonrpc":"2.0","id":1,"result":{"resultType":"input_required"}}`,
			wantErr:     "elicitation or sampling",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := stub(t, func(w http.ResponseWriter, _ *http.Request, _ testRequest) {
				w.Header().Set("Content-Type", tt.contentType)
				_, _ = fmt.Fprint(w, tt.body)
			})

			result, err := client.CallTool(context.Background(), "read", map[string]any{"path": "/a"})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("CallTool: %v", err)
			}
			if result.Content != tt.wantContent || result.IsError != tt.wantIsError {
				t.Errorf("got %+v, want content %q isError %v", result, tt.wantContent, tt.wantIsError)
			}
		})
	}
}

// A server on another revision is reported as unsupported, naming what it
// supports when it says; auth refusals and transport failures are not.
func TestClient_Discover_UnsupportedProtocol(t *testing.T) {
	tests := []struct {
		name            string
		handle          func(w http.ResponseWriter)
		wantUnsupported bool
		wantInMessage   string
	}{
		{
			name: "unsupported protocol version error",
			handle: func(w http.ResponseWriter) {
				writeError(w, http.StatusBadRequest, codeUnsupportedProtocolVersion, "unsupported protocol version",
					map[string]any{"requested": ProtocolVersion, "supported": []string{"2025-11-25"}})
			},
			wantUnsupported: true,
			wantInMessage:   "server supports 2025-11-25",
		},
		{
			name:            "legacy server without server/discover",
			handle:          func(w http.ResponseWriter) { writeError(w, http.StatusOK, -32601, "method not found", nil) },
			wantUnsupported: true,
		},
		{
			name: "legacy server rejecting a request without a session",
			handle: func(w http.ResponseWriter) {
				http.Error(w, "Bad Request: Unsupported protocol version (supported versions: 2025-11-25)", http.StatusBadRequest)
			},
			wantUnsupported: true,
			wantInMessage:   "supported versions: 2025-11-25",
		},
		{
			name: "discover without our version",
			handle: func(w http.ResponseWriter) {
				writeResult(w, map[string]any{"supportedVersions": []string{"2027-01-01"}})
			},
			wantUnsupported: true,
			wantInMessage:   "server supports 2027-01-01",
		},
		{
			name:   "auth refusal",
			handle: func(w http.ResponseWriter) { http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized) },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := stub(t, func(w http.ResponseWriter, _ *http.Request, _ testRequest) { tt.handle(w) })

			err := client.Discover(context.Background())
			if err == nil {
				t.Fatal("Discover succeeded, want an error")
			}
			if got := errors.Is(err, errUnsupportedProtocol); got != tt.wantUnsupported {
				t.Errorf("unsupported = %v, want %v (err: %v)", got, tt.wantUnsupported, err)
			}
			if !strings.Contains(err.Error(), tt.wantInMessage) {
				t.Errorf("err = %q, want it to contain %q", err, tt.wantInMessage)
			}
		})
	}

	t.Run("unreachable server", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		srv.Close()
		if err := NewClient(srv.URL).Discover(context.Background()); err == nil || errors.Is(err, errUnsupportedProtocol) {
			t.Errorf("err = %v, want a plain transport error", err)
		}
	})
}
