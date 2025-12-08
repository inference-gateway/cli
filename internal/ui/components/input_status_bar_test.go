package components

import (
	"strings"
	"testing"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"
)

func TestInputStatusBar_MasterToggle(t *testing.T) {
	tests := []struct {
		name         string
		enabled      bool
		expectOutput bool
	}{
		{
			name:         "enabled shows output",
			enabled:      true,
			expectOutput: true,
		},
		{
			name:         "disabled hides output",
			enabled:      false,
			expectOutput: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modelService := &domainmocks.FakeModelService{}
			modelService.GetCurrentModelReturns("test-model")

			themeService := &domainmocks.FakeThemeService{}
			themeService.GetCurrentThemeNameReturns("tokyo-night")

			stateManager := &domainmocks.FakeStateManager{}
			stateManager.GetAgentModeReturns(domain.AgentModeStandard)

			cfg := config.DefaultConfig()
			cfg.Chat.StatusBar.Enabled = tt.enabled

			statusBar := &InputStatusBar{
				width:         100,
				styleProvider: nil,
				modelService:  modelService,
				themeService:  themeService,
				stateManager:  stateManager,
				configService: cfg,
			}

			output := statusBar.Render()

			if tt.expectOutput && output == "" {
				t.Error("Expected output but got empty string")
			}
			if !tt.expectOutput && output != "" {
				t.Errorf("Expected empty output but got: %s", output)
			}
		})
	}
}

func TestInputStatusBar_ShouldShowIndicator(t *testing.T) {
	tests := []struct {
		name          string
		indicator     string
		configEnabled bool
		expected      bool
	}{
		{
			name:          "model enabled returns true",
			indicator:     "model",
			configEnabled: true,
			expected:      true,
		},
		{
			name:          "model disabled returns false",
			indicator:     "model",
			configEnabled: false,
			expected:      false,
		},
		{
			name:          "theme enabled returns true",
			indicator:     "theme",
			configEnabled: true,
			expected:      true,
		},
		{
			name:          "theme disabled returns false",
			indicator:     "theme",
			configEnabled: false,
			expected:      false,
		},
		{
			name:          "unknown indicator returns true",
			indicator:     "unknown",
			configEnabled: false,
			expected:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DefaultConfig()

			switch tt.indicator {
			case "model":
				cfg.Chat.StatusBar.Indicators.Model = tt.configEnabled
			case "theme":
				cfg.Chat.StatusBar.Indicators.Theme = tt.configEnabled
			case "max_output":
				cfg.Chat.StatusBar.Indicators.MaxOutput = tt.configEnabled
			case "a2a_agents":
				cfg.Chat.StatusBar.Indicators.A2AAgents = tt.configEnabled
			case "tools":
				cfg.Chat.StatusBar.Indicators.Tools = tt.configEnabled
			case "background_shells":
				cfg.Chat.StatusBar.Indicators.BackgroundShells = tt.configEnabled
			case "mcp":
				cfg.Chat.StatusBar.Indicators.MCP = tt.configEnabled
			case "context_usage":
				cfg.Chat.StatusBar.Indicators.ContextUsage = tt.configEnabled
			}

			statusBar := &InputStatusBar{
				configService: cfg,
			}

			result := statusBar.shouldShowIndicator(tt.indicator)

			if result != tt.expected {
				t.Errorf("Expected %v but got %v", tt.expected, result)
			}
		})
	}
}

func TestInputStatusBar_ShouldShowIndicator_NilConfig(t *testing.T) {
	statusBar := &InputStatusBar{
		configService: nil,
	}

	result := statusBar.shouldShowIndicator("model")
	if !result {
		t.Error("Expected true when config is nil, but got false")
	}
}

func TestInputStatusBar_BuildThemeIndicator(t *testing.T) {
	themeService := &domainmocks.FakeThemeService{}
	themeService.GetCurrentThemeNameReturns("tokyo-night")

	statusBar := &InputStatusBar{
		themeService: themeService,
	}

	result := statusBar.buildThemeIndicator()
	expectedText := "tokyo-night"

	if result != expectedText {
		t.Errorf("Expected '%s' but got '%s'", expectedText, result)
	}
}

func TestInputStatusBar_BuildMaxOutputIndicator(t *testing.T) {
	tests := []struct {
		name         string
		maxTokens    int
		expectedText string
		expectEmpty  bool
	}{
		{
			name:         "returns max output when set",
			maxTokens:    8000,
			expectedText: "Max Output: 8000",
			expectEmpty:  false,
		},
		{
			name:         "returns empty when max tokens is zero",
			maxTokens:    0,
			expectedText: "",
			expectEmpty:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.Agent.MaxTokens = tt.maxTokens

			statusBar := &InputStatusBar{
				configService: cfg,
			}

			result := statusBar.buildMaxOutputIndicator()

			if tt.expectEmpty && result != "" {
				t.Errorf("Expected empty string but got: %s", result)
			}
			if !tt.expectEmpty && result != tt.expectedText {
				t.Errorf("Expected '%s' but got '%s'", tt.expectedText, result)
			}
		})
	}
}

