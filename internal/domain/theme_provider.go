package domain

import (
	"fmt"

	"github.com/inference-gateway/cli/internal/ui/styles/colors"
)

// ThemeProvider implements ThemeService and manages available themes
type ThemeProvider struct {
	themes      map[string]Theme
	currentName string
}

// NewThemeProvider creates a new theme provider with default themes
func NewThemeProvider() *ThemeProvider {
	provider := &ThemeProvider{
		themes:      make(map[string]Theme),
		currentName: "tokyo-night",
	}

	provider.registerDefaultThemes()
	return provider
}

// registerDefaultThemes registers all built-in themes
func (tp *ThemeProvider) registerDefaultThemes() {
	tp.themes["tokyo-night"] = NewTokyoNightTheme()
	tp.themes["github-light"] = NewGithubLightTheme()
	tp.themes["dracula"] = NewDraculaTheme()
}

// GetTheme returns the theme by name, or the current theme if name is empty
func (tp *ThemeProvider) GetTheme(name string) (Theme, error) {
	if name == "" {
		name = tp.currentName
	}

	theme, exists := tp.themes[name]
	if !exists {
		return nil, fmt.Errorf("theme '%s' not found", name)
	}

	return theme, nil
}

// SetTheme sets the current theme by name (implements ThemeService interface)
func (tp *ThemeProvider) SetTheme(name string) error {
	if _, exists := tp.themes[name]; !exists {
		return fmt.Errorf("theme '%s' not found", name)
	}

	tp.currentName = name
	return nil
}

// GetCurrentTheme returns the currently active theme (implements ThemeService interface)
func (tp *ThemeProvider) GetCurrentTheme() Theme {
	return tp.themes[tp.currentName]
}

// GetCurrentThemeName returns the name of the currently active theme (implements ThemeService interface)
func (tp *ThemeProvider) GetCurrentThemeName() string {
	return tp.currentName
}

// ListThemes returns all available theme names (implements ThemeService interface)
func (tp *ThemeProvider) ListThemes() []string {
	names := make([]string, 0, len(tp.themes))
	for name := range tp.themes {
		names = append(names, name)
	}
	return names
}

// SetCurrentTheme is an alias for SetTheme for backward compatibility
func (tp *ThemeProvider) SetCurrentTheme(name string) error {
	return tp.SetTheme(name)
}

// TokyoNightTheme is the default theme (same as DefaultTheme)
type TokyoNightTheme struct{}

func NewTokyoNightTheme() *TokyoNightTheme {
	return &TokyoNightTheme{}
}

func (t *TokyoNightTheme) GetUserColor() string       { return colors.UserColor.ANSI }
func (t *TokyoNightTheme) GetAssistantColor() string  { return colors.AssistantColor.ANSI }
func (t *TokyoNightTheme) GetErrorColor() string      { return colors.ErrorColor.ANSI }
func (t *TokyoNightTheme) GetStatusColor() string     { return colors.StatusColor.ANSI }
func (t *TokyoNightTheme) GetAccentColor() string     { return colors.AccentColor.ANSI }
func (t *TokyoNightTheme) GetDimColor() string        { return colors.DimColor.ANSI }
func (t *TokyoNightTheme) GetBorderColor() string     { return colors.BorderColor.ANSI }
func (t *TokyoNightTheme) GetDiffAddColor() string    { return colors.DiffAddColor.ANSI }
func (t *TokyoNightTheme) GetDiffRemoveColor() string { return colors.DiffRemoveColor.ANSI }

// GithubLightTheme provides a light theme similar to GitHub's interface
type GithubLightTheme struct{}

func NewGithubLightTheme() *GithubLightTheme {
	return &GithubLightTheme{}
}

func (t *GithubLightTheme) GetUserColor() string       { return "\033[38;2;3;102;214m" }   // GitHub blue
func (t *GithubLightTheme) GetAssistantColor() string  { return "\033[38;2;36;41;46m" }    // Dark gray
func (t *GithubLightTheme) GetErrorColor() string      { return "\033[38;2;207;34;46m" }   // GitHub red
func (t *GithubLightTheme) GetStatusColor() string     { return "\033[38;2;130;80;223m" }  // GitHub purple
func (t *GithubLightTheme) GetAccentColor() string     { return "\033[38;2;3;102;214m" }   // GitHub blue
func (t *GithubLightTheme) GetDimColor() string        { return "\033[38;2;101;109;118m" } // GitHub gray
func (t *GithubLightTheme) GetBorderColor() string     { return "\033[38;2;208;215;222m" } // Light gray border
func (t *GithubLightTheme) GetDiffAddColor() string    { return "\033[38;2;40;167;69m" }   // GitHub green
func (t *GithubLightTheme) GetDiffRemoveColor() string { return "\033[38;2;207;34;46m" }   // GitHub red

// DraculaTheme provides the popular Dracula color scheme
type DraculaTheme struct{}

func NewDraculaTheme() *DraculaTheme {
	return &DraculaTheme{}
}

func (t *DraculaTheme) GetUserColor() string       { return "\033[38;2;139;233;253m" } // Cyan
func (t *DraculaTheme) GetAssistantColor() string  { return "\033[38;2;248;248;242m" } // Foreground
func (t *DraculaTheme) GetErrorColor() string      { return "\033[38;2;255;85;85m" }   // Red
func (t *DraculaTheme) GetStatusColor() string     { return "\033[38;2;189;147;249m" } // Purple
func (t *DraculaTheme) GetAccentColor() string     { return "\033[38;2;255;121;198m" } // Pink
func (t *DraculaTheme) GetDimColor() string        { return "\033[38;2;98;114;164m" }  // Comment
func (t *DraculaTheme) GetBorderColor() string     { return "\033[38;2;68;71;90m" }    // Selection
func (t *DraculaTheme) GetDiffAddColor() string    { return "\033[38;2;80;250;123m" }  // Green
func (t *DraculaTheme) GetDiffRemoveColor() string { return "\033[38;2;255;85;85m" }   // Red