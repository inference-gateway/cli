package components

import (
	"encoding/json"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	huh "charm.land/huh/v2"

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

	// expandedReservedLines is the vertical space kept for the header, input,
	// status bar, and box chrome when the diff is expanded (ctrl+o), so even a
	// full-file diff leaves the input on-screen.
	expandedReservedLines = 12

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
	stateManager  domain.ApprovalUIManager
	toolFormatter domain.ToolFormatter

	// active is the approval state the form was built for; a mismatch with
	// the StateManager (cleared externally) marks the form stale.
	active *domain.ApprovalUIState
	form   *huh.Form
	choice domain.ApprovalAction

	// expanded switches the diff from the height-capped preview to a scrollable
	// window over the full diff (ctrl+o), mirroring the conversation view's
	// tool-result expansion. scrollOffset is the first visible diff line in that
	// window. Both reset for each new approval.
	expanded     bool
	scrollOffset int
}

// ToggleExpanded flips between the capped diff preview and the scrollable full-diff
// window, matching the ctrl+o tool-result expansion in the conversation view.
func (av *ApprovalBoxView) ToggleExpanded() {
	av.expanded = !av.expanded
	av.scrollOffset = 0
}

// ScrollDiff moves the expanded diff window by delta lines (up/down). It is a no-op
// unless the diff is expanded; the top is clamped here and the bottom at render time
// (which is where the window height is known).
func (av *ApprovalBoxView) ScrollDiff(delta int) {
	if !av.expanded {
		return
	}
	av.scrollOffset += delta
	if av.scrollOffset < 0 {
		av.scrollOffset = 0
	}
}

// IsActive reports whether an approval is currently being shown, so the caller can
// route ctrl+o to this box instead of the conversation.
func (av *ApprovalBoxView) IsActive() bool {
	return av.active != nil && av.form != nil
}

// IsExpanded reports whether the diff is in the scrollable expanded view, so the
// caller can route up/down to scroll it instead of the conversation.
func (av *ApprovalBoxView) IsExpanded() bool {
	return av.expanded
}

func NewApprovalBoxView(styleProvider *styles.Provider, stateManager domain.ApprovalUIManager, toolFormatter domain.ToolFormatter) *ApprovalBoxView {
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
	if approvalState == nil || approvalState.PendingToolCall == nil ||
		approvalState != av.active || av.form == nil {
		return ""
	}

	return av.renderApprovalBox(approvalState)
}

// Begin builds the action select for the approval currently in the
// StateManager. Call it when a ToolApprovalRequestedEvent has set up the state.
func (av *ApprovalBoxView) Begin() tea.Cmd {
	state := av.stateManager.GetApprovalUIState()
	if state == nil || state.PendingToolCall == nil {
		return nil
	}
	av.active = state
	av.choice = domain.ApprovalApprove
	av.expanded = false
	av.scrollOffset = 0
	av.form = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[domain.ApprovalAction]().
				Options(
					huh.NewOption("Approve", domain.ApprovalApprove),
					huh.NewOption("Reject", domain.ApprovalReject),
					huh.NewOption("Auto-Approve", domain.ApprovalAutoAccept),
				).
				Inline(true).
				Value(&av.choice),
		),
	).
		WithShowHelp(false).
		WithWidth(av.summaryBudget()).
		WithTheme(huhTheme(av.styleProvider))
	return av.form.Init()
}

// Forward delegates a message to the action select. On completion it emits the
// ToolApprovalResponseEvent that the approval coordinator consumes.
func (av *ApprovalBoxView) Forward(msg tea.Msg) tea.Cmd {
	state := av.stateManager.GetApprovalUIState()
	if state == nil || state != av.active || av.form == nil {
		av.active = nil
		av.form = nil
		return nil
	}

	model, cmd := av.form.Update(msg)
	if f, ok := model.(*huh.Form); ok {
		av.form = f
	}

	if av.form.State == huh.StateCompleted {
		action := av.choice
		toolCall := *state.PendingToolCall
		av.active = nil
		av.form = nil
		return func() tea.Msg {
			return domain.ToolApprovalResponseEvent{Action: action, ToolCall: toolCall}
		}
	}
	return cmd
}

// renderApprovalBox frames the pending tool call and the action buttons in a
// bordered box so the approval prompt is unmistakable and shows *what* is being
// approved, instead of bare buttons floating above the input. The border uses the
// accent colour to echo the focused input box directly below it.
func (av *ApprovalBoxView) renderApprovalBox(state *domain.ApprovalUIState) string {
	accentColor := av.styleProvider.GetThemeColor("accent")

	title := av.styleProvider.RenderWithColorAndBold("Approval required", accentColor)
	body := av.renderBody(state.PendingToolCall)

	content := strings.Join([]string{title, body, av.form.View()}, "\n")
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

// capLines bounds the preview height so a large edit can't blow out the layout.
// Collapsed it keeps the first previewLineLimit() lines with a "… N more lines"
// hint; expanded (ctrl+o) it shows a scrollable window over the whole diff so a
// file larger than the screen stays fully reviewable while the action buttons stay
// pinned. The full diff still renders in the conversation after the edit runs.
func (av *ApprovalBoxView) capLines(s string) string {
	body := strings.TrimRight(s, "\n")
	lines := strings.Split(body, "\n")
	limit := av.previewLineLimit()
	if len(lines) <= limit {
		av.scrollOffset = 0
		return body
	}

	if !av.expanded {
		hidden := len(lines) - limit
		hint := av.styleProvider.RenderDimText(
			fmt.Sprintf("… %d more lines (ctrl+o to expand, full diff shown after approval)", hidden),
		)
		return strings.Join(lines[:limit], "\n") + "\n" + hint
	}

	// Clamp the bottom here (the window height is only known now); the top is
	// clamped on scroll.
	maxOffset := len(lines) - limit
	if av.scrollOffset > maxOffset {
		av.scrollOffset = maxOffset
	}
	window := strings.Join(lines[av.scrollOffset:av.scrollOffset+limit], "\n")
	hint := av.styleProvider.RenderDimText(
		fmt.Sprintf("↑/↓ scroll · lines %d-%d of %d (ctrl+o to collapse)",
			av.scrollOffset+1, av.scrollOffset+limit, len(lines)),
	)
	return window + "\n" + hint
}

// previewLineLimit is the number of diff lines shown in the box at once. Collapsed
// it is about half the terminal height so the conversation and input keep room,
// bounded to [minPreviewLines, maxPreviewLines]; expanded (ctrl+o) it grows to fill
// the screen (minus the header, input, and box chrome so the layout can't overflow)
// and the rest is reached by scrolling. It falls back to defaultPreviewLines before
// the terminal height is known (height <= 0).
func (av *ApprovalBoxView) previewLineLimit() int {
	if av.height <= 0 {
		return defaultPreviewLines
	}
	if av.expanded {
		limit := av.height - expandedReservedLines
		if limit < minPreviewLines {
			return minPreviewLines
		}
		return limit
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
