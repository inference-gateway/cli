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
	sm.GetApprovalUIStateReturns(approvalStateWith("Write", `{"file_path":"/x/y.txt"}`, domain.ApprovalApprove))

	av := NewApprovalBoxView(createMockStyleProvider(), sm, argsAwareToolFormatter{})
	av.SetWidth(80)
	out := av.Render()

	for _, want := range []string{
		"Approval required",                 // title
		"Write(file_path=/x/y.txt)",         // pending call summary
		"Approve", "Reject", "Auto-Approve", // action buttons
		"╭", "╰", // rounded border (framed box)
	} {
		if !strings.Contains(out, want) {
			t.Errorf("approval box render missing %q\n---\n%s", want, out)
		}
	}
}

func TestApprovalBox_NilFormatterFallsBackToName(t *testing.T) {
	sm := &domainmocks.FakeStateManager{}
	sm.GetApprovalUIStateReturns(approvalStateWith("Write", `{"file_path":"/x/y.txt"}`, domain.ApprovalApprove))

	av := NewApprovalBoxView(createMockStyleProvider(), sm, nil)
	av.SetWidth(80)
	out := av.Render()

	if !strings.Contains(out, "Write(...)") {
		t.Errorf("expected nil-formatter fallback to render \"Write(...)\", got:\n%s", out)
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
	sm.GetApprovalUIStateReturns(approvalStateWith("Write", fmt.Sprintf(`{"file_path":%q}`, longPath), domain.ApprovalApprove))

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
