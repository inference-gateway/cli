package internal

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbletea"
)

// UpdateHistoryMsg is used to update chat history
type UpdateHistoryMsg struct {
	History []string
}

// SetStatusMsg is used to set status
type SetStatusMsg struct {
	Message string
	Spinner bool
}

// ApprovalRequestMsg is used to request approval for a command
type ApprovalRequestMsg struct {
	Command string
}

// ChatInputModel represents a persistent chat input interface
type ChatInputModel struct {
	textarea         []string
	cursor           int
	lineIndex        int
	width            int
	height           int
	chatHistory      []string
	historyScroll    int
	focusOnHistory   bool
	statusMessage    string
	showSpinner      bool
	spinnerFrame     int
	inputSubmitted   bool
	lastInput        string
	startTime        time.Time
	showTimer        bool
	cancelled        bool
	approvalPending  bool
	approvalCommand  string
	approvalResponse int // 0=deny, 1=allow, 2=allow all
	approvalSelected int // Currently selected option in dropdown
}

// SpinnerTick represents a spinner animation tick
type SpinnerTick struct{}

// NewChatInputModel creates a new chat input model
func NewChatInputModel() *ChatInputModel {
	return &ChatInputModel{
		textarea:         []string{""},
		cursor:           0,
		lineIndex:        0,
		width:            80,
		height:           20,
		chatHistory:      []string{},
		historyScroll:    0,
		focusOnHistory:   false,
		statusMessage:    "",
		showSpinner:      false,
		spinnerFrame:     0,
		inputSubmitted:   false,
		lastInput:        "",
		startTime:        time.Now(),
		showTimer:        false,
		cancelled:        false,
		approvalPending:  false,
		approvalCommand:  "",
		approvalResponse: -1,
		approvalSelected: 0,
	}
}

func (m *ChatInputModel) Init() tea.Cmd {
	return nil
}

func (m *ChatInputModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case UpdateHistoryMsg:
		m.chatHistory = msg.History
		return m, nil

	case ApprovalRequestMsg:
		m.approvalPending = true
		m.approvalCommand = msg.Command
		m.approvalResponse = -1
		m.approvalSelected = 0 // Start with first option selected
		return m, nil

	case SetStatusMsg:
		m.statusMessage = msg.Message
		if msg.Spinner {
			m.showSpinner = true
			m.showTimer = true
			m.startTime = time.Now()
			m.spinnerFrame = 0
			return m, tea.Tick(time.Millisecond*100, func(t time.Time) tea.Msg {
				return SpinnerTick{}
			})
		} else {
			m.showSpinner = false
			m.showTimer = false
		}
		return m, nil

	case SpinnerTick:
		if m.showSpinner {
			m.spinnerFrame++
			return m, tea.Tick(time.Millisecond*100, func(t time.Time) tea.Msg {
				return SpinnerTick{}
			})
		}
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		if m.approvalPending {
			switch msg.String() {
			case "up":
				if m.approvalSelected > 0 {
					m.approvalSelected--
				}
				return m, nil
			case "down":
				if m.approvalSelected < 2 {
					m.approvalSelected++
				}
				return m, nil
			case "enter":
				switch m.approvalSelected {
				case 0:
					m.approvalResponse = 1 // Allow
				case 1:
					m.approvalResponse = 2 // Allow all
				case 2:
					m.approvalResponse = 0 // Deny
				}
				m.approvalPending = false
				return m, nil
			case "esc", "ctrl+c":
				m.approvalResponse = 0
				m.approvalPending = false
				return m, nil
			}
			return m, nil
		}

		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit

		case "esc":
			if m.showSpinner {
				m.cancelled = true
				m.showSpinner = false
				m.showTimer = false
				m.statusMessage = "❌ Generation cancelled by user"
			}
			return m, nil

		case "ctrl+d":
			if len(m.textarea) == 1 && m.textarea[0] == "" {
				return m, nil
			}
			m.lastInput = strings.Join(m.textarea, "\n")
			m.inputSubmitted = true
			return m, nil

		case "tab":
			m.focusOnHistory = !m.focusOnHistory
			return m, nil

		case "enter":
			if !m.focusOnHistory {
				currentLine := m.textarea[m.lineIndex]
				before := currentLine[:m.cursor]
				after := currentLine[m.cursor:]
				m.textarea[m.lineIndex] = before + " " + after
				m.cursor++
			}
			return m, nil

		case "backspace":
			if !m.focusOnHistory {
				if m.cursor > 0 {
					currentLine := m.textarea[m.lineIndex]
					before := currentLine[:m.cursor-1]
					after := currentLine[m.cursor:]
					m.textarea[m.lineIndex] = before + after
					m.cursor--
				}
			}
			return m, nil

		case "up":
			if m.focusOnHistory && len(m.chatHistory) > 0 {
				if m.historyScroll > 0 {
					m.historyScroll--
				}
			}
			return m, nil

		case "down":
			if m.focusOnHistory && len(m.chatHistory) > 0 {
				maxVisibleLines := m.getHistoryVisibleLines()
				maxScroll := max(0, len(m.chatHistory)-maxVisibleLines)
				if m.historyScroll < maxScroll {
					m.historyScroll++
				}
			}
			return m, nil

		case "left":
			if !m.focusOnHistory {
				if m.cursor > 0 {
					m.cursor--
				}
			}
			return m, nil

		case "right":
			if !m.focusOnHistory {
				currentText := strings.Join(m.textarea, " ")
				if m.cursor < len(currentText) {
					m.cursor++
				}
			}
			return m, nil

		default:
			if !m.focusOnHistory && len(msg.String()) == 1 && msg.String()[0] >= 32 {
				char := msg.String()
				currentLine := m.textarea[m.lineIndex]
				before := currentLine[:m.cursor]
				after := currentLine[m.cursor:]
				m.textarea[m.lineIndex] = before + char + after
				m.cursor++
			}
			return m, nil
		}
	}

	return m, nil
}

