package cmd

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	cobra "github.com/spf13/cobra"
	require "github.com/stretchr/testify/require"

	config "github.com/inference-gateway/cli/config"
	configutils "github.com/inference-gateway/cli/config/utils"
)

// runInit builds a bare command carrying init's flags and runs initializeProject.
func runInit(t *testing.T, flags map[string]bool) error {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.Flags().Bool("overwrite", false, "")
	cmd.Flags().Bool("project", false, "")
	cmd.Flags().Bool("skip-migrations", false, "")
	for name, val := range flags {
		require.NoError(t, cmd.Flags().Set(name, strconv.FormatBool(val)))
	}
	return initializeProject(cmd)
}

// TestInitializeProject pins the userspace-first model (issue #680): a plain
// `infer init` seeds the full baseline into ~/.infer/ and writes nothing into
// the project, while `infer init --project` seeds only the project-overridable
// files into ./.infer/ and keeps userspace-only files in ~/.infer/.
func TestInitializeProject(t *testing.T) {
	projectOverridable := []string{
		".infer/config.yaml", ".infer/.gitignore", ".infer/prompts.yaml",
		".infer/hooks.yaml", ".infer/agents.yaml", ".infer/mcp.yaml",
		".infer/shortcuts/scm.yaml",
	}
	userspaceOnly := []string{
		"keybindings.yaml", "reminders.yaml", "channels.yaml",
		"heartbeat.yaml", "computer_use.yaml",
	}

	t.Run("default seeds userspace baseline, nothing in project", func(t *testing.T) {
		homeDir, projectDir := splitHomeProjectEnv(t)

		require.NoError(t, runInit(t, map[string]bool{"skip-migrations": true}))

		for _, f := range []string{"config.yaml", ".gitignore", "prompts.yaml", "keybindings.yaml", "computer_use.yaml", "channels.yaml"} {
			require.FileExists(t, filepath.Join(homeDir, config.ConfigDirName, f))
		}
		require.NoDirExists(t, filepath.Join(projectDir, config.ConfigDirName))
		require.NoFileExists(t, filepath.Join(projectDir, "AGENTS.md"))
	})

	t.Run("project seeds override layer, userspace-only files stay home", func(t *testing.T) {
		homeDir, projectDir := splitHomeProjectEnv(t)

		require.NoError(t, runInit(t, map[string]bool{"project": true, "skip-migrations": true}))

		for _, f := range projectOverridable {
			require.FileExists(t, filepath.Join(projectDir, f))
		}
		for _, f := range userspaceOnly {
			require.NoFileExists(t, filepath.Join(projectDir, config.ConfigDirName, f))
			require.FileExists(t, filepath.Join(homeDir, config.ConfigDirName, f))
		}
		require.NoFileExists(t, filepath.Join(projectDir, "AGENTS.md"))

		// The project config.yaml is a sparse scaffold, not a full config dump.
		data, err := os.ReadFile(filepath.Join(projectDir, config.DefaultConfigPath))
		require.NoError(t, err)
		require.Contains(t, string(data), "Project-level configuration overrides")
		require.NotContains(t, string(data), "gateway:")
	})
}

func TestInitWritesConfigYAMLWithDocMarker(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "infer-config-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	configPath := tmpDir + "/.infer/config.yaml"

	if err := configutils.SaveYAML(configPath, "config", config.DefaultConfig()); err != nil {
		t.Fatalf("SaveYAML() error = %v", err)
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatal("expected config file to be created")
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config file: %v", err)
	}

	if !strings.HasPrefix(string(content), "---\n") {
		t.Errorf("config file should start with `---\\n`, got %q", string(content[:min(8, len(content))]))
	}
	if !strings.Contains(string(content), "gateway:") {
		t.Errorf("config file does not contain expected gateway section")
	}
}

func TestCheckFileExists(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "infer-check-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Test non-existent file
	err = checkFileExists(tmpDir+"/nonexistent.txt", "test file")
	if err != nil {
		t.Errorf("checkFileExists() should not error for non-existent file: %v", err)
	}

	// Test existing file
	existingFile := tmpDir + "/existing.txt"
	if err := os.WriteFile(existingFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	err = checkFileExists(existingFile, "test file")
	if err == nil {
		t.Errorf("checkFileExists() should error for existing file")
	}
}
