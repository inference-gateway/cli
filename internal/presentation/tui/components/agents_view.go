package components

import (
	"cmp"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	key "charm.land/bubbles/v2/key"
	list "charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	styles "github.com/inference-gateway/cli/internal/presentation/tui/styles"
)

// Row scopes for the agents list: every row, or the /a2a pre-filter to a2a
// rows only.
const (
	AgentsScopeAll = ""
	AgentsScopeA2A = "a2a"
)

// Row kinds, shown as type chips.
const (
	agentKindLocal = "local"
	agentKindA2A   = "a2a"
)

// SubagentCatalog supplies the Markdown-defined subagent presets
// (.infer/agents/*.md) for the agents listing.
type SubagentCatalog interface {
	ListMarkdownSubagents() []agentdomain.SubagentInfo
}

// agentItem is a single row in the agents list.
type agentItem struct {
	kind   string
	name   string
	state  string
	failed bool
	detail string
}

// FilterValue is what the list filters against when the user searches (/).
// The kind prefix lets the user filter to a type by typing it.
func (i agentItem) FilterValue() string { return i.kind + " " + i.name }

// agentDelegate renders an agentItem on two rows: the name with its type chip
// and state, then its detail line dimmed.
type agentDelegate struct {
	styleProvider *styles.Provider
}

func (d agentDelegate) Height() int                             { return 2 }
func (d agentDelegate) Spacing() int                            { return 0 }
func (d agentDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d agentDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	it, ok := item.(agentItem)
	if !ok {
		return
	}

	selected := index == m.Index()

	prefix := "  "
	if selected {
		prefix = "▶ "
	}

	name := prefix + it.name
	if selected {
		name = d.styleProvider.RenderWithColor(name, d.styleProvider.GetThemeColor("accent"))
	}

	kind := d.styleProvider.RenderWithColor(" ["+it.kind+"]", d.styleProvider.GetThemeColor("dim"))

	stateColor := d.styleProvider.GetThemeColor("status")
	if it.failed {
		stateColor = d.styleProvider.GetThemeColor("error")
	}
	state := d.styleProvider.RenderWithColor(" ["+it.state+"]", stateColor)

	detail := it.detail
	if detail != "" {
		detail = d.styleProvider.RenderWithColor("      "+detail, d.styleProvider.GetThemeColor("dim"))
	}

	_, _ = fmt.Fprint(w, name+kind+state+"\n"+detail)
}

// subagentCapabilities summarizes the preset: the tool allowlist (or the
// inherited parent set), the model (or inherit) and where the file lives.
func subagentCapabilities(info agentdomain.SubagentInfo) string {
	tools := "all tools (inherited)"
	if len(info.Tools) > 0 {
		tools = strings.Join(info.Tools, ", ")
	}
	return fmt.Sprintf("tools: %s | model: %s | %s", tools, cmp.Or(info.Model, "inherit"), info.Source)
}

// AgentsView is a read-only, filterable list of every agent the chat can use:
// local Markdown presets (.infer/agents/*.md) and remote A2A agents. The /a2a
// alias opens it pre-filtered to the a2a rows.
type AgentsView struct {
	list          list.Model
	width         int
	height        int
	cancelled     bool
	readiness     AgentReadiness
	catalog       SubagentCatalog
	scope         string
	styleProvider *styles.Provider
}

// NewAgentsView creates the agents list view. Items are populated by Reset on
// every entry because readiness changes while agents start up.
func NewAgentsView(readiness AgentReadiness, catalog SubagentCatalog, styleProvider *styles.Provider) *AgentsView {
	l := list.New(
		nil,
		agentDelegate{styleProvider: styleProvider},
		80, 24,
	)
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(true)
	l.DisableQuitKeybindings()
	l.Styles.Title = lipgloss.NewStyle().
		Foreground(lipgloss.Color(styleProvider.GetThemeColor("accent"))).
		Bold(true)

	m := &AgentsView{
		list:          l,
		width:         80,
		height:        24,
		readiness:     readiness,
		catalog:       catalog,
		styleProvider: styleProvider,
	}
	m.Reset(AgentsScopeAll)
	return m
}

