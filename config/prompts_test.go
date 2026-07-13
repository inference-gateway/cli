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
		"agent.system_prompt_plan":                    cfg.Agent.SystemPromptPlan,
		"agent.system_prompt_auto":                    cfg.Agent.SystemPromptAuto,
		"agent.system_prompt_remote":                  cfg.Agent.SystemPromptRemote,
		"agent.system_prompt_heartbeat":               cfg.Agent.SystemPromptHeartbeat,
		"git.commit_message.system_prompt":            cfg.Git.CommitMessage.SystemPrompt,
		"conversation.title_generation.system_prompt": cfg.Conversation.TitleGeneration.SystemPrompt,
		"init.prompt":                                 cfg.Init.Prompt,
		"tools.Bash.description":                      cfg.Tools.Bash.Description,
		"tools.BashOutput.description":                cfg.Tools.BashOutput.Description,
		"tools.KillShell.description":                 cfg.Tools.KillShell.Description,
		"tools.ListShells.description":                cfg.Tools.ListShells.Description,
		"tools.Read.description":                      cfg.Tools.Read.Description,
		"tools.Write.description":                     cfg.Tools.Write.Description,
		"tools.Edit.description":                      cfg.Tools.Edit.Description,
		"tools.MultiEdit.description":                 cfg.Tools.MultiEdit.Description,
		"tools.Delete.description":                    cfg.Tools.Delete.Description,
		"tools.Grep.description":                      cfg.Tools.Grep.Description,
		"tools.Tree.description":                      cfg.Tools.Tree.Description,
		"tools.TodoWrite.description":                 cfg.Tools.TodoWrite.Description,
		"tools.RequestPlanApproval.description":       cfg.Tools.RequestPlanApproval.Description,
		"tools.WebFetch.description":                  cfg.Tools.WebFetch.Description,
		"tools.WebSearch.description":                 cfg.Tools.WebSearch.Description,
		"tools.Schedule.description":                  cfg.Tools.Schedule.Description,
		"tools.A2A_QueryAgent.description":            cfg.Tools.A2AQueryAgent.Description,
		"tools.A2A_QueryTask.description":             cfg.Tools.A2AQueryTask.Description,
		"tools.A2A_SubmitTask.description":            cfg.Tools.A2ASubmitTask.Description,
		"tools.MouseMove.description":                 cfg.Tools.MouseMove.Description,
		"tools.MouseClick.description":                cfg.Tools.MouseClick.Description,
		"tools.MouseScroll.description":               cfg.Tools.MouseScroll.Description,
		"tools.KeyboardType.description":              cfg.Tools.KeyboardType.Description,
		"tools.GetFocusedApp.description":             cfg.Tools.GetFocusedApp.Description,
		"tools.ActivateApp.description":               cfg.Tools.ActivateApp.Description,
		"tools.GetLatestScreenshot.description":       cfg.Tools.GetLatestScreenshot.Description,
	}

	for key, val := range cases {
		if val == "" {
			t.Errorf("default prompt %q is empty", key)
		}
	}
}

// The plan-mode prompt advertises a fixed Markdown section template to
// the model. This guards the template against accidental edits and makes
// the contract with docs/plan-mode.md explicit.
func TestDefaultPromptsConfig_PlanPromptStructure(t *testing.T) {
	cfg := config.DefaultPromptsConfig()
	plan := cfg.Agent.SystemPromptPlan

	wantSections := []string{
		"## Context",
		"## Files to Modify",
		"## Current Code",
		"## Changes",
		"## Performance Impact",
		"## Critical Files",
		"## Edge Cases",
		"## Verification",
	}
	for _, section := range wantSections {
		if !strings.Contains(plan, section) {
			t.Errorf("plan-mode system prompt missing section heading %q", section)
		}
	}

	if !strings.Contains(plan, "title") {
		t.Errorf("plan-mode system prompt should mention the 'title' parameter")
	}

	desc := cfg.Tools.RequestPlanApproval.Description
	if !strings.Contains(desc, "title") || !strings.Contains(desc, "plan") {
		t.Errorf("RequestPlanApproval description should mention both 'title' and 'plan' parameters, got %q", desc)
	}
	if !strings.Contains(desc, "<configDir>/plans/") {
		t.Errorf("RequestPlanApproval description should mention the on-disk path, got %q", desc)
	}
}

