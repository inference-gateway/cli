package config

import (
	"fmt"
	"os"
	"path/filepath"

	utils "github.com/inference-gateway/cli/config/utils"
)

const (
	PluginsFileName    = "plugins.yaml"
	PluginsDirName     = "plugins"
	PluginAgentsMDName = "AGENTS.md"
	PluginManifestPath = ".claude-plugin/plugin.json"
)

// PluginsConfig represents the userspace-only ~/.infer/plugins.yaml registry
// of installed Claude Code-format plugins. Unlike other sidecar files it is
// never seeded by `infer init` - it is created lazily by the `infer plugins`
// commands, so `init --overwrite` can never clobber it.
type PluginsConfig struct {
	Enabled              bool          `yaml:"enabled" mapstructure:"enabled"`
	Dir                  string        `yaml:"dir,omitempty" mapstructure:"dir,omitempty"`
	MaxInstructionsChars int           `yaml:"max_instructions_chars,omitempty" mapstructure:"max_instructions_chars,omitempty"`
	MaxInstructionsLines int           `yaml:"max_instructions_lines,omitempty" mapstructure:"max_instructions_lines,omitempty"`
	Plugins              []PluginEntry `yaml:"plugins" mapstructure:"plugins"`

	path string
}

var _ CollectionConfig[PluginEntry] = (*PluginsConfig)(nil)

// PluginEntry is one installed plugin in the registry. Source keeps the
// canonical install source ("owner/repo", a github.com URL, or an absolute
// local path) so `infer plugins update` can re-fetch it.
type PluginEntry struct {
	Name    string `yaml:"name" mapstructure:"name"`
	Source  string `yaml:"source" mapstructure:"source"`
	Ref     string `yaml:"ref,omitempty" mapstructure:"ref,omitempty"`
	Version string `yaml:"version,omitempty" mapstructure:"version,omitempty"`
	Enabled bool   `yaml:"enabled" mapstructure:"enabled"`
}

// DefaultPluginsConfig returns the default plugins configuration.
func DefaultPluginsConfig() *PluginsConfig {
	return &PluginsConfig{
		Enabled:              true,
		MaxInstructionsChars: DefaultInstructionsMaxChars,
		MaxInstructionsLines: DefaultInstructionsMaxLines,
		Plugins:              []PluginEntry{},
	}
}

// LoadPlugins reads plugins.yaml from disk. When the file is missing it
// returns the in-code defaults so callers can treat absence as "no plugins
// installed" without special-casing.
func LoadPlugins(path string) (*PluginsConfig, error) {
	cfg, err := utils.LoadYAML(path, "Plugins", DefaultPluginsConfig)
	if err != nil {
		return nil, err
	}
	if cfg.MaxInstructionsChars <= 0 {
		cfg.MaxInstructionsChars = DefaultInstructionsMaxChars
	}
	if cfg.MaxInstructionsLines <= 0 {
		cfg.MaxInstructionsLines = DefaultInstructionsMaxLines
	}
	cfg.path = path
	return cfg, nil
}

// SavePlugins writes the plugins registry to disk, creating any missing
// parent directories.
func SavePlugins(path string, cfg *PluginsConfig) error {
	return utils.SaveYAML(path, "Plugins", cfg)
}

// ResolveDir returns the plugins storage root: the Dir override when set
// (mainly for tests), otherwise ~/.infer/plugins.
func (c PluginsConfig) ResolveDir() (string, error) {
	if c.Dir != "" {
		return c.Dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, ConfigDirName, PluginsDirName), nil
}

// PluginRoot returns the on-disk root of an installed plugin.
func (c PluginsConfig) PluginRoot(name string) (string, error) {
	dir, err := c.ResolveDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

// PluginSkillsDir returns the skills directory of an installed plugin.
func (c PluginsConfig) PluginSkillsDir(name string) (string, error) {
	root, err := c.PluginRoot(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "skills"), nil
}

// PluginInstructionsPath returns the AGENTS.md path of an installed plugin.
func (c PluginsConfig) PluginInstructionsPath(name string) (string, error) {
	root, err := c.PluginRoot(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, PluginAgentsMDName), nil
}

// EnabledEntries returns the registered plugins that are individually
// enabled, in registry order. Empty when the master switch is off.
func (c PluginsConfig) EnabledEntries() []PluginEntry {
	if !c.Enabled {
		return nil
	}
	var out []PluginEntry
	for _, p := range c.Plugins {
		if p.Enabled {
			out = append(out, p)
		}
	}
	return out
}

func pluginName(e PluginEntry) string { return e.Name }

const pluginKind = "plugin"

// CreateEntry implements CollectionConfig.
func (c *PluginsConfig) CreateEntry(entry PluginEntry) error {
	next, err := appendEntry(c.Plugins, entry, entry.Name, pluginName, pluginKind)
	if err != nil {
		return err
	}
	c.Plugins = next
	return SavePlugins(c.path, c)
}

// ReadEntry implements CollectionConfig.
func (c *PluginsConfig) ReadEntry(name string) (*PluginEntry, error) {
	return findEntry(c.Plugins, name, pluginName, pluginKind)
}

// UpdateEntry implements CollectionConfig.
func (c *PluginsConfig) UpdateEntry(entry PluginEntry) error {
	next, err := replaceEntry(c.Plugins, entry, entry.Name, pluginName, pluginKind)
	if err != nil {
		return err
	}
	c.Plugins = next
	return SavePlugins(c.path, c)
}

// DeleteEntry implements CollectionConfig.
func (c *PluginsConfig) DeleteEntry(name string) error {
	next, err := removeEntry(c.Plugins, name, pluginName, pluginKind)
	if err != nil {
		return err
	}
	c.Plugins = next
	return SavePlugins(c.path, c)
}

// ListEntries implements CollectionConfig.
func (c *PluginsConfig) ListEntries() []PluginEntry { return c.Plugins }
