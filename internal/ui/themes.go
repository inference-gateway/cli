package ui

import (
	"fmt"

	"github.com/inference-gateway/cli/internal/ui/shared"
	"github.com/inference-gateway/cli/internal/ui/styles/colors"
)

// ThemeProvider manages available themes
type ThemeProvider struct {
	themes      map[string]shared.Theme
	currentName string
}

// NewThemeProvider creates a new theme provider with default themes
func NewThemeProvider() *ThemeProvider {
	provider := &ThemeProvider{
		themes:      make(map[string]shared.Theme),
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
func (tp *ThemeProvider) GetTheme(name string) (shared.Theme, error) {
	if name == "" {
		name = tp.currentName
	}

	theme, exists := tp.themes[name]
	if !exists {
		return nil, fmt.Errorf("theme '%s' not found", name)
	}

	return theme, nil
}

// SetCurrentTheme sets the current theme by name
func (tp *ThemeProvider) SetCurrentTheme(name string) error {
	if _, exists := tp.themes[name]; !exists {
		return fmt.Errorf("theme '%s' not found", name)
	}

	tp.currentName = name
	return nil
}

// GetCurrentTheme returns the currently active theme
func (tp *ThemeProvider) GetCurrentTheme() shared.Theme {
	return tp.themes[tp.currentName]
}

// GetCurrentThemeName returns the name of the currently active theme
func (tp *ThemeProvider) GetCurrentThemeName() string {
	return tp.currentName
}

// ListThemes returns all available theme names
func (tp *ThemeProvider) ListThemes() []string {
	names := make([]string, 0, len(tp.themes))
	for name := range tp.themes {
		names = append(names, name)
	}
	return names
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

func (t *GithubLightTheme) GetUserColor() string       { return colors.GithubUserColor.Lipgloss }
func (t *GithubLightTheme) GetAssistantColor() string  { return colors.GithubAssistantColor.Lipgloss }
func (t *GithubLightTheme) GetErrorColor() string      { return colors.GithubErrorColor.Lipgloss }
func (t *GithubLightTheme) GetStatusColor() string     { return colors.GithubStatusColor.Lipgloss }
func (t *GithubLightTheme) GetAccentColor() string     { return colors.GithubAccentColor.Lipgloss }
func (t *GithubLightTheme) GetDimColor() string        { return colors.GithubDimColor.Lipgloss }
func (t *GithubLightTheme) GetBorderColor() string     { return colors.GithubBorderColor.Lipgloss }
func (t *GithubLightTheme) GetDiffAddColor() string    { return colors.GithubDiffAddColor.Lipgloss }
func (t *GithubLightTheme) GetDiffRemoveColor() string { return colors.GithubDiffRemoveColor.Lipgloss }

// DraculaTheme provides the popular Dracula color scheme
type DraculaTheme struct{}

func NewDraculaTheme() *DraculaTheme {
	return &DraculaTheme{}
}

func (t *DraculaTheme) GetUserColor() string       { return colors.DraculaUserColor.Lipgloss }
func (t *DraculaTheme) GetAssistantColor() string  { return colors.DraculaAssistantColor.Lipgloss }
func (t *DraculaTheme) GetErrorColor() string      { return colors.DraculaErrorColor.Lipgloss }
func (t *DraculaTheme) GetStatusColor() string     { return colors.DraculaStatusColor.Lipgloss }
func (t *DraculaTheme) GetAccentColor() string     { return colors.DraculaAccentColor.Lipgloss }
func (t *DraculaTheme) GetDimColor() string        { return colors.DraculaDimColor.Lipgloss }
func (t *DraculaTheme) GetBorderColor() string     { return colors.DraculaBorderColor.Lipgloss }
func (t *DraculaTheme) GetDiffAddColor() string    { return colors.DraculaDiffAddColor.Lipgloss }
func (t *DraculaTheme) GetDiffRemoveColor() string { return colors.DraculaDiffRemoveColor.Lipgloss }
