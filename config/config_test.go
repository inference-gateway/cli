package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	viper "github.com/spf13/viper"
	yaml "go.yaml.in/yaml/v3"
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
	t.Run("export defaults", func(t *testing.T) {
		testExportDefaults(t, cfg)
	})
	t.Run("chat defaults", func(t *testing.T) {
		testChatDefaults(t, cfg)
	})
	t.Run("speech_to_text defaults", func(t *testing.T) {
		testSpeechToTextDefaults(t, cfg)
	})
}

func testSpeechToTextDefaults(t *testing.T, cfg *Config) {
	if cfg.SpeechToText.Enabled {
		t.Error("Expected speech_to_text to be disabled by default")
	}
	if cfg.IsSpeechToTextEnabled() {
		t.Error("Expected IsSpeechToTextEnabled to be false by default")
	}
	if cfg.SpeechToText.Engine != "whisper.cpp" {
		t.Errorf("Expected engine 'whisper.cpp', got %q", cfg.SpeechToText.Engine)
	}
	if cfg.SpeechToText.Model != "tiny" {
		t.Errorf("Expected default model 'tiny', got %q", cfg.SpeechToText.Model)
	}
	if !cfg.SpeechToText.AutoDownload {
		t.Error("Expected auto_download to be true by default")
	}
	if cfg.SpeechToText.MaxRecordingSeconds != 30 {
		t.Errorf("Expected max_recording_seconds 30, got %d", cfg.SpeechToText.MaxRecordingSeconds)
	}
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

func testExportDefaults(t *testing.T, cfg *Config) {
	if cfg.Export.OutputDir != ".infer/tmp" {
		t.Errorf("Expected export output dir to be '.infer/tmp', got %q", cfg.Export.OutputDir)
	}
}

func testChatDefaults(t *testing.T, cfg *Config) {
	if cfg.Agent.Model != "" {
		t.Errorf("Expected default model to be empty, got %q", cfg.Agent.Model)
	}
	if cfg.Prompts.Agent.SystemPrompt != "" {
		t.Errorf("Expected DefaultConfig to leave cfg.Prompts empty, got %q", cfg.Prompts.Agent.SystemPrompt)
	}
	if cfg.Prompts.Agent.CustomInstructions != "" {
		t.Errorf("Expected DefaultConfig to leave cfg.Prompts empty, got %q", cfg.Prompts.Agent.CustomInstructions)
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

	v := viper.New()
	v.SetConfigFile(configPath)
	err = v.ReadInConfig()
	if expectError && err == nil {
		t.Error("Expected error but got none")
	}
	if !expectError && err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if validator != nil && err == nil {
		cfg := &Config{}
		if err := v.Unmarshal(cfg); err != nil {
			t.Fatalf("Failed to unmarshal config: %v", err)
		}
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
    timeout: 120
    mode:
      all:
        allow:
          - echo( .*)?
          - ls( .*)?
          - pwd( .*)?
          - tree( .*)?
          - wc( .*)?
          - sort( .*)?
          - uniq( .*)?
          - head( .*)?
          - tail( .*)?
          - task( .*)?
          - make( .*)?
          - find( .*)?
          - git status( .*)?
          - git branch( --show-current)?( -[alrvd])?
          - git log( .*)?
          - git diff( .*)?
          - git remote( -v)?
          - git show( .*)?
          - gh (issue|pr|repo|release|run|workflow) (list|view|status|diff|checks)( .*)?
          - gh auth status( .*)?
          - gh search (issues|code|prs|repos|commits)( .*)?
          - gh project (list|view|item-list|field-list)( .*)?
      plan:
        allow: []
      standard:
        allow:
          - gh issue (create|edit|comment)( .*)?
          - gh pr create( .*)?
          - gh project (item-add|item-edit|item-list|field-list|view|list)( .*)?
      auto:
        allow:
          - .*
    background_shells:
      enabled: true
      max_concurrent: 5
      max_output_buffer_mb: 10
      retention_minutes: 60
  web_fetch:
    enabled: false
    allowed_domains: []
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
  safety:
    require_approval: false

export:
  output_dir: ".infer"

agent:
  model: "openai/gpt-5"
  system_prompt: "You are a helpful assistant"

chat:
  theme: dracula
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
	if cfg.Agent.Model != "openai/gpt-5" {
		t.Errorf("Expected default model to be 'openai/gpt-5', got %q", cfg.Agent.Model)
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
				cfg.Agent.Model = "anthropic/claude-sonnet-4-6"
				cfg.Prompts.Agent.SystemPrompt = "Be helpful"
				cfg.Gateway.APIKey = "secret-key"
			},
			validator: func(t *testing.T, cfg *Config) {
				if cfg.Agent.Model != "anthropic/claude-sonnet-4-6" {
					t.Errorf("Expected default model to be 'anthropic/claude-sonnet-4-6', got %q", cfg.Agent.Model)
				}
				if cfg.Prompts.Agent.SystemPrompt != "" {
					t.Errorf("Expected system prompt to be empty after round-trip (it lives in prompts.yaml), got %q", cfg.Prompts.Agent.SystemPrompt)
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

	v := viper.New()
	v.SetConfigFile(configPath)

	v.Set("gateway.url", cfg.Gateway.URL)
	v.Set("gateway.api_key", cfg.Gateway.APIKey)
	v.Set("gateway.timeout", cfg.Gateway.Timeout)
	v.Set("client.timeout", cfg.Client.Timeout)
	v.Set("logging.debug", cfg.Logging.Debug)
	v.Set("tools.enabled", cfg.Tools.Enabled)
	v.Set("tools.web_search.enabled", cfg.Tools.WebSearch.Enabled)
	v.Set("tools.web_search.default_engine", cfg.Tools.WebSearch.DefaultEngine)
	v.Set("tools.web_search.max_results", cfg.Tools.WebSearch.MaxResults)
	v.Set("tools.web_search.timeout", cfg.Tools.WebSearch.Timeout)
	v.Set("tools.web_search.engines", cfg.Tools.WebSearch.Engines)
	v.Set("agent.model", cfg.Agent.Model)

	if err := writeViperConfigForTest(v, 2); err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	loadV := viper.New()
	loadV.SetConfigFile(configPath)
	if err := loadV.ReadInConfig(); err != nil {
		t.Fatalf("Failed to load saved config: %v", err)
	}

	loadedCfg := DefaultConfig()
	if err := loadV.Unmarshal(loadedCfg); err != nil {
		t.Fatalf("Failed to unmarshal saved config: %v", err)
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

func setTestEnv(t *testing.T, key, value string) func() {
	if value == "" {
		return func() {}
	}
	if err := os.Setenv(key, value); err != nil {
		t.Fatalf("Failed to set %s: %v", key, err)
	}
	return func() {
		if err := os.Unsetenv(key); err != nil {
			t.Errorf("Failed to unset %s: %v", key, err)
		}
	}
}

func createA2AViperConfig() *viper.Viper {
	v := viper.New()
	defaults := DefaultConfig()
	v.SetDefault("a2a", defaults.A2A)
	v.SetEnvPrefix("INFER")
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	return v
}

func configureA2AAgents(v *viper.Viper) {
	a2aAgents := os.Getenv("INFER_A2A_AGENTS")
	if a2aAgents == "" {
		return
	}
	var agents []string
	for _, agent := range strings.FieldsFunc(a2aAgents, func(c rune) bool {
		return c == ',' || c == '\n'
	}) {
		if trimmed := strings.TrimSpace(agent); trimmed != "" {
			agents = append(agents, trimmed)
		}
	}
	v.Set("a2a.agents", agents)
}

func configureA2AEnabled(v *viper.Viper) {
	a2aEnabled := os.Getenv("INFER_A2A_ENABLED")
	if a2aEnabled == "" {
		return
	}
	enabled := a2aEnabled == "true"
	v.Set("a2a.enabled", enabled)
}

func normalizeAgents(agents []string) []string {
	if agents == nil {
		return []string{}
	}
	return agents
}

func TestA2AConfigFromEnv(t *testing.T) {
	tests := []struct {
		name            string
		envEnabled      string
		envAgents       string
		expectedEnabled bool
		expectedAgents  []string
	}{
		{
			name:            "A2A enabled true",
			envEnabled:      "true",
			envAgents:       "",
			expectedEnabled: true,
			expectedAgents:  nil,
		},
		{
			name:            "A2A enabled false",
			envEnabled:      "false",
			envAgents:       "",
			expectedEnabled: false,
			expectedAgents:  nil,
		},
		{
			name:            "A2A enabled with agents",
			envEnabled:      "true",
			envAgents:       "agent1,agent2",
			expectedEnabled: true,
			expectedAgents:  []string{"agent1", "agent2"},
		},
		{
			name:            "A2A not set",
			envEnabled:      "",
			envAgents:       "",
			expectedEnabled: true,
			expectedAgents:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanupEnabled := setTestEnv(t, "INFER_A2A_ENABLED", tt.envEnabled)
			defer cleanupEnabled()

			cleanupAgents := setTestEnv(t, "INFER_A2A_AGENTS", tt.envAgents)
			defer cleanupAgents()

			v := createA2AViperConfig()
			configureA2AAgents(v)
			configureA2AEnabled(v)

			cfg := &Config{}
			if err := v.Unmarshal(cfg); err != nil {
				t.Fatalf("Failed to unmarshal config: %v", err)
			}

			if cfg.A2A.Enabled != tt.expectedEnabled {
				t.Errorf("Expected A2A.Enabled to be %v, got %v", tt.expectedEnabled, cfg.A2A.Enabled)
			}

			actualAgents := normalizeAgents(cfg.A2A.Agents)
			expectedAgents := normalizeAgents(tt.expectedAgents)
			if !reflect.DeepEqual(actualAgents, expectedAgents) {
				t.Errorf("Expected A2A.Agents to be %v, got %v", tt.expectedAgents, cfg.A2A.Agents)
			}

			if cfg.IsA2AToolsEnabled() != tt.expectedEnabled {
				t.Errorf("Expected IsA2AToolsEnabled() to be %v, got %v", tt.expectedEnabled, cfg.IsA2AToolsEnabled())
			}
		})
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

// writeViperConfigForTest is a test helper to write viper config without circular import
func writeViperConfigForTest(v *viper.Viper, indent int) error {
	filename := v.ConfigFileUsed()
	if filename == "" {
		return fmt.Errorf("no config file is currently being used")
	}

	cfg := DefaultConfig()

	if err := v.Unmarshal(cfg); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	var buf bytes.Buffer
	yamlEncoder := yaml.NewEncoder(&buf)
	yamlEncoder.SetIndent(indent)

	if err := yamlEncoder.Encode(cfg); err != nil {
		return fmt.Errorf("failed to marshal config to YAML: %w", err)
	}

	if err := yamlEncoder.Close(); err != nil {
		return fmt.Errorf("failed to close YAML encoder: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if err := os.WriteFile(filename, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

func TestIsBashCommandAllowed_GhDefaults(t *testing.T) {
	cfg := DefaultConfig()

	allowed := []string{
		// read-only gh
		"gh issue list", "gh issue view 5", "gh pr view 5", "gh pr diff",
		"gh pr checks", "gh repo view", "gh run list", "gh release view v1",
		"gh workflow view ci.yml", "gh auth status",
		// read-only gh project (baseline)
		"gh project list --owner o", "gh project view 7", "gh project item-list 7",
		// targeted writes
		"gh issue create --title x --body y", "gh issue edit 5 --add-label foo",
		"gh issue comment 5 --body hi", "gh pr create --title x --body y",
	}
	for _, cmd := range allowed {
		if !cfg.IsBashCommandAllowed(cmd, "standard") {
			t.Errorf("expected %q to be allowed", cmd)
		}
	}

	denied := []string{
		// destructive gh
		"gh pr merge 5", "gh repo delete o/r", "gh release create v1",
		"gh release delete v1", "gh run cancel 5", "gh auth login",
		"gh workflow run ci.yml", "gh issue delete 5", "gh pr close 5",
		// gh project writes are not auto-approved - they require approval
		"gh project item-add 7 --url u", "gh project item-edit 7 --field Status",
		// gh api is no longer a default - even a read-only GET must now fall
		// through to approval unless the user adds it to a mode's allow-list.
		"gh api repos/o/r/issues", "gh api user --paginate",
		"gh api repos/o/r/issues -X POST",
		// env inspection leaks secrets (API keys, tokens) - must fall through to approval
		"env", "printenv", "printenv PATH",
	}
	for _, cmd := range denied {
		if cfg.IsBashCommandAllowed(cmd, "standard") {
			t.Errorf("expected %q NOT to be allowed", cmd)
		}
	}
}

func TestDefaultStatusBarConfig(t *testing.T) {
	cfg := DefaultConfig()

	if !cfg.Chat.StatusBar.Enabled {
		t.Error("Expected status bar to be enabled by default")
	}

	indicators := cfg.Chat.StatusBar.Indicators
	if !indicators.Model {
		t.Error("Expected model indicator to be enabled by default")
	}
	if !indicators.Theme {
		t.Error("Expected theme indicator to be enabled by default")
	}
	if indicators.MaxOutput {
		t.Error("Expected max_output indicator to be disabled by default")
	}
	if !indicators.A2AAgents {
		t.Error("Expected a2a_agents indicator to be enabled by default")
	}
	if !indicators.Tools {
		t.Error("Expected tools indicator to be enabled by default")
	}
	if !indicators.BackgroundShells {
		t.Error("Expected background_shells indicator to be enabled by default")
	}
	if !indicators.MCP {
		t.Error("Expected mcp indicator to be enabled by default")
	}
	if !indicators.ContextUsage {
		t.Error("Expected context_usage indicator to be enabled by default")
	}
}

func TestGetDefaultStatusBarConfig(t *testing.T) {
	cfg := GetDefaultStatusBarConfig()

	if !cfg.Enabled {
		t.Error("Expected status bar to be enabled by default")
	}

	if !cfg.Indicators.Model {
		t.Error("Expected model indicator to be enabled by default")
	}
	if !cfg.Indicators.Theme {
		t.Error("Expected theme indicator to be enabled by default")
	}
	if cfg.Indicators.MaxOutput {
		t.Error("Expected max_output indicator to be disabled by default")
	}
	if !cfg.Indicators.A2AAgents {
		t.Error("Expected a2a_agents indicator to be enabled by default")
	}
	if !cfg.Indicators.Tools {
		t.Error("Expected tools indicator to be enabled by default")
	}
	if !cfg.Indicators.BackgroundShells {
		t.Error("Expected background_shells indicator to be enabled by default")
	}
	if !cfg.Indicators.MCP {
		t.Error("Expected mcp indicator to be enabled by default")
	}
	if !cfg.Indicators.ContextUsage {
		t.Error("Expected context_usage indicator to be enabled by default")
	}
}

func TestValidatePathInSandbox_SkillsCarveOut(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir available")
	}

	userSkill := filepath.Join(home, ConfigDirName, "skills", "demo", "SKILL.md")
	projectSkill, err := filepath.Abs(filepath.Join(ConfigDirName, "skills", "demo", "SKILL.md"))
	if err != nil {
		t.Fatalf("failed to resolve project skill path: %v", err)
	}

	relSkill := filepath.Join(ConfigDirName, "skills", "demo", "SKILL.md")

	t.Run("skills enabled: carve-out grants read access", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Agent.Skills.Enabled = true

		t.Run("user skills dir allowed", func(t *testing.T) {
			if err := cfg.ValidatePathInSandbox(userSkill); err != nil {
				t.Fatalf("expected %s allowed, got %v", userSkill, err)
			}
		})

		t.Run("project skills dir allowed", func(t *testing.T) {
			if err := cfg.ValidatePathInSandbox(projectSkill); err != nil {
				t.Fatalf("expected %s allowed, got %v", projectSkill, err)
			}
		})

		t.Run("relative project skill path allowed", func(t *testing.T) {
			if err := cfg.ValidatePathInSandbox(relSkill); err != nil {
				t.Fatalf("expected relative %s allowed, got %v", relSkill, err)
			}
		})

		t.Run("user config.yaml still denied (protected paths)", func(t *testing.T) {
			denied := filepath.Join(home, ConfigDirName, "config.yaml")
			if err := cfg.ValidatePathInSandbox(denied); err == nil {
				t.Fatalf("expected %s to be denied", denied)
			}
		})

		t.Run("user conversations.db still denied (protected paths)", func(t *testing.T) {
			denied := filepath.Join(home, ConfigDirName, "conversations.db")
			if err := cfg.ValidatePathInSandbox(denied); err == nil {
				t.Fatalf("expected %s to be denied", denied)
			}
		})

		t.Run("lookalike sibling dir not allowed", func(t *testing.T) {
			sibling := filepath.Join(home, ConfigDirName, "skills-evil", "SKILL.md")
			if err := cfg.ValidatePathInSandbox(sibling); err == nil {
				t.Fatalf("expected sibling %s rejected (prefix must be a path boundary)", sibling)
			}
		})

		t.Run("protected paths under skills dir still block", func(t *testing.T) {
			secret := filepath.Join(home, ConfigDirName, "skills", "demo", "creds.env")
			if err := cfg.ValidatePathInSandbox(secret); err == nil {
				t.Fatalf("expected protected file %s to be denied", secret)
			}
		})
	})

	t.Run("skills disabled (default): carve-out is off, skills dir denied", func(t *testing.T) {
		cfg := DefaultConfig()
		if cfg.Agent.Skills.Enabled {
			t.Fatalf("expected skills disabled by default")
		}

		for _, p := range []string{userSkill, projectSkill, relSkill} {
			if err := cfg.ValidatePathInSandbox(p); err == nil {
				t.Fatalf("expected %s denied while skills are disabled", p)
			}
		}
	})
}

// TestValidatePathInSandbox_ConfigDir locks in the directory-wide protection of
// the config dir: sensitive config files are denied wholesale, while the
// operational subdirs (tmp, plans) stay reachable - except for files that match
// a hard protection like *.env.
func TestValidatePathInSandbox_ConfigDir(t *testing.T) {
	cfg := DefaultConfig()

	denied := []string{
		ConfigDirName + "/config.yaml",
		ConfigDirName + "/agents.yaml",
		ConfigDirName + "/conversations.db",
		ConfigDirName + "/shortcuts/git.yaml",
		ConfigDirName + "/tmp/leaked.env",
	}
	for _, p := range denied {
		t.Run("deny "+p, func(t *testing.T) {
			if err := cfg.ValidatePathInSandbox(p); err == nil {
				t.Fatalf("expected %s to be denied", p)
			}
		})
	}

	allowed := []string{
		ConfigDirName + "/tmp/scratch.txt",
		ConfigDirName + "/plans/2026-06-01-do-thing.md",
	}
	for _, p := range allowed {
		t.Run("allow "+p, func(t *testing.T) {
			if err := cfg.ValidatePathInSandbox(p); err != nil {
				t.Fatalf("expected %s allowed, got %v", p, err)
			}
		})
	}
}
