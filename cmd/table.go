package cmd

import (
	"fmt"

	lipgloss "charm.land/lipgloss/v2"
	table "charm.land/lipgloss/v2/table"

	telemetry "github.com/inference-gateway/cli/internal/telemetry"
	colors "github.com/inference-gateway/cli/internal/ui/styles/colors"
	icons "github.com/inference-gateway/cli/internal/ui/styles/icons"
)

// Shared styling for non-TUI `infer ... list` command output. These draw from
// the static CLI palette (internal/ui/styles/colors) so list commands render
// consistently without needing an interactive theme session. lipgloss v2 does
// NOT degrade colour on a non-TTY stdout (it always emits the style's escape
// sequences), so disableOutputColors resets these vars when colour is off;
// the table borders still render as box-drawing characters either way.
var (
	listTitleStyle  = lipgloss.NewStyle().Bold(true).Foreground(colors.AccentColor.GetLipglossColor())
	listLabelStyle  = lipgloss.NewStyle().Bold(true).Foreground(colors.HeaderColor.GetLipglossColor())
	listHintStyle   = lipgloss.NewStyle().Foreground(colors.DimColor.GetLipglossColor())
	tableHeaderCell = lipgloss.NewStyle().Bold(true).Foreground(colors.AccentColor.GetLipglossColor()).Padding(0, 1)
	tableBodyCell   = lipgloss.NewStyle().Padding(0, 1)
	tableBorderInk  = lipgloss.NewStyle().Foreground(colors.BorderColor.GetLipglossColor())
)

// disableOutputColors strips every SGR-emitting attribute (colour and bold)
// from the shared command-output styles, keeping only layout (cell padding),
// so piped/redirected output and --no-colors runs contain no escape sequences.
// Must run before any command renders (wired into rootCmd.PersistentPreRun).
// outputColorsDisabled lets commands with non-lipgloss output (e.g. glamour
// markdown rendering in plans.go) follow the same decision.
var outputColorsDisabled bool

func disableOutputColors() {
	outputColorsDisabled = true
	listTitleStyle = lipgloss.NewStyle()
	listLabelStyle = lipgloss.NewStyle()
	listHintStyle = lipgloss.NewStyle()
	tableHeaderCell = lipgloss.NewStyle().Padding(0, 1)
	tableBodyCell = lipgloss.NewStyle().Padding(0, 1)
	tableBorderInk = lipgloss.NewStyle()
	traceTreeStyle = telemetry.TreeStyle{}
}

// newListTable returns a lipgloss table pre-styled for command-line list
// output: a rounded border in the palette's border colour, a bold accent
// header row and single-space cell padding. Columns size to their content.
// Callers add Row()/Rows() and call Render().
func newListTable(headers ...string) *table.Table {
	return table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(tableBorderInk).
		Headers(headers...).
		StyleFunc(func(row, _ int) lipgloss.Style {
			if row == table.HeaderRow {
				return tableHeaderCell
			}
			return tableBodyCell
		})
}

// listTitle renders a bold accent section title, e.g. "MCP Configuration".
func listTitle(title string) string {
	return listTitleStyle.Render(title)
}

// listField renders a "Label: value" line with a bold label.
func listField(label, value string) string {
	return fmt.Sprintf("%s %s", listLabelStyle.Render(label+":"), value)
}

// listHint renders dim, secondary text (counts, legends, pagination notes).
func listHint(text string) string {
	return listHintStyle.Render(text)
}

// statusIcon returns the check/cross icon for a boolean flag.
func statusIcon(enabled bool) string {
	if enabled {
		return icons.CheckMark
	}
	return icons.CrossMark
}

// statusLegend renders the standard "✓ = enabled, ✗ = disabled" footer.
func statusLegend() string {
	return listHint(fmt.Sprintf("%s = enabled, %s = disabled", icons.CheckMark, icons.CrossMark))
}
