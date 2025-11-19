package components

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	domain "github.com/inference-gateway/cli/internal/domain"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

// ThemeSelectorImpl implements theme selection UI
type ThemeSelectorImpl struct {
	themes         []string
	filteredThemes []string
	selected       int
	width          int
	height         int
	done           bool
	cancelled      bool
	themeService   domain.ThemeService
	styleProvider  *styles.Provider
	searchQuery    string
	searchMode     bool
}

// NewThemeSelector creates a new theme selector
func NewThemeSelector(themeService domain.ThemeService, styleProvider *styles.Provider) *ThemeSelectorImpl {
	themes := themeService.ListThemes()
	m := &ThemeSelectorImpl{
		themes:         themes,
		filteredThemes: make([]string, len(themes)),
		selected:       0,
		width:          80,
		height:         24,
		themeService:   themeService,
		styleProvider:  styleProvider,
		searchQuery:    "",
		searchMode:     false,
	}
	copy(m.filteredThemes, themes)

	currentTheme := themeService.GetCurrentThemeName()
	for i, themeName := range themes {
		if themeName == currentTheme {
			m.selected = i
			break
		}
	}

	return m
}

func (m *ThemeSelectorImpl) Init() tea.Cmd {
	return nil
}

func (m *ThemeSelectorImpl) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleWindowResize(msg)
	case tea.KeyMsg:
		return m.handleKeyInput(msg)
	}

	return m, nil
}

func (m *ThemeSelectorImpl) handleWindowResize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height
	return m, nil
}

func (m *ThemeSelectorImpl) handleKeyInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m.handleCancel()
	case "esc":
		return m.handleEscape()
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

func (m *ThemeSelectorImpl) handleEscape() (tea.Model, tea.Cmd) {
	if m.searchMode {
		m.searchMode = false
		m.searchQuery = ""
		m.updateSearch()
		return m, nil
	}
	return m.handleCancel()
}

func (m *ThemeSelectorImpl) handleCancel() (tea.Model, tea.Cmd) {
	m.cancelled = true
	m.done = true
	return m, nil
}

func (m *ThemeSelectorImpl) handleNavigationUp() (tea.Model, tea.Cmd) {
	if m.selected > 0 {
		m.selected--
	}
	return m, nil
}

func (m *ThemeSelectorImpl) handleNavigationDown() (tea.Model, tea.Cmd) {
	if m.selected < len(m.filteredThemes)-1 {
		m.selected++
	}
	return m, nil
}

func (m *ThemeSelectorImpl) handleSelection() (tea.Model, tea.Cmd) {
	if len(m.filteredThemes) > 0 {
		selectedTheme := m.filteredThemes[m.selected]
		if err := m.themeService.SetTheme(selectedTheme); err == nil {
			m.done = true
			return m, func() tea.Msg {
				return domain.ThemeSelectedEvent{Theme: selectedTheme}
			}
		}
	}
	return m, nil
}

func (m *ThemeSelectorImpl) handleSearchToggle() (tea.Model, tea.Cmd) {
	m.searchMode = true
	return m, nil
}

func (m *ThemeSelectorImpl) handleBackspace() (tea.Model, tea.Cmd) {
	if m.searchMode && len(m.searchQuery) > 0 {
		m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
		m.updateSearch()
	} else if m.searchMode && len(m.searchQuery) == 0 {
		m.searchMode = false
	}
	return m, nil
}

func (m *ThemeSelectorImpl) handleCharacterInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.searchMode && len(msg.String()) == 1 && msg.String()[0] >= 32 {
		m.searchQuery += msg.String()
		m.updateSearch()
	}
	return m, nil
}

func (m *ThemeSelectorImpl) updateSearch() {
	m.filterThemes()
	m.selected = 0
}

