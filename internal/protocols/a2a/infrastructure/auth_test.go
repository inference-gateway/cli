package infrastructure

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	client "github.com/inference-gateway/adk/client"
	adk "github.com/inference-gateway/adk/types"
)

const jsonRPCResult = `{"jsonrpc":"2.0","id":"1","result":{"id":"t1"}}`

// writeAgentsYAML makes body the agents.yaml of an otherwise empty project.
func writeAgentsYAML(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", dir)
	if err := os.MkdirAll(".infer", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(".infer/agents.yaml", []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// authRecorder is an agent that remembers the Authorization header of every request.
type authRecorder struct {
	mu   sync.Mutex
	seen []string
}

func (r *authRecorder) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	r.seen = append(r.seen, req.Header.Get("Authorization"))
	r.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if req.Method == http.MethodGet {
		_, _ = w.Write([]byte(`{"name":"research"}`))
		return
	}
	_, _ = w.Write([]byte(jsonRPCResult))
}

func (r *authRecorder) headers() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.seen...)
}

func TestNewClient_SendsBearerTokenOnEveryRequestType(t *testing.T) {
	requests := map[string]func(t *testing.T, c client.A2AClient) error{
		"agent card and liveness probe": func(t *testing.T, c client.A2AClient) error {
			_, err := c.GetAgentCard(t.Context())
			return err
		},
		"send message": func(t *testing.T, c client.A2AClient) error {
			_, err := c.SendTask(t.Context(), adk.SendMessageRequest{})
			return err
		},
		"get task": func(t *testing.T, c client.A2AClient) error {
			_, err := c.GetTask(t.Context(), adk.GetTaskRequest{ID: "t1"})
			return err
		},
		"cancel task": func(t *testing.T, c client.A2AClient) error {
			_, err := c.CancelTask(t.Context(), adk.CancelTaskRequest{ID: "t1"})
			return err
		},
	}
	for name, send := range requests {
		t.Run(name, func(t *testing.T) {
			agent := &authRecorder{}
			srv := httptest.NewServer(agent)
			defer srv.Close()
			writeAgentsYAML(t, fmt.Sprintf("agents:\n  - name: research\n    url: %s\n    auth:\n      token_env: RESEARCH_TOKEN\n", srv.URL))
			t.Setenv("RESEARCH_TOKEN", "s3cret")

			if err := send(t, NewClient(srv.URL)); err != nil {
				t.Fatalf("request failed: %v", err)
			}
			if got := agent.headers(); len(got) != 1 || got[0] != "Bearer s3cret" {
				t.Fatalf("Authorization headers = %q, want one Bearer s3cret", got)
			}
		})
	}
}

func TestNewClient_KeepsCredentialsOnTheAgentOrigin(t *testing.T) {
	other := &authRecorder{}
	otherSrv := httptest.NewServer(other)
	defer otherSrv.Close()
	redirecting := httptest.NewServer(http.RedirectHandler(otherSrv.URL+"/.well-known/agent-card.json", http.StatusFound))
	defer redirecting.Close()
	writeAgentsYAML(t, fmt.Sprintf("agents:\n  - name: research\n    url: %s\n    auth:\n      token_env: RESEARCH_TOKEN\n", redirecting.URL))
	t.Setenv("RESEARCH_TOKEN", "s3cret")

	if _, err := NewClient(redirecting.URL).GetAgentCard(t.Context()); err != nil {
		t.Fatalf("redirected card fetch: %v", err)
	}
	if _, err := NewClient(otherSrv.URL).GetAgentCard(t.Context()); err != nil {
		t.Fatalf("unconfigured card fetch: %v", err)
	}
	for _, header := range other.headers() {
		if header != "" {
			t.Fatalf("another origin received Authorization %q", header)
		}
	}
}

