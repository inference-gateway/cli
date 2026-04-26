package services

import (
	"os"
	"path/filepath"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

func TestKeybindingsConfigService_Load_NonExistentFile(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "non-existent.yaml")

	service := NewKeybindingsConfigService(configPath)
	cfg, err := service.Load()

	if err != nil {
		t.Fatalf("Load() should not error for non-existent file, got: %v", err)
	}
	if cfg == nil {
		t.Fatal("Load() returned nil config")
	}
	if !cfg.Enabled {
		t.Error("Default keybindings config should be enabled")
	}
	if len(cfg.Bindings) == 0 {
		t.Error("Default keybindings config should have bindings populated")
	}
	defaults := config.GetDefaultKeybindings()
	if len(cfg.Bindings) != len(defaults) {
		t.Errorf("Expected %d default bindings, got %d", len(defaults), len(cfg.Bindings))
	}
}

func TestKeybindingsConfigService_Load_ValidYAML(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "keybindings.yaml")

	yamlContent := `---
enabled: true
bindings:
  mode_cycle_agent_mode:
    keys:
      - ctrl+m
    description: cycle agent mode
    category: mode
    enabled: true
`

	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write test config file: %v", err)
	}

	service := NewKeybindingsConfigService(configPath)
	cfg, err := service.Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if !cfg.Enabled {
		t.Error("Expected Enabled to be true")
	}
	binding, ok := cfg.Bindings["mode_cycle_agent_mode"]
	if !ok {
		t.Fatal("Expected mode_cycle_agent_mode binding to be present")
	}
	if len(binding.Keys) != 1 || binding.Keys[0] != "ctrl+m" {
		t.Errorf("Expected keys [ctrl+m], got %v", binding.Keys)
	}
}

func TestKeybindingsConfigService_Save_RoundTrip(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "keybindings.yaml")
	service := NewKeybindingsConfigService(configPath)

	enabled := true
	original := &config.KeybindingsConfig{
		Enabled: true,
		Bindings: map[string]config.KeyBindingEntry{
			"global_quit": {
				Keys:        []string{"ctrl+q"},
				Description: "exit application",
				Category:    "global",
				Enabled:     &enabled,
			},
		},
	}

	if err := service.Save(original); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("Save() did not create file: %v", err)
	}

	loaded, err := service.Load()
	if err != nil {
		t.Fatalf("Load() after save failed: %v", err)
	}
	binding, ok := loaded.Bindings["global_quit"]
	if !ok {
		t.Fatal("Expected global_quit to round-trip")
	}
	if len(binding.Keys) != 1 || binding.Keys[0] != "ctrl+q" {
		t.Errorf("Expected keys [ctrl+q], got %v", binding.Keys)
	}
	if binding.Description != "exit application" {
		t.Errorf("Description not preserved, got %q", binding.Description)
	}
}

func TestKeybindingsConfigService_Save_CreatesParentDirectory(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "nested", "deep", "keybindings.yaml")
	service := NewKeybindingsConfigService(configPath)

	if err := service.Save(DefaultKeybindingsConfig()); err != nil {
		t.Fatalf("Save() failed to create nested dirs: %v", err)
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("File not created at nested path: %v", err)
	}
}

func TestDefaultKeybindingsConfig(t *testing.T) {
	cfg := DefaultKeybindingsConfig()
	if cfg == nil {
		t.Fatal("DefaultKeybindingsConfig returned nil")
	}
	if !cfg.Enabled {
		t.Error("Default config should be enabled")
	}
	if len(cfg.Bindings) == 0 {
		t.Error("Default config should have bindings")
	}
}
