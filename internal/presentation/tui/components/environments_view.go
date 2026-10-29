package components

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"time"

	key "charm.land/bubbles/v2/key"
	list "charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	ansi "github.com/charmbracelet/x/ansi"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	styles "github.com/inference-gateway/cli/internal/presentation/tui/styles"
	icons "github.com/inference-gateway/cli/internal/presentation/tui/styles/icons"
)

// environmentReloadKey reprobes the environments.
var environmentReloadKey = key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "reprobe"))

// environmentItem is one environment the session can run on.
type environmentItem struct {
	status  agentdomain.EnvironmentStatus
	current bool
	moving  bool
}

// FilterValue lets the user filter by name or kind (/).
func (i environmentItem) FilterValue() string { return i.status.Name + " " + string(i.status.Kind) }

// environmentDelegate renders an environment on two rows: its name with its
// kind, reachability and where the session is, then its details dimmed.
type environmentDelegate struct {
	styleProvider *styles.Provider
}

func (d environmentDelegate) Height() int                             { return 2 }
func (d environmentDelegate) Spacing() int                            { return 0 }
func (d environmentDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d environmentDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	it, ok := item.(environmentItem)
	if !ok {
		return
	}
	color := d.styleProvider.GetThemeColor
	name := "  " + it.status.Name
	if index == m.Index() {
		name = d.styleProvider.RenderWithColor("▶ "+it.status.Name, color("accent"))
	}
	kind := d.styleProvider.RenderWithColor("["+string(it.status.Kind)+"]", color("dim"))
	reach := d.styleProvider.RenderWithColor("[reachable]", color("status"))
	if !it.status.Reachable {
		reach = d.styleProvider.RenderWithColor("[unreachable]", color("error"))
	}
	row := name + " " + kind + " " + reach
	switch {
	case it.moving:
		row += " " + d.styleProvider.RenderWithColor(icons.MovingSession+" moving", color("warning"))
	case it.current:
		row += " " + d.styleProvider.RenderWithColor("● here", color("success"))
	}
	detailColor := color("dim")
	if it.status.Error != "" {
		detailColor = color("error")
	}
	detail := ansi.Truncate("      "+environmentDetail(it.status), max(m.Width()-1, 8), "…")
	_, _ = fmt.Fprint(w, row+"\n"+d.styleProvider.RenderWithColor(detail, detailColor))
}

// environmentDetail is an environment's error, or its capabilities sorted.
func environmentDetail(status agentdomain.EnvironmentStatus) string {
	if status.Error != "" {
		return status.Error
	}
	keys := slices.Sorted(maps.Keys(status.Capabilities))
	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, k+"="+status.Capabilities[k])
	}
	return strings.Join(pairs, "  ")
}

// EnvironmentsView picks the environment a daemon chat's session runs on:
// every environment the daemon knows with whether it is reachable and where
// the session is. Enter on another reachable one moves the session there.
type EnvironmentsView struct {
	list          list.Model
	width         int
	height        int
	placement     tui.SessionPlacement
	load          func() tea.Cmd
	loading       bool
	err           error
	hint          string
	cancelled     bool
	styleProvider *styles.Provider
}

// NewEnvironmentsView builds the picker. load lists the environments, and
// answers with a tui.EnvironmentsLoadedEvent.
func NewEnvironmentsView(styleProvider *styles.Provider, load func() tea.Cmd) *EnvironmentsView {
	l := list.New(nil, environmentDelegate{styleProvider: styleProvider}, 80, 24)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(true)
	l.DisableQuitKeybindings()
	l.Styles.Title = toolsTitleStyle(styleProvider)
	l.AdditionalShortHelpKeys = func() []key.Binding { return []key.Binding{environmentReloadKey} }
	return &EnvironmentsView{list: l, width: 80, height: 24, load: load, styleProvider: styleProvider}
}

// Reset opens the picker for the session at placement and starts listing.
func (m *EnvironmentsView) Reset(placement tui.SessionPlacement) tea.Cmd {
	m.placement = placement
	m.cancelled = false
	m.hint = ""
	m.list.ResetFilter()
	return m.reload()
}

func (m *EnvironmentsView) reload() tea.Cmd {
	m.loading, m.err = true, nil
	m.list.Title = m.title()
	if m.load == nil {
		return nil
	}
	return m.load()
}

