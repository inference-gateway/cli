package components

import (
	"strings"
	"testing"

	domain "github.com/inference-gateway/cli/internal/domain"
	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"
	uimocks "github.com/inference-gateway/cli/tests/mocks/ui"

	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

// createMockStyleProviderForStatus creates a mock styles provider for testing
func createMockStyleProviderForStatus() *styles.Provider {
	fakeTheme := &uimocks.FakeTheme{}
	fakeThemeService := &domainmocks.FakeThemeService{}
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

func TestStatusView_ShowStatus(t *testing.T) {
	sv := NewStatusView(createMockStyleProviderForStatus())

	testMessage := "Processing request..."
	sv.ShowStatus(testMessage)

	if sv.message != testMessage {
		t.Errorf("Expected message '%s', got '%s'", testMessage, sv.message)
	}

	if sv.IsShowingSpinner() {
		t.Error("Expected showSpinner to be false for status message")
	}

	if sv.IsShowingError() {
		t.Error("Expected showError to be false for status message")
	}
}

func TestStatusView_ShowError(t *testing.T) {
	sv := NewStatusView(createMockStyleProviderForStatus())

	testError := "Connection failed"
	sv.ShowError(testError)

	if sv.message != testError {
		t.Errorf("Expected error message '%s', got '%s'", testError, sv.message)
	}

	if !sv.IsShowingError() {
		t.Error("Expected showError to be true")
	}

	if sv.IsShowingSpinner() {
		t.Error("Expected showSpinner to be false for error message")
	}
}

func TestStatusView_ShowSpinner(t *testing.T) {
	sv := NewStatusView(createMockStyleProviderForStatus())

	testMessage := "Loading..."
	sv.ShowSpinner(testMessage)

	if sv.message != testMessage {
		t.Errorf("Expected spinner message '%s', got '%s'", testMessage, sv.message)
	}

	if !sv.IsShowingSpinner() {
		t.Error("Expected showSpinner to be true")
	}

	if sv.IsShowingError() {
		t.Error("Expected showError to be false for spinner message")
	}
}

func TestStatusView_ClearStatus(t *testing.T) {
	sv := NewStatusView(createMockStyleProviderForStatus())

	sv.ShowError("Some error")

	sv.ClearStatus()

	if sv.message != "" {
		t.Errorf("Expected empty message after clear, got '%s'", sv.message)
	}

	if sv.IsShowingSpinner() {
		t.Error("Expected showSpinner to be false after clear")
	}

	if sv.IsShowingError() {
		t.Error("Expected showError to be false after clear")
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
	sm := &domainmocks.FakeStateManager{}
	sv.SetStateManager(sm)
	sv.ShowSpinner("Executing tools")

	sm.GetApprovalUIStateReturns(&domain.ApprovalUIState{})
	paused := sv.Render()
	if strings.Contains(paused, "(") {
		t.Errorf("expected no elapsed timer while approval pending, got %q", paused)
	}
	if !strings.Contains(paused, "Executing tools") {
		t.Errorf("expected message to remain visible, got %q", paused)
	}

	sm.GetApprovalUIStateReturns(nil)
	resumed := sv.Render()
	if !strings.Contains(resumed, "(") {
		t.Errorf("expected elapsed timer after approval resolved, got %q", resumed)
	}
}
