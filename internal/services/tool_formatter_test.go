package services

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"
	uimocks "github.com/inference-gateway/cli/tests/mocks/ui"

	lipgloss "charm.land/lipgloss/v2"
	sdk "github.com/inference-gateway/sdk"

	domain "github.com/inference-gateway/cli/internal/domain"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

var ansiRE = regexp.MustCompile("\x1b\\[[0-9;]*m")

func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

// stripCard removes the rounded card framing (top/bottom border and the "│ … │"
// per line, plus horizontal padding) added by wrapCard, returning the inner content
// for structural assertions. Call it on already-ANSI-stripped output.
func stripCard(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) < 3 {
		return s
	}
	inner := make([]string, 0, len(lines)-2)
	for _, ln := range lines[1 : len(lines)-1] {
		ln = strings.TrimSuffix(ln, "│")
		ln = strings.TrimPrefix(ln, "│")
		ln = strings.TrimPrefix(ln, " ")
		inner = append(inner, strings.TrimRight(ln, " "))
	}
	return strings.Join(inner, "\n")
}

// cardTitle returns the top-border line of a card (carries the tool name).
func cardTitle(s string) string {
	return strings.SplitN(s, "\n", 2)[0]
}

// fakeTool is a configurable domain.Tool (and ResultBodyProvider) for formatter tests.
type fakeTool struct {
	name         string
	preview      string
	body         string
	hasBody      bool
	llm          string
	alwaysExpand bool
}

func (t *fakeTool) Definition() sdk.ChatCompletionTool { return sdk.ChatCompletionTool{} }
func (t *fakeTool) Execute(context.Context, map[string]any) (*domain.ToolExecutionResult, error) {
	return nil, nil
}
func (t *fakeTool) Validate(map[string]any) error { return nil }
func (t *fakeTool) IsEnabled() bool               { return true }
func (t *fakeTool) FormatResult(_ *domain.ToolExecutionResult, ft domain.FormatterType) string {
	if ft == domain.FormatterLLM {
		return t.llm
	}
	return t.preview
}
func (t *fakeTool) FormatPreview(*domain.ToolExecutionResult) string { return t.preview }
func (t *fakeTool) ShouldCollapseArg(string) bool                    { return false }
func (t *fakeTool) ShouldAlwaysExpand() bool                         { return t.alwaysExpand }

// FormatResultBody satisfies ResultBodyProvider; returns "" when hasBody is false so
// resultBody falls back to FormatPreview (simulating summary-only tools).
func (t *fakeTool) FormatResultBody(*domain.ToolExecutionResult) string {
	if !t.hasBody {
		return ""
	}
	return t.body
}

type fakeRegistry struct{ tool domain.Tool }

func (r *fakeRegistry) GetTool(string) (domain.Tool, error) { return r.tool, nil }
func (r *fakeRegistry) ListAvailableTools() []string        { return nil }

type fakeHint struct{}

func (fakeHint) GetKeyOnly(string) string { return "ctrl+o" }

func newTestStyleProvider() *styles.Provider {
	theme := &uimocks.FakeTheme{}
	theme.GetAccentColorReturns("#7aa2f7")
	theme.GetDimColorReturns("#7a7f9a")
	theme.GetSuccessColorReturns("#9ece6a")
	theme.GetErrorColorReturns("#f7768e")
	theme.GetAssistantColorReturns("#a9b1d6")
	ts := &domainmocks.FakeThemeService{}
	ts.GetCurrentThemeReturns(theme)
	return styles.NewProvider(ts)
}

func newTestService(tool domain.Tool) *ToolFormatterService {
	svc := NewToolFormatterService(&fakeRegistry{tool: tool}, newTestStyleProvider())
	svc.SetHintFormatter(fakeHint{})
	return svc
}

func bashResult(success bool, args map[string]any) *domain.ToolExecutionResult {
	return &domain.ToolExecutionResult{
		ToolName:  "Bash",
		Success:   success,
		Duration:  19 * time.Millisecond,
		Arguments: args,
	}
}

