package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/inference-gateway/cli/internal/ui/shared"
)

// HelpBar displays keyboard shortcuts at the bottom of the screen
type HelpBar struct {
	enabled   bool
	width     int
	shortcuts []shared.KeyShortcut
}

func NewHelpBar() *HelpBar {
	return &HelpBar{
		enabled:   false,
		width:     80,
		shortcuts: make([]shared.KeyShortcut, 0),
	}
}

func (hb *HelpBar) SetShortcuts(shortcuts []shared.KeyShortcut) {
	hb.shortcuts = shortcuts
}

func (hb *HelpBar) IsEnabled() bool {
	return hb.enabled
}

func (hb *HelpBar) SetEnabled(enabled bool) {
	hb.enabled = enabled
}

func (hb *HelpBar) SetWidth(width int) {
	hb.width = width
}

func (hb *HelpBar) SetHeight(height int) {
	// Help bar has fixed height
}

func (hb *HelpBar) Render() string {
	if !hb.enabled || len(hb.shortcuts) == 0 {
		return ""
	}

	return hb.renderResponsiveTable()
}

// renderResponsiveTable creates a 4-row by 3-column grid layout for shortcuts
func (hb *HelpBar) renderResponsiveTable() string {
	if len(hb.shortcuts) == 0 {
		return ""
	}

	const rows = 4
	const cols = 3

	colWidth := (hb.width - 6) / cols
	if colWidth < 20 {
		colWidth = 20
	}

	grid := make([][]string, rows)
	for i := range grid {
		grid[i] = make([]string, cols)
	}

	var firstColumnKeys []shared.KeyShortcut
	var otherKeys []shared.KeyShortcut

	priorityKeys := []string{"!", "/", "@", "#"}
	for _, shortcut := range hb.shortcuts {
		isPriority := false
		for _, priority := range priorityKeys {
			if shortcut.Key == priority {
				firstColumnKeys = append(firstColumnKeys, shortcut)
				isPriority = true
				break
			}
		}
		if !isPriority {
			otherKeys = append(otherKeys, shortcut)
		}
	}

	for i, shortcut := range firstColumnKeys {
		if i >= rows {
			break
		}

		shortcutText := fmt.Sprintf("%s %s", shortcut.Key, shortcut.Description)

		if len(shortcutText) > colWidth-2 {
			shortcutText = shortcutText[:colWidth-5] + "..."
		}

		grid[i][0] = shortcutText
	}

	cellIndex := 0
	for _, shortcut := range otherKeys {
		for cellIndex < rows*cols {
			row := cellIndex / cols
			col := cellIndex % cols

			if col == 0 && row < len(firstColumnKeys) {
				cellIndex++
				continue
			}

			shortcutText := fmt.Sprintf("%s %s", shortcut.Key, shortcut.Description)

			if len(shortcutText) > colWidth-2 {
				shortcutText = shortcutText[:colWidth-5] + "..."
			}

			grid[row][col] = shortcutText
			cellIndex++
			break
		}

		if cellIndex >= rows*cols {
			break
		}
	}

	var tableRows []string
	for _, row := range grid {
		var cells []string
		for _, cell := range row {
			cellStyle := lipgloss.NewStyle().
				Width(colWidth).
				Align(lipgloss.Left)
			cells = append(cells, cellStyle.Render(cell))
		}
		tableRows = append(tableRows, lipgloss.JoinHorizontal(lipgloss.Left, cells...))
	}

	tableStyle := lipgloss.NewStyle().
		Foreground(shared.DimColor.GetLipglossColor()).
		Width(hb.width)

	return tableStyle.Render(strings.Join(tableRows, "\n"))
}

// Bubble Tea interface
func (hb *HelpBar) Init() tea.Cmd { return nil }

func (hb *HelpBar) View() string { return hb.Render() }

func (hb *HelpBar) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		hb.SetWidth(msg.Width)
	case shared.ToggleHelpBarMsg:
		hb.enabled = !hb.enabled
	case shared.HideHelpBarMsg:
		hb.enabled = false
	}
	return hb, nil
}
