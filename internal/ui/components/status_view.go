package components

import (
	"fmt"
	"time"

	progress "charm.land/bubbles/v2/progress"
	spinner "charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	domain "github.com/inference-gateway/cli/internal/domain"
	formatting "github.com/inference-gateway/cli/internal/formatting"
	hints "github.com/inference-gateway/cli/internal/ui/hints"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
	icons "github.com/inference-gateway/cli/internal/ui/styles/icons"
)

// StatusView handles status messages, errors, and loading spinners
type StatusView struct {
	message          string
	isError          bool
	isSpinner        bool
	spinner          spinner.Model
	startTime        time.Time
	baseMessage      string
	debugInfo        string
	width            int
	statusType       domain.StatusType
	progress         *domain.StatusProgress
	savedState       *StatusState
	styleProvider    *styles.Provider
	keyHintFormatter *hints.Formatter
	toolName         string
	stateManager     statusViewState
	pausedAt         time.Time
}

// statusViewState is the narrow slice of StateManager the status view reads:
// the approval/question overlays (to pause timers) plus retry status.
type statusViewState interface {
	approvalOverlayReader
	domain.ChatSessionManager
}

// SetStateManager wires the state manager so the spinner line can reflect
// connection health (retries and stalled streams) while waiting for chunks.
func (sv *StatusView) SetStateManager(stateManager statusViewState) {
	sv.stateManager = stateManager
}

// StatusState represents a saved status state
type StatusState struct {
	message     string
	isError     bool
	isSpinner   bool
	startTime   time.Time
	baseMessage string
	statusType  domain.StatusType
	progress    *domain.StatusProgress
}

func NewStatusView(styleProvider *styles.Provider) *StatusView {
	s := newModernSpinner()
	s.Style = styleProvider.GetSpinnerStyle()
	return &StatusView{
		message:       "",
		isError:       false,
		isSpinner:     false,
		spinner:       s,
		styleProvider: styleProvider,
	}
}

func (sv *StatusView) ShowStatus(message string) {
	sv.message = message
	sv.baseMessage = message
	sv.isError = false
	sv.isSpinner = false
	sv.statusType = domain.StatusDefault
	sv.progress = nil
}

func (sv *StatusView) ShowStatusWithType(message string, statusType domain.StatusType, progress *domain.StatusProgress) {
	sv.message = message
	sv.baseMessage = message
	sv.isError = false
	sv.isSpinner = false
	sv.statusType = statusType
	sv.progress = progress
}

func (sv *StatusView) ShowError(message string) {
	sv.message = message
	sv.isError = true
	sv.isSpinner = false
}

func (sv *StatusView) ShowSpinner(message string) {
	sv.baseMessage = message
	sv.message = message
	sv.isError = false
	sv.isSpinner = true
	sv.startTime = time.Now()
	sv.pausedAt = time.Time{}
	sv.statusType = domain.StatusDefault
	sv.progress = nil
}

func (sv *StatusView) ShowSpinnerWithType(message string, statusType domain.StatusType, progress *domain.StatusProgress) {
	sv.baseMessage = message
	sv.message = message
	sv.isError = false
	sv.isSpinner = true
	sv.startTime = time.Now()
	sv.pausedAt = time.Time{}
	sv.statusType = statusType
	sv.progress = progress
}

func (sv *StatusView) UpdateSpinnerMessage(message string, statusType domain.StatusType) {
	if sv.isSpinner {
		sv.baseMessage = message
		sv.message = message
		sv.statusType = statusType
	}
}

func (sv *StatusView) ClearStatus() {
	sv.message = ""
	sv.baseMessage = ""
	sv.isError = false
	sv.isSpinner = false
	sv.startTime = time.Time{}
	sv.pausedAt = time.Time{}
	sv.debugInfo = ""
	sv.statusType = domain.StatusDefault
	sv.progress = nil
}

// SaveCurrentState saves the current status state for later restoration
func (sv *StatusView) SaveCurrentState() {
	sv.savedState = &StatusState{
		message:     sv.message,
		isError:     sv.isError,
		isSpinner:   sv.isSpinner,
		startTime:   sv.startTime,
		baseMessage: sv.baseMessage,
		statusType:  sv.statusType,
		progress:    sv.progress,
	}
}

// RestoreSavedState restores the previously saved status state
func (sv *StatusView) RestoreSavedState() tea.Cmd {
	if sv.savedState == nil {
		return nil
	}

	wasSpinner := sv.savedState.isSpinner
	sv.message = sv.savedState.message
	sv.isError = sv.savedState.isError
	sv.isSpinner = sv.savedState.isSpinner
	sv.startTime = sv.savedState.startTime
	sv.baseMessage = sv.savedState.baseMessage
	sv.statusType = sv.savedState.statusType
	sv.progress = sv.savedState.progress

	sv.savedState = nil

	if wasSpinner {
		return sv.spinner.Tick
	}

	return nil
}

