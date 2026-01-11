package tools

import (
	"context"
	"testing"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	mocks "github.com/inference-gateway/cli/tests/mocks/domain"
	sdk "github.com/inference-gateway/sdk"
)

// testConfigService is a minimal mock implementation of domain.ConfigService for testing
type testConfigService struct {
	config *config.Config
}

func newTestConfigService(cfg *config.Config) domain.ConfigService {
	return &testConfigService{config: cfg}
}

func (t *testConfigService) GetConfig() *config.Config {
	return t.config
}

func (t *testConfigService) Reload() (*config.Config, error) {
	return t.config, nil
}

func (t *testConfigService) SetValue(key, value string) error {
	return nil
}

func (t *testConfigService) IsApprovalRequired(toolName string) bool {
	return t.config.IsApprovalRequired(toolName)
}

func (t *testConfigService) IsBashCommandWhitelisted(command string) bool {
	return t.config.IsBashCommandWhitelisted(command)
}

func (t *testConfigService) GetOutputDirectory() string {
	return t.config.GetOutputDirectory()
}

func (t *testConfigService) GetGatewayURL() string {
	return t.config.Gateway.URL
}

func (t *testConfigService) GetAPIKey() string {
	return t.config.Gateway.APIKey
}

func (t *testConfigService) GetTimeout() int {
	return t.config.Gateway.Timeout
}

func (t *testConfigService) GetAgentConfig() *config.AgentConfig {
	return t.config.GetAgentConfig()
}

func (t *testConfigService) GetSandboxDirectories() []string {
	return t.config.GetSandboxDirectories()
}

func (t *testConfigService) GetProtectedPaths() []string {
	return t.config.GetProtectedPaths()
}

func createTestRegistry() *Registry {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Bash: config.BashToolConfig{
				Enabled: true,
				Whitelist: config.ToolWhitelistConfig{
					Commands: []string{"echo", "pwd", "ls"},
					Patterns: []string{"^git status$"},
				},
			},
			WebFetch: config.WebFetchToolConfig{
				Enabled: true,
			},
			WebSearch: config.WebSearchToolConfig{
				Enabled:       true,
				DefaultEngine: "duckduckgo",
				MaxResults:    10,
				Engines:       []string{"google", "duckduckgo"},
			},
		},
	}

	return NewRegistry(newTestConfigService(cfg), nil, nil, nil, nil, nil)
}

func TestRegistry_GetTool_Unknown(t *testing.T) {
	registry := createTestRegistry()

	_, err := registry.GetTool("UnknownTool")
	if err == nil {
		t.Error("Expected error for unknown tool")
	}
}

func TestRegistry_DisabledTools(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Bash: config.BashToolConfig{
				Enabled: true,
				Whitelist: config.ToolWhitelistConfig{
					Commands: []string{"echo", "pwd", "ls"},
					Patterns: []string{"^git status$"},
				},
			},
			WebFetch: config.WebFetchToolConfig{
				Enabled: false,
			},
			WebSearch: config.WebSearchToolConfig{
				Enabled: false,
			},
		},
	}

	registry := NewRegistry(newTestConfigService(cfg), nil, nil, nil, nil, nil)

	tools := registry.ListAvailableTools()

	hasCore := false
	hasFetch := false
	hasWebSearch := false

	for _, tool := range tools {
		switch tool {
		case "Bash", "Read", "Grep":
			hasCore = true
		case "WebFetch":
			hasFetch = true
		case "WebSearch":
			hasWebSearch = true
		}
	}

	if !hasCore {
		t.Error("Expected core tools to be available")
	}

	if hasFetch {
		t.Error("WebFetch tool should not be available when disabled")
	}

	if hasWebSearch {
		t.Error("WebSearch tool should not be available when disabled")
	}
}

func TestRegistry_NewRegistry(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Bash: config.BashToolConfig{
				Enabled: true,
				Whitelist: config.ToolWhitelistConfig{
					Commands: []string{"echo", "pwd", "ls"},
					Patterns: []string{"^git status$"},
				},
			},
			WebFetch: config.WebFetchToolConfig{
				Enabled: false,
			},
			WebSearch: config.WebSearchToolConfig{
				Enabled: false,
			},
		},
	}

	configService := newTestConfigService(cfg)
	registry := NewRegistry(configService, nil, nil, nil, nil, nil)

	if registry == nil {
		t.Fatal("Expected non-nil registry")
	}

	if registry.config.GetConfig() != cfg {
		t.Error("Expected config to be set correctly")
	}

	if registry.tools == nil {
		t.Error("Expected tools map to be initialized")
	}
}

