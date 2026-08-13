package config

import (
	utils "github.com/inference-gateway/cli/config/utils"
)

const (
	BrowserUseFileName    = "browser_use.yaml"
	DefaultBrowserUsePath = ConfigDirName + "/" + BrowserUseFileName
)

// BrowserUseConfig contains browser automation tool settings. It mirrors
// ComputerUseConfig: a global enabled flag, per-tool enable flags, and rate
// limiting, stored in its own browser_use.yaml file.
type BrowserUseConfig struct {
	Enabled   bool                  `yaml:"enabled" mapstructure:"enabled"`
	Browser   BrowserConfig         `yaml:"browser" mapstructure:"browser"`
	RateLimit RateLimitConfig       `yaml:"rate_limit" mapstructure:"rate_limit"`
	Tools     BrowserUseToolsConfig `yaml:"tools" mapstructure:"tools"`
}

// BrowserConfig contains settings for launching or attaching to the browser.
type BrowserConfig struct {
	// Channel selects the locally installed browser to drive (e.g. "chrome",
	// "msedge", "chromium"). Empty means Playwright's bundled Chromium.
	Channel  string `yaml:"channel" mapstructure:"channel"`
	Headless bool   `yaml:"headless" mapstructure:"headless"`
	// CDPEndpoint attaches to an already-running browser over the Chrome
	// DevTools Protocol (e.g. "http://localhost:9222") instead of launching one.
	CDPEndpoint string `yaml:"cdp_endpoint" mapstructure:"cdp_endpoint"`
	// TimeoutSeconds bounds navigations and element interactions.
	TimeoutSeconds int `yaml:"timeout_seconds" mapstructure:"timeout_seconds"`
}

// BrowserUseToolsConfig contains individual browser use tool settings.
type BrowserUseToolsConfig struct {
	Navigate BrowserToolConfig `yaml:"navigate" mapstructure:"navigate"`
	Click    BrowserToolConfig `yaml:"click" mapstructure:"click"`
	Type     BrowserToolConfig `yaml:"type" mapstructure:"type"`
	Read     BrowserToolConfig `yaml:"read" mapstructure:"read"`
}

// BrowserToolConfig contains per-tool settings.
type BrowserToolConfig struct {
	Enabled bool `yaml:"enabled" mapstructure:"enabled"`
}

// DefaultBrowserUseConfig returns the in-code default browser_use
// configuration used when no browser_use.yaml file exists. `infer init`
// seeds the file from this and the runtime falls back to it when the file
// is absent.
func DefaultBrowserUseConfig() *BrowserUseConfig {
	return &BrowserUseConfig{
		Enabled: false,
		Browser: BrowserConfig{
			Channel:        "chrome",
			Headless:       false,
			CDPEndpoint:    "",
			TimeoutSeconds: 30,
		},
		RateLimit: RateLimitConfig{
			Enabled:             true,
			MaxActionsPerMinute: 60,
			WindowSeconds:       60,
		},
		Tools: BrowserUseToolsConfig{
			Navigate: BrowserToolConfig{Enabled: true},
			Click:    BrowserToolConfig{Enabled: true},
			Type:     BrowserToolConfig{Enabled: true},
			Read:     BrowserToolConfig{Enabled: true},
		},
	}
}

// LoadBrowserUse reads browser_use.yaml from disk. When the file is missing
// it returns the in-code defaults so callers can treat absence as "use
// defaults" without special-casing.
func LoadBrowserUse(path string) (*BrowserUseConfig, error) {
	return utils.LoadYAML(path, "browser_use", DefaultBrowserUseConfig)
}

// SaveBrowserUse writes the browser_use configuration to disk, creating any
// missing parent directories.
func SaveBrowserUse(path string, cfg *BrowserUseConfig) error {
	return utils.SaveYAML(path, "browser_use", cfg)
}
