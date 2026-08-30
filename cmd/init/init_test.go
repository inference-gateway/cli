package initcmd

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	require "github.com/stretchr/testify/require"

	cobra "github.com/spf13/cobra"

	runtime "github.com/inference-gateway/cli/cmd/runtime"
	config "github.com/inference-gateway/cli/config"
	configutils "github.com/inference-gateway/cli/config/utils"
)

func splitHomeProjectEnv(t *testing.T) (homeDir, projectDir string) {
	t.Helper()
	saved := make(map[string]string)
	for _, env := range os.Environ() {
		key, value, ok := strings.Cut(env, "=")
		if ok && strings.HasPrefix(key, "INFER_") {
			saved[key] = value
			require.NoError(t, os.Unsetenv(key))
		}
	}
	t.Cleanup(func() {
		for key, value := range saved {
			_ = os.Setenv(key, value)
		}
	})
	homeDir, projectDir = t.TempDir(), t.TempDir()
	t.Setenv("HOME", homeDir)
	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(projectDir))
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	return homeDir, projectDir
}

// runInit builds a bare command carrying init's flags and runs initializeProject.
func runInit(t *testing.T, flags map[string]bool) error {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.Flags().Bool("overwrite", false, "")
	cmd.Flags().Bool("skip-migrations", false, "")
	for name, val := range flags {
		require.NoError(t, cmd.Flags().Set(name, strconv.FormatBool(val)))
	}
	return initializeProject(runtime.NewState(), cmd)
}

// TestInitializeProject pins the userspace-first model (issues #680/#1125):
// a plain `infer init` seeds the full baseline into ~/.infer/ and writes
// nothing into the project. A project override layer is created only by
// explicit `infer config set --project` writes, never by init.
func TestInitializeProject(t *testing.T) {
	homeDir, projectDir := splitHomeProjectEnv(t)

	require.NoError(t, runInit(t, map[string]bool{"skip-migrations": true}))

	for _, f := range []string{"config.yaml", "prompts.yaml", "keybindings.yaml", "computer_use.yaml", "channels.yaml"} {
		require.FileExists(t, filepath.Join(homeDir, config.ConfigDirName, f))
	}
	require.NoDirExists(t, filepath.Join(projectDir, config.ConfigDirName))
	require.NoFileExists(t, filepath.Join(projectDir, "AGENTS.md"))
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