func TestRegistry_GetTool(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Bash: config.BashToolConfig{
				Enabled: true,
				Whitelist: config.ToolWhitelistConfig{
					Commands: []string{"echo", "pwd", "ls"},
					Patterns: []string{"^git status$"},
				},
			},
			WebFetch: config.WebFetchToolConfig{
				Enabled: true,
			},
			WebSearch: config.WebSearchToolConfig{
				Enabled: true,
			},
		},
	}

	registry := NewRegistry(newTestConfigService(cfg), nil, nil, nil, nil, nil)

	tests := []struct {
		name     string
		toolName string
		wantErr  bool
	}{
		{
			name:     "get existing bash tool",
			toolName: "Bash",
			wantErr:  false,
		},
		{
			name:     "get existing read tool",
			toolName: "Read",
			wantErr:  false,
		},
		{
			name:     "get existing grep tool",
			toolName: "Grep",
			wantErr:  false,
		},
		{
			name:     "get existing webfetch tool",
			toolName: "WebFetch",
			wantErr:  false,
		},
		{
			name:     "get existing websearch tool",
			toolName: "WebSearch",
			wantErr:  false,
		},
		{
			name:     "get non-existent tool",
			toolName: "NonExistent",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool, err := registry.GetTool(tt.toolName)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetTool() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tool == nil {
				t.Error("Expected non-nil tool")
			}
		})
	}
}

func TestRegistry_ListAvailableTools(t *testing.T) {
	tests := []struct {
		name             string
		config           *config.Config
		expectedMin      int
		expectedMax      int
		shouldContain    []string
		shouldNotContain []string
	}{
		{
			name: "all tools enabled",
			config: &config.Config{
				Tools: config.ToolsConfig{
					Enabled: true,
					Bash: config.BashToolConfig{
						Enabled: true,
						Whitelist: config.ToolWhitelistConfig{
							Commands: []string{"echo", "pwd", "ls"},
							Patterns: []string{"^git status$"},
						},
					},
					Read: config.ReadToolConfig{
						Enabled: true,
					},
					Grep: config.GrepToolConfig{
						Enabled: true,
					},
					WebFetch: config.WebFetchToolConfig{
						Enabled: true,
					},
					WebSearch: config.WebSearchToolConfig{
						Enabled: true,
					},
				},
			},
			expectedMin:   5,
			expectedMax:   15,
			shouldContain: []string{"Bash", "Read", "Grep", "WebFetch", "WebSearch"},
		},
		{
			name: "only core tools enabled",
			config: &config.Config{
				Tools: config.ToolsConfig{
					Enabled: true,
					Bash: config.BashToolConfig{
						Enabled: true,
						Whitelist: config.ToolWhitelistConfig{
							Commands: []string{"echo", "pwd", "ls"},
							Patterns: []string{"^git status$"},
						},
					},
					Read: config.ReadToolConfig{
						Enabled: true,
					},
					Grep: config.GrepToolConfig{
						Enabled: true,
					},
					WebFetch: config.WebFetchToolConfig{
						Enabled: false,
					},
					WebSearch: config.WebSearchToolConfig{
						Enabled: false,
					},
				},
			},
			expectedMin:      3,
			expectedMax:      15,
			shouldContain:    []string{"Bash", "Read", "Grep"},
			shouldNotContain: []string{"WebFetch", "WebSearch"},
		},
		{
			name: "all tools disabled",
			config: &config.Config{
				Tools: config.ToolsConfig{
					Enabled: false,
					Bash: config.BashToolConfig{
						Enabled: false,
						Whitelist: config.ToolWhitelistConfig{
							Commands: []string{"echo", "pwd", "ls"},
							Patterns: []string{"^git status$"},
						},
					},
					WebFetch: config.WebFetchToolConfig{
						Enabled: false,
					},
					WebSearch: config.WebSearchToolConfig{
						Enabled: false,
					},
				},
			},
			expectedMin:      0,
			expectedMax:      5, // Allow for always-enabled tools
			shouldNotContain: []string{"Bash", "Read", "Grep", "WebFetch", "WebSearch"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewRegistry(newTestConfigService(tt.config), nil, nil, nil, nil, nil)
			tools := registry.ListAvailableTools()

			if len(tools) < tt.expectedMin || len(tools) > tt.expectedMax {
				t.Errorf("Expected %d-%d tools, got %d", tt.expectedMin, tt.expectedMax, len(tools))
			}

			toolSet := make(map[string]bool)
			for _, tool := range tools {
				toolSet[tool] = true
			}

			for _, shouldContain := range tt.shouldContain {
				if !toolSet[shouldContain] {
					t.Errorf("Expected to contain tool '%s'", shouldContain)
				}
			}

			for _, shouldNotContain := range tt.shouldNotContain {
				if toolSet[shouldNotContain] {
					t.Errorf("Expected not to contain tool '%s'", shouldNotContain)
				}
			}
		})
	}
}

