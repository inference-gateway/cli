package runtime

import (
	"os"
	"path/filepath"
	"testing"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	config "github.com/inference-gateway/cli/config"
)

// Every section, config.yaml and sidecar alike, takes INFER_* overrides through
// the one Viper: struct-tag keys, INFER prefix, dot-to-underscore replacer.
func TestEnvOverridesReachEverySection(t *testing.T) {
	withHermeticEnv(t)
	env := map[string]string{
		"INFER_A2A_SERVER_ENABLED":                              "true",
		"INFER_A2A_SERVER_HOST":                                 "0.0.0.0",
		"INFER_A2A_SERVER_PORT":                                 "9443",
		"INFER_CHAT_SUGGESTIONS_ENABLED":                        "true",
		"INFER_CHAT_SUGGESTIONS_CONTEXT_MESSAGES":               "6",
		"INFER_CHAT_INPUT_MAX_LINES":                            "7",
		"INFER_TEXT_TO_SPEECH_ENABLED":                          "true",
		"INFER_TEXT_TO_SPEECH_OUTPUT_DIR":                       "/tmp/tts-out",
		"INFER_MEMORY_BACKEND_TYPE":                             "git",
		"INFER_MEMORY_BACKEND_GIT_REPO":                         "git@github.com:o/r.git",
		"INFER_MEMORY_BACKEND_GIT_TIMEOUT":                      "30",
		"INFER_MEMORY_MAX_CHARS":                                "1500",
		"INFER_MCP_ENABLED":                                     "true",
		"INFER_MCP_CONNECTION_TIMEOUT":                          "45",
		"INFER_REMINDERS_ENABLED":                               "false",
		"INFER_CHANNELS_MAX_WORKERS":                            " 9 ",
		"INFER_CHANNELS_REQUIRE_APPROVAL":                       " false ",
		"INFER_CHAT_KEYBINDINGS_BINDINGS_HELP_TOGGLE_HELP_KEYS": "ctrl+h, f1",
		"INFER_GATEWAY_TLS_CA_FILE":                             "~/gateway-ca.crt",
		"INFER_GATEWAY_TLS_CERT_FILE":                           "~/gateway-client.crt",
		"INFER_GATEWAY_TLS_KEY_FILE":                            "~/gateway-client.key",
	}
	for k, v := range env {
		t.Setenv(k, v)
	}

	cfg := loadTestConfig(t)

	assert.Equal(t, config.A2AServerConfig{Enabled: true, Host: "0.0.0.0", Port: 9443, Mode: "standard"}, cfg.A2A.Server)
	assert.True(t, cfg.Chat.Suggestions.Enabled)
	assert.Equal(t, 6, cfg.Chat.Suggestions.ContextMessages)
	assert.Equal(t, 512, cfg.Chat.Suggestions.MaxTokens, "untouched keys keep their default")
	assert.Equal(t, 7, cfg.Chat.InputMaxLines)
	assert.True(t, cfg.TextToSpeech.Enabled)
	assert.Equal(t, "/tmp/tts-out", cfg.TextToSpeech.OutputDir)
	assert.Equal(t, "git", cfg.Memory.Backend.Type)
	assert.Equal(t, "git@github.com:o/r.git", cfg.Memory.Backend.Git.Repo)
	assert.Equal(t, 30, cfg.Memory.Backend.Git.Timeout)
	assert.Equal(t, 1500, cfg.Memory.MaxChars)
	assert.True(t, cfg.MCP.Enabled)
	assert.Equal(t, 45, cfg.MCP.ConnectionTimeout)
	assert.False(t, cfg.Reminders.Enabled)
	assert.Equal(t, 9, cfg.Channels.MaxWorkers, "padded numbers parse")
	assert.False(t, cfg.Channels.RequireApproval)
	assert.Equal(t, []string{"ctrl+h", "f1"}, cfg.Chat.Keybindings.Bindings["help_toggle_help"].Keys)
	assert.Equal(t, config.PeerTLSConfig{CAFile: "~/gateway-ca.crt", CertFile: "~/gateway-client.crt", KeyFile: "~/gateway-client.key"}, cfg.Gateway.TLS)
}

