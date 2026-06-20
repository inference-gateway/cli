package components

import (
	"encoding/json"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	sdk "github.com/inference-gateway/sdk"

	domain "github.com/inference-gateway/cli/internal/domain"
	formatting "github.com/inference-gateway/cli/internal/formatting"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

// minApprovalSummaryWidth is the floor for the truncated tool-call summary so a
// very narrow terminal still shows a usable amount of the pending call.
const minApprovalSummaryWidth = 20

// Diff-preview height bounds for the approval box. The preview is capped so a large
// edit can't push the conversation/input off-screen (the box height is measured by
// line count and subtracted from the layout). The cap tracks terminal height (~half)
// within [minPreviewLines, maxPreviewLines], falling back to defaultPreviewLines
// before the terminal height is known.
const (
	minPreviewLines     = 6
	maxPreviewLines     = 30
	defaultPreviewLines = 16

	// diffBorderPadding is the box border (2) plus horizontal padding (2) reserved
	// when sizing the diff to the available width; minDiffWidth keeps it usable on
	// very narrow terminals.
	diffBorderPadding = 4
	minDiffWidth      = 20
)

type ApprovalBoxView struct {
	width         int
	height        int
	styleProvider *styles.Provider
	stateManager  domain.StateManager
	toolFormatter domain.ToolFormatter
}

func NewApprovalBoxView(styleProvider *styles.Provider, stateManager domain.StateManager, toolFormatter domain.ToolFormatter) *ApprovalBoxView {
	return &ApprovalBoxView{
		width:         80,
		styleProvider: styleProvider,
		stateManager:  stateManager,
		toolFormatter: toolFormatter,
	}
}

func (av *ApprovalBoxView) SetWidth(width int) {
	av.width = width
}

func (av *ApprovalBoxView) SetHeight(height int) {
	av.height = height
}

func (av *ApprovalBoxView) Render() string {
	if av.stateManager == nil {
		return ""
	}

	approvalState := av.stateManager.GetApprovalUIState()
	if approvalState == nil || approvalState.PendingToolCall == nil {
		return ""
	}

	return av.renderApprovalBox(approvalState)
}

// renderApprovalBox frames the pending tool call and the action buttons in a
// bordered box so the approval prompt is unmistakable and shows *what* is being
// approved, instead of bare buttons floating above the input. The border uses the
// accent colour to echo the focused input box directly below it.
func (av *ApprovalBoxView) renderApprovalBox(state *domain.ApprovalUIState) string {
	accentColor := av.styleProvider.GetThemeColor("accent")

	title := av.styleProvider.RenderWithColorAndBold("Approval required", accentColor)
	body := av.renderBody(state.PendingToolCall)
	buttons := av.renderApprovalButtons(state.SelectedIndex)

	content := strings.Join([]string{title, body, buttons}, "\n")
	return av.styleProvider.RenderBorderedBox(content, accentColor, 0, 1)
}

// renderBody renders what is being approved. For the file-mutating tools
// (Edit/MultiEdit/Write) it shows a height-capped, theme-aware colored diff so the
// user sees the change before approving; every other tool keeps the compact
// "Name(arg=value, ...)" one-liner. It also falls back to the one-liner when the
// arguments don't parse.
func (av *ApprovalBoxView) renderBody(tc *sdk.ChatCompletionMessageToolCall) string {
	var args map[string]any
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err == nil {
		if preview, ok := av.renderDiffPreview(tc.Function.Name, args); ok {
			return preview
		}
	}
	return av.renderSummary(tc)
}

// renderSummary renders the dim "Name(arg=value, ...)" one-liner used for every
// non-diff tool, truncated to the available width.
func (av *ApprovalBoxView) renderSummary(tc *sdk.ChatCompletionMessageToolCall) string {
	dimColor := av.styleProvider.GetThemeColor("dim")
	summary := formatting.TruncateText(av.toolCallSummary(tc), av.summaryBudget())
	return av.styleProvider.RenderWithColor(summary, dimColor)
}

// renderDiffPreview renders the file diff for the mutating tools using the shared,
// theme-aware DiffRenderer (same package). The second return is false for any other
// tool so the caller falls back to the one-liner summary. The diff is sized to the
// box width and capped to a bounded number of lines (see capLines).
//
// The tool names are matched as literals on purpose: internal/agent/tools imports
// this package for the diff renderer, so importing it back for its name constants
// would create an import cycle.
func (av *ApprovalBoxView) renderDiffPreview(toolName string, args map[string]any) (string, bool) {
	renderer := NewDiffRenderer(av.styleProvider).SetWidth(av.diffWidth())

	var rendered string
	switch toolName {
	case "Edit":
		rendered = renderer.SetContextLines(InlineDiffContextLines).RenderEditToolArguments(args)
	case "MultiEdit":
		rendered = renderer.RenderMultiEditToolArguments(args)
	case "Write":
		rendered = renderer.RenderWriteToolArguments(args)
	default:
		return "", false
	}

	return av.capLines(rendered), true
}

// capLines bounds the preview height so a large edit can't blow out the layout: it
// keeps the first previewLineLimit() lines and replaces the rest with a dim
// "… N more lines" hint. The full diff still renders in the conversation after the
// edit runs.
func (av *ApprovalBoxView) capLines(s string) string {
	body := strings.TrimRight(s, "\n")
	lines := strings.Split(body, "\n")
	limit := av.previewLineLimit()
	if len(lines) <= limit {
		return body
	}

	hidden := len(lines) - limit
	hint := av.styleProvider.RenderDimText(
		fmt.Sprintf("… %d more lines (full diff shown after approval)", hidden),
	)
	return strings.Join(lines[:limit], "\n") + "\n" + hint
}

// previewLineLimit is the max diff lines shown in the box: about half the terminal
// height so the conversation and input keep room, bounded to
// [minPreviewLines, maxPreviewLines]. It falls back to defaultPreviewLines before
// the terminal height is known (height <= 0).
func (av *ApprovalBoxView) previewLineLimit() int {
	if av.height <= 0 {
		return defaultPreviewLines
	}
	limit := av.height / 2
	if limit < minPreviewLines {
		return minPreviewLines
	}
	if limit > maxPreviewLines {
		return maxPreviewLines
	}
	return limit
}

// diffWidth is the width available to the diff after the box border and padding.
func (av *ApprovalBoxView) diffWidth() int {
	w := av.width - diffBorderPadding
	if w < minDiffWidth {
		return minDiffWidth
	}
	return w
}

// toolCallSummary renders the pending call as "Name(arg=value, ...)" using the
// shared tool formatter so it matches the conversation view. It degrades to
// "Name(...)" when the formatter is unavailable or the arguments are unparseable.
func (av *ApprovalBoxView) toolCallSummary(tc *sdk.ChatCompletionMessageToolCall) string {
	name := tc.Function.Name
	if av.toolFormatter == nil {
		return fmt.Sprintf("%s(...)", name)
	}

	var args map[string]any
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		return fmt.Sprintf("%s(...)", name)
	}
	return av.toolFormatter.FormatToolCall(name, args)
}

