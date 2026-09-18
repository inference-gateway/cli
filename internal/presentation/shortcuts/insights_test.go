package shortcuts

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdkmocks "github.com/inference-gateway/cli/tests/mocks/sdk"
	storagemocks "github.com/inference-gateway/cli/tests/mocks/storage"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	telemetry "github.com/inference-gateway/cli/internal/platform/telemetry"
)

// newFakeStore wires the generated ConversationStorage fake to serve a fixed set
// of sessions.
func newFakeStore(summaries []convdomain.ConversationSummary, entries map[string][]convdomain.ConversationEntry) *storagemocks.FakeConversationStorage {
	store := &storagemocks.FakeConversationStorage{}
	store.ListConversationsReturns(summaries, nil)
	store.LoadConversationStub = func(_ context.Context, id string) ([]convdomain.ConversationEntry, convdomain.ConversationMetadata, error) {
		return entries[id], convdomain.ConversationMetadata{ID: id}, nil
	}
	return store
}

func userEntry(text string) convdomain.ConversationEntry {
	return convdomain.ConversationEntry{
		Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent(text)},
	}
}

func toolEntry(name, errMsg string, success, rejected bool) convdomain.ConversationEntry {
	return convdomain.ConversationEntry{
		Message: sdk.Message{Role: "tool", Content: sdk.NewMessageContent("")},
		ToolExecution: &agentdomain.ToolExecutionResult{
			ToolName: name,
			Success:  success,
			Rejected: rejected,
			Error:    errMsg,
		},
	}
}

// TestCollectAndBuildDigest covers the deterministic half of the feature: a
// failed tool call is counted, a rejection is not, and repeats of the same error
// fold together regardless of the numbers inside the message.
func TestCollectAndBuildDigest(t *testing.T) {
	store := newFakeStore(
		[]convdomain.ConversationSummary{
			{ID: "a", Title: "Fix flaky test", Project: "/repos/cli", UpdatedAt: time.Now()},
			{ID: "b", Title: "Add reset flag", Project: "/repos/cli", UpdatedAt: time.Now()},
		},
		map[string][]convdomain.ConversationEntry{
			"a": {
				userEntry("please fix the flaky test"),
				toolEntry("Grep", "ripgrep execution failed: exit status 2", false, false),
				toolEntry("Grep", "ripgrep execution failed: exit status 127", false, false),
				toolEntry("Read", "", true, false),
			},
			"b": {
				userEntry("add an insights flag to reset"),
				toolEntry("Delete", "user declined", false, true),
				toolEntry("Grep", "ripgrep execution failed: exit status 2", false, false),
			},
		},
	)

	g := &InsightsGenerator{store: store}
	sessions, failures, err := g.collect(context.Background(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}

	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}
	if sessions[0].Intent != "please fix the flaky test" {
		t.Errorf("intent not taken from the first user message: %q", sessions[0].Intent)
	}

	if len(failures) != 1 {
		t.Fatalf("expected only Grep to have failures, got %d entries: %+v", len(failures), failures)
	}
	if failures[0].Tool != "Grep" {
		t.Errorf("expected Grep, got %s", failures[0].Tool)
	}
	if got := totalErrors(failures[0]); got != 3 {
		t.Errorf("expected 3 Grep failures, got %d", got)
	}
	if len(failures[0].Errors) != 1 {
		t.Errorf("exit status 2 and 127 should fold into one key, got %d: %+v", len(failures[0].Errors), failures[0].Errors)
	}

	digest := buildDigest(sessions, failures, []telemetry.ToolStat{{Name: "Grep", Calls: 4, Failures: 3, AvgMs: 47}}, "")
	for _, want := range []string{"Fix flaky test", "Add reset flag", "Grep x3", "ripgrep execution failed", "Grep: 4/3/47"} {
		if !strings.Contains(digest, want) {
			t.Errorf("digest missing %q:\n%s", want, digest)
		}
	}
	if strings.Contains(digest, "user declined") {
		t.Errorf("a rejection is not a failure and must not reach the digest:\n%s", digest)
	}
}

