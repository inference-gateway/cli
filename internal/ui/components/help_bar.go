package components

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
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

	const rows = 11
	const cols = 3

	colWidth := (hb.width - 6) / cols
	if colWidth < 20 {
		colWidth = 20
	}

	// Create grid of shortcuts (not formatted text yet)
	grid := make([][]shared.KeyShortcut, rows)
	for i := range grid {
		grid[i] = make([]shared.KeyShortcut, cols)
	}

	var firstColumnKeys []shared.KeyShortcut
	var otherKeys []shared.KeyShortcut

	priorityKeys := []string{"!", "/", "@", "#", "ctrl+s", "shift+down", "shift+up", "pgdn/page_down", "pgup/page_up"}
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
		grid[i][0] = shortcut
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

			grid[row][col] = shortcut
			cellIndex++
			break
		}

		if cellIndex >= rows*cols {
			break
		}
	}

	colKeyWidths := make([]int, cols)
	for col := 0; col < cols; col++ {
		maxKeyWidth := 0
		for row := 0; row < rows; row++ {
			if grid[row][col].Key != "" {
				if len(grid[row][col].Key) > maxKeyWidth {
					maxKeyWidth = len(grid[row][col].Key)
				}
			}
		}
		colKeyWidths[col] = maxKeyWidth + 1
	}

	var tableRows []string
	for row := 0; row < rows; row++ {
		var cells []string
		for col := 0; col < cols; col++ {
			shortcut := grid[row][col]
			var cellText string

			if shortcut.Key != "" {
				keyWidth := colKeyWidths[col]
				padding := keyWidth - len(shortcut.Key)
				cellText = shortcut.Key + strings.Repeat(" ", padding) + shortcut.Description

				if len(cellText) > colWidth-2 {
					cellText = cellText[:colWidth-5] + "..."
				}
			}

			cellStyle := lipgloss.NewStyle().
				Width(colWidth).
				Align(lipgloss.Left)
			cells = append(cells, cellStyle.Render(cellText))
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