// summaryBudget is the display width available for the tool-call summary after
// reserving room for the box border (2) and horizontal padding (2), plus a small
// slack so the text never touches the right border.
func (av *ApprovalBoxView) summaryBudget() int {
	budget := av.width - 6
	if budget < minApprovalSummaryWidth {
		return minApprovalSummaryWidth
	}
	return budget
}

func (av *ApprovalBoxView) renderApprovalButtons(selectedIndex int) string {
	approveText := "Approve"
	rejectText := "Reject"
	autoApproveText := "Auto-Approve"

	successColor := av.styleProvider.GetThemeColor("success")
	errorColor := av.styleProvider.GetThemeColor("error")
	accentColor := av.styleProvider.GetThemeColor("accent")
	highlightBg := av.styleProvider.GetThemeColor("selection_bg")

	var approveStyled, rejectStyled, autoApproveStyled string
	if selectedIndex == int(domain.ApprovalApprove) {
		approveStyled = av.styleProvider.RenderStyledText("[ "+approveText+" ]", styles.StyleOptions{
			Foreground: successColor,
			Background: highlightBg,
			Bold:       true,
		})
	} else {
		approveStyled = av.styleProvider.RenderWithColor("[ "+approveText+" ]", successColor)
	}

	if selectedIndex == int(domain.ApprovalReject) {
		rejectStyled = av.styleProvider.RenderStyledText("[ "+rejectText+" ]", styles.StyleOptions{
			Foreground: errorColor,
			Background: highlightBg,
			Bold:       true,
		})
	} else {
		rejectStyled = av.styleProvider.RenderWithColor("[ "+rejectText+" ]", errorColor)
	}

	if selectedIndex == int(domain.ApprovalAutoAccept) {
		autoApproveStyled = av.styleProvider.RenderStyledText("[ "+autoApproveText+" ]", styles.StyleOptions{
			Foreground: accentColor,
			Background: highlightBg,
			Bold:       true,
		})
	} else {
		autoApproveStyled = av.styleProvider.RenderWithColor("[ "+autoApproveText+" ]", accentColor)
	}

	return fmt.Sprintf("%s  %s  %s", approveStyled, rejectStyled, autoApproveStyled)
}

func (av *ApprovalBoxView) Init() tea.Cmd {
	return nil
}

func (av *ApprovalBoxView) View() tea.View {
	return tea.NewView("")
}

func (av *ApprovalBoxView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if windowMsg, ok := msg.(tea.WindowSizeMsg); ok {
		av.SetWidth(windowMsg.Width)
	}
	return av, nil
}
