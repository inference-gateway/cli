package styles

import (
	"strings"

	lipgloss "charm.land/lipgloss/v2"

	domain "github.com/inference-gateway/cli/internal/domain"
)

// Theme-independent styles, built once at package init. plainStyle and
// roundedBox are the bases the dynamic-color escape hatches derive from so no
// Render* method has to construct a style chain from scratch per call.
var (
	plainStyle   = lipgloss.NewStyle()
	boldStyle    = lipgloss.NewStyle().Bold(true)
	reverseStyle = lipgloss.NewStyle().Reverse(true)
	roundedBox   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder(), true)
)

// themedStyles holds every style derived from theme colors, pre-baked once per
// theme change instead of rebuilt inside each Render* call per frame.
// Width-parameterized methods apply .Width(w) on the cached base — a struct
// copy without color re-parsing.
type themedStyles struct {
	accentBold        lipgloss.Style
	dim               lipgloss.Style
	dimItalic         lipgloss.Style
	user              lipgloss.Style
	assistant         lipgloss.Style
	errorText         lipgloss.Style
	errorBold         lipgloss.Style
	success           lipgloss.Style
	successBold       lipgloss.Style
	status            lipgloss.Style
	statusBold        lipgloss.Style
	selectedIndicator lipgloss.Style
	modal             lipgloss.Style
	modalTitle        lipgloss.Style
	inputField        lipgloss.Style
	inputFieldFocused lipgloss.Style
	buttonSelected    lipgloss.Style
	buttonUnselected  lipgloss.Style
	approveSelected   lipgloss.Style
	approveUnselected lipgloss.Style
	rejectSelected    lipgloss.Style
	rejectUnselected  lipgloss.Style
	header            lipgloss.Style
	bordered          lipgloss.Style
	codeBlock         lipgloss.Style
	diffAdd           lipgloss.Style
	diffRemove        lipgloss.Style
}

func buildThemedStyles(theme domain.Theme) themedStyles {
	accent := lipgloss.Color(theme.GetAccentColor())
	dim := lipgloss.Color(theme.GetDimColor())
	border := lipgloss.Color(theme.GetBorderColor())
	errColor := lipgloss.Color(theme.GetErrorColor())
	success := lipgloss.Color(theme.GetSuccessColor())
	status := lipgloss.Color(theme.GetStatusColor())

	const approvalButtonWidth = 16
	approvalBase := lipgloss.NewStyle().
		Width(approvalButtonWidth).
		Align(lipgloss.Center).
		Padding(0, 1).
		Border(lipgloss.RoundedBorder())

	return themedStyles{
		accentBold: lipgloss.NewStyle().Foreground(accent).Bold(true),
		dim:        lipgloss.NewStyle().Foreground(dim),
		dimItalic:  lipgloss.NewStyle().Foreground(dim).Italic(true),
		user:       lipgloss.NewStyle().Foreground(lipgloss.Color(theme.GetUserColor())),
		assistant:  lipgloss.NewStyle().Foreground(lipgloss.Color(theme.GetAssistantColor())),
		errorText:  lipgloss.NewStyle().Foreground(errColor),
		errorBold:  lipgloss.NewStyle().Foreground(errColor).Bold(true),
		success:    lipgloss.NewStyle().Foreground(success),
		successBold: lipgloss.NewStyle().
			Foreground(success).
			Bold(true),
		status:     lipgloss.NewStyle().Foreground(status),
		statusBold: lipgloss.NewStyle().Foreground(status).Bold(true),
		selectedIndicator: lipgloss.NewStyle().
			Foreground(accent).
			Reverse(true).
			Bold(true).
			Padding(0, 1),
		modal: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(border).
			Padding(1, 2),
		modalTitle: lipgloss.NewStyle().
			Foreground(accent).
			Bold(true).
			Padding(0, 1),
		inputField: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(border).
			Padding(0, 1),
		inputFieldFocused: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(accent).
			Padding(0, 1),
		buttonSelected: lipgloss.NewStyle().
			Foreground(accent).
			Background(border).
			Bold(true).
			Padding(0, 2),
		buttonUnselected: lipgloss.NewStyle().
			Foreground(dim).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(border).
			Padding(0, 2),
		approveSelected: approvalBase.
			BorderForeground(accent).
			Background(accent).
			Foreground(lipgloss.Color("#000000")).
			Bold(true),
		approveUnselected: approvalBase.
			BorderForeground(accent).
			Foreground(accent),
		rejectSelected: approvalBase.
			BorderForeground(errColor).
			Background(errColor).
			Foreground(lipgloss.Color("#ffffff")).
			Bold(true),
		rejectUnselected: approvalBase.
			BorderForeground(errColor).
			Foreground(errColor),
		header: lipgloss.NewStyle().
			Foreground(accent).
			Bold(true).
			Align(lipgloss.Center),
		bordered: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(border).
			Padding(1, 2),
		codeBlock: lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.GetAssistantColor())).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(border).
			Padding(1, 2),
		diffAdd:    lipgloss.NewStyle().Foreground(lipgloss.Color(theme.GetDiffAddColor())),
		diffRemove: lipgloss.NewStyle().Foreground(lipgloss.Color(theme.GetDiffRemoveColor())),
	}
}

