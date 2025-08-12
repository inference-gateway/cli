package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbletea"
	"github.com/inference-gateway/cli/internal/domain"
)

// ModelSelectorImpl implements model selection UI
type ModelSelectorImpl struct {
	models         []string
	filteredModels []string
	selected       int
	width          int
	height         int
	theme          Theme
	done           bool
	cancelled      bool
	modelService   domain.ModelService
	searchQuery    string
	searchMode     bool
}

// NewModelSelector creates a new model selector
func NewModelSelector(models []string, modelService domain.ModelService, theme Theme) *ModelSelectorImpl {
	m := &ModelSelectorImpl{
		models:         models,
		filteredModels: make([]string, len(models)),
		selected:       0,
		width:          80,
		height:         24,
		theme:          theme,
		modelService:   modelService,
		searchQuery:    "",
		searchMode:     false,
	}
	copy(m.filteredModels, models)
	return m
}

func (m *ModelSelectorImpl) Init() tea.Cmd {
	return nil
}

func (m *ModelSelectorImpl) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			m.cancelled = true
			m.done = true
			return m, tea.Quit

		case "up", "k":
			if m.selected > 0 {
				m.selected--
			}
			return m, nil

		case "down", "j":
			if m.selected < len(m.filteredModels)-1 {
				m.selected++
			}
			return m, nil

		case "enter", " ":
			if len(m.filteredModels) > 0 {
				selectedModel := m.filteredModels[m.selected]
				// Select the model
				if err := m.modelService.SelectModel(selectedModel); err == nil {
					m.done = true
					return m, func() tea.Msg {
						return ModelSelectedMsg{Model: selectedModel}
					}
				}
			}
			return m, nil

		case "esc":
			if m.searchMode {
				m.searchMode = false
				m.searchQuery = ""
				m.filterModels()
				m.selected = 0
				return m, nil
			}
			m.cancelled = true
			m.done = true
			return m, tea.Quit

		case "/":
			m.searchMode = true
			return m, nil

		case "backspace":
			if m.searchMode && len(m.searchQuery) > 0 {
				m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
				m.filterModels()
				m.selected = 0
			}
			return m, nil

		default:
			if m.searchMode && len(msg.String()) == 1 && msg.String()[0] >= 32 {
				m.searchQuery += msg.String()
				m.filterModels()
				m.selected = 0
			}
			return m, nil
		}
	}

	return m, nil
}

func (m *ModelSelectorImpl) View() string {
	var b strings.Builder

	// Header
	b.WriteString(fmt.Sprintf("%s🤖 Select a Model%s\n\n",
		m.theme.GetAccentColor(), "\033[0m"))

	// Search box
	if m.searchMode {
		b.WriteString(fmt.Sprintf("%sSearch: %s%s│%s\n\n",
			m.theme.GetStatusColor(), m.searchQuery, m.theme.GetAccentColor(), "\033[0m"))
	} else {
		b.WriteString(fmt.Sprintf("%sPress / to search • %d models available%s\n\n",
			m.theme.GetDimColor(), len(m.models), "\033[0m"))
	}

	if len(m.filteredModels) == 0 {
		if m.searchQuery != "" {
			b.WriteString(fmt.Sprintf("%sNo models match '%s'%s\n",
				m.theme.GetErrorColor(), m.searchQuery, "\033[0m"))
		} else {
			b.WriteString(fmt.Sprintf("%sNo models available%s\n",
				m.theme.GetErrorColor(), "\033[0m"))
		}
		return b.String()
	}

	// Model list
	maxVisible := m.height - 10 // Reserve space for header, search, and footer
	if maxVisible > len(m.filteredModels) {
		maxVisible = len(m.filteredModels)
	}

	start := 0
	if m.selected >= maxVisible {
		start = m.selected - maxVisible + 1
	}

	for i := start; i < start+maxVisible && i < len(m.filteredModels); i++ {
		model := m.filteredModels[i]

		if i == m.selected {
			b.WriteString(fmt.Sprintf("%s▶ %s%s\n",
				m.theme.GetAccentColor(), model, "\033[0m"))
		} else {
			b.WriteString(fmt.Sprintf("  %s\n", model))
		}
	}

	if len(m.filteredModels) > maxVisible {
		b.WriteString(fmt.Sprintf("\n%sShowing %d-%d of %d models%s\n",
			m.theme.GetDimColor(), start+1, start+maxVisible, len(m.filteredModels), "\033[0m"))
	}

	// Footer
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", m.width))
	b.WriteString("\n")
	if m.searchMode {
		b.WriteString(fmt.Sprintf("%sType to search, ↑↓ to navigate, Enter to select, Esc to clear search%s",
			m.theme.GetDimColor(), "\033[0m"))
	} else {
		b.WriteString(fmt.Sprintf("%sUse ↑↓ arrows to navigate, Enter to select, / to search, Esc/Ctrl+C to cancel%s",
			m.theme.GetDimColor(), "\033[0m"))
	}

	return b.String()
}

// filterModels filters the models based on the search query
func (m *ModelSelectorImpl) filterModels() {
	if m.searchQuery == "" {
		m.filteredModels = make([]string, len(m.models))
		copy(m.filteredModels, m.models)
		return
	}

	m.filteredModels = m.filteredModels[:0]
	query := strings.ToLower(m.searchQuery)

	for _, model := range m.models {
		if strings.Contains(strings.ToLower(model), query) {
			m.filteredModels = append(m.filteredModels, model)
		}
	}
}

// IsSelected returns true if a model was selected
func (m *ModelSelectorImpl) IsSelected() bool {
	return m.done && !m.cancelled
}

// IsCancelled returns true if selection was cancelled
func (m *ModelSelectorImpl) IsCancelled() bool {
	return m.cancelled
}

// GetSelected returns the selected model
func (m *ModelSelectorImpl) GetSelected() string {
	if m.IsSelected() && len(m.models) > 0 {
		return m.models[m.selected]
	}
	return ""
}

// SetWidth sets the width of the model selector
func (m *ModelSelectorImpl) SetWidth(width int) {
	m.width = width
}

// SetHeight sets the height of the model selector
func (m *ModelSelectorImpl) SetHeight(height int) {
	m.height = height
}
