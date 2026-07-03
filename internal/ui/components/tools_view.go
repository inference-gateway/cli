package components

import (
	"fmt"
	"io"
	"strings"

	list "charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	ansi "github.com/charmbracelet/x/ansi"

	domain "github.com/inference-gateway/cli/internal/domain"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

// toolItem is a single row in the tools list: a tool name and a one-line
// summary of its (possibly empty) description.
type toolItem struct {
	name        string
	description string
}

// FilterValue is what the list filters against when the user searches (/).
func (i toolItem) FilterValue() string { return i.name }

// Title and Description satisfy list.DefaultItem so the default delegate can
// render the item as name + dim summary line.
func (i toolItem) Title() string       { return i.name }
func (i toolItem) Description() string { return i.description }

// summarizeDescription collapses a tool description to a single line: the
// first non-empty line with its whitespace normalized. Tool descriptions
// often carry multi-line usage blocks that would wreck a list row.
func summarizeDescription(s string) string {
	for line := range strings.Lines(s) {
		if fields := strings.Fields(line); len(fields) > 0 {
			return strings.Join(fields, " ")
		}
	}
	return ""
}

// toolDescLines is how many wrapped description lines an item may use before
// the last one is cut with an ellipsis.
const toolDescLines = 2

// toolDelegate wraps the default delegate to word-wrap the description to the
// list width at render time instead of cutting it to a single line.
type toolDelegate struct {
	list.DefaultDelegate
}

func (d toolDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	if it, ok := item.(toolItem); ok {
		it.description = wrapDescription(it.description, m.Width()-4)
		item = it
	}
	d.DefaultDelegate.Render(w, m, index, item)
}

// wrapDescription word-wraps a one-line description to at most toolDescLines
// lines of the given width, ending the last line with an ellipsis when text
// is cut. The width discounts the delegate's item padding; on absurdly narrow
// widths the text is left for the delegate's own truncation.
func wrapDescription(desc string, width int) string {
	if desc == "" || width < 8 {
		return desc
	}
	lines := strings.Split(ansi.Wrap(desc, width, ""), "\n")
	if len(lines) > toolDescLines {
		lines = lines[:toolDescLines]
		lines[toolDescLines-1] = ansi.Truncate(lines[toolDescLines-1], width-1, "") + "…"
	}
	return strings.Join(lines, "\n")
}

// newToolDelegate builds the default delegate restyled with the current
// theme - accent bar + accent name on the selected row, dim descriptions,
// underlined filter matches - sized for a name plus wrapped description.
func newToolDelegate(styleProvider *styles.Provider) toolDelegate {
	accent := lipgloss.Color(styleProvider.GetThemeColor("accent"))
	dim := lipgloss.Color(styleProvider.GetThemeColor("dim"))

	d := list.NewDefaultDelegate()
	d.SetHeight(1 + toolDescLines)
	d.Styles.NormalTitle = lipgloss.NewStyle().Padding(0, 0, 0, 2)
	d.Styles.NormalDesc = d.Styles.NormalTitle.Foreground(dim)
	d.Styles.SelectedTitle = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(accent).
		Foreground(accent).
		Bold(true).
		Padding(0, 0, 0, 1)
	d.Styles.SelectedDesc = d.Styles.SelectedTitle.Bold(false).Foreground(dim)
	d.Styles.DimmedTitle = lipgloss.NewStyle().Foreground(dim).Padding(0, 0, 0, 2)
	d.Styles.DimmedDesc = d.Styles.DimmedTitle
	d.Styles.FilterMatch = lipgloss.NewStyle().Underline(true).Foreground(accent)
	return toolDelegate{DefaultDelegate: d}
}

// toolsTitleStyle matches the status-bar selection pill: accent-on-background
// via reverse video, padded.
func toolsTitleStyle(styleProvider *styles.Provider) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(styleProvider.GetThemeColor("accent"))).
		Reverse(true).
		Bold(true).
		Padding(0, 1)
}

