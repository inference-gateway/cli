package styles

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/inference-gateway/cli/internal/domain"
)

// Provider centralizes all styling logic and provides complete abstraction from Lipgloss.
// Components should NEVER import lipgloss directly - they interact with styling through this provider.
type Provider struct {
	themeService domain.ThemeService
}

// NewProvider creates a new style provider
func NewProvider(themeService domain.ThemeService) *Provider {
	return &Provider{
		themeService: themeService,
	}
}

// Modal styles

// RenderModal renders a modal with rounded border
func (p *Provider) RenderModal(content string, width int) string {
	theme := p.themeService.GetCurrentTheme()
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(theme.GetBorderColor())).
		Padding(1, 2).
		Width(width)
	return style.Render(content)
}

// RenderModalTitle renders a modal title with emphasis
func (p *Provider) RenderModalTitle(title string) string {
	theme := p.themeService.GetCurrentTheme()
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.GetAccentColor())).
		Bold(true).
		Padding(0, 1)
	return style.Render(title)
}

// List/Selection styles

// RenderListItem renders a list item (selected or unselected)
func (p *Provider) RenderListItem(content string, selected bool) string {
	theme := p.themeService.GetCurrentTheme()

	if selected {
		style := lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.GetAccentColor())).
			Bold(true)
		return "▶ " + style.Render(content)
	}

	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.GetDimColor()))
	return "  " + style.Render(content)
}

// RenderListItemWithDescription renders a list item with a description
func (p *Provider) RenderListItemWithDescription(title, description string, selected bool) string {
	theme := p.themeService.GetCurrentTheme()

	var titleStyle, descStyle lipgloss.Style
	prefix := "  "

	if selected {
		titleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.GetAccentColor())).
			Bold(true)
		descStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.GetDimColor()))
		prefix = "▶ "
	} else {
		titleStyle = lipgloss.NewStyle()
		descStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.GetDimColor()))
	}

	return prefix + titleStyle.Render(title) + "\n   " + descStyle.Render(description)
}

// Input styles

// RenderInputField renders an input field with border
func (p *Provider) RenderInputField(content string, width int, focused bool) string {
	theme := p.themeService.GetCurrentTheme()

	borderColor := theme.GetBorderColor()
	if focused {
		borderColor = theme.GetAccentColor()
	}

	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(borderColor)).
		Padding(0, 1).
		Width(width)

	return style.Render(content)
}

// RenderInputPlaceholder renders placeholder text
func (p *Provider) RenderInputPlaceholder(text string) string {
	theme := p.themeService.GetCurrentTheme()
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.GetDimColor())).
		Italic(true)
	return style.Render(text)
}

// Button/Option styles

// RenderButton renders a button or selectable option
func (p *Provider) RenderButton(text string, selected bool) string {
	theme := p.themeService.GetCurrentTheme()

	if selected {
		style := lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.GetAccentColor())).
			Background(lipgloss.Color(theme.GetBorderColor())).
			Bold(true).
			Padding(0, 2)
		return style.Render(text)
	}

	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.GetDimColor())).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(theme.GetBorderColor())).
		Padding(0, 2)
	return style.Render(text)
}

// RenderApprovalButton renders an approval-style button with custom colors and fixed width
func (p *Provider) RenderApprovalButton(text string, selected bool, isApprove bool) string {
	theme := p.themeService.GetCurrentTheme()

	borderColor := theme.GetAccentColor()
	if !isApprove {
		borderColor = theme.GetErrorColor()
	}

	buttonWidth := 16

	if selected {
		bgColor := borderColor
		fgColor := "#000000"
		if !isApprove {
			fgColor = "#ffffff"
		}

		style := lipgloss.NewStyle().
			Width(buttonWidth).
			Align(lipgloss.Center).
			Padding(0, 1).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(borderColor)).
			Background(lipgloss.Color(bgColor)).
			Foreground(lipgloss.Color(fgColor)).
			Bold(true)
		return style.Render(text)
	}

	style := lipgloss.NewStyle().
		Width(buttonWidth).
		Align(lipgloss.Center).
		Padding(0, 1).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(borderColor)).
		Foreground(lipgloss.Color(borderColor))
	return style.Render(text)
}

// Text styles

// RenderUserText renders text in the user color
func (p *Provider) RenderUserText(text string) string {
	theme := p.themeService.GetCurrentTheme()
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.GetUserColor()))
	return style.Render(text)
}

