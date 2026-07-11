package app

import (
	"image/color"
	"testing"

	tea "charm.land/bubbletea/v2"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
)

func TestHandleBackgroundColorDetected(t *testing.T) {
	tests := []struct {
		name            string
		configuredTheme string
		background      color.Color
		wantTheme       string
	}{
		{"light terminal, no configured theme -> light theme", "", color.White, "github-light"},
		{"dark terminal keeps dark default", "", color.Black, "tokyo-night"},
		{"configured theme wins over detection", "dracula", color.White, "tokyo-night"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Chat.Theme = tt.configuredTheme
			ts := domain.NewThemeProvider()

			app := &ChatApplication{config: cfg, themeService: ts}
			app.handleBackgroundColorDetected(tea.BackgroundColorMsg{Color: tt.background})

			if got := ts.GetCurrentThemeName(); got != tt.wantTheme {
				t.Errorf("theme = %q, want %q", got, tt.wantTheme)
			}
		})
	}
}
