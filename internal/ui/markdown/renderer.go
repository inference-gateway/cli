package markdown

import (
	"strings"

	glamour "github.com/charmbracelet/glamour"
	ansi "github.com/charmbracelet/glamour/ansi"
	domain "github.com/inference-gateway/cli/internal/domain"
)

// Renderer handles markdown to styled terminal output conversion
type Renderer struct {
	themeService domain.ThemeService
	width        int
	renderer     *glamour.TermRenderer
}

// NewRenderer creates a new markdown renderer with theme integration
func NewRenderer(themeService domain.ThemeService, width int) *Renderer {
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

	// Skip rendering if content doesn't contain markdown
	if !containsMarkdown(content) {
		return content
	}

	rendered, err := r.renderer.Render(content)
	if err != nil {
		return content
	}

	// Trim extra whitespace that glamour adds
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
		// Fallback to default if custom style fails
		renderer, _ = glamour.NewTermRenderer(
			glamour.WithAutoStyle(),
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
			Margin:         uintPtr(0),
		},
		BlockQuote: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: stringPtr(dimColor),
			},
			Indent:      uintPtr(1),
			IndentToken: stringPtr("│ "),
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
				Color:           stringPtr(statusColor),
				BackgroundColor: stringPtr("#1a1a2e"),
			},
		},
		CodeBlock: ansi.StyleCodeBlock{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{},
				Margin:         uintPtr(1),
			},
			Chroma: r.buildChromaConfig(),
		},
		Table: ansi.StyleTable{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{},
			},
			CenterSeparator: stringPtr("┼"),
			ColumnSeparator: stringPtr("│"),
			RowSeparator:    stringPtr("─"),
		},
		DefinitionList: ansi.StyleBlock{},
		DefinitionTerm: ansi.StylePrimitive{
			Bold: boolPtr(true),
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
			Color: stringPtr(assistantColor),
		},
		Error: ansi.StylePrimitive{
			Color: stringPtr(errorColor),
		},
		Comment: ansi.StylePrimitive{
			Color: stringPtr(dimColor),
		},
		CommentPreproc: ansi.StylePrimitive{
			Color: stringPtr(accentColor),
		},
		Keyword: ansi.StylePrimitive{
			Color: stringPtr(accentColor),
		},
		KeywordReserved: ansi.StylePrimitive{
			Color: stringPtr(accentColor),
		},
		KeywordNamespace: ansi.StylePrimitive{
			Color: stringPtr(accentColor),
		},
		KeywordType: ansi.StylePrimitive{
			Color: stringPtr(userColor),
		},
		Operator: ansi.StylePrimitive{
			Color: stringPtr(accentColor),
		},
		Punctuation: ansi.StylePrimitive{
			Color: stringPtr(assistantColor),
		},
		Name: ansi.StylePrimitive{
			Color: stringPtr(assistantColor),
		},
		NameBuiltin: ansi.StylePrimitive{
			Color: stringPtr(userColor),
		},
		NameTag: ansi.StylePrimitive{
			Color: stringPtr(accentColor),
		},
		NameAttribute: ansi.StylePrimitive{
			Color: stringPtr(successColor),
		},
		NameClass: ansi.StylePrimitive{
			Color: stringPtr(userColor),
		},
		NameConstant: ansi.StylePrimitive{
			Color: stringPtr(statusColor),
		},
		NameDecorator: ansi.StylePrimitive{
			Color: stringPtr(successColor),
		},
		NameException: ansi.StylePrimitive{
			Color: stringPtr(errorColor),
		},
		NameFunction: ansi.StylePrimitive{
			Color: stringPtr(successColor),
		},
		NameOther: ansi.StylePrimitive{
			Color: stringPtr(assistantColor),
		},
		Literal: ansi.StylePrimitive{
			Color: stringPtr(statusColor),
		},
		LiteralNumber: ansi.StylePrimitive{
			Color: stringPtr(statusColor),
		},
		LiteralDate: ansi.StylePrimitive{
			Color: stringPtr(statusColor),
		},
		LiteralString: ansi.StylePrimitive{
			Color: stringPtr(statusColor),
		},
		LiteralStringEscape: ansi.StylePrimitive{
			Color: stringPtr(accentColor),
		},
		GenericDeleted: ansi.StylePrimitive{
			Color: stringPtr(errorColor),
		},
		GenericEmph: ansi.StylePrimitive{
			Italic: boolPtr(true),
		},
		GenericInserted: ansi.StylePrimitive{
			Color: stringPtr(successColor),
		},
		GenericStrong: ansi.StylePrimitive{
			Bold: boolPtr(true),
		},
		GenericSubheading: ansi.StylePrimitive{
			Color: stringPtr(userColor),
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

	// Check for common markdown patterns
	patterns := []string{
		"```",  // Code blocks
		"**",   // Bold
		"__",   // Bold (alt)
		"# ",   // Headers
		"## ",  // H2
		"### ", // H3
		"1. ",  // Ordered list
		"> ",   // Blockquote
		"---",  // Horizontal rule
		"***",  // Horizontal rule (alt)
	}

	for _, pattern := range patterns {
		if strings.Contains(content, pattern) {
			return true
		}
	}

	// Check for inline code (backticks) but not in tool output
	if strings.Contains(content, "`") && !strings.Contains(content, "Exit Code") {
		return true
	}

	// Check for links [text](url) pattern
	if strings.Contains(content, "](") && strings.Contains(content, "[") {
		return true
	}

	return false
}

// Helper functions for creating pointers
func stringPtr(s string) *string {
	return &s
}

func boolPtr(b bool) *bool {
	return &b
}

func uintPtr(u uint) *uint {
	return &u
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
				Bold:  boolPtr(true),
				Color: stringPtr(accentColor),
			},
		},
		H1: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Bold:  boolPtr(true),
				Color: stringPtr(accentColor),
			},
		},
		H2: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Bold:  boolPtr(true),
				Color: stringPtr(statusColor),
			},
		},
		H3: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Bold:  boolPtr(true),
				Color: stringPtr(successColor),
			},
		},
		H4: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Bold:  boolPtr(true),
				Color: stringPtr(statusColor),
			},
		},
		H5: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Bold:  boolPtr(true),
				Color: stringPtr(dimColor),
			},
		},
		H6: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Bold:  boolPtr(true),
				Color: stringPtr(dimColor),
			},
		},
	}
}

// buildTextFormattingStyles creates the text formatting styles for the style config
func (r *Renderer) buildTextFormattingStyles(accentColor, dimColor, borderColor string) ansi.StyleConfig {
	return ansi.StyleConfig{
		Strikethrough: ansi.StylePrimitive{
			CrossedOut: boolPtr(true),
		},
		Emph: ansi.StylePrimitive{
			Italic: boolPtr(true),
		},
		Strong: ansi.StylePrimitive{
			Bold: boolPtr(true),
		},
		HorizontalRule: ansi.StylePrimitive{
			Color:  stringPtr(borderColor),
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
			Color:     stringPtr(accentColor),
			Underline: boolPtr(true),
		},
		LinkText: ansi.StylePrimitive{
			Color: stringPtr(accentColor),
			Bold:  boolPtr(true),
		},
		Image: ansi.StylePrimitive{
			Color: stringPtr(dimColor),
		},
		ImageText: ansi.StylePrimitive{
			Color:  stringPtr(dimColor),
			Format: "🖼  {{.text}}",
		},
	}
}
