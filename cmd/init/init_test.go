package initcmd

import (
	"os"
	"path/filepath"
	"testing"

	require "github.com/stretchr/testify/require"

	yaml "gopkg.in/yaml.v3"

	runtime "github.com/inference-gateway/cli/cmd/runtime"
	config "github.com/inference-gateway/cli/config"
)

func runInit(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	state := runtime.NewState()
	command := NewCommand(state)
	require.NoError(t, command.Flags().Set("overwrite", "true"))
	require.NoError(t, initializeProject(state, command))
}

func TestInitSeedsComposerYAML(t *testing.T) {
	home := t.TempDir()
	runInit(t, home)

	data, err := os.ReadFile(filepath.Join(home, config.ConfigDirName, config.ComposerFileName))
	require.NoError(t, err, "init seeds composer.yaml")
	require.Contains(t, string(data), "'@':", "the seeded trigger table names the at trigger")
	require.Contains(t, string(data), "status_bar:", "the seeded file carries the status bar settings")
}

func TestInitMovesLegacyStatusBarIntoComposer(t *testing.T) {
	home := t.TempDir()
	cfgDir := filepath.Join(home, config.ConfigDirName)
	require.NoError(t, os.MkdirAll(cfgDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cfgDir, config.ConfigFileName),
		[]byte("chat:\n  theme: dark\n  status_bar:\n    enabled: true\n    indicators:\n      cost: false\n"), 0o644))

	runInit(t, home)

	data, err := os.ReadFile(filepath.Join(cfgDir, config.ComposerFileName))
	require.NoError(t, err)
	var composer struct {
		StatusBar config.StatusBarConfig `yaml:"status_bar"`
	}
	require.NoError(t, yaml.Unmarshal(data, &composer))
	require.True(t, composer.StatusBar.Enabled, "the legacy chat.status_bar block moved into composer.yaml")
	require.False(t, composer.StatusBar.Indicators.Cost, "the legacy block's indicator overrides moved along")

	configData, err := os.ReadFile(filepath.Join(cfgDir, config.ConfigFileName))
	require.NoError(t, err)
	require.NotContains(t, string(configData), "status_bar", "the regenerated config.yaml no longer carries the moved block")
	require.Contains(t, string(configData), "chat:", "the regenerated config.yaml keeps its chat section")
}
