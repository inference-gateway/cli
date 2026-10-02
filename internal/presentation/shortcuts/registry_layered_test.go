package shortcuts

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	require "github.com/stretchr/testify/require"

	zap "go.uber.org/zap"
	zapcore "go.uber.org/zap/zapcore"
	observer "go.uber.org/zap/zaptest/observer"

	config "github.com/inference-gateway/cli/config"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
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

// TestLoadCustomShortcutsBuiltinsWin pins that a custom YAML shortcut cannot
// shadow a built-in one: the init-seeded a2a.yaml once overwrote /agents, so
// the Agents view never opened on any machine that had run `infer init`.
func TestLoadCustomShortcutsBuiltinsWin(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	writeShortcutFile(t, filepath.Join(homeDir, config.ConfigDirName), "a2a.yaml", "agents", "custom agents")

	registry := NewRegistry()
	registry.Register(NewAgentsShortcut())
	require.NoError(t, registry.LoadCustomShortcuts(config.ConfigLookupDirs(), nil, nil, nil, nil))

	agents, ok := registry.Get("agents")
	require.True(t, ok)
	_, isBuiltin := agents.(*AgentsShortcut)
	require.True(t, isBuiltin, "built-in /agents must win over the custom a2a.yaml shortcut")

	_, ok = registry.Get("a2a")
	require.False(t, ok, "the /a2a alias is removed: nothing registers it")
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

// TestShadowWarningOncePerProcess pins that the shadow warning fires once per
// process, not once per registry: the container, the daemon's metadata registry
// and every CLI command build their own registries, so one stale
// ~/.infer/shortcuts/a2a.yaml used to repeat the warning on each of them.
func TestShadowWarningOncePerProcess(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	writeShortcutFile(t, filepath.Join(homeDir, config.ConfigDirName), "a2a.yaml", "agents", "custom agents")

	observerCore, logs := observer.New(zapcore.WarnLevel)
	prevLogger := logger.GetGlobalLogger()
	logger.SetGlobalLogger(zap.New(observerCore))
	t.Cleanup(func() { logger.SetGlobalLogger(prevLogger) })

	prevWarned := warnedShadowed
	warnedShadowed = new(sync.Map)
	t.Cleanup(func() { warnedShadowed = prevWarned })

	for range 2 {
		registry := NewRegistry()
		registry.Register(NewAgentsShortcut())
		require.NoError(t, registry.LoadCustomShortcuts(config.ConfigLookupDirs(), nil, nil, nil, nil))
		_, ok := registry.Get("agents")
		require.True(t, ok, "built-in /agents must survive the custom override")
	}

	entries := logs.AllUntimed()
	require.Len(t, entries, 1, "the shadow warning must fire once per process, not once per registry")
	require.Contains(t, entries[0].Message, "delete or rename")

	fields := entries[0].ContextMap()
	require.Equal(t, "agents", fields["name"])
	file, ok := fields["file"].(string)
	require.True(t, ok, "the file field must be a string")
	require.True(t, strings.HasSuffix(file, "a2a.yaml"), "file = %s", file)
}
