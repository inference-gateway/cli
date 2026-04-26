package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

func TestPromptsConfigService_Load_NonExistentFile(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "non-existent.yaml")

	service := NewPromptsConfigService(configPath)
	cfg, err := service.Load()

	if err != nil {
		t.Fatalf("Load() should not error for non-existent file, got: %v", err)
	}
	if cfg == nil {
		t.Fatal("Load() returned nil config")
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

func TestPromptsConfigService_Load_ValidYAML(t *testing.T) {
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

	service := NewPromptsConfigService(configPath)
	cfg, err := service.Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
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

func TestPromptsConfigService_Load_PartialYAML(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "prompts.yaml")

	yamlContent := `---
agent:
  system_prompt: only this field is set
`

	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write test config file: %v", err)
	}

	service := NewPromptsConfigService(configPath)
	cfg, err := service.Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if cfg.Agent.SystemPrompt != "only this field is set" {
		t.Errorf("Expected partial value, got %q", cfg.Agent.SystemPrompt)
	}
	// Other prompt fields stay zero — getConfigFromViper's overlay is what
	// fills them back in from defaults at runtime.
	if cfg.Agent.SystemPromptPlan != "" {
		t.Errorf("Expected unset plan prompt to be empty, got %q", cfg.Agent.SystemPromptPlan)
	}
	if cfg.Git.CommitMessage.SystemPrompt != "" {
		t.Errorf("Expected unset commit prompt to be empty, got %q", cfg.Git.CommitMessage.SystemPrompt)
	}
}

func TestPromptsConfigService_Save_RoundTrip(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "prompts.yaml")
	service := NewPromptsConfigService(configPath)

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

	if err := service.Save(original); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	loaded, err := service.Load()
	if err != nil {
		t.Fatalf("Load() after save failed: %v", err)
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

func TestPromptsConfigService_Save_CreatesParentDirectory(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "nested", "deep", "prompts.yaml")
	service := NewPromptsConfigService(configPath)

	if err := service.Save(DefaultPromptsConfig()); err != nil {
		t.Fatalf("Save() failed to create nested dirs: %v", err)
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("File not created at nested path: %v", err)
	}
}

func TestPromptsConfigService_Save_StartsWithYAMLDocumentMarker(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "prompts.yaml")
	service := NewPromptsConfigService(configPath)

	if err := service.Save(DefaultPromptsConfig()); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if !strings.HasPrefix(string(data), "---\n") {
		t.Errorf("Saved file should start with YAML document marker, got: %q", string(data[:min(20, len(data))]))
	}
}

func TestDefaultPromptsConfig(t *testing.T) {
	cfg := DefaultPromptsConfig()
	if cfg == nil {
		t.Fatal("DefaultPromptsConfig returned nil")
	}
	if cfg.Agent.SystemPrompt == "" {
		t.Error("agent.system_prompt should be non-empty")
	}
	if cfg.Agent.SystemPromptPlan == "" {
		t.Error("agent.system_prompt_plan should be non-empty")
	}
	if cfg.Agent.SystemPromptRemote == "" {
		t.Error("agent.system_prompt_remote should be non-empty")
	}
	if cfg.Agent.SystemReminders.ReminderText == "" {
		t.Error("agent.system_reminders.reminder_text should be non-empty")
	}
	if cfg.Git.CommitMessage.SystemPrompt == "" {
		t.Error("git.commit_message.system_prompt should be non-empty")
	}
	if cfg.Conversation.TitleGeneration.SystemPrompt == "" {
		t.Error("conversation.title_generation.system_prompt should be non-empty")
	}
	if cfg.Init.Prompt == "" {
		t.Error("init.prompt should be non-empty")
	}
}