func TestFormatToolResultForUI_CollapsedSuccessTruncatesToFive(t *testing.T) {
	tool := &fakeTool{name: "Bash", hasBody: true, body: "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8"}
	svc := newTestService(tool)

	full := stripANSI(svc.FormatToolResultForUI(bashResult(true, map[string]any{"command": "git branch"}), 80))
	if !strings.Contains(cardTitle(full), "Bash") {
		t.Errorf("card top border missing tool name: %q", cardTitle(full))
	}
	lines := strings.Split(stripCard(full), "\n")

	if !strings.Contains(lines[0], "Bash(command=git branch)") || !strings.Contains(lines[0], "19ms") {
		t.Fatalf("status line missing name/duration: %q", lines[0])
	}
	// status + dim separator + 5 preview lines + footer
	if len(lines) != 8 {
		t.Fatalf("expected 8 inner lines, got %d: %#v", len(lines), lines)
	}
	for i, want := range []string{"l1", "l2", "l3", "l4", "l5"} {
		if strings.TrimSpace(lines[2+i]) != want {
			t.Errorf("preview line %d = %q, want %q", i, lines[2+i], want)
		}
	}
	footer := lines[7]
	if !strings.Contains(footer, "+3 lines") || !strings.Contains(footer, "ctrl+o to expand") {
		t.Errorf("footer = %q, want +3 lines + expand hint", footer)
	}
	if !strings.Contains(footer, "+3 lines · ") {
		t.Errorf("footer = %q, want count and hint side-by-side separated by a dot", footer)
	}
}

func TestFormatToolResultForUI_FailureShowsFullBody(t *testing.T) {
	tool := &fakeTool{name: "Bash", hasBody: true, body: "e1\ne2\ne3\ne4\ne5"}
	svc := newTestService(tool)

	inner := stripCard(stripANSI(svc.FormatToolResultForUI(bashResult(false, map[string]any{"command": "boom"}), 80)))
	lines := strings.Split(inner, "\n")

	// status + separator + 5 full body lines + hint footer
	if len(lines) != 8 {
		t.Fatalf("expected 8 inner lines, got %d: %#v", len(lines), lines)
	}
	if strings.Contains(inner, "+") && strings.Contains(inner, "more") {
		t.Errorf("failure output should not truncate: %q", inner)
	}
	if strings.Contains(inner, "+5") {
		t.Errorf("failure output should not show a hidden-line count: %q", inner)
	}
	if !strings.Contains(lines[len(lines)-1], "ctrl+o to expand") {
		t.Errorf("footer should still show expand hint: %q", lines[len(lines)-1])
	}
}

func TestFormatToolResultForUI_SummaryFallsBackToPreview(t *testing.T) {
	tool := &fakeTool{name: "Write", preview: "Created main.go (123 bytes)"}
	svc := newTestService(tool)

	res := &domain.ToolExecutionResult{ToolName: "Write", Success: true, Duration: 5 * time.Millisecond}
	lines := strings.Split(stripCard(stripANSI(svc.FormatToolResultForUI(res, 80))), "\n")

	// status + separator + 1 preview + footer
	if len(lines) != 4 {
		t.Fatalf("expected status + separator + 1 preview + footer = 4 lines, got %d: %#v", len(lines), lines)
	}
	if strings.TrimSpace(lines[2]) != "Created main.go (123 bytes)" {
		t.Errorf("preview line = %q", lines[2])
	}
}

func TestFormatToolResultForUI_NoBodyOmitsPreview(t *testing.T) {
	tool := &fakeTool{name: "Bash", hasBody: true, body: ""} // empty body, e.g. silent success
	svc := newTestService(tool)

	lines := strings.Split(stripCard(stripANSI(svc.FormatToolResultForUI(bashResult(true, nil), 80))), "\n")

	if len(lines) != 2 {
		t.Fatalf("expected 2 lines (status + hint), got %d: %#v", len(lines), lines)
	}
	if !strings.Contains(lines[1], "ctrl+o to expand") {
		t.Errorf("expected expand hint, got %q", lines[1])
	}
}

func TestFormatToolResultForUI_RejectedShowsStatusAndHintOnly(t *testing.T) {
	tool := &fakeTool{name: "Bash", preview: "Execution failed"}
	svc := newTestService(tool)

	res := &domain.ToolExecutionResult{
		ToolName:  "Bash",
		Success:   false,
		Rejected:  true,
		Error:     "rejected by user",
		Arguments: map[string]any{"command": "rm -rf x"},
	}
	lines := strings.Split(stripCard(stripANSI(svc.FormatToolResultForUI(res, 80))), "\n")

	if len(lines) != 2 {
		t.Fatalf("expected status + hint = 2 lines, got %d: %#v", len(lines), lines)
	}
	if !strings.Contains(lines[0], "Bash(command=rm -rf x)") || !strings.Contains(lines[0], "· Rejected") {
		t.Errorf("status line = %q, want args and Rejected label", lines[0])
	}
	if strings.Contains(lines[0], "0ns") {
		t.Errorf("rejected status line should not show a duration: %q", lines[0])
	}
	if !strings.Contains(lines[1], "ctrl+o to expand") {
		t.Errorf("expected expand hint, got %q", lines[1])
	}
}

