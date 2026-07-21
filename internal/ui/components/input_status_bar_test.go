package components

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	sdk "github.com/inference-gateway/sdk"

	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"
	uimocks "github.com/inference-gateway/cli/tests/mocks/ui"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	models "github.com/inference-gateway/cli/internal/models"
	ui "github.com/inference-gateway/cli/internal/ui"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

type stubTokenEstimator struct {
	estimate   int
	toolTokens int
	toolCount  int
}

func (s *stubTokenEstimator) GetToolStats(domain.ToolService, domain.AgentMode) (int, int) {
	return s.toolTokens, s.toolCount
}

func (s *stubTokenEstimator) EstimateMessagesTokens([]sdk.Message) int {
	return s.estimate
}

func (s *stubTokenEstimator) EffectiveContextTokens(lastInputTokens int, _ []sdk.Message) int {
	if s.estimate > lastInputTokens {
		return s.estimate
	}
	return lastInputTokens
}

// readinessStateManager returns a real ApplicationState whose readiness matches
// the given TotalAgents/ReadyAgents (nil r leaves readiness uninitialized).
func readinessStateManager(r *domain.AgentReadinessState) *domain.ApplicationState {
	st := domain.NewApplicationState()
	if r != nil {
		st.InitializeAgentReadiness(r.TotalAgents)
		for i := 0; i < r.ReadyAgents; i++ {
			st.UpdateAgentStatus(fmt.Sprintf("agent-%d", i), domain.AgentStateReady, "", "", "")
		}
	}
	return st
}

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

			stateManager := domain.NewApplicationState()
			stateManager.SetAgentMode(domain.AgentModeStandard)

			cfg := config.DefaultConfig()
			cfg.Chat.StatusBar.Enabled = tt.enabled

			statusBar := &InputStatusBar{
				width:         100,
				styleProvider: nil,
				modelService:  modelService,
				themeService:  themeService,
				stateManager:  stateManager,
				config:        cfg,
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
				config: cfg,
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
		config: nil,
	}

	result := statusBar.shouldShowIndicator("model")
	if !result {
		t.Error("Expected true when config is nil, but got false")
	}
}