// HasSavedState returns true if there's a saved state that can be restored
func (sv *StatusView) HasSavedState() bool {
	return sv.savedState != nil
}

func (sv *StatusView) IsShowingError() bool {
	return sv.isError
}

func (sv *StatusView) IsShowingSpinner() bool {
	return sv.isSpinner
}

func (sv *StatusView) SetWidth(width int) {
	sv.width = width
}

func (sv *StatusView) SetHeight(height int) {
	// Status view has dynamic height
}

// SetKeyHintFormatter sets the key hint formatter for displaying keybinding hints
func (sv *StatusView) SetKeyHintFormatter(formatter *hints.Formatter) {
	sv.keyHintFormatter = formatter
}

func (sv *StatusView) Render() string {
	if sv.message == "" && sv.baseMessage == "" && sv.debugInfo == "" {
		return ""
	}

	sv.syncApprovalPause()

	var prefix, color, displayMessage string
	if sv.isError {
		prefix, color, displayMessage = sv.formatErrorStatus()
	} else if sv.isSpinner && !sv.pausedAt.IsZero() {
		prefix, color, displayMessage = "", sv.styleProvider.GetThemeColor("status"), sv.formatStatusWithType(sv.baseMessage)
	} else if sv.isSpinner {
		prefix, color, displayMessage = sv.formatSpinnerStatus()
	} else {
		prefix, color, displayMessage = sv.formatNormalStatus()
	}

	if sv.debugInfo != "" {
		if displayMessage != "" {
			displayMessage = fmt.Sprintf("%s | %s", displayMessage, sv.debugInfo)
		} else {
			displayMessage = sv.debugInfo
		}
	}

	if sv.width > 0 {
		availableWidth := sv.width - 4
		if availableWidth > 0 {
			displayMessage = formatting.WrapText(displayMessage, availableWidth)
		}
	}

	statusLine := displayMessage
	if prefix != "" {
		statusLine = fmt.Sprintf("%s %s", prefix, displayMessage)
	}
	styledStatusLine := sv.styleProvider.RenderWithColor(statusLine, color)
	return " " + styledStatusLine
}

// formatStatusWithType enhances the status message with type-specific formatting and progress
func (sv *StatusView) formatStatusWithType(message string) string {
	if sv.progress != nil && sv.progress.Total > 0 {
		progressBar := sv.createProgressBar()
		return fmt.Sprintf("%s %s", message, progressBar)
	}
	return message
}

// createProgressBar renders a gradient progress bar (via bubbles/v2/progress,
// matching the TodoBox treatment) followed by the current/total count when
// progress information is available.
func (sv *StatusView) createProgressBar() string {
	if sv.progress == nil || sv.progress.Total == 0 {
		return ""
	}

	percent := float64(sv.progress.Current) / float64(sv.progress.Total)
	bar := progress.New(
		progress.WithoutPercentage(),
		progress.WithWidth(10),
		progress.WithDefaultBlend(),
	).ViewAs(percent)

	return fmt.Sprintf("%s %d/%d", bar, sv.progress.Current, sv.progress.Total)
}

func (sv *StatusView) formatErrorStatus() (string, string, string) {
	errorColor := sv.styleProvider.GetThemeColor("error")
	return icons.CrossMarkStyle.Render(icons.CrossMark), errorColor, sv.message
}

func (sv *StatusView) formatSpinnerStatus() (string, string, string) {
	prefix := sv.spinner.View()

	elapsed := time.Since(sv.startTime)
	seconds := elapsed.Seconds()

	if reconnecting := sv.reconnectingMessage(); reconnecting != "" {
		message := fmt.Sprintf("%s (%.1fs)", reconnecting, seconds)
		return prefix, sv.styleProvider.GetThemeColor("status"),
			sv.styleProvider.RenderWithColor(message, sv.styleProvider.GetThemeColor("error"))
	}

	baseMsg := sv.formatStatusWithType(sv.baseMessage)
	displayMessage := fmt.Sprintf("%s (%.1fs)", baseMsg, seconds)

	statusColor := sv.styleProvider.GetThemeColor("status")
	return prefix, statusColor, displayMessage
}

