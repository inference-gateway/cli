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

// compute.yaml is read from home only, and a workspace that names a cluster
// there arrives resolved, with the cluster's kind, connection and gateway.
func TestComputeResolvesWorkspacesFromHome(t *testing.T) {
	homeDir, projectDir := splitHomeProjectEnv(t)
	homeFile := filepath.Join(homeDir, config.ConfigDirName, config.ComputeFileName)
	require.NoError(t, os.MkdirAll(filepath.Dir(homeFile), 0o755))
	require.NoError(t, os.WriteFile(homeFile, []byte("clusters:\n  - name: k3d\n    context: k3d-agents\n    namespace: agents\n    gateway_env:\n      - ANTHROPIC_API_KEY\n"), 0o644))
	projectFile := filepath.Join(projectDir, config.ConfigDirName, config.ComputeFileName)
	require.NoError(t, os.MkdirAll(filepath.Dir(projectFile), 0o755))
	require.NoError(t, os.WriteFile(projectFile, []byte("clusters:\n  - name: project-only\n    namespace: elsewhere\n"), 0o644))
	writeHomeFile(t, config.DaemonFileName, "workspaces:\n  - name: reviewer\n    compute: k3d\n    image: infer:1\n")

	cfg := loadTestConfig(t)
	require.Len(t, cfg.Compute.Clusters, 1)
	assert.Equal(t, "k3d", cfg.Compute.Clusters[0].Name, "a project compute.yaml is never read")
	require.Len(t, cfg.Daemon.Workspaces, 1)
	workspace := cfg.Daemon.Workspaces[0]
	assert.Equal(t, config.WorkspaceKindKubernetes, workspace.Kind)
	assert.Equal(t, "k3d-agents", workspace.Context)
	assert.Equal(t, "agents", workspace.Namespace)
	assert.Equal(t, []string{"ANTHROPIC_API_KEY"}, workspace.GatewayEnv)
}

// A workspace's user_data is a script for the computer's shell, so its $VARS
// stay as written while the rest of daemon.yaml resolves ${VAR} references.
func TestWorkspaceUserDataKeepsItsShellVariables(t *testing.T) {
	withHermeticEnv(t)
	t.Setenv("WORKSPACE_IMAGE", "computer:1")
	t.Setenv("1", "leaked")
	script := "#!/bin/sh\ndir=/opt/opentask\ninstall -d \"$dir\" \"${1}\"\n"
	writeHomeFile(t, config.DaemonFileName, "workspaces:\n  - name: desktop\n    kind: kubernetes\n    namespace: agents\n    image: ${WORKSPACE_IMAGE}\n    user_data: |\n      #!/bin/sh\n      dir=/opt/opentask\n      install -d \"$dir\" \"${1}\"\n")

	cfg := loadTestConfig(t)
	require.Len(t, cfg.Daemon.Workspaces, 1)
	assert.Equal(t, "computer:1", cfg.Daemon.Workspaces[0].Image)
	assert.Equal(t, script, cfg.Daemon.Workspaces[0].UserData)
}

// An unquoted ${VAR} takes the type of its value, so a number reaches a
// number field even through a sidecar that reshapes its file.
func TestExpandedUnquotedValueKeepsItsType(t *testing.T) {
	withHermeticEnv(t)
	t.Setenv("MAX_RESULT_BYTES", "42")
	writeHomeFile(t, config.ToolsFileName, "max_result_bytes: ${MAX_RESULT_BYTES}\n")

	cfg := loadTestConfig(t)
	assert.Equal(t, 42, cfg.Tools.MaxResultBytes)
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

func TestWorkspaceVolumesLoadFromDaemonYAML(t *testing.T) {
	withHermeticEnv(t)
	writeHomeFile(t, config.DaemonFileName, "workspaces:\n  - name: desktop\n    kind: kubernetes\n    namespace: agents\n    image: computer:1\n    volumes:\n      - name: resolve\n        claim: davinci-resolve\n        mount_path: /opt/resolve\n      - name: footage\n        claim: raw-footage\n        mount_path: /mnt/footage\n        read_only: true\n")

	cfg := loadTestConfig(t)
	require.Len(t, cfg.Daemon.Workspaces, 1)
	assert.Equal(t, []config.WorkspaceVolume{
		{Name: "resolve", Claim: "davinci-resolve", MountPath: "/opt/resolve"},
		{Name: "footage", Claim: "raw-footage", MountPath: "/mnt/footage", ReadOnly: true},
	}, cfg.Daemon.Workspaces[0].Volumes)
}
