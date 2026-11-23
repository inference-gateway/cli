package components

import (
	"slices"
	"strings"

	lipgloss "charm.land/lipgloss/v2"

	styles "github.com/inference-gateway/cli/internal/presentation/tui/styles"
	icons "github.com/inference-gateway/cli/internal/presentation/tui/styles/icons"
)

// pickerMaxInner caps the dropdown's content width, so a wide terminal does
// not stretch the details into one unreadable line.
const pickerMaxInner = 100

// pickerMinInner keeps the dropdown readable in a narrow row.
const pickerMinInner = 24

// pickerChrome is the width the dropdown's margin, border and padding take.
const pickerChrome = 5

const pickerCursor = "❯"

// PickerSpec is what a dropdown says about itself: its title, a hint after the
// title, and the keys it takes.
type PickerSpec struct {
	Title string
	Hint  string
	Keys  string
}

// PickerRow is one choice of a dropdown: the value it picks, its label, an
// optional tag in a theme color such as "success", and its detail.
type PickerRow struct {
	Value    string
	Label    string
	Tag      string
	TagColor string
	Detail   string
}

// Picker is a dropdown under the status row, such as the agent or the room
// switcher. It marks the current value with a check and opens with it
// selected. The rows keep the order they are given in.
type Picker struct {
	styleProvider *styles.Provider
	styles        pickerStyles
	spec          PickerSpec
	rows          []PickerRow
	current       string
	selected      int
	open          bool
	width         int
}

// pickerStyles are built when the picker opens, so a theme switch carries over
// without restyling every frame.
type pickerStyles struct {
	box, title, faint, cursor, current, selected lipgloss.Style
}

func newPickerStyles(provider *styles.Provider) pickerStyles {
	accent := lipgloss.Color(provider.GetThemeColor("accent"))
	dim := lipgloss.Color(provider.GetThemeColor("dim"))
	success := lipgloss.Color(provider.GetThemeColor("success"))
	return pickerStyles{
		box:      lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent).Padding(0, 1).MarginLeft(1),
		title:    lipgloss.NewStyle().Bold(true).Foreground(accent),
		faint:    lipgloss.NewStyle().Foreground(dim),
		cursor:   lipgloss.NewStyle().Bold(true).Foreground(accent),
		current:  lipgloss.NewStyle().Foreground(success),
		selected: lipgloss.NewStyle().Bold(true).Foreground(accent),
	}
}

// NewPicker creates a closed picker.
func NewPicker(styleProvider *styles.Provider) *Picker {
	return &Picker{styleProvider: styleProvider, width: 80}
}

// Open lists rows under spec with the row of current selected. With no rows
// the picker stays closed.
func (p *Picker) Open(spec PickerSpec, rows []PickerRow, current string) {
	p.spec = spec
	p.rows = slices.Clone(rows)
	p.current = current
	p.selected = max(0, slices.IndexFunc(p.rows, func(row PickerRow) bool { return row.Value == current }))
	p.open = len(p.rows) > 0
	if p.styleProvider != nil {
		p.styles = newPickerStyles(p.styleProvider)
	}
}

// Close hides the picker.
func (p *Picker) Close() { p.open = false }

// IsOpen reports whether the picker shows.
func (p *Picker) IsOpen() bool { return p.open }

// Move moves the selection by delta, wrapping around the list.
func (p *Picker) Move(delta int) {
	if len(p.rows) == 0 {
		return
	}
	p.selected = (p.selected + delta + len(p.rows)) % len(p.rows)
}

// Selected is the value of the highlighted row.
func (p *Picker) Selected() string {
	if len(p.rows) == 0 {
		return ""
	}
	return p.rows[p.selected].Value
}

// SetWidth sets the width of the row the picker opens under.
func (p *Picker) SetWidth(width int) { p.width = width }

// Render draws the open picker: a title, one aligned row per choice and the
// keys, in a rounded box under the status row. "" while closed.
func (p *Picker) Render() string {
	if !p.open || p.styleProvider == nil {
		return ""
	}
	inner := min(max(p.width-pickerChrome, pickerMinInner), pickerMaxInner)
	labelWidth, tagWidth := 0, 0
	for _, row := range p.rows {
		labelWidth = max(labelWidth, lipgloss.Width(row.Label))
		tagWidth = max(tagWidth, lipgloss.Width(row.Tag))
	}
	title := p.styles.title.Render(p.spec.Title)
	if p.spec.Hint != "" {
		title += p.styles.faint.Render(" · " + p.spec.Hint)
	}
	lines := []string{title, ""}
	for i, row := range p.rows {
		lines = append(lines, p.renderRow(i, row, labelWidth, tagWidth, inner))
	}
	lines = append(lines, "", p.styles.faint.Render(p.spec.Keys))
	return p.styles.box.Render(strings.Join(lines, "\n"))
}

// renderRow is one choice: the cursor, a check on the current value, the label
// padded to the longest, the tag and as much of the detail as fits.
func (p *Picker) renderRow(i int, row PickerRow, labelWidth, tagWidth, inner int) string {
	s := p.styles
	cursor, mark, label := "  ", "  ", lipgloss.NewStyle().Width(labelWidth)
	if i == p.selected {
		cursor, label = s.cursor.Render(pickerCursor)+" ", s.selected.Width(labelWidth)
	}
	if row.Value == p.current {
		mark = s.current.Render(icons.CheckMark) + " "
	}
	line := cursor + mark + label.Render(row.Label) + "  "
	if tagWidth > 0 {
		tag := lipgloss.NewStyle().Width(tagWidth)
		if row.TagColor != "" {
			tag = tag.Foreground(lipgloss.Color(p.styleProvider.GetThemeColor(row.TagColor)))
		}
		line += tag.Render(row.Tag) + "  "
	}
	detail := truncateRunes(row.Detail, max(inner-lipgloss.Width(line), 0))
	if i == p.selected {
		return line + detail
	}
	return line + s.faint.Render(detail)
}
