package components

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	key "charm.land/bubbles/v2/key"
	list "charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	ansi "github.com/charmbracelet/x/ansi"

	formatting "github.com/inference-gateway/cli/internal/platform/formatting"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	styles "github.com/inference-gateway/cli/internal/presentation/tui/styles"
)

// The artifacts view's own keys: o saves and opens, r lists again.
var (
	artifactOpenKey   = key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "save and open"))
	artifactReloadKey = key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "reload"))
	artifactSaveKey   = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "save here"))
)

// artifactItem is one artifact of the session.
type artifactItem struct {
	artifact tui.Artifact
}

// FilterValue lets the user filter by name (/).
func (i artifactItem) FilterValue() string { return i.artifact.Name }

// artifactDelegate renders an artifact on one row: its name, size and
// modification time.
type artifactDelegate struct {
	styleProvider *styles.Provider
}

func (d artifactDelegate) Height() int                             { return 1 }
func (d artifactDelegate) Spacing() int                            { return 0 }
func (d artifactDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d artifactDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	it, ok := item.(artifactItem)
	if !ok {
		return
	}
	color := d.styleProvider.GetThemeColor
	name := "  " + it.artifact.Name
	if index == m.Index() {
		name = d.styleProvider.RenderWithColor("▶ "+it.artifact.Name, color("accent"))
	}
	details := d.styleProvider.RenderWithColor(
		fmt.Sprintf("%s  %s", formatting.FormatBytes(it.artifact.Size), it.artifact.ModifiedAt.Local().Format("2006-01-02 15:04")),
		color("dim"))
	_, _ = fmt.Fprint(w, ansi.Truncate(name+"  "+details, max(m.Width()-1, 8), "…"))
}

// ArtifactsView lists the current session's artifacts from the workspace it
// runs on, newest first. Enter saves the selected one on this machine and o
// opens it with the system viewer too.
type ArtifactsView struct {
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

// NewArtifactsView builds the view. load lists the artifacts and answers
// with a tui.ArtifactsLoadedEvent.
func NewArtifactsView(styleProvider *styles.Provider, load func() tea.Cmd) *ArtifactsView {
	l := list.New(nil, artifactDelegate{styleProvider: styleProvider}, 80, 24)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(true)
	l.DisableQuitKeybindings()
	l.Styles.Title = toolsTitleStyle(styleProvider)
	l.AdditionalShortHelpKeys = func() []key.Binding { return []key.Binding{artifactSaveKey, artifactOpenKey, artifactReloadKey} }
	return &ArtifactsView{list: l, width: 80, height: 24, load: load, styleProvider: styleProvider}
}

// Reset opens the view for the session at placement and starts listing.
func (m *ArtifactsView) Reset(placement tui.SessionPlacement) tea.Cmd {
	m.placement = placement
	m.cancelled = false
	m.hint = ""
	m.list.ResetFilter()
	return m.reload()
}

func (m *ArtifactsView) reload() tea.Cmd {
	m.loading, m.err = true, nil
	m.list.Title = m.title()
	if m.load == nil {
		return nil
	}
	return m.load()
}

func (m *ArtifactsView) Init() tea.Cmd { return nil }

func (m *ArtifactsView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.SetWidth(msg.Width)
		m.SetHeight(msg.Height)
		return m, nil
	case tui.ArtifactsLoadedEvent:
		return m, m.loaded(msg)
	case tui.ArtifactSavedEvent:
		m.saved(msg)
		return m, nil
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

// loaded shows a listing, keeping the cursor where it was.
func (m *ArtifactsView) loaded(event tui.ArtifactsLoadedEvent) tea.Cmd {
	m.loading, m.err = false, event.Err
	items := make([]list.Item, 0, len(event.Artifacts))
	for _, artifact := range event.Artifacts {
		items = append(items, artifactItem{artifact: artifact})
	}
	cmd := m.list.SetItems(items)
	m.list.Title = m.title()
	return cmd
}

// saved shows where the artifact landed, or why it did not, under the list.
func (m *ArtifactsView) saved(event tui.ArtifactSavedEvent) {
	switch {
	case event.Err != nil:
		m.hint = "Could not save " + event.Name + ": " + event.Err.Error()
	case event.Open:
		m.hint = "✓ Saved and opened " + homeTilde(event.Path)
	default:
		m.hint = "✓ Saved " + homeTilde(event.Path)
	}
	m.list.Title = m.title()
}

// homeTilde shortens a path under the home dir to ~/..., so it reads at a glance.
func homeTilde(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if rest, ok := strings.CutPrefix(path, home+string(os.PathSeparator)); ok {
		return "~/" + rest
	}
	return path
}

func (m *ArtifactsView) title() string {
	switch {
	case m.loading:
		return "Artifacts · listing " + spinnerFrame(time.Now(), SpinnerFrameInterval)
	case m.err != nil:
		return "Could not list artifacts: " + m.err.Error() + " · r retries"
	case len(m.list.Items()) == 0:
		return "No artifacts yet" + m.where()
	}
	return fmt.Sprintf("Artifacts (%d)", len(m.list.Items())) + m.where()
}

// where names the workspace the session runs on, nothing for an in-process chat.
func (m *ArtifactsView) where() string {
	if m.placement.Where() == "" {
		return ""
	}
	return " · session on " + m.placement.Where()
}

// handleKey owns the view's keys unless the list is filtering: enter saves
// the artifact here, o saves and opens it, r lists again, esc and ctrl+c
// close. A hint shows in the title until the next key.
func (m *ArtifactsView) handleKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
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
	case key.Matches(msg, artifactReloadKey):
		return true, m.reload()
	case key.Matches(msg, artifactSaveKey):
		return true, m.choose(false)
	case key.Matches(msg, artifactOpenKey):
		return true, m.choose(true)
	}
	return false, nil
}

