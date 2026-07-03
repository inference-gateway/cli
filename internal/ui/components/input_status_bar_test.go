package components

import (
	"strings"
	"testing"

	sdk "github.com/inference-gateway/sdk"

	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"
	uimocks "github.com/inference-gateway/cli/tests/mocks/ui"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	ui "github.com/inference-gateway/cli/internal/ui"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

type stubTokenEstimator struct {
	estimate int
}

func (s *stubTokenEstimator) GetToolStats(domain.ToolService, domain.AgentMode) (int, int) {
	return 0, 0
}

func (s *stubTokenEstimator) EstimateMessagesTokens([]sdk.Message) int {
	return s.estimate
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

func TestInputStatusBar_GetContextUsageIndicator(t *testing.T) {
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

func TestInputStatusBar_BuildSessionTokensIndicator_FallsBackToEstimator(t *testing.T) {
	mockRepo := &domainmocks.FakeConversationRepository{}
	mockRepo.GetSessionTokensReturns(domain.SessionTokenStats{TotalInputTokens: 0})
	mockRepo.GetMessagesReturns([]domain.ConversationEntry{
		{Message: sdk.Message{Role: sdk.User}},
	})

	statusBar := &InputStatusBar{
		conversationRepo: mockRepo,
		tokenEstimator:   &stubTokenEstimator{estimate: 6643},
	}

	if got := statusBar.buildSessionTokensIndicator(); got != "T.6643" {
		t.Errorf("expected fallback estimate T.6643, got: %s", got)
	}
}

func TestInputStatusBar_GetContextUsageIndicator_FallsBackToEstimator(t *testing.T) {
	mockRepo := &domainmocks.FakeConversationRepository{}
	mockRepo.GetSessionTokensReturns(domain.SessionTokenStats{TotalInputTokens: 0})
	mockRepo.GetMessagesReturns([]domain.ConversationEntry{
		{Message: sdk.Message{Role: sdk.User}},
	})

	statusBar := &InputStatusBar{
		conversationRepo: mockRepo,
		tokenEstimator:   &stubTokenEstimator{estimate: 6643},
	}

	if got := statusBar.getContextUsageIndicator("deepseek/deepseek-v4-flash"); got != "Context: 0.7%" {
		t.Errorf("expected fallback estimator percentage, got: %s", got)
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

func TestInputStatusBar_BuildModelDisplayText_WithSessionTokens(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agent.MaxTokens = 8000
	cfg.Chat.StatusBar.Indicators.SessionTokens = true

	mockRepo := &domainmocks.FakeConversationRepository{}
	mockRepo.GetSessionTokensReturns(domain.SessionTokenStats{
		TotalInputTokens: 1234,
	})

	themeService := &domainmocks.FakeThemeService{}
	themeService.GetCurrentThemeNameReturns("tokyo-night")

	statusBar := &InputStatusBar{
		config:           cfg,
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
		config: cfg,
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
		config:       cfg,
		themeService: themeService,
	}

	result := statusBar.buildModelDisplayText("test-model")

	if result == "" {
		t.Error("Expected non-empty output when indicators are enabled")
	}
	if !strings.Contains(result, "test-model") {
		t.Error("Expected output to contain model information")
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
	if got := statusBar.SelectedAction(); got != ui.StatusIndicatorActionTaskManagement {
		t.Errorf("after second SelectNext: %v, want task management", got)
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

	statusBar.Blur()
	if got := statusBar.Render(); got != unfocused {
		t.Errorf("blurred render should match the original unfocused render")
	}
}
