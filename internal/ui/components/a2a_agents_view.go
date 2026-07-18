package components

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	key "charm.land/bubbles/v2/key"
	list "charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"

	domain "github.com/inference-gateway/cli/internal/domain"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

// a2aAgentItem is a single row in the A2A agents list.
type a2aAgentItem struct {
	name   string
	url    string
	state  string
	failed bool
	detail string
}

// FilterValue is what the list filters against when the user searches (/).
func (i a2aAgentItem) FilterValue() string { return i.name }

// a2aAgentDelegate renders an a2aAgentItem as a single line: a caret on the
// highlighted row, the agent name, its state (error-colored on failure), and
// the URL dimmed.
type a2aAgentDelegate struct {
	styleProvider *styles.Provider
}

func (d a2aAgentDelegate) Height() int                             { return 1 }
func (d a2aAgentDelegate) Spacing() int                            { return 0 }
func (d a2aAgentDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d a2aAgentDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	it, ok := item.(a2aAgentItem)
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

	stateColor := d.styleProvider.GetThemeColor("status")
	if it.failed {
		stateColor = d.styleProvider.GetThemeColor("error")
	}
	state := d.styleProvider.RenderWithColor(" ["+it.state+"]", stateColor)

	detail := it.detail
	if detail == "" {
		detail = it.url
	}
	if detail != "" {
		detail = d.styleProvider.RenderWithColor(" - "+detail, d.styleProvider.GetThemeColor("dim"))
	}

	_, _ = fmt.Fprint(w, name+state+detail)
}

// A2AAgentsViewImpl is a read-only, filterable list of the registered A2A
// agents and their readiness. Like the tools view it is display-only for now.
type A2AAgentsViewImpl struct {
	list          list.Model
	width         int
	height        int
	cancelled     bool
	stateManager  domain.AgentReadinessManager
	styleProvider *styles.Provider
}

// NewA2AAgentsView creates the A2A agents list view. Items are populated by
// Reset on every entry because agent readiness changes as agents start up.
func NewA2AAgentsView(stateManager domain.AgentReadinessManager, styleProvider *styles.Provider) *A2AAgentsViewImpl {
	l := list.New(
		nil,
		a2aAgentDelegate{styleProvider: styleProvider},
		80, 24,
	)
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(true)
	l.DisableQuitKeybindings()
	l.Styles.Title = lipgloss.NewStyle().
		Foreground(lipgloss.Color(styleProvider.GetThemeColor("accent"))).
		Bold(true)

	m := &A2AAgentsViewImpl{
		list:          l,
		width:         80,
		height:        24,
		stateManager:  stateManager,
		styleProvider: styleProvider,
	}
	m.Reset()
	return m
}

// agentItems builds the list items from the current agent readiness state,
// sorted by name for a stable order.
func (m *A2AAgentsViewImpl) agentItems() ([]list.Item, int, int) {
	if m.stateManager == nil {
		return nil, 0, 0
	}

	readiness := m.stateManager.GetAgentReadiness()
	if readiness == nil {
		return nil, 0, 0
	}

	items := make([]list.Item, 0, len(readiness.Agents))
	for _, name := range slices.Sorted(maps.Keys(readiness.Agents)) {
		status := readiness.Agents[name]
		if status == nil {
			continue
		}
		item := a2aAgentItem{
			name:   status.Name,
			url:    status.URL,
			state:  status.State.DisplayName(),
			failed: status.State == domain.AgentStateFailed,
		}
		if item.name == "" {
			item.name = name
		}
		if status.Error != "" {
			item.detail = status.Error
		} else if status.Message != "" {
			item.detail = status.Message
		}
		item.detail = strings.Join(strings.Fields(item.detail), " ")
		if status.State == domain.AgentStatePullingImage && status.LayersTotal > 0 {
			item.detail = fmt.Sprintf("%s (%d/%d layers)", item.detail, status.LayersDone, status.LayersTotal)
		}
		items = append(items, item)
	}
	return items, readiness.ReadyAgents, readiness.TotalAgents
}

func (m *A2AAgentsViewImpl) Init() tea.Cmd { return nil }

func (m *A2AAgentsViewImpl) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
	case domain.AgentStatusUpdateEvent:
		return m, m.refreshItems()
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

// handleKey intercepts the cancel keys when the list is not actively
// filtering; otherwise it lets the list own typing, enter (apply filter) and
// esc (clear filter). Enter outside filtering is consumed as a no-op.
func (m *A2AAgentsViewImpl) handleKey(msg tea.KeyPressMsg) (handled bool, cmd tea.Cmd) {
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

func (m *A2AAgentsViewImpl) View() tea.View {
	return tea.NewView(m.list.View())
}

// IsCancelled returns true once the user has dismissed the view.
func (m *A2AAgentsViewImpl) IsCancelled() bool { return m.cancelled }

// SetWidth sets the width of the agents view.
func (m *A2AAgentsViewImpl) SetWidth(width int) {
	m.width = width
	m.list.SetSize(width, m.height)
}

// SetHeight sets the height of the agents view.
func (m *A2AAgentsViewImpl) SetHeight(height int) {
	m.height = height
	m.list.SetSize(m.width, height)
}

// Reset returns the view to its initial state and rebuilds the items so the
// list reflects the latest agent readiness.
func (m *A2AAgentsViewImpl) Reset() {
	m.cancelled = false
	m.list.ResetFilter()
	m.refreshItems()
	m.list.Select(0)
}

// refreshItems rebuilds the rows and title from the latest agent readiness
// without touching the user's selection or filter, so the open view stays
// live while agents pull and start (AgentStatusUpdateEvent).
func (m *A2AAgentsViewImpl) refreshItems() tea.Cmd {
	items, ready, total := m.agentItems()
	cmd := m.list.SetItems(items)
	m.list.Title = fmt.Sprintf("A2A Agents (%d/%d ready)", ready, total)
	return cmd
}