// TestNewClient_OIDCTokenIsFetchedOnceAndRefreshed: clients are built per
// request, yet they share one token until it is about to expire.
func TestNewClient_OIDCTokenIsFetchedOnceAndRefreshed(t *testing.T) {
	var tokenRequests atomic.Int32
	var issuer *httptest.Server
	issuer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/.well-known/openid-configuration" {
			_, _ = fmt.Fprintf(w, `{"token_endpoint":%q}`, issuer.URL+"/token")
			return
		}
		if err := r.ParseForm(); err != nil || r.Form.Get("audience") != "research-agent" {
			http.Error(w, "missing audience", http.StatusBadRequest)
			return
		}
		n := tokenRequests.Add(1)
		expiresIn := 3600
		if n == 1 {
			expiresIn = 5
		}
		_, _ = fmt.Fprintf(w, `{"access_token":"token-%d","token_type":"Bearer","expires_in":%d}`, n, expiresIn)
	}))
	defer issuer.Close()

	agent := &authRecorder{}
	srv := httptest.NewServer(agent)
	defer srv.Close()
	writeAgentsYAML(t, fmt.Sprintf(`agents:
  - name: research
    url: %s
    auth:
      oidc:
        issuer_url: %s
        client_id: infer
        client_secret_env: RESEARCH_CLIENT_SECRET
        audience: research-agent
`, srv.URL, issuer.URL))
	t.Setenv("RESEARCH_CLIENT_SECRET", "s3cret")

	for range 4 {
		if _, err := NewClient(srv.URL).GetTask(t.Context(), adk.GetTaskRequest{ID: "t1"}); err != nil {
			t.Fatalf("GetTask: %v", err)
		}
	}

	want := []string{"Bearer token-1", "Bearer token-2", "Bearer token-2", "Bearer token-2"}
	if got := agent.headers(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("Authorization headers = %q, want %q (first token expires at once, the second is reused)", got, want)
	}
}

func TestNewClient_UnsetTokenVariableIsAnAuthError(t *testing.T) {
	agent := &authRecorder{}
	srv := httptest.NewServer(agent)
	defer srv.Close()
	writeAgentsYAML(t, fmt.Sprintf("agents:\n  - name: research\n    url: %s\n    auth:\n      token_env: RESEARCH_TOKEN_UNSET\n", srv.URL))

	_, err := NewClient(srv.URL).GetAgentCard(t.Context())

	var authErr *AuthError
	if !errors.As(err, &authErr) || !strings.Contains(authErr.Reason, "RESEARCH_TOKEN_UNSET") {
		t.Fatalf("err = %v, want an AuthError naming RESEARCH_TOKEN_UNSET", err)
	}
	if len(agent.headers()) != 0 {
		t.Fatal("a request was sent without credentials")
	}
}

// TestAuthFailure_NamesTheAgentForRejectedRequests pins the ADK error text
// AuthFailure reads the status from.
func TestAuthFailure_NamesTheAgentForRejectedRequests(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		send         func(t *testing.T, c client.A2AClient) error
		wantRejected bool
	}{
		{"401 on the agent card", http.StatusUnauthorized, fetchCard, true},
		{"403 on the agent card", http.StatusForbidden, fetchCard, true},
		{"401 on a task request", http.StatusUnauthorized, fetchTask, true},
		{"403 on a task request", http.StatusForbidden, fetchTask, true},
		{"500 is not an auth failure", http.StatusInternalServerError, fetchTask, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, `{"error":"invalid token body-marker"}`, tt.status)
			}))
			defer srv.Close()
			writeAgentsYAML(t, fmt.Sprintf("agents:\n  - name: research\n    url: %s\n    auth:\n      token_env: RESEARCH_TOKEN\n", srv.URL))
			t.Setenv("RESEARCH_TOKEN", "s3cret")

			err := tt.send(t, NewClient(srv.URL))
			message, rejected := AuthFailure(srv.URL, err)

			if rejected != tt.wantRejected {
				t.Fatalf("AuthFailure(%v) rejected = %v, want %v", err, rejected, tt.wantRejected)
			}
			if !tt.wantRejected {
				return
			}
			for _, want := range []string{"Authentication failed", `"research"`, fmt.Sprint(tt.status)} {
				if !strings.Contains(message, want) {
					t.Errorf("message %q does not contain %q", message, want)
				}
			}
			for _, leaked := range []string{"s3cret", "body-marker"} {
				if strings.Contains(message, leaked) {
					t.Errorf("message %q leaks %q", message, leaked)
				}
			}
		})
	}
}

func fetchCard(t *testing.T, c client.A2AClient) error {
	_, err := c.GetAgentCard(t.Context())
	return err
}

func fetchTask(t *testing.T, c client.A2AClient) error {
	_, err := c.GetTask(t.Context(), adk.GetTaskRequest{ID: "t1"})
	return err
}
