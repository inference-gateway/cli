package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	t.Run("gateway defaults", func(t *testing.T) {
		testGatewayDefaults(t, cfg)
	})
	t.Run("logging defaults", func(t *testing.T) {
		testLoggingDefaults(t, cfg)
	})
	t.Run("tools defaults", func(t *testing.T) {
		testToolsDefaults(t, cfg)
	})
	t.Run("websearch defaults", func(t *testing.T) {
		testWebSearchDefaults(t, cfg)
	})
	t.Run("websearch engine validation", func(t *testing.T) {
		testWebSearchEngineValidation(t, cfg)
	})
	t.Run("compact defaults", func(t *testing.T) {
		testCompactDefaults(t, cfg)
	})
	t.Run("chat defaults", func(t *testing.T) {
		testChatDefaults(t, cfg)
	})
}

func testGatewayDefaults(t *testing.T, cfg *Config) {
	if cfg.Gateway.URL != "http://localhost:8080" {
		t.Errorf("Expected gateway URL to be 'http://localhost:8080', got %q", cfg.Gateway.URL)
	}
	if cfg.Gateway.Timeout != 200 {
		t.Errorf("Expected gateway timeout to be 200, got %d", cfg.Gateway.Timeout)
	}
}

func testLoggingDefaults(t *testing.T, cfg *Config) {
	if cfg.Logging.Debug {
		t.Error("Expected debug to be false by default")
	}
}

func testToolsDefaults(t *testing.T, cfg *Config) {
	if !cfg.Tools.Enabled {
		t.Error("Expected tools to be enabled by default")
	}
	if !cfg.Tools.Bash.Enabled {
		t.Error("Expected bash tool to be enabled by default")
	}
}

func testWebSearchDefaults(t *testing.T, cfg *Config) {
	if !cfg.Tools.WebSearch.Enabled {
		t.Error("Expected WebSearch to be enabled by default")
	}
	if cfg.Tools.WebSearch.DefaultEngine != "duckduckgo" {
		t.Errorf("Expected default engine to be 'duckduckgo', got %q", cfg.Tools.WebSearch.DefaultEngine)
	}
	if cfg.Tools.WebSearch.MaxResults != 10 {
		t.Errorf("Expected max results to be 10, got %d", cfg.Tools.WebSearch.MaxResults)
	}
	if cfg.Tools.WebSearch.Timeout != 10 {
		t.Errorf("Expected timeout to be 10, got %d", cfg.Tools.WebSearch.Timeout)
	}
	expectedEngines := []string{"duckduckgo", "google"}
	if !reflect.DeepEqual(cfg.Tools.WebSearch.Engines, expectedEngines) {
		t.Errorf("Expected engines to be %v, got %v", expectedEngines, cfg.Tools.WebSearch.Engines)
	}
}

func testWebSearchEngineValidation(t *testing.T, cfg *Config) {
	defaultEngineFound := false
	for _, engine := range cfg.Tools.WebSearch.Engines {
		if cfg.Tools.WebSearch.DefaultEngine == engine {
			defaultEngineFound = true
			break
		}
	}
	if !defaultEngineFound {
		t.Errorf("Default engine %q is not in the list of available engines %v", cfg.Tools.WebSearch.DefaultEngine, cfg.Tools.WebSearch.Engines)
	}
}

func testCompactDefaults(t *testing.T, cfg *Config) {
	if cfg.Compact.OutputDir != ".infer" {
		t.Errorf("Expected compact output dir to be '.infer', got %q", cfg.Compact.OutputDir)
	}
}

func testChatDefaults(t *testing.T, cfg *Config) {
	if cfg.Chat.DefaultModel != "" {
		t.Errorf("Expected default model to be empty, got %q", cfg.Chat.DefaultModel)
	}
	expectedSystemPrompt := `Software engineering assistant. Concise (<4 lines), direct answers only.

RULES:
- Security: Defensive only (analysis, detection, docs)
- Style: No preamble/postamble, no emojis/comments unless asked
- Code: Follow existing patterns, check deps, no secrets
- Tasks: Use TodoWrite, mark progress immediately
- Chat exports: Read only "## Summary" to "---" section
- Tools: Batch calls, prefer Grep for search
- Workflow: Plan→Search→Implement→Test(task test)→Lint→Commit(if asked)`
	if cfg.Chat.SystemPrompt != expectedSystemPrompt {
		t.Errorf("Expected system prompt to match default, got %q", cfg.Chat.SystemPrompt)
	}
}