// TestBuildDigestBounded checks the digest stays capped no matter how many
// sessions the store holds.
func TestBuildDigestBounded(t *testing.T) {
	sessions := make([]sessionDigest, 500)
	for i := range sessions {
		sessions[i] = sessionDigest{Title: strings.Repeat("x", 200), Intent: strings.Repeat("y", 200)}
	}
	if got := len(buildDigest(sessions, nil, nil, "")); got > maxDigestChars+3 {
		t.Errorf("digest not bounded: %d chars", got)
	}
}

// TestInsightsGeneratorAvailable covers the nil dependencies the metadata
// registry constructs shortcuts with.
func TestInsightsGeneratorAvailable(t *testing.T) {
	var nilGen *InsightsGenerator
	if nilGen.Available() {
		t.Error("a nil generator must not report itself available")
	}
	if (&InsightsGenerator{}).Available() {
		t.Error("a generator without a store or client must not report itself available")
	}
}

// TestInsightsDirIsReadableByAgent checks the sandbox carve-out, so the agent can
// read a report back when asked to turn a suggestion into a skill. The reports
// live in userspace wherever config resolves from, so a project-local
// .infer/config.yaml must not hide them.
func TestInsightsDirIsReadableByAgent(t *testing.T) {
	for _, configDir := range []string{config.UserSpaceConfigDir(), config.ConfigDirName} {
		t.Run(configDir, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Chdir(t.TempDir())

			cfg := &config.Config{Storage: config.StorageConfig{Enabled: true, Type: config.StorageTypeJsonl}}
			cfg.Tools.Sandbox.ProtectedPaths = []string{".infer/"}
			cfg.SetConfigDir(configDir)

			if err := cfg.ValidatePathInSandbox(filepath.Join(config.InsightsDir(), "report.md")); err != nil {
				t.Errorf("insights report should be readable despite .infer/ being protected: %v", err)
			}
		})
	}
}

