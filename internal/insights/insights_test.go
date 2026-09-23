package insights

import (
	"context"
	"errors"
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
	llm "github.com/inference-gateway/cli/internal/platform/llm"
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
			{ID: "stub", Title: "New Conversation", Project: "/repos/cli", UpdatedAt: time.Now()},
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
			"stub": {
				{Message: sdk.Message{Role: sdk.Assistant, Content: sdk.NewMessageContent("# Insights report")}},
			},
		},
	)

	g := &Generator{store: store}
	sessions, failures, err := g.collect(context.Background(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}

	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions (the report-only stub skipped), got %d", len(sessions))
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

	digest := buildDigest(sessions, failures, []telemetry.ToolStat{{Name: "Grep", Calls: 4, Failures: 3, AvgMs: 47}}, "", logDigest{})
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
	if got := len(buildDigest(sessions, nil, nil, "", logDigest{})); got > maxDigestChars+3 {
		t.Errorf("digest not bounded: %d chars", got)
	}
}

// TestGeneratorAvailable covers the nil dependencies the metadata
// registry constructs shortcuts with.
func TestGeneratorAvailable(t *testing.T) {
	var nilGen *Generator
	if nilGen.Available() {
		t.Error("a nil generator must not report itself available")
	}
	if (&Generator{}).Available() {
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

		LogRecords: 1482,
		LogGroups:  9,
	}

	got := renderReport(meta, nil, nil, logDigest{}, "### Repeatable workflows worth a skill\nNothing repeats yet.")

	for _, want := range []string{
		"generated: 2026-09-18T14:51:45Z",
		`model: "ollama_cloud/glm-5.3-flash"`,
		`infer_version: "1.2.3"`,
		`window_since: "2026-09-17T14:51:45Z"`,
		"sessions: 10",
		"tool_calls: 997",
		"tool_failures: 37",
		"log_records: 1482",
		"log_groups: 9",
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
	got := renderReport(reportMeta{Generated: time.Now()}, nil, nil, logDigest{}, "x")
	if !strings.Contains(got, `window_since: "all"`) {
		t.Errorf("an empty window must render as all:\n%s", got)
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

	index := (&Generator{cfg: cfg}).memoryIndex()
	if countMemoryFacts(index) != 2 {
		t.Errorf("expected 2 facts, got %d from:\n%s", countMemoryFacts(index), index)
	}

	digest := buildDigest(nil, nil, nil, index, logDigest{})
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

	index := (&Generator{cfg: cfg}).memoryIndex()

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

	if got := (&Generator{cfg: memoryConfig(4000)}).memoryIndex(); got != "" {
		t.Errorf("missing memory dir should yield an empty index, got %q", got)
	}

	disabled := memoryConfig(4000)
	disabled.Memory.Enabled = false
	writeMemoryIndex(t, disabled, "- [x](x.md) - y\n")
	if got := (&Generator{cfg: disabled}).memoryIndex(); got != "" {
		t.Errorf("disabled memory should yield an empty index, got %q", got)
	}

	if got := (&Generator{}).memoryIndex(); got != "" {
		t.Errorf("nil config should yield an empty index, got %q", got)
	}
}

// fakeAnalyzer returns a FakeClient serving one canned analysis.
func fakeAnalyzer(content string, finish sdk.FinishReason) *sdkmocks.FakeClient {
	client := &sdkmocks.FakeClient{}
	client.WithOptionsReturns(client)
	client.WithMiddlewareOptionsReturns(client)
	client.GenerateContentReturns(&sdk.CreateChatCompletionResponse{
		Choices: []sdk.ChatCompletionChoice{{
			FinishReason: finish,
			Message:      sdk.Message{Content: sdk.NewMessageContent(content)},
		}},
	}, nil)
	return client
}

// TestAnalyzeUsesAgentMaxTokens pins the floor: agent.max_tokens can raise the
// budget but not lower it below what a reasoning model needs to answer at all.
func TestAnalyzeUsesAgentMaxTokens(t *testing.T) {
	tests := []struct {
		name      string
		cfg       *config.Config
		wantToken int
	}{
		{"a larger budget wins", &config.Config{Agent: config.AgentConfig{MaxTokens: 64000}}, 64000},
		{"the chat default cannot lower the floor", &config.Config{Agent: config.AgentConfig{MaxTokens: 8192}}, insightsMinTokens},
		{"unset falls back to the floor", &config.Config{}, insightsMinTokens},
		{"nil config falls back to the floor", nil, insightsMinTokens},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fakeAnalyzer("done", sdk.Stop)
			g := &Generator{client: client, cfg: tt.cfg}

			if _, _, err := g.analyze(context.Background(), "openai/gpt-4o", "DIGEST"); err != nil {
				t.Fatal(err)
			}

			opts := client.WithOptionsArgsForCall(0)
			if opts == nil || opts.MaxTokens == nil {
				t.Fatalf("no max_tokens sent")
			}
			if *opts.MaxTokens != tt.wantToken {
				t.Errorf("max_tokens = %d, want %d", *opts.MaxTokens, tt.wantToken)
			}
		})
	}
}

