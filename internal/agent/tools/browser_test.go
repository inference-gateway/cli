package tools

import (
	"fmt"
	"strings"
	"testing"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	utils "github.com/inference-gateway/cli/internal/utils"
)

func browserTestConfig(enabled bool) *config.Config {
	cfg := &config.Config{}
	cfg.BrowserUse = *config.DefaultBrowserUseConfig()
	cfg.BrowserUse.Enabled = enabled
	cfg.Prompts = *config.DefaultPromptsConfig()
	return cfg
}

func newBrowserTestTools(cfg *config.Config) []domain.Tool {
	session := newBrowserSession(&cfg.BrowserUse)
	limiter := utils.NewRateLimiter(cfg.BrowserUse.RateLimit)
	return []domain.Tool{
		NewBrowserNavigateTool(cfg, limiter, session),
		NewBrowserClickTool(cfg, limiter, session),
		NewBrowserTypeTool(cfg, limiter, session),
		NewBrowserReadTool(cfg, limiter, session),
	}
}

func TestBrowserToolsEnablement(t *testing.T) {
	for _, tool := range newBrowserTestTools(browserTestConfig(true)) {
		if !tool.IsEnabled() {
			t.Errorf("%s should be enabled", tool.Definition().Function.Name)
		}
	}
	for _, tool := range newBrowserTestTools(browserTestConfig(false)) {
		if tool.IsEnabled() {
			t.Errorf("%s should be disabled when browser use is off", tool.Definition().Function.Name)
		}
	}

	cfg := browserTestConfig(true)
	cfg.BrowserUse.Tools.Click.Enabled = false
	for _, tool := range newBrowserTestTools(cfg) {
		name := tool.Definition().Function.Name
		if name == "BrowserClick" && tool.IsEnabled() {
			t.Error("BrowserClick should honor its per-tool enable flag")
		}
		if name == "BrowserNavigate" && !tool.IsEnabled() {
			t.Error("BrowserNavigate should stay enabled")
		}
	}
}

func TestBrowserToolsValidate(t *testing.T) {
	cfg := browserTestConfig(true)
	session := newBrowserSession(&cfg.BrowserUse)
	limiter := utils.NewRateLimiter(cfg.BrowserUse.RateLimit)

	tests := []struct {
		name    string
		tool    domain.Tool
		args    map[string]any
		wantErr bool
	}{
		{"navigate valid", NewBrowserNavigateTool(cfg, limiter, session), map[string]any{"url": "https://example.com"}, false},
		{"navigate missing url", NewBrowserNavigateTool(cfg, limiter, session), map[string]any{}, true},
		{"navigate relative url", NewBrowserNavigateTool(cfg, limiter, session), map[string]any{"url": "/foo"}, true},
		{"click valid", NewBrowserClickTool(cfg, limiter, session), map[string]any{"selector": "text=Sign in"}, false},
		{"click missing selector", NewBrowserClickTool(cfg, limiter, session), map[string]any{}, true},
		{"type valid", NewBrowserTypeTool(cfg, limiter, session), map[string]any{"selector": "input", "text": "hi"}, false},
		{"type missing text", NewBrowserTypeTool(cfg, limiter, session), map[string]any{"selector": "input"}, true},
		{"read no args", NewBrowserReadTool(cfg, limiter, session), map[string]any{}, false},
		{"read bad selector type", NewBrowserReadTool(cfg, limiter, session), map[string]any{"selector": 42}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.tool.Validate(tt.args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate(%v) err = %v, wantErr %v", tt.args, err, tt.wantErr)
			}
		})
	}
}

func TestBrowserSessionEventBuffer(t *testing.T) {
	session := newBrowserSession(config.DefaultBrowserUseConfig())

	for i := range maxBrowserEvents + 10 {
		session.recordEvent(fmt.Sprintf("event %d", i))
	}
	events := session.DrainEvents()
	if len(events) != maxBrowserEvents {
		t.Fatalf("Expected %d buffered events, got %d", maxBrowserEvents, len(events))
	}
	if events[0] != "event 10" {
		t.Errorf("Expected oldest events dropped, first is %q", events[0])
	}
	if len(session.DrainEvents()) != 0 {
		t.Error("Expected buffer cleared after drain")
	}
}

func TestBrowserToolFormatForLLM(t *testing.T) {
	cfg := browserTestConfig(true)
	session := newBrowserSession(&cfg.BrowserUse)
	limiter := utils.NewRateLimiter(cfg.BrowserUse.RateLimit)
	tool := NewBrowserReadTool(cfg, limiter, session)

	result := &domain.ToolExecutionResult{
		ToolName: "BrowserRead",
		Success:  true,
		Data: domain.BrowserToolResult{
			Action:  "read",
			URL:     "https://example.com",
			Title:   "Example",
			Content: "hello world",
			Events:  []string{"console.log: hi"},
		},
	}
	out := tool.FormatForLLM(result)
	for _, want := range []string{"read", "https://example.com", "Example", "hello world", "console.log: hi"} {
		if !strings.Contains(out, want) {
			t.Errorf("FormatForLLM output missing %q:\n%s", want, out)
		}
	}

	failed := &domain.ToolExecutionResult{ToolName: "BrowserRead", Success: false, Error: "boom"}
	if out := tool.FormatForLLM(failed); !strings.Contains(out, "boom") {
		t.Errorf("Expected error in output, got %q", out)
	}
}
