package config_test

import (
	"os"
	"path/filepath"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

const browserUseValidYAML = `---
enabled: true
browser:
  channel: msedge
  headless: true
  cdp_endpoint: http://localhost:9222
  timeout_seconds: 10
rate_limit:
  enabled: false
  max_actions_per_minute: 30
  window_seconds: 30
tools:
  navigate:
    enabled: true
  click:
    enabled: false
  type:
    enabled: false
  read:
    enabled: true
`

func TestBrowserUseDefaults(t *testing.T) {
	cfg := config.DefaultBrowserUseConfig()

	if cfg.Enabled {
		t.Error("Expected browser use disabled by default")
	}
	if cfg.Browser.Channel != "chrome" {
		t.Errorf("Expected default channel 'chrome', got %q", cfg.Browser.Channel)
	}
	if cfg.Browser.TimeoutSeconds != 30 {
		t.Errorf("Expected default timeout 30s, got %d", cfg.Browser.TimeoutSeconds)
	}
	if !cfg.RateLimit.Enabled {
		t.Error("Expected rate limiting enabled by default")
	}
	for name, enabled := range map[string]bool{
		"navigate": cfg.Tools.Navigate.Enabled,
		"click":    cfg.Tools.Click.Enabled,
		"type":     cfg.Tools.Type.Enabled,
		"read":     cfg.Tools.Read.Enabled,
	} {
		if !enabled {
			t.Errorf("Expected tool %s enabled by default", name)
		}
	}
}

func TestLoadBrowserUse(t *testing.T) {
	t.Run("missing file returns defaults", func(t *testing.T) {
		cfg, err := config.LoadBrowserUse(filepath.Join(t.TempDir(), "nope.yaml"))
		if err != nil {
			t.Fatalf("LoadBrowserUse() err = %v", err)
		}
		if cfg.Enabled || cfg.Browser.Channel != "chrome" {
			t.Errorf("Expected defaults, got %+v", cfg)
		}
	})

	t.Run("valid file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), config.BrowserUseFileName)
		if err := os.WriteFile(path, []byte(browserUseValidYAML), 0644); err != nil {
			t.Fatal(err)
		}
		cfg, err := config.LoadBrowserUse(path)
		if err != nil {
			t.Fatalf("LoadBrowserUse() err = %v", err)
		}
		if !cfg.Enabled || cfg.Browser.Channel != "msedge" || !cfg.Browser.Headless ||
			cfg.Browser.CDPEndpoint != "http://localhost:9222" || cfg.Browser.TimeoutSeconds != 10 {
			t.Errorf("Unexpected browser config: %+v", cfg.Browser)
		}
		if cfg.RateLimit.Enabled || cfg.RateLimit.MaxActionsPerMinute != 30 {
			t.Errorf("Unexpected rate limit config: %+v", cfg.RateLimit)
		}
		if !cfg.Tools.Navigate.Enabled || cfg.Tools.Click.Enabled || cfg.Tools.Type.Enabled || !cfg.Tools.Read.Enabled {
			t.Errorf("Unexpected tools config: %+v", cfg.Tools)
		}
	})
}

func TestSaveBrowserUseRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), config.BrowserUseFileName)
	original := config.DefaultBrowserUseConfig()
	original.Enabled = true
	original.Browser.Headless = true

	if err := config.SaveBrowserUse(path, original); err != nil {
		t.Fatalf("SaveBrowserUse() err = %v", err)
	}
	loaded, err := config.LoadBrowserUse(path)
	if err != nil {
		t.Fatalf("LoadBrowserUse() err = %v", err)
	}
	if !loaded.Enabled || !loaded.Browser.Headless || loaded.Browser.Channel != "chrome" {
		t.Errorf("Round trip mismatch: %+v", loaded)
	}
}
