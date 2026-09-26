package insights

import (
	"context"
	"strings"
	"testing"
	"time"

	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
)

const testToken = "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnop"

// TestBuildDigestRedactsSecrets is the load-bearing one: a failed tool call
// whose error echoes a credential must leave the digest masked before it
// reaches the analysis model, without disturbing the counts.
func TestBuildDigestRedactsSecrets(t *testing.T) {
	store := newFakeStore(
		[]convdomain.ConversationSummary{{ID: "a", Title: "Leaky session", Project: "/repos/cli", UpdatedAt: time.Now()}},
		map[string][]convdomain.ConversationEntry{
			"a": {userEntry("why does curl fail"), toolEntry("Bash", "curl failed: Authorization: Bearer "+testToken, false, false)},
		},
	)

	g := &Generator{store: store}
	sessions, failures, err := g.collect(context.Background(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}

	digest := buildDigest(sessions, failures, nil, "", logDigest{})

	if strings.Contains(digest, testToken) {
		t.Errorf("a token echoed by a failed tool call reached the digest:\n%s", digest)
	}
	if !strings.Contains(digest, "Bearer "+redacted) {
		t.Errorf("the masked sample must keep its error context:\n%s", digest)
	}
	if totalErrors(failures[0]) != 1 {
		t.Errorf("redaction must not affect counts, got %d", totalErrors(failures[0]))
	}
}

// TestRenderReportRedactsSamples pins the saved report: failure and log
// samples are masked the same way the digest is.
func TestRenderReportRedactsSamples(t *testing.T) {
	failures := []toolFailure{{
		Tool:   "Bash",
		Errors: map[string]int{"auth": 1},
		Sample: map[string]string{"auth": "Bearer " + testToken},
	}}
	logs := logDigest{Groups: []logGroup{{Count: 3, Sample: "token " + testToken + " rejected"}}}

	got := renderReport(reportMeta{Generated: time.Now()}, failures, nil, logs, "analysis")

	if strings.Contains(got, testToken) {
		t.Errorf("the saved report must not carry the token:\n%s", got)
	}
	if !strings.Contains(got, "Bearer "+redacted) {
		t.Errorf("the report sample must keep its context:\n%s", got)
	}
}

// TestRedactCredentialShapes covers the always-on patterns, in step with
// infer-action's redact.ts.
func TestRedactCredentialShapes(t *testing.T) {
	fineGrained := "github_pat_" + strings.Repeat("a1_", 27) + "a"
	pem := "-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAK\n-----END RSA PRIVATE KEY-----"
	openssh := "-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaG9ydA\n-----END OPENSSH PRIVATE KEY-----"

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"classic user token", "token " + testToken + " in url", "token " + redacted + " in url"},
		{"github fine-grained token", "pat " + fineGrained + " expired", "pat " + redacted + " expired"},
		{"pem key", "cert " + pem + " rejected", "cert " + redacted + " rejected"},
		{"openssh key", pem + "\n" + openssh, redacted + "\n" + redacted},
		{"short token shape survives", "ghp_short", "ghp_short"},
		{"plain text survives", "exit status 2 on /repos/cli", "exit status 2 on /repos/cli"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := redact(tt.input); got != tt.want {
				t.Errorf("redact(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestRedactSecretEnvValues covers the environment: a provider key set in this
// process is masked plain and JSON-escaped, a too-short value is not.
func TestRedactSecretEnvValues(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", `sk-secret"value123`)
	t.Setenv("OPENAI_API_KEY", "tiny")

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain env value", `failed auth with sk-secret"value123`, "failed auth with " + redacted},
		{"json-escaped env value", `{"error":"auth with sk-secret\"value123"}`, `{"error":"auth with ` + redacted + `"}`},
		{"too-short env value survives", "tiny", "tiny"},
		{"unrelated text survives", "exit status 2", "exit status 2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := redact(tt.input); got != tt.want {
				t.Errorf("redact(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