// RenderAssistantText renders text in the assistant color
func (p *Provider) RenderAssistantText(text string) string {
	theme := p.themeService.GetCurrentTheme()
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.GetAssistantColor()))
	return style.Render(text)
}

// RenderErrorText renders text in the error color
func (p *Provider) RenderErrorText(text string) string {
	theme := p.themeService.GetCurrentTheme()
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.GetErrorColor())).
		Bold(true)
	return style.Render(text)
}

// RenderSuccessText renders text in the success color
func (p *Provider) RenderSuccessText(text string) string {
	theme := p.themeService.GetCurrentTheme()
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.GetSuccessColor())).
		Bold(true)
	return style.Render(text)
}

// RenderWarningText renders text in the warning/status color
func (p *Provider) RenderWarningText(text string) string {
	theme := p.themeService.GetCurrentTheme()
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.GetStatusColor()))
	return style.Render(text)
}

// RenderDimText renders text in a dimmed style
func (p *Provider) RenderDimText(text string) string {
	theme := p.themeService.GetCurrentTheme()
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.GetDimColor()))
	return style.Render(text)
}

// RenderPathText renders a file path with accent color and bold
func (p *Provider) RenderPathText(text string) string {
	theme := p.themeService.GetCurrentTheme()
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.GetAccentColor())).
		Bold(true)
	return style.Render(text)
}

// RenderMetricText renders metric/info text (e.g., byte counts, line counts)
func (p *Provider) RenderMetricText(text string) string {
	theme := p.themeService.GetCurrentTheme()
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.GetStatusColor()))
	return style.Render(text)
}

// RenderCreatedText renders "created" status text (success color, bold)
func (p *Provider) RenderCreatedText(text string) string {
	theme := p.themeService.GetCurrentTheme()
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.GetSuccessColor())).
		Bold(true)
	return style.Render(text)
}

// RenderUpdatedText renders "updated" status text (status/warning color, bold)
func (p *Provider) RenderUpdatedText(text string) string {
	theme := p.themeService.GetCurrentTheme()
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.GetStatusColor())).
		Bold(true)
	return style.Render(text)
}

// RenderSuccessIcon renders a success icon (checkmark, etc.)
func (p *Provider) RenderSuccessIcon(text string) string {
	theme := p.themeService.GetCurrentTheme()
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.GetSuccessColor()))
	return style.Render(text)
}

// RenderErrorIcon renders an error icon (X mark, etc.)
func (p *Provider) RenderErrorIcon(text string) string {
	theme := p.themeService.GetCurrentTheme()
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.GetErrorColor()))
	return style.Render(text)
}

// RenderBoldText renders bold text
func (p *Provider) RenderBoldText(text string) string {
	style := lipgloss.NewStyle().Bold(true)
	return style.Render(text)
}

// Layout/Structure styles

// RenderSeparator renders a horizontal separator line
func (p *Provider) RenderSeparator(width int, char string) string {
	theme := p.themeService.GetCurrentTheme()
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.GetDimColor()))
	return style.Render(strings.Repeat(char, width))
}

// RenderHeader renders a centered header
func (p *Provider) RenderHeader(text string, width int) string {
	theme := p.themeService.GetCurrentTheme()
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.GetAccentColor())).
		Bold(true).
		Width(width).
		Align(lipgloss.Center)
	return style.Render(text)
}

// RenderBordered renders content with a border
func (p *Provider) RenderBordered(content string, width int) string {
	theme := p.themeService.GetCurrentTheme()
	style := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(theme.GetBorderColor())).
		Padding(1, 2).
		Width(width)
	return style.Render(content)
}

// Diff/Code styles

// RenderDiffAddition renders a diff addition line
func (p *Provider) RenderDiffAddition(content string) string {
	theme := p.themeService.GetCurrentTheme()
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.GetDiffAddColor()))
	return style.Render("+ " + content)
}

// RenderDiffRemoval renders a diff removal line
func (p *Provider) RenderDiffRemoval(content string) string {
	theme := p.themeService.GetCurrentTheme()
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.GetDiffRemoveColor()))
	return style.Render("- " + content)
}

// RenderCodeBlock renders a code block with subtle background
func (p *Provider) RenderCodeBlock(code string, width int) string {
	theme := p.themeService.GetCurrentTheme()
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.GetAssistantColor())).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(theme.GetBorderColor())).
		Padding(1, 2).
		Width(width)
	return style.Render(code)
}

// Status/Badge styles

