package components

import (
	"fmt"
	"io"
	"slices"
	"strings"

	key "charm.land/bubbles/v2/key"
	list "charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	styles "github.com/inference-gateway/cli/internal/presentation/tui/styles"
)

// SubagentCatalog supplies the Markdown-defined subagent presets
// (.infer/agents/*.md) for the /agents listing.
type SubagentCatalog interface {
	ListMarkdownSubagents() []agentdomain.SubagentInfo
}

// subagentItem is a single row in the subagents list.
type subagentItem struct {
	info agentdomain.SubagentInfo
}

// FilterValue is what the list filters against when the user searches (/).
func (i subagentItem) FilterValue() string { return i.info.Name }

// subagentDelegate renders a subagentItem on two rows: the name, its capability
// type and description, then the tool allowlist, model and source it runs with.
type subagentDelegate struct {
	styleProvider *styles.Provider
}

func (d subagentDelegate) Height() int                             { return 2 }
func (d subagentDelegate) Spacing() int                            { return 0 }
func (d subagentDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d subagentDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	it, ok := item.(subagentItem)
	if !ok {
		return
	}

	selected := index == m.Index()

	prefix := "  "
	if selected {
		prefix = "▶ "
	}

	name := prefix + it.info.Name
	if selected {
		name = d.styleProvider.RenderWithColor(name, d.styleProvider.GetThemeColor("accent"))
	}

	mode := "read-write"
	modeColor := d.styleProvider.GetThemeColor("accent")
	if it.info.ReadOnly {
		mode = "read-only"
		modeColor = d.styleProvider.GetThemeColor("status")
	}
	modeTag := d.styleProvider.RenderWithColor(" ["+mode+"]", modeColor)
	detail := d.styleProvider.RenderWithColor(" - "+it.info.Description, d.styleProvider.GetThemeColor("dim"))

	caps := d.styleProvider.RenderWithColor("      "+subagentCapabilities(it.info), d.styleProvider.GetThemeColor("dim"))
	_, _ = fmt.Fprint(w, name+modeTag+detail+"\n"+caps)
}

// subagentCapabilities summarizes the preset: the tool allowlist (or the
// inherited parent set), the model (or inherit) and where the file lives.
func subagentCapabilities(info agentdomain.SubagentInfo) string {
	tools := "all tools (inherited)"
	if len(info.Tools) > 0 {
		tools = strings.Join(info.Tools, ", ")
	}
	model := info.Model
	if model == "" {
		model = "inherit"
	}
	return fmt.Sprintf("tools: %s | model: %s | %s", tools, model, info.Source)
}

// SubagentsView is a read-only, filterable list of the Markdown-defined
// subagent presets and their capabilities. Like the A2A agents view it is
// display-only.
type SubagentsView struct {
	list          list.Model
	width         int
	height        int
	cancelled     bool
	catalog       SubagentCatalog
	styleProvider *styles.Provider
}

// NewSubagentsView creates the subagents list view. Items are rebuilt on every
// Reset because presets are loaded once per session at startup.
func NewSubagentsView(catalog SubagentCatalog, styleProvider *styles.Provider) *SubagentsView {
	l := list.New(
		nil,
		subagentDelegate{styleProvider: styleProvider},
		80, 24,
	)
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(true)
	l.DisableQuitKeybindings()
	l.Styles.Title = lipgloss.NewStyle().
		Foreground(lipgloss.Color(styleProvider.GetThemeColor("accent"))).
		Bold(true)

	m := &SubagentsView{
		list:          l,
		width:         80,
		height:        24,
		catalog:       catalog,
		styleProvider: styleProvider,
	}
	m.Reset()
	return m
}

// subagentItems builds the list items from the catalog, sorted by name for a
// stable order.
func (m *SubagentsView) subagentItems() []list.Item {
	if m.catalog == nil {
		return nil
	}
	infos := m.catalog.ListMarkdownSubagents()
	slices.SortFunc(infos, func(a, b agentdomain.SubagentInfo) int {
		return strings.Compare(a.Name, b.Name)
	})
	items := make([]list.Item, 0, len(infos))
	for _, info := range infos {
		items = append(items, subagentItem{info: info})
	}
	return items
}

func (m *SubagentsView) Init() tea.Cmd { return nil }

func (m *SubagentsView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
// esc (clear filter). Enter outside filtering is consumed as a no-op.
func (m *SubagentsView) handleKey(msg tea.KeyPressMsg) (handled bool, cmd tea.Cmd) {
	if m.list.FilterState() == list.Filtering {
		return false, nil
	}

	switch {
	case key.Matches(msg, listViewKeys.cancel):
		m.cancelled = true
		return true, nil
	case key.Matches(msg, listViewKeys.esc):
		if m.list.FilterState() == list.FilterApplied {
			return false, nil
		}
		m.cancelled = true
		return true, nil
	case key.Matches(msg, listViewKeys.selectKey):
		return true, nil
	}
	return false, nil
}

func (m *SubagentsView) View() tea.View {
	return tea.NewView(m.list.View())
}

// IsCancelled returns true once the user has dismissed the view.
func (m *SubagentsView) IsCancelled() bool { return m.cancelled }

// SetWidth sets the width of the subagents view.
func (m *SubagentsView) SetWidth(width int) {
	m.width = width
	m.list.SetSize(width, m.height)
}

// SetHeight sets the height of the subagents view.
func (m *SubagentsView) SetHeight(height int) {
	m.height = height
	m.list.SetSize(m.width, height)
}

// Reset returns the view to its initial state and rebuilds the items.
func (m *SubagentsView) Reset() {
	m.cancelled = false
	m.list.ResetFilter()
	items := m.subagentItems()
	_ = m.list.SetItems(items)
	m.list.Title = fmt.Sprintf("Subagents (%d)", len(items))
	m.list.Select(0)
}