func TestRegistry_GetToolDefinitions(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Bash: config.BashToolConfig{
				Enabled: true,
				Whitelist: config.ToolWhitelistConfig{
					Commands: []string{"echo", "pwd", "ls"},
					Patterns: []string{"^git status$"},
				},
			},
			Read: config.ReadToolConfig{
				Enabled: true,
			},
			Grep: config.GrepToolConfig{
				Enabled: true,
			},
			WebFetch: config.WebFetchToolConfig{
				Enabled: true,
			},
			WebSearch: config.WebSearchToolConfig{
				Enabled: true,
			},
		},
	}

	registry := NewRegistry(newTestConfigService(cfg), nil, nil, nil, nil, nil)
	definitions := registry.GetToolDefinitions()

	if len(definitions) < 5 || len(definitions) > 15 {
		t.Errorf("Expected 5-15 tool definitions, got %d", len(definitions))
	}

	definitionNames := make(map[string]bool)
	for _, def := range definitions {
		definitionNames[def.Function.Name] = true

		if def.Function.Name == "" {
			t.Error("Tool definition should have a name")
		}
		if *def.Function.Description == "" {
			t.Error("Tool definition should have a description")
		}
		if def.Function.Parameters == nil {
			t.Error("Tool definition should have parameters")
		}
	}

	expectedTools := []string{"Bash", "Read", "Grep", "WebFetch", "WebSearch"}
	for _, tool := range expectedTools {
		if !definitionNames[tool] {
			t.Errorf("Expected tool definition for '%s'", tool)
		}
	}
}

func TestRegistry_IsToolEnabled(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Bash: config.BashToolConfig{
				Enabled: true,
				Whitelist: config.ToolWhitelistConfig{
					Commands: []string{"echo", "pwd", "ls"},
					Patterns: []string{"^git status$"},
				},
			},
			WebFetch: config.WebFetchToolConfig{
				Enabled: true,
			},
			WebSearch: config.WebSearchToolConfig{
				Enabled: false,
			},
		},
	}

	registry := NewRegistry(newTestConfigService(cfg), nil, nil, nil, nil, nil)

	tests := []struct {
		name     string
		toolName string
		expected bool
	}{
		{
			name:     "bash tool enabled",
			toolName: "Bash",
			expected: true,
		},
		{
			name:     "webfetch tool enabled",
			toolName: "WebFetch",
			expected: true,
		},
		{
			name:     "websearch tool disabled",
			toolName: "WebSearch",
			expected: false,
		},
		{
			name:     "non-existent tool",
			toolName: "NonExistent",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enabled := registry.IsToolEnabled(tt.toolName)
			if enabled != tt.expected {
				t.Errorf("IsToolEnabled(%s) = %v, expected %v", tt.toolName, enabled, tt.expected)
			}
		})
	}
}

func TestRegistry_WithMockedTool(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Bash: config.BashToolConfig{
				Enabled: true,
				Whitelist: config.ToolWhitelistConfig{
					Commands: []string{"echo", "pwd", "ls"},
					Patterns: []string{"^git status$"},
				},
			},
		},
	}

	registry := NewRegistry(newTestConfigService(cfg), nil, nil, nil, nil, nil)

	fakeTool := &mocks.FakeTool{}
	fakeTool.IsEnabledReturns(true)
	mockDesc := "A mocked tool for testing"
	mockParams := sdk.FunctionParameters(map[string]any{})
	fakeTool.DefinitionReturns(sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "MockTool",
			Description: &mockDesc,
			Parameters:  &mockParams,
		},
	})
	fakeTool.ValidateReturns(nil)
	fakeTool.ExecuteReturns(&domain.ToolExecutionResult{
		ToolName: "MockTool",
		Success:  true,
	}, nil)

	registry.tools["MockTool"] = fakeTool

	tool, err := registry.GetTool("MockTool")
	if err != nil {
		t.Fatalf("GetTool() failed: %v", err)
	}

	if tool != fakeTool {
		t.Error("Expected to get the mocked tool")
	}

	if !registry.IsToolEnabled("MockTool") {
		t.Error("Expected mocked tool to be enabled")
	}

	availableTools := registry.ListAvailableTools()
	found := false
	for _, toolName := range availableTools {
		if toolName == "MockTool" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected mocked tool to appear in available tools")
	}

	definitions := registry.GetToolDefinitions()
	foundDefinition := false
	for _, def := range definitions {
		if def.Function.Name == "MockTool" {
			foundDefinition = true
			break
		}
	}
	if !foundDefinition {
		t.Error("Expected mocked tool definition to be included")
	}

	ctx := context.Background()
	args := map[string]any{"test": "value"}

	result, err := tool.Execute(ctx, args)
	if err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}

	if result.ToolName != "MockTool" {
		t.Errorf("Expected tool name 'MockTool', got %s", result.ToolName)
	}

	if !result.Success {
		t.Error("Expected successful execution")
	}

	if fakeTool.ExecuteCallCount() != 1 {
		t.Errorf("Expected Execute to be called once, got %d calls", fakeTool.ExecuteCallCount())
	}

	actualCtx, actualArgs := fakeTool.ExecuteArgsForCall(0)
	if actualCtx != ctx {
		t.Error("Expected context to be passed correctly")
	}

	if actualArgs["test"] != "value" {
		t.Error("Expected arguments to be passed correctly")
	}
}