func (m *ChatInputModel) getHistoryVisibleLines() int {
	inputAreaHeight := 3
	statusAreaHeight := 3
	messagesHeight := m.height - inputAreaHeight - statusAreaHeight
	return max(0, messagesHeight)
}

func (m *ChatInputModel) View() string {
	var b strings.Builder

	inputAreaHeight := 3
	statusAreaHeight := 3

	messagesHeight := m.height - inputAreaHeight - statusAreaHeight

	if messagesHeight > 0 {
		maxVisibleLines := messagesHeight

		var startIdx, endIdx int

		if !m.focusOnHistory {
			if len(m.chatHistory) <= maxVisibleLines {
				startIdx = 0
				endIdx = len(m.chatHistory)
			} else {
				startIdx = len(m.chatHistory) - maxVisibleLines
				endIdx = len(m.chatHistory)
			}
		} else {
			startIdx = m.historyScroll
			endIdx = min(len(m.chatHistory), startIdx+maxVisibleLines)
		}

		displayedLines := 0

		linesShown := endIdx - startIdx
		emptyLinesAtTop := maxVisibleLines - linesShown
		for i := 0; i < emptyLinesAtTop; i++ {
			b.WriteString("\n")
			displayedLines++
		}

		for i := startIdx; i < endIdx && displayedLines < maxVisibleLines; i++ {
			line := m.chatHistory[i]
			b.WriteString(line + "\n")
			displayedLines++
		}

		for displayedLines < maxVisibleLines {
			b.WriteString("\n")
			displayedLines++
		}
	}

	b.WriteString("\n")

	if m.approvalPending {
		b.WriteString("⚠️  Command execution approval required:\n")
		b.WriteString(fmt.Sprintf("Command: %s\n\n", m.approvalCommand))

		options := []string{
			"Yes - Execute this command",
			"Yes, and don't ask again - Execute this and all future commands",
			"No - Cancel command execution",
		}

		for i, option := range options {
			if i == m.approvalSelected {
				b.WriteString(fmt.Sprintf("▶ \033[36;1m%s\033[0m\n", option))
			} else {
				b.WriteString(fmt.Sprintf("  %s\n", option))
			}
		}

		b.WriteString("\nUse ↑↓ arrows to navigate, Enter to select, Esc to cancel\n")
		b.WriteString(strings.Repeat("─", m.width) + "\n")
	} else {
		statusLine := ""
		if m.showSpinner {
			spinner := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
			spinnerChar := spinner[m.spinnerFrame%len(spinner)]

			if m.showTimer {
				elapsed := time.Since(m.startTime)
				seconds := elapsed.Seconds()
				statusLine = fmt.Sprintf("%s %s (%.1fs) - Press Esc to cancel", spinnerChar, m.statusMessage, seconds)
			} else {
				statusLine = fmt.Sprintf("%s %s - Press Esc to cancel", spinnerChar, m.statusMessage)
			}
		} else if m.statusMessage != "" {
			statusLine = m.statusMessage
		}

		if statusLine != "" {
			b.WriteString(fmt.Sprintf("ℹ️  %s\n", statusLine))
		} else {
			b.WriteString("\n")
		}
		b.WriteString(strings.Repeat("─", m.width) + "\n")
	}

	currentText := strings.Join(m.textarea, " ")
	if len(currentText) > 0 {
		if m.cursor <= len(currentText) {
			before := currentText[:min(m.cursor, len(currentText))]
			after := currentText[min(m.cursor, len(currentText)):]
			b.WriteString(fmt.Sprintf("> %s│%s", before, after))
		} else {
			b.WriteString(fmt.Sprintf("> %s│", currentText))
		}
	} else {
		b.WriteString("> │")
	}

	b.WriteString("\n\n\033[90mPress Ctrl+D to send message, Ctrl+C to exit\033[0m")

	return b.String()
}

