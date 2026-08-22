package output

import (
	"fmt"

	glamour "charm.land/glamour/v2"
	lipgloss "charm.land/lipgloss/v2"
	table "charm.land/lipgloss/v2/table"

	utils "github.com/inference-gateway/cli/internal/platform/utils"
	styles "github.com/inference-gateway/cli/internal/presentation/tui/styles"
	colors "github.com/inference-gateway/cli/internal/presentation/tui/styles/colors"
	icons "github.com/inference-gateway/cli/internal/presentation/tui/styles/icons"
)

// TUICommandAnnotation marks commands whose terminal UI owns stdout.
const TUICommandAnnotation = "infer.tui"

// Renderer owns the styles shared by non-TUI command output.
type Renderer struct {
	titleStyle      lipgloss.Style
	labelStyle      lipgloss.Style
	hintStyle       lipgloss.Style
	tableHeaderCell lipgloss.Style
	tableBodyCell   lipgloss.Style
	tableBorderInk  lipgloss.Style
	disabled        bool
}

func NewRenderer() *Renderer {
	return &Renderer{
		titleStyle:      lipgloss.NewStyle().Bold(true).Foreground(colors.AccentColor.GetLipglossColor()),
		labelStyle:      lipgloss.NewStyle().Bold(true).Foreground(colors.HeaderColor.GetLipglossColor()),
		hintStyle:       lipgloss.NewStyle().Foreground(colors.DimColor.GetLipglossColor()),
		tableHeaderCell: lipgloss.NewStyle().Bold(true).Foreground(colors.AccentColor.GetLipglossColor()).Padding(0, 1),
		tableBodyCell:   lipgloss.NewStyle().Padding(0, 1),
		tableBorderInk:  lipgloss.NewStyle().Foreground(colors.BorderColor.GetLipglossColor()),
	}
}

func (r *Renderer) DisableColors() {
	r.disabled = true
	utils.SetColorsDisabled(true)
	r.titleStyle = lipgloss.NewStyle()
	r.labelStyle = lipgloss.NewStyle()
	r.hintStyle = lipgloss.NewStyle()
	r.tableHeaderCell = lipgloss.NewStyle().Padding(0, 1)
	r.tableBodyCell = lipgloss.NewStyle().Padding(0, 1)
	r.tableBorderInk = lipgloss.NewStyle()
	icons.CheckMarkStyle = lipgloss.NewStyle()
	icons.CrossMarkStyle = lipgloss.NewStyle()
}

func (r *Renderer) ColorsDisabled() bool { return r.disabled }

func (r *Renderer) NewListTable(headers ...string) *table.Table {
	return table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(r.tableBorderInk).
		Headers(headers...).
		StyleFunc(func(row, _ int) lipgloss.Style {
			if row == table.HeaderRow {
				return r.tableHeaderCell
			}
			return r.tableBodyCell
		})
}

func (r *Renderer) Title(title string) string { return r.titleStyle.Render(title) }

func (r *Renderer) Field(label, value string) string {
	return fmt.Sprintf("%s %s", r.labelStyle.Render(label+":"), value)
}

func (r *Renderer) Hint(text string) string { return r.hintStyle.Render(text) }

func (r *Renderer) StatusIcon(enabled bool) string {
	if enabled {
		return icons.CheckMark
	}
	return icons.CrossMark
}

func (r *Renderer) StatusLegend() string {
	return r.Hint(fmt.Sprintf("%s = enabled, %s = disabled", icons.CheckMark, icons.CrossMark))
}

func (r *Renderer) TraceTreeStyle() styles.TreeStyle {
	if r.disabled {
		return styles.TreeStyle{}
	}
	return styles.TreeStyle{
		Enumerator: r.hintStyle,
		Duration:   r.titleStyle,
		Error:      lipgloss.NewStyle().Foreground(colors.ErrorColor.GetLipglossColor()),
	}
}

func (r *Renderer) RenderMarkdown(markdown string) (string, error) {
	renderer, err := glamour.NewTermRenderer(glamour.WithWordWrap(0))
	if err != nil {
		return "", err
	}
	return renderer.Render(markdown)
}

func (r *Renderer) PrintMarkdown(markdown string) {
	if !r.disabled {
		if rendered, err := r.RenderMarkdown(markdown); err == nil {
			fmt.Print(rendered)
			return
		}
	}
	fmt.Println(markdown)
}
