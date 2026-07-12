package components

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"

	sdk "github.com/inference-gateway/sdk"

	domain "github.com/inference-gateway/cli/internal/domain"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"
	uimocks "github.com/inference-gateway/cli/tests/mocks/ui"
)

// argsAwareToolFormatter is a domain.ToolFormatter whose FormatToolCall renders
// the file_path argument, so the approval-box summary tests can assert that the
// pending call's arguments reach the box (the package's other stubToolFormatter
// ignores args, which would defeat these assertions).
type argsAwareToolFormatter struct{}

func (argsAwareToolFormatter) FormatToolCall(toolName string, args map[string]any) string {
	if p, ok := args["file_path"]; ok {
		return fmt.Sprintf("%s(file_path=%v)", toolName, p)
	}
	return fmt.Sprintf("%s()", toolName)
}
func (argsAwareToolFormatter) FormatToolResultForUI(*domain.ToolExecutionResult, int) string {
	return ""
}
func (argsAwareToolFormatter) FormatToolResultExpanded(*domain.ToolExecutionResult, int) string {
	return ""
}
func (argsAwareToolFormatter) FormatToolResultForLLM(*domain.ToolExecutionResult) string { return "" }
func (argsAwareToolFormatter) ShouldAlwaysExpandTool(string) bool                        { return false }
func (argsAwareToolFormatter) RenderToolSummary(icon, toolName string, args map[string]any, trailing string, _ int) string {
	if p, ok := args["file_path"]; ok {
		return fmt.Sprintf("%s %s(file_path=%v) %s", icon, toolName, p, trailing)
	}
	return fmt.Sprintf("%s %s() %s", icon, toolName, trailing)
}

func approvalStateWith(toolName, arguments string) *domain.ApprovalUIState {
	return &domain.ApprovalUIState{
		PendingToolCall: &sdk.ChatCompletionMessageToolCall{
			ID: "call_1",
			Function: sdk.ChatCompletionMessageToolCallFunction{
				Name:      toolName,
				Arguments: arguments,
			},
		},
	}
}

// approvalStateManager returns a real ApplicationState primed with the given
// pending approval (or none when s is nil).
func approvalStateManager(s *domain.ApprovalUIState) *domain.ApplicationState {
	st := domain.NewApplicationState()
	if s != nil {
		st.SetupApprovalUIState(s.PendingToolCall, nil)
	}
	return st
}

// TestApprovalHuhTheme_SelectedOptionIsButton asserts the focused option is styled
// as a solid button (accent background, bold) rather than bare accent text.
func TestApprovalHuhTheme_SelectedOptionIsButton(t *testing.T) {
	const accent = "#5f5fff"
	fakeTheme := &uimocks.FakeTheme{}
	fakeTheme.GetAccentColorReturns(accent)
	fakeThemeService := &domainmocks.FakeThemeService{}
	fakeThemeService.GetCurrentThemeReturns(fakeTheme)
	p := styles.NewProvider(fakeThemeService)

	s := approvalHuhTheme(p).Theme(true)

	if got := s.Focused.SelectedOption.GetBackground(); got != lipgloss.Color(accent) {
		t.Errorf("selected option should have the accent background %q, got %v", accent, got)
	}
	if !s.Focused.SelectedOption.GetBold() {
		t.Error("selected option button should be bold")
	}
}

func TestApprovalBox_EmptyWhenNoPendingCall(t *testing.T) {
	sm := approvalStateManager(nil)

	av := NewApprovalBoxView(createMockStyleProvider(), sm, argsAwareToolFormatter{})
	if got := av.Render(); got != "" {
		t.Errorf("expected empty render when no pending call, got %q", got)
	}
}

func TestApprovalBox_FramesSummaryAndButtons(t *testing.T) {
	sm := approvalStateManager(approvalStateWith("Read", `{"file_path":"/x/y.txt"}`))

	av := NewApprovalBoxView(createMockStyleProvider(), sm, argsAwareToolFormatter{})
	av.SetWidth(80)
	_ = av.Begin()
	out := av.Render()

	// The inline select shows only the focused option (← Approve →).
	for _, want := range []string{
		"Approval required",
		"Read(file_path=/x/y.txt)",
		"Approve",
		"╭", "╰",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("approval box render missing %q\n---\n%s", want, out)
		}
	}
}