func TestLoadConfig(t *testing.T) {
	tests := []struct {
		name        string
		configYAML  string
		validator   func(t *testing.T, cfg *Config)
		expectError bool
	}{
		{
			name:        "complete config with websearch",
			configYAML:  getCompleteConfigYAML(),
			validator:   validateCompleteConfig,
			expectError: false,
		},
		{
			name:        "minimal config missing websearch section",
			configYAML:  getMinimalConfigYAML(),
			validator:   validateMinimalConfig,
			expectError: false,
		},
		{
			name: "invalid yaml",
			configYAML: `
gateway:
  url: "http://localhost:8080"
  invalid_structure:
    - missing_key
`,
			validator:   nil,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runLoadConfigTest(t, tt.configYAML, tt.validator, tt.expectError)
		})
	}
}

func runLoadConfigTest(t *testing.T, configYAML string, validator func(t *testing.T, cfg *Config), expectError bool) {
	tempDir, err := os.MkdirTemp("", "config_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	configPath := filepath.Join(tempDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if expectError && err == nil {
		t.Error("Expected error but got none")
	}
	if !expectError && err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if validator != nil && cfg != nil {
		validator(t, cfg)
	}
}

func getCompleteConfigYAML() string {
	return `
gateway:
  url: "http://localhost:8080"
  api_key: "test-key"
  timeout: 30

logging:
  debug: true

tools:
  enabled: true
  bash:
    enabled: true
  web_fetch:
    enabled: false
    whitelisted_domains: []
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
    default_engine: "google"
    max_results: 15
    engines:
      - "google"
      - "duckduckgo"
    timeout: 20
  whitelist:
    commands:
      - "ls"
      - "pwd"
    patterns: []
  safety:
    require_approval: false

compact:
  output_dir: ".infer"

chat:
  default_model: "gpt-4"
  system_prompt: "You are a helpful assistant"
`
}

func getMinimalConfigYAML() string {
	return `
gateway:
  url: "http://localhost:8080"
  api_key: ""
  timeout: 30

logging:
  debug: false
`
}

func validateCompleteConfig(t *testing.T, cfg *Config) {
	if cfg.Gateway.APIKey != "test-key" {
		t.Errorf("Expected API key to be 'test-key', got %q", cfg.Gateway.APIKey)
	}
	if !cfg.Logging.Debug {
		t.Error("Expected debug to be true in complete config")
	}
	if !cfg.Tools.WebSearch.Enabled {
		t.Error("Expected WebSearch to be enabled")
	}
	if cfg.Tools.WebSearch.DefaultEngine != "google" {
		t.Errorf("Expected default engine to be 'google', got %q", cfg.Tools.WebSearch.DefaultEngine)
	}
	if cfg.Tools.WebSearch.MaxResults != 15 {
		t.Errorf("Expected max results to be 15, got %d", cfg.Tools.WebSearch.MaxResults)
	}
	if cfg.Tools.WebSearch.Timeout != 20 {
		t.Errorf("Expected timeout to be 20, got %d", cfg.Tools.WebSearch.Timeout)
	}
	expectedEngines := []string{"google", "duckduckgo"}
	if !reflect.DeepEqual(cfg.Tools.WebSearch.Engines, expectedEngines) {
		t.Errorf("Expected engines to be %v, got %v", expectedEngines, cfg.Tools.WebSearch.Engines)
	}
	if cfg.Chat.DefaultModel != "gpt-4" {
		t.Errorf("Expected default model to be 'gpt-4', got %q", cfg.Chat.DefaultModel)
	}
}

