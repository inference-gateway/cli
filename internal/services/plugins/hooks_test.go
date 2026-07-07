package plugins

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	require "github.com/stretchr/testify/require"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
)

// writePluginHooks materializes a hooks.yaml for plugin name under the
// registry's storage dir, as the installer would.
func writePluginHooks(t *testing.T, dir, name, content string) {
	t.Helper()
	root := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, config.HooksFileName), []byte(content), 0o644))
}

// hooksCfg builds a config with user hooks enabled, a temp plugin storage dir,
// and the given registry entries.
func hooksCfg(t *testing.T, userHooks []config.HookCommandConfig, entries ...config.PluginEntry) *config.Config {
	t.Helper()
	return &config.Config{
		Hooks: config.HooksConfig{Enabled: true, Hooks: userHooks},
		Plugins: config.PluginsConfig{
			Enabled: true,
			Dir:     t.TempDir(),
			Plugins: entries,
		},
	}
}

const pluginHooksYAML = "---\nenabled: true\nhooks:\n  - name: fmt\n    hook: post_session\n    command: gofmt -w .\n    timeout: 30\n"

func TestNewPluginHookCommandProvider_NilWhenDisabled(t *testing.T) {
	require.Nil(t, NewPluginHookCommandProvider(nil))

	cfg := &config.Config{Plugins: config.PluginsConfig{Enabled: false}}
	require.Nil(t, NewPluginHookCommandProvider(cfg))
}

func TestCommandsDue_MasterHooksSwitchGatesEverything(t *testing.T) {
	cfg := hooksCfg(t, nil, config.PluginEntry{Name: "p", Enabled: true, HooksEnabled: true})
	cfg.Hooks.Enabled = false
	writePluginHooks(t, cfg.Plugins.Dir, "p", pluginHooksYAML)

	provider := NewPluginHookCommandProvider(cfg)
	require.NotNil(t, provider)
	require.Nil(t, provider.CommandsDue(domain.HookPostSession))
}

func TestCommandsDue_PluginHooksAreOptIn(t *testing.T) {
	tests := []struct {
		name  string
		entry config.PluginEntry
		want  int
	}{
		{"hooks not enabled", config.PluginEntry{Name: "p", Enabled: true, HooksEnabled: false}, 0},
		{"plugin disabled", config.PluginEntry{Name: "p", Enabled: false, HooksEnabled: true}, 0},
		{"both enabled", config.PluginEntry{Name: "p", Enabled: true, HooksEnabled: true}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := hooksCfg(t, nil, tt.entry)
			writePluginHooks(t, cfg.Plugins.Dir, "p", pluginHooksYAML)

			due := NewPluginHookCommandProvider(cfg).CommandsDue(domain.HookPostSession)
			require.Len(t, due, tt.want)
		})
	}
}

func TestCommandsDue_UserHooksFirstThenPluginsInRegistryOrder(t *testing.T) {
	user := []config.HookCommandConfig{{Name: "mine", Hook: domain.HookPostSession, Command: "echo user", Timeout: 5}}
	cfg := hooksCfg(t, user,
		config.PluginEntry{Name: "alpha", Enabled: true, HooksEnabled: true},
		config.PluginEntry{Name: "beta", Enabled: true, HooksEnabled: true},
	)
	writePluginHooks(t, cfg.Plugins.Dir, "alpha", pluginHooksYAML)
	writePluginHooks(t, cfg.Plugins.Dir, "beta", pluginHooksYAML)

	due := NewPluginHookCommandProvider(cfg).CommandsDue(domain.HookPostSession)
	require.Len(t, due, 3)
	require.Equal(t, "mine", due[0].Name)
	require.Equal(t, "alpha:fmt", due[1].Name)
	require.Equal(t, "beta:fmt", due[2].Name)
	require.Equal(t, 30*time.Second, due[1].Timeout)
}

func TestCommandsDue_FiltersByHookPoint(t *testing.T) {
	cfg := hooksCfg(t, nil, config.PluginEntry{Name: "p", Enabled: true, HooksEnabled: true})
	writePluginHooks(t, cfg.Plugins.Dir, "p", pluginHooksYAML)

	provider := NewPluginHookCommandProvider(cfg)
	require.Len(t, provider.CommandsDue(domain.HookPostSession), 1)
	require.Empty(t, provider.CommandsDue(domain.HookPreSession))
}

func TestCommandsDue_MissingPluginHooksYAML(t *testing.T) {
	user := []config.HookCommandConfig{{Name: "mine", Hook: domain.HookPostSession, Command: "echo user", Timeout: 5}}
	cfg := hooksCfg(t, user, config.PluginEntry{Name: "p", Enabled: true, HooksEnabled: true})

	due := NewPluginHookCommandProvider(cfg).CommandsDue(domain.HookPostSession)
	require.Len(t, due, 1)
	require.Equal(t, "mine", due[0].Name)
}