func (m *ThemeSelectorImpl) View() string {
	var b strings.Builder

	b.WriteString(m.styleProvider.RenderWithColor("Select a Theme", m.styleProvider.GetThemeColor("accent")))
	b.WriteString("\n\n")

	if m.searchMode {
		b.WriteString(m.styleProvider.RenderWithColor("Search: "+m.searchQuery, m.styleProvider.GetThemeColor("status")))
		b.WriteString(m.styleProvider.RenderWithColor("│", m.styleProvider.GetThemeColor("accent")))
		b.WriteString("\n\n")
	} else {
		helpText := fmt.Sprintf("Press / to search • %d themes available", len(m.themes))
		b.WriteString(m.styleProvider.RenderDimText(helpText))
		b.WriteString("\n\n")
	}

	if len(m.filteredThemes) == 0 {
		if m.searchQuery != "" {
			b.WriteString(m.styleProvider.RenderWithColor(fmt.Sprintf("No themes match '%s'", m.searchQuery), m.styleProvider.GetThemeColor("error")))
		} else {
			b.WriteString(m.styleProvider.RenderWithColor("No themes available", m.styleProvider.GetThemeColor("error")))
		}
		b.WriteString("\n")
		return b.String()
	}

	maxVisible := m.height - 10
	if maxVisible > len(m.filteredThemes) {
		maxVisible = len(m.filteredThemes)
	}

	start := 0
	if m.selected >= maxVisible {
		start = m.selected - maxVisible + 1
	}

	currentTheme := m.themeService.GetCurrentThemeName()

	for i := start; i < start+maxVisible && i < len(m.filteredThemes); i++ {
		themeName := m.filteredThemes[i]

		prefix := "  "
		suffix := ""

		if i == m.selected {
			prefix = "▶ "
		}

		if themeName == currentTheme {
			suffix = " ✓"
		}

		line := prefix + themeName + suffix
		if i == m.selected {
			b.WriteString(m.styleProvider.RenderWithColor(line, m.styleProvider.GetThemeColor("accent")))
		} else if themeName == currentTheme {
			b.WriteString(m.styleProvider.RenderWithColor(line, m.styleProvider.GetThemeColor("status")))
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}

	if len(m.filteredThemes) > maxVisible {
		paginationText := fmt.Sprintf("Showing %d-%d of %d themes", start+1, start+maxVisible, len(m.filteredThemes))
		b.WriteString("\n")
		b.WriteString(m.styleProvider.RenderDimText(paginationText))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", m.width))
	b.WriteString("\n")

	if m.searchMode {
		b.WriteString(m.styleProvider.RenderDimText("Type to search, ↑↓ to navigate, Enter to select, Esc to clear search"))
	} else {
		b.WriteString(m.styleProvider.RenderDimText("Use ↑↓ arrows to navigate, Enter to select, / to search, Esc/Ctrl+C to cancel"))
	}

	return b.String()
}

// filterThemes filters the themes based on the search query
func (m *ThemeSelectorImpl) filterThemes() {
	if m.searchQuery == "" {
		m.filteredThemes = make([]string, len(m.themes))
		copy(m.filteredThemes, m.themes)
		return
	}

	m.filteredThemes = m.filteredThemes[:0]
	query := strings.ToLower(m.searchQuery)

	for _, themeName := range m.themes {
		if strings.Contains(strings.ToLower(themeName), query) {
			m.filteredThemes = append(m.filteredThemes, themeName)
		}
	}
}

// IsSelected returns true if a theme was selected
func (m *ThemeSelectorImpl) IsSelected() bool {
	return m.done && !m.cancelled
}

// IsCancelled returns true if selection was cancelled
func (m *ThemeSelectorImpl) IsCancelled() bool {
	return m.cancelled
}

// GetSelected returns the selected theme
func (m *ThemeSelectorImpl) GetSelected() string {
	if m.IsSelected() && len(m.themes) > 0 {
		return m.themes[m.selected]
	}
	return ""
}

// SetWidth sets the width of the theme selector
func (m *ThemeSelectorImpl) SetWidth(width int) {
	m.width = width
}

// SetHeight sets the height of the theme selector
func (m *ThemeSelectorImpl) SetHeight(height int) {
	m.height = height
}

// Reset resets the theme selector to its initial state
func (m *ThemeSelectorImpl) Reset() {
	m.done = false
	m.cancelled = false
	m.searchQuery = ""
	m.searchMode = false
	m.selected = 0
	m.updateSearch()

	// Reset to current theme selection
	currentTheme := m.themeService.GetCurrentThemeName()
	for i, themeName := range m.themes {
		if themeName == currentTheme {
			m.selected = i
			break
		}
	}
}