// TestBudgetErrorNamesTheKnob keeps the failure actionable.
func TestBudgetErrorNamesTheKnob(t *testing.T) {
	g := &Generator{client: fakeAnalyzer("", sdk.Length), cfg: &config.Config{}}

	_, _, err := g.analyze(context.Background(), "openai/gpt-4o", "DIGEST")
	if err == nil {
		t.Fatal("expected a budget error")
	}
	if !errors.Is(err, llm.ErrTokenBudgetExhausted) {
		t.Errorf("budget failure must be identifiable with errors.Is, got: %v", err)
	}
	if !strings.Contains(err.Error(), "agent.max_tokens") {
		t.Errorf("error must name the knob to raise, got: %v", err)
	}

	empty := &Generator{client: fakeAnalyzer("  ", sdk.Stop), cfg: &config.Config{}}
	_, _, err = empty.analyze(context.Background(), "openai/gpt-4o", "DIGEST")
	if err == nil || errors.Is(err, llm.ErrTokenBudgetExhausted) {
		t.Errorf("an unrelated empty response must not carry the budget remedy, got: %v", err)
	}
}

// TestAnalysisIsCapped bounds what reaches the report regardless of what the
// model returns.
func TestAnalysisIsCapped(t *testing.T) {
	var long strings.Builder
	for i := range 500 {
		fmt.Fprintf(&long, "line %d\n", i)
	}
	g := &Generator{client: fakeAnalyzer(long.String(), sdk.Stop), cfg: &config.Config{}}

	analysis, _, err := g.analyze(context.Background(), "openai/gpt-4o", "DIGEST")
	if err != nil {
		t.Fatal(err)
	}

	if got := strings.Count(analysis, "\n") + 1; got > insightsMaxAnalysisLines+3 {
		t.Errorf("analysis not capped: %d lines", got)
	}
	if len(analysis) > insightsMaxAnalysisChars+64 {
		t.Errorf("analysis not capped: %d chars", len(analysis))
	}
	if !strings.Contains(analysis, "truncated") {
		t.Errorf("a truncated analysis must say so:\n%s", analysis[max(0, len(analysis)-200):])
	}
	if strings.Contains(analysis, "line 499") {
		t.Errorf("cap must drop the tail")
	}
}

