package components

import (
	"fmt"
	"io"

	list "charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"

	domain "github.com/inference-gateway/cli/internal/domain"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

// toolItem is a single row in the tools list: a tool name and its (possibly
// empty) description.
type toolItem struct {
	name        string
	description string
}

// FilterValue is what the list filters against when the user searches (/).
func (i toolItem) FilterValue() string { return i.name }

// toolDelegate renders a toolItem as a single line: a caret on the highlighted
// row, the tool name in accent, and the description dimmed and truncated to
// the available width.
type toolDelegate struct {
	styleProvider *styles.Provider
}

func (d toolDelegate) Height() int                             { return 1 }
func (d toolDelegate) Spacing() int                            { return 0 }
func (d toolDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d toolDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	it, ok := item.(toolItem)
	if !ok {
		return
	}

	selected := index == m.Index()

	prefix := "  "
	if selected {
		prefix = "▶ "
	}

	desc := it.description
	if maxDesc := m.Width() - len(prefix) - len(it.name) - 3; maxDesc > 0 && len(desc) > maxDesc {
		desc = desc[:maxDesc-1] + "…"
	}

	name := prefix + it.name
	if selected {
		name = d.styleProvider.RenderWithColor(name, d.styleProvider.GetThemeColor("accent"))
	}
	if desc != "" {
		desc = d.styleProvider.RenderWithColor(" — "+desc, d.styleProvider.GetThemeColor("dim"))
	}

	_, _ = fmt.Fprint(w, name+desc)
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
		toolDelegate{styleProvider: styleProvider},
		80, 24,
	)
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(true)
	l.DisableQuitKeybindings()
	l.Styles.Title = lipgloss.NewStyle().
		Foreground(lipgloss.Color(styleProvider.GetThemeColor("accent"))).
		Bold(true)

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
			item.description = *tool.Function.Description
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
// was last shown.
func (m *ToolsViewImpl) Reset() {
	m.cancelled = false
	m.list.ResetFilter()
	items := m.toolItems()
	m.list.SetItems(items)
	m.list.Select(0)
	m.list.Title = fmt.Sprintf("Available Tools (%d)", len(items))
}
