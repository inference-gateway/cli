package markdown

import (
	"strings"

	glamour "charm.land/glamour/v2"
	ansi "charm.land/glamour/v2/ansi"

	formatting "github.com/inference-gateway/cli/internal/platform/formatting"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
)

// Renderer handles markdown to styled terminal output conversion
type Renderer struct {
	themeService tui.ThemeService
	width        int
	renderer     *glamour.TermRenderer
}

// NewRenderer creates a new markdown renderer with theme integration
func NewRenderer(themeService tui.ThemeService, width int) *Renderer {
	r := &Renderer{
		themeService: themeService,
		width:        width,
	}
	r.updateRenderer()
	return r
}

// SetWidth updates the renderer width for responsive rendering
func (r *Renderer) SetWidth(width int) {
	if width != r.width {
		r.width = width
		r.updateRenderer()
	}
}

// RefreshTheme rebuilds the renderer with current theme colors
// Call this when the theme changes
func (r *Renderer) RefreshTheme() {
	r.updateRenderer()
}

// Render converts markdown text to styled terminal output
func (r *Renderer) Render(content string) string {
	if r.renderer == nil {
		return content
	}

	if !containsMarkdown(content) {
		return formatting.FormatResponsiveMessage(content, r.width)
	}

	rendered, err := r.renderer.Render(content)
	if err != nil {
		return content
	}

	rendered = strings.TrimSpace(rendered)
	return rendered
}

// RenderBlock renders a markdown block with surrounding context
func (r *Renderer) RenderBlock(content string) string {
	rendered := r.Render(content)
	return rendered
}

// updateRenderer recreates the glamour renderer with current theme settings
func (r *Renderer) updateRenderer() {
	styleConfig := r.buildStyleConfig()

	renderer, err := glamour.NewTermRenderer(
		glamour.WithStyles(styleConfig),
		glamour.WithWordWrap(r.width),
	)
	if err != nil {
		renderer, _ = glamour.NewTermRenderer(
			glamour.WithWordWrap(r.width),
		)
	}
	r.renderer = renderer
}

// buildStyleConfig creates a glamour style config from the current theme
func (r *Renderer) buildStyleConfig() ansi.StyleConfig {
	accentColor, dimColor, successColor, statusColor, borderColor := r.getThemeColors()

	config := r.buildBaseStyleConfig(dimColor, statusColor)

	headingStyles := r.buildHeadingStyles(accentColor, dimColor, successColor, statusColor)
	config.Heading = headingStyles.Heading
	config.H1 = headingStyles.H1
	config.H2 = headingStyles.H2
	config.H3 = headingStyles.H3
	config.H4 = headingStyles.H4
	config.H5 = headingStyles.H5
	config.H6 = headingStyles.H6

	textFormattingStyles := r.buildTextFormattingStyles(accentColor, dimColor, borderColor)
	config.Strikethrough = textFormattingStyles.Strikethrough
	config.Emph = textFormattingStyles.Emph
	config.Strong = textFormattingStyles.Strong
	config.HorizontalRule = textFormattingStyles.HorizontalRule
	config.Item = textFormattingStyles.Item
	config.Enumeration = textFormattingStyles.Enumeration
	config.Task = textFormattingStyles.Task
	config.Link = textFormattingStyles.Link
	config.LinkText = textFormattingStyles.LinkText
	config.Image = textFormattingStyles.Image
	config.ImageText = textFormattingStyles.ImageText

	return config
}