// custom_instructions is intentionally empty - it's a user-supplied
// opt-in. This guards it in the opposite direction so a future "fill in
// a default" change is intentional.
func TestDefaultPromptsConfig_OptionalPromptsBlank(t *testing.T) {
	cfg := config.DefaultPromptsConfig()

	if cfg.Agent.CustomInstructions != "" {
		t.Errorf("agent.custom_instructions should ship empty, got %q", cfg.Agent.CustomInstructions)
	}
}

// Reminders moved out of prompts.yaml into their own reminders.yaml; their
// defaults are covered by TestDefaultRemindersConfig in reminders_test.go.

// LoadPrompts backfills unset fields from DefaultPromptsConfig() (via
// mergeToolDefaults for tools), and tool YAML keys use the LLM-visible
// names (PascalCase or A2A_* forms) - both contracts are guarded here.
func checkPromptsValidYAML(t *testing.T, cfg *config.PromptsConfig) {
	t.Helper()
	if cfg.Agent.SystemPrompt != "custom agent prompt" {
		t.Errorf("Expected custom system_prompt, got %q", cfg.Agent.SystemPrompt)
	}
	if cfg.Agent.SystemPromptPlan != "custom plan prompt" {
		t.Errorf("Expected custom plan prompt, got %q", cfg.Agent.SystemPromptPlan)
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
				if cfg.Agent.SystemPromptPlan == "" {
					t.Error("Default prompts config should populate agent.system_prompt_plan")
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
  system_prompt_plan: custom plan prompt
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
				if cfg.Agent.SystemPromptPlan != defaults.Agent.SystemPromptPlan {
					t.Errorf("Expected unset plan prompt to be backfilled with default, got %q", cfg.Agent.SystemPromptPlan)
				}
				if cfg.Git.CommitMessage.SystemPrompt != defaults.Git.CommitMessage.SystemPrompt {
					t.Errorf("Expected unset commit prompt to be backfilled with default, got %q", cfg.Git.CommitMessage.SystemPrompt)
				}
			},
		},
		{
			name: "partial tool override backfills other tools",
			yaml: `---
tools:
  Bash:
    description: my custom bash description
`,
			check: func(t *testing.T, cfg *config.PromptsConfig) {
				if cfg.Tools.Bash.Description != "my custom bash description" {
					t.Errorf("Expected Bash override to be preserved, got %q", cfg.Tools.Bash.Description)
				}
				if cfg.Tools.Read.Description != defaults.Tools.Read.Description {
					t.Errorf("Expected unset Read description to be backfilled, got %q", cfg.Tools.Read.Description)
				}
				if cfg.Tools.Edit.Description != defaults.Tools.Edit.Description {
					t.Errorf("Expected unset Edit description to be backfilled, got %q", cfg.Tools.Edit.Description)
				}
				if cfg.Tools.A2ASubmitTask.Description != defaults.Tools.A2ASubmitTask.Description {
					t.Errorf("Expected unset A2A_SubmitTask description to be backfilled, got %q", cfg.Tools.A2ASubmitTask.Description)
				}
			},
		},
		{
			name: "tool yaml key contract",
			yaml: `---
tools:
  MultiEdit:
    description: pascal case worked
  A2A_SubmitTask:
    description: a2a key worked
`,
			check: func(t *testing.T, cfg *config.PromptsConfig) {
				if cfg.Tools.MultiEdit.Description != "pascal case worked" {
					t.Errorf("Expected MultiEdit YAML key to map to MultiEdit field, got %q", cfg.Tools.MultiEdit.Description)
				}
				if cfg.Tools.A2ASubmitTask.Description != "a2a key worked" {
					t.Errorf("Expected A2A_SubmitTask YAML key to map to A2ASubmitTask field, got %q", cfg.Tools.A2ASubmitTask.Description)
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
