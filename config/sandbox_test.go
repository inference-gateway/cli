package config

import (
	"os"
	"path/filepath"
	"testing"

	require "github.com/stretchr/testify/require"

	yaml "gopkg.in/yaml.v3"
)

func TestLoadSandbox(t *testing.T) {
	t.Run("missing file is the defaults", func(t *testing.T) {
		cfg, err := LoadSandbox(filepath.Join(t.TempDir(), SandboxFileName))
		require.NoError(t, err)
		require.Equal(t, DefaultSandboxConfig(), cfg)
	})

	t.Run("partial file keeps the default protected paths", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), SandboxFileName)
		require.NoError(t, os.WriteFile(path, []byte("---\ndirectories:\n  - /data\n"), 0o644))
		cfg, err := LoadSandbox(path)
		require.NoError(t, err)
		require.Equal(t, []string{"/data"}, cfg.Directories)
		require.Equal(t, DefaultSandboxConfig().ProtectedPaths, cfg.ProtectedPaths)
	})

	t.Run("save and load round-trip", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "nested", SandboxFileName)
		want := &SandboxConfig{Directories: []string{".", "/srv"}, ProtectedPaths: []string{"*.pem"}}
		require.NoError(t, SaveSandbox(path, want))
		got, err := LoadSandbox(path)
		require.NoError(t, err)
		require.Equal(t, want, got)
	})
}

func TestSandboxIsNotInConfigYAML(t *testing.T) {
	cfg := DefaultConfig()
	require.NotEmpty(t, cfg.Tools.Sandbox.Directories, "DefaultConfig still carries the policy for in-process use")
	out, err := yaml.Marshal(cfg)
	require.NoError(t, err)
	require.NotContains(t, string(out), "sandbox:", "the policy must not serialise into config.yaml")
}