// buildBaseStyleConfig creates the base style configuration with document, block, and code styles
func (r *Renderer) buildBaseStyleConfig(dimColor, statusColor string) ansi.StyleConfig {
	return ansi.StyleConfig{
		Document: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{},
			Margin:         new(uint(0)),
		},
		BlockQuote: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: new(dimColor),
			},
			Indent:      new(uint(1)),
			IndentToken: new("│ "),
		},
		Paragraph: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{},
		},
		List: ansi.StyleList{
			StyleBlock:  ansi.StyleBlock{},
			LevelIndent: 2,
		},
		Code: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color:           new(statusColor),
				BackgroundColor: new("#1a1a2e"),
			},
		},
		CodeBlock: ansi.StyleCodeBlock{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{},
				Margin:         new(uint(1)),
			},
			Chroma: r.buildChromaConfig(),
		},
		Table: ansi.StyleTable{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{},
			},
			CenterSeparator: new("┼"),
			ColumnSeparator: new("│"),
			RowSeparator:    new("─"),
		},
		DefinitionList: ansi.StyleBlock{},
		DefinitionTerm: ansi.StylePrimitive{
			Bold: new(true),
		},
		DefinitionDescription: ansi.StylePrimitive{
			BlockPrefix: "  ",
		},
	}
}

// buildChromaConfig creates the syntax highlighting configuration for code blocks
// Uses the injected theme service for consistent colors across themes
func (r *Renderer) buildChromaConfig() *ansi.Chroma {
	theme := r.themeService.GetCurrentTheme()

	accentColor := theme.GetAccentColor()
	dimColor := theme.GetDimColor()
	successColor := theme.GetSuccessColor()
	statusColor := theme.GetStatusColor()
	errorColor := theme.GetErrorColor()
	userColor := theme.GetUserColor()
	assistantColor := theme.GetAssistantColor()

	return &ansi.Chroma{
		Text: ansi.StylePrimitive{
			Color: new(assistantColor),
		},
		Error: ansi.StylePrimitive{
			Color: new(errorColor),
		},
		Comment: ansi.StylePrimitive{
			Color: new(dimColor),
		},
		CommentPreproc: ansi.StylePrimitive{
			Color: new(accentColor),
		},
		Keyword: ansi.StylePrimitive{
			Color: new(accentColor),
		},
		KeywordReserved: ansi.StylePrimitive{
			Color: new(accentColor),
		},
		KeywordNamespace: ansi.StylePrimitive{
			Color: new(accentColor),
		},
		KeywordType: ansi.StylePrimitive{
			Color: new(userColor),
		},
		Operator: ansi.StylePrimitive{
			Color: new(accentColor),
		},
		Punctuation: ansi.StylePrimitive{
			Color: new(assistantColor),
		},
		Name: ansi.StylePrimitive{
			Color: new(assistantColor),
		},
		NameBuiltin: ansi.StylePrimitive{
			Color: new(userColor),
		},
		NameTag: ansi.StylePrimitive{
			Color: new(accentColor),
		},
		NameAttribute: ansi.StylePrimitive{
			Color: new(successColor),
		},
		NameClass: ansi.StylePrimitive{
			Color: new(userColor),
		},
		NameConstant: ansi.StylePrimitive{
			Color: new(statusColor),
		},
		NameDecorator: ansi.StylePrimitive{
			Color: new(successColor),
		},
		NameException: ansi.StylePrimitive{
			Color: new(errorColor),
		},
		NameFunction: ansi.StylePrimitive{
			Color: new(successColor),
		},
		NameOther: ansi.StylePrimitive{
			Color: new(assistantColor),
		},
		Literal: ansi.StylePrimitive{
			Color: new(statusColor),
		},
		LiteralNumber: ansi.StylePrimitive{
			Color: new(statusColor),
		},
		LiteralDate: ansi.StylePrimitive{
			Color: new(statusColor),
		},
		LiteralString: ansi.StylePrimitive{
			Color: new(statusColor),
		},
		LiteralStringEscape: ansi.StylePrimitive{
			Color: new(accentColor),
		},
		GenericDeleted: ansi.StylePrimitive{
			Color: new(errorColor),
		},
		GenericEmph: ansi.StylePrimitive{
			Italic: new(true),
		},
		GenericInserted: ansi.StylePrimitive{
			Color: new(successColor),
		},
		GenericStrong: ansi.StylePrimitive{
			Bold: new(true),
		},
		GenericSubheading: ansi.StylePrimitive{
			Color: new(userColor),
		},
		Background: ansi.StylePrimitive{},
	}
}

