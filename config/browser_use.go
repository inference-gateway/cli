package config

import (
	configutils "github.com/inference-gateway/cli/config/utils"
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
	Backend   string                `yaml:"backend" mapstructure:"backend"`
	Browser   BrowserConfig         `yaml:"browser" mapstructure:"browser"`
	Extension ExtensionConfig       `yaml:"extension" mapstructure:"extension"`
	RateLimit RateLimitConfig       `yaml:"rate_limit" mapstructure:"rate_limit"`
	Tools     BrowserUseToolsConfig `yaml:"tools" mapstructure:"tools"`
}

// BrowserBackendExtension selects the opentask extension bridge backend.
const BrowserBackendExtension = "extension"

// DefaultExtensionPort is used when extension.port is absent from
// browser_use.yaml (LoadYAML unmarshals into a zero struct, so files written
// before the extension section existed load with Port 0).
const DefaultExtensionPort = 52789

// EffectivePort returns the configured port, falling back to the default.
func (e ExtensionConfig) EffectivePort() int {
	if e.Port > 0 {
		return e.Port
	}
	return DefaultExtensionPort
}

// ExtensionConfig configures the localhost WebSocket bridge the opentask
// browser extension dials into when backend is "extension".
type ExtensionConfig struct {
	// Port the CLI listens on (127.0.0.1 only) for the extension connection.
	Port int `yaml:"port" mapstructure:"port"`
	// Token is the shared secret the extension must present in its hello
	// message. Required when backend is "extension".
	Token string `yaml:"token" mapstructure:"token"`
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
	Navigate   BrowserToolConfig `yaml:"navigate" mapstructure:"navigate"`
	Click      BrowserToolConfig `yaml:"click" mapstructure:"click"`
	Type       BrowserToolConfig `yaml:"type" mapstructure:"type"`
	Read       BrowserToolConfig `yaml:"read" mapstructure:"read"`
	Screenshot BrowserToolConfig `yaml:"screenshot" mapstructure:"screenshot"`
	Tabs       BrowserToolConfig `yaml:"tabs" mapstructure:"tabs"`
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
		Backend: "playwright",
		Browser: BrowserConfig{
			Channel:        "chrome",
			Headless:       false,
			CDPEndpoint:    "",
			TimeoutSeconds: 30,
		},
		Extension: ExtensionConfig{
			Port:  DefaultExtensionPort,
			Token: "",
		},
		RateLimit: RateLimitConfig{
			Enabled:             true,
			MaxActionsPerMinute: 60,
			WindowSeconds:       60,
		},
		Tools: BrowserUseToolsConfig{
			Navigate:   BrowserToolConfig{Enabled: true},
			Click:      BrowserToolConfig{Enabled: true},
			Type:       BrowserToolConfig{Enabled: true},
			Read:       BrowserToolConfig{Enabled: true},
			Screenshot: BrowserToolConfig{Enabled: true},
			Tabs:       BrowserToolConfig{Enabled: true},
		},
	}
}

// LoadBrowserUse reads browser_use.yaml from disk. When the file is missing
// it returns the in-code defaults so callers can treat absence as "use
// defaults" without special-casing.
func LoadBrowserUse(path string) (*BrowserUseConfig, error) {
	return configutils.LoadYAML(path, "browser_use", DefaultBrowserUseConfig)
}

// SaveBrowserUse writes the browser_use configuration to disk, creating any
// missing parent directories.
func SaveBrowserUse(path string, cfg *BrowserUseConfig) error {
	return configutils.SaveYAML(path, "browser_use", cfg)
}