func TestFormatToolResultExpanded_ThemingPreservesTree(t *testing.T) {
	tree := "Bash(command=git branch)\n" +
		"├─ Duration: 19ms\n" +
		"├─ Status: ✓ Success\n" +
		"└─ Result:\n" +
		"   Exit Code: 0\n" +
		"   * main"
	tool := &fakeTool{name: "Bash", llm: tree}
	svc := newTestService(tool)

	full := stripANSI(svc.FormatToolResultExpanded(bashResult(true, nil), 80))
	if !strings.Contains(cardTitle(full), "Bash") {
		t.Errorf("expanded card top border missing tool name: %q", cardTitle(full))
	}
	out := stripCard(full)

	want := tree + "\n· ctrl+o to collapse"
	if out != want {
		t.Errorf("themed tree changed content.\n got: %q\nwant: %q", out, want)
	}
}

func TestFormatToolResultExpanded_AlwaysExpandOmitsHint(t *testing.T) {
	tool := &fakeTool{name: "Edit", llm: "Edit(file_path=x)\n└─ Result:\n   diff", alwaysExpand: true}
	svc := newTestService(tool)

	out := stripANSI(svc.FormatToolResultExpanded(&domain.ToolExecutionResult{ToolName: "Edit", Success: true}, 80))
	if strings.Contains(out, "ctrl+o") {
		t.Errorf("always-expand tool should not show a collapse hint: %q", out)
	}
}

// TestRenderToolSummary_SharedAndWidthAware checks the one summary builder used by
// every surface: it formats the name + width-aware argument preview identically and
// omits an empty icon/trailing.
func TestRenderToolSummary_SharedAndWidthAware(t *testing.T) {
	svc := newTestService(&fakeTool{name: "Bash"})

	full := stripANSI(svc.RenderToolSummary("i", "Bash", map[string]any{"command": "ls"}, "t", 80))
	if full != "i Bash(command=ls) t" {
		t.Errorf("summary = %q, want %q", full, "i Bash(command=ls) t")
	}

	noIcon := stripANSI(svc.RenderToolSummary("", "Bash", nil, "", 80))
	if noIcon != "Bash()" {
		t.Errorf("summary without icon/args = %q, want %q", noIcon, "Bash()")
	}

	// long values are truncated to the budget, never byte-sliced mid-rune
	long := strings.Repeat("λ", 200)
	got := stripANSI(svc.RenderToolSummary("", "Bash", map[string]any{"command": long}, "", 80))
	if !strings.Contains(got, "…") && !strings.Contains(got, "...") {
		t.Errorf("long value should be truncated with an ellipsis: %q", got)
	}
	if strings.Contains(got, "�") {
		t.Errorf("truncation split a multibyte rune: %q", got)
	}
}

// TestFormatToolResultForUI_CardWidthTracksInput checks the card fills the width it is
// handed and grows with the terminal, rather than double-shrinking.
func TestFormatToolResultForUI_CardWidthTracksInput(t *testing.T) {
	svc := newTestService(&fakeTool{name: "Bash", hasBody: true, body: "l1\nl2"})

	for _, w := range []int{60, 100, 148} {
		card := svc.FormatToolResultForUI(bashResult(true, map[string]any{"command": "ls"}), w)
		if got := lipgloss.Width(stripANSI(card)); got != w {
			t.Errorf("card outer width at input %d = %d, want %d (must not double-shrink)", w, got, w)
		}
	}

	narrow := lipgloss.Width(stripANSI(svc.FormatToolResultForUI(bashResult(true, nil), 60)))
	wide := lipgloss.Width(stripANSI(svc.FormatToolResultForUI(bashResult(true, nil), 120)))
	if wide <= narrow {
		t.Errorf("card width should grow with terminal width: 60→%d, 120→%d", narrow, wide)
	}
}

// TestFormatToolResultForLLM_NoCard guards that the LLM/headless path is never
// card-framed and carries no injected UI styling.
func TestFormatToolResultForLLM_NoCard(t *testing.T) {
	tree := "Bash(command=x)\n├── Duration: 1ms\n╰── Result:\n   ok"
	svc := newTestService(&fakeTool{name: "Bash", llm: tree})
	got := svc.FormatToolResultForLLM(bashResult(true, nil))
	if strings.ContainsAny(got, "╭╮╯") {
		t.Errorf("LLM output must not be card-framed: %q", got)
	}
	if got != tree {
		t.Errorf("LLM output must be the tool tree verbatim: got %q want %q", got, tree)
	}
}