// containsMarkdown checks if content contains markdown syntax
// Returns false for structured tool output to avoid mangling it
func containsMarkdown(content string) bool {
	if strings.Contains(content, "├─") || strings.Contains(content, "└─") ||
		strings.Contains(content, "│") || strings.Contains(content, "┌") ||
		strings.Contains(content, "┐") || strings.Contains(content, "┘") ||
		strings.Contains(content, "┴") || strings.Contains(content, "┬") {
		return false
	}

	if strings.Contains(content, "Duration:") && strings.Contains(content, "Status:") {
		return false
	}

	patterns := []string{
		"```",
		"**",
		"__",
		"# ",
		"## ",
		"### ",
		"1. ",
		"> ",
		"---",
		"***",
	}

	for _, pattern := range patterns {
		if strings.Contains(content, pattern) {
			return true
		}
	}

	if strings.Contains(content, "`") && !strings.Contains(content, "Exit Code") {
		return true
	}

	if strings.Contains(content, "](") && strings.Contains(content, "[") {
		return true
	}

	return false
}

// getThemeColors retrieves all theme colors used in styling
func (r *Renderer) getThemeColors() (accentColor, dimColor, successColor, statusColor, borderColor string) {
	theme := r.themeService.GetCurrentTheme()
	accentColor = theme.GetAccentColor()
	dimColor = theme.GetDimColor()
	successColor = theme.GetSuccessColor()
	statusColor = theme.GetStatusColor()
	borderColor = theme.GetBorderColor()
	return
}

// buildHeadingStyles creates the heading styles for the style config
func (r *Renderer) buildHeadingStyles(accentColor, dimColor, successColor, statusColor string) ansi.StyleConfig {
	return ansi.StyleConfig{
		Heading: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Bold:  new(true),
				Color: new(accentColor),
			},
		},
		H1: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Bold:  new(true),
				Color: new(accentColor),
			},
		},
		H2: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Bold:  new(true),
				Color: new(statusColor),
			},
		},
		H3: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Bold:  new(true),
				Color: new(successColor),
			},
		},
		H4: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Bold:  new(true),
				Color: new(statusColor),
			},
		},
		H5: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Bold:  new(true),
				Color: new(dimColor),
			},
		},
		H6: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Bold:  new(true),
				Color: new(dimColor),
			},
		},
	}
}

// buildTextFormattingStyles creates the text formatting styles for the style config
func (r *Renderer) buildTextFormattingStyles(accentColor, dimColor, borderColor string) ansi.StyleConfig {
	return ansi.StyleConfig{
		Strikethrough: ansi.StylePrimitive{
			CrossedOut: new(true),
		},
		Emph: ansi.StylePrimitive{
			Italic: new(true),
		},
		Strong: ansi.StylePrimitive{
			Bold: new(true),
		},
		HorizontalRule: ansi.StylePrimitive{
			Color:  new(borderColor),
			Format: "─────────────────────────────────────────",
		},
		Item: ansi.StylePrimitive{
			BlockPrefix: "• ",
		},
		Enumeration: ansi.StylePrimitive{
			BlockPrefix: ". ",
		},
		Task: ansi.StyleTask{
			Ticked:   "[✓] ",
			Unticked: "[ ] ",
		},
		Link: ansi.StylePrimitive{
			Color:     new(accentColor),
			Underline: new(true),
		},
		LinkText: ansi.StylePrimitive{
			Color: new(accentColor),
			Bold:  new(true),
		},
		Image: ansi.StylePrimitive{
			Color: new(dimColor),
		},
		ImageText: ansi.StylePrimitive{
			Color:  new(dimColor),
			Format: "[Image: {{.text}}]",
		},
	}
}