// choose asks to save the selected artifact here, opening it when asked.
func (m *ArtifactsView) choose(open bool) tea.Cmd {
	it, ok := m.list.SelectedItem().(artifactItem)
	if !ok {
		return nil
	}
	if m.loading {
		m.hint = "Still listing · wait a moment"
		m.list.Title = m.title()
		return nil
	}
	m.hint = "Saving " + it.artifact.Name + "…"
	m.list.Title = m.title()
	name := it.artifact.Name
	return func() tea.Msg { return tui.ArtifactChosenEvent{Name: name, Open: open} }
}

// View is the list, with the last save's outcome word-wrapped under it so a
// long path stays readable.
func (m *ArtifactsView) View() tea.View {
	hint := m.hintLines()
	m.list.SetSize(m.width, m.height-len(hint))
	if len(hint) == 0 {
		return tea.NewView(m.list.View())
	}
	color := m.styleProvider.GetThemeColor("success")
	if strings.HasPrefix(m.hint, "Could not") {
		color = m.styleProvider.GetThemeColor("error")
	}
	return tea.NewView(m.list.View() + "\n" + m.styleProvider.RenderWithColor(strings.Join(hint, "\n"), color))
}

// hintLines wraps the hint to the view's width, at path separators too.
func (m *ArtifactsView) hintLines() []string {
	if m.hint == "" {
		return nil
	}
	return strings.Split(ansi.Wordwrap(m.hint, max(m.width-2, 8), "/"), "\n")
}

// IsCancelled reports whether the view closed.
func (m *ArtifactsView) IsCancelled() bool { return m.cancelled }

// IsLoading reports whether the view is listing, which animates its title.
func (m *ArtifactsView) IsLoading() bool { return m.loading }

// SetWidth sets the width of the view.
func (m *ArtifactsView) SetWidth(width int) {
	m.width = width
	m.list.SetSize(width, m.height)
}

// SetHeight sets the height of the view.
func (m *ArtifactsView) SetHeight(height int) {
	m.height = height
	m.list.SetSize(m.width, height)
}