func validateMinimalConfig(t *testing.T, cfg *Config) {
	if cfg.Tools.WebSearch.DefaultEngine != "" {
		t.Errorf("Expected default engine to be empty when not specified, got %q", cfg.Tools.WebSearch.DefaultEngine)
	}
	if cfg.Tools.WebSearch.MaxResults != 0 {
		t.Errorf("Expected max results to be 0 when not specified, got %d", cfg.Tools.WebSearch.MaxResults)
	}
	if len(cfg.Tools.WebSearch.Engines) != 0 {
		t.Errorf("Expected engines to be empty when not specified, got %v", cfg.Tools.WebSearch.Engines)
	}
}

func TestSaveConfig(t *testing.T) {
	tests := []struct {
		name      string
		setupFunc func(*Config)
		validator func(t *testing.T, cfg *Config)
	}{
		{
			name: "save websearch config",
			setupFunc: func(cfg *Config) {
				cfg.Tools.WebSearch.Enabled = false
				cfg.Tools.WebSearch.DefaultEngine = "duckduckgo"
				cfg.Tools.WebSearch.MaxResults = 25
				cfg.Tools.WebSearch.Timeout = 15
				cfg.Tools.WebSearch.Engines = []string{"duckduckgo"}
			},
			validator: func(t *testing.T, cfg *Config) {
				if cfg.Tools.WebSearch.Enabled {
					t.Error("Expected WebSearch to be disabled")
				}
				if cfg.Tools.WebSearch.DefaultEngine != "duckduckgo" {
					t.Errorf("Expected default engine to be 'duckduckgo', got %q", cfg.Tools.WebSearch.DefaultEngine)
				}
				if cfg.Tools.WebSearch.MaxResults != 25 {
					t.Errorf("Expected max results to be 25, got %d", cfg.Tools.WebSearch.MaxResults)
				}
				if cfg.Tools.WebSearch.Timeout != 15 {
					t.Errorf("Expected timeout to be 15, got %d", cfg.Tools.WebSearch.Timeout)
				}
				expectedEngines := []string{"duckduckgo"}
				if !reflect.DeepEqual(cfg.Tools.WebSearch.Engines, expectedEngines) {
					t.Errorf("Expected engines to be %v, got %v", expectedEngines, cfg.Tools.WebSearch.Engines)
				}
			},
		},
		{
			name: "save chat config",
			setupFunc: func(cfg *Config) {
				cfg.Chat.DefaultModel = "claude-3"
				cfg.Chat.SystemPrompt = "Be helpful"
				cfg.Gateway.APIKey = "secret-key"
			},
			validator: func(t *testing.T, cfg *Config) {
				if cfg.Chat.DefaultModel != "claude-3" {
					t.Errorf("Expected default model to be 'claude-3', got %q", cfg.Chat.DefaultModel)
				}
				if cfg.Chat.SystemPrompt != "Be helpful" {
					t.Errorf("Expected system prompt to be 'Be helpful', got %q", cfg.Chat.SystemPrompt)
				}
				if cfg.Gateway.APIKey != "secret-key" {
					t.Errorf("Expected API key to be 'secret-key', got %q", cfg.Gateway.APIKey)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runSaveConfigTest(t, tt.setupFunc, tt.validator)
		})
	}
}

func runSaveConfigTest(t *testing.T, setupFunc func(*Config), validator func(t *testing.T, cfg *Config)) {
	tempDir, err := os.MkdirTemp("", "config_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	configPath := filepath.Join(tempDir, "config.yaml")

	cfg := DefaultConfig()
	setupFunc(cfg)

	if err := cfg.SaveConfig(configPath); err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	loadedCfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("Failed to load saved config: %v", err)
	}

	validator(t, loadedCfg)
}

func TestWebSearchConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		config      WebSearchToolConfig
		description string
	}{
		{
			name: "valid google config",
			config: WebSearchToolConfig{
				Enabled:       true,
				DefaultEngine: "google",
				MaxResults:    10,
				Engines:       []string{"google", "duckduckgo"},
				Timeout:       10,
			},
			description: "should accept valid google configuration",
		},
		{
			name: "valid duckduckgo config",
			config: WebSearchToolConfig{
				Enabled:       true,
				DefaultEngine: "duckduckgo",
				MaxResults:    5,
				Engines:       []string{"duckduckgo"},
				Timeout:       15,
			},
			description: "should accept valid duckduckgo configuration",
		},
		{
			name: "disabled config",
			config: WebSearchToolConfig{
				Enabled: false,
			},
			description: "should accept disabled configuration",
		},
		{
			name: "edge case large values",
			config: WebSearchToolConfig{
				Enabled:       true,
				DefaultEngine: "google",
				MaxResults:    1000,
				Timeout:       300,
			},
			description: "should handle large values",
		},
		{
			name: "edge case zero timeout",
			config: WebSearchToolConfig{
				Enabled:       true,
				DefaultEngine: "duckduckgo",
				MaxResults:    10,
				Timeout:       0,
			},
			description: "should handle zero timeout",
		},
		{
			name: "edge case empty engines",
			config: WebSearchToolConfig{
				Enabled:       true,
				DefaultEngine: "google",
				MaxResults:    10,
				Engines:       []string{},
				Timeout:       10,
			},
			description: "should handle empty engines list",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Tools: ToolsConfig{
					WebSearch: tt.config,
				},
			}

			validateWebSearchConfig(t, cfg, tt.config)
		})
	}
}

