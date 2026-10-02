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

	t.Run("entries are strings or maps", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), SandboxFileName)
		body := "---\nallowed:\n  - .\n  - path: vendor/\n    access: read\ndenied:\n  - \"*.env\"\n  - path: deploy/\n    on_violation: approval\n"
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
		cfg, err := LoadSandbox(path)
		require.NoError(t, err)
		require.Equal(t, []sandboxdomain.Allowed{{Path: ".", Access: sandboxdomain.AccessWrite}, {Path: "vendor/", Access: sandboxdomain.AccessRead}}, cfg.Allowed)
		require.Equal(t, []sandboxdomain.Denied{{Path: "*.env"}, {Path: "deploy/", OnViolation: sandboxdomain.ViolationApproval}}, cfg.Denied)
	})

	t.Run("invalid values are rejected", func(t *testing.T) {
		for name, body := range map[string]string{
			"unknown access":    "allowed:\n  - path: /x\n    access: none\n",
			"unknown violation": "denied:\n  - path: /x\n    on_violation: ask\n",
			"missing path":      "allowed:\n  - access: read\n",
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
		require.Contains(t, string(raw), "allowed:\n  - ~/.infer/tmp\n  - path: .infer/\n    access: read\n  - .\n  - /tmp\n  - /private/tmp\n")
		require.Contains(t, string(raw), "denied:\n  - .git/\n")
		got, err := LoadSandbox(path)
		require.NoError(t, err)
		require.Equal(t, DefaultSandboxConfig(), got)
	})
}

func TestSandboxIsNotInConfigYAML(t *testing.T) {
	cfg := DefaultConfig()
	require.NotEmpty(t, cfg.Tools.Sandbox.Allowed, "DefaultConfig still carries the policy for in-process use")
	out, err := yaml.Marshal(cfg)
	require.NoError(t, err)
	require.NotContains(t, string(out), "sandbox:", "the policy must not serialise into config.yaml")
}
