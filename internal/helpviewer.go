package internal

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbletea"
)

// HelpViewerModel represents a professional help display interface
type HelpViewerModel struct {
	width  int
	height int
	done   bool
}

// NewHelpViewerModel creates a new help viewer
func NewHelpViewerModel() *HelpViewerModel {
	return &HelpViewerModel{
		width:  80,
		height: 20,
		done:   false,
	}
}

func (m *HelpViewerModel) Init() tea.Cmd {
	return nil
}

func (m *HelpViewerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		// Any key press closes the help
		m.done = true
		return m, tea.Quit
	}

	return m, nil
}

func (m *HelpViewerModel) View() string {
	var b strings.Builder

	// Center the content
	maxWidth := min(m.width, 80)
	padding := (m.width - maxWidth) / 2
	if padding < 0 {
		padding = 0
	}

	// Title
	b.WriteString(strings.Repeat(" ", padding))
	b.WriteString("💬 \033[1;36mChat Session Help\033[0m\n\n")

	// Commands section
	b.WriteString(strings.Repeat(" ", padding))
	b.WriteString("🔧 \033[1mCommands:\033[0m\n")
	commands := [][]string{
		{"/exit, /quit", "Exit chat session"},
		{"/clear", "Clear conversation history"},
		{"/history", "Show conversation history"},
		{"/models", "Show current and available models"},
		{"/switch", "Switch to a different model"},
		{"/compact", "Export conversation to markdown file"},
		{"/help, ?", "Show this help"},
	}

	for _, cmd := range commands {
		b.WriteString(strings.Repeat(" ", padding))
		b.WriteString(fmt.Sprintf("  \033[36m%-15s\033[0m - %s\n", cmd[0], cmd[1]))
	}
	b.WriteString("\n")

	// File references section
	b.WriteString(strings.Repeat(" ", padding))
	b.WriteString("📁 \033[1mFile References:\033[0m\n")
	b.WriteString(strings.Repeat(" ", padding))
	b.WriteString("  \033[36m@filename.txt\033[0m    - Include file contents in your message\n")
	b.WriteString(strings.Repeat(" ", padding))
	b.WriteString("  \033[36m@./config.yaml\033[0m   - Include contents from current directory\n")
	b.WriteString(strings.Repeat(" ", padding))
	b.WriteString("  \033[36m@../README.md\033[0m    - Include contents from parent directory\n")
	b.WriteString(strings.Repeat(" ", padding))
	b.WriteString("  \033[90mMaximum file size: 100KB\033[0m\n\n")

	// Tools section
	b.WriteString(strings.Repeat(" ", padding))
	b.WriteString("🔧 \033[1mTools:\033[0m\n")
	b.WriteString(strings.Repeat(" ", padding))
	b.WriteString("  Models can invoke available tools automatically during conversation\n")
	b.WriteString(strings.Repeat(" ", padding))
	b.WriteString("  Use \033[36minfer tools list\033[0m to see whitelisted commands\n")
	b.WriteString(strings.Repeat(" ", padding))
	b.WriteString("  Use \033[36minfer tools enable/disable\033[0m to control tool access\n\n")

	// Input tips section
	b.WriteString(strings.Repeat(" ", padding))
	b.WriteString("💡 \033[1mInput Tips:\033[0m\n")
	inputTips := [][]string{
		{"Ctrl+D", "Send message"},
		{"Ctrl+C", "Cancel current input"},
		{"Esc", "Cancel generation while model is responding"},
		{"Tab", "Scroll through chat history"},
		{"↑↓", "Navigate text and history"},
		{"Enter", "New line in multi-line input"},
	}

	for _, tip := range inputTips {
		b.WriteString(strings.Repeat(" ", padding))
		b.WriteString(fmt.Sprintf("  \033[36m%-8s\033[0m - %s\n", tip[0], tip[1]))
	}
	b.WriteString("\n")

	// Footer with instructions
	b.WriteString(strings.Repeat(" ", padding))
	b.WriteString(strings.Repeat("─", min(maxWidth, 60)) + "\n")
	b.WriteString(strings.Repeat(" ", padding))
	b.WriteString("💡 \033[90;1mPress any key to return to chat\033[0m")

	return b.String()
}

// IsDone returns true if help viewing is complete
func (m *HelpViewerModel) IsDone() bool {
	return m.done
}
