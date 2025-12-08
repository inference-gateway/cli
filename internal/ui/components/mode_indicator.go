package components

import (
	"strings"

	domain "github.com/inference-gateway/cli/internal/domain"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

// ModeIndicator displays the current agent mode (PLAN/AUTO) on its own line
type ModeIndicator struct {
	width         int
	stateManager  domain.StateManager
	styleProvider *styles.Provider
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

// SetStateManager sets the state manager
func (mi *ModeIndicator) SetStateManager(stateManager domain.StateManager) {
	mi.stateManager = stateManager
}

// Render renders the mode indicator line
func (mi *ModeIndicator) Render() string {
	if mi.stateManager == nil || mi.width == 0 {
		return ""
	}

	agentMode := mi.stateManager.GetAgentMode()
	if agentMode == domain.AgentModeStandard {
		return ""
	}

	var modeText string
	switch agentMode {
	case domain.AgentModePlan:
		modeText = "▶ PLAN"
	case domain.AgentModeAutoAccept:
		modeText = "▸ AUTO"
	}

	styledMode := mi.styleProvider.RenderStyledText(
		modeText,
		styles.StyleOptions{
			Foreground: mi.styleProvider.GetThemeColor("accent"),
			Bold:       true,
		},
	)

	// Right-align the mode indicator
	modeWidth := 7 // "▶ PLAN" or "▸ AUTO"
	availableWidth := mi.width - 4
	spacingWidth := availableWidth - modeWidth

	if spacingWidth > 0 {
		return strings.Repeat(" ", spacingWidth) + styledMode
	}
	return styledMode
}
