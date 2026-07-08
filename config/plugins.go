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

// PluginsConfig is the userspace ~/.infer/plugins.yaml registry of installed
// plugins. It is created lazily by `infer plugins`, never seeded by `infer init`.
type PluginsConfig struct {
	Enabled              bool          `yaml:"enabled" mapstructure:"enabled"`
	Dir                  string        `yaml:"dir,omitempty" mapstructure:"dir,omitempty"`
	MaxInstructionsChars int           `yaml:"max_instructions_chars,omitempty" mapstructure:"max_instructions_chars,omitempty"`
	MaxInstructionsLines int           `yaml:"max_instructions_lines,omitempty" mapstructure:"max_instructions_lines,omitempty"`
	Plugins              []PluginEntry `yaml:"plugins" mapstructure:"plugins"`

	path string
}

// PluginEntry is one installed plugin in the registry.
type PluginEntry struct {
	Name         string `yaml:"name" mapstructure:"name"`
	Source       string `yaml:"source" mapstructure:"source"`
	Ref          string `yaml:"ref,omitempty" mapstructure:"ref,omitempty"`
	Version      string `yaml:"version,omitempty" mapstructure:"version,omitempty"`
	Enabled      bool   `yaml:"enabled" mapstructure:"enabled"`
	HooksEnabled bool   `yaml:"hooks_enabled,omitempty" mapstructure:"hooks_enabled,omitempty"`
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

// LoadPlugins reads plugins.yaml from disk, returning defaults when the file
// is missing.
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

// SavePlugins writes the plugins registry to disk.
func SavePlugins(path string, cfg *PluginsConfig) error {
	return utils.SaveYAML(path, "Plugins", cfg)
}

// ResolveDir returns the plugins storage root: Dir when set, otherwise
// ~/.infer/plugins.
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

// PluginHooksPath returns the hooks.yaml path of an installed plugin.
func (c PluginsConfig) PluginHooksPath(name string) (string, error) {
	root, err := c.PluginRoot(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, HooksFileName), nil
}

// EnabledEntries returns the enabled plugins in registry order, or nil when
// the master switch is off.
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
