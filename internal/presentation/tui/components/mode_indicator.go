package components

import (
	"strings"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	styles "github.com/inference-gateway/cli/internal/presentation/tui/styles"
)

// ModeIndicator displays the current agent mode (PLAN/AUTO) on its own line
type ModeIndicator struct {
	width         int
	stateManager  agentdomain.AgentModeManager
	styleProvider *styles.Provider
	judgeModel    string
}

// NewModeIndicator creates a new mode indicator
func NewModeIndicator(styleProvider *styles.Provider) *ModeIndicator {
	return &ModeIndicator{
		styleProvider: styleProvider,
	}
}

// SetWidth sets the width of the mode indicator
func (mi *ModeIndicator) SetWidth(width int) {
	mi.width = width
}

// SetJudgeModel names the model that approves tool calls in Auto+Judge mode
// so the indicator shows who the judge is.
func (mi *ModeIndicator) SetJudgeModel(model string) {
	mi.judgeModel = model
}

// SetStateManager sets the state manager
func (mi *ModeIndicator) SetStateManager(stateManager agentdomain.AgentModeManager) {
	mi.stateManager = stateManager
}

// Render renders the mode indicator line
func (mi *ModeIndicator) Render() string {
	if mi.stateManager == nil || mi.width == 0 {
		return ""
	}

	agentMode := mi.stateManager.GetAgentMode()
	if agentMode == agentdomain.AgentModeStandard {
		return ""
	}

	var modeText string
	switch agentMode {
	case agentdomain.AgentModePlan:
		modeText = "▶ PLAN"
	case agentdomain.AgentModeAutoAccept:
		modeText = "▸ AUTO"
	case agentdomain.AgentModeAutoWithJudge:
		modeText = "▸ AUTO+JUDGE"
		if mi.judgeModel != "" {
			modeText += " · " + mi.judgeModel
		}
	case agentdomain.AgentModeReadOnly:
		modeText = "▸ READ-ONLY"
	}

	styledMode := mi.styleProvider.RenderStyledText(
		modeText,
		styles.StyleOptions{
			Foreground: mi.styleProvider.GetThemeColor("accent"),
			Bold:       true,
		},
	)

	modeWidth := len([]rune(modeText))
	availableWidth := mi.width - 4
	spacingWidth := availableWidth - modeWidth

	if spacingWidth > 0 {
		return strings.Repeat(" ", spacingWidth) + styledMode
	}
	return styledMode
}
