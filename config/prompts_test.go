package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

// Guards against accidental deletions of default prompts. Every prompt
// field surfaced through prompts.yaml must ship a non-empty default so
// the runtime overlay can fall back to it when a user blanks a key.
func TestDefaultPromptsConfig_AllPromptsPopulated(t *testing.T) {
	cfg := config.DefaultPromptsConfig()

	cases := map[string]string{
		"agent.system_prompt":                         cfg.Agent.SystemPrompt,
		"agent.system_prompt_remote":                  cfg.Agent.SystemPromptRemote,
		"agent.system_prompt_heartbeat":               cfg.Agent.SystemPromptHeartbeat,
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

// custom_instructions and the per-mode adjustment overrides are intentionally
// empty - they're user-supplied opt-ins whose built-in texts live in the
// mode-change reminder guidance (reminders.go). This guards them in the
// opposite direction so a future "fill in a default" change is intentional.
func TestDefaultPromptsConfig_OptionalPromptsBlank(t *testing.T) {
	cfg := config.DefaultPromptsConfig()

	if cfg.Agent.CustomInstructions != "" {
		t.Errorf("agent.custom_instructions should ship empty, got %q", cfg.Agent.CustomInstructions)
	}
	if cfg.Agent.ModeAdjustmentPlan != "" {
		t.Errorf("agent.mode_adjustment_plan should ship empty (built-ins live in the reminder guidance), got %q", cfg.Agent.ModeAdjustmentPlan)
	}
	if cfg.Agent.ModeAdjustmentAuto != "" {
		t.Errorf("agent.mode_adjustment_auto should ship empty (built-ins live in the reminder guidance), got %q", cfg.Agent.ModeAdjustmentAuto)
	}
}

// Reminders moved out of prompts.yaml into their own reminders.yaml; their
// defaults are covered by TestDefaultRemindersConfig in reminders_test.go.

// LoadPrompts backfills unset prompts from DefaultPromptsConfig().
func checkPromptsValidYAML(t *testing.T, cfg *config.PromptsConfig) {
	t.Helper()
	if cfg.Agent.SystemPrompt != "custom agent prompt" {
		t.Errorf("Expected custom system_prompt, got %q", cfg.Agent.SystemPrompt)
	}
	if cfg.Agent.ModeAdjustmentPlan != "custom plan prompt" {
		t.Errorf("Expected custom mode_adjustment_plan key to decode into ModeAdjustmentPlan, got %q", cfg.Agent.ModeAdjustmentPlan)
	}
	if cfg.Git.CommitMessage.SystemPrompt != "custom commit prompt" {
		t.Errorf("Expected custom commit prompt, got %q", cfg.Git.CommitMessage.SystemPrompt)
	}
	if cfg.Init.Prompt != "custom init prompt" {
		t.Errorf("Expected custom init prompt, got %q", cfg.Init.Prompt)
	}
}

func TestLoadPrompts(t *testing.T) {
	defaults := config.DefaultPromptsConfig()

	tests := []struct {
		name  string
		yaml  string
		check func(t *testing.T, cfg *config.PromptsConfig)
	}{
		{
			name: "non-existent file returns populated defaults",
			check: func(t *testing.T, cfg *config.PromptsConfig) {
				if cfg.Agent.SystemPrompt == "" {
					t.Error("Default prompts config should populate agent.system_prompt")
				}
				if cfg.Git.CommitMessage.SystemPrompt == "" {
					t.Error("Default prompts config should populate git.commit_message.system_prompt")
				}
			},
		},
		{
			name: "valid yaml",
			yaml: `---
agent:
  system_prompt: custom agent prompt
  mode_adjustment_plan: custom plan prompt
git:
  commit_message:
    system_prompt: custom commit prompt
init:
  prompt: custom init prompt
`,
			check: checkPromptsValidYAML,
		},
		{
			name: "partial yaml backfills unset prompts",
			yaml: `---
agent:
  system_prompt: only this field is set
`,
			check: func(t *testing.T, cfg *config.PromptsConfig) {
				if cfg.Agent.SystemPrompt != "only this field is set" {
					t.Errorf("Expected user override to be preserved, got %q", cfg.Agent.SystemPrompt)
				}
				if cfg.Agent.ModeAdjustmentPlan != "" {
					t.Errorf("unset mode adjustments must not be backfilled (built-ins live in the reminder guidance), got %q", cfg.Agent.ModeAdjustmentPlan)
				}
				if cfg.Git.CommitMessage.SystemPrompt != defaults.Git.CommitMessage.SystemPrompt {
					t.Errorf("Expected unset commit prompt to be backfilled with default, got %q", cfg.Git.CommitMessage.SystemPrompt)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), "prompts.yaml")
			if tt.yaml != "" {
				if err := os.WriteFile(configPath, []byte(tt.yaml), 0644); err != nil {
					t.Fatalf("Failed to write test config file: %v", err)
				}
			}

			cfg, err := config.LoadPrompts(configPath)
			if err != nil {
				t.Fatalf("LoadPrompts() failed: %v", err)
			}
			if cfg == nil {
				t.Fatal("LoadPrompts() returned nil config")
			}
			tt.check(t, cfg)
		})
	}
}

func TestSavePrompts(t *testing.T) {
	roundTrip := &config.PromptsConfig{
		Agent: config.PromptsAgentConfig{
			SystemPrompt: "round trip system prompt",
		},
		Git: config.PromptsGitConfig{
			CommitMessage: config.PromptsGitCommitMessageConfig{
				SystemPrompt: "round trip commit prompt",
			},
		},
	}

	tests := []struct {
		name  string
		path  []string
		cfg   *config.PromptsConfig
		check func(t *testing.T, path string)
	}{
		{
			name: "round trip preserves prompts",
			path: []string{"prompts.yaml"},
			cfg:  roundTrip,
			check: func(t *testing.T, path string) {
				loaded, err := config.LoadPrompts(path)
				if err != nil {
					t.Fatalf("LoadPrompts() after save failed: %v", err)
				}
				if loaded.Agent.SystemPrompt != roundTrip.Agent.SystemPrompt {
					t.Errorf("agent.system_prompt not preserved, got %q", loaded.Agent.SystemPrompt)
				}
				if loaded.Git.CommitMessage.SystemPrompt != roundTrip.Git.CommitMessage.SystemPrompt {
					t.Errorf("git.commit_message.system_prompt not preserved, got %q", loaded.Git.CommitMessage.SystemPrompt)
				}
			},
		},
		{
			name: "creates parent directory",
			path: []string{"nested", "deep", "prompts.yaml"},
			cfg:  config.DefaultPromptsConfig(),
		},
		{
			name: "starts with yaml document marker",
			path: []string{"prompts.yaml"},
			cfg:  config.DefaultPromptsConfig(),
			check: func(t *testing.T, path string) {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("ReadFile failed: %v", err)
				}
				if !strings.HasPrefix(string(data), "---\n") {
					t.Errorf("Saved file should start with YAML document marker, got: %q", string(data[:min(20, len(data))]))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(append([]string{t.TempDir()}, tt.path...)...)
			if err := config.SavePrompts(path, tt.cfg); err != nil {
				t.Fatalf("SavePrompts() failed: %v", err)
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("File not created at %q: %v", path, err)
			}
			if tt.check != nil {
				tt.check(t, path)
			}
		})
	}
}
