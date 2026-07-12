package components

import (
	"image/color"
	"strings"

	key "charm.land/bubbles/v2/key"
	viewport "charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	table "charm.land/lipgloss/v2/table"

	domain "github.com/inference-gateway/cli/internal/domain"
	ui "github.com/inference-gateway/cli/internal/ui"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

// helpMaxTableWidth caps how wide the help tables grow on large terminals so
// the description columns stay readable instead of stretching edge to edge.
const helpMaxTableWidth = 100

// HelpCommand is a single slash-command row in the help overlay.
type HelpCommand struct {
	Name        string
	Description string
}

// HelpViewImpl is a full-screen, scrollable overlay documenting every available
// slash command and keybinding in two lipgloss tables. Both tables are sized to
// the terminal width - long descriptions wrap rather than truncate - and the
// whole view lives inside a viewport, so every row stays reachable even on a
// narrow or short terminal. It is read-only: esc/q returns to the chat.
type HelpViewImpl struct {
	width         int
	height        int
	themeService  domain.ThemeService
	styleProvider *styles.Provider
	viewport      viewport.Model
	commands      []HelpCommand
	keybindings   []ui.KeyShortcut
	cancelled     bool
}

// NewHelpView creates a new help overlay component.
func NewHelpView(themeService domain.ThemeService, styleProvider *styles.Provider) *HelpViewImpl {
	vp := viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	vp.SetContent("")

	return &HelpViewImpl{
		width:         80,
		height:        24,
		themeService:  themeService,
		styleProvider: styleProvider,
		viewport:      vp,
	}
}

func (h *HelpViewImpl) Init() tea.Cmd { return nil }

// SetContent loads the rows to display, rebuilds the rendered tables and
// resets the scroll position to the top.
func (h *HelpViewImpl) SetContent(commands []HelpCommand, keybindings []ui.KeyShortcut) {
	h.commands = commands
	h.keybindings = keybindings
	h.rebuild()
	h.viewport.GotoTop()
}

// Reset clears the cancelled flag and scroll position for reuse.
func (h *HelpViewImpl) Reset() {
	h.cancelled = false
	h.viewport.GotoTop()
}

// IsCancelled reports whether the user dismissed the help overlay.
func (h *HelpViewImpl) IsCancelled() bool { return h.cancelled }

// SetWidth sets the overlay width and rebuilds the tables to fit. Rebuilding is
// skipped when the width is unchanged so steady-state renders stay cheap.
func (h *HelpViewImpl) SetWidth(width int) {
	if width == h.width {
		return
	}
	h.width = width
	h.viewport.SetWidth(width)
	h.rebuild()
}

// SetHeight sets the overlay height, reserving the bottom two lines for the
// footer hint. The rendered tables depend only on width, so changing the height
// just resizes the viewport window - no rebuild required.
func (h *HelpViewImpl) SetHeight(height int) {
	h.height = height
	h.viewport.SetHeight(max(height-2, 1))
}

func (h *HelpViewImpl) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		h.SetWidth(msg.Width)
		h.SetHeight(msg.Height)
		return h, nil
	case tea.KeyPressMsg:
		return h.handleKey(msg)
	}

	var cmd tea.Cmd
	h.viewport, cmd = h.viewport.Update(msg)
	return h, cmd
}

func (h *HelpViewImpl) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, helpViewKeys.dismiss):
		h.cancelled = true
	case key.Matches(msg, helpViewKeys.navUp):
		h.viewport.ScrollUp(1)
	case key.Matches(msg, helpViewKeys.navDown):
		h.viewport.ScrollDown(1)
	case key.Matches(msg, helpViewKeys.pgUp):
		h.viewport.PageUp()
	case key.Matches(msg, helpViewKeys.pgDown):
		h.viewport.PageDown()
	case key.Matches(msg, helpViewKeys.top):
		h.viewport.GotoTop()
	case key.Matches(msg, helpViewKeys.bottom):
		h.viewport.GotoBottom()
	default:
		var cmd tea.Cmd
		h.viewport, cmd = h.viewport.Update(msg)
		return h, cmd
	}
	return h, nil
}

func (h *HelpViewImpl) View() tea.View {
	return tea.NewView(h.viewContent())
}

