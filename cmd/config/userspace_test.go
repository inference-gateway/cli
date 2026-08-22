package configcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	require "github.com/stretchr/testify/require"

	cobra "github.com/spf13/cobra"

	config "github.com/inference-gateway/cli/config"
)

func splitHomeProjectEnv(t *testing.T) (homeDir, projectDir string) {
	t.Helper()
	for _, env := range os.Environ() {
		key, _, ok := strings.Cut(env, "=")
		if ok && strings.HasPrefix(key, "INFER_") {
			t.Setenv(key, "")
		}
	}
	homeDir, projectDir = t.TempDir(), t.TempDir()
	t.Setenv("HOME", homeDir)
	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(projectDir))
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	return homeDir, projectDir
}

func newProjectFlagCommand(project bool) *cobra.Command {
	command := &cobra.Command{}
	command.Flags().Bool("project", false, "")
	if project {
		_ = command.Flags().Set("project", "true")
	}
	return command
}

func TestConfigSetDefaultWritesUserspace(t *testing.T) {
	homeDir, projectDir := splitHomeProjectEnv(t)
	require.NoError(t, setConfigValue(newProjectFlagCommand(false), []string{"agent.model", "home-set-model"}))
	homeConfig := filepath.Join(homeDir, config.ConfigDirName, config.ConfigFileName)
	require.FileExists(t, homeConfig)
	data, err := os.ReadFile(homeConfig)
	require.NoError(t, err)
	require.Contains(t, string(data), "home-set-model")
	require.NoFileExists(t, filepath.Join(projectDir, config.DefaultConfigPath))
}

func TestConfigSetProjectWritesSparseOverride(t *testing.T) {
	homeDir, projectDir := splitHomeProjectEnv(t)
	require.NoError(t, setConfigValue(newProjectFlagCommand(true), []string{"agent.model", "proj-set-model"}))
	projectConfig := filepath.Join(projectDir, config.DefaultConfigPath)
	require.FileExists(t, projectConfig)
	data, err := os.ReadFile(projectConfig)
	require.NoError(t, err)
	content := string(data)
	require.Contains(t, content, "proj-set-model")
	require.Contains(t, content, "agent:")
	require.NotContains(t, content, "gateway:")
	require.NotContains(t, content, "storage:")
	require.NoFileExists(t, filepath.Join(homeDir, config.ConfigDirName, config.ConfigFileName))
}