func (m *AgentsView) Init() tea.Cmd { return nil }

func (m *AgentsView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
	case tui.AgentStatusUpdateEvent:
		return m, m.refreshItems()
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

// handleKey intercepts the cancel keys when the list is not actively
// filtering; otherwise it lets the list own typing, enter (apply filter) and
// esc (clear filter). Enter outside filtering is consumed as a no-op.
func (m *AgentsView) handleKey(msg tea.KeyPressMsg) (handled bool, cmd tea.Cmd) {
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

func (m *AgentsView) View() tea.View {
	return tea.NewView(m.list.View())
}

// IsCancelled returns true once the user has dismissed the view.
func (m *AgentsView) IsCancelled() bool { return m.cancelled }

// SetWidth sets the width of the agents view.
func (m *AgentsView) SetWidth(width int) {
	m.width = width
	m.list.SetSize(width, m.height)
}

// SetHeight sets the height of the agents view.
func (m *AgentsView) SetHeight(height int) {
	m.height = height
	m.list.SetSize(m.width, height)
}

// Reset returns the view to its initial state and rebuilds the items within
// the given row scope.
func (m *AgentsView) Reset(scope string) {
	m.cancelled = false
	m.scope = scope
	m.list.ResetFilter()
	m.refreshItems()
	m.list.Select(0)
}

// refreshItems rebuilds the rows and title without touching the user's
// selection or filter, so the open view stays live while agents pull and
// start (AgentStatusUpdateEvent).
func (m *AgentsView) refreshItems() tea.Cmd {
	items := m.agentItems()
	cmd := m.list.SetItems(items)
	m.list.Title = m.listTitle(len(items))
	return cmd
}

func (m *AgentsView) listTitle(count int) string {
	if m.scope == AgentsScopeA2A {
		ready, total := 0, 0
		if r := m.readinessState(); r != nil {
			ready, total = r.ReadyAgents, r.TotalAgents
		}
		return fmt.Sprintf("A2A Agents (%d/%d ready)", ready, total)
	}
	return fmt.Sprintf("Agents (%d)", count)
}

// readinessState is the nil-safe readiness lookup.
func (m *AgentsView) readinessState() *tui.AgentReadinessState {
	if m.readiness == nil {
		return nil
	}
	return m.readiness.GetAgentReadiness()
}

// agentItems builds the merged rows: remote a2a agents from the readiness
// state and local Markdown presets from the catalog, sorted by name for a
// stable order.
func (m *AgentsView) agentItems() []list.Item {
	items := make([]list.Item, 0)

	if readiness := m.readinessState(); readiness != nil {
		for _, name := range slices.Sorted(maps.Keys(readiness.Agents)) {
			status := readiness.Agents[name]
			if status == nil {
				continue
			}
			item := agentItem{
				kind:   agentKindA2A,
				name:   cmp.Or(status.Name, name),
				state:  status.State.DisplayName(),
				failed: status.State == agentdomain.AgentStateFailed,
			}
			item.detail = cmp.Or(status.Error, status.Message, status.URL)
			item.detail = strings.Join(strings.Fields(item.detail), " ")
			if status.State == agentdomain.AgentStatePullingImage && status.LayersTotal > 0 {
				item.detail = fmt.Sprintf("%s (%d/%d layers)", item.detail, status.LayersDone, status.LayersTotal)
			}
			items = append(items, item)
		}
	}

	if m.scope != AgentsScopeA2A && m.catalog != nil {
		for _, info := range m.catalog.ListMarkdownSubagents() {
			state := "read-write"
			if info.ReadOnly {
				state = "read-only"
			}
			items = append(items, agentItem{
				kind:   agentKindLocal,
				name:   info.Name,
				state:  state,
				detail: subagentCapabilities(info),
			})
		}
	}

	slices.SortFunc(items, func(a, b list.Item) int {
		return strings.Compare(a.(agentItem).name, b.(agentItem).name)
	})
	return items
}
