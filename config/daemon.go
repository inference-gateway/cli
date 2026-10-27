package config

import (
	configutils "github.com/inference-gateway/cli/config/utils"
)

const (
	DaemonFileName   = "daemon.yaml"
	DefaultDaemonDir = ConfigDirName + "/" + DaemonFileName
)

// DaemonConfig contains settings for the `infer daemon` command itself, the
// long-running host of the channels, scheduler, heartbeat and the AG-UI
// binding. Stored in its own daemon.yaml sidecar.
type DaemonConfig struct {
	Binding BindingConfig `yaml:"binding" mapstructure:"binding"`
}

// BindingConfig configures the AG-UI WebSocket binding the daemon hosts for
// the desktop app, the opentask extension and the channel clients. It serves
// on its own switch, port and token; browser_use.extension's port and token
// stay the fallback.
type BindingConfig struct {
	// Enabled turns the binding on for the daemon, independent of
	// browser_use, so every client of the daemon's threads can reach it.
	Enabled bool `yaml:"enabled" mapstructure:"enabled"`
	// Port the binding listens on (127.0.0.1 only). Empty falls back to
	// browser_use.extension.port.
	Port int `yaml:"port" mapstructure:"port"`
	// Token is the shared secret clients must present in their hello.
	// Empty falls back to browser_use.extension.token.
	Token string `yaml:"token" mapstructure:"token"`
}

// BindingAddress returns the port and token the binding serves on. The
// daemon.yaml values win, and browser_use.extension's port and token stay
// the fallback.
func (c *Config) BindingAddress() (port int, token string) {
	port = c.Daemon.Binding.Port
	if port <= 0 {
		port = c.BrowserUse.Extension.EffectivePort()
	}
	token = c.Daemon.Binding.Token
	if token == "" {
		token = c.BrowserUse.Extension.Token
	}
	return port, token
}

// BindingEnabled reports whether the daemon hosts the AG-UI binding: on its
// own switch, or because the browser extension backend needs it.
func (c *Config) BindingEnabled() bool {
	if c.Daemon.Binding.Enabled {
		return true
	}
	return c.BrowserUse.Enabled && c.BrowserUse.Backend == BrowserBackendExtension
}

// BindingConfigured reports whether a client of the daemon's threads can dial
// the binding: it is enabled and has a token to present.
func (c *Config) BindingConfigured() bool {
	_, token := c.BindingAddress()
	return c.BindingEnabled() && token != ""
}

// DefaultDaemonConfig returns the in-code defaults used when no daemon.yaml
// exists: the binding is off until the user or the desktop app enables it.
func DefaultDaemonConfig() *DaemonConfig {
	return &DaemonConfig{}
}

// LoadDaemon reads daemon.yaml from disk. When the file is missing it
// returns the in-code defaults so callers can treat absence as "use
// defaults" without special-casing.
func LoadDaemon(path string) (*DaemonConfig, error) {
	return configutils.LoadYAML(path, "daemon", DefaultDaemonConfig)
}
