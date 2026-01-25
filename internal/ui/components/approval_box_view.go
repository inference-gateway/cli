package components

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	domain "github.com/inference-gateway/cli/internal/domain"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

type ApprovalBoxView struct {
	width         int
	styleProvider *styles.Provider
	stateManager  domain.StateManager
}

func NewApprovalBoxView(styleProvider *styles.Provider, stateManager domain.StateManager) *ApprovalBoxView {
	return &ApprovalBoxView{
		width:         80,
		styleProvider: styleProvider,
		stateManager:  stateManager,
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

	return av.renderApprovalButtons(approvalState.SelectedIndex)
}

func (av *ApprovalBoxView) renderApprovalButtons(selectedIndex int) string {
	approveText := "Approve"
	rejectText := "Reject"
	autoApproveText := "Auto-Approve"

	successColor := av.styleProvider.GetThemeColor("success")
	errorColor := av.styleProvider.GetThemeColor("error")
	accentColor := av.styleProvider.GetThemeColor("accent")
	highlightBg := av.styleProvider.GetThemeColor("selection_bg")

	// Render buttons with highlighting for selected one
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

	return fmt.Sprintf("  %s  %s  %s", approveStyled, rejectStyled, autoApproveStyled)
}

func (av *ApprovalBoxView) Init() tea.Cmd {
	return nil
}

func (av *ApprovalBoxView) View() string {
	return ""
}

func (av *ApprovalBoxView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if windowMsg, ok := msg.(tea.WindowSizeMsg); ok {
		av.SetWidth(windowMsg.Width)
	}
	return av, nil
}
