package infrastructure

import (
	"cmp"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
	card string
}

func (r *authRecorder) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	r.seen = append(r.seen, req.Header.Get("Authorization"))
	r.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if req.Method == http.MethodGet {
		_, _ = w.Write([]byte(cmp.Or(r.card, `{"name":"research"}`)))
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

// fakeIssuer serves an OIDC discovery document and a token endpoint that
// insists on wantScope. Its first token expires at once, later ones are long lived.
func fakeIssuer(t *testing.T, wantScope string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var tokenRequests atomic.Int32
	var issuer *httptest.Server
	issuer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/.well-known/openid-configuration" {
			_, _ = fmt.Fprintf(w, `{"token_endpoint":%q}`, issuer.URL+"/token")
			return
		}
		if err := r.ParseForm(); err != nil || r.Form.Get("audience") != "research-agent" || r.Form.Get("scope") != wantScope {
			http.Error(w, "missing audience or scope", http.StatusBadRequest)
			return
		}
		n := tokenRequests.Add(1)
		expiresIn := 3600
		if n == 1 {
			expiresIn = 5
		}
		_, _ = fmt.Fprintf(w, `{"access_token":"token-%d","token_type":"Bearer","expires_in":%d}`, n, expiresIn)
	}))
	t.Cleanup(issuer.Close)
	return issuer, &tokenRequests
}

const oidcAgentsYAML = `agents:
  - name: research
    url: %s
    auth:
      oidc:
        client_id: infer
        client_secret_env: RESEARCH_CLIENT_SECRET
        audience: research-agent
`

// TestNewClient_OIDCTokenComesFromTheCardAndIsReused: the agent card says
// where tokens are issued. Clients are built per request, yet they share one
// token until it is about to expire.
func TestNewClient_OIDCTokenComesFromTheCardAndIsReused(t *testing.T) {
	tests := []struct {
		name   string
		scheme func(issuerURL string) string
	}{
		{"openIdConnect scheme", func(issuerURL string) string {
			return fmt.Sprintf(`{"openIdConnectSecurityScheme":{"openIdConnectUrl":%q}}`, issuerURL+"/.well-known/openid-configuration")
		}},
		{"oauth2 client-credentials flow", func(issuerURL string) string {
			return fmt.Sprintf(`{"oauth2SecurityScheme":{"flows":{"clientCredentials":{"tokenUrl":%q,"scopes":{}}}}}`, issuerURL+"/token")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issuer, _ := fakeIssuer(t, "tasks")
			agent := &authRecorder{card: fmt.Sprintf(
				`{"name":"research","securitySchemes":{"idp":%s},"securityRequirements":[{"schemes":{"idp":{"list":["tasks"]}}}]}`,
				tt.scheme(issuer.URL))}
			srv := httptest.NewServer(agent)
			defer srv.Close()
			writeAgentsYAML(t, fmt.Sprintf(oidcAgentsYAML, srv.URL))
			t.Setenv("RESEARCH_CLIENT_SECRET", "s3cret")

			for range 4 {
				if _, err := NewClient(srv.URL).GetTask(t.Context(), adk.GetTaskRequest{ID: "t1"}); err != nil {
					t.Fatalf("GetTask: %v", err)
				}
			}

			want := []string{"", "Bearer token-1", "Bearer token-2", "Bearer token-2", "Bearer token-2"}
			if got := agent.headers(); strings.Join(got, ",") != strings.Join(want, ",") {
				t.Fatalf("Authorization headers = %q, want %q (one card read, the first token expires at once, the second is reused)", got, want)
			}
		})
	}
}

// TestNewClient_OIDCFallsBackToTheConfiguredIssuer: an agent whose card
// declares no security scheme is reached through auth.oidc.issuer_url.
func TestNewClient_OIDCFallsBackToTheConfiguredIssuer(t *testing.T) {
	issuer, _ := fakeIssuer(t, "")
	agent := &authRecorder{}
	srv := httptest.NewServer(agent)
	defer srv.Close()
	writeAgentsYAML(t, fmt.Sprintf(oidcAgentsYAML, srv.URL)+"        issuer_url: "+issuer.URL+"\n")
	t.Setenv("RESEARCH_CLIENT_SECRET", "s3cret")

	if _, err := NewClient(srv.URL).GetTask(t.Context(), adk.GetTaskRequest{ID: "t1"}); err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	want := []string{"", "Bearer token-1"}
	if got := agent.headers(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("Authorization headers = %q, want %q", got, want)
	}
}

