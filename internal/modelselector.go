package internal

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbletea"
)

// ModelSelectorModel represents a professional model selection interface
type ModelSelectorModel struct {
	models         []string
	filteredModels []string
	cursor         int
	searchQuery    string
	width          int
	height         int
	selected       string
	cancelled      bool
	done           bool
}

// NewModelSelectorModel creates a new model selector
func NewModelSelectorModel(models []string) *ModelSelectorModel {
	return &ModelSelectorModel{
		models:         models,
		filteredModels: models,
		cursor:         0,
		searchQuery:    "",
		width:          80,
		height:         20,
		selected:       "",
		cancelled:      false,
		done:           false,
	}
}

func (m *ModelSelectorModel) Init() tea.Cmd {
	return nil
}

func (m *ModelSelectorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			if len(m.filteredModels) > 0 && m.cursor < len(m.filteredModels) {
				m.selected = m.filteredModels[m.cursor]
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
			if m.cursor < len(m.filteredModels)-1 {
				m.cursor++
			}
			return m, nil

		case "backspace":
			if len(m.searchQuery) > 0 {
				m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
				m.filterModels()
				m.adjustCursor()
			}
			return m, nil

		default:
			if len(msg.String()) == 1 && msg.String()[0] >= 32 && msg.String()[0] <= 126 {
				m.searchQuery += msg.String()
				m.filterModels()
				m.adjustCursor()
			}
			return m, nil
		}
	}

	return m, nil
}

func (m *ModelSelectorModel) filterModels() {
	if m.searchQuery == "" {
		m.filteredModels = m.models
		return
	}

	var filtered []string
	query := strings.ToLower(m.searchQuery)

	for _, model := range m.models {
		modelLower := strings.ToLower(model)
		if strings.Contains(modelLower, query) {
			filtered = append(filtered, model)
		}
	}

	m.filteredModels = filtered
}

func (m *ModelSelectorModel) adjustCursor() {
	if m.cursor >= len(m.filteredModels) {
		if len(m.filteredModels) > 0 {
			m.cursor = len(m.filteredModels) - 1
		} else {
			m.cursor = 0
		}
	}
}

func (m *ModelSelectorModel) View() string {
	var b strings.Builder

	b.WriteString("🤖 Select a model for the chat session\n\n")

	searchBox := fmt.Sprintf("🔍 Search: %s", m.searchQuery)
	if len(m.searchQuery) == 0 {
		searchBox += "│"
	}
	b.WriteString(searchBox + "\n")
	b.WriteString(strings.Repeat("─", min(m.width, 60)) + "\n\n")

	if len(m.filteredModels) != len(m.models) {
		b.WriteString(fmt.Sprintf("Showing %d of %d models\n\n", len(m.filteredModels), len(m.models)))
	}

	if len(m.filteredModels) == 0 {
		b.WriteString("❌ No models match your search\n")
	} else {
		maxVisible := min(10, m.height-8)
		startIdx := 0
		endIdx := len(m.filteredModels)

		if len(m.filteredModels) > maxVisible {
			if m.cursor >= maxVisible/2 {
				startIdx = min(m.cursor-maxVisible/2, len(m.filteredModels)-maxVisible)
				endIdx = startIdx + maxVisible
			} else {
				endIdx = maxVisible
			}
		}

		for i := startIdx; i < endIdx && i < len(m.filteredModels); i++ {
			model := m.filteredModels[i]
			if i == m.cursor {
				b.WriteString(fmt.Sprintf("▶ \033[36;1m%s\033[0m\n", model))
			} else {
				b.WriteString(fmt.Sprintf("  %s\n", model))
			}
		}

		if len(m.filteredModels) > maxVisible {
			if startIdx > 0 {
				b.WriteString("\n  ↑ More models above\n")
			}
			if endIdx < len(m.filteredModels) {
				b.WriteString("  ↓ More models below\n")
			}
		}
	}

	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", min(m.width, 60)) + "\n")
	b.WriteString("💡 \033[90mType to search • ↑↓ Navigate • Enter Select • Esc Cancel\033[0m")

	return b.String()
}

// IsSelected returns true if a model was selected
func (m *ModelSelectorModel) IsSelected() bool {
	return m.done && !m.cancelled && m.selected != ""
}

// IsCancelled returns true if selection was cancelled
func (m *ModelSelectorModel) IsCancelled() bool {
	return m.cancelled
}

// GetSelected returns the selected model
func (m *ModelSelectorModel) GetSelected() string {
	return m.selected
}

// IsDone returns true if selection process is complete
func (m *ModelSelectorModel) IsDone() bool {
	return m.done
}