func TestInputStatusBar_BuildA2AAgentsIndicator(t *testing.T) {
	tests := []struct {
		name         string
		readiness    *domain.AgentReadinessState
		expectedText string
		expectEmpty  bool
	}{
		{
			name: "returns agent readiness",
			readiness: &domain.AgentReadinessState{
				TotalAgents: 5,
				ReadyAgents: 3,
			},
			expectedText: "Agents: 3/5",
			expectEmpty:  false,
		},
		{
			name:         "returns empty when readiness is nil",
			readiness:    nil,
			expectedText: "",
			expectEmpty:  true,
		},
		{
			name: "returns empty when total agents is zero",
			readiness: &domain.AgentReadinessState{
				TotalAgents: 0,
				ReadyAgents: 0,
			},
			expectedText: "",
			expectEmpty:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stateManager := &domainmocks.FakeStateManager{}
			stateManager.GetAgentReadinessReturns(tt.readiness)

			statusBar := &InputStatusBar{
				stateManager: stateManager,
			}

			result := statusBar.buildA2AAgentsIndicator()

			if tt.expectEmpty && result != "" {
				t.Errorf("Expected empty string but got: %s", result)
			}
			if !tt.expectEmpty && result != tt.expectedText {
				t.Errorf("Expected '%s' but got '%s'", tt.expectedText, result)
			}
		})
	}
}

func TestInputStatusBar_BuildMCPIndicator(t *testing.T) {
	tests := []struct {
		name         string
		mcpStatus    *domain.MCPServerStatus
		serverCount  int
		expectedText string
		expectEmpty  bool
	}{
		{
			name: "returns MCP status with tools",
			mcpStatus: &domain.MCPServerStatus{
				TotalServers:     4,
				ConnectedServers: 3,
				TotalTools:       2500,
			},
			serverCount:  4,
			expectedText: "🔌 3/4 (2500)",
			expectEmpty:  false,
		},
		{
			name: "returns MCP status without tools",
			mcpStatus: &domain.MCPServerStatus{
				TotalServers:     4,
				ConnectedServers: 3,
				TotalTools:       0,
			},
			serverCount:  4,
			expectedText: "🔌 3/4",
			expectEmpty:  false,
		},
		{
			name:         "returns empty when MCP status is nil",
			mcpStatus:    nil,
			serverCount:  0,
			expectedText: "",
			expectEmpty:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			// Add server configs if needed
			if tt.serverCount > 0 {
				cfg.MCP.Servers = make([]config.MCPServerEntry, tt.serverCount)
			}

			statusBar := &InputStatusBar{
				mcpStatus:     tt.mcpStatus,
				configService: cfg,
			}

			result := statusBar.buildMCPIndicator()

			if tt.expectEmpty && result != "" {
				t.Errorf("Expected empty string but got: %s", result)
			}
			if !tt.expectEmpty && result != tt.expectedText {
				t.Errorf("Expected '%s' but got '%s'", tt.expectedText, result)
			}
		})
	}
}

func TestInputStatusBar_BuildSessionTokensIndicator(t *testing.T) {
	tests := []struct {
		name         string
		stats        domain.SessionTokenStats
		expectedText string
		expectEmpty  bool
		nilRepo      bool
	}{
		{
			name: "returns session tokens when present",
			stats: domain.SessionTokenStats{
				TotalInputTokens:  500,
				TotalOutputTokens: 300,
				TotalTokens:       800,
				RequestCount:      5,
			},
			expectedText: "T.800",
			expectEmpty:  false,
			nilRepo:      false,
		},
		{
			name: "returns empty when total tokens is zero",
			stats: domain.SessionTokenStats{
				TotalTokens: 0,
			},
			expectedText: "",
			expectEmpty:  true,
			nilRepo:      false,
		},
		{
			name:         "returns empty when repo is nil",
			expectedText: "",
			expectEmpty:  true,
			nilRepo:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var conversationRepo domain.ConversationRepository
			if !tt.nilRepo {
				mockRepo := &domainmocks.FakeConversationRepository{}
				mockRepo.GetSessionTokensReturns(tt.stats)
				conversationRepo = mockRepo
			}

			statusBar := &InputStatusBar{
				conversationRepo: conversationRepo,
			}

			result := statusBar.buildSessionTokensIndicator()

			if tt.expectEmpty && result != "" {
				t.Errorf("Expected empty string but got: %s", result)
			}
			if !tt.expectEmpty && result != tt.expectedText {
				t.Errorf("Expected '%s' but got '%s'", tt.expectedText, result)
			}
		})
	}
}

