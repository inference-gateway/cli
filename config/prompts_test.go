package config

import "testing"

// Guards against accidental deletions of default prompts. Every prompt
// field surfaced through prompts.yaml must ship a non-empty default so
// the runtime overlay can fall back to it when a user blanks a key.
func TestDefaultPromptsConfig_AllPromptsPopulated(t *testing.T) {
	cfg := DefaultPromptsConfig()

	cases := map[string]string{
		"agent.system_prompt":                         cfg.Agent.SystemPrompt,
		"agent.system_prompt_plan":                    cfg.Agent.SystemPromptPlan,
		"agent.system_prompt_remote":                  cfg.Agent.SystemPromptRemote,
		"agent.system_reminders.reminder_text":        cfg.Agent.SystemReminders.ReminderText,
		"git.commit_message.system_prompt":            cfg.Git.CommitMessage.SystemPrompt,
		"conversation.title_generation.system_prompt": cfg.Conversation.TitleGeneration.SystemPrompt,
		"init.prompt":                                 cfg.Init.Prompt,
	}

	for key, val := range cases {
		if val == "" {
			t.Errorf("default prompt %q is empty", key)
		}
	}
}

// custom_instructions is intentionally empty - it's a user-supplied
// opt-in. This guards it in the opposite direction so a future "fill in
// a default" change is intentional.
func TestDefaultPromptsConfig_OptionalPromptsBlank(t *testing.T) {
	cfg := DefaultPromptsConfig()

	if cfg.Agent.CustomInstructions != "" {
		t.Errorf("agent.custom_instructions should ship empty, got %q", cfg.Agent.CustomInstructions)
	}
}