func TestInputStatusBar_GetBackgroundJobsInfo(t *testing.T) {
	t.Run("nil registry yields nothing", func(t *testing.T) {
		sb := &InputStatusBar{}
		if got := sb.getBackgroundJobsInfo(); got != "" {
			t.Fatalf("expected empty, got %q", got)
		}
	})

	t.Run("only non-zero kinds are shown", func(t *testing.T) {
		reg := &domainmocks.FakeBackgroundTaskRegistry{}
		reg.CountRunningJobsStub = func(kind domain.JobKind) int {
			switch kind {
			case domain.JobKindA2A:
				return 2
			case domain.JobKindSubagent:
				return 3
			default:
				return 0
			}
		}
		sb := &InputStatusBar{backgroundTaskRegistry: reg}
		got := sb.getBackgroundJobsInfo()
		if !strings.Contains(got, "2 A2A") || !strings.Contains(got, "3 subagents") {
			t.Fatalf("missing counts: %q", got)
		}
		if strings.Contains(got, "shells") {
			t.Fatalf("zero-count kind should be omitted: %q", got)
		}
	})

	t.Run("all zero yields nothing", func(t *testing.T) {
		reg := &domainmocks.FakeBackgroundTaskRegistry{}
		reg.CountRunningJobsReturns(0)
		sb := &InputStatusBar{backgroundTaskRegistry: reg}
		if got := sb.getBackgroundJobsInfo(); got != "" {
			t.Fatalf("expected empty, got %q", got)
		}
	})
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
				config: cfg,
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
			expectedText: "A2A: 3/5",
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
			stateManager := readinessStateManager(tt.readiness)

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
				mcpStatus: tt.mcpStatus,
				config:    cfg,
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
			name: "renders cumulative total input tokens when present",
			stats: domain.SessionTokenStats{
				TotalInputTokens:  20_000,
				TotalOutputTokens: 314,
				TotalTokens:       20_314,
				RequestCount:      3,
				LastInputTokens:   7_250,
			},
			expectedText: "T.20000",
			expectEmpty:  false,
			nilRepo:      false,
		},
		{
			name: "returns empty when total input tokens is zero",
			stats: domain.SessionTokenStats{
				TotalTokens: 800,
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

func TestInputStatusBar_BuildCachedTokensIndicator(t *testing.T) {
	tests := []struct {
		name         string
		stats        domain.SessionTokenStats
		expectedText string
		nilRepo      bool
	}{
		{
			name:         "renders cumulative cached tokens when present",
			stats:        domain.SessionTokenStats{TotalInputTokens: 20_000, TotalCachedTokens: 17_500},
			expectedText: "C.17500",
		},
		{
			name:  "hidden while zero",
			stats: domain.SessionTokenStats{TotalInputTokens: 20_000},
		},
		{
			name:    "hidden when repo is nil",
			nilRepo: true,
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

			if result := statusBar.buildCachedTokensIndicator(); result != tt.expectedText {
				t.Errorf("Expected '%s' but got '%s'", tt.expectedText, result)
			}
		})
	}
}

func TestInputStatusBar_GetContextUsageIndicator(t *testing.T) {
	models.SetGatewayContextWindows(map[string]int{"deepseek/deepseek-v4-flash": 1_000_000})
	defer models.SetGatewayContextWindows(nil)

	tests := []struct {
		name         string
		stats        domain.SessionTokenStats
		model        string
		expectedText string
		expectEmpty  bool
		nilRepo      bool
	}{
		{
			name: "renders percentage at low usage from LastInputTokens",
			stats: domain.SessionTokenStats{
				LastInputTokens: 20_000,
			},
			model:        "deepseek/deepseek-v4-flash",
			expectedText: "Context: 2.0%",
			expectEmpty:  false,
		},
		{
			name: "renders HIGH label between 75 and 90 percent",
			stats: domain.SessionTokenStats{
				LastInputTokens: 800_000,
			},
			model:        "deepseek/deepseek-v4-flash",
			expectedText: "Context: 80% HIGH",
			expectEmpty:  false,
		},
		{
			name: "renders FULL label at or above 90 percent",
			stats: domain.SessionTokenStats{
				LastInputTokens: 950_000,
			},
			model:        "deepseek/deepseek-v4-flash",
			expectedText: "Context: 95% FULL",
			expectEmpty:  false,
		},
		{
			name: "uses last request size, ignores cumulative total",
			stats: domain.SessionTokenStats{
				TotalInputTokens: 3_163_117,
				LastInputTokens:  50_000,
			},
			model:        "deepseek/deepseek-v4-flash",
			expectedText: "Context: 5.0%",
			expectEmpty:  false,
		},
		{
			name: "returns empty when no usage and no estimator",
			stats: domain.SessionTokenStats{
				LastInputTokens: 0,
			},
			model:        "deepseek/deepseek-v4-flash",
			expectedText: "",
			expectEmpty:  true,
		},
		{
			name:         "returns empty when repo is nil",
			model:        "deepseek/deepseek-v4-flash",
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

			result := statusBar.getContextUsageIndicator(tt.model)

			if tt.expectEmpty && result != "" {
				t.Errorf("Expected empty string but got: %s", result)
			}
			if !tt.expectEmpty && result != tt.expectedText {
				t.Errorf("Expected '%s' but got '%s'", tt.expectedText, result)
			}
		})
	}
}

