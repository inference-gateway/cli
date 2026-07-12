package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	sdk "github.com/inference-gateway/sdk"

	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	services "github.com/inference-gateway/cli/internal/services"
	components "github.com/inference-gateway/cli/internal/ui/components"
	keybinding "github.com/inference-gateway/cli/internal/ui/keybinding"
)

// newStatusBarTestApp wires the minimal ChatApplication surface used by the
// status-indicator focus flow: a real InputStatusBar with a visible model
// indicator (plus, optionally, theme and jobs indicators) and a fake state
// manager capturing view transitions.
func newStatusBarTestApp(t *testing.T, withJobs, withTheme bool) (*ChatApplication, *services.StateManager) {
	t.Helper()

	modelService := &domainmocks.FakeModelService{}
	modelService.GetCurrentModelReturns("test-model")

	statusBar := components.NewInputStatusBar(nil)
	statusBar.SetModelService(modelService)
	statusBar.SetConfig(config.DefaultConfig())

	if withJobs {
		registry := &domainmocks.FakeBackgroundTaskRegistry{}
		registry.CountRunningJobsReturns(1)
		statusBar.SetBackgroundTaskRegistry(registry)
	}

	if withTheme {
		themeService := &domainmocks.FakeThemeService{}
		themeService.GetCurrentThemeNameReturns("tokyo-night")
		statusBar.SetThemeService(themeService)
	}

	stateManager := services.NewStateManager(false)
	_ = stateManager.TransitionToView(domain.ViewStateChat)

	app := &ChatApplication{
		inputStatusBar: statusBar,
		stateManager:   stateManager,
	}
	return app, stateManager
}

func TestFocusStatusBarEventFocusesRow(t *testing.T) {
	app, _ := newStatusBarTestApp(t, false, false)

	app.handleChatView(domain.FocusStatusBarEvent{})
	if !app.statusBarFocused {
		t.Fatal("FocusStatusBarEvent should focus the row when an indicator is actionable")
	}
	if !app.inputStatusBar.IsFocused() {
		t.Fatal("the status bar component should be focused too")
	}
}

func TestFocusStatusBarEventNoopsWithoutActionableIndicator(t *testing.T) {
	app, _ := newStatusBarTestApp(t, false, false)
	statusBar := app.inputStatusBar.(*components.InputStatusBar)
	cfg := config.DefaultConfig()
	cfg.Chat.StatusBar.Indicators.Model = false
	statusBar.SetConfig(cfg)

	app.handleChatView(domain.FocusStatusBarEvent{})
	if app.statusBarFocused {
		t.Fatal("nothing actionable: focus must stay in the input")
	}
}

// TestDuplicateKeyGuardConsumesMarkedKeysOnce asserts the guard trusts the
// consumed-key mark set by chat-view handlers: a marked key is skipped exactly
// once (even if the handler transitioned the view mid-cycle), and unmarked
// keys flow through to the components.
func TestDuplicateKeyGuardConsumesMarkedKeysOnce(t *testing.T) {
	stateManager := services.NewStateManager(false)
	if err := stateManager.TransitionToView(domain.ViewStateChat); err != nil {
		t.Fatalf("transitioning to chat: %v", err)
	}
	app := &ChatApplication{stateManager: stateManager}
	app.keyBindingManager = keybinding.NewKeyBindingManager(app, nil)

	enter := tea.KeyPressMsg{Code: tea.KeyEnter}
	var cmds []tea.Cmd

	if got := app.handleDuplicateKeyEvents(enter, &cmds); got {
		t.Fatal("unmarked key must not be skipped")
	}

	app.lastHandledKey = enter.String()
	if err := stateManager.TransitionToView(domain.ViewStateThemeSelection); err != nil {
		t.Fatalf("transitioning to theme selection: %v", err)
	}
	if got := app.handleDuplicateKeyEvents(enter, &cmds); !got {
		t.Fatal("marked key must be skipped even after a mid-cycle view transition")
	}
	if got := app.handleDuplicateKeyEvents(enter, &cmds); got {
		t.Fatal("mark must be consumed after one skip")
	}
}

// TestStatusBarEnterOpensModelSelection drives the full chat-view key path:
// enter on the focused row transitions to model selection with the same
// status message the /model shortcut emits.
func TestStatusBarEnterOpensModelSelection(t *testing.T) {
	app, stateManager := newStatusBarTestApp(t, false, false)
	app.handleChatView(domain.FocusStatusBarEvent{})

	cmds := app.handleChatViewKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter})

	if got := stateManager.GetCurrentView(); got != domain.ViewStateModelSelection {
		t.Errorf("transitioned to %v, want model selection", got)
	}
	if app.statusBarFocused || app.inputStatusBar.IsFocused() {
		t.Error("activation must return focus to the input")
	}

	if len(cmds) != 1 {
		t.Fatalf("expected one status command, got %d", len(cmds))
	}
	ev, ok := cmds[0]().(domain.SetStatusEvent)
	if !ok {
		t.Fatalf("expected a SetStatusEvent, got %T", cmds[0]())
	}
	if ev.Message != "Select a model from the dropdown" {
		t.Errorf("unexpected status message %q", ev.Message)
	}
}

func TestStatusBarEnterOpensThemeSelection(t *testing.T) {
	app, stateManager := newStatusBarTestApp(t, false, true)
	app.handleChatView(domain.FocusStatusBarEvent{})

	_ = app.handleChatViewKeyPress(tea.KeyPressMsg{Code: tea.KeyRight})
	cmds := app.handleChatViewKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter})

	if got := stateManager.GetCurrentView(); got != domain.ViewStateThemeSelection {
		t.Errorf("transitioned to %v, want theme selection", got)
	}
	if len(cmds) != 1 {
		t.Fatalf("expected one status command, got %d", len(cmds))
	}
	if _, ok := cmds[0]().(domain.SetStatusEvent); !ok {
		t.Fatalf("expected a SetStatusEvent, got %T", cmds[0]())
	}
}