// A set-but-empty variable is the zero value, a value that does not parse
// fails the load instead of silently flipping a safety default.
func TestEnvOverridesEmptyAndInvalid(t *testing.T) {
	withHermeticEnv(t)

	t.Setenv("INFER_CHANNELS_REQUIRE_APPROVAL", "")
	t.Setenv("INFER_CHANNELS_MAX_WORKERS", "")
	t.Setenv("INFER_PROMPTS_AGENT_CUSTOM_INSTRUCTIONS", "")
	cfg := loadTestConfig(t)
	assert.False(t, cfg.Channels.RequireApproval)
	assert.Equal(t, 0, cfg.Channels.MaxWorkers)
	assert.Empty(t, cfg.Prompts.Agent.CustomInstructions)

	t.Setenv("INFER_CHANNELS_MAX_WORKERS", "30s")
	_, _, err := loadConfig(rootCmd)
	require.ErrorContains(t, err, "channels.max_workers")
}

// Pointer options are tri-state: unset stays nil, env sets either value, and
// a registered default survives a load with no override.
func TestPointerOptions(t *testing.T) {
	withHermeticEnv(t)

	cfg := loadTestConfig(t)
	require.Nil(t, cfg.TextToSpeech.RequireApproval, "ships unset")
	require.NotNil(t, cfg.Tools.Write.RequireApproval)
	assert.Equal(t, *config.DefaultConfig().Tools.Write.RequireApproval, *cfg.Tools.Write.RequireApproval)
	assert.True(t, cfg.ComputerUse.Recording.ApprovalRequired())

	t.Setenv("INFER_TEXT_TO_SPEECH_REQUIRE_APPROVAL", "true")
	t.Setenv("INFER_TOOLS_AGENT_REQUIRE_APPROVAL", "false")
	t.Setenv("INFER_COMPUTER_USE_RECORDING_REQUIRE_APPROVAL", "false")
	cfg = loadTestConfig(t)
	require.NotNil(t, cfg.TextToSpeech.RequireApproval)
	assert.True(t, *cfg.TextToSpeech.RequireApproval)
	require.NotNil(t, cfg.Tools.Agent.RequireApproval)
	assert.False(t, *cfg.Tools.Agent.RequireApproval)
	assert.False(t, cfg.ComputerUse.Recording.ApprovalRequired())
}

// Precedence: defaults < config file < env, for a pointer option.
func TestPointerOptionPrecedence(t *testing.T) {
	withHermeticEnv(t)
	configPath := filepath.Join(os.Getenv("HOME"), config.ConfigDirName, config.ConfigFileName)
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))
	require.NoError(t, os.WriteFile(configPath, []byte("text_to_speech:\n  enabled: true\n  require_approval: false\n"), 0o644))

	cfg := loadTestConfig(t)
	require.NotNil(t, cfg.TextToSpeech.RequireApproval)
	assert.False(t, *cfg.TextToSpeech.RequireApproval, "config file wins over defaults")

	t.Setenv("INFER_TEXT_TO_SPEECH_REQUIRE_APPROVAL", "true")
	cfg = loadTestConfig(t)
	assert.True(t, *cfg.TextToSpeech.RequireApproval, "env wins over config file")
}

func TestPruneMemoryRemindersIfDisabled(t *testing.T) {
	countMemory := func(rs []config.ReminderConfig) int {
		n := 0
		for _, r := range rs {
			if r.Name == "memory-consult" || r.Name == "memory-hygiene" {
				n++
			}
		}
		return n
	}
	has := func(rs []config.ReminderConfig, name string) bool {
		for _, r := range rs {
			if r.Name == name {
				return true
			}
		}
		return false
	}

	disabled := &config.Config{Reminders: *config.DefaultRemindersConfig()}
	pruneMemoryRemindersIfDisabled(disabled)
	assert.Zero(t, countMemory(disabled.Reminders.Reminders), "memory reminders are pruned when memory is disabled")
	assert.True(t, has(disabled.Reminders.Reminders, "todo-hygiene"))

	enabled := &config.Config{Reminders: *config.DefaultRemindersConfig()}
	enabled.Memory.Enabled = true
	pruneMemoryRemindersIfDisabled(enabled)
	assert.Equal(t, 2, countMemory(enabled.Reminders.Reminders))
}
