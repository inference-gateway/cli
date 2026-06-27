package components

import (
	"cmp"
	"slices"

	help "charm.land/bubbles/v2/help"
	key "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"

	domain "github.com/inference-gateway/cli/internal/domain"
	ui "github.com/inference-gateway/cli/internal/ui"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

// HelpBar displays keyboard shortcuts at the bottom of the screen
type HelpBar struct {
	enabled       bool
	width         int
	shortcuts     []ui.KeyShortcut
	styleProvider *styles.Provider
	help          help.Model

	styledAccent string
	styledDim    string
}

func NewHelpBar(styleProvider *styles.Provider) *HelpBar {
	return &HelpBar{
		enabled:       false,
		width:         80,
		shortcuts:     make([]ui.KeyShortcut, 0),
		styleProvider: styleProvider,
		help:          help.New(),
	}
}

func (hb *HelpBar) SetShortcuts(shortcuts []ui.KeyShortcut) {
	sortedShortcuts := make([]ui.KeyShortcut, len(shortcuts))
	copy(sortedShortcuts, shortcuts)

	slices.SortFunc(sortedShortcuts, func(a, b ui.KeyShortcut) int {
		if c := cmp.Compare(a.Key, b.Key); c != 0 {
			return c
		}
		return cmp.Compare(a.Description, b.Description)
	})

	hb.shortcuts = sortedShortcuts
}

func (hb *HelpBar) IsEnabled() bool {
	return hb.enabled
}

func (hb *HelpBar) SetEnabled(enabled bool) {
	hb.enabled = enabled
}

func (hb *HelpBar) SetWidth(width int) {
	hb.width = width
	hb.help.SetWidth(width)
}

func (hb *HelpBar) SetHeight(height int) {
	// Help bar height is driven by its content
}

// Render draws the shortcuts as a multi-column cheat sheet using
// bubbles/v2/help, which handles column layout, key/description alignment, and
// width-aware truncation. Colours follow the active theme.
func (hb *HelpBar) Render() string {
	if !hb.enabled || len(hb.shortcuts) == 0 {
		return ""
	}

	hb.help.SetWidth(hb.width)
	hb.refreshStyles()
	return hb.help.FullHelpView(hb.helpColumns())
}

// refreshStyles rebuilds the help styles only when the active theme colours
// change, so Render does not allocate three lipgloss styles on every frame.
func (hb *HelpBar) refreshStyles() {
	accent := hb.styleProvider.GetThemeColor("accent")
	dim := hb.styleProvider.GetThemeColor("dim")
	if accent == hb.styledAccent && dim == hb.styledDim {
		return
	}
	hb.styledAccent = accent
	hb.styledDim = dim

	dimColor := lipgloss.Color(dim)
	hb.help.Styles.FullKey = lipgloss.NewStyle().Foreground(lipgloss.Color(accent))
	hb.help.Styles.FullDesc = lipgloss.NewStyle().Foreground(dimColor)
	hb.help.Styles.FullSeparator = lipgloss.NewStyle().Foreground(dimColor)
}

// helpColumns converts the sorted shortcuts into key.Binding columns laid out
// column-major, so each column reads top to bottom.
func (hb *HelpBar) helpColumns() [][]key.Binding {
	bindings := make([]key.Binding, 0, len(hb.shortcuts))
	for _, s := range hb.shortcuts {
		bindings = append(bindings, key.NewBinding(
			key.WithKeys(s.Key),
			key.WithHelp(s.Key, s.Description),
		))
	}

	cols := hb.columnCount()
	rowsPerCol := (len(bindings) + cols - 1) / cols

	groups := make([][]key.Binding, 0, cols)
	for start := 0; start < len(bindings); start += rowsPerCol {
		end := min(start+rowsPerCol, len(bindings))
		groups = append(groups, bindings[start:end])
	}
	return groups
}

// columnCount picks how many help columns fit within the current width,
// clamped to between 1 and 4.
func (hb *HelpBar) columnCount() int {
	const minColumnWidth = 28
	return min(max(hb.width/minColumnWidth, 1), 4)
}

// Bubble Tea interface
func (hb *HelpBar) Init() tea.Cmd { return nil }

func (hb *HelpBar) View() tea.View { return tea.NewView(hb.Render()) }

func (hb *HelpBar) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		hb.SetWidth(msg.Width)
	case domain.ToggleHelpBarEvent:
		hb.enabled = !hb.enabled
	case domain.HideHelpBarEvent:
		hb.enabled = false
	}
	return hb, nil
}
