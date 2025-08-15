package components

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbletea"
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
}

func (sv *StatusView) ClearStatus() {
	sv.message = ""
	sv.baseMessage = ""
	sv.isError = false
	sv.isSpinner = false
	sv.tokenUsage = ""
	sv.startTime = time.Time{}
	sv.debugInfo = ""
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
		prefix = "❌"
		color = shared.ErrorColor.ANSI
		displayMessage = sv.message
	} else if sv.isSpinner {
		prefix = sv.spinner.View()
		color = shared.StatusColor.ANSI

		elapsed := time.Since(sv.startTime)
		seconds := int(elapsed.Seconds())
		displayMessage = fmt.Sprintf("%s (%ds) - Press ESC to interrupt", sv.baseMessage, seconds)
	} else {
		prefix = "ℹ️"
		color = shared.StatusColor.ANSI
		displayMessage = sv.message

		if sv.tokenUsage != "" {
			displayMessage = fmt.Sprintf("%s (%s)", displayMessage, sv.tokenUsage)
		}
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
			sv.ShowSpinner(msg.Message)
			if cmd == nil {
				cmd = sv.spinner.Tick
			}
		} else {
			sv.ShowStatus(msg.Message)
			if msg.TokenUsage != "" {
				sv.SetTokenUsage(msg.TokenUsage)
			}
		}
	case shared.ShowErrorMsg:
		sv.ShowError(msg.Error)
	case shared.ClearErrorMsg:
		sv.ClearStatus()
	case shared.DebugKeyMsg:
		sv.debugInfo = fmt.Sprintf("DEBUG: %s -> %s", msg.Key, msg.Handler)
	}

	return sv, cmd
}
