package config_test

import (
	"os"
	"path/filepath"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

func TestHeartbeatConstants(t *testing.T) {
	if config.HeartbeatFileName != "heartbeat.yaml" {
		t.Errorf("Expected HeartbeatFileName 'heartbeat.yaml', got %q", config.HeartbeatFileName)
	}
	expectedPath := config.ConfigDirName + "/" + config.HeartbeatFileName
	if config.DefaultHeartbeatPath != expectedPath {
		t.Errorf("Expected DefaultHeartbeatPath %q, got %q", expectedPath, config.DefaultHeartbeatPath)
	}
}

func TestDefaultHeartbeatConfig(t *testing.T) {
	cfg := config.DefaultHeartbeatConfig()
	if cfg == nil {
		t.Fatal("DefaultHeartbeatConfig() returned nil")
	}
	if cfg.Enabled {
		t.Error("Expected Enabled to be false by default — heartbeat is opt-in")
	}
	if cfg.Interval != "1h" {
		t.Errorf("Expected Interval='1h', got %q", cfg.Interval)
	}
	if cfg.InitialDelay != "1m" {
		t.Errorf("Expected InitialDelay='1m', got %q", cfg.InitialDelay)
	}
	if cfg.Model != "" {
		t.Errorf("Expected empty Model (falls back to agent.model), got %q", cfg.Model)
	}
	if cfg.Prompt == "" {
		t.Error("Expected non-empty default Prompt")
	}
}

func TestLoadHeartbeat_NonExistentFile(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "non-existent.yaml")

	cfg, err := config.LoadHeartbeat(path)
	if err != nil {
		t.Fatalf("LoadHeartbeat() should not error for missing file, got: %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadHeartbeat() returned nil")
	}
	defaults := config.DefaultHeartbeatConfig()
	if cfg.Enabled != defaults.Enabled || cfg.Interval != defaults.Interval {
		t.Errorf("Expected defaults, got %+v", cfg)
	}
}

func TestLoadHeartbeat_ValidYAML(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "heartbeat.yaml")

	yamlContent := `---
enabled: true
interval: 30s
initial_delay: 5s
model: openai/gpt-4
prompt: "Custom heartbeat prompt"
`
	if err := os.WriteFile(path, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write yaml: %v", err)
	}

	cfg, err := config.LoadHeartbeat(path)
	if err != nil {
		t.Fatalf("LoadHeartbeat() failed: %v", err)
	}
	if !cfg.Enabled {
		t.Error("Expected Enabled true")
	}
	if cfg.Interval != "30s" {
		t.Errorf("Expected Interval='30s', got %q", cfg.Interval)
	}
	if cfg.InitialDelay != "5s" {
		t.Errorf("Expected InitialDelay='5s', got %q", cfg.InitialDelay)
	}
	if cfg.Model != "openai/gpt-4" {
		t.Errorf("Expected Model='openai/gpt-4', got %q", cfg.Model)
	}
	if cfg.Prompt != "Custom heartbeat prompt" {
		t.Errorf("Expected custom prompt, got %q", cfg.Prompt)
	}
}

func TestLoadHeartbeat_EnvironmentVariableExpansion(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "heartbeat.yaml")

	t.Setenv("TEST_HEARTBEAT_MODEL", "expanded/model")

	yamlContent := `---
enabled: true
interval: "10s"
model: "${TEST_HEARTBEAT_MODEL}"
`
	if err := os.WriteFile(path, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write yaml: %v", err)
	}

	cfg, err := config.LoadHeartbeat(path)
	if err != nil {
		t.Fatalf("LoadHeartbeat() failed: %v", err)
	}
	if cfg.Model != "expanded/model" {
		t.Errorf("Expected expanded model 'expanded/model', got %q", cfg.Model)
	}
}

func TestLoadHeartbeat_InvalidYAML(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "heartbeat.yaml")
	if err := os.WriteFile(path, []byte("not: valid: yaml: ["), 0644); err != nil {
		t.Fatalf("Failed to write yaml: %v", err)
	}

	if _, err := config.LoadHeartbeat(path); err == nil {
		t.Fatal("Expected error from invalid YAML, got nil")
	}
}

func TestSaveHeartbeat_RoundTrip(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "subdir", "heartbeat.yaml")

	cfg := &config.HeartbeatConfig{
		Enabled:      true,
		Interval:     "15m",
		InitialDelay: "30s",
		Model:        "openai/gpt-4",
		Prompt:       "test prompt",
	}

	if err := config.SaveHeartbeat(path, cfg); err != nil {
		t.Fatalf("SaveHeartbeat() failed: %v", err)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("File was not created")
	}

	loaded, err := config.LoadHeartbeat(path)
	if err != nil {
		t.Fatalf("LoadHeartbeat() failed: %v", err)
	}
	if loaded.Enabled != cfg.Enabled || loaded.Interval != cfg.Interval ||
		loaded.InitialDelay != cfg.InitialDelay || loaded.Model != cfg.Model ||
		loaded.Prompt != cfg.Prompt {
		t.Errorf("Round-trip mismatch: got %+v, want %+v", loaded, cfg)
	}
}

func TestSaveHeartbeat_CreatesParentDirectory(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "deeply", "nested", "heartbeat.yaml")
	if err := config.SaveHeartbeat(path, config.DefaultHeartbeatConfig()); err != nil {
		t.Fatalf("SaveHeartbeat() failed: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("File not created at nested path: %v", err)
	}
}
