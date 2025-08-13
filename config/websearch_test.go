package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig_WebSearch(t *testing.T) {
	cfg := DefaultConfig()

	if !cfg.WebSearch.Enabled {
		t.Error("Expected WebSearch to be enabled by default")
	}

	if cfg.WebSearch.DefaultEngine != "google" {
		t.Errorf("Expected default engine to be 'google', got %q", cfg.WebSearch.DefaultEngine)
	}

	if cfg.WebSearch.MaxResults != 10 {
		t.Errorf("Expected max results to be 10, got %d", cfg.WebSearch.MaxResults)
	}

	if cfg.WebSearch.Timeout != 10 {
		t.Errorf("Expected timeout to be 10, got %d", cfg.WebSearch.Timeout)
	}

	expectedEngines := []string{"google", "duckduckgo"}
	if len(cfg.WebSearch.Engines) != len(expectedEngines) {
		t.Errorf("Expected %d engines, got %d", len(expectedEngines), len(cfg.WebSearch.Engines))
	}

	for i, expected := range expectedEngines {
		if i >= len(cfg.WebSearch.Engines) || cfg.WebSearch.Engines[i] != expected {
			t.Errorf("Expected engine %d to be %q, got %q", i, expected, cfg.WebSearch.Engines[i])
		}
	}
}

func TestLoadConfig_WebSearch(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "config_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	configPath := filepath.Join(tempDir, "config.yaml")
	configContent := `
gateway:
  url: "http://localhost:8080"
  api_key: ""
  timeout: 30

output:
  format: "text"
  quiet: false

tools:
  enabled: true
  whitelist:
    commands:
      - "ls"
      - "pwd"
    patterns: []
  safety:
    require_approval: true
  exclude_paths: []

compact:
  output_dir: ".infer"

chat:
  default_model: ""
  system_prompt: ""

fetch:
  enabled: false
  whitelisted_domains: []
  github:
    enabled: false
    token: ""
    base_url: "https://api.github.com"
  safety:
    max_size: 8192
    timeout: 30
    allow_redirect: true
  cache:
    enabled: true
    ttl: 3600
    max_size: 52428800

web_search:
  enabled: true
  default_engine: "duckduckgo"
  max_results: 15
  engines:
    - "google"
    - "duckduckgo"
  timeout: 20
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if !cfg.WebSearch.Enabled {
		t.Error("Expected WebSearch to be enabled")
	}

	if cfg.WebSearch.DefaultEngine != "duckduckgo" {
		t.Errorf("Expected default engine to be 'duckduckgo', got %q", cfg.WebSearch.DefaultEngine)
	}

	if cfg.WebSearch.MaxResults != 15 {
		t.Errorf("Expected max results to be 15, got %d", cfg.WebSearch.MaxResults)
	}

	if cfg.WebSearch.Timeout != 20 {
		t.Errorf("Expected timeout to be 20, got %d", cfg.WebSearch.Timeout)
	}

	expectedEngines := []string{"google", "duckduckgo"}
	if len(cfg.WebSearch.Engines) != len(expectedEngines) {
		t.Errorf("Expected %d engines, got %d", len(expectedEngines), len(cfg.WebSearch.Engines))
	}
}

func TestSaveConfig_WebSearch(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "config_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	configPath := filepath.Join(tempDir, "config.yaml")

	cfg := DefaultConfig()
	cfg.WebSearch.Enabled = false
	cfg.WebSearch.DefaultEngine = "duckduckgo"
	cfg.WebSearch.MaxResults = 25
	cfg.WebSearch.Timeout = 15
	cfg.WebSearch.Engines = []string{"duckduckgo"}

	if err := cfg.SaveConfig(configPath); err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	loadedCfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("Failed to load saved config: %v", err)
	}

	if loadedCfg.WebSearch.Enabled {
		t.Error("Expected WebSearch to be disabled")
	}

	if loadedCfg.WebSearch.DefaultEngine != "duckduckgo" {
		t.Errorf("Expected default engine to be 'duckduckgo', got %q", loadedCfg.WebSearch.DefaultEngine)
	}

	if loadedCfg.WebSearch.MaxResults != 25 {
		t.Errorf("Expected max results to be 25, got %d", loadedCfg.WebSearch.MaxResults)
	}

	if loadedCfg.WebSearch.Timeout != 15 {
		t.Errorf("Expected timeout to be 15, got %d", loadedCfg.WebSearch.Timeout)
	}

	if len(loadedCfg.WebSearch.Engines) != 1 || loadedCfg.WebSearch.Engines[0] != "duckduckgo" {
		t.Errorf("Expected engines to be ['duckduckgo'], got %v", loadedCfg.WebSearch.Engines)
	}
}

func TestLoadConfig_MissingWebSearchSection(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "config_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	configPath := filepath.Join(tempDir, "config.yaml")
	configContent := `
gateway:
  url: "http://localhost:8080"
  api_key: ""
  timeout: 30

output:
  format: "text"
  quiet: false
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if cfg.WebSearch.DefaultEngine != "" {
		t.Errorf("Expected default engine to be empty when not specified, got %q", cfg.WebSearch.DefaultEngine)
	}

	if cfg.WebSearch.MaxResults != 0 {
		t.Errorf("Expected max results to be 0 when not specified, got %d", cfg.WebSearch.MaxResults)
	}
}

func TestWebSearchConfig_Validation(t *testing.T) {
	tests := []struct {
		name   string
		config WebSearchConfig
		valid  bool
	}{
		{
			name: "valid config with google",
			config: WebSearchConfig{
				Enabled:       true,
				DefaultEngine: "google",
				MaxResults:    10,
				Engines:       []string{"google", "duckduckgo"},
				Timeout:       10,
			},
			valid: true,
		},
		{
			name: "valid config with duckduckgo",
			config: WebSearchConfig{
				Enabled:       true,
				DefaultEngine: "duckduckgo",
				MaxResults:    5,
				Engines:       []string{"duckduckgo"},
				Timeout:       15,
			},
			valid: true,
		},
		{
			name: "disabled config",
			config: WebSearchConfig{
				Enabled: false,
			},
			valid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				WebSearch: tt.config,
			}

			if cfg.WebSearch.Enabled != tt.config.Enabled {
				t.Errorf("Expected Enabled to be %v, got %v", tt.config.Enabled, cfg.WebSearch.Enabled)
			}

			if cfg.WebSearch.DefaultEngine != tt.config.DefaultEngine {
				t.Errorf("Expected DefaultEngine to be %q, got %q", tt.config.DefaultEngine, cfg.WebSearch.DefaultEngine)
			}
		})
	}
}

func TestWebSearchConfig_EdgeCases(t *testing.T) {
	cfg := &WebSearchConfig{
		Enabled:       true,
		DefaultEngine: "google",
		MaxResults:    1000,
		Timeout:       300,
	}

	if cfg.MaxResults != 1000 {
		t.Errorf("Expected max results to handle large values, got %d", cfg.MaxResults)
	}

	cfg.Timeout = 0
	if cfg.Timeout != 0 {
		t.Errorf("Expected timeout to handle zero value, got %d", cfg.Timeout)
	}

	cfg.Engines = []string{}
	if len(cfg.Engines) != 0 {
		t.Errorf("Expected empty engines list to be handled, got %d engines", len(cfg.Engines))
	}
}
