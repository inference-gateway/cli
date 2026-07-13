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
		t.Error("Expected Enabled to be false by default - heartbeat is opt-in")
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

func TestLoadHeartbeat(t *testing.T) {
	defaults := config.DefaultHeartbeatConfig()

	tests := []struct {
		name    string
		yaml    string
		env     map[string]string
		wantErr bool
		check   func(t *testing.T, cfg *config.HeartbeatConfig)
	}{
		{
			name: "non-existent file returns defaults",
			check: func(t *testing.T, cfg *config.HeartbeatConfig) {
				if cfg.Enabled != defaults.Enabled || cfg.Interval != defaults.Interval {
					t.Errorf("Expected defaults, got %+v", cfg)
				}
			},
		},
		{
			name: "valid yaml",
			yaml: `---
enabled: true
interval: 30s
initial_delay: 5s
model: openai/gpt-4
prompt: "Custom heartbeat prompt"
`,
			check: func(t *testing.T, cfg *config.HeartbeatConfig) {
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
			},
		},
		{
			name: "environment variable expansion",
			env:  map[string]string{"TEST_HEARTBEAT_MODEL": "expanded/model"},
			yaml: `---
enabled: true
interval: "10s"
model: "${TEST_HEARTBEAT_MODEL}"
`,
			check: func(t *testing.T, cfg *config.HeartbeatConfig) {
				if cfg.Model != "expanded/model" {
					t.Errorf("Expected expanded model 'expanded/model', got %q", cfg.Model)
				}
			},
		},
		{
			name:    "invalid yaml returns error",
			yaml:    "not: valid: yaml: [",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "heartbeat.yaml")
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			if tt.yaml != "" {
				if err := os.WriteFile(path, []byte(tt.yaml), 0644); err != nil {
					t.Fatalf("Failed to write yaml: %v", err)
				}
			}

			cfg, err := config.LoadHeartbeat(path)
			if tt.wantErr {
				if err == nil {
					t.Fatal("Expected error from invalid YAML, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadHeartbeat() failed: %v", err)
			}
			if cfg == nil {
				t.Fatal("LoadHeartbeat() returned nil")
			}
			tt.check(t, cfg)
		})
	}
}

func TestSaveHeartbeat(t *testing.T) {
	roundTrip := &config.HeartbeatConfig{
		Enabled:      true,
		Interval:     "15m",
		InitialDelay: "30s",
		Model:        "openai/gpt-4",
		Prompt:       "test prompt",
	}

	tests := []struct {
		name  string
		path  []string
		cfg   *config.HeartbeatConfig
		check func(t *testing.T, path string)
	}{
		{
			name: "round trip preserves fields",
			path: []string{"subdir", "heartbeat.yaml"},
			cfg:  roundTrip,
			check: func(t *testing.T, path string) {
				loaded, err := config.LoadHeartbeat(path)
				if err != nil {
					t.Fatalf("LoadHeartbeat() failed: %v", err)
				}
				if loaded.Enabled != roundTrip.Enabled || loaded.Interval != roundTrip.Interval ||
					loaded.InitialDelay != roundTrip.InitialDelay || loaded.Model != roundTrip.Model ||
					loaded.Prompt != roundTrip.Prompt {
					t.Errorf("Round-trip mismatch: got %+v, want %+v", loaded, roundTrip)
				}
			},
		},
		{
			name: "creates parent directory",
			path: []string{"deeply", "nested", "heartbeat.yaml"},
			cfg:  config.DefaultHeartbeatConfig(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(append([]string{t.TempDir()}, tt.path...)...)
			if err := config.SaveHeartbeat(path, tt.cfg); err != nil {
				t.Fatalf("SaveHeartbeat() failed: %v", err)
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