func (h *HelpViewImpl) viewContent() string {
	dim := h.styleProvider.GetThemeColor("dim")

	var b strings.Builder
	b.WriteString(h.viewport.View())
	b.WriteString("\n")
	b.WriteString(h.styleProvider.RenderWithColor(
		"↑/↓ scroll · g/G top/bottom · esc to close", dim))
	return b.String()
}

// rebuild renders both tables into the viewport content.
func (h *HelpViewImpl) rebuild() {
	accent := lipgloss.Color(h.styleProvider.GetThemeColor("accent"))
	dim := lipgloss.Color(h.styleProvider.GetThemeColor("dim"))
	border := lipgloss.Color(h.styleProvider.GetThemeColor("border"))

	width := h.viewport.Width()
	if width > helpMaxTableWidth {
		width = helpMaxTableWidth
	}
	if width < 1 {
		width = 1
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(accent)
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(accent)
	subtitleStyle := lipgloss.NewStyle().Foreground(dim)

	var b strings.Builder
	b.WriteString(titleStyle.Render("Help"))
	b.WriteString("\n")
	b.WriteString(subtitleStyle.Render("Slash commands and keyboard shortcuts"))
	b.WriteString("\n\n")

	b.WriteString(sectionStyle.Render("Commands"))
	b.WriteString("\n")
	b.WriteString(h.renderCommandsTable(width, accent, dim, border))
	b.WriteString("\n\n")

	b.WriteString(sectionStyle.Render("Keybindings"))
	b.WriteString("\n")
	b.WriteString(h.renderKeybindingsTable(width, accent, dim, border))

	h.viewport.SetContent(b.String())
}

func (h *HelpViewImpl) renderCommandsTable(width int, accent, dim, border color.Color) string {
	rows := make([][2]string, 0, len(h.commands))
	for _, c := range h.commands {
		rows = append(rows, [2]string{"/" + c.Name, c.Description})
	}
	if len(rows) == 0 {
		rows = append(rows, [2]string{"-", "No commands available"})
	}
	return renderHelpTable(width, accent, dim, border, "Command", "Description", rows)
}

func (h *HelpViewImpl) renderKeybindingsTable(width int, accent, dim, border color.Color) string {
	rows := make([][2]string, 0, len(h.keybindings))
	for _, k := range h.keybindings {
		rows = append(rows, [2]string{k.Key, k.Description})
	}
	if len(rows) == 0 {
		rows = append(rows, [2]string{"-", "No keybindings available"})
	}
	return renderHelpTable(width, accent, dim, border, "Key", "Action", rows)
}

// renderHelpTable builds a themed two-column table that fits exactly into the
// given width. The first column hugs its content (commands and keys are short,
// so it never wastes space), and the second column absorbs the remaining width
// and wraps long descriptions rather than clipping them.
func renderHelpTable(width int, accent, dim, border color.Color, h0, h1 string, rows [][2]string) string {
	// Cell padding is one column on each side; the rounded border takes three
	// columns total (left edge, middle divider, right edge).
	const cellPadding = 2
	const borderCols = 3

	firstContent := lipgloss.Width(h0)
	for _, r := range rows {
		if w := lipgloss.Width(r[0]); w > firstContent {
			firstContent = w
		}
	}

	firstCol := firstContent + cellPadding
	if maxFirst := width / 3; firstCol > maxFirst {
		firstCol = maxFirst
	}
	secondCol := max(width-borderCols-firstCol, 12)

	header := lipgloss.NewStyle().Bold(true).Foreground(accent)
	keyCell := lipgloss.NewStyle().Foreground(accent)
	descCell := lipgloss.NewStyle().Foreground(dim)

	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(border)).
		Headers(h0, h1).
		StyleFunc(func(row, col int) lipgloss.Style {
			style := descCell
			switch {
			case row == table.HeaderRow:
				style = header
			case col == 0:
				style = keyCell
			}
			if col == 0 {
				return style.Width(firstCol).Padding(0, 1)
			}
			return style.Width(secondCol).Padding(0, 1)
		})

	for _, r := range rows {
		t.Row(r[0], r[1])
	}
	return t.Render()
}