func TestApprovalBox_SelectEmitsResponseEvent(t *testing.T) {
	sm := approvalStateManager(approvalStateWith("Read", `{"file_path":"/x/y.txt"}`))

	av := NewApprovalBoxView(createMockStyleProvider(), sm, argsAwareToolFormatter{})
	av.SetWidth(80)
	_ = av.Begin()

	_ = av.Forward(tea.KeyPressMsg{Code: tea.KeyRight})
	cmd := av.Forward(tea.KeyPressMsg{Code: tea.KeyEnter})
	for cmd != nil {
		msg := cmd()
		if ev, ok := msg.(domain.ToolApprovalResponseEvent); ok {
			if ev.Action != domain.ApprovalReject {
				t.Errorf("expected Reject after one right arrow, got %v", ev.Action)
			}
			if ev.ToolCall.ID != "call_1" {
				t.Errorf("expected the pending tool call echoed, got %+v", ev.ToolCall)
			}
			return
		}
		cmd = av.Forward(msg)
	}
	t.Fatal("expected a ToolApprovalResponseEvent after enter")
}

func TestApprovalBox_NilFormatterFallsBackToName(t *testing.T) {
	sm := approvalStateManager(approvalStateWith("Read", `{"file_path":"/x/y.txt"}`))

	av := NewApprovalBoxView(createMockStyleProvider(), sm, nil)
	av.SetWidth(80)
	_ = av.Begin()
	out := av.Render()

	if !strings.Contains(out, "Read(...)") {
		t.Errorf("expected nil-formatter fallback to render \"Read(...)\", got:\n%s", out)
	}
}

func TestApprovalBox_UnparseableArgsFallBack(t *testing.T) {
	sm := approvalStateManager(approvalStateWith("Bash", `not json`))

	av := NewApprovalBoxView(createMockStyleProvider(), sm, argsAwareToolFormatter{})
	av.SetWidth(80)
	_ = av.Begin()
	out := av.Render()

	if !strings.Contains(out, "Bash(...)") {
		t.Errorf("expected unparseable-args fallback to render \"Bash(...)\", got:\n%s", out)
	}
}

func TestApprovalBox_TruncatesLongSummaryOnNarrowWidth(t *testing.T) {
	longPath := "/very/long/path/to/some/deeply/nested/file/name/that/keeps/going.txt"
	sm := approvalStateManager(approvalStateWith("Read", fmt.Sprintf(`{"file_path":%q}`, longPath)))

	av := NewApprovalBoxView(createMockStyleProvider(), sm, argsAwareToolFormatter{})
	av.SetWidth(34)
	_ = av.Begin()
	out := av.Render()

	if strings.Contains(out, longPath) {
		t.Errorf("expected long summary to be truncated on a narrow box, but full path is present:\n%s", out)
	}
	if !strings.Contains(out, "...") {
		t.Errorf("expected truncation ellipsis in narrow box, got:\n%s", out)
	}
}

// TestApprovalBox_RendersEditDiff asserts the file-mutating tools show a colored
// diff of the change (file path + old/new content) instead of the one-liner, so the
// user can see what they are approving before approving.
func TestApprovalBox_RendersEditDiff(t *testing.T) {
	args := `{"file_path":"/x/y.txt","old_string":"OLD_CONTENT","new_string":"NEW_CONTENT"}`
	sm := approvalStateManager(approvalStateWith("Edit", args))

	av := NewApprovalBoxView(createMockStyleProvider(), sm, argsAwareToolFormatter{})
	av.SetWidth(80)
	_ = av.Begin()
	out := stripANSI(av.Render())

	if !strings.Contains(out, "/x/y.txt") {
		t.Errorf("expected the diff preview to show the file path, got:\n%s", out)
	}
	if !strings.Contains(out, "NEW_CONTENT") {
		t.Errorf("expected the diff preview to show the new content, got:\n%s", out)
	}
	if strings.Contains(out, "Edit(") {
		t.Errorf("expected a diff preview, not the one-liner summary, got:\n%s", out)
	}
}

