package config

import (
	"fmt"

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

// Binding is the AG-UI binding as the config resolves it: daemon.yaml's own
// values, with browser_use.extension's port and token as the fallback, and
// on when the daemon switch or the extension backend needs it.
type Binding struct {
	Enabled bool
	Port    int
	Token   string
}

// Addr is the loopback address a client dials.
func (b Binding) Addr() string {
	return fmt.Sprintf("127.0.0.1:%d", b.Port)
}

// Dialable reports whether a client can reach the binding: it is on and has
// a token to present.
func (b Binding) Dialable() bool {
	return b.Enabled && b.Token != ""
}

// Binding resolves the AG-UI binding from daemon.yaml and browser_use.yaml.
func (c *Config) Binding() Binding {
	b := Binding{
		Enabled: c.Daemon.Binding.Enabled || c.BrowserUse.Enabled && c.BrowserUse.Backend == BrowserBackendExtension,
		Port:    c.Daemon.Binding.Port,
		Token:   c.Daemon.Binding.Token,
	}
	if b.Port <= 0 {
		b.Port = c.BrowserUse.Extension.EffectivePort()
	}
	if b.Token == "" {
		b.Token = c.BrowserUse.Extension.Token
	}
	return b
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
