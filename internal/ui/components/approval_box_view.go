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

type ApprovalBoxView struct {
	width         int
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
	dimColor := av.styleProvider.GetThemeColor("dim")

	title := av.styleProvider.RenderWithColorAndBold("Approval required", accentColor)

	summary := formatting.TruncateText(av.toolCallSummary(state.PendingToolCall), av.summaryBudget())
	summaryStyled := av.styleProvider.RenderWithColor(summary, dimColor)

	buttons := av.renderApprovalButtons(state.SelectedIndex)

	content := strings.Join([]string{title, summaryStyled, buttons}, "\n")
	return av.styleProvider.RenderBorderedBox(content, accentColor, 0, 1)
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
