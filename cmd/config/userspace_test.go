package configcmd

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	require "github.com/stretchr/testify/require"

	cobra "github.com/spf13/cobra"

	runtime "github.com/inference-gateway/cli/cmd/runtime"
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

func TestConfigGetComposerSection(t *testing.T) {
	root := map[string]any{}
	require.NoError(t, injectComposer(root, *config.DefaultComposerConfig()))

	value, err := lookupConfigKey(root, "composer.triggers.chat.@")
	require.NoError(t, err, "config get composer resolves the trigger table the files merged into")
	require.Equal(t, []any{"files", "agents"}, value, "the chat @ section order survives the dump roundtrip")

	value, err = lookupConfigKey(root, "composer.triggers.room.@")
	require.NoError(t, err)
	require.Equal(t, []any{"agents", "files"}, value, "the room @ section order survives the dump roundtrip")

	section, err := lookupConfigKey(root, "composer.status_bar.enabled")
	require.NoError(t, err)
	require.Equal(t, true, section, "the moved status bar settings print under composer")
}

func TestConfigSetDefaultWritesUserspace(t *testing.T) {
	homeDir, projectDir := splitHomeProjectEnv(t)
	require.NoError(t, setConfigValue(newProjectFlagCommand(false), []string{"agent.model", "home-set-model"}, acceptConfig))
	homeConfig := filepath.Join(homeDir, config.ConfigDirName, config.ConfigFileName)
	require.FileExists(t, homeConfig)
	data, err := os.ReadFile(homeConfig)
	require.NoError(t, err)
	require.Contains(t, string(data), "home-set-model")
	require.NoFileExists(t, filepath.Join(projectDir, config.DefaultConfigPath))
}

func TestConfigSetProjectWritesSparseOverride(t *testing.T) {
	homeDir, projectDir := splitHomeProjectEnv(t)
	require.NoError(t, setConfigValue(newProjectFlagCommand(true), []string{"agent.model", "proj-set-model"}, acceptConfig))
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

// An empty list value must reach the project file as an empty sequence, not be
// dropped or written as null, so the override still clears the baseline list.
func TestConfigSetProjectEmptyListWritesEmptySequence(t *testing.T) {
	_, projectDir := splitHomeProjectEnv(t)
	require.NoError(t, setConfigValue(newProjectFlagCommand(true), []string{"gateway.include_models", ""}, acceptConfig))
	data, err := os.ReadFile(filepath.Join(projectDir, config.DefaultConfigPath))
	require.NoError(t, err)
	require.Contains(t, string(data), "include_models: []")
}

func acceptConfig() (*config.Config, error) { return config.DefaultConfig(), nil }

func rejectConfig() (*config.Config, error) { return nil, errors.New("invalid agent.reasoning_effort") }

// A value the loader rejects must leave the file as it was, otherwise every
// later infer call, including the config set that would fix it, fails to start.
func TestConfigSetRollsBackRejectedValue(t *testing.T) {
	homeDir, _ := splitHomeProjectEnv(t)
	homeConfig := filepath.Join(homeDir, config.ConfigDirName, config.ConfigFileName)

	err := setConfigValue(newProjectFlagCommand(false), []string{"agent.reasoning_effort", "bogus"}, rejectConfig)
	require.ErrorContains(t, err, "was not saved")
	require.NoFileExists(t, homeConfig, "a file the rejected write created must be removed")

	require.NoError(t, setConfigValue(newProjectFlagCommand(false), []string{"agent.reasoning_effort", "high"}, acceptConfig))
	before, err := os.ReadFile(homeConfig)
	require.NoError(t, err)

	err = setConfigValue(newProjectFlagCommand(false), []string{"agent.reasoning_effort", "bogus"}, rejectConfig)
	require.ErrorContains(t, err, "was not saved")
	after, err := os.ReadFile(homeConfig)
	require.NoError(t, err)
	require.Equal(t, string(before), string(after))
}

// A userspace write keeps the keys Config does not declare, such as the desktop
// app's projects.root, never brings back a declared key it omits as empty, and
// matches keys case-insensitively as Viper does.
func TestConfigSetKeepsKeysConfigDoesNotDeclare(t *testing.T) {
	homeDir, _ := splitHomeProjectEnv(t)
	homeConfig := filepath.Join(homeDir, config.ConfigDirName, config.ConfigFileName)
	require.NoError(t, os.MkdirAll(filepath.Dir(homeConfig), 0o755))
	existing := "gateway:\n  oci: ghcr.io/example/old:1\n  desktop_only: kept-nested\n" +
		"projects:\n  root: /home/x/repos\ncomputer_use:\n  enabled: true\nLogging:\n  debug: true\n"
	require.NoError(t, os.WriteFile(homeConfig, []byte(existing), 0o644))

	require.NoError(t, setConfigValue(newProjectFlagCommand(false), []string{"gateway.oci", ""}, acceptConfig))

	data, err := os.ReadFile(homeConfig)
	require.NoError(t, err)
	content := string(data)
	require.Contains(t, content, "projects:\n  root: /home/x/repos")
	require.Contains(t, content, "desktop_only: kept-nested")
	require.NotContains(t, content, "ghcr.io/example/old:1")
	require.NotContains(t, content, "computer_use:")
	require.NotContains(t, content, "Logging:")
	require.Contains(t, content, "logging:\n  debug: true")
}

func newConfigTestCommand(t *testing.T) *cobra.Command {
	t.Helper()
	command := NewCommand(runtime.NewState())
	command.SetOut(io.Discard)
	command.SetErr(io.Discard)
	return command
}

// TestConfigInitWritesUserspaceOnly pins that `infer config init` seeds only the
// userspace baseline. A project ./.infer/config.yaml is a sparse override written
// by `config set --project`. A full default config there would shadow the whole
// baseline, since project values replace userspace ones key-by-key.
func TestConfigInitWritesUserspaceOnly(t *testing.T) {
	homeDir, projectDir := splitHomeProjectEnv(t)

	command := newConfigTestCommand(t)
	command.SetArgs([]string{"init"})
	require.NoError(t, command.Execute())

	require.FileExists(t, filepath.Join(homeDir, config.ConfigDirName, config.ConfigFileName))
	require.NoFileExists(t, filepath.Join(projectDir, config.DefaultConfigPath))
}

// TestConfigInitRejectsProjectFlag pins that --project lives on `config set` only,
// so no command can seed a full default config into a project override layer.
func TestConfigInitRejectsProjectFlag(t *testing.T) {
	splitHomeProjectEnv(t)

	command := newConfigTestCommand(t)
	command.SetArgs([]string{"init", "--project"})
	require.ErrorContains(t, command.Execute(), "unknown flag: --project")
}