func (m *EnvironmentsView) Init() tea.Cmd { return nil }

func (m *EnvironmentsView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.SetWidth(msg.Width)
		m.SetHeight(msg.Height)
		return m, nil
	case tui.EnvironmentsLoadedEvent:
		return m, m.loaded(msg)
	case tui.SessionPlacementChangedEvent:
		m.placement = msg.Placement
		return m, m.refreshItems()
	case tui.LiveTickEvent:
		m.list.Title = m.title()
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

// loaded shows a listing, keeping the cursor on the environment it was on.
func (m *EnvironmentsView) loaded(event tui.EnvironmentsLoadedEvent) tea.Cmd {
	m.loading, m.err = false, event.Err
	items := make([]list.Item, 0, len(event.Environments))
	for _, status := range event.Environments {
		items = append(items, environmentItem{status: status})
	}
	cmd := m.list.SetItems(items)
	refresh := m.refreshItems()
	return tea.Batch(cmd, refresh)
}

// refreshItems marks where the session runs and where it moves to.
func (m *EnvironmentsView) refreshItems() tea.Cmd {
	items := m.list.Items()
	for i, item := range items {
		it, ok := item.(environmentItem)
		if !ok {
			continue
		}
		it.current = it.status.Name == m.placement.Where()
		it.moving = it.status.Name == m.placement.Transferring
		items[i] = it
	}
	m.list.Title = m.title()
	return m.list.SetItems(items)
}

func (m *EnvironmentsView) title() string {
	switch {
	case m.hint != "":
		return m.hint
	case m.loading:
		return "Environments · probing " + spinnerFrame(time.Now(), SpinnerFrameInterval)
	case m.err != nil:
		return "Could not list environments: " + m.err.Error() + " · r retries"
	}
	return fmt.Sprintf("Environments (%d) · session on %s", len(m.list.Items()), m.placement.Where())
}

// handleKey owns the picker's keys unless the list is filtering: enter moves
// the session, r reprobes, esc and ctrl+c close. A refusal shows in the title
// until the next key.
func (m *EnvironmentsView) handleKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if m.list.FilterState() == list.Filtering {
		return false, nil
	}
	if m.hint != "" {
		m.hint = ""
		m.list.Title = m.title()
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
	case key.Matches(msg, environmentReloadKey):
		return true, m.reload()
	case key.Matches(msg, listViewKeys.selectKey):
		return true, m.choose()
	}
	return false, nil
}

// choose moves the session to the selected environment, or says why it
// cannot go there.
func (m *EnvironmentsView) choose() tea.Cmd {
	it, ok := m.list.SelectedItem().(environmentItem)
	if !ok {
		return nil
	}
	if refusal := m.refusal(it); refusal != "" {
		m.hint = refusal
		m.list.Title = m.title()
		return nil
	}
	m.cancelled = true
	target := it.status.Name
	return func() tea.Msg { return tui.TransferRequestedEvent{Target: target} }
}

// refusal is why the session cannot move to it, or "".
func (m *EnvironmentsView) refusal(it environmentItem) string {
	switch {
	case m.placement.Transferring != "":
		return "A transfer to " + m.placement.Transferring + " is already running · esc in the chat cancels it"
	case it.current:
		return "The session already runs on " + it.status.Name
	case m.placement.Remote() && it.status.Name != m.placement.Local:
		return "From " + m.placement.Environment + " the session moves only back to " + m.placement.Local
	case !it.status.Reachable:
		return it.status.Name + " is unreachable · r reprobes"
	}
	return ""
}

func (m *EnvironmentsView) View() tea.View {
	return tea.NewView(m.list.View())
}

// IsCancelled reports whether the picker closed.
func (m *EnvironmentsView) IsCancelled() bool { return m.cancelled }

// IsLoading reports whether the picker is probing, which animates its title.
func (m *EnvironmentsView) IsLoading() bool { return m.loading }

// SetWidth sets the width of the picker.
func (m *EnvironmentsView) SetWidth(width int) {
	m.width = width
	m.list.SetSize(width, m.height)
}

// SetHeight sets the height of the picker.
func (m *EnvironmentsView) SetHeight(height int) {
	m.height = height
	m.list.SetSize(m.width, height)
}
