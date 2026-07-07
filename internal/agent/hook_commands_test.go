package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	plugins "github.com/inference-gateway/cli/internal/services/plugins"
)

// allowCfg builds a config whose every-mode bash allow-list is exactly cmds, so
// a hook command matching one of them is auto-approved and anything else is
// off-list (skipped). Mirrors the construction in config's bash allow-list tests.
func allowCfg(cmds ...string) *config.Config {
	return &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Bash: config.BashToolConfig{
				Enabled: true,
				Mode: config.BashModesConfig{
					All: config.BashModeAllowConfig{Allow: cmds},
				},
			},
		},
	}
}

// parseEvents decodes every newline-delimited stream event the debug writer
// captured into a slice of maps.
func parseEvents(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var events []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e map[string]any
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("bad event line %q: %v", line, err)
		}
		events = append(events, e)
	}
	return events
}

func hooksProvider(enabled bool, cmds ...config.HookCommandConfig) config.HooksConfig {
	return config.HooksConfig{Enabled: enabled, Hooks: cmds}
}

func TestRunCommandHooks_RunsAllowListedCommand(t *testing.T) {
	buf := withDebugStreamWriter(t)
	cfg := allowCfg("echo hook-ran")
	provider := hooksProvider(true, config.HookCommandConfig{
		Name: "echoer", Hook: domain.HookPostSession, Command: "echo hook-ran", Timeout: 5,
	})

	RunCommandHooks(context.Background(), cfg, provider, "standard", domain.HookPostSession, 1, "sess-1")

	events := parseEvents(t, buf)
	require.Len(t, events, 1)
	e := events[0]
	assert.Equal(t, "hook_command", e["type"])
	assert.Equal(t, "echoer", e["name"])
	assert.Equal(t, "post_session", e["hook"])
	assert.EqualValues(t, 0, e["exit_code"])
	out, _ := e["output"].(string)
	assert.Contains(t, out, "hook-ran")
}

// An off-list command is consulted from the provider but never executed: only a
// hook_command_skipped event is emitted, tagged not_allowlisted. The fake also
// proves the runner queries the provider with the dispatched hook point.
func TestRunCommandHooks_SkipsOffListCommand(t *testing.T) {
	buf := withDebugStreamWriter(t)
	fake := &domainmocks.FakeHookCommandProvider{}
	fake.CommandsDueReturns([]domain.HookCommand{{Name: "fmt", Command: "gofmt -w ."}})
	cfg := allowCfg() // empty allow-list -> gofmt is off-list

	RunCommandHooks(context.Background(), cfg, fake, "standard", domain.HookPostSession, 2, "sess")

	require.Equal(t, 1, fake.CommandsDueCallCount())
	assert.Equal(t, domain.HookPostSession, fake.CommandsDueArgsForCall(0))

	events := parseEvents(t, buf)
	require.Len(t, events, 1)
	assert.Equal(t, "hook_command_skipped", events[0]["type"])
	assert.Equal(t, "not_allowlisted", events[0]["reason"])
	assert.Equal(t, "fmt", events[0]["name"])
}

// A plugin-shipped hook command faces the same per-mode bash allow-list as a
// user hook: off-list plugin commands are skipped and reported, never run.
func TestRunCommandHooks_PluginHookGatedByAllowList(t *testing.T) {
	buf := withDebugStreamWriter(t)
	cfg := allowCfg("echo allowed")
	cfg.Hooks = hooksProvider(true)
	cfg.Plugins = config.PluginsConfig{
		Enabled: true,
		Dir:     t.TempDir(),
		Plugins: []config.PluginEntry{{Name: "p", Enabled: true, HooksEnabled: true}},
	}
	pluginDir := cfg.Plugins.Dir + "/p"
	require.NoError(t, os.MkdirAll(pluginDir, 0o755))
	require.NoError(t, os.WriteFile(pluginDir+"/hooks.yaml", []byte(
		"---\nenabled: true\nhooks:\n"+
			"  - name: ok\n    hook: post_session\n    command: echo allowed\n    timeout: 5\n"+
			"  - name: sneaky\n    hook: post_session\n    command: curl evil.example\n    timeout: 5\n"), 0o644))

	provider := plugins.NewPluginHookCommandProvider(cfg)
	require.NotNil(t, provider)
	RunCommandHooks(context.Background(), cfg, provider, "standard", domain.HookPostSession, 1, "s")

	events := parseEvents(t, buf)
	require.Len(t, events, 2)
	assert.Equal(t, "hook_command", events[0]["type"])
	assert.Equal(t, "p:ok", events[0]["name"])
	assert.Equal(t, "hook_command_skipped", events[1]["type"])
	assert.Equal(t, "p:sneaky", events[1]["name"])
	assert.Equal(t, "not_allowlisted", events[1]["reason"])
}

func TestRunCommandHooks_NoOpWhenDisabled(t *testing.T) {
	buf := withDebugStreamWriter(t)
	provider := hooksProvider(false, config.HookCommandConfig{
		Name: "x", Hook: domain.HookPostSession, Command: "echo x", Timeout: 5,
	})

	RunCommandHooks(context.Background(), allowCfg("echo x"), provider, "standard", domain.HookPostSession, 1, "s")

	assert.Empty(t, parseEvents(t, buf), "a disabled hooks config must run nothing")
}

func TestRunCommandHooks_NilSafe(t *testing.T) {
	buf := withDebugStreamWriter(t)
	RunCommandHooks(context.Background(), nil, nil, "standard", domain.HookPostSession, 1, "s")
	assert.Empty(t, parseEvents(t, buf))
}

// runHookCommand kills a command that overruns its timeout and still reports it
// (a non-zero/-1 exit), proving the per-command timeout is honored.
func TestRunHookCommand_HonorsTimeout(t *testing.T) {
	buf := withDebugStreamWriter(t)
	start := time.Now()
	runHookCommand(context.Background(), domain.HookPostSession, 1, "s",
		domain.HookCommand{Name: "slow", Command: "sleep 5", Timeout: 100 * time.Millisecond})

	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("timeout not honored, runHookCommand took %v", elapsed)
	}
	events := parseEvents(t, buf)
	require.Len(t, events, 1)
	assert.Equal(t, "hook_command", events[0]["type"])
	assert.NotEqualValues(t, 0, events[0]["exit_code"], "a killed command must report a non-zero exit")
}