// Provider centralizes all styling logic and provides complete abstraction from Lipgloss.
// Components should NEVER import lipgloss directly - they interact with styling through this provider.
type Provider struct {
	themeService domain.ThemeService
	built        bool         // whether s has been built at all (a theme name can legitimately be "")
	themeName    string       // theme the cache was built for
	s            themedStyles // styles pre-baked for themeName
}

// NewProvider creates a new style provider
func NewProvider(themeService domain.ThemeService) *Provider {
	return &Provider{
		themeService: themeService,
	}
}

// styles returns the pre-baked styles for the current theme, lazily rebuilding
// them when the theme changed since the last call. There is no theme-change
// notification in ThemeService, so a name check per access is the invalidation
// mechanism; it is one string compare versus rebuilding every style chain per
// render. Not goroutine-safe: each Provider instance is owned by a single
// goroutine today (TUI loop or a tool's executor), matching existing usage.
func (p *Provider) styles() *themedStyles {
	if name := p.themeService.GetCurrentThemeName(); !p.built || name != p.themeName {
		p.s = buildThemedStyles(p.themeService.GetCurrentTheme())
		p.themeName = name
		p.built = true
	}
	return &p.s
}

// GetCurrentTheme returns the active theme, exposing it for callers that need
// to derive colors outside of Provider's pre-baked render methods (e.g. the
// diffview Style selector).
func (p *Provider) GetCurrentTheme() domain.Theme {
	return p.themeService.GetCurrentTheme()
}

// Modal styles

// RenderModal renders a modal with rounded border
func (p *Provider) RenderModal(content string, width int) string {
	return p.styles().modal.Width(width).Render(content)
}

// RenderModalTitle renders a modal title with emphasis
func (p *Provider) RenderModalTitle(title string) string {
	return p.styles().modalTitle.Render(title)
}

// List/Selection styles

// RenderListItem renders a list item (selected or unselected)
func (p *Provider) RenderListItem(content string, selected bool) string {
	if selected {
		return "▶ " + p.styles().accentBold.Render(content)
	}
	return "  " + p.styles().dim.Render(content)
}

// RenderListItemWithDescription renders a list item with a description
func (p *Provider) RenderListItemWithDescription(title, description string, selected bool) string {
	s := p.styles()

	titleStyle := plainStyle
	prefix := "  "
	if selected {
		titleStyle = s.accentBold
		prefix = "▶ "
	}

	return prefix + titleStyle.Render(title) + "\n   " + s.dim.Render(description)
}

// Input styles