func TestInputStatusBar_FallsBackToEstimator(t *testing.T) {
	models.SetGatewayContextWindows(map[string]int{"deepseek/deepseek-v4-flash": 1_000_000})
	defer models.SetGatewayContextWindows(nil)

	tests := []struct {
		name string
		call func(*InputStatusBar) string
		want string
	}{
		{
			name: "session tokens indicator uses estimate",
			call: func(sb *InputStatusBar) string { return sb.buildSessionTokensIndicator() },
			want: "T.6643",
		},
		{
			name: "context usage indicator uses estimate",
			call: func(sb *InputStatusBar) string { return sb.getContextUsageIndicator("deepseek/deepseek-v4-flash") },
			want: "Context: 0.7%",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := &domainmocks.FakeConversationRepository{}
			mockRepo.GetSessionTokensReturns(domain.SessionTokenStats{TotalInputTokens: 0})
			mockRepo.GetMessagesReturns([]domain.ConversationEntry{
				{Message: sdk.Message{Role: sdk.User}},
			})

			statusBar := &InputStatusBar{
				conversationRepo: mockRepo,
				tokenEstimator:   &stubTokenEstimator{estimate: 6643},
			}

			if got := tt.call(statusBar); got != tt.want {
				t.Errorf("expected fallback estimate %s, got: %s", tt.want, got)
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
				config: cfg,
			}

			result := statusBar.shouldShowIndicator("session_tokens")

			if result != tt.expected {
				t.Errorf("Expected %v but got %v", tt.expected, result)
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
				config: cfg,
			}

			result := statusBar.shouldShowIndicator("git_branch")

			if result != tt.expected {
				t.Errorf("Expected %v but got %v", tt.expected, result)
			}
		})
	}
}

func TestInputStatusBar_BuildModelDisplayText(t *testing.T) {
	tests := []struct {
		name         string
		setup        func(t *testing.T) *InputStatusBar
		wantEmpty    bool
		wantContains string
	}{
		{
			name: "all indicators disabled yields empty",
			setup: func(t *testing.T) *InputStatusBar {
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

				return &InputStatusBar{config: cfg}
			},
			wantEmpty: true,
		},
		{
			name: "all enabled contains model information",
			setup: func(t *testing.T) *InputStatusBar {
				cfg := config.DefaultConfig()
				cfg.Agent.MaxTokens = 8000

				themeService := &domainmocks.FakeThemeService{}
				themeService.GetCurrentThemeNameReturns("tokyo-night")

				return &InputStatusBar{config: cfg, themeService: themeService}
			},
			wantContains: "test-model",
		},
		{
			name: "session tokens indicator included when enabled",
			setup: func(t *testing.T) *InputStatusBar {
				cfg := config.DefaultConfig()
				cfg.Agent.MaxTokens = 8000
				cfg.Chat.StatusBar.Indicators.SessionTokens = true

				mockRepo := &domainmocks.FakeConversationRepository{}
				mockRepo.GetSessionTokensReturns(domain.SessionTokenStats{
					TotalInputTokens: 1234,
				})

				themeService := &domainmocks.FakeThemeService{}
				themeService.GetCurrentThemeNameReturns("tokyo-night")

				return &InputStatusBar{config: cfg, themeService: themeService, conversationRepo: mockRepo}
			},
			wantContains: "T.1234",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			statusBar := tt.setup(t)

			result := statusBar.buildModelDisplayText("test-model")

			if tt.wantEmpty {
				if result != "" {
					t.Errorf("Expected empty string when all indicators disabled, got: %s", result)
				}
				return
			}
			if result == "" {
				t.Error("Expected non-empty output when indicators are enabled")
			}
			if !strings.Contains(result, tt.wantContains) {
				t.Errorf("Expected output to contain '%s', got: %s", tt.wantContains, result)
			}
		})
	}
}

func TestInputStatusBar_NilStyleProviderFallback(t *testing.T) {
	modelService := &domainmocks.FakeModelService{}
	modelService.GetCurrentModelReturns("test-model")

	statusBar := &InputStatusBar{
		width:         100,
		styleProvider: nil,
		modelService:  modelService,
		config:        config.DefaultConfig(),
	}

	lines := statusBar.buildStatusLines()

	if len(lines) != 1 {
		t.Fatalf("expected a single fallback row with a nil provider, got %d: %#v", len(lines), lines)
	}
}

