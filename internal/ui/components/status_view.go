package components

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/inference-gateway/cli/internal/ui/shared"
)

// StatusView handles status messages, errors, and loading spinners
type StatusView struct {
	message     string
	isError     bool
	isSpinner   bool
	spinner     spinner.Model
	startTime   time.Time
	tokenUsage  string
	baseMessage string
	debugInfo   string
	width       int
	statusType  shared.StatusType
	progress    *shared.StatusProgress
	savedState  *StatusState
}

// StatusState represents a saved status state
type StatusState struct {
	message     string
	isError     bool
	isSpinner   bool
	startTime   time.Time
	tokenUsage  string
	baseMessage string
	statusType  shared.StatusType
	progress    *shared.StatusProgress
}

func NewStatusView() *StatusView {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(shared.SpinnerColor.GetLipglossColor())
	return &StatusView{
		message:   "",
		isError:   false,
		isSpinner: false,
		spinner:   s,
	}
}

func (sv *StatusView) ShowStatus(message string) {
	sv.message = message
	sv.baseMessage = message
	sv.isError = false
	sv.isSpinner = false
	sv.tokenUsage = ""
	sv.statusType = shared.StatusDefault
	sv.progress = nil
}

func (sv *StatusView) ShowStatusWithType(message string, statusType shared.StatusType, progress *shared.StatusProgress) {
	sv.message = message
	sv.baseMessage = message
	sv.isError = false
	sv.isSpinner = false
	sv.tokenUsage = ""
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
	sv.tokenUsage = ""
	sv.statusType = shared.StatusDefault
	sv.progress = nil
}

func (sv *StatusView) ShowSpinnerWithType(message string, statusType shared.StatusType, progress *shared.StatusProgress) {
	sv.baseMessage = message
	sv.message = message
	sv.isError = false
	sv.isSpinner = true
	sv.startTime = time.Now()
	sv.tokenUsage = ""
	sv.statusType = statusType
	sv.progress = progress
}

func (sv *StatusView) UpdateSpinnerMessage(message string, statusType shared.StatusType) {
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
	sv.tokenUsage = ""
	sv.startTime = time.Time{}
	sv.debugInfo = ""
	sv.statusType = shared.StatusDefault
	sv.progress = nil
}

// SaveCurrentState saves the current status state for later restoration
func (sv *StatusView) SaveCurrentState() {
	sv.savedState = &StatusState{
		message:     sv.message,
		isError:     sv.isError,
		isSpinner:   sv.isSpinner,
		startTime:   sv.startTime,
		tokenUsage:  sv.tokenUsage,
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
	sv.tokenUsage = sv.savedState.tokenUsage
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

func (sv *StatusView) SetTokenUsage(usage string) {
	sv.tokenUsage = usage
}

func (sv *StatusView) SetWidth(width int) {
	sv.width = width
}

func (sv *StatusView) SetHeight(height int) {
	// Status view has dynamic height
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
			displayMessage = shared.WrapText(displayMessage, availableWidth)
		}
	}

	return fmt.Sprintf("%s%s %s%s", color, prefix, displayMessage, shared.Reset())
}

// getStatusIcon returns the appropriate icon for the current status type
func (sv *StatusView) getStatusIcon() string {
	switch sv.statusType {
	case shared.StatusThinking:
		return "🤔"
	case shared.StatusGenerating:
		return "🤖"
	case shared.StatusWorking:
		return "⚡"
	case shared.StatusProcessing:
		return "🔄"
	case shared.StatusPreparing:
		return "📋"
	default:
		return "📊"
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
	return "❌", shared.ErrorColor.ANSI, sv.message
}

func (sv *StatusView) formatSpinnerStatus() (string, string, string) {
	var prefix string
	if sv.statusType == shared.StatusThinking {
		prefix = fmt.Sprintf("%s %s", sv.getStatusIcon(), sv.spinner.View())
	} else {
		prefix = sv.spinner.View()
	}

	elapsed := time.Since(sv.startTime)
	seconds := int(elapsed.Seconds())
	baseMsg := sv.formatStatusWithType(sv.baseMessage)
	displayMessage := fmt.Sprintf("%s (%ds) - Press ESC to interrupt", baseMsg, seconds)

	return prefix, shared.StatusColor.ANSI, displayMessage
}

func (sv *StatusView) formatNormalStatus() (string, string, string) {
	prefix := sv.getStatusIcon()
	color := shared.StatusColor.ANSI
	displayMessage := sv.formatStatusWithType(sv.message)

	if sv.tokenUsage != "" {
		displayMessage = fmt.Sprintf("%s (%s)", displayMessage, sv.tokenUsage)
	}

	return prefix, color, displayMessage
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
	case shared.SetStatusMsg:
		if msg.Spinner {
			sv.ShowSpinnerWithType(msg.Message, msg.StatusType, msg.Progress)
			if cmd == nil {
				cmd = sv.spinner.Tick
			}
		} else {
			sv.ShowStatusWithType(msg.Message, msg.StatusType, msg.Progress)
			if msg.TokenUsage != "" {
				sv.SetTokenUsage(msg.TokenUsage)
			}
		}
	case shared.UpdateStatusMsg:
		sv.UpdateSpinnerMessage(msg.Message, msg.StatusType)
	case shared.ShowErrorMsg:
		sv.ShowError(msg.Error)
	case shared.ClearErrorMsg:
		sv.ClearStatus()
	case shared.DebugKeyMsg:
		sv.debugInfo = fmt.Sprintf("DEBUG: %s -> %s", msg.Key, msg.Handler)
	}

	return sv, cmd
}