// TestRenderReportFrontmatter pins the metadata header: which model wrote the
// analysis, when, over what window, and which projects it covered.
func TestRenderReportFrontmatter(t *testing.T) {
	generated := time.Date(2026, 9, 18, 14, 51, 45, 0, time.UTC)
	meta := reportMeta{
		Generated: generated,
		Model:     "ollama_cloud/glm-5.3-flash",
		Version:   "1.2.3",
		Since:     generated.Add(-24 * time.Hour),
		Sessions:  10,
		Projects:  []string{"/repos/cli", "/repos/docs"},
		Calls:     997,
		Failures:  37,
	}

	got := renderReport(meta, nil, nil, "### Repeatable workflows worth a skill\nNothing repeats yet.")

	for _, want := range []string{
		"generated: 2026-09-18T14:51:45Z",
		`model: "ollama_cloud/glm-5.3-flash"`,
		`infer_version: "1.2.3"`,
		`window_since: "2026-09-17T14:51:45Z"`,
		"sessions: 10",
		"tool_calls: 997",
		"tool_failures: 37",
		`  - "/repos/cli"`,
		`  - "/repos/docs"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("frontmatter missing %q:\n%s", want, got)
		}
	}
	if !strings.HasPrefix(got, "---\n") {
		t.Errorf("report must open with frontmatter:\n%s", got)
	}
	if !strings.Contains(got, "## Analysis\n\n### Repeatable workflows") {
		t.Errorf("analysis must follow its heading:\n%s", got)
	}
}

// TestRenderReportAllTimeWindow covers the zero-value window.
func TestRenderReportAllTimeWindow(t *testing.T) {
	got := renderReport(reportMeta{Generated: time.Now()}, nil, nil, "x")
	if !strings.Contains(got, `window_since: "all"`) {
		t.Errorf("an empty window must render as all:\n%s", got)
	}
}

// TestCallLLMRejectsEmptyResponse covers the bug behind an empty "## Analysis":
// a reasoning model spends max_tokens thinking and returns no content, which
// callLLM used to hand back as a successful empty string.
func TestCallLLMRejectsEmptyResponse(t *testing.T) {
	tests := []struct {
		name    string
		finish  sdk.FinishReason
		content string
		wantErr string
	}{
		{"budget exhausted", sdk.Length, "", "max_tokens"},
		{"empty for another reason", sdk.Stop, "  \n ", "empty response"},
		{"real content passes", sdk.Stop, " analysis ", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &sdkmocks.FakeClient{}
			client.WithOptionsReturns(client)
			client.WithMiddlewareOptionsReturns(client)
			client.GenerateContentReturns(&sdk.CreateChatCompletionResponse{
				Choices: []sdk.ChatCompletionChoice{{
					FinishReason: tt.finish,
					Message:      sdk.Message{Content: sdk.NewMessageContent(tt.content)},
				}},
			}, nil)

			got, err := callLLM(context.Background(), client, "openai/gpt-4o", "prompt", 4000)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got != "analysis" {
					t.Errorf("expected trimmed content, got %q", got)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected an error naming %q, got content %q", tt.wantErr, got)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error must mention %q, got: %v", tt.wantErr, err)
			}
		})
	}
}

// memoryConfig points a generator at a memory dir under the test HOME.
func memoryConfig(maxChars int) *config.Config {
	cfg := &config.Config{Storage: config.StorageConfig{Enabled: true, Type: config.StorageTypeJsonl}}
	cfg.Memory.Enabled = true
	cfg.Memory.MaxChars = maxChars
	return cfg
}

func writeMemoryIndex(t *testing.T, cfg *config.Config, body string) {
	t.Helper()
	dir, err := cfg.ResolveMemoryDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, config.MemoryIndexFileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestMemoryIndexReachesTheDigest is the load-bearing one: `/reset insights`
// runs before the wipe precisely so the facts survive in the report.
func TestMemoryIndexReachesTheDigest(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := memoryConfig(4000)
	writeMemoryIndex(t, cfg, "- [prefers-tabs](prefers-tabs.md) - user indents Go with tabs\n- [cli/no-footers](cli/no-footers.md) - no commit footers\n")

	index := (&InsightsGenerator{cfg: cfg}).memoryIndex()
	if countMemoryFacts(index) != 2 {
		t.Errorf("expected 2 facts, got %d from:\n%s", countMemoryFacts(index), index)
	}

	digest := buildDigest(nil, nil, nil, index)
	for _, want := range []string{"PERSISTENT MEMORY", "prefers-tabs", "cli/no-footers"} {
		if !strings.Contains(digest, want) {
			t.Errorf("digest missing %q:\n%s", want, digest)
		}
	}
}

// TestMemoryIndexIsCapped keeps ingestion cheap in tokens: a runaway index must
// be truncated at Memory.MaxChars on a line boundary, not passed through whole.
func TestMemoryIndexIsCapped(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := memoryConfig(200)

	var body strings.Builder
	for i := range 100 {
		fmt.Fprintf(&body, "- [fact-%02d](fact-%02d.md) - a stored fact worth remembering\n", i, i)
	}
	writeMemoryIndex(t, cfg, body.String())

	index := (&InsightsGenerator{cfg: cfg}).memoryIndex()

	if len(index) > 200+len("\n... (memory index truncated)") {
		t.Errorf("index not capped: %d chars", len(index))
	}
	if !strings.Contains(index, "truncated") {
		t.Errorf("a truncated index must say so:\n%s", index)
	}
	if strings.Contains(index, "fact-99") {
		t.Errorf("cap must drop later entries:\n%s", index)
	}
}

// TestMemoryIndexAbsentIsHarmless covers the degrade path: no memory dir, or
// memory disabled, costs the section and nothing else.
func TestMemoryIndexAbsentIsHarmless(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if got := (&InsightsGenerator{cfg: memoryConfig(4000)}).memoryIndex(); got != "" {
		t.Errorf("missing memory dir should yield an empty index, got %q", got)
	}

	disabled := memoryConfig(4000)
	disabled.Memory.Enabled = false
	writeMemoryIndex(t, disabled, "- [x](x.md) - y\n")
	if got := (&InsightsGenerator{cfg: disabled}).memoryIndex(); got != "" {
		t.Errorf("disabled memory should yield an empty index, got %q", got)
	}

	if got := (&InsightsGenerator{}).memoryIndex(); got != "" {
		t.Errorf("nil config should yield an empty index, got %q", got)
	}
}
