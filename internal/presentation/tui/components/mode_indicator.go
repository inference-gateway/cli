package components

import (
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	styles "github.com/inference-gateway/cli/internal/presentation/tui/styles"
)

// ModeIndicator names the current agent mode (PLAN/AUTO/JUDGE) as a label the
// application view lays out on the right of the status row.
type ModeIndicator struct {
	stateManager  agentdomain.AgentModeState
	styleProvider *styles.Provider
	judgeModel    func() string
}

// NewModeIndicator creates a new mode indicator
func NewModeIndicator(styleProvider *styles.Provider) *ModeIndicator {
	return &ModeIndicator{
		styleProvider: styleProvider,
	}
}

// SetJudgeModelFn names the model that approves tool calls in Auto+Judge mode
// so the indicator shows who the judge is; resolved at render time so it
// follows a model switched during the session.
func (mi *ModeIndicator) SetJudgeModelFn(fn func() string) {
	mi.judgeModel = fn
}

// SetStateManager sets the state manager
func (mi *ModeIndicator) SetStateManager(stateManager agentdomain.AgentModeState) {
	mi.stateManager = stateManager
}

// Text returns the styled mode label, empty while the default mode is active.
func (mi *ModeIndicator) Text() string {
	if mi.stateManager == nil {
		return ""
	}

	label := mi.modeLabel(mi.stateManager.GetAgentMode())
	if label == "" {
		return ""
	}

	return mi.styleProvider.RenderStyledText(
		label,
		styles.StyleOptions{
			Foreground: mi.styleProvider.GetThemeColor("accent"),
			Bold:       true,
		},
	)
}

// modeLabel names the mode, appending the judge model for Auto+Judge. The
// default (standard) mode has no indicator, so it maps to an empty label.
func (mi *ModeIndicator) modeLabel(mode agentdomain.AgentMode) string {
	switch mode {
	case agentdomain.AgentModePlan:
		return "▶ PLAN"
	case agentdomain.AgentModeAutoAccept:
		return "▸ AUTO"
	case agentdomain.AgentModeAutoWithJudge:
		label := "▸ AUTO+JUDGE"
		if mi.judgeModel != nil {
			if model := mi.judgeModel(); model != "" {
				label += " · " + model
			}
		}
		return label
	case agentdomain.AgentModeReadOnly:
		return "▸ READ-ONLY"
	}
	return ""
}
