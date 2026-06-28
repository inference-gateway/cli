package cmd

import (
	"os"
	"path/filepath"
	"testing"

	require "github.com/stretchr/testify/require"

	config "github.com/inference-gateway/cli/config"
)

// TestLoadConfigFromViper_PromptsDefaultsWhenFileAbsent confirms cfg.Prompts
// falls back to in-code defaults when no prompts.yaml exists, so
// freshly-cloned repos still get a working agent prompt.
func TestLoadConfigFromViper_PromptsDefaultsWhenFileAbsent(t *testing.T) {
	withHermeticEnv(t)
	initConfig()

	cfg := Cfg

	defaults := config.DefaultPromptsConfig()
	require.Equal(t, defaults.Agent.SystemPrompt, cfg.Prompts.Agent.SystemPrompt)
	require.Equal(t, defaults.Agent.SystemPromptPlan, cfg.Prompts.Agent.SystemPromptPlan)
	require.Equal(t, defaults.Agent.SystemPromptRemote, cfg.Prompts.Agent.SystemPromptRemote)
	require.Equal(t, defaults.Agent.SystemPromptHeartbeat, cfg.Prompts.Agent.SystemPromptHeartbeat)
	require.NotEmpty(t, cfg.Prompts.Agent.SystemPromptHeartbeat, "heartbeat prompt must have a non-empty default")
	require.Equal(t, defaults.Agent.SystemReminders.Reminders, cfg.Prompts.Agent.SystemReminders.Reminders)
	require.Equal(t, defaults.Git.CommitMessage.SystemPrompt, cfg.Prompts.Git.CommitMessage.SystemPrompt)
	require.Equal(t, defaults.Conversation.TitleGeneration.SystemPrompt, cfg.Prompts.Conversation.TitleGeneration.SystemPrompt)
	require.Equal(t, defaults.Init.Prompt, cfg.Prompts.Init.Prompt)
}

// TestLoadConfigFromViper_PromptsPartialFileFallsBackForUnsetFields
// guards the partial-load rule: if a user blanks out (or never sets) a
// single prompt key, the others must still resolve to defaults instead
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
	require.Equal(t, "USER OVERRIDE: only this is set", cfg.Prompts.Agent.SystemPrompt)
	require.Equal(t, defaults.Agent.SystemPromptPlan, cfg.Prompts.Agent.SystemPromptPlan, "unset plan prompt should fall back to default")
	require.Equal(t, defaults.Agent.SystemPromptHeartbeat, cfg.Prompts.Agent.SystemPromptHeartbeat, "unset heartbeat prompt should fall back to default")
	require.Equal(t, defaults.Git.CommitMessage.SystemPrompt, cfg.Prompts.Git.CommitMessage.SystemPrompt, "unset git prompt should fall back to default")
	require.Equal(t, defaults.Init.Prompt, cfg.Prompts.Init.Prompt, "unset init prompt should fall back to default")
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

	require.Equal(t, "from-env", cfg.Prompts.Agent.SystemPrompt)
}

// TestLoadConfigFromViper_ToolDescriptionEnvOverride mirrors the
// agent-prompt env-override guarantee for the new tool description
// slots - confirms a single tool can be retuned at deploy time
// without editing prompts.yaml.
func TestLoadConfigFromViper_ToolDescriptionEnvOverride(t *testing.T) {
	withHermeticEnv(t)

	t.Setenv("INFER_PROMPTS_TOOLS_BASH_DESCRIPTION", "bash from env")
	t.Setenv("INFER_PROMPTS_TOOLS_A2A_SUBMIT_TASK_DESCRIPTION", "a2a from env")

	initConfig()
	cfg := Cfg

	require.Equal(t, "bash from env", cfg.Prompts.Tools.Bash.Description)
	require.Equal(t, "a2a from env", cfg.Prompts.Tools.A2ASubmitTask.Description)

	defaults := config.DefaultPromptsConfig()
	require.Equal(t, defaults.Tools.Read.Description, cfg.Prompts.Tools.Read.Description,
		"untouched tool descriptions must still resolve to defaults")
}

// TestLoadConfigFromViper_ToolDescriptionsDefaultWhenFileAbsent
// guards the load-side contract: cfg.Prompts.Tools is fully populated
// even when the user has no prompts.yaml at all. Without this, every
// tool's Definition() would emit an empty description on a fresh
// install.
func TestLoadConfigFromViper_ToolDescriptionsDefaultWhenFileAbsent(t *testing.T) {
	withHermeticEnv(t)
	initConfig()

	cfg := Cfg
	defaults := config.DefaultPromptsConfig()

	require.Equal(t, defaults.Tools.Bash.Description, cfg.Prompts.Tools.Bash.Description)
	require.Equal(t, defaults.Tools.Read.Description, cfg.Prompts.Tools.Read.Description)
	require.Equal(t, defaults.Tools.Edit.Description, cfg.Prompts.Tools.Edit.Description)
	require.Equal(t, defaults.Tools.GetLatestScreenshot.Description, cfg.Prompts.Tools.GetLatestScreenshot.Description)
}
