package config

import (
	"os"
	"path/filepath"
	"testing"

	require "github.com/stretchr/testify/require"

	yaml "gopkg.in/yaml.v3"

	sandboxdomain "github.com/inference-gateway/cli/internal/sandbox/domain"
)

func TestLoadSandbox(t *testing.T) {
	t.Run("missing file is the defaults", func(t *testing.T) {
		cfg, err := LoadSandbox(filepath.Join(t.TempDir(), SandboxFileName))
		require.NoError(t, err)
		require.Equal(t, DefaultSandboxConfig(), cfg)
	})

	t.Run("an omitted list keeps its default", func(t *testing.T) {
		defaults := DefaultSandboxConfig().Filesystem
		for name, tt := range map[string]struct {
			body        string
			wantAllowed []sandboxdomain.Allowed
			wantDenied  []sandboxdomain.Denied
		}{
			"empty file":   {"", defaults.Allowed, defaults.Denied},
			"denied only":  {"filesystem:\n  denied:\n    - secrets/\n", defaults.Allowed, sandboxdomain.Deny("secrets/")},
			"allowed only": {"filesystem:\n  allowed:\n    - /work\n", sandboxdomain.Allow("/work"), defaults.Denied},
		} {
			path := filepath.Join(t.TempDir(), SandboxFileName)
			require.NoError(t, os.WriteFile(path, []byte(tt.body), 0o644))
			cfg, err := LoadSandbox(path)
			require.NoError(t, err, name)
			require.Equal(t, tt.wantAllowed, cfg.Filesystem.Allowed, name)
			require.Equal(t, tt.wantDenied, cfg.Filesystem.Denied, name)
		}
	})

	t.Run("entries are strings or maps", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), SandboxFileName)
		body := "---\nfilesystem:\n  allowed:\n    - .\n    - path: vendor/\n      access: read\n  denied:\n    - \"*.env\"\n    - path: deploy/\n      on_violation: approval\n"
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
		cfg, err := LoadSandbox(path)
		require.NoError(t, err)
		require.Equal(t, []sandboxdomain.Allowed{{Path: ".", Access: sandboxdomain.AccessWrite}, {Path: "vendor/", Access: sandboxdomain.AccessRead}}, cfg.Filesystem.Allowed)
		require.Equal(t, []sandboxdomain.Denied{{Path: "*.env"}, {Path: "deploy/", OnViolation: sandboxdomain.ViolationApproval}}, cfg.Filesystem.Denied)
	})

	t.Run("invalid values are rejected", func(t *testing.T) {
		for name, body := range map[string]string{
			"unknown access":    "filesystem:\n  allowed:\n    - path: /x\n      access: none\n",
			"unknown violation": "filesystem:\n  denied:\n    - path: /x\n      on_violation: ask\n",
			"missing path":      "filesystem:\n  allowed:\n    - access: read\n",
		} {
			path := filepath.Join(t.TempDir(), SandboxFileName)
			require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
			_, err := LoadSandbox(path)
			require.Error(t, err, name)
		}
	})

	t.Run("defaults save as flat lists and round-trip", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "nested", SandboxFileName)
		require.NoError(t, SaveSandbox(path, DefaultSandboxConfig()))
		raw, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Contains(t, string(raw), "filesystem:\n  allowed:\n    - .\n    - /tmp\n")
		require.Contains(t, string(raw), "  denied:\n    - path: .infer/\n      on_violation: approval\n    - .git/\n")
		got, err := LoadSandbox(path)
		require.NoError(t, err)
		require.Equal(t, DefaultSandboxConfig(), got)
	})
}

func TestSandboxIsNotInConfigYAML(t *testing.T) {
	cfg := DefaultConfig()
	require.NotEmpty(t, cfg.Tools.Sandbox.Filesystem.Allowed, "DefaultConfig still carries the policy for in-process use")
	out, err := yaml.Marshal(cfg)
	require.NoError(t, err)
	require.NotContains(t, string(out), "sandbox:", "the policy must not serialise into config.yaml")
}