func TestInputStatusBar_ShouldShowIndicator_SessionTokens(t *testing.T) {
	tests := []struct {
		name          string
		configEnabled bool
		expected      bool
	}{
		{
			name:          "session_tokens enabled returns true",
			configEnabled: true,
			expected:      true,
		},
		{
			name:          "session_tokens disabled returns false",
			configEnabled: false,
			expected:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.Chat.StatusBar.Indicators.SessionTokens = tt.configEnabled

			statusBar := &InputStatusBar{
				configService: cfg,
			}

			result := statusBar.shouldShowIndicator("session_tokens")

			if result != tt.expected {
				t.Errorf("Expected %v but got %v", tt.expected, result)
			}
		})
	}
}

func TestInputStatusBar_BuildModelDisplayText_WithSessionTokens(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agent.MaxTokens = 8000
	cfg.Chat.StatusBar.Indicators.SessionTokens = true

	mockRepo := &domainmocks.FakeConversationRepository{}
	mockRepo.GetSessionTokensReturns(domain.SessionTokenStats{
		TotalTokens: 1234,
	})

	themeService := &domainmocks.FakeThemeService{}
	themeService.GetCurrentThemeNameReturns("tokyo-night")

	statusBar := &InputStatusBar{
		configService:    cfg,
		themeService:     themeService,
		conversationRepo: mockRepo,
	}

	result := statusBar.buildModelDisplayText("test-model")

	if result == "" {
		t.Error("Expected non-empty output when indicators are enabled")
	}
	if !strings.Contains(result, "T.1234") {
		t.Errorf("Expected output to contain 'T.1234', got: %s", result)
	}
}

func TestInputStatusBar_BuildGitBranchIndicator(t *testing.T) {
	tests := []struct {
		name         string
		branch       string
		branchExists bool
		expectedText string
		expectEmpty  bool
	}{
		{
			name:         "shows branch name when in git repo",
			branch:       "main",
			branchExists: true,
			expectedText: "⎇ main",
			expectEmpty:  false,
		},
		{
			name:         "shows feature branch",
			branch:       "feature/git-indicator",
			branchExists: true,
			expectedText: "⎇ feature/git-indicator",
			expectEmpty:  false,
		},
		{
			name:         "truncates very long branch names",
			branch:       "feature/this-is-a-very-long-branch-name-that-should-be-truncated",
			branchExists: true,
			expectedText: "⎇ feature/this-is-a-very-long-branch-...",
			expectEmpty:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			statusBar := &InputStatusBar{
				gitBranchCache:     tt.branch,
				gitBranchCacheTime: time.Now(),
				gitBranchCacheTTL:  5 * time.Second,
			}

			result := statusBar.buildGitBranchIndicator()

			if tt.expectEmpty && result != "" {
				t.Errorf("Expected empty string but got: %s", result)
			}
			if !tt.expectEmpty && result != tt.expectedText {
				t.Errorf("Expected '%s' but got '%s'", tt.expectedText, result)
			}
		})
	}
}

func TestInputStatusBar_ShouldShowIndicator_GitBranch(t *testing.T) {
	tests := []struct {
		name          string
		configEnabled bool
		expected      bool
	}{
		{
			name:          "git_branch enabled returns true",
			configEnabled: true,
			expected:      true,
		},
		{
			name:          "git_branch disabled returns false",
			configEnabled: false,
			expected:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.Chat.StatusBar.Indicators.GitBranch = tt.configEnabled

			statusBar := &InputStatusBar{
				configService: cfg,
			}

			result := statusBar.shouldShowIndicator("git_branch")

			if result != tt.expected {
				t.Errorf("Expected %v but got %v", tt.expected, result)
			}
		})
	}
}

func TestInputStatusBar_BuildModelDisplayText(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Chat.StatusBar.Indicators.Model = false
	cfg.Chat.StatusBar.Indicators.Theme = false
	cfg.Chat.StatusBar.Indicators.MaxOutput = false
	cfg.Chat.StatusBar.Indicators.A2AAgents = false
	cfg.Chat.StatusBar.Indicators.Tools = false
	cfg.Chat.StatusBar.Indicators.BackgroundShells = false
	cfg.Chat.StatusBar.Indicators.MCP = false
	cfg.Chat.StatusBar.Indicators.ContextUsage = false
	cfg.Chat.StatusBar.Indicators.SessionTokens = false
	cfg.Chat.StatusBar.Indicators.GitBranch = false

	statusBar := &InputStatusBar{
		configService: cfg,
	}

	result := statusBar.buildModelDisplayText("test-model")

	if result != "" {
		t.Errorf("Expected empty string when all indicators disabled, got: %s", result)
	}
}

func TestInputStatusBar_BuildModelDisplayText_AllEnabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agent.MaxTokens = 8000

	themeService := &domainmocks.FakeThemeService{}
	themeService.GetCurrentThemeNameReturns("tokyo-night")

	statusBar := &InputStatusBar{
		configService: cfg,
		themeService:  themeService,
	}

	result := statusBar.buildModelDisplayText("test-model")

	if result == "" {
		t.Error("Expected non-empty output when indicators are enabled")
	}
	if !strings.Contains(result, "test-model") {
		t.Error("Expected output to contain model information")
	}
}