func TestFormatToolResultForLLM_Unchanged(t *testing.T) {
	tree := "Bash(command=x)\n├─ Duration: 1ms\n└─ Result:\n   ok"
	tool := &fakeTool{name: "Bash", llm: tree}
	svc := newTestService(tool)

	res := bashResult(true, nil)
	if got := svc.FormatToolResultForLLM(res); got != tree {
		t.Errorf("LLM format must be unchanged.\n got: %q\nwant: %q", got, tree)
	}
}

func TestPreviewLines(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		success   bool
		wantLines int
		wantMore  int
	}{
		{"success truncates", "a\nb\nc\nd\ne\nf\ng\nh", true, 5, 3},
		{"success short", "a\nb", true, 2, 0},
		{"success exact", "a\nb\nc\nd\ne", true, 5, 0},
		{"failure full", "a\nb\nc\nd\ne\nf\ng\nh", false, 8, 0},
		{"empty", "", true, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines, more := previewLines(tt.body, tt.success, 100)
			if len(lines) != tt.wantLines || more != tt.wantMore {
				t.Errorf("previewLines() = %d lines / more %d, want %d / %d", len(lines), more, tt.wantLines, tt.wantMore)
			}
		})
	}
}

func TestPreviewLinesWidthCapping(t *testing.T) {
	lines, _ := previewLines(strings.Repeat("x", 500), true, 30)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	if got := len([]rune(lines[0])); got > 30 {
		t.Errorf("line length %d exceeds width 30", got)
	}
}

func TestPluralizeLines(t *testing.T) {
	if pluralizeLines(1) != "+1 line" {
		t.Errorf("pluralizeLines(1) = %q", pluralizeLines(1))
	}
	if pluralizeLines(3) != "+3 lines" {
		t.Errorf("pluralizeLines(3) = %q", pluralizeLines(3))
	}
}

func TestSplitTreePrefix(t *testing.T) {
	tests := []struct{ in, prefix, rest string }{
		{"├─ Duration: 19ms", "├─ ", "Duration: 19ms"},
		{"│  └─ command: git branch", "│  └─ ", "command: git branch"},
		{"   Exit Code: 0", "   ", "Exit Code: 0"},
		{"Bash(command=x)", "", "Bash(command=x)"},
	}
	for _, tt := range tests {
		p, r := splitTreePrefix(tt.in)
		if p != tt.prefix || r != tt.rest {
			t.Errorf("splitTreePrefix(%q) = (%q,%q), want (%q,%q)", tt.in, p, r, tt.prefix, tt.rest)
		}
	}
}

func TestIsFieldLine(t *testing.T) {
	if !isFieldLine("├─ ") || !isFieldLine("│  └─ ") {
		t.Error("branch-glyph prefixes should be field lines")
	}
	if isFieldLine("│  ") || isFieldLine("   ") {
		t.Error("continuation/body prefixes should not be field lines")
	}
}

func TestSplitLabel(t *testing.T) {
	label, value, ok := splitLabel("Duration: 19ms")
	if !ok || label != "Duration:" || value != " 19ms" {
		t.Errorf("splitLabel = (%q,%q,%v)", label, value, ok)
	}
	if _, _, ok := splitLabel("no colon here"); ok {
		t.Error("expected no label for line without colon")
	}
}

func TestFormatDurationShort(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{19 * time.Millisecond, "19ms"},
		{1500 * time.Millisecond, "1.5s"},
		{90 * time.Second, "1m30s"},
	}
	for _, tt := range tests {
		if got := formatDurationShort(tt.d); got != tt.want {
			t.Errorf("formatDurationShort(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestCapToolResult(t *testing.T) {
	if got := capToolResult("hello", 0); got != "hello" {
		t.Errorf("cap 0 should be unlimited, got %q", got)
	}
	if got := capToolResult("hello", 100); got != "hello" {
		t.Errorf("content within cap should be unchanged, got %q", got)
	}

	big := strings.Repeat("a", 500) + "TAIL_MARKER"
	got := capToolResult(big, 200)
	if len(got) > 200 {
		t.Errorf("capped output %d exceeds cap 200", len(got))
	}
	if !strings.HasPrefix(got, "aaaa") {
		t.Error("head should be preserved")
	}
	if !strings.HasSuffix(got, "TAIL_MARKER") {
		t.Error("tail should be preserved")
	}
	if !strings.Contains(got, "truncated") {
		t.Error("truncation marker should be present")
	}
}