func TestNewClient_OIDCRefusesACardItCannotTrust(t *testing.T) {
	tests := []struct {
		name       string
		card       func(issuerURL string) string
		pin        string
		wantReason string
	}{
		{
			name:       "card declares no usable scheme",
			card:       func(string) string { return `{"name":"research"}` },
			wantReason: "declares no openIdConnect or oauth2 client-credentials security scheme",
		},
		{
			name: "card points outside the pinned issuer",
			card: func(issuerURL string) string {
				return fmt.Sprintf(`{"name":"research","securitySchemes":{"idp":{"openIdConnectSecurityScheme":{"openIdConnectUrl":%q}}}}`, issuerURL+"/.well-known/openid-configuration")
			},
			pin:        "https://idp.example.com/realms/agents",
			wantReason: "outside the pinned issuer_url",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issuer, tokenRequests := fakeIssuer(t, "tasks")
			srv := httptest.NewServer(&authRecorder{card: tt.card(issuer.URL)})
			defer srv.Close()
			agents := fmt.Sprintf(oidcAgentsYAML, srv.URL)
			if tt.pin != "" {
				agents += "        issuer_url: " + tt.pin + "\n"
			}
			writeAgentsYAML(t, agents)
			t.Setenv("RESEARCH_CLIENT_SECRET", "s3cret")

			_, err := NewClient(srv.URL).GetAgentCard(t.Context())

			var authErr *AuthError
			if !errors.As(err, &authErr) || !strings.Contains(authErr.Reason, tt.wantReason) {
				t.Fatalf("err = %v, want an AuthError containing %q", err, tt.wantReason)
			}
			if n := tokenRequests.Load(); n != 0 {
				t.Fatalf("the client secret was sent to the issuer %d times", n)
			}
		})
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

// useGatewayCredential makes key the credential of the gateway at gatewayURL for one test.
func useGatewayCredential(t *testing.T, gatewayURL, key string) {
	t.Helper()
	UseGatewayCredential(gatewayURL, key)
	t.Cleanup(func() { UseGatewayCredential("", "") })
}

func TestNewClient_SendsGatewayKeyOnEveryRequestType(t *testing.T) {
	requests := map[string]func(t *testing.T, c client.A2AClient) error{
		"agent card": fetchCard,
		"send message": func(t *testing.T, c client.A2AClient) error {
			_, err := c.SendTask(t.Context(), adk.SendMessageRequest{})
			return err
		},
		"get task": fetchTask,
		"cancel task": func(t *testing.T, c client.A2AClient) error {
			_, err := c.CancelTask(t.Context(), adk.CancelTaskRequest{ID: "t1"})
			return err
		},
	}
	for name, send := range requests {
		t.Run(name, func(t *testing.T) {
			gateway := &authRecorder{}
			srv := httptest.NewServer(gateway)
			defer srv.Close()
			writeAgentsYAML(t, "agents: []\n")
			useGatewayCredential(t, srv.URL+"/v1", "gateway-key")

			if err := send(t, NewClient(srv.URL)); err != nil {
				t.Fatalf("request failed: %v", err)
			}
			if got := gateway.headers(); len(got) != 1 || got[0] != "Bearer gateway-key" {
				t.Fatalf("Authorization headers = %q, want one Bearer gateway-key", got)
			}
		})
	}
}

func TestNewClient_KeepsGatewayKeyOnTheGateway(t *testing.T) {
	other := &authRecorder{}
	otherSrv := httptest.NewServer(other)
	defer otherSrv.Close()
	gateway := httptest.NewServer(http.RedirectHandler(otherSrv.URL+"/.well-known/agent-card.json", http.StatusFound))
	defer gateway.Close()
	writeAgentsYAML(t, "agents: []\n")
	useGatewayCredential(t, gateway.URL, "gateway-key")

	if _, err := NewClient(gateway.URL).GetAgentCard(t.Context()); err != nil {
		t.Fatalf("redirected card fetch: %v", err)
	}
	if _, err := NewClient(otherSrv.URL).GetAgentCard(t.Context()); err != nil {
		t.Fatalf("other agent card fetch: %v", err)
	}
	for _, header := range other.headers() {
		if header != "" {
			t.Fatalf("another origin received Authorization %q", header)
		}
	}
}

func TestNewClient_AgentCredentialsBeatTheGatewayKey(t *testing.T) {
	gateway := &authRecorder{}
	srv := httptest.NewServer(gateway)
	defer srv.Close()
	writeAgentsYAML(t, fmt.Sprintf("agents:\n  - name: gateway\n    url: %s\n    auth:\n      token_env: GATEWAY_AGENT_TOKEN\n", srv.URL))
	t.Setenv("GATEWAY_AGENT_TOKEN", "agent-token")
	useGatewayCredential(t, srv.URL, "gateway-key")

	if err := fetchTask(t, NewClient(srv.URL)); err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if got := gateway.headers(); len(got) != 1 || got[0] != "Bearer agent-token" {
		t.Fatalf("Authorization headers = %q, want one Bearer agent-token", got)
	}
}

// TestRejection_ReadsTheGatewayResponses pins the ADK error text Rejection
// reads a gateway 401 and a guardrail 403 from.
func TestRejection_ReadsTheGatewayResponses(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		want       []string
		wantAbsent string
	}{
		{
			name:       "401 is an authentication failure without the body",
			status:     http.StatusUnauthorized,
			body:       `{"error":"unauthorized body-marker"}`,
			want:       []string{"Authentication failed", "401", "gateway.api_key"},
			wantAbsent: "body-marker",
		},
		{
			name:       "403 with a JSON-RPC error carries the policy message",
			status:     http.StatusForbidden,
			body:       `{"jsonrpc":"2.0","id":"1","error":{"code":-32001,"message":"request contains sensitive payment information"}}`,
			want:       []string{"refused by a guardrail policy", "request contains sensitive payment information"},
			wantAbsent: "Authentication failed",
		},
		{
			name:       "method not found says retrying will not help",
			status:     http.StatusOK,
			body:       `{"jsonrpc":"2.0","id":"1","error":{"code":-32601,"message":"method not found: GetTask"}}`,
			want:       []string{"does not implement the A2A method", "Retrying will not help"},
			wantAbsent: "Authentication failed",
		},
		{
			name:       "403 without a JSON-RPC error stays an authentication failure",
			status:     http.StatusForbidden,
			body:       `{"error":"forbidden body-marker"}`,
			want:       []string{"Authentication failed", "403"},
			wantAbsent: "body-marker",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, tt.body, tt.status)
			}))
			defer srv.Close()
			writeAgentsYAML(t, "agents: []\n")
			useGatewayCredential(t, srv.URL, "gateway-key")

			message, rejected := Rejection(srv.URL, fetchTask(t, NewClient(srv.URL)))

			if !rejected {
				t.Fatal("Rejection did not recognise the response")
			}
			for _, want := range tt.want {
				if !strings.Contains(message, want) {
					t.Errorf("message %q does not contain %q", message, want)
				}
			}
			for _, leaked := range []string{"gateway-key", tt.wantAbsent} {
				if strings.Contains(message, leaked) {
					t.Errorf("message %q contains %q", message, leaked)
				}
			}
		})
	}
}

