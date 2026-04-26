package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

func TestLoadPrompts_NonExistentFile(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "non-existent.yaml")

	cfg, err := config.LoadPrompts(configPath)
	if err != nil {
		t.Fatalf("LoadPrompts() should not error for non-existent file, got: %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadPrompts() returned nil config")
	}
	if cfg.Agent.SystemPrompt == "" {
		t.Error("Default prompts config should populate agent.system_prompt")
	}
	if cfg.Agent.SystemPromptPlan == "" {
		t.Error("Default prompts config should populate agent.system_prompt_plan")
	}
	if cfg.Git.CommitMessage.SystemPrompt == "" {
		t.Error("Default prompts config should populate git.commit_message.system_prompt")
	}
}

func TestLoadPrompts_ValidYAML(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "prompts.yaml")

	yamlContent := `---
agent:
  system_prompt: custom agent prompt
  system_prompt_plan: custom plan prompt
git:
  commit_message:
    system_prompt: custom commit prompt
init:
  prompt: custom init prompt
`

	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write test config file: %v", err)
	}

	cfg, err := config.LoadPrompts(configPath)
	if err != nil {
		t.Fatalf("LoadPrompts() failed: %v", err)
	}
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

func TestLoadPrompts_PartialYAML(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "prompts.yaml")

	yamlContent := `---
agent:
  system_prompt: only this field is set
`

	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write test config file: %v", err)
	}

	cfg, err := config.LoadPrompts(configPath)
	if err != nil {
		t.Fatalf("LoadPrompts() failed: %v", err)
	}
	if cfg.Agent.SystemPrompt != "only this field is set" {
		t.Errorf("Expected partial value, got %q", cfg.Agent.SystemPrompt)
	}
	// Other prompt fields stay zero — the runtime overlay (cmd applies it
	// on top of LoadPrompts) is what fills them back in from defaults.
	if cfg.Agent.SystemPromptPlan != "" {
		t.Errorf("Expected unset plan prompt to be empty, got %q", cfg.Agent.SystemPromptPlan)
	}
	if cfg.Git.CommitMessage.SystemPrompt != "" {
		t.Errorf("Expected unset commit prompt to be empty, got %q", cfg.Git.CommitMessage.SystemPrompt)
	}
}

func TestSavePrompts_RoundTrip(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "prompts.yaml")

	original := &config.PromptsConfig{
		Agent: config.PromptsAgentConfig{
			SystemPrompt: "round trip system prompt",
			SystemReminders: config.PromptsAgentRemindersConfig{
				ReminderText: "round trip reminder",
			},
		},
		Git: config.PromptsGitConfig{
			CommitMessage: config.PromptsGitCommitMessageConfig{
				SystemPrompt: "round trip commit prompt",
			},
		},
	}

	if err := config.SavePrompts(configPath, original); err != nil {
		t.Fatalf("SavePrompts() failed: %v", err)
	}

	loaded, err := config.LoadPrompts(configPath)
	if err != nil {
		t.Fatalf("LoadPrompts() after save failed: %v", err)
	}
	if loaded.Agent.SystemPrompt != original.Agent.SystemPrompt {
		t.Errorf("agent.system_prompt not preserved, got %q", loaded.Agent.SystemPrompt)
	}
	if loaded.Agent.SystemReminders.ReminderText != original.Agent.SystemReminders.ReminderText {
		t.Errorf("reminder_text not preserved, got %q", loaded.Agent.SystemReminders.ReminderText)
	}
	if loaded.Git.CommitMessage.SystemPrompt != original.Git.CommitMessage.SystemPrompt {
		t.Errorf("git.commit_message.system_prompt not preserved, got %q", loaded.Git.CommitMessage.SystemPrompt)
	}
}

func TestSavePrompts_CreatesParentDirectory(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "nested", "deep", "prompts.yaml")

	if err := config.SavePrompts(configPath, config.DefaultPromptsConfig()); err != nil {
		t.Fatalf("SavePrompts() failed to create nested dirs: %v", err)
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("File not created at nested path: %v", err)
	}
}

func TestSavePrompts_StartsWithYAMLDocumentMarker(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "prompts.yaml")

	if err := config.SavePrompts(configPath, config.DefaultPromptsConfig()); err != nil {
		t.Fatalf("SavePrompts() failed: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if !strings.HasPrefix(string(data), "---\n") {
		t.Errorf("Saved file should start with YAML document marker, got: %q", string(data[:min(20, len(data))]))
	}
}