// newSelectableStatusBar builds a status bar with visible model and theme
// indicators and, optionally, a background-jobs indicator (two running jobs
// per kind).
func newSelectableStatusBar(withJobs bool) *InputStatusBar {
	modelService := &domainmocks.FakeModelService{}
	modelService.GetCurrentModelReturns("test-model")

	themeService := &domainmocks.FakeThemeService{}
	themeService.GetCurrentThemeNameReturns("tokyo-night")

	statusBar := &InputStatusBar{
		width:        100,
		modelService: modelService,
		themeService: themeService,
		config:       config.DefaultConfig(),
	}

	if withJobs {
		registry := &domainmocks.FakeBackgroundTaskRegistry{}
		registry.CountRunningJobsReturns(2)
		statusBar.backgroundTaskRegistry = registry
	}

	return statusBar
}

func TestInputStatusBar_FocusRequiresActionableIndicator(t *testing.T) {
	statusBar := &InputStatusBar{width: 100, config: config.DefaultConfig()}
	if statusBar.Focus() {
		t.Error("Focus should fail without a model service (no indicators at all)")
	}

	statusBar = newSelectableStatusBar(false)
	statusBar.config.Chat.StatusBar.Indicators.Model = false
	statusBar.config.Chat.StatusBar.Indicators.Theme = false
	if statusBar.Focus() {
		t.Error("Focus should fail when no visible indicator opens a view")
	}
	if statusBar.IsFocused() {
		t.Error("a failed Focus must not leave the bar focused")
	}

	statusBar = newSelectableStatusBar(false)
	if !statusBar.Focus() {
		t.Fatal("Focus should succeed with the model indicator visible")
	}
	if !statusBar.IsFocused() {
		t.Error("a successful Focus must report focused")
	}
	if got := statusBar.SelectedAction(); got != ui.StatusIndicatorActionModelSelection {
		t.Errorf("initial selection = %v, want model selection", got)
	}

	statusBar.Blur()
	if statusBar.IsFocused() {
		t.Error("Blur must clear focus")
	}
}

func TestInputStatusBar_SelectionCyclesActionableIndicators(t *testing.T) {
	statusBar := newSelectableStatusBar(true)
	statusBar.toolService = &domainmocks.FakeToolService{}
	statusBar.tokenEstimator = &stubTokenEstimator{toolTokens: 8017, toolCount: 25}
	statusBar.stateManager = readinessStateManager(&domain.AgentReadinessState{TotalAgents: 1, ReadyAgents: 1})
	if !statusBar.Focus() {
		t.Fatal("Focus should succeed")
	}

	if got := statusBar.SelectedAction(); got != ui.StatusIndicatorActionModelSelection {
		t.Fatalf("initial selection = %v, want model selection", got)
	}

	statusBar.SelectNext()
	if got := statusBar.SelectedAction(); got != ui.StatusIndicatorActionThemeSelection {
		t.Errorf("after SelectNext: %v, want theme selection", got)
	}

	statusBar.SelectNext()
	if got := statusBar.SelectedAction(); got != ui.StatusIndicatorActionA2AAgents {
		t.Errorf("after second SelectNext: %v, want A2A agents", got)
	}

	statusBar.SelectNext()
	if got := statusBar.SelectedAction(); got != ui.StatusIndicatorActionToolsList {
		t.Errorf("after third SelectNext: %v, want tools list", got)
	}

	statusBar.SelectNext()
	if got := statusBar.SelectedAction(); got != ui.StatusIndicatorActionTaskManagement {
		t.Errorf("after fourth SelectNext: %v, want task management", got)
	}

	statusBar.SelectNext()
	if got := statusBar.SelectedAction(); got != ui.StatusIndicatorActionModelSelection {
		t.Errorf("SelectNext should wrap back to model selection, got %v", got)
	}

	statusBar.SelectPrev()
	if got := statusBar.SelectedAction(); got != ui.StatusIndicatorActionTaskManagement {
		t.Errorf("SelectPrev should wrap to task management, got %v", got)
	}
}

