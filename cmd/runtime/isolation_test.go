package runtime

import (
	"os"
	"path/filepath"
	"testing"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	config "github.com/inference-gateway/cli/config"
	sandboxdomain "github.com/inference-gateway/cli/internal/sandbox/domain"
)

func writeHomeFile(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(os.Getenv("HOME"), config.ConfigDirName, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	return path
}

// A block in config.yaml whose section lives in a split file is dropped
// before the merge, so it can never reach the typed config.
func TestSidecarBlocksInConfigYAMLAreIgnored(t *testing.T) {
	withHermeticEnv(t)
	writeHomeFile(t, config.ConfigFileName, `---
daemon:
  binding:
    enabled: true
    port: 1
channels:
  enabled: true
tools:
  enabled: false
  sandbox:
    filesystem:
      allowed:
        - /
chat:
  theme: dracula
  keybindings:
    enabled: false
mcp:
  enabled: true
`)

	cfg := loadTestConfig(t)
	assert.Equal(t, config.DefaultDaemonConfig(), &cfg.Daemon)
	assert.Equal(t, config.DefaultChannelsConfig(), &cfg.Channels)
	assert.Equal(t, config.DefaultToolsConfig(), &cfg.Tools)
	assert.Equal(t, config.DefaultKeybindingsConfig(), &cfg.Chat.Keybindings)
	assert.Equal(t, config.DefaultMCPConfig(), &cfg.MCP)
	assert.Equal(t, "dracula", cfg.Chat.Theme, "sibling keys of a stripped block survive")
}

// Sidecars layer like config.yaml: home first, project on top, lists replaced.
func TestSidecarLayersHomeThenProject(t *testing.T) {
	homeDir, projectDir := splitHomeProjectEnv(t)
	homeFile := filepath.Join(homeDir, config.ConfigDirName, config.ChannelsFileName)
	require.NoError(t, os.MkdirAll(filepath.Dir(homeFile), 0o755))
	require.NoError(t, os.WriteFile(homeFile, []byte("enabled: true\nmax_workers: 12\ntelegram:\n  allowed_users: [a, b]\n"), 0o644))
	projectFile := filepath.Join(projectDir, config.ConfigDirName, config.ChannelsFileName)
	require.NoError(t, os.MkdirAll(filepath.Dir(projectFile), 0o755))
	require.NoError(t, os.WriteFile(projectFile, []byte("max_workers: 3\ntelegram:\n  allowed_users: [c]\n"), 0o644))

	cfg := loadTestConfig(t)
	assert.True(t, cfg.Channels.Enabled, "keys the project omits come from home")
	assert.Equal(t, 3, cfg.Channels.MaxWorkers, "project wins key by key")
	assert.Equal(t, []string{"c"}, cfg.Channels.Telegram.AllowedUsers, "lists are replaced wholesale")
}

// MCP server env keys keep their case: Viper lowercases untyped map keys.
func TestMCPServerEnvKeepsCase(t *testing.T) {
	withHermeticEnv(t)
	t.Setenv("MY_API_KEY", "secret")
	writeHomeFile(t, config.MCPFileName, "servers:\n  - name: api\n    env:\n      API_KEY: ${MY_API_KEY}\n      Log_Level: debug\n")

	cfg := loadTestConfig(t)
	require.Len(t, cfg.MCP.Servers, 1)
	assert.Equal(t, map[string]string{"API_KEY": "secret", "Log_Level": "debug"}, cfg.MCP.Servers[0].Env)
}

// Sandbox entries decode through the domain's YAML decoders: a bare path or a
// mapping, with access defaulting to write.
func TestSandboxEntriesDecode(t *testing.T) {
	withHermeticEnv(t)
	writeHomeFile(t, config.SandboxFileName, "filesystem:\n  allowed:\n    - /plain\n    - path: /ro\n      access: read\n  denied:\n    - .git/\n    - path: '*.env.*'\n      except: [.env.example]\n")

	cfg := loadTestConfig(t)
	assert.Equal(t, []sandboxdomain.Allowed{{Path: "/plain", Access: sandboxdomain.AccessWrite}, {Path: "/ro", Access: sandboxdomain.AccessRead}}, cfg.Tools.Sandbox.Filesystem.Allowed)
	assert.Equal(t, []sandboxdomain.Denied{{Path: ".git/"}, {Path: "*.env.*", Except: []string{".env.example"}}}, cfg.Tools.Sandbox.Filesystem.Denied)
}

// tools.yaml nests the per-tool sections under tools: and is lifted onto the
// flat ToolsConfig keys, with keys the file omits keeping their default.
func TestToolsYAMLLayoutLoads(t *testing.T) {
	withHermeticEnv(t)
	writeHomeFile(t, config.ToolsFileName, "max_result_bytes: 42\ntools:\n  bash:\n    timeout: 7\n")

	cfg := loadTestConfig(t)
	assert.Equal(t, 42, cfg.Tools.MaxResultBytes)
	assert.Equal(t, 7, cfg.Tools.Bash.Timeout)
	assert.True(t, cfg.Tools.Bash.Enabled, "omitted keys keep their default")
	assert.Equal(t, config.DefaultToolsConfig().Bash.Mode.All.Allow, cfg.Tools.Bash.Mode.All.Allow)
}

// A broken non-fatal sidecar falls back to defaults with a warning, a broken
// tools.yaml or sandbox.yaml fails the load.
func TestBrokenSidecars(t *testing.T) {
	withHermeticEnv(t)
	writeHomeFile(t, config.ChannelsFileName, "enabled: [unterminated")
	cfg := loadTestConfig(t)
	assert.Equal(t, config.DefaultChannelsConfig(), &cfg.Channels)

	toolsFile := writeHomeFile(t, config.ToolsFileName, "enabled: [unterminated")
	_, _, err := loadConfig(rootCmd)
	require.ErrorContains(t, err, toolsFile)
}
