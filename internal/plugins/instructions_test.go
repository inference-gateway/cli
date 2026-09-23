package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	require "github.com/stretchr/testify/require"

	config "github.com/inference-gateway/cli/config"
)

// writePlugin lays out <dir>/<name>/AGENTS.md and returns a config whose
// registry lists the given plugins.
func pluginsCfg(t *testing.T, dir string, entries ...config.PluginEntry) *config.Config {
	t.Helper()
	cfg := &config.Config{}
	cfg.Plugins = *config.DefaultPluginsConfig()
	cfg.Plugins.Dir = dir
	cfg.Plugins.Plugins = entries
	return cfg
}

func writeInstructions(t *testing.T, dir, name, content string) {
	t.Helper()
	root := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, config.PluginAgentsMDName), []byte(content), 0o644))
}

func TestInstructions_EnabledPluginsOnly(t *testing.T) {
	dir := t.TempDir()
	writeInstructions(t, dir, "on", "be lazy")
	writeInstructions(t, dir, "off", "never seen")
	cfg := pluginsCfg(t, dir,
		config.PluginEntry{Name: "on", Enabled: true},
		config.PluginEntry{Name: "off", Enabled: false},
	)

	got := Instructions(cfg)
	require.Len(t, got, 1)
	require.Equal(t, "on", got[0].PluginName)
	require.Equal(t, "be lazy", got[0].Content)
}

func TestInstructions_MissingAgentsMDSkipped(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "skills-only"), 0o755))
	cfg := pluginsCfg(t, dir, config.PluginEntry{Name: "skills-only", Enabled: true})
	require.Empty(t, Instructions(cfg))
}

func TestInstructions_MasterSwitchOff(t *testing.T) {
	dir := t.TempDir()
	writeInstructions(t, dir, "p", "rules")
	cfg := pluginsCfg(t, dir, config.PluginEntry{Name: "p", Enabled: true})
	cfg.Plugins.Enabled = false
	require.Empty(t, Instructions(cfg))
}

func TestInstructions_EnvVarsStayLiteral(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PLUGIN_TEST_SECRET", "leaked")
	writeInstructions(t, dir, "p", "value: ${PLUGIN_TEST_SECRET} and $PLUGIN_TEST_SECRET")
	cfg := pluginsCfg(t, dir, config.PluginEntry{Name: "p", Enabled: true})

	got := Instructions(cfg)
	require.Len(t, got, 1)
	require.NotContains(t, got[0].Content, "leaked")
	require.Contains(t, got[0].Content, "${PLUGIN_TEST_SECRET}")
}

func TestInstructionsBlock_FormatAndOrder(t *testing.T) {
	dir := t.TempDir()
	writeInstructions(t, dir, "first", "rule one")
	writeInstructions(t, dir, "second", "rule two")
	cfg := pluginsCfg(t, dir,
		config.PluginEntry{Name: "first", Enabled: true},
		config.PluginEntry{Name: "second", Enabled: true},
	)

	block := InstructionsBlock(cfg)
	i1 := strings.Index(block, "PLUGIN INSTRUCTIONS (first):\nrule one")
	i2 := strings.Index(block, "PLUGIN INSTRUCTIONS (second):\nrule two")
	require.GreaterOrEqual(t, i1, 0)
	require.Greater(t, i2, i1, "registry order must be preserved")
}

func TestInstructionsBlock_TruncationMarker(t *testing.T) {
	dir := t.TempDir()
	writeInstructions(t, dir, "big", strings.Repeat("line\n", 500))
	cfg := pluginsCfg(t, dir, config.PluginEntry{Name: "big", Enabled: true})
	cfg.Plugins.MaxInstructionsLines = 3

	block := InstructionsBlock(cfg)
	require.Contains(t, block, "[truncated at 3 lines]")
}
