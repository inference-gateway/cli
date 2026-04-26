package cmd

import (
	"os"
	"path/filepath"
	"testing"

	require "github.com/stretchr/testify/require"

	config "github.com/inference-gateway/cli/config"
)

// TestLoadConfigFromViper_ChannelsDefaultsWhenFileAbsent confirms that
// runtime falls back to DefaultChannelsConfig when no channels.yaml exists,
// so a fresh checkout doesn't crash the channels-manager daemon when it
// tries to read cfg.Channels.
func TestLoadConfigFromViper_ChannelsDefaultsWhenFileAbsent(t *testing.T) {
	withHermeticEnv(t)
	initConfig()

	defaults := config.DefaultChannelsConfig()
	require.Equal(t, defaults.Enabled, Cfg.Channels.Enabled)
	require.Equal(t, defaults.MaxWorkers, Cfg.Channels.MaxWorkers)
	require.Equal(t, defaults.RequireApproval, Cfg.Channels.RequireApproval)
	require.Equal(t, defaults.Telegram.PollTimeout, Cfg.Channels.Telegram.PollTimeout)
	require.Equal(t, defaults.WhatsApp.WebhookPort, Cfg.Channels.WhatsApp.WebhookPort)
}

// TestLoadConfigFromViper_ChannelsLoadsFromUserspace verifies the project
// → userspace fallback by writing channels.yaml only to ~/.infer/ (the
// hermetic HOME) and confirming initConfig picks it up.
func TestLoadConfigFromViper_ChannelsLoadsFromUserspace(t *testing.T) {
	withHermeticEnv(t)

	homeDir := os.Getenv("HOME")
	channelsPath := filepath.Join(homeDir, config.ConfigDirName, config.ChannelsFileName)
	require.NoError(t, config.SaveChannels(channelsPath, &config.ChannelsConfig{
		Enabled:    true,
		MaxWorkers: 12,
		Telegram: config.TelegramChannelConfig{
			Enabled:  true,
			BotToken: "from-userspace",
		},
	}))

	initConfig()

	require.True(t, Cfg.Channels.Enabled)
	require.Equal(t, 12, Cfg.Channels.MaxWorkers)
	require.True(t, Cfg.Channels.Telegram.Enabled)
	require.Equal(t, "from-userspace", Cfg.Channels.Telegram.BotToken)
}

// TestLoadConfigFromViper_ChannelsEnvOverridesFile pins the precedence
// order: env > file > in-code defaults. The docker-compose example in
// examples/telegram-channel ships INFER_CHANNELS_TELEGRAM_BOT_TOKEN as an
// env var rather than committing the secret to channels.yaml, so this
// path must keep working.
func TestLoadConfigFromViper_ChannelsEnvOverridesFile(t *testing.T) {
	withHermeticEnv(t)

	homeDir := os.Getenv("HOME")
	channelsPath := filepath.Join(homeDir, config.ConfigDirName, config.ChannelsFileName)
	require.NoError(t, config.SaveChannels(channelsPath, &config.ChannelsConfig{
		Enabled:  false,
		Telegram: config.TelegramChannelConfig{Enabled: false, BotToken: "from-file"},
	}))

	t.Setenv("INFER_CHANNELS_ENABLED", "true")
	t.Setenv("INFER_CHANNELS_TELEGRAM_ENABLED", "true")
	t.Setenv("INFER_CHANNELS_TELEGRAM_BOT_TOKEN", "from-env")
	t.Setenv("INFER_CHANNELS_TELEGRAM_ALLOWED_USERS", "111, 222")
	t.Setenv("INFER_CHANNELS_MAX_WORKERS", "9")
	t.Setenv("INFER_CHANNELS_REQUIRE_APPROVAL", "false")

	initConfig()

	require.True(t, Cfg.Channels.Enabled, "INFER_CHANNELS_ENABLED should win")
	require.True(t, Cfg.Channels.Telegram.Enabled)
	require.Equal(t, "from-env", Cfg.Channels.Telegram.BotToken)
	require.Equal(t, []string{"111", "222"}, Cfg.Channels.Telegram.AllowedUsers)
	require.Equal(t, 9, Cfg.Channels.MaxWorkers)
	require.False(t, Cfg.Channels.RequireApproval)
}

// TestLoadConfigFromViper_ChannelsBlockInConfigYAMLIsIgnored asserts the
// hard contract from issue #441: a stale `channels:` block in config.yaml
// must be ignored at runtime. Only channels.yaml feeds cfg.Channels.
// `infer init` provides the migration path; the loader does not.
func TestLoadConfigFromViper_ChannelsBlockInConfigYAMLIsIgnored(t *testing.T) {
	withHermeticEnv(t)

	homeDir := os.Getenv("HOME")
	configDir := filepath.Join(homeDir, config.ConfigDirName)
	require.NoError(t, os.MkdirAll(configDir, 0755))

	configYAML := `---
channels:
  enabled: true
  telegram:
    enabled: true
    bot_token: legacy-token
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, config.ConfigFileName), []byte(configYAML), 0644))

	initConfig()

	require.False(t, Cfg.Channels.Enabled, "legacy channels: block in config.yaml must not enable channels")
	require.False(t, Cfg.Channels.Telegram.Enabled)
	require.Empty(t, Cfg.Channels.Telegram.BotToken)
}

func TestGetEffectiveChannelsConfigPath_PrefersProject(t *testing.T) {
	withHermeticEnv(t)

	require.NoError(t, os.MkdirAll(config.ConfigDirName, 0755))
	projectPath := config.DefaultChannelsPath
	require.NoError(t, os.WriteFile(projectPath, []byte("---\nenabled: true\n"), 0644))

	got := getEffectiveChannelsConfigPath()
	require.Equal(t, projectPath, got)
}

func TestGetEffectiveChannelsConfigPath_FallsBackToUserspace(t *testing.T) {
	withHermeticEnv(t)

	projectDir := t.TempDir()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(projectDir))
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	homeDir := os.Getenv("HOME")
	userPath := filepath.Join(homeDir, config.ConfigDirName, config.ChannelsFileName)
	require.NoError(t, os.MkdirAll(filepath.Dir(userPath), 0755))
	require.NoError(t, os.WriteFile(userPath, []byte("---\nenabled: true\n"), 0644))

	got := getEffectiveChannelsConfigPath()
	require.Equal(t, userPath, got)
}

func TestGetEffectiveChannelsConfigPath_NeitherExists(t *testing.T) {
	withHermeticEnv(t)

	got := getEffectiveChannelsConfigPath()
	require.Equal(t, config.DefaultChannelsPath, got)
}