// RenderInputField renders an input field with border. When branchLabel is
// non-empty it is embedded in the top border, right-aligned, as a titled border
// (e.g. "╭──────── ⎇ main ─╮"); pass "" for a plain border.
func (p *Provider) RenderInputField(content string, width int, focused bool, branchLabel string) string {
	theme := p.themeService.GetCurrentTheme()

	style := p.styles().inputField
	borderColor := theme.GetBorderColor()
	if focused {
		style = p.styles().inputFieldFocused
		borderColor = theme.GetAccentColor()
	}

	rendered := style.Width(width).Render(content)
	if branchLabel == "" {
		return rendered
	}

	return p.spliceBranchIntoTopBorder(rendered, branchLabel, borderColor, theme.GetStatusColor())
}

// spliceBranchIntoTopBorder rebuilds the top border line of an already-rendered
// rounded box so label sits near the right corner, styled distinctly from the
// border. The label is truncated with an ellipsis when the box is narrow and
// dropped entirely when there is no reasonable room for it. The rebuilt line
// keeps the original measured width so the sides and bottom border stay aligned.
func (p *Provider) spliceBranchIntoTopBorder(box, label, borderColor, labelColor string) string {
	lines := strings.Split(box, "\n")
	if len(lines) == 0 {
		return box
	}

	const (
		corners       = 2 // ╭ + ╮
		rightMargin   = 2 // dashes between label and the right corner
		minLeftDashes = 1
		minTitleWidth = 8  // below this, render a plain border instead
		maxTitleWidth = 40 // cap so a long branch never dominates a wide box
		spaces        = 2  // one space on each side of the label
		ellipsis      = "..."
	)

	boxWidth := lipgloss.Width(lines[0])
	budget := boxWidth - corners - rightMargin - minLeftDashes
	if budget < minTitleWidth {
		return box
	}
	if budget > maxTitleWidth {
		budget = maxTitleWidth
	}

	label = " " + truncateBorderLabel(label, budget-spaces, ellipsis) + " "
	labelWidth := lipgloss.Width(label)
	leftDashes := max(boxWidth-corners-rightMargin-labelWidth, minLeftDashes)

	lines[0] = p.RenderWithColor("╭"+strings.Repeat("─", leftDashes), borderColor) +
		p.RenderWithColor(label, labelColor) +
		p.RenderWithColor(strings.Repeat("─", rightMargin)+"╮", borderColor)

	return strings.Join(lines, "\n")
}

// truncateBorderLabel shortens s to at most maxWidth display columns, appending
// tail when it has to cut. A short final space-separated token (e.g. the PR
// "#854" in "⎇ branch  #854") is preserved and the cut lands before it, so the
// PR number survives long branch names. It walks runes so multi-byte names are
// never split mid-character and wide runes are accounted for.
func truncateBorderLabel(s string, maxWidth int, tail string) string {
	if lipgloss.Width(s) <= maxWidth {
		return s
	}

	head, suffix := s, ""
	if i := strings.LastIndex(s, " "); i >= 0 && lipgloss.Width(s[i:]) <= maxWidth/2 {
		head, suffix = s[:i], s[i:]
	}
	target := max(maxWidth-lipgloss.Width(tail)-lipgloss.Width(suffix), 0)

	var out []rune
	width := 0
	for _, r := range head {
		rw := lipgloss.Width(string(r))
		if width+rw > target {
			break
		}
		out = append(out, r)
		width += rw
	}
	return string(out) + tail + suffix
}

// RenderInputPlaceholder renders placeholder text
func (p *Provider) RenderInputPlaceholder(text string) string {
	return p.styles().dimItalic.Render(text)
}

// Button/Option styles

// RenderButton renders a button or selectable option
func (p *Provider) RenderButton(text string, selected bool) string {
	if selected {
		return p.styles().buttonSelected.Render(text)
	}
	return p.styles().buttonUnselected.Render(text)
}

