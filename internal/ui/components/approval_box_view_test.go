package components

import (
	"fmt"
	"strings"
	"testing"

	sdk "github.com/inference-gateway/sdk"

	domain "github.com/inference-gateway/cli/internal/domain"
	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"
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

func approvalStateWith(toolName, arguments string, selected domain.ApprovalAction) *domain.ApprovalUIState {
	return &domain.ApprovalUIState{
		SelectedIndex: int(selected),
		PendingToolCall: &sdk.ChatCompletionMessageToolCall{
			ID: "call_1",
			Function: sdk.ChatCompletionMessageToolCallFunction{
				Name:      toolName,
				Arguments: arguments,
			},
		},
	}
}

func TestApprovalBox_EmptyWhenNoPendingCall(t *testing.T) {
	sm := &domainmocks.FakeStateManager{}
	sm.GetApprovalUIStateReturns(nil)

	av := NewApprovalBoxView(createMockStyleProvider(), sm, argsAwareToolFormatter{})
	if got := av.Render(); got != "" {
		t.Errorf("expected empty render when no pending call, got %q", got)
	}
}

func TestApprovalBox_FramesSummaryAndButtons(t *testing.T) {
	sm := &domainmocks.FakeStateManager{}
	sm.GetApprovalUIStateReturns(approvalStateWith("Read", `{"file_path":"/x/y.txt"}`, domain.ApprovalApprove))

	av := NewApprovalBoxView(createMockStyleProvider(), sm, argsAwareToolFormatter{})
	av.SetWidth(80)
	out := av.Render()

	for _, want := range []string{
		"Approval required",
		"Read(file_path=/x/y.txt)",
		"Approve", "Reject", "Auto-Approve",
		"╭", "╰",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("approval box render missing %q\n---\n%s", want, out)
		}
	}
}

func TestApprovalBox_NilFormatterFallsBackToName(t *testing.T) {
	sm := &domainmocks.FakeStateManager{}
	sm.GetApprovalUIStateReturns(approvalStateWith("Read", `{"file_path":"/x/y.txt"}`, domain.ApprovalApprove))

	av := NewApprovalBoxView(createMockStyleProvider(), sm, nil)
	av.SetWidth(80)
	out := av.Render()

	if !strings.Contains(out, "Read(...)") {
		t.Errorf("expected nil-formatter fallback to render \"Read(...)\", got:\n%s", out)
	}
}

func TestApprovalBox_UnparseableArgsFallBack(t *testing.T) {
	sm := &domainmocks.FakeStateManager{}
	sm.GetApprovalUIStateReturns(approvalStateWith("Bash", `not json`, domain.ApprovalApprove))

	av := NewApprovalBoxView(createMockStyleProvider(), sm, argsAwareToolFormatter{})
	av.SetWidth(80)
	out := av.Render()

	if !strings.Contains(out, "Bash(...)") {
		t.Errorf("expected unparseable-args fallback to render \"Bash(...)\", got:\n%s", out)
	}
}

func TestApprovalBox_TruncatesLongSummaryOnNarrowWidth(t *testing.T) {
	longPath := "/very/long/path/to/some/deeply/nested/file/name/that/keeps/going.txt"
	sm := &domainmocks.FakeStateManager{}
	sm.GetApprovalUIStateReturns(approvalStateWith("Read", fmt.Sprintf(`{"file_path":%q}`, longPath), domain.ApprovalApprove))

	av := NewApprovalBoxView(createMockStyleProvider(), sm, argsAwareToolFormatter{})
	av.SetWidth(34)
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
	sm := &domainmocks.FakeStateManager{}
	args := `{"file_path":"/x/y.txt","old_string":"OLD_CONTENT","new_string":"NEW_CONTENT"}`
	sm.GetApprovalUIStateReturns(approvalStateWith("Edit", args, domain.ApprovalApprove))

	av := NewApprovalBoxView(createMockStyleProvider(), sm, argsAwareToolFormatter{})
	av.SetWidth(80)
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

	sm := &domainmocks.FakeStateManager{}
	sm.GetApprovalUIStateReturns(approvalStateWith("Edit", args, domain.ApprovalApprove))

	av := NewApprovalBoxView(createMockStyleProvider(), sm, argsAwareToolFormatter{})
	av.SetWidth(80)
	av.SetHeight(24) // previewLineLimit -> 12

	out := stripANSI(av.Render())

	if !strings.Contains(out, "more lines") {
		t.Errorf("expected a truncation hint for a long diff, got:\n%s", out)
	}
	if strings.Contains(out, "LINE_79") {
		t.Errorf("expected the tail of a long diff to be capped away, but LINE_79 is present:\n%s", out)
	}
}

// TestApprovalBox_DiffToolIgnoresFormatter asserts the diff path does not depend on
// the tool formatter (a nil formatter still yields a diff, not the name fallback).
func TestApprovalBox_DiffToolIgnoresFormatter(t *testing.T) {
	sm := &domainmocks.FakeStateManager{}
	args := `{"file_path":"/x/y.txt","old_string":"OLD","new_string":"NEW"}`
	sm.GetApprovalUIStateReturns(approvalStateWith("Edit", args, domain.ApprovalApprove))

	av := NewApprovalBoxView(createMockStyleProvider(), sm, nil)
	av.SetWidth(80)
	out := stripANSI(av.Render())

	if strings.Contains(out, "Edit(") {
		t.Errorf("expected a diff even with a nil formatter, got summary:\n%s", out)
	}
	if !strings.Contains(out, "/x/y.txt") {
		t.Errorf("expected the diff preview to render the file path, got:\n%s", out)
	}
}