func TestInputStatusBar_SelectionClampsWhenIndicatorsDisappear(t *testing.T) {
	statusBar := newSelectableStatusBar(true)
	if !statusBar.Focus() {
		t.Fatal("Focus should succeed")
	}
	statusBar.SelectNext()
	statusBar.SelectNext()
	if got := statusBar.SelectedAction(); got != ui.StatusIndicatorActionTaskManagement {
		t.Fatalf("precondition failed: expected task management selected, got %v", got)
	}

	registry := statusBar.backgroundTaskRegistry.(*domainmocks.FakeBackgroundTaskRegistry)
	registry.CountRunningJobsReturns(0)

	if got := statusBar.SelectedAction(); got != ui.StatusIndicatorActionThemeSelection {
		t.Errorf("selection should clamp to the last remaining indicator, got %v", got)
	}
}

func TestInputStatusBar_MarkSelectedFlagsSelectedIndicator(t *testing.T) {
	statusBar := newSelectableStatusBar(true)
	if !statusBar.Focus() {
		t.Fatal("Focus should succeed")
	}
	statusBar.SelectNext()

	marked := statusBar.markSelected(statusBar.getAllIndicatorParts())

	var flagged []string
	for _, part := range marked {
		if part.selected {
			flagged = append(flagged, part.text)
		}
	}
	if len(flagged) != 1 {
		t.Fatalf("exactly one part must be flagged, got %d: %v", len(flagged), flagged)
	}
	if flagged[0] != "tokyo-night" {
		t.Errorf("flagged part = %q, want the theme indicator", flagged[0])
	}
}

func TestInputStatusBar_FocusedRenderHighlightsSelection(t *testing.T) {
	fakeTheme := &uimocks.FakeTheme{}
	fakeTheme.GetDimColorReturns("#888888")
	fakeTheme.GetAccentColorReturns("#ff9e64")
	fakeTheme.GetBorderColorReturns("#3b4261")
	themeService := &domainmocks.FakeThemeService{}
	themeService.GetCurrentThemeReturns(fakeTheme)

	statusBar := newSelectableStatusBar(false)
	statusBar.styleProvider = styles.NewProvider(themeService)

	unfocused := statusBar.Render()
	if !strings.Contains(unfocused, "test-model") {
		t.Fatalf("render should contain the model indicator, got %q", unfocused)
	}

	if !statusBar.Focus() {
		t.Fatal("Focus should succeed")
	}
	focused := statusBar.Render()
	if !strings.Contains(focused, "test-model") {
		t.Fatalf("focused render should still contain the model indicator, got %q", focused)
	}
	if focused == unfocused {
		t.Error("focused render should apply the selection highlight and differ from the unfocused render")
	}
	ansi := regexp.MustCompile("\x1b\\[[0-9;]*m")
	focusedWidth := len(ansi.ReplaceAllString(focused, ""))
	unfocusedWidth := len(ansi.ReplaceAllString(unfocused, ""))
	if focusedWidth != unfocusedWidth+2 {
		t.Errorf("the selected pill should add one column of padding per side: focused width %d, unfocused %d", focusedWidth, unfocusedWidth)
	}

	statusBar.Blur()
	if got := statusBar.Render(); got != unfocused {
		t.Errorf("blurred render should match the original unfocused render")
	}
}

func TestInputStatusBar_SelectedPillWidthForcesWrap(t *testing.T) {
	statusBar := &InputStatusBar{}
	parts := []indicatorPart{{text: "aaaa"}, {text: "bbbb"}}

	groups := statusBar.splitPartsIntoLines(parts, 11, 2, 3)
	if len(groups) != 1 {
		t.Fatalf("unselected parts should fit on one line, got %d", len(groups))
	}

	parts[1].selected = true
	groups = statusBar.splitPartsIntoLines(parts, 11, 2, 3)
	if len(groups) != 2 {
		t.Fatalf("the selected pill's padding must count toward the line width, got %d line(s)", len(groups))
	}
}

func agentStartupProvider() *styles.Provider {
	fakeTheme := &uimocks.FakeTheme{}
	fakeTheme.GetDimColorReturns("#888888")
	fakeTheme.GetSuccessColorReturns("#9ece6a")
	fakeTheme.GetErrorColorReturns("#f7768e")
	fakeTheme.GetAccentColorReturns("#ff9e64")
	themeService := &domainmocks.FakeThemeService{}
	themeService.GetCurrentThemeReturns(fakeTheme)
	return styles.NewProvider(themeService)
}