// RenderApprovalButton renders an approval-style button with custom colors and fixed width
func (p *Provider) RenderApprovalButton(text string, selected bool, isApprove bool) string {
	s := p.styles()
	switch {
	case selected && isApprove:
		return s.approveSelected.Render(text)
	case selected:
		return s.rejectSelected.Render(text)
	case isApprove:
		return s.approveUnselected.Render(text)
	default:
		return s.rejectUnselected.Render(text)
	}
}

// Text styles

// RenderUserText renders text in the user color
func (p *Provider) RenderUserText(text string) string {
	return p.styles().user.Render(text)
}

// RenderAssistantText renders text in the assistant color
func (p *Provider) RenderAssistantText(text string) string {
	return p.styles().assistant.Render(text)
}

// RenderErrorText renders text in the error color
func (p *Provider) RenderErrorText(text string) string {
	return p.styles().errorBold.Render(text)
}

// RenderSuccessText renders text in the success color
func (p *Provider) RenderSuccessText(text string) string {
	return p.styles().successBold.Render(text)
}

// RenderWarningText renders text in the warning/status color
func (p *Provider) RenderWarningText(text string) string {
	return p.styles().status.Render(text)
}

// RenderDimText renders text in a dimmed style
func (p *Provider) RenderDimText(text string) string {
	return p.styles().dim.Render(text)
}

// RenderSelectedIndicator renders the status-bar indicator holding the
// selection as a padded pill: reverse video paints the cell background in the
// accent color with the terminal background as text color, which works on any
// theme (Theme exposes no background color). The one-column side padding adds
// two columns of width - splitPartsIntoLines accounts for it.
func (p *Provider) RenderSelectedIndicator(text string) string {
	return p.styles().selectedIndicator.Render(text)
}

// RenderPathText renders a file path with accent color and bold
func (p *Provider) RenderPathText(text string) string {
	return p.styles().accentBold.Render(text)
}

// RenderMetricText renders metric/info text (e.g., byte counts, line counts)
func (p *Provider) RenderMetricText(text string) string {
	return p.styles().status.Render(text)
}

// RenderCreatedText renders "created" status text (success color, bold)
func (p *Provider) RenderCreatedText(text string) string {
	return p.styles().successBold.Render(text)
}

// RenderUpdatedText renders "updated" status text (status/warning color, bold)
func (p *Provider) RenderUpdatedText(text string) string {
	return p.styles().statusBold.Render(text)
}

// RenderSuccessIcon renders a success icon (checkmark, etc.)
func (p *Provider) RenderSuccessIcon(text string) string {
	return p.styles().success.Render(text)
}

// RenderErrorIcon renders an error icon (X mark, etc.)
func (p *Provider) RenderErrorIcon(text string) string {
	return p.styles().errorText.Render(text)
}

// RenderBoldText renders bold text
func (p *Provider) RenderBoldText(text string) string {
	return boldStyle.Render(text)
}

// Layout/Structure styles

// RenderSeparator renders a horizontal separator line
func (p *Provider) RenderSeparator(width int, char string) string {
	return p.styles().dim.Render(strings.Repeat(char, width))
}

// RenderHeader renders a centered header
func (p *Provider) RenderHeader(text string, width int) string {
	return p.styles().header.Width(width).Render(text)
}

// RenderBordered renders content with a border
func (p *Provider) RenderBordered(content string, width int) string {
	return p.styles().bordered.Width(width).Render(content)
}

// Diff/Code styles

// RenderDiffAddition renders a diff addition line
func (p *Provider) RenderDiffAddition(content string) string {
	return p.styles().diffAdd.Render("+ " + content)
}

// RenderDiffRemoval renders a diff removal line
func (p *Provider) RenderDiffRemoval(content string) string {
	return p.styles().diffRemove.Render("- " + content)
}

// RenderCodeBlock renders a code block with subtle background
func (p *Provider) RenderCodeBlock(code string, width int) string {
	return p.styles().codeBlock.Width(width).Render(code)
}

// Status/Badge styles

// RenderStatusBadge renders a status badge (e.g., "ENABLED", "DISABLED")
func (p *Provider) RenderStatusBadge(text string, positive bool) string {
	if positive {
		return p.styles().successBold.Render(text)
	}
	return p.styles().errorBold.Render(text)
}

