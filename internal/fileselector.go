package internal

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbletea"
)

// FileSelectorModel represents a file selection interface
type FileSelectorModel struct {
	files         []string
	filteredFiles []string
	cursor        int
	searchQuery   string
	width         int
	height        int
	selected      string
	cancelled     bool
	done          bool
}

// NewFileSelectorModel creates a new file selector
func NewFileSelectorModel(files []string) *FileSelectorModel {
	return &FileSelectorModel{
		files:         files,
		filteredFiles: files,
		cursor:        0,
		searchQuery:   "",
		width:         80,
		height:        20,
		selected:      "",
		cancelled:     false,
		done:          false,
	}
}

func (m *FileSelectorModel) Init() tea.Cmd {
	return nil
}

func (m *FileSelectorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.cancelled = true
			m.done = true
			return m, tea.Quit

		case "enter":
			if len(m.filteredFiles) > 0 && m.cursor < len(m.filteredFiles) {
				m.selected = m.filteredFiles[m.cursor]
				m.done = true
				return m, tea.Quit
			}
			return m, nil

		case "up":
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil

		case "down":
			if m.cursor < len(m.filteredFiles)-1 {
				m.cursor++
			}
			return m, nil

		case "backspace":
			if len(m.searchQuery) > 0 {
				m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
				m.filterFiles()
				m.adjustCursor()
			}
			return m, nil

		default:
			if len(msg.String()) == 1 && msg.String()[0] >= 32 && msg.String()[0] <= 126 {
				m.searchQuery += msg.String()
				m.filterFiles()
				m.adjustCursor()
			}
			return m, nil
		}
	}

	return m, nil
}

func (m *FileSelectorModel) filterFiles() {
	if m.searchQuery == "" {
		m.filteredFiles = m.files
		return
	}

	var filtered []string
	query := strings.ToLower(m.searchQuery)

	for _, file := range m.files {
		fileLower := strings.ToLower(file)
		if strings.Contains(fileLower, query) {
			filtered = append(filtered, file)
		}
	}

	m.filteredFiles = filtered
}

func (m *FileSelectorModel) adjustCursor() {
	if m.cursor >= len(m.filteredFiles) {
		if len(m.filteredFiles) > 0 {
			m.cursor = len(m.filteredFiles) - 1
		} else {
			m.cursor = 0
		}
	}
}

func (m *FileSelectorModel) View() string {
	var b strings.Builder

	b.WriteString("📁 Select a file to include (type to search, ESC to cancel)\n\n")

	searchBox := fmt.Sprintf("🔍 Search: %s", m.searchQuery)
	if len(m.searchQuery) == 0 {
		searchBox += "│"
	}
	b.WriteString(searchBox + "\n")
	b.WriteString(strings.Repeat("─", min(m.width, 60)) + "\n\n")

	if len(m.filteredFiles) != len(m.files) {
		b.WriteString(fmt.Sprintf("Showing %d of %d files\n\n", len(m.filteredFiles), len(m.files)))
	}

	if len(m.filteredFiles) == 0 {
		b.WriteString("❌ No files match your search\n")
	} else {
		maxVisible := min(15, m.height-8)
		startIdx := 0
		endIdx := len(m.filteredFiles)

		if len(m.filteredFiles) > maxVisible {
			if m.cursor >= maxVisible/2 {
				startIdx = min(m.cursor-maxVisible/2, len(m.filteredFiles)-maxVisible)
				endIdx = startIdx + maxVisible
			} else {
				endIdx = maxVisible
			}
		}

		for i := startIdx; i < endIdx && i < len(m.filteredFiles); i++ {
			file := m.filteredFiles[i]
			if i == m.cursor {
				b.WriteString(fmt.Sprintf("▶ \033[36;1m%s\033[0m\n", file))
			} else {
				b.WriteString(fmt.Sprintf("  %s\n", file))
			}
		}

		if len(m.filteredFiles) > maxVisible {
			if startIdx > 0 {
				b.WriteString("\n  ↑ More files above\n")
			}
			if endIdx < len(m.filteredFiles) {
				b.WriteString("  ↓ More files below\n")
			}
		}
	}

	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", min(m.width, 60)) + "\n")
	b.WriteString("💡 \033[90mType to search • ↑↓ Navigate • Enter Select • Esc Cancel\033[0m")

	return b.String()
}

// IsSelected returns true if a file was selected
func (m *FileSelectorModel) IsSelected() bool {
	return m.done && !m.cancelled && m.selected != ""
}

// IsCancelled returns true if selection was cancelled
func (m *FileSelectorModel) IsCancelled() bool {
	return m.cancelled
}

// GetSelected returns the selected file
func (m *FileSelectorModel) GetSelected() string {
	return m.selected
}

// IsDone returns true if selection process is complete
func (m *FileSelectorModel) IsDone() bool {
	return m.done
}