// UpdateHistory updates the chat history
func (m *ChatInputModel) UpdateHistory(history []string) {
	m.chatHistory = history
}

// SetStatusMessage sets a status message
func (m *ChatInputModel) SetStatusMessage(message string) {
	m.statusMessage = message
	m.showSpinner = false
	m.showTimer = false
}

// SetSpinnerMessage sets a status message with spinner
func (m *ChatInputModel) SetSpinnerMessage(message string) tea.Cmd {
	m.statusMessage = message
	m.showSpinner = true
	m.showTimer = true
	m.startTime = time.Now()
	m.spinnerFrame = 0
	return tea.Tick(time.Millisecond*100, func(t time.Time) tea.Msg {
		return SpinnerTick{}
	})
}

// ClearStatus clears the status message
func (m *ChatInputModel) ClearStatus() {
	m.statusMessage = ""
	m.showSpinner = false
	m.showTimer = false
}

// HasInput returns true if there's input ready to be processed
func (m *ChatInputModel) HasInput() bool {
	return m.inputSubmitted
}

// GetInput returns the submitted input and clears the flag
func (m *ChatInputModel) GetInput() string {
	if m.inputSubmitted {
		input := m.lastInput
		m.inputSubmitted = false
		// Clear the textarea for next input
		m.textarea = []string{""}
		m.cursor = 0
		m.lineIndex = 0
		return input
	}
	return ""
}

// IsCancelled returns true if generation was cancelled
func (m *ChatInputModel) IsCancelled() bool {
	return m.cancelled
}

// ResetCancellation resets the cancellation flag
func (m *ChatInputModel) ResetCancellation() {
	m.cancelled = false
}

// IsApprovalPending returns true if approval is pending
func (m *ChatInputModel) IsApprovalPending() bool {
	return m.approvalPending
}

// GetApprovalResponse returns the approval response (-1=none, 0=deny, 1=allow, 2=allow all)
func (m *ChatInputModel) GetApprovalResponse() int {
	return m.approvalResponse
}

// ResetApproval resets the approval state
func (m *ChatInputModel) ResetApproval() {
	m.approvalPending = false
	m.approvalCommand = ""
	m.approvalResponse = -1
	m.approvalSelected = 0
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