// toolStatsEstimator is a minimal domain.TokenEstimator so the tools
// indicator renders in tests.
type toolStatsEstimator struct{}

func (toolStatsEstimator) GetToolStats(domain.ToolService, domain.AgentMode) (int, int) {
	return 8017, 25
}

func (toolStatsEstimator) EstimateMessagesTokens([]sdk.Message) int { return 0 }

func (toolStatsEstimator) EffectiveContextTokens(lastInputTokens int, _ []sdk.Message) int {
	return lastInputTokens
}

func TestStatusBarEnterOpensToolsList(t *testing.T) {
	app, stateManager := newStatusBarTestApp(t, false, false)
	statusBar := app.inputStatusBar.(*components.InputStatusBar)
	statusBar.SetToolService(&domainmocks.FakeToolService{})
	statusBar.SetTokenEstimator(toolStatsEstimator{})

	app.handleChatView(domain.FocusStatusBarEvent{})

	_ = app.handleChatViewKeyPress(tea.KeyPressMsg{Code: tea.KeyRight})
	cmds := app.handleChatViewKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter})

	if got := stateManager.GetCurrentView(); got != domain.ViewStateToolsList {
		t.Errorf("transitioned to %v, want the tools list", got)
	}
	if len(cmds) != 1 {
		t.Fatalf("expected one status command, got %d", len(cmds))
	}
	if _, ok := cmds[0]().(domain.SetStatusEvent); !ok {
		t.Fatalf("expected a SetStatusEvent, got %T", cmds[0]())
	}
}

func TestStatusBarEnterOpensA2AAgents(t *testing.T) {
	app, stateManager := newStatusBarTestApp(t, false, false)
	statusBar := app.inputStatusBar.(*components.InputStatusBar)
	barStateManager := services.NewStateManager(false)
	barStateManager.InitializeAgentReadiness(1)
	barStateManager.UpdateAgentStatus("agent", domain.AgentStateReady, "", "", "")
	statusBar.SetStateManager(barStateManager)

	app.handleChatView(domain.FocusStatusBarEvent{})

	_ = app.handleChatViewKeyPress(tea.KeyPressMsg{Code: tea.KeyRight})
	cmds := app.handleChatViewKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter})

	if got := stateManager.GetCurrentView(); got != domain.ViewStateA2AAgents {
		t.Errorf("transitioned to %v, want the A2A agents view", got)
	}
	if len(cmds) != 1 {
		t.Fatalf("expected one status command, got %d", len(cmds))
	}
	if _, ok := cmds[0]().(domain.SetStatusEvent); !ok {
		t.Fatalf("expected a SetStatusEvent, got %T", cmds[0]())
	}
}

func TestStatusBarEnterOpensTaskManagement(t *testing.T) {
	app, stateManager := newStatusBarTestApp(t, true, false)
	app.handleChatView(domain.FocusStatusBarEvent{})

	_ = app.handleChatViewKeyPress(tea.KeyPressMsg{Code: tea.KeyRight})
	cmds := app.handleChatViewKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter})

	if got := stateManager.GetCurrentView(); got != domain.ViewStateA2ATaskManagement {
		t.Errorf("transitioned to %v, want task management", got)
	}

	if len(cmds) != 1 {
		t.Fatalf("expected one status command, got %d", len(cmds))
	}
	ev, ok := cmds[0]().(domain.SetStatusEvent)
	if !ok {
		t.Fatalf("expected a SetStatusEvent, got %T", cmds[0]())
	}
	if ev.Message != "Task management interface" {
		t.Errorf("unexpected status message %q", ev.Message)
	}
}

func TestStatusBarNavigationAndExitKeys(t *testing.T) {
	app, stateManager := newStatusBarTestApp(t, true, false)

	tests := []struct {
		name      string
		key       tea.KeyPressMsg
		handled   bool
		stillOpen bool
	}{
		{"right selects next", tea.KeyPressMsg{Code: tea.KeyRight}, true, true},
		{"left selects previous", tea.KeyPressMsg{Code: tea.KeyLeft}, true, true},
		{"tab selects next", tea.KeyPressMsg{Code: tea.KeyTab}, true, true},
		{"down is consumed", tea.KeyPressMsg{Code: tea.KeyDown}, true, true},
		{"esc returns to input", tea.KeyPressMsg{Code: tea.KeyEscape}, true, false},
		{"up returns to input", tea.KeyPressMsg{Code: tea.KeyUp}, true, false},
		{"typing falls through to the input", tea.KeyPressMsg{Text: "a", Code: 'a'}, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app.handleChatView(domain.FocusStatusBarEvent{})
			if !app.statusBarFocused {
				t.Fatal("precondition failed: row not focused")
			}

			_, handled := app.handleStatusBarKeys(tt.key)
			if handled != tt.handled {
				t.Errorf("handled = %v, want %v", handled, tt.handled)
			}
			if app.statusBarFocused != tt.stillOpen {
				t.Errorf("statusBarFocused = %v, want %v", app.statusBarFocused, tt.stillOpen)
			}
		})
	}

	if got := stateManager.GetCurrentView(); got != domain.ViewStateChat {
		t.Errorf("navigation and exit keys must not transition views, got %v", got)
	}
}
