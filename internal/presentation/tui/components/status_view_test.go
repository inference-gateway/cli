package components

import (
	"strings"
	"testing"

	tuimocks "github.com/inference-gateway/cli/tests/mocks/tui"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	styles "github.com/inference-gateway/cli/internal/presentation/tui/styles"
)

// statusViewStateStub wraps the real ApplicationState to satisfy the status
// view's statusViewState interface, which adds the tui.ChatSessionState methods
// (IsAgentBusy and an error-returning StartChatSession) that the wrapped
// statemanager.Store normally provides.
type statusViewStateStub struct {
	*tui.ApplicationState
	busy bool
}

func (s statusViewStateStub) IsAgentBusy() bool { return s.busy }

func (s statusViewStateStub) StartChatSession(requestID, model string, eventChan <-chan agentdomain.ChatEvent) error {
	s.ApplicationState.StartChatSession(requestID, model, eventChan)
	return nil
}

// createMockStyleProviderForStatus creates a mock styles provider for testing
func createMockStyleProviderForStatus() *styles.Provider {
	fakeTheme := &tuimocks.FakeTheme{}
	fakeThemeService := &tuimocks.FakeThemeService{}
	fakeThemeService.GetCurrentThemeReturns(fakeTheme)
	return styles.NewProvider(fakeThemeService)
}

func TestNewStatusView(t *testing.T) {
	sv := NewStatusView(createMockStyleProviderForStatus())

	if sv.width != 0 {
		t.Errorf("Expected default width 0, got %d", sv.width)
	}

	if sv.message != "" {
		t.Errorf("Expected empty message, got '%s'", sv.message)
	}

	if sv.IsShowingSpinner() {
		t.Error("Expected showSpinner to be false")
	}

	if sv.IsShowingError() {
		t.Error("Expected showError to be false")
	}
}

func TestStatusView_ShowMethods(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(sv *StatusView)
		wantMsg     string
		wantError   bool
		wantSpinner bool
	}{
		{
			name:        "show status sets message and clears flags",
			setup:       func(sv *StatusView) { sv.ShowStatus("Processing request...") },
			wantMsg:     "Processing request...",
			wantError:   false,
			wantSpinner: false,
		},
		{
			name:        "show error sets message and error flag",
			setup:       func(sv *StatusView) { sv.ShowError("Connection failed") },
			wantMsg:     "Connection failed",
			wantError:   true,
			wantSpinner: false,
		},
		{
			name:        "show spinner sets message and spinner flag",
			setup:       func(sv *StatusView) { sv.ShowSpinner("Loading...") },
			wantMsg:     "Loading...",
			wantError:   false,
			wantSpinner: true,
		},
		{
			name: "clear status resets message and flags",
			setup: func(sv *StatusView) {
				sv.ShowError("Some error")
				sv.ClearStatus()
			},
			wantMsg:     "",
			wantError:   false,
			wantSpinner: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sv := NewStatusView(createMockStyleProviderForStatus())
			tt.setup(sv)

			if sv.message != tt.wantMsg {
				t.Errorf("Expected message %q, got %q", tt.wantMsg, sv.message)
			}
			if sv.IsShowingError() != tt.wantError {
				t.Errorf("Expected IsShowingError=%v, got %v", tt.wantError, sv.IsShowingError())
			}
			if sv.IsShowingSpinner() != tt.wantSpinner {
				t.Errorf("Expected IsShowingSpinner=%v, got %v", tt.wantSpinner, sv.IsShowingSpinner())
			}
		})
	}
}

func TestStatusView_IsShowingError(t *testing.T) {
	sv := NewStatusView(createMockStyleProviderForStatus())

	if sv.IsShowingError() {
		t.Error("Expected IsShowingError to be false initially")
	}

	sv.ShowError("Test error")

	if !sv.IsShowingError() {
		t.Error("Expected IsShowingError to be true after ShowError")
	}

	sv.ShowStatus("Normal status")

	if sv.IsShowingError() {
		t.Error("Expected IsShowingError to be false after ShowStatus")
	}
}

func TestStatusView_IsShowingSpinner(t *testing.T) {
	sv := NewStatusView(createMockStyleProviderForStatus())

	if sv.IsShowingSpinner() {
		t.Error("Expected IsShowingSpinner to be false initially")
	}

	sv.ShowSpinner("Loading...")

	if !sv.IsShowingSpinner() {
		t.Error("Expected IsShowingSpinner to be true after ShowSpinner")
	}

	sv.ShowStatus("Normal status")

	if sv.IsShowingSpinner() {
		t.Error("Expected IsShowingSpinner to be false after ShowStatus")
	}
}

func TestStatusView_SetWidth(t *testing.T) {
	sv := NewStatusView(createMockStyleProviderForStatus())

	sv.SetWidth(120)

	if sv.width != 120 {
		t.Errorf("Expected width 120, got %d", sv.width)
	}
}

func TestStatusView_SetHeight(t *testing.T) {
	sv := NewStatusView(createMockStyleProviderForStatus())

	sv.SetHeight(4)
}

