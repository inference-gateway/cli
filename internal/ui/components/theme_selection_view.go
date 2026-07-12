package components

import (
	"fmt"
	"io"

	key "charm.land/bubbles/v2/key"
	list "charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"

	domain "github.com/inference-gateway/cli/internal/domain"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

// themeItem is a single selectable theme row in the theme list.
type themeItem struct {
	name    string
	current bool
}

// FilterValue is what the list filters against when the user searches (/).
func (i themeItem) FilterValue() string { return i.name }

// themeDelegate renders a themeItem as a single line: a caret on the
// highlighted row, the theme name, and a check marker on the active theme.
// It is the reference pattern for migrating the other hand-rolled selectors
// (model / conversation / file) to bubbles/v2/list.
type themeDelegate struct {
	styleProvider *styles.Provider
}

func (d themeDelegate) Height() int                             { return 1 }
func (d themeDelegate) Spacing() int                            { return 0 }
func (d themeDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d themeDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	it, ok := item.(themeItem)
	if !ok {
		return
	}

	selected := index == m.Index()

	prefix := "  "
	if selected {
		prefix = "▶ "
	}
	suffix := ""
	if it.current {
		suffix = " ✓"
	}
	line := prefix + it.name + suffix

	switch {
	case selected:
		line = d.styleProvider.RenderWithColor(line, d.styleProvider.GetThemeColor("accent"))
	case it.current:
		line = d.styleProvider.RenderWithColor(line, d.styleProvider.GetThemeColor("status"))
	}

	_, _ = fmt.Fprint(w, line)
}

// ThemeSelectorImpl implements theme selection UI on top of bubbles/v2/list,
// which provides cursor movement, fuzzy filtering (press /), pagination and
// help for free.
type ThemeSelectorImpl struct {
	list          list.Model
	themes        []string
	width         int
	height        int
	done          bool
	cancelled     bool
	selectedTheme string
	themeService  domain.ThemeService
	styleProvider *styles.Provider
}

// NewThemeSelector creates a new theme selector.
func NewThemeSelector(themeService domain.ThemeService, styleProvider *styles.Provider) *ThemeSelectorImpl {
	themes := themeService.ListThemes()

	l := list.New(
		themeItems(themes, themeService.GetCurrentThemeName()),
		themeDelegate{styleProvider: styleProvider},
		80, 24,
	)
	l.Title = "Select a Theme"
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(true)
	l.DisableQuitKeybindings()
	l.Styles.Title = lipgloss.NewStyle().
		Foreground(lipgloss.Color(styleProvider.GetThemeColor("accent"))).
		Bold(true)

	m := &ThemeSelectorImpl{
		list:          l,
		themes:        themes,
		width:         80,
		height:        24,
		themeService:  themeService,
		styleProvider: styleProvider,
	}
	m.selectCurrentTheme()
	return m
}

// themeItems builds the list items, marking the active theme.
func themeItems(themes []string, current string) []list.Item {
	items := make([]list.Item, len(themes))
	for i, name := range themes {
		items[i] = themeItem{name: name, current: name == current}
	}
	return items
}

// selectCurrentTheme moves the cursor to the active theme.
func (m *ThemeSelectorImpl) selectCurrentTheme() {
	current := m.themeService.GetCurrentThemeName()
	for i, name := range m.themes {
		if name == current {
			m.list.Select(i)
			return
		}
	}
}

func (m *ThemeSelectorImpl) Init() tea.Cmd { return nil }

func (m *ThemeSelectorImpl) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.list.SetSize(msg.Width, msg.Height)
		return m, nil
	case tea.KeyPressMsg:
		if handled, cmd := m.handleKey(msg); handled {
			return m, cmd
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

// handleKey intercepts selection/cancel keys when the list is not actively
// filtering; otherwise it lets the list own typing, enter (apply filter) and
// esc (clear filter).
func (m *ThemeSelectorImpl) handleKey(msg tea.KeyPressMsg) (handled bool, cmd tea.Cmd) {
	if m.list.FilterState() == list.Filtering {
		return false, nil
	}

	switch {
	case key.Matches(msg, listViewKeys.cancel):
		m.cancel()
		return true, nil
	case key.Matches(msg, listViewKeys.esc):
		if m.list.FilterState() == list.FilterApplied {
			return false, nil
		}
		m.cancel()
		return true, nil
	case key.Matches(msg, listViewKeys.selectKey):
		return true, m.selectTheme()
	}
	return false, nil
}

func (m *ThemeSelectorImpl) cancel() {
	m.cancelled = true
	m.done = true
}

func (m *ThemeSelectorImpl) selectTheme() tea.Cmd {
	item, ok := m.list.SelectedItem().(themeItem)
	if !ok {
		return nil
	}
	if err := m.themeService.SetTheme(item.name); err != nil {
		return nil
	}
	m.selectedTheme = item.name
	m.done = true
	return func() tea.Msg {
		return domain.ThemeSelectedEvent{Theme: item.name}
	}
}

func (m *ThemeSelectorImpl) View() tea.View {
	return tea.NewView(m.list.View())
}

// IsSelected returns true if a theme was selected.
func (m *ThemeSelectorImpl) IsSelected() bool { return m.done && !m.cancelled }

// IsCancelled returns true if selection was cancelled.
func (m *ThemeSelectorImpl) IsCancelled() bool { return m.cancelled }

// GetSelected returns the selected theme.
func (m *ThemeSelectorImpl) GetSelected() string {
	if m.IsSelected() {
		return m.selectedTheme
	}
	return ""
}

// SetWidth sets the width of the theme selector.
func (m *ThemeSelectorImpl) SetWidth(width int) {
	m.width = width
	m.list.SetSize(width, m.height)
}

// SetHeight sets the height of the theme selector.
func (m *ThemeSelectorImpl) SetHeight(height int) {
	m.height = height
	m.list.SetSize(m.width, height)
}

// Reset returns the selector to its initial state, rebuilding the items so the
// active-theme marker reflects any theme change since it was last shown.
func (m *ThemeSelectorImpl) Reset() {
	m.done = false
	m.cancelled = false
	m.selectedTheme = ""
	m.list.ResetFilter()
	m.list.SetItems(themeItems(m.themes, m.themeService.GetCurrentThemeName()))
	m.selectCurrentTheme()
}
