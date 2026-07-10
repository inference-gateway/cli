package config_test

import (
	"os"
	"path/filepath"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

func TestLoadKeybindings_NonExistentFile(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "non-existent.yaml")

	cfg, err := config.LoadKeybindings(configPath)
	if err != nil {
		t.Fatalf("LoadKeybindings() should not error for non-existent file, got: %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadKeybindings() returned nil config")
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

func TestLoadKeybindings_ValidYAML(t *testing.T) {
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

	cfg, err := config.LoadKeybindings(configPath)
	if err != nil {
		t.Fatalf("LoadKeybindings() failed: %v", err)
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

func TestSaveKeybindings_RoundTrip(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "keybindings.yaml")

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

	if err := config.SaveKeybindings(configPath, original); err != nil {
		t.Fatalf("SaveKeybindings() failed: %v", err)
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("SaveKeybindings() did not create file: %v", err)
	}

	loaded, err := config.LoadKeybindings(configPath)
	if err != nil {
		t.Fatalf("LoadKeybindings() after save failed: %v", err)
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

func TestSaveKeybindings_CreatesParentDirectory(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "nested", "deep", "keybindings.yaml")

	if err := config.SaveKeybindings(configPath, config.DefaultKeybindingsConfig()); err != nil {
		t.Fatalf("SaveKeybindings() failed to create nested dirs: %v", err)
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("File not created at nested path: %v", err)
	}
}

func TestDefaultKeybindingsConfig(t *testing.T) {
	cfg := config.DefaultKeybindingsConfig()
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

func TestResolveKeybindings(t *testing.T) {
	quitID := config.ActionID(config.NamespaceGlobal, "quit")
	cancelID := config.ActionID(config.NamespaceGlobal, "cancel")
	disabled := false

	tests := []struct {
		name        string
		cfg         config.KeybindingsConfig
		wantKeys    map[string][]string
		wantEnabled map[string]bool
		wantUnknown []string
	}{
		{
			name:     "no overrides yields defaults",
			cfg:      config.KeybindingsConfig{Enabled: true},
			wantKeys: map[string][]string{quitID: {"ctrl+c"}},
		},
		{
			name: "keys override replaces defaults",
			cfg: config.KeybindingsConfig{Enabled: true, Bindings: map[string]config.KeyBindingEntry{
				quitID: {Keys: []string{"ctrl+q"}},
			}},
			wantKeys: map[string][]string{quitID: {"ctrl+q"}, cancelID: {"esc"}},
		},
		{
			name: "enabled=false override disables action",
			cfg: config.KeybindingsConfig{Enabled: true, Bindings: map[string]config.KeyBindingEntry{
				cancelID: {Enabled: &disabled},
			}},
			wantEnabled: map[string]bool{cancelID: false, quitID: true},
		},
		{
			name: "disabled config ignores overrides",
			cfg: config.KeybindingsConfig{Enabled: false, Bindings: map[string]config.KeyBindingEntry{
				quitID: {Keys: []string{"ctrl+q"}},
			}},
			wantKeys: map[string][]string{quitID: {"ctrl+c"}},
		},
		{
			name: "unknown IDs reported",
			cfg: config.KeybindingsConfig{Enabled: true, Bindings: map[string]config.KeyBindingEntry{
				"no_such_action": {Keys: []string{"x"}},
			}},
			wantUnknown: []string{"no_such_action"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved, unknown := config.ResolveKeybindings(tt.cfg)
			if len(resolved) != len(config.GetDefaultKeybindings()) {
				t.Errorf("resolved map has %d entries, want all %d defaults", len(resolved), len(config.GetDefaultKeybindings()))
			}
			for id, keys := range tt.wantKeys {
				got := resolved[id].Keys
				if len(got) != len(keys) {
					t.Fatalf("action %s keys = %v, want %v", id, got, keys)
				}
				for i := range keys {
					if got[i] != keys[i] {
						t.Errorf("action %s keys = %v, want %v", id, got, keys)
					}
				}
			}
			for id, want := range tt.wantEnabled {
				entry := resolved[id]
				got := entry.Enabled == nil || *entry.Enabled
				if got != want {
					t.Errorf("action %s enabled = %v, want %v", id, got, want)
				}
			}
			if len(unknown) != len(tt.wantUnknown) {
				t.Errorf("unknown = %v, want %v", unknown, tt.wantUnknown)
			}
		})
	}
}
