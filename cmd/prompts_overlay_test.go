package cmd

import (
	"os"
	"path/filepath"
	"testing"

	require "github.com/stretchr/testify/require"

	config "github.com/inference-gateway/cli/config"
)

// TestLoadConfigFromViper_PromptsDefaultsWhenFileAbsent confirms the
// overlay falls back to in-code defaults when no prompts.yaml exists,
// so freshly-cloned repos still get a working agent prompt.
func TestLoadConfigFromViper_PromptsDefaultsWhenFileAbsent(t *testing.T) {
	withHermeticEnv(t)
	initConfig()

	cfg := Cfg

	defaults := config.DefaultPromptsConfig()
	require.Equal(t, defaults.Agent.SystemPrompt, cfg.Agent.SystemPrompt)
	require.Equal(t, defaults.Agent.SystemPromptPlan, cfg.Agent.SystemPromptPlan)
	require.Equal(t, defaults.Agent.SystemPromptRemote, cfg.Agent.SystemPromptRemote)
	require.Equal(t, defaults.Agent.SystemReminders.ReminderText, cfg.Agent.SystemReminders.ReminderText)
	require.Equal(t, defaults.Git.CommitMessage.SystemPrompt, cfg.Git.CommitMessage.SystemPrompt)
	require.Equal(t, defaults.Conversation.TitleGeneration.SystemPrompt, cfg.Conversation.TitleGeneration.SystemPrompt)
	require.Equal(t, defaults.Init.Prompt, cfg.Init.Prompt)
}

// TestLoadConfigFromViper_PromptsPartialFileFallsBackForUnsetFields
// guards the partial-overlay rule: if a user blanks out (or never sets)
// a single prompt key, the others must still resolve to defaults instead
// of becoming empty strings. Empty prompts at runtime would cause the
// LLM to receive no system instructions.
func TestLoadConfigFromViper_PromptsPartialFileFallsBackForUnsetFields(t *testing.T) {
	withHermeticEnv(t)

	homeDir := os.Getenv("HOME")
	promptsPath := filepath.Join(homeDir, config.ConfigDirName, config.PromptsFileName)
	custom := &config.PromptsConfig{
		Agent: config.PromptsAgentConfig{
			SystemPrompt: "USER OVERRIDE: only this is set",
		},
	}
	require.NoError(t, config.SavePrompts(promptsPath, custom))

	initConfig()
	cfg := Cfg

	defaults := config.DefaultPromptsConfig()
	require.Equal(t, "USER OVERRIDE: only this is set", cfg.Agent.SystemPrompt)
	require.Equal(t, defaults.Agent.SystemPromptPlan, cfg.Agent.SystemPromptPlan, "unset plan prompt should fall back to default")
	require.Equal(t, defaults.Git.CommitMessage.SystemPrompt, cfg.Git.CommitMessage.SystemPrompt, "unset git prompt should fall back to default")
	require.Equal(t, defaults.Init.Prompt, cfg.Init.Prompt, "unset init prompt should fall back to default")
}

// TestLoadConfigFromViper_PromptsEnvOverridesFile pins the precedence
// order: env > file > in-code defaults. Without this guarantee, ops
// teams cannot inject a prompt at deploy time without editing the file
// inside the container image.
func TestLoadConfigFromViper_PromptsEnvOverridesFile(t *testing.T) {
	withHermeticEnv(t)

	homeDir := os.Getenv("HOME")
	promptsPath := filepath.Join(homeDir, config.ConfigDirName, config.PromptsFileName)
	require.NoError(t, config.SavePrompts(promptsPath, &config.PromptsConfig{
		Agent: config.PromptsAgentConfig{SystemPrompt: "from-file"},
	}))
	t.Setenv("INFER_PROMPTS_AGENT_SYSTEM_PROMPT", "from-env")

	initConfig()
	cfg := Cfg

	require.Equal(t, "from-env", cfg.Agent.SystemPrompt)
}