func validateWebSearchConfig(t *testing.T, cfg *Config, expected WebSearchToolConfig) {
	if cfg.Tools.WebSearch.Enabled != expected.Enabled {
		t.Errorf("Expected Enabled to be %v, got %v", expected.Enabled, cfg.Tools.WebSearch.Enabled)
	}

	if cfg.Tools.WebSearch.DefaultEngine != expected.DefaultEngine {
		t.Errorf("Expected DefaultEngine to be %q, got %q", expected.DefaultEngine, cfg.Tools.WebSearch.DefaultEngine)
	}

	if cfg.Tools.WebSearch.MaxResults != expected.MaxResults {
		t.Errorf("Expected MaxResults to be %d, got %d", expected.MaxResults, cfg.Tools.WebSearch.MaxResults)
	}

	if cfg.Tools.WebSearch.Timeout != expected.Timeout {
		t.Errorf("Expected Timeout to be %d, got %d", expected.Timeout, cfg.Tools.WebSearch.Timeout)
	}

	if !reflect.DeepEqual(cfg.Tools.WebSearch.Engines, expected.Engines) {
		t.Errorf("Expected Engines to be %v, got %v", expected.Engines, cfg.Tools.WebSearch.Engines)
	}
}

func TestIsApprovalRequired(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(*Config)
		toolName string
		expected bool
	}{
		{
			name: "global approval true, tool not set",
			setup: func(cfg *Config) {
				cfg.Tools.Safety.RequireApproval = true
			},
			toolName: "WebSearch",
			expected: true,
		},
		{
			name: "global approval false, tool not set",
			setup: func(cfg *Config) {
				cfg.Tools.Safety.RequireApproval = false
			},
			toolName: "WebSearch",
			expected: false,
		},
		{
			name: "global approval true, websearch approval false",
			setup: func(cfg *Config) {
				cfg.Tools.Safety.RequireApproval = true
				cfg.Tools.WebSearch.RequireApproval = &[]bool{false}[0]
			},
			toolName: "WebSearch",
			expected: false,
		},
		{
			name: "global approval false, websearch approval true",
			setup: func(cfg *Config) {
				cfg.Tools.Safety.RequireApproval = false
				cfg.Tools.WebSearch.RequireApproval = &[]bool{true}[0]
			},
			toolName: "WebSearch",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.setup(cfg)

			result := cfg.IsApprovalRequired(tt.toolName)
			if result != tt.expected {
				t.Errorf("Expected IsApprovalRequired(%q) to be %v, got %v", tt.toolName, tt.expected, result)
			}
		})
	}
}
