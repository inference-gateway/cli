package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	require "github.com/stretchr/testify/require"

	config "github.com/inference-gateway/cli/config"
)

// splitHomeProjectEnv clears INFER_* env vars, points HOME at one temp dir, and
// chdirs into a separate project temp dir, returning both. Unlike
// withHermeticEnv (which makes HOME == cwd), it keeps the userspace baseline and
// the project layer in distinct directories so userspace-first behavior is
// observable.
func splitHomeProjectEnv(t *testing.T) (homeDir, projectDir string) {
	t.Helper()

	saved := make(map[string]string)
	for _, env := range os.Environ() {
		key, val, ok := strings.Cut(env, "=")
		if !ok {
			continue
		}
		if strings.HasPrefix(key, "INFER_") {
			saved[key] = val
			require.NoError(t, os.Unsetenv(key))
		}
	}
	t.Cleanup(func() {
		for k, v := range saved {
			_ = os.Setenv(k, v)
		}
	})

	homeDir = t.TempDir()
	projectDir = t.TempDir()
	t.Setenv("HOME", homeDir)

	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(projectDir))
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	return homeDir, projectDir
}

// TestInitConfigProjectMergesOntoHome pins the core of issue #680: the project
// ./.infer/config.yaml merges onto the userspace ~/.infer/config.yaml key by
// key. Keys the project sets win; keys it omits are inherited from home.
func TestInitConfigProjectMergesOntoHome(t *testing.T) {
	homeDir, projectDir := splitHomeProjectEnv(t)

	homeCfg := filepath.Join(homeDir, config.ConfigDirName, config.ConfigFileName)
	require.NoError(t, os.MkdirAll(filepath.Dir(homeCfg), 0o755))
	require.NoError(t, os.WriteFile(homeCfg, []byte("---\nagent:\n  model: home-model\n  max_turns: 99\n"), 0o644))

	projCfg := filepath.Join(projectDir, config.DefaultConfigPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(projCfg), 0o755))
	require.NoError(t, os.WriteFile(projCfg, []byte("---\nagent:\n  model: project-model\n"), 0o644))

	initConfig()

	require.Equal(t, "project-model", V.GetString("agent.model"), "project config.yaml must override the home key")
	require.Equal(t, 99, V.GetInt("agent.max_turns"), "keys absent from the project layer are inherited from home")
	require.Equal(t, "project-model", Cfg.Agent.Model)
	require.Equal(t, 99, Cfg.Agent.MaxTurns)
}

// TestInitConfigInheritsHomeWhenProjectAbsent confirms a project with no
// config.yaml inherits the home baseline wholesale.
func TestInitConfigInheritsHomeWhenProjectAbsent(t *testing.T) {
	homeDir, _ := splitHomeProjectEnv(t)

	homeCfg := filepath.Join(homeDir, config.ConfigDirName, config.ConfigFileName)
	require.NoError(t, os.MkdirAll(filepath.Dir(homeCfg), 0o755))
	require.NoError(t, os.WriteFile(homeCfg, []byte("---\nagent:\n  model: home-model\n"), 0o644))

	initConfig()

	require.Equal(t, "home-model", V.GetString("agent.model"))
	require.Equal(t, "home-model", Cfg.Agent.Model)
}