// RenderSpinner renders a spinner with status color
func (p *Provider) RenderSpinner(frame string) string {
	return p.styles().status.Render(frame)
}

// Utility methods

// GetThemeColor returns a theme color for custom styling (use sparingly)
func (p *Provider) GetThemeColor(colorName string) string {
	theme := p.themeService.GetCurrentTheme()

	switch colorName {
	case "user":
		return theme.GetUserColor()
	case "assistant":
		return theme.GetAssistantColor()
	case "error":
		return theme.GetErrorColor()
	case "success":
		return theme.GetSuccessColor()
	case "status":
		return theme.GetStatusColor()
	case "accent":
		return theme.GetAccentColor()
	case "dim":
		return theme.GetDimColor()
	case "border":
		return theme.GetBorderColor()
	case "diffAdd":
		return theme.GetDiffAddColor()
	case "diffRemove":
		return theme.GetDiffRemoveColor()
	default:
		return theme.GetAssistantColor()
	}
}

// Layout utilities to avoid components depending on lipgloss

// JoinVertical joins strings vertically
func (p *Provider) JoinVertical(strs ...string) string {
	return lipgloss.JoinVertical(lipgloss.Left, strs...)
}

// JoinHorizontal joins strings horizontally
func (p *Provider) JoinHorizontal(strs ...string) string {
	return lipgloss.JoinHorizontal(lipgloss.Top, strs...)
}

// PlaceHorizontal places two components horizontally with the second one on the right
func (p *Provider) PlaceHorizontal(width int, left string, right string) string {
	leftWidth := lipgloss.Width(left)
	rightWidth := lipgloss.Width(right)

	if leftWidth+rightWidth >= width {
		return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	}

	padding := width - leftWidth - rightWidth
	spacer := strings.Repeat(" ", padding)

	return lipgloss.JoinHorizontal(lipgloss.Top, left, spacer, right)
}

// PlaceCenter places content in the center of the given dimensions
func (p *Provider) PlaceCenter(width, height int, content string) string {
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, content)
}

// PlaceCenterTop places content in the center-top of the given dimensions
func (p *Provider) PlaceCenterTop(width, height int, content string) string {
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Top, content)
}

// GetHeight returns the rendered height of a string
func (p *Provider) GetHeight(s string) int {
	return lipgloss.Height(s)
}

// GetWidth returns the rendered width of a string
func (p *Provider) GetWidth(s string) int {
	return lipgloss.Width(s)
}

// ClampToSize hard-truncates s to width columns and height rows (ANSI-aware) so a
// frame containing an over-wide or over-tall line cannot wrap and corrupt the inline
// (non-alt-screen) render. MaxWidth/MaxHeight truncate rather than pad or wrap.
func (p *Provider) ClampToSize(s string, width, height int) string {
	if width <= 0 || height <= 0 {
		return s
	}
	return plainStyle.MaxWidth(width).MaxHeight(height).Render(s)
}

// Custom rendering - for complex styling needs

// RenderWithColor renders text with a specific hex color
func (p *Provider) RenderWithColor(text, hexColor string) string {
	return plainStyle.Foreground(lipgloss.Color(hexColor)).Render(text)
}

// RenderWithColorAndBold renders text with color and bold
func (p *Provider) RenderWithColorAndBold(text, hexColor string) string {
	return boldStyle.Foreground(lipgloss.Color(hexColor)).Render(text)
}

// RenderBold renders text with bold styling
func (p *Provider) RenderBold(text string) string {
	return boldStyle.Render(text)
}