// TestLogSectionSurvivesManySessions checks a busy conversation store cannot
// push the log section past the digest cap and out of the prompt.
func TestLogSectionSurvivesManySessions(t *testing.T) {
	sessions := make([]sessionDigest, 50)
	for i := range sessions {
		sessions[i] = sessionDigest{
			Title:   strings.Repeat("t", 200),
			Project: "/repos/cli",
			Intent:  strings.Repeat("i", 200),
			Tools:   []string{strings.Repeat("T", 100)},
		}
	}

	failures := make([]toolFailure, 30)
	for i := range failures {
		errs := map[string]int{}
		samples := map[string]string{}
		for j := range maxErrorsPerTool {
			key := fmt.Sprintf("err-%d-%d %s", i, j, strings.Repeat("e", 150))
			errs[key] = j + 1
			samples[key] = key
		}
		failures[i] = toolFailure{Tool: fmt.Sprintf("Tool%d", i), Errors: errs, Sample: samples}
	}

	logs := logDigest{Scanned: 400, Groups: []logGroup{{
		Template: "failed to start gateway container port n already in use",
		Count:    400,
		First:    time.Now().Add(-time.Minute),
		Last:     time.Now(),
		Sample:   "failed to start gateway container port 8080 already in use",
	}}}

	digest := buildDigest(sessions, failures, nil, "", logs)

	if len(digest) > maxDigestChars+3 {
		t.Errorf("digest not bounded: %d chars", len(digest))
	}
	for _, want := range []string{"LOG FAILURES", "x400", "failed to start gateway container"} {
		if !strings.Contains(digest, want) {
			t.Errorf("a busy session store must not crowd out %q:\n...%s", want, digest[max(0, len(digest)-400):])
		}
	}
}

// TestPromptCoversEveryDigestSection catches a digest section the prompt never
// tells the model to report on.
func TestPromptCoversEveryDigestSection(t *testing.T) {
	digest := buildDigest(
		[]sessionDigest{{Title: "t", Project: "/repos/cli"}},
		[]toolFailure{{Tool: "Grep", Errors: map[string]int{"e": 1}, Sample: map[string]string{"e": "boom"}}},
		[]telemetry.ToolStat{{Name: "Grep", Calls: 1}},
		"- [fact](fact.md) - x",
		logDigest{Scanned: 1, Groups: []logGroup{{Count: 1, Sample: "boom"}}},
	)

	headings := map[string]string{
		"PERSISTENT MEMORY": "PERSISTENT MEMORY",
		"SESSIONS":          "sessions",
		"TOOL CALL TOTALS":  "Tool calls that keep failing",
		"FAILED TOOL CALLS": "Tool calls that keep failing",
		"LOG FAILURES":      "LOG FAILURES",
	}
	for section, instruction := range headings {
		if !strings.Contains(digest, section) {
			t.Fatalf("digest no longer emits %q - update this test", section)
		}
		if !strings.Contains(insightsPrompt, instruction) {
			t.Errorf("digest carries %q but the prompt never mentions %q, so the model is told to ignore it", section, instruction)
		}
	}
}

// TestReportRecordsWhatItCost pins the token counts in the frontmatter.
func TestReportRecordsWhatItCost(t *testing.T) {
	reasoning := int64(9000)
	usage := &sdk.CompletionUsage{PromptTokens: 1704, CompletionTokens: 13225, TotalTokens: 14929}
	usage.CompletionTokensDetails = &struct {
		AcceptedPredictionTokens *int64 `json:"accepted_prediction_tokens,omitempty"`
		AudioTokens              *int64 `json:"audio_tokens,omitempty"`
		ReasoningTokens          *int64 `json:"reasoning_tokens,omitempty"`
		RejectedPredictionTokens *int64 `json:"rejected_prediction_tokens,omitempty"`
	}{ReasoningTokens: &reasoning}

	got := renderReport(reportMeta{Generated: time.Now(), Usage: usage}, nil, nil, logDigest{}, "x")
	for _, want := range []string{
		"analysis_prompt_tokens: 1704",
		"analysis_completion_tokens: 13225",
		"analysis_total_tokens: 14929",
		"analysis_reasoning_tokens: 9000",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("frontmatter missing %q:\n%s", want, got)
		}
	}

	none := renderReport(reportMeta{Generated: time.Now()}, nil, nil, logDigest{}, "x")
	if strings.Contains(none, "analysis_total_tokens") {
		t.Errorf("absent usage must not emit the keys:\n%s", none)
	}
	flat := renderReport(reportMeta{Generated: time.Now(), Usage: &sdk.CompletionUsage{TotalTokens: 10}}, nil, nil, logDigest{}, "x")
	if strings.Contains(flat, "analysis_reasoning_tokens") {
		t.Errorf("a zero reasoning breakdown must be omitted, not printed as 0:\n%s", flat)
	}
}
