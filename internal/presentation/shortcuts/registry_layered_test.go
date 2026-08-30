package shortcuts

import (
	"os"
	"path/filepath"
	"testing"

	require "github.com/stretchr/testify/require"

	config "github.com/inference-gateway/cli/config"
)

func writeShortcutFile(t *testing.T, dir, file, name, description string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "shortcuts"), 0o755))
	body := "---\nshortcuts:\n  - name: " + name + "\n    description: \"" + description + "\"\n    command: echo\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "shortcuts", file), []byte(body), 0o644))
}

// TestLoadCustomShortcutsLayered pins that a project .infer/ never hides the
// userspace shortcuts `infer init` seeds. Before this was layered, creating any
// ./.infer/config.yaml (which `infer config set --project` does) flipped
// ResolveConfigDir to the project and every ~/.infer/shortcuts/ entry vanished.
func TestLoadCustomShortcutsLayered(t *testing.T) {
	homeDir, projectDir := t.TempDir(), t.TempDir()
	t.Setenv("HOME", homeDir)
	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(projectDir))
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	homeCfgDir := filepath.Join(homeDir, config.ConfigDirName)
	writeShortcutFile(t, homeCfgDir, "git.yaml", "git", "userspace git")
	writeShortcutFile(t, homeCfgDir, "scm.yaml", "scm", "userspace scm")

	projectCfgDir := filepath.Join(projectDir, config.ConfigDirName)
	require.NoError(t, os.MkdirAll(projectCfgDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectCfgDir, config.ConfigFileName),
		[]byte("---\nagent:\n  model: project-model\n"), 0o644))
	writeShortcutFile(t, projectCfgDir, "scm.yaml", "scm", "project scm")
	writeShortcutFile(t, projectCfgDir, "deploy.yaml", "deploy", "project deploy")

	registry := NewRegistry()
	require.NoError(t, registry.LoadCustomShortcuts(config.ConfigLookupDirs(), nil, nil, nil, nil))

	git, ok := registry.Get("git")
	require.True(t, ok, "userspace shortcut must survive a project override layer")
	require.Equal(t, "userspace git", git.GetDescription())

	scm, ok := registry.Get("scm")
	require.True(t, ok)
	require.Equal(t, "project scm", scm.GetDescription(), "project shortcut overlays the userspace one by name")

	deploy, ok := registry.Get("deploy")
	require.True(t, ok, "project-only shortcut must load")
	require.Equal(t, "project deploy", deploy.GetDescription())
}

// TestConfigLookupDirsWithoutProjectLayer pins that a project dir is only added
// when it actually exists, so the userspace baseline stands alone by default.
func TestConfigLookupDirsWithoutProjectLayer(t *testing.T) {
	homeDir, projectDir := t.TempDir(), t.TempDir()
	t.Setenv("HOME", homeDir)
	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(projectDir))
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	require.Equal(t, []string{filepath.Join(homeDir, config.ConfigDirName)}, config.ConfigLookupDirs())
}