// TestApprovalBox_CapsLongDiffWithHint asserts a large edit is height-capped (so it
// cannot push the conversation/input off-screen) and that the omitted tail is
// summarised with a "more lines" hint.
func TestApprovalBox_CapsLongDiffWithHint(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&b, "LINE_%02d\n", i)
	}
	args := fmt.Sprintf(`{"file_path":"/x/y.txt","old_string":"","new_string":%q}`, b.String())

	sm := approvalStateManager(approvalStateWith("Edit", args))

	av := NewApprovalBoxView(createMockStyleProvider(), sm, argsAwareToolFormatter{})
	av.SetWidth(80)
	av.SetHeight(24) // previewLineLimit -> 12

	_ = av.Begin()
	out := stripANSI(av.Render())

	if !strings.Contains(out, "more lines") {
		t.Errorf("expected a truncation hint for a long diff, got:\n%s", out)
	}
	if strings.Contains(out, "LINE_79") {
		t.Errorf("expected the tail of a long diff to be capped away, but LINE_79 is present:\n%s", out)
	}
}

// TestApprovalBox_ExpandScrollsToDiffTail asserts ctrl+o (ToggleExpanded) opens a
// scrollable window whose tail is reachable with ScrollDiff, so a diff taller than
// the screen is fully reviewable; collapsing resets it.
func TestApprovalBox_ExpandScrollsToDiffTail(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&b, "LINE_%02d\n", i)
	}
	args := fmt.Sprintf(`{"file_path":"/x/y.txt","old_string":"","new_string":%q}`, b.String())

	sm := approvalStateManager(approvalStateWith("Edit", args))

	av := NewApprovalBoxView(createMockStyleProvider(), sm, argsAwareToolFormatter{})
	av.SetWidth(80)
	av.SetHeight(30)

	_ = av.Begin()

	av.ToggleExpanded()
	top := stripANSI(av.Render())
	if !strings.Contains(top, "LINE_00") {
		t.Fatalf("expanded window should start at the diff head:\n%s", top)
	}
	if strings.Contains(top, "LINE_39") {
		t.Fatalf("diff tail should be below the fold before scrolling:\n%s", top)
	}
	if !strings.Contains(top, "scroll") {
		t.Errorf("expanded window should show a scroll hint:\n%s", top)
	}

	av.ScrollDiff(1000)
	bottom := stripANSI(av.Render())
	if !strings.Contains(bottom, "LINE_39") {
		t.Errorf("scrolling down should reveal the diff tail:\n%s", bottom)
	}

	av.ToggleExpanded()
	if recollapsed := stripANSI(av.Render()); strings.Contains(recollapsed, "LINE_39") {
		t.Errorf("collapsing should return to the capped head, LINE_39 still present:\n%s", recollapsed)
	}
}

// TestApprovalBox_IsActive reports true only while a form is built for a pending
// approval, so ctrl+o routes here instead of the conversation.
func TestApprovalBox_IsActive(t *testing.T) {
	sm := approvalStateManager(approvalStateWith("Edit", `{"file_path":"/x/y.txt","old_string":"OLD","new_string":"NEW"}`))
	av := NewApprovalBoxView(createMockStyleProvider(), sm, argsAwareToolFormatter{})
	if av.IsActive() {
		t.Error("IsActive should be false before Begin")
	}
	_ = av.Begin()
	if !av.IsActive() {
		t.Error("IsActive should be true after Begin with a pending approval")
	}
}

// TestApprovalBox_IsActiveFalseAfterExternalClear guards the esc-rejection bug: esc
// clears the StateManager's approval state without completing the form, so av.active
// /av.form linger. IsActive must consult the live state and report false, or ctrl+o
// would be swallowed by the defunct box instead of expanding the rejected result.
func TestApprovalBox_IsActiveFalseAfterExternalClear(t *testing.T) {
	sm := approvalStateManager(approvalStateWith("Write", `{"file_path":"/x/y.txt","content":"hi"}`))
	av := NewApprovalBoxView(createMockStyleProvider(), sm, argsAwareToolFormatter{})
	_ = av.Begin()
	if !av.IsActive() {
		t.Fatal("precondition: IsActive true while the approval is pending")
	}

	sm.ClearApprovalUIState()

	if av.IsActive() {
		t.Error("IsActive must be false once the approval state is cleared externally")
	}
}

// TestApprovalBox_DiffToolIgnoresFormatter asserts the diff path does not depend on
// the tool formatter (a nil formatter still yields a diff, not the name fallback).
func TestApprovalBox_DiffToolIgnoresFormatter(t *testing.T) {
	args := `{"file_path":"/x/y.txt","old_string":"OLD","new_string":"NEW"}`
	sm := approvalStateManager(approvalStateWith("Edit", args))

	av := NewApprovalBoxView(createMockStyleProvider(), sm, nil)
	av.SetWidth(80)
	_ = av.Begin()
	out := stripANSI(av.Render())

	if strings.Contains(out, "Edit(") {
		t.Errorf("expected a diff even with a nil formatter, got summary:\n%s", out)
	}
	if !strings.Contains(out, "/x/y.txt") {
		t.Errorf("expected the diff preview to render the file path, got:\n%s", out)
	}
}

