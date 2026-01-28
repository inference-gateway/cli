package components

import (
	"testing"

	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"
	uimocks "github.com/inference-gateway/cli/tests/mocks/ui"

	shortcuts "github.com/inference-gateway/cli/internal/shortcuts"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
	shortcutsmocks "github.com/inference-gateway/cli/tests/mocks/shortcuts"
)

func TestConversationSelectorImpl_Reset(t *testing.T) {
	mockRepo := &shortcutsmocks.FakePersistentConversationRepository{}
	fakeTheme := &uimocks.FakeTheme{}
	fakeTheme.GetDimColorReturns("#888888")

	fakeThemeService := &domainmocks.FakeThemeService{}
	fakeThemeService.GetCurrentThemeReturns(fakeTheme)

	styleProvider := styles.NewProvider(fakeThemeService)

	selector := NewConversationSelector(mockRepo, styleProvider)

	selector.done = true
	selector.cancelled = true
	selector.selected = 5
	selector.searchQuery = "test query"
	selector.searchMode = true
	selector.loading = true
	selector.loadError = nil

	if !selector.done || !selector.cancelled {
		t.Error("Expected selector to be in completed state before reset")
	}

	selector.Reset()

	if selector.done {
		t.Error("Expected done to be false after reset")
	}
	if selector.cancelled {
		t.Error("Expected cancelled to be false after reset")
	}
	if selector.selected != 0 {
		t.Errorf("Expected selected to be 0 after reset, got %d", selector.selected)
	}
	if selector.searchQuery != "" {
		t.Errorf("Expected searchQuery to be empty after reset, got %q", selector.searchQuery)
	}
	if selector.searchMode {
		t.Error("Expected searchMode to be false after reset")
	}
	if !selector.loading {
		t.Error("Expected loading to be true after reset (initial state)")
	}
	if selector.loadError != nil {
		t.Errorf("Expected loadError to be nil after reset, got %v", selector.loadError)
	}
	if len(selector.conversations) != 0 {
		t.Errorf("Expected conversations to be empty after reset, got %d", len(selector.conversations))
	}
	if len(selector.filteredConversations) != 0 {
		t.Errorf("Expected filteredConversations to be empty after reset, got %d", len(selector.filteredConversations))
	}
}

func TestConversationSelectorImpl_ResetAllowsReuse(t *testing.T) {
	mockRepo := &shortcutsmocks.FakePersistentConversationRepository{}
	fakeTheme := &uimocks.FakeTheme{}
	fakeTheme.GetDimColorReturns("#888888")

	fakeThemeService := &domainmocks.FakeThemeService{}
	fakeThemeService.GetCurrentThemeReturns(fakeTheme)

	styleProvider := styles.NewProvider(fakeThemeService)

	selector := NewConversationSelector(mockRepo, styleProvider)

	selector.cancelled = true
	selector.done = true

	if !selector.IsCancelled() {
		t.Error("Expected selector to be cancelled after first use")
	}

	selector.Reset()

	if selector.IsSelected() {
		t.Error("Expected selector not to be selected after reset")
	}
	if selector.IsCancelled() {
		t.Error("Expected selector not to be cancelled after reset")
	}

	selector.done = true
	selector.cancelled = false
	selector.loading = false
	selector.filteredConversations = []shortcuts.ConversationSummary{
		{ID: "test", Title: "Test"},
	}

	if !selector.IsSelected() {
		t.Error("Expected selector to be selected after second use")
	}
}
