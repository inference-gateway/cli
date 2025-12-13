package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
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
	if cfg.Export.OutputDir != ".infer" {
		t.Errorf("Expected export output dir to be '.infer', got %q", cfg.Export.OutputDir)
	}
}

func testChatDefaults(t *testing.T, cfg *Config) {
	if cfg.Agent.Model != "" {
		t.Errorf("Expected default model to be empty, got %q", cfg.Agent.Model)
	}
	expectedSystemPrompt := `Autonomous software engineering agent. Execute tasks iteratively until completion.

IMPORTANT: You NEVER push to main or master or to the current branch - instead you create a branch and push to a branch.
IMPORTANT: You ALWAYS prefer to search for specific matches in a file rather than reading it all - prefer to use Grep tool over Read tool for efficiency.
IMPORTANT: You ALWAYS prefer to see AGENTS.md before README.md files.
IMPORTANT: When reading project documentation, prefer AGENTS.md if available, otherwise fallback to README.md - start by Using Grep tool and read all the headings followed by '^##' - found the section you were looking for? great - use Read tool. You didn't find anything? continue to see '^###'

RULES:
- Security: Defensive only (analysis, detection, docs)
- Style: no emojis/comments unless asked, use conventional commits
- Code: Follow existing patterns, check deps, no secrets
- Tasks: Use TodoWrite, mark progress immediately
- Chat exports: Read only "## Summary" to "---" section
- Tools: ALWAYS use parallel execution when possible - batch multiple tool calls in a single response to improve efficiency
- Tools: Prefer Grep for search, Read for specific files

PARALLEL TOOL EXECUTION:
- When you need to perform multiple operations, make ALL tool calls in a single response
- Examples: Read multiple files, search multiple patterns, execute multiple commands
- The system supports up to 5 concurrent tool executions by default
- This reduces back-and-forth communication and significantly improves performance

WORKFLOW:
When asked to implement features or fix issues:
1. Plan with TodoWrite
2. Search codebase to understand context
3. Implement solution
4. Run tests with: task test
5. Run lint/format with: task fmt and task lint
6. Commit changes (only if explicitly asked)
7. Create a pull request (only if explicitly asked)

A2A ARTIFACT DOWNLOADS:
When a delegated A2A task completes with artifacts:
1. Wait for the automatic completion notification
2. The completion message will show artifact details including Download URLs
3. Use WebFetch with download=true to automatically save artifacts to disk
   Example: WebFetch(url="http://agent/artifacts/123/file.png", download=true)
4. The file will be saved to <configDir>/tmp with filename extracted from URL
5. Check the tool result for the saved file path

EXAMPLE:
<user>Can you create a pull request with the changes?</user>
<assistant>I will checkout to a new branch</assistant>
<tool>Bash(git checkout -b feat/my-new-feature)</tool>
<assistant>Now I will modify the files</assistant>
<tool>Read|Edit|Grep etc</tool>
<tool>Bash(git add <files>)</tool>
<tool>Bash(git commit -m <message>)</tool>
<assistant>Now I will push the changes</assistant>
<tool>Bash(git push origin <branch>)</tool>
<assistant>Now I'll create a pull request</assistant>
<tool>Github(...)</tool>
`
	if cfg.Agent.SystemPrompt != expectedSystemPrompt {
		t.Errorf("Expected system prompt to match default, got %q", cfg.Agent.SystemPrompt)
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
				cfg.Agent.Model = "anthropic/claude-4"
				cfg.Agent.SystemPrompt = "Be helpful"
				cfg.Gateway.APIKey = "secret-key"
			},
			validator: func(t *testing.T, cfg *Config) {
				if cfg.Agent.Model != "anthropic/claude-4" {
					t.Errorf("Expected default model to be 'anthropic/claude-4', got %q", cfg.Agent.Model)
				}
				if cfg.Agent.SystemPrompt != "Be helpful" {
					t.Errorf("Expected system prompt to be 'Be helpful', got %q", cfg.Agent.SystemPrompt)
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

	// Create a new Viper instance for this test
	v := viper.New()
	v.SetConfigFile(configPath)

	// Set all config values in Viper
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
	v.Set("agent.system_prompt", cfg.Agent.SystemPrompt)

	if err := writeViperConfigForTest(v, 2); err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	// Load the saved config back
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

func TestParseGithubOwnerFromURL(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "HTTPS URL with .git extension",
			url:      "https://github.com/inference-gateway/cli.git",
			expected: "inference-gateway",
		},
		{
			name:     "HTTPS URL without .git extension",
			url:      "https://github.com/inference-gateway/cli",
			expected: "inference-gateway",
		},
		{
			name:     "SSH URL with .git extension",
			url:      "git@github.com:inference-gateway/cli.git",
			expected: "inference-gateway",
		},
		{
			name:     "SSH URL without .git extension",
			url:      "git@github.com:inference-gateway/cli",
			expected: "inference-gateway",
		},
		{
			name:     "HTTP URL (not HTTPS)",
			url:      "http://github.com/test-org/test-repo.git",
			expected: "test-org",
		},
		{
			name:     "URL with trailing whitespace",
			url:      "https://github.com/myorg/myrepo.git  ",
			expected: "myorg",
		},
		{
			name:     "URL with leading whitespace",
			url:      "  git@github.com:myorg/myrepo.git",
			expected: "myorg",
		},
		{
			name:     "Non-GitHub HTTPS URL",
			url:      "https://gitlab.com/myorg/myrepo.git",
			expected: "",
		},
		{
			name:     "Non-GitHub SSH URL",
			url:      "git@gitlab.com:myorg/myrepo.git",
			expected: "",
		},
		{
			name:     "Empty URL",
			url:      "",
			expected: "",
		},
		{
			name:     "Invalid URL format",
			url:      "not-a-url",
			expected: "",
		},
		{
			name:     "GitHub Enterprise URL",
			url:      "https://github.enterprise.com/myorg/myrepo.git",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseGithubOwnerFromURL(tt.url)
			if result != tt.expected {
				t.Errorf("parseGithubOwnerFromURL(%q) = %q, expected %q", tt.url, result, tt.expected)
			}
		})
	}
}

func TestDetectGithubOwner(t *testing.T) {
	// Note: This test will only pass if the test is run in the actual git repository
	// For CI/CD environments, this should detect the owner correctly
	owner := DetectGithubOwner()

	// We can't assert a specific value since the test might run in different contexts
	// But we can verify it returns a string (empty or not)
	if owner != "" {
		t.Logf("Detected GitHub owner: %s", owner)
		// Basic validation: owner should not contain slashes or special characters
		if strings.Contains(owner, "/") {
			t.Errorf("GitHub owner should not contain slashes: %s", owner)
		}
	} else {
		t.Log("No GitHub owner detected (not a git repo or not a GitHub remote)")
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