// syncApprovalPause pauses the spinner and elapsed timer while an approval,
// plan-approval, or user-question overlay is waiting on the user, and shifts
// startTime forward on resume so the wait doesn't count as generation time.
// Derived from state on each render, like reconnectingMessage, so it needs no
// event ordering guarantees against the tool-progress spinner events.
func (sv *StatusView) syncApprovalPause() {
	syncApprovalPause(sv.stateManager, &sv.pausedAt, func(pause time.Duration) {
		sv.startTime = sv.startTime.Add(pause)
	})
}

// approvalOverlayReader is the narrow read surface for detecting whether an
// approval, plan-approval, or user-question overlay is blocked on the user.
// Shared by StatusView and ToolCallRenderer to pause their running timers.
type approvalOverlayReader interface {
	domain.ApprovalUIManager
	domain.PlanApprovalUIManager
	domain.UserQuestionUIManager
}

// syncApprovalPause records when the UI becomes blocked on a user decision and,
// on resume, calls shift with the paused duration so callers can push their
// running timers forward.
func syncApprovalPause(sm approvalOverlayReader, pausedAt *time.Time, shift func(time.Duration)) {
	if awaitingUserDecision(sm) {
		if pausedAt.IsZero() {
			*pausedAt = time.Now()
		}
		return
	}
	if !pausedAt.IsZero() {
		shift(time.Since(*pausedAt))
		*pausedAt = time.Time{}
	}
}

// awaitingUserDecision reports whether an approval, plan-approval, or
// user-question overlay is blocked on the user.
func awaitingUserDecision(sm approvalOverlayReader) bool {
	if sm == nil {
		return false
	}
	return sm.GetApprovalUIState() != nil ||
		sm.GetPlanApprovalUIState() != nil ||
		sm.GetUserQuestionUIState() != nil
}

// reconnectingMessage returns the reconnect notice when the HTTP client is
// retrying or the stream has stalled past the configured threshold, empty
// otherwise. Derived from state on each render, so it appears and clears with
// the regular spinner tick.
func (sv *StatusView) reconnectingMessage() string {
	if sv.stateManager == nil {
		return ""
	}
	status := sv.stateManager.GetRetryStatus()
	if status == nil {
		return ""
	}
	if status.Attempt == 0 {
		return "Reconnecting..."
	}
	return fmt.Sprintf("Reconnecting (%d/%d)", status.Attempt, status.MaxAttempts)
}

func (sv *StatusView) formatNormalStatus() (string, string, string) {
	statusColor := sv.styleProvider.GetThemeColor("status")
	displayMessage := sv.formatStatusWithType(sv.message)

	return "", statusColor, displayMessage
}

// Bubble Tea interface
func (sv *StatusView) Init() tea.Cmd { return sv.spinner.Tick }

func (sv *StatusView) View() tea.View { return tea.NewView(sv.Render()) }

func (sv *StatusView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	if windowMsg, ok := msg.(tea.WindowSizeMsg); ok {
		sv.SetWidth(windowMsg.Width)
	}

	if sv.isSpinner {
		sv.spinner, cmd = sv.spinner.Update(msg)
	}

	switch msg := msg.(type) {
	case domain.ChatStartEvent:
		sv.ShowSpinnerWithType("Starting response...", domain.StatusGenerating, nil)
		if cmd == nil {
			cmd = sv.spinner.Tick
		}

	case domain.ChatCompleteEvent:
		sv.ClearStatus()

	case domain.ChatErrorEvent:
		sv.ShowError(fmt.Sprintf("Error: %v", msg.Error))

	case domain.SetStatusEvent:
		sv.toolName = msg.ToolName
		if msg.Spinner {
			sv.ShowSpinnerWithType(msg.Message, msg.StatusType, msg.Progress)
			if cmd == nil {
				cmd = sv.spinner.Tick
			}
		} else {
			sv.ShowStatusWithType(msg.Message, msg.StatusType, msg.Progress)
		}

	case domain.UpdateStatusEvent:
		sv.toolName = msg.ToolName
		sv.UpdateSpinnerMessage(msg.Message, msg.StatusType)

	case domain.ShowErrorEvent:
		sv.ShowError(msg.Error)

	case domain.ClearErrorEvent:
		sv.ClearStatus()

	case domain.SaveStatusStateEvent:
		sv.SaveCurrentState()

	case domain.RestoreStatusStateEvent:
		if sv.HasSavedState() {
			restoreCmd := sv.RestoreSavedState()
			if cmd == nil {
				cmd = restoreCmd
			}
		}

	case domain.DebugKeyEvent:
		sv.debugInfo = fmt.Sprintf("DEBUG: %s -> %s", msg.Key, msg.Handler)

	case domain.BashCommandCompletedEvent:
		sv.ClearStatus()
	}

	return sv, cmd
}