// contextSnippet is a 9-line block with a single changed middle line, so a diff
// renders one hunk whose context width (2 vs 3 lines) is observable: at 2 lines
// only charlie/delta and foxtrot/golf survive; at 3 lines bravo/hotel show too.
const (
	oldContextSnippet = "alpha\nbravo\ncharlie\ndelta\necho_OLD\nfoxtrot\ngolf\nhotel\nindia"
	newContextSnippet = "alpha\nbravo\ncharlie\ndelta\necho_NEW\nfoxtrot\ngolf\nhotel\nindia"
)

// TestApprovalBox_EditDiffUsesTwoContextLines asserts the snippet-fallback path
// (the target file does not exist, so old_string/new_string are diffed directly)
// is tightened to 2 context lines: the 2 nearest unchanged lines on each side
// remain while the 3rd-out lines (and beyond) are trimmed.
func TestApprovalBox_EditDiffUsesTwoContextLines(t *testing.T) {
	args := fmt.Sprintf(`{"file_path":"/x/y.txt","old_string":%q,"new_string":%q}`, oldContextSnippet, newContextSnippet)

	sm := approvalStateManager(approvalStateWith("Edit", args))

	av := NewApprovalBoxView(createMockStyleProvider(), sm, argsAwareToolFormatter{})
	av.SetWidth(120)
	av.SetHeight(60)
	_ = av.Begin()
	out := stripANSI(av.Render())

	for _, want := range []string{"charlie", "delta", "foxtrot", "golf", "echo_NEW"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected the Edit approval diff to keep 2 context lines incl %q:\n%s", want, out)
		}
	}
	for _, absent := range []string{"bravo", "hotel", "alpha", "india"} {
		if strings.Contains(out, absent) {
			t.Fatalf("expected the Edit approval diff to trim context beyond 2 lines, but %q is present:\n%s", absent, out)
		}
	}
}

// TestApprovalBox_MultiEditKeepsThreeContext locks in that only Edit was tightened:
// the MultiEdit approval preview still renders the default 3 context lines (so the
// 3rd-out lines bravo/hotel remain visible).
func TestApprovalBox_MultiEditKeepsThreeContext(t *testing.T) {
	args := fmt.Sprintf(`{"file_path":"/x/y.txt","edits":[{"old_string":%q,"new_string":%q}]}`, oldContextSnippet, newContextSnippet)

	sm := approvalStateManager(approvalStateWith("MultiEdit", args))

	av := NewApprovalBoxView(createMockStyleProvider(), sm, argsAwareToolFormatter{})
	av.SetWidth(120)
	av.SetHeight(60)
	_ = av.Begin()
	out := stripANSI(av.Render())

	for _, want := range []string{"bravo", "hotel", "echo_NEW"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected the MultiEdit approval diff to keep the default 3 context lines incl %q:\n%s", want, out)
		}
	}
}

// TestApprovalBox_EditDiffShowsFileContext asserts that for a real file, the Edit
// approval preview diffs against the actual file content so the 2 context lines
// are real neighbouring file lines around the change (not just the old_string),
// and that context beyond 2 lines is trimmed.
func TestApprovalBox_EditDiffShowsFileContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.txt")
	content := "line1\nline2\nline3\nTARGET\nline5\nline6\nline7\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	args := fmt.Sprintf(`{"file_path":%q,"old_string":"TARGET","new_string":"CHANGED"}`, path)

	sm := approvalStateManager(approvalStateWith("Edit", args))

	av := NewApprovalBoxView(createMockStyleProvider(), sm, argsAwareToolFormatter{})
	av.SetWidth(120)
	av.SetHeight(60)
	_ = av.Begin()
	out := stripANSI(av.Render())

	for _, want := range []string{"line2", "line3", "CHANGED", "line5", "line6"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected the Edit approval diff to show 2 lines of file context incl %q:\n%s", want, out)
		}
	}
	for _, absent := range []string{"line1", "line7"} {
		if strings.Contains(out, absent) {
			t.Fatalf("expected file context beyond 2 lines to be trimmed, but %q is present:\n%s", absent, out)
		}
	}
}
