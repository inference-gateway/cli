package components

import (
	"fmt"
	"time"

	spinner "github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
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
	s := spinner.New()
	s.Spinner = spinner.Dot
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
	sv.statusType = domain.StatusDefault
	sv.progress = nil
}

func (sv *StatusView) ShowSpinnerWithType(message string, statusType domain.StatusType, progress *domain.StatusProgress) {
	sv.baseMessage = message
	sv.message = message
	sv.isError = false
	sv.isSpinner = true
	sv.startTime = time.Now()
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

	var prefix, color, displayMessage string
	if sv.isError {
		prefix, color, displayMessage = sv.formatErrorStatus()
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

	statusLine := fmt.Sprintf("%s %s", prefix, displayMessage)
	return sv.styleProvider.RenderWithColor(statusLine, color)
}

// getStatusIcon returns the appropriate icon for the current status type
func (sv *StatusView) getStatusIcon() string {
	switch sv.statusType {
	case domain.StatusThinking:
		return ""
	case domain.StatusGenerating:
		return ""
	case domain.StatusWorking:
		return ""
	case domain.StatusProcessing:
		return ""
	case domain.StatusPreparing:
		return ""
	default:
		return ""
	}
}

// formatStatusWithType enhances the status message with type-specific formatting and progress
func (sv *StatusView) formatStatusWithType(message string) string {
	if sv.progress != nil && sv.progress.Total > 0 {
		progressBar := sv.createProgressBar()
		return fmt.Sprintf("%s %s", message, progressBar)
	}
	return message
}

// createProgressBar creates a visual progress bar when progress information is available
func (sv *StatusView) createProgressBar() string {
	if sv.progress == nil || sv.progress.Total == 0 {
		return ""
	}

	barWidth := 10
	filled := int(float64(sv.progress.Current) / float64(sv.progress.Total) * float64(barWidth))

	bar := "["
	for i := 0; i < barWidth; i++ {
		if i < filled {
			bar += "█"
		} else {
			bar += "░"
		}
	}
	bar += fmt.Sprintf("] %d/%d", sv.progress.Current, sv.progress.Total)

	return bar
}

func (sv *StatusView) formatErrorStatus() (string, string, string) {
	errorColor := sv.styleProvider.GetThemeColor("error")
	return icons.CrossMarkStyle.Render(icons.CrossMark), errorColor, sv.message
}

func (sv *StatusView) formatSpinnerStatus() (string, string, string) {
	var prefix string
	if sv.statusType == domain.StatusThinking {
		prefix = fmt.Sprintf("%s %s", sv.getStatusIcon(), sv.spinner.View())
	} else {
		prefix = sv.spinner.View()
	}

	elapsed := time.Since(sv.startTime)
	seconds := elapsed.Seconds()
	baseMsg := sv.formatStatusWithType(sv.baseMessage)

	displayMessage := fmt.Sprintf("%s (%.1fs)", baseMsg, seconds)

	statusColor := sv.styleProvider.GetThemeColor("status")
	return prefix, statusColor, displayMessage
}

func (sv *StatusView) formatNormalStatus() (string, string, string) {
	prefix := sv.getStatusIcon()
	statusColor := sv.styleProvider.GetThemeColor("status")
	displayMessage := sv.formatStatusWithType(sv.message)

	return prefix, statusColor, displayMessage
}

// Bubble Tea interface
func (sv *StatusView) Init() tea.Cmd { return sv.spinner.Tick }

func (sv *StatusView) View() string { return sv.Render() }

func (sv *StatusView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	if windowMsg, ok := msg.(tea.WindowSizeMsg); ok {
		sv.SetWidth(windowMsg.Width)
	}

	if sv.isSpinner {
		sv.spinner, cmd = sv.spinner.Update(msg)
	}

	switch msg := msg.(type) {
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
	case domain.DebugKeyEvent:
		sv.debugInfo = fmt.Sprintf("DEBUG: %s -> %s", msg.Key, msg.Handler)
	case domain.BashCommandCompletedEvent:
		sv.ClearStatus()
	}

	return sv, cmd
}
