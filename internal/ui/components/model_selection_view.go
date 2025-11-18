package components

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	lipgloss "github.com/charmbracelet/lipgloss"
	domain "github.com/inference-gateway/cli/internal/domain"
	colors "github.com/inference-gateway/cli/internal/ui/styles/colors"
)

// ModelSelectorImpl implements model selection UI
type ModelSelectorImpl struct {
	models         []string
	filteredModels []string
	selected       int
	width          int
	height         int
	themeService   domain.ThemeService
	done           bool
	cancelled      bool
	modelService   domain.ModelService
	searchQuery    string
	searchMode     bool
}

// NewModelSelector creates a new model selector
func NewModelSelector(models []string, modelService domain.ModelService, themeService domain.ThemeService) *ModelSelectorImpl {
	m := &ModelSelectorImpl{
		models:         models,
		filteredModels: make([]string, len(models)),
		selected:       0,
		width:          80,
		height:         24,
		themeService:   themeService,
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
		return m.handleWindowResize(msg)
	case tea.KeyMsg:
		return m.handleKeyInput(msg)
	}

	return m, nil
}

func (m *ModelSelectorImpl) handleWindowResize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height
	return m, nil
}

func (m *ModelSelectorImpl) handleKeyInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m.handleCancel()
	case "up":
		return m.handleNavigationUp()
	case "down":
		return m.handleNavigationDown()
	case "enter", " ":
		return m.handleSelection()
	case "/":
		if !m.searchMode {
			return m.handleSearchToggle()
		}
		return m.handleCharacterInput(msg)
	case "backspace":
		return m.handleBackspace()
	default:
		return m.handleCharacterInput(msg)
	}
}

func (m *ModelSelectorImpl) handleCancel() (tea.Model, tea.Cmd) {
	m.cancelled = true
	m.done = true
	return m, tea.Quit
}

func (m *ModelSelectorImpl) handleNavigationUp() (tea.Model, tea.Cmd) {
	if m.selected > 0 {
		m.selected--
	}
	return m, nil
}

func (m *ModelSelectorImpl) handleNavigationDown() (tea.Model, tea.Cmd) {
	if m.selected < len(m.filteredModels)-1 {
		m.selected++
	}
	return m, nil
}

func (m *ModelSelectorImpl) handleSelection() (tea.Model, tea.Cmd) {
	if len(m.filteredModels) > 0 {
		selectedModel := m.filteredModels[m.selected]
		if err := m.modelService.SelectModel(selectedModel); err == nil {
			m.done = true
			return m, func() tea.Msg {
				return domain.ModelSelectedEvent{Model: selectedModel}
			}
		}
	}
	return m, nil
}

func (m *ModelSelectorImpl) handleSearchToggle() (tea.Model, tea.Cmd) {
	m.searchMode = true
	return m, nil
}

func (m *ModelSelectorImpl) handleBackspace() (tea.Model, tea.Cmd) {
	if m.searchMode && len(m.searchQuery) > 0 {
		m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
		m.updateSearch()
	}
	return m, nil
}

func (m *ModelSelectorImpl) handleCharacterInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.searchMode && len(msg.String()) == 1 && msg.String()[0] >= 32 {
		m.searchQuery += msg.String()
		m.updateSearch()
	}
	return m, nil
}

func (m *ModelSelectorImpl) updateSearch() {
	m.filterModels()
	m.selected = 0
}

func (m *ModelSelectorImpl) View() string {
	var b strings.Builder

	// Use Lipgloss styling
	accentColor := m.themeService.GetCurrentTheme().GetAccentColor()
	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(accentColor))
	b.WriteString(titleStyle.Render("Select a Model"))
	b.WriteString("\n\n")

	if m.searchMode {
		statusColor := m.themeService.GetCurrentTheme().GetStatusColor()
		searchStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(statusColor))
		cursorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(accentColor))

		b.WriteString(searchStyle.Render("Search: " + m.searchQuery))
		b.WriteString(cursorStyle.Render("│"))
		b.WriteString("\n\n")
	} else {
		dimColor := m.themeService.GetCurrentTheme().GetDimColor()
		dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(dimColor))
		helpText := fmt.Sprintf("Press / to search • %d models available", len(m.models))
		b.WriteString(dimStyle.Render(helpText))
		b.WriteString("\n\n")
	}

	if len(m.filteredModels) == 0 {
		errorColor := m.themeService.GetCurrentTheme().GetErrorColor()
		errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(errorColor))

		if m.searchQuery != "" {
			b.WriteString(errorStyle.Render(fmt.Sprintf("No models match '%s'", m.searchQuery)))
		} else {
			b.WriteString(errorStyle.Render("No models available"))
		}
		b.WriteString("\n")
		return b.String()
	}

	maxVisible := m.height - 10
	if maxVisible > len(m.filteredModels) {
		maxVisible = len(m.filteredModels)
	}

	start := 0
	if m.selected >= maxVisible {
		start = m.selected - maxVisible + 1
	}

	selectedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(accentColor))

	for i := start; i < start+maxVisible && i < len(m.filteredModels); i++ {
		model := m.filteredModels[i]

		if i == m.selected {
			b.WriteString(selectedStyle.Render("▶ " + model))
			b.WriteString("\n")
		} else {
			b.WriteString(fmt.Sprintf("  %s\n", model))
		}
	}

	if len(m.filteredModels) > maxVisible {
		dimColor := m.themeService.GetCurrentTheme().GetDimColor()
		dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(dimColor))
		paginationText := fmt.Sprintf("Showing %d-%d of %d models", start+1, start+maxVisible, len(m.filteredModels))

		b.WriteString("\n")
		b.WriteString(dimStyle.Render(paginationText))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(colors.CreateSeparator(m.width, "─"))
	b.WriteString("\n")

	dimColor := m.themeService.GetCurrentTheme().GetDimColor()
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(dimColor))

	if m.searchMode {
		b.WriteString(helpStyle.Render("Type to search, ↑↓ to navigate, Enter to select, Esc to clear search"))
	} else {
		b.WriteString(helpStyle.Render("Use ↑↓ arrows to navigate, Enter to select, / to search, Esc/Ctrl+C to cancel"))
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