// RenderStyledText renders text with custom Lipgloss-compatible styling
// This is an escape hatch for complex styling not covered by other methods
func (p *Provider) RenderStyledText(text string, opts StyleOptions) string {
	style := plainStyle

	if opts.Foreground != "" {
		style = style.Foreground(lipgloss.Color(opts.Foreground))
	}
	if opts.Background != "" {
		style = style.Background(lipgloss.Color(opts.Background))
	}
	if opts.Bold {
		style = style.Bold(true)
	}
	if opts.Italic {
		style = style.Italic(true)
	}
	if opts.Faint {
		style = style.Faint(true)
	}
	if opts.Width > 0 {
		style = style.Width(opts.Width)
	}
	if opts.Padding[0] > 0 || opts.Padding[1] > 0 {
		style = style.Padding(opts.Padding[0], opts.Padding[1])
	}
	if opts.MarginBottom > 0 {
		style = style.MarginBottom(opts.MarginBottom)
	}
	if opts.MarginTop > 0 {
		style = style.MarginTop(opts.MarginTop)
	}

	return style.Render(text)
}

// StyleOptions provides options for custom text styling
type StyleOptions struct {
	Foreground   string
	Background   string
	Bold         bool
	Italic       bool
	Faint        bool
	Width        int
	Padding      [2]int
	MarginBottom int
	MarginTop    int
}

// RenderCursor renders text with cursor styling. It uses lipgloss Reverse,
// which swaps the foreground/background of whatever theme colours are in
// effect, so the cursor stays legible on every theme instead of relying on a
// hardcoded grey-on-black pair.
func (p *Provider) RenderCursor(text string) string {
	return reverseStyle.Render(text)
}

// RenderBorderedBox renders text inside a rounded border with padding
func (p *Provider) RenderBorderedBox(text, borderColor string, paddingV, paddingH int) string {
	return roundedBox.
		BorderForeground(lipgloss.Color(borderColor)).
		Padding(paddingV, paddingH).
		Render(text)
}

// RenderCenteredBoldWithColor renders text centered, bold, and with a specific color
func (p *Provider) RenderCenteredBoldWithColor(text, hexColor string, width int) string {
	return boldStyle.
		Width(width).
		Align(lipgloss.Center).
		Foreground(lipgloss.Color(hexColor)).
		Padding(0, 1).
		Render(text)
}

// RenderCenteredBorderedBox renders text in a centered bordered box with specified dimensions
func (p *Provider) RenderCenteredBorderedBox(text, borderColor string, width, height, paddingV, paddingH int) string {
	return roundedBox.
		Width(width).
		Height(height).
		Align(lipgloss.Center, lipgloss.Center).
		BorderForeground(lipgloss.Color(borderColor)).
		Padding(paddingV, paddingH).
		Render(text)
}

// RenderLeftAlignedBorderedBox renders text in a left-aligned bordered box with specified dimensions
func (p *Provider) RenderLeftAlignedBorderedBox(text, borderColor string, width, height, paddingV, paddingH int) string {
	return roundedBox.
		Width(width).
		Height(height).
		Align(lipgloss.Left, lipgloss.Center).
		BorderForeground(lipgloss.Color(borderColor)).
		Padding(paddingV, paddingH).
		Render(text)
}

// RenderTopAlignedBorderedBox renders text in a top-left aligned bordered box with specified dimensions
func (p *Provider) RenderTopAlignedBorderedBox(text, borderColor string, width, height, paddingV, paddingH int) string {
	return roundedBox.
		Width(width).
		Height(height).
		Align(lipgloss.Left, lipgloss.Top).
		BorderForeground(lipgloss.Color(borderColor)).
		Padding(paddingV, paddingH).
		Render(text)
}

// GetSpinnerStyle returns a lipgloss.Style for use with third-party components like Bubbles spinner
// This is an exception to complete abstraction, needed for library compatibility
func (p *Provider) GetSpinnerStyle() lipgloss.Style {
	return p.styles().status
}

// GetThemeService returns the underlying theme service for advanced integrations
// This is needed for components like the markdown renderer that need direct theme access
func (p *Provider) GetThemeService() domain.ThemeService {
	return p.themeService
}

// RenderStatusLine renders a status line with no padding
func (p *Provider) RenderStatusLine(content string) string {
	return content
}
