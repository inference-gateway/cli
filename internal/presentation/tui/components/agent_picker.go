package components

import (
	"slices"
	"strings"

	lipgloss "charm.land/lipgloss/v2"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	styles "github.com/inference-gateway/cli/internal/presentation/tui/styles"
	icons "github.com/inference-gateway/cli/internal/presentation/tui/styles/icons"
)

// agentPickerMaxInner caps the dropdown's content width, so a wide terminal
// does not stretch the descriptions into one unreadable line.
const agentPickerMaxInner = 100

// agentPickerChrome is the width the dropdown's margin, border and padding take.
const agentPickerChrome = 5

const (
	agentPickerTitle = "Switch agent"
	agentPickerHint  = "the next message runs as it"
	agentPickerKeys  = "↑/↓ move · enter switch · esc close"
	agentCursor      = "❯"
	readOnlyBadge    = "read-only"
	readWriteBadge   = "read-write"
)

// AgentPicker is the dropdown under the status row that switches the main
// agent a chat runs as. It lists the Markdown agents with the default main
// agent first, marks the one the chat runs as, and opens with it selected.
type AgentPicker struct {
	styleProvider *styles.Provider
	styles        agentPickerStyles
	agents        []agentdomain.SubagentInfo
	current       string
	selected      int
	open          bool
	width         int
}

// agentPickerStyles are built when the picker opens, so a theme switch
// carries over without restyling every frame.
type agentPickerStyles struct {
	box, title, faint, cursor, current, selected, readOnly, readWrite lipgloss.Style
}

func newAgentPickerStyles(provider *styles.Provider) agentPickerStyles {
	accent := lipgloss.Color(provider.GetThemeColor("accent"))
	dim := lipgloss.Color(provider.GetThemeColor("dim"))
	success := lipgloss.Color(provider.GetThemeColor("success"))
	warning := lipgloss.Color(provider.GetThemeColor("warning"))
	return agentPickerStyles{
		box:       lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent).Padding(0, 1).MarginLeft(1),
		title:     lipgloss.NewStyle().Bold(true).Foreground(accent),
		faint:     lipgloss.NewStyle().Foreground(dim),
		cursor:    lipgloss.NewStyle().Bold(true).Foreground(accent),
		current:   lipgloss.NewStyle().Foreground(success),
		selected:  lipgloss.NewStyle().Bold(true).Foreground(accent),
		readOnly:  lipgloss.NewStyle().Foreground(success),
		readWrite: lipgloss.NewStyle().Foreground(warning),
	}
}

// NewAgentPicker creates a closed agent picker.
func NewAgentPicker(styleProvider *styles.Provider) *AgentPicker {
	return &AgentPicker{styleProvider: styleProvider, width: 80}
}

// Open lists agents, the default main agent first, with current selected.
func (p *AgentPicker) Open(agents []agentdomain.SubagentInfo, current string) {
	p.agents = slices.Clone(agents)
	slices.SortStableFunc(p.agents, func(a, b agentdomain.SubagentInfo) int {
		return mainAgentFirst(a.Name) - mainAgentFirst(b.Name)
	})
	p.current = current
	p.selected = max(0, slices.IndexFunc(p.agents, func(a agentdomain.SubagentInfo) bool { return a.Name == current }))
	p.open = len(p.agents) > 0
	if p.styleProvider != nil {
		p.styles = newAgentPickerStyles(p.styleProvider)
	}
}

func mainAgentFirst(name string) int {
	if name == agentdomain.DefaultMainAgentName {
		return 0
	}
	return 1
}

// Close hides the picker.
func (p *AgentPicker) Close() { p.open = false }

// IsOpen reports whether the picker shows.
func (p *AgentPicker) IsOpen() bool { return p.open }

// Move moves the selection by delta, wrapping around the list.
func (p *AgentPicker) Move(delta int) {
	if len(p.agents) == 0 {
		return
	}
	p.selected = (p.selected + delta + len(p.agents)) % len(p.agents)
}

// Selected is the name of the highlighted agent.
func (p *AgentPicker) Selected() string {
	if len(p.agents) == 0 {
		return ""
	}
	return p.agents[p.selected].Name
}

// SetWidth sets the width of the row the picker opens under.
func (p *AgentPicker) SetWidth(width int) { p.width = width }

// Render draws the open picker: a title, one aligned row per agent and the
// keys, in a rounded box under the status row. "" while closed.
func (p *AgentPicker) Render() string {
	if !p.open || p.styleProvider == nil {
		return ""
	}
	inner := min(max(p.width-agentPickerChrome, 24), agentPickerMaxInner)
	nameWidth := 0
	for _, agent := range p.agents {
		nameWidth = max(nameWidth, lipgloss.Width(agent.Name))
	}
	lines := []string{p.styles.title.Render(agentPickerTitle) + p.styles.faint.Render(" · "+agentPickerHint), ""}
	for i, agent := range p.agents {
		lines = append(lines, p.renderRow(i, agent, nameWidth, inner))
	}
	lines = append(lines, "", p.styles.faint.Render(agentPickerKeys))
	return p.styles.box.Render(strings.Join(lines, "\n"))
}

// renderRow is one agent: the cursor, a check on the agent the chat runs as,
// the name padded to the longest, its capability and as much of its
// description as fits.
func (p *AgentPicker) renderRow(i int, agent agentdomain.SubagentInfo, nameWidth, inner int) string {
	s := p.styles
	cursor, mark, name := "  ", "  ", lipgloss.NewStyle().Width(nameWidth)
	if i == p.selected {
		cursor, name = s.cursor.Render(agentCursor)+" ", s.selected.Width(nameWidth)
	}
	if agent.Name == p.current {
		mark = s.current.Render(icons.CheckMark) + " "
	}
	badge := s.readWrite.Width(len(readWriteBadge)).Render(readWriteBadge)
	if agent.ReadOnly {
		badge = s.readOnly.Width(len(readWriteBadge)).Render(readOnlyBadge)
	}
	row := cursor + mark + name.Render(agent.Name) + "  " + badge + "  "
	description := truncateRunes(agent.Description, max(inner-lipgloss.Width(row), 0))
	if i == p.selected {
		return row + description
	}
	return row + s.faint.Render(description)
}
