package components

import (
	"cmp"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	styles "github.com/inference-gateway/cli/internal/presentation/tui/styles"
)

// standardModeLabel names the standard mode, which the chat leaves unlabelled
// but a room shows, since its members' mode differs from the chat's.
const standardModeLabel = "▸ STANDARD"

// ModeIndicator names the current agent mode (PLAN/AUTO/JUDGE) as a label the
// application view lays out on the right of the status row. Inside a room it
// names the mode the room's members run in, which rooms.yaml sets, instead.
type ModeIndicator struct {
	stateManager  agentdomain.AgentModeState
	styleProvider *styles.Provider
	judgeModel    func() string
	room          string
	roomMode      string
}

// NewModeIndicator creates a new mode indicator
func NewModeIndicator(styleProvider *styles.Provider) *ModeIndicator {
	return &ModeIndicator{
		styleProvider: styleProvider,
	}
}

// SetJudgeModelFn names the model that approves tool calls in Auto+Judge mode
// so the indicator shows who the judge is. Resolved at render time so it
// follows a model switched during the session.
func (mi *ModeIndicator) SetJudgeModelFn(fn func() string) {
	mi.judgeModel = fn
}

// SetStateManager sets the state manager
func (mi *ModeIndicator) SetStateManager(stateManager agentdomain.AgentModeState) {
	mi.stateManager = stateManager
}

// SetRoom shows the mode the members of room run in, as the daemon reported
// it, instead of the chat's own. An empty room shows the chat's mode again.
func (mi *ModeIndicator) SetRoom(room, mode string) {
	mi.room = room
	mi.roomMode = mode
}

// Text returns the styled mode label, empty while the default mode is active
// outside a room.
func (mi *ModeIndicator) Text() string {
	var label string
	switch {
	case mi.room != "":
		label = roomLabel(mi.room, mi.roomMode)
	case mi.stateManager != nil:
		label = mi.modeLabel(mi.stateManager.GetAgentMode())
	}
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

// modeLabel names the chat's mode, appending the judge model for Auto+Judge.
// The default (standard) mode has no indicator, so it maps to an empty label.
func (mi *ModeIndicator) modeLabel(mode agentdomain.AgentMode) string {
	label := modeName(mode)
	if mode == agentdomain.AgentModeAutoWithJudge && mi.judgeModel != nil {
		if model := mi.judgeModel(); model != "" {
			label += " · " + model
		}
	}
	return label
}

// roomLabel names a room and the mode its members run in. A mode the daemon did
// not report, as a daemon of an older build never does, is left out rather
// than shown as standard, since the members may run in any mode.
func roomLabel(room, mode string) string {
	parsed, known := agentdomain.ParseAgentMode(mode)
	if !known {
		return "#" + room
	}
	return "#" + room + " " + cmp.Or(modeName(parsed), standardModeLabel)
}

// modeName names a mode, "" for the standard one.
func modeName(mode agentdomain.AgentMode) string {
	switch mode {
	case agentdomain.AgentModePlan:
		return "▶ PLAN"
	case agentdomain.AgentModeAutoAccept:
		return "▸ AUTO"
	case agentdomain.AgentModeAutoWithJudge:
		return "▸ AUTO+JUDGE"
	case agentdomain.AgentModeReadOnly:
		return "▸ READ-ONLY"
	}
	return ""
}