func TestStatusView_Render(t *testing.T) {
	sv := NewStatusView(createMockStyleProviderForStatus())

	output := sv.Render()
	if output != "" {
		t.Error("Expected empty render output with no message")
	}

	sv.ShowStatus("Test status")
	output = sv.Render()

	if !strings.Contains(output, "Test status") {
		t.Error("Expected render output to contain status message")
	}

	sv.ShowError("Test error")
	output = sv.Render()

	if !strings.Contains(output, "Test error") {
		t.Error("Expected render output to contain error message")
	}

	sv.ShowSpinner("Loading...")
	output = sv.Render()

	if !strings.Contains(output, "Loading...") {
		t.Error("Expected render output to contain spinner message")
	}
}

func TestStatusView_StateTransitions(t *testing.T) {
	sv := NewStatusView(createMockStyleProviderForStatus())

	sv.ShowStatus("Normal")
	sv.ShowError("Error occurred")

	if !sv.IsShowingError() {
		t.Error("Expected to be showing error after ShowError")
	}

	if sv.IsShowingSpinner() {
		t.Error("Expected to not be showing spinner after ShowError")
	}

	sv.ShowSpinner("Processing...")

	if sv.IsShowingError() {
		t.Error("Expected to not be showing error after ShowSpinner")
	}

	if !sv.IsShowingSpinner() {
		t.Error("Expected to be showing spinner after ShowSpinner")
	}

	sv.ClearStatus()

	if sv.IsShowingError() || sv.IsShowingSpinner() {
		t.Error("Expected no error or spinner after ClearStatus")
	}
}

func TestStatusView_Render_PausesSpinnerDuringApproval(t *testing.T) {
	sv := NewStatusView(createMockStyleProviderForStatus())
	sm := statusViewStateStub{ApplicationState: tui.NewApplicationState()}
	sv.SetStateManager(sm)
	sv.ShowSpinner("Executing tools")

	sm.SetupApprovalUIState(nil, nil)
	paused := sv.Render()
	if strings.Contains(paused, "(") {
		t.Errorf("expected no elapsed timer while approval pending, got %q", paused)
	}
	if !strings.Contains(paused, "Executing tools") {
		t.Errorf("expected message to remain visible, got %q", paused)
	}

	sm.ClearApprovalUIState()
	resumed := sv.Render()
	if !strings.Contains(resumed, "(") {
		t.Errorf("expected elapsed timer after approval resolved, got %q", resumed)
	}
}

func TestStatusView_BlankStatusKeepsARunsProgress(t *testing.T) {
	blank := tui.SetStatusEvent{Message: "", Spinner: false, StatusType: tui.StatusDefault}
	tests := []struct {
		name        string
		session     bool
		busy        bool
		panel       bool
		event       tui.SetStatusEvent
		wantSpinner bool
		wantMessage string
	}{
		{name: "a panel closing during a run keeps it", session: true, busy: true, event: blank, wantSpinner: true, wantMessage: "Generating response..."},
		{name: "a panel's own status under the panel keeps it", session: true, busy: true, panel: true, event: tui.SetStatusEvent{Message: "Select a model"}, wantSpinner: true, wantMessage: "Generating response..."},
		{name: "a run's progress under a panel still replaces it", session: true, busy: true, panel: true, event: tui.SetStatusEvent{Message: "Thinking...", Spinner: true}, wantSpinner: true, wantMessage: "Thinking..."},
		{name: "a status on the chat still replaces it", session: true, busy: true, event: tui.SetStatusEvent{Message: "Theme changed"}, wantMessage: "Theme changed"},
		{name: "it clears once the run is over", session: true, busy: false, event: blank},
		{name: "a direct tool with no chat session clears it", session: false, busy: true, event: blank},
		{name: "a run's own status still replaces it", session: true, busy: true, event: tui.SetStatusEvent{Message: "Thinking...", Spinner: true}, wantSpinner: true, wantMessage: "Thinking..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := tui.NewApplicationState()
			if tt.session {
				state.StartChatSession("req-1", "m", nil)
			}
			if err := state.TransitionToView(tui.ViewStateChat); err != nil {
				t.Fatalf("TransitionToView: %v", err)
			}
			if tt.panel {
				if err := state.TransitionToView(tui.ViewStateA2ATaskManagement); err != nil {
					t.Fatalf("TransitionToView: %v", err)
				}
			}
			sv := NewStatusView(createMockStyleProviderForStatus())
			sv.SetStateManager(statusViewStateStub{ApplicationState: state, busy: tt.busy})
			sv.ShowSpinner("Generating response...")

			sv.Update(tt.event)

			if sv.IsShowingSpinner() != tt.wantSpinner || sv.message != tt.wantMessage {
				t.Errorf("spinner = %v, message = %q, want %v and %q", sv.IsShowingSpinner(), sv.message, tt.wantSpinner, tt.wantMessage)
			}
		})
	}
}