// writeUserspaceAgentsYAML makes body the ~/.infer/agents.yaml of a home
// directory that holds no project, so token_command is honoured.
func writeUserspaceAgentsYAML(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, ".infer"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".infer", "agents.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "project"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(filepath.Join(dir, "project"))
}

func TestNewClient_TokenFileIsReadOnEveryRequest(t *testing.T) {
	agent := &authRecorder{}
	srv := httptest.NewServer(agent)
	defer srv.Close()
	tokenPath := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenPath, []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeAgentsYAML(t, fmt.Sprintf("agents:\n  - name: research\n    url: %s\n    auth:\n      token_file: %s\n", srv.URL, tokenPath))

	c := NewClient(srv.URL)
	if err := fetchCard(t, c); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tokenPath, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := fetchTask(t, c); err != nil {
		t.Fatal(err)
	}

	want := []string{"Bearer first", "Bearer second"}
	if got := agent.headers(); !slices.Equal(got, want) {
		t.Fatalf("headers = %q, want %q", got, want)
	}
}

func TestNewClient_UnreadableOrEmptyTokenFileIsAnAuthError(t *testing.T) {
	empty := filepath.Join(t.TempDir(), "empty")
	if err := os.WriteFile(empty, []byte(" \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{"missing": filepath.Join(t.TempDir(), "missing"), "empty": empty} {
		t.Run(name, func(t *testing.T) {
			agent := &authRecorder{}
			srv := httptest.NewServer(agent)
			defer srv.Close()
			writeAgentsYAML(t, fmt.Sprintf("agents:\n  - name: research\n    url: %s\n    auth:\n      token_file: %s\n", srv.URL, path))

			err := fetchCard(t, NewClient(srv.URL))

			var authErr *AuthError
			if !errors.As(err, &authErr) || !strings.Contains(authErr.Reason, path) {
				t.Fatalf("err = %v, want an AuthError naming %s", err, path)
			}
			if len(agent.headers()) != 0 {
				t.Fatal("a request was sent without credentials")
			}
		})
	}
}

// countingTokenCommand returns an argv that prints token and appends one line
// to a counter file, and a function reading how often it ran.
func countingTokenCommand(t *testing.T, token string) ([]string, func() int) {
	t.Helper()
	counter := filepath.Join(t.TempDir(), "runs")
	argv := []string{"sh", "-c", fmt.Sprintf("echo run >> %s && printf '%%s\\n' '%s'", counter, token)}
	runs := func() int {
		data, _ := os.ReadFile(counter)
		return strings.Count(string(data), "run")
	}
	return argv, runs
}

func agentsYAMLWithCommand(url string, argv []string) string {
	quoted := make([]string, len(argv))
	for i, arg := range argv {
		quoted[i] = fmt.Sprintf("%q", arg)
	}
	return fmt.Sprintf("agents:\n  - name: research\n    url: %s\n    auth:\n      token_command: [%s]\n", url, strings.Join(quoted, ", "))
}

func expiredJWT(t *testing.T) string {
	t.Helper()
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d}`, time.Now().Add(-time.Minute).Unix())))
	return "eyJhbGciOiJub25lIn0." + payload + ".sig"
}

func TestNewClient_TokenCommandIsReusedUntilItExpires(t *testing.T) {
	agent := &authRecorder{}
	srv := httptest.NewServer(agent)
	defer srv.Close()
	argv, runs := countingTokenCommand(t, "cmd-token")
	writeUserspaceAgentsYAML(t, agentsYAMLWithCommand(srv.URL, argv))

	c := NewClient(srv.URL)
	for range 2 {
		if err := fetchTask(t, c); err != nil {
			t.Fatal(err)
		}
	}

	if got := agent.headers(); !slices.Equal(got, []string{"Bearer cmd-token", "Bearer cmd-token"}) {
		t.Fatalf("headers = %q", got)
	}
	if runs() != 1 {
		t.Fatalf("the command ran %d times, want once", runs())
	}
}

func TestNewClient_ExpiredJWTFromTokenCommandIsFetchedAgain(t *testing.T) {
	agent := &authRecorder{}
	srv := httptest.NewServer(agent)
	defer srv.Close()
	argv, runs := countingTokenCommand(t, expiredJWT(t))
	writeUserspaceAgentsYAML(t, agentsYAMLWithCommand(srv.URL, argv))

	c := NewClient(srv.URL)
	for range 2 {
		if err := fetchTask(t, c); err != nil {
			t.Fatal(err)
		}
	}

	if runs() != 2 {
		t.Fatalf("the command ran %d times, want twice", runs())
	}
}

func TestNewClient_FailingTokenCommandIsAnAuthErrorWithoutItsOutput(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want string
	}{
		{"non-zero exit", []string{"sh", "-c", "echo leaked-secret; exit 3"}, "exit status 3"},
		{"no output", []string{"true"}, "printed no token"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := &authRecorder{}
			srv := httptest.NewServer(agent)
			defer srv.Close()
			writeUserspaceAgentsYAML(t, agentsYAMLWithCommand(srv.URL, tt.argv))

			err := fetchCard(t, NewClient(srv.URL))

			var authErr *AuthError
			if !errors.As(err, &authErr) || !strings.Contains(authErr.Reason, tt.want) {
				t.Fatalf("err = %v, want an AuthError containing %q", err, tt.want)
			}
			if strings.Contains(authErr.Reason, "leaked-secret") {
				t.Fatalf("the error quotes the command output: %v", err)
			}
			if len(agent.headers()) != 0 {
				t.Fatal("a request was sent without credentials")
			}
		})
	}
}

func TestNewClient_TokenCommandInAProjectFileIsRefused(t *testing.T) {
	agent := &authRecorder{}
	srv := httptest.NewServer(agent)
	defer srv.Close()
	writeAgentsYAML(t, agentsYAMLWithCommand(srv.URL, []string{"echo", "tok"}))
	t.Setenv("HOME", t.TempDir())

	err := fetchCard(t, NewClient(srv.URL))

	var authErr *AuthError
	if !errors.As(err, &authErr) || !strings.Contains(authErr.Reason, "~/.infer/agents.yaml") {
		t.Fatalf("err = %v, want an AuthError naming the userspace file", err)
	}
	if len(agent.headers()) != 0 {
		t.Fatal("a request was sent without credentials")
	}
}

func TestNewClient_SeveralCredentialSourcesAreAnAuthError(t *testing.T) {
	agent := &authRecorder{}
	srv := httptest.NewServer(agent)
	defer srv.Close()
	t.Setenv("RESEARCH_TOKEN", "s3cret")
	writeAgentsYAML(t, fmt.Sprintf("agents:\n  - name: research\n    url: %s\n    auth:\n      token_env: RESEARCH_TOKEN\n      token_file: /dev/null\n", srv.URL))

	err := fetchCard(t, NewClient(srv.URL))

	var authErr *AuthError
	if !errors.As(err, &authErr) || !strings.Contains(authErr.Reason, "keep one") {
		t.Fatalf("err = %v, want an AuthError asking for one source", err)
	}
}