// ToolsViewImpl is a read-only, filterable list of the tools currently
// available to the agent. It reuses the bubbles/v2 list plumbing from the
// theme selector; enter deliberately does nothing yet - selecting a tool to
// inspect or execute it is future work.
type ToolsViewImpl struct {
	list          list.Model
	width         int
	height        int
	cancelled     bool
	toolService   domain.ToolService
	stateManager  domain.StateManager
	styleProvider *styles.Provider
}

// NewToolsView creates the tools list view. Items are populated by Reset on
// every entry because the tool set changes with the agent mode and with async
// MCP tool registration.
func NewToolsView(toolService domain.ToolService, stateManager domain.StateManager, styleProvider *styles.Provider) *ToolsViewImpl {
	l := list.New(
		nil,
		newToolDelegate(styleProvider),
		80, 24,
	)
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(true)
	l.DisableQuitKeybindings()
	l.SetStatusBarItemName("tool", "tools")

	m := &ToolsViewImpl{
		list:          l,
		width:         80,
		height:        24,
		toolService:   toolService,
		stateManager:  stateManager,
		styleProvider: styleProvider,
	}
	m.Reset()
	return m
}

// toolItems builds the list items from the tools the agent can currently use.
func (m *ToolsViewImpl) toolItems() []list.Item {
	if m.toolService == nil {
		return nil
	}

	mode := domain.AgentModeStandard
	if m.stateManager != nil {
		mode = m.stateManager.GetAgentMode()
	}

	tools := m.toolService.ListToolsForMode(mode)
	items := make([]list.Item, len(tools))
	for i, tool := range tools {
		item := toolItem{name: tool.Function.Name}
		if tool.Function.Description != nil {
			item.description = summarizeDescription(*tool.Function.Description)
		}
		items[i] = item
	}
	return items
}

func (m *ToolsViewImpl) Init() tea.Cmd { return nil }

func (m *ToolsViewImpl) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

// handleKey intercepts the cancel keys when the list is not actively
// filtering; otherwise it lets the list own typing, enter (apply filter) and
// esc (clear filter). Enter outside filtering is consumed as a no-op: the
// view is read-only for now.
func (m *ToolsViewImpl) handleKey(msg tea.KeyPressMsg) (handled bool, cmd tea.Cmd) {
	if m.list.FilterState() == list.Filtering {
		return false, nil
	}

	switch msg.String() {
	case "ctrl+c":
		m.cancelled = true
		return true, nil
	case "esc":
		if m.list.FilterState() == list.FilterApplied {
			return false, nil
		}
		m.cancelled = true
		return true, nil
	case "enter", " ":
		return true, nil
	}
	return false, nil
}

func (m *ToolsViewImpl) View() tea.View {
	return tea.NewView(m.list.View())
}

// IsCancelled returns true once the user has dismissed the view.
func (m *ToolsViewImpl) IsCancelled() bool { return m.cancelled }

// SetWidth sets the width of the tools view.
func (m *ToolsViewImpl) SetWidth(width int) {
	m.width = width
	m.list.SetSize(width, m.height)
}

// SetHeight sets the height of the tools view.
func (m *ToolsViewImpl) SetHeight(height int) {
	m.height = height
	m.list.SetSize(m.width, height)
}

// Reset returns the view to its initial state and rebuilds the items so the
// list reflects the current agent mode and any MCP tools registered since it
// was last shown. The delegate and title styles are rebuilt too so a theme
// switch is picked up on re-entry.
func (m *ToolsViewImpl) Reset() {
	m.cancelled = false
	m.list.ResetFilter()
	m.list.SetDelegate(newToolDelegate(m.styleProvider))
	m.list.Styles.Title = toolsTitleStyle(m.styleProvider)
	items := m.toolItems()
	m.list.SetItems(items)
	m.list.Select(0)
	m.list.Title = fmt.Sprintf("Available Tools (%d)", len(items))
}