// RenderStatusBadge renders a status badge (e.g., "ENABLED", "DISABLED")
func (p *Provider) RenderStatusBadge(text string, positive bool) string {
	theme := p.themeService.GetCurrentTheme()

	var color string
	if positive {
		color = theme.GetSuccessColor()
	} else {
		color = theme.GetErrorColor()
	}

	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color(color)).
		Bold(true)
	return style.Render(text)
}

// RenderSpinner renders a spinner with status color
func (p *Provider) RenderSpinner(frame string) string {
	theme := p.themeService.GetCurrentTheme()
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.GetStatusColor()))
	return style.Render(frame)
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

// Custom rendering - for complex styling needs

// RenderWithColor renders text with a specific hex color
func (p *Provider) RenderWithColor(text, hexColor string) string {
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(hexColor))
	return style.Render(text)
}

// RenderWithColorAndBold renders text with color and bold
func (p *Provider) RenderWithColorAndBold(text, hexColor string) string {
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color(hexColor)).
		Bold(true)
	return style.Render(text)
}

// RenderBold renders text with bold styling
func (p *Provider) RenderBold(text string) string {
	return lipgloss.NewStyle().Bold(true).Render(text)
}

// RenderStyledText renders text with custom Lipgloss-compatible styling
// This is an escape hatch for complex styling not covered by other methods
func (p *Provider) RenderStyledText(text string, opts StyleOptions) string {
	style := lipgloss.NewStyle()

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

// RenderCursor renders text with cursor styling
func (p *Provider) RenderCursor(text string) string {
	style := lipgloss.NewStyle().
		Background(lipgloss.Color("#808080")).
		Foreground(lipgloss.Color("#000000"))
	return style.Render(text)
}

// RenderBorderedBox renders text inside a rounded border with padding
func (p *Provider) RenderBorderedBox(text, borderColor string, paddingV, paddingH int) string {
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder(), true).
		BorderForeground(lipgloss.Color(borderColor)).
		Padding(paddingV, paddingH)
	return style.Render(text)
}

// RenderCenteredBoldWithColor renders text centered, bold, and with a specific color
func (p *Provider) RenderCenteredBoldWithColor(text, hexColor string, width int) string {
	style := lipgloss.NewStyle().
		Width(width).
		Align(lipgloss.Center).
		Foreground(lipgloss.Color(hexColor)).
		Bold(true).
		Padding(0, 1)
	return style.Render(text)
}

// RenderCenteredBorderedBox renders text in a centered bordered box with specified dimensions
func (p *Provider) RenderCenteredBorderedBox(text, borderColor string, width, height, paddingV, paddingH int) string {
	style := lipgloss.NewStyle().
		Width(width).
		Height(height).
		Align(lipgloss.Center, lipgloss.Center).
		Border(lipgloss.RoundedBorder(), true).
		BorderForeground(lipgloss.Color(borderColor)).
		Padding(paddingV, paddingH)
	return style.Render(text)
}

// RenderLeftAlignedBorderedBox renders text in a left-aligned bordered box with specified dimensions
func (p *Provider) RenderLeftAlignedBorderedBox(text, borderColor string, width, height, paddingV, paddingH int) string {
	style := lipgloss.NewStyle().
		Width(width).
		Height(height).
		Align(lipgloss.Left, lipgloss.Center).
		Border(lipgloss.RoundedBorder(), true).
		BorderForeground(lipgloss.Color(borderColor)).
		Padding(paddingV, paddingH)
	return style.Render(text)
}

// RenderTopAlignedBorderedBox renders text in a top-left aligned bordered box with specified dimensions
func (p *Provider) RenderTopAlignedBorderedBox(text, borderColor string, width, height, paddingV, paddingH int) string {
	style := lipgloss.NewStyle().
		Width(width).
		Height(height).
		Align(lipgloss.Left, lipgloss.Top).
		Border(lipgloss.RoundedBorder(), true).
		BorderForeground(lipgloss.Color(borderColor)).
		Padding(paddingV, paddingH)
	return style.Render(text)
}

// GetSpinnerStyle returns a lipgloss.Style for use with third-party components like Bubbles spinner
// This is an exception to complete abstraction, needed for library compatibility
func (p *Provider) GetSpinnerStyle() lipgloss.Style {
	theme := p.themeService.GetCurrentTheme()
	return lipgloss.NewStyle().Foreground(lipgloss.Color(theme.GetStatusColor()))
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
