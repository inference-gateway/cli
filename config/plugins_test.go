package config

import (
	"path/filepath"
	"testing"

	require "github.com/stretchr/testify/require"
)

func TestLoadPlugins_MissingFileReturnsDefaults(t *testing.T) {
	cfg, err := LoadPlugins(filepath.Join(t.TempDir(), PluginsFileName))
	require.NoError(t, err)
	require.True(t, cfg.Enabled)
	require.Equal(t, DefaultInstructionsMaxChars, cfg.MaxInstructionsChars)
	require.Equal(t, DefaultInstructionsMaxLines, cfg.MaxInstructionsLines)
	require.Empty(t, cfg.Plugins)
}

func TestPlugins_SaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), PluginsFileName)
	cfg := DefaultPluginsConfig()
	cfg.Plugins = []PluginEntry{
		{Name: "ponytail", Source: "DietrichGebert/ponytail", Ref: "main", Version: "4.8.4", Enabled: true},
		{Name: "disabled-one", Source: "org/repo", Enabled: false},
	}
	require.NoError(t, SavePlugins(path, cfg))

	loaded, err := LoadPlugins(path)
	require.NoError(t, err)
	require.Len(t, loaded.Plugins, 2)
	require.Equal(t, "ponytail", loaded.Plugins[0].Name)
	require.True(t, loaded.Plugins[0].Enabled)
	require.False(t, loaded.Plugins[1].Enabled, "explicit enabled: false must survive the round trip")
}

func TestPlugins_CRUD(t *testing.T) {
	path := filepath.Join(t.TempDir(), PluginsFileName)
	cfg := DefaultPluginsConfig()
	cfg.path = path

	entry := PluginEntry{Name: "ponytail", Source: "DietrichGebert/ponytail", Enabled: true}
	require.NoError(t, cfg.CreateEntry(entry))
	require.Error(t, cfg.CreateEntry(entry), "duplicate name must be rejected")

	got, err := cfg.ReadEntry("ponytail")
	require.NoError(t, err)
	require.Equal(t, "DietrichGebert/ponytail", got.Source)

	entry.Enabled = false
	require.NoError(t, cfg.UpdateEntry(entry))
	got, err = cfg.ReadEntry("ponytail")
	require.NoError(t, err)
	require.False(t, got.Enabled)

	_, err = cfg.ReadEntry("nope")
	require.Error(t, err)

	require.NoError(t, cfg.DeleteEntry("ponytail"))
	require.Error(t, cfg.DeleteEntry("ponytail"))
	require.Empty(t, cfg.ListEntries())
}

func TestPlugins_ResolveDirAndPaths(t *testing.T) {
	dir := t.TempDir()
	cfg := PluginsConfig{Dir: dir}

	resolved, err := cfg.ResolveDir()
	require.NoError(t, err)
	require.Equal(t, dir, resolved)

	skillsDir, err := cfg.PluginSkillsDir("ponytail")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dir, "ponytail", "skills"), skillsDir)

	instr, err := cfg.PluginInstructionsPath("ponytail")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dir, "ponytail", PluginAgentsMDName), instr)
}

func TestPlugins_EnabledEntries(t *testing.T) {
	cfg := PluginsConfig{
		Enabled: true,
		Plugins: []PluginEntry{
			{Name: "a", Enabled: true},
			{Name: "b", Enabled: false},
			{Name: "c", Enabled: true},
		},
	}
	names := []string{}
	for _, p := range cfg.EnabledEntries() {
		names = append(names, p.Name)
	}
	require.Equal(t, []string{"a", "c"}, names)

	cfg.Enabled = false
	require.Empty(t, cfg.EnabledEntries(), "master switch off must disable all")
}

func TestValidatePathInSandbox_PluginsCarveOut(t *testing.T) {
	t.Chdir(t.TempDir())
	pluginsDir := filepath.Join(ConfigDirName, PluginsDirName)
	cfg := DefaultConfig()
	cfg.Plugins = *DefaultPluginsConfig()
	cfg.Plugins.Dir = pluginsDir

	skillPath := filepath.Join(pluginsDir, "ponytail", "skills", "ponytail", "SKILL.md")
	require.NoError(t, cfg.ValidatePathInSandbox(skillPath))

	envPath := filepath.Join(pluginsDir, "ponytail", ".env")
	require.Error(t, cfg.ValidatePathInSandbox(envPath), "file-level protections must still apply inside the plugins dir")

	cfg.Plugins.Enabled = false
	require.Error(t, cfg.ValidatePathInSandbox(skillPath), "carve-out must be gated on plugins.enabled")
}