func TestInputStatusBar_A2AIndicatorColor(t *testing.T) {
	provider := agentStartupProvider()

	tests := []struct {
		name  string
		setup func() *domain.ApplicationState
		want  string
	}{
		{
			name:  "no readiness state has no color",
			setup: domain.NewApplicationState,
			want:  "",
		},
		{
			name: "startup in progress has no color",
			setup: func() *domain.ApplicationState {
				st := domain.NewApplicationState()
				st.InitializeAgentReadiness(1)
				st.UpdateAgentStatus("agent-a", domain.AgentStatePullingImage, "", "", "")
				return st
			},
			want: "",
		},
		{
			name: "all ready is green",
			setup: func() *domain.ApplicationState {
				st := domain.NewApplicationState()
				st.InitializeAgentReadiness(1)
				st.UpdateAgentStatus("agent-a", domain.AgentStateReady, "", "", "")
				return st
			},
			want: "#9ece6a",
		},
		{
			name: "any failed is red",
			setup: func() *domain.ApplicationState {
				st := domain.NewApplicationState()
				st.InitializeAgentReadiness(2)
				st.UpdateAgentStatus("agent-a", domain.AgentStateReady, "", "", "")
				st.UpdateAgentStatus("agent-b", domain.AgentStateFailed, "", "", "")
				return st
			},
			want: "#f7768e",
		},
		{
			name: "agent going down turns red",
			setup: func() *domain.ApplicationState {
				st := domain.NewApplicationState()
				st.InitializeAgentReadiness(1)
				st.UpdateAgentStatus("agent-a", domain.AgentStateReady, "", "", "")
				st.UpdateAgentStatus("agent-a", domain.AgentStateFailed, "", "", "")
				return st
			},
			want: "#f7768e",
		},
		{
			name: "recovered agent is green again",
			setup: func() *domain.ApplicationState {
				st := domain.NewApplicationState()
				st.InitializeAgentReadiness(1)
				st.UpdateAgentStatus("agent-a", domain.AgentStateReady, "", "", "")
				st.UpdateAgentStatus("agent-a", domain.AgentStateFailed, "", "", "")
				st.UpdateAgentStatus("agent-a", domain.AgentStateReady, "", "", "")
				return st
			},
			want: "#9ece6a",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			statusBar := &InputStatusBar{stateManager: tt.setup(), styleProvider: provider}
			if got := statusBar.a2aIndicatorColor(); got != tt.want {
				t.Errorf("a2aIndicatorColor() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestInputStatusBar_A2AIndicatorCountsDown pins the ready count to the live
// per-agent states: a flapping agent must count down on failure and never
// exceed the total on recovery (regression for the "A2A: 2/1" drift).
func TestInputStatusBar_A2AIndicatorCountsDown(t *testing.T) {
	st := domain.NewApplicationState()
	st.InitializeAgentReadiness(1)
	statusBar := &InputStatusBar{stateManager: st}

	st.UpdateAgentStatus("agent-a", domain.AgentStateReady, "", "", "")
	if got := statusBar.buildA2AAgentsIndicator(); got != "A2A: 1/1" {
		t.Fatalf("after ready: got %q, want %q", got, "A2A: 1/1")
	}

	st.UpdateAgentStatus("agent-a", domain.AgentStateFailed, "", "", "")
	if got := statusBar.buildA2AAgentsIndicator(); got != "A2A: 0/1" {
		t.Fatalf("after failure: got %q, want %q", got, "A2A: 0/1")
	}

	st.UpdateAgentStatus("agent-a", domain.AgentStateReady, "", "", "")
	if got := statusBar.buildA2AAgentsIndicator(); got != "A2A: 1/1" {
		t.Fatalf("after recovery: got %q, want %q", got, "A2A: 1/1")
	}
}
