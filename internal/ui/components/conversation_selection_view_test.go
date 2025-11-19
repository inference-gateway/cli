package components

import (
	"context"
	"testing"

	domain "github.com/inference-gateway/cli/internal/domain"
	shortcuts "github.com/inference-gateway/cli/internal/shortcuts"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

func TestConversationSelectorImpl_Reset(t *testing.T) {
	mockRepo := &mockPersistentConversationRepository{}
	themeService := &mockThemeService{}
	styleProvider := styles.NewProvider(themeService)

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
	mockRepo := &mockPersistentConversationRepository{}
	themeService := &mockThemeService{}
	styleProvider := styles.NewProvider(themeService)

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

// Mock repository for testing
type mockPersistentConversationRepository struct{}

func (m *mockPersistentConversationRepository) ListSavedConversations(ctx context.Context, limit, offset int) ([]shortcuts.ConversationSummary, error) {
	return []shortcuts.ConversationSummary{}, nil
}

func (m *mockPersistentConversationRepository) LoadConversation(ctx context.Context, conversationID string) error {
	return nil
}

func (m *mockPersistentConversationRepository) GetCurrentConversationMetadata() shortcuts.ConversationMetadata {
	return shortcuts.ConversationMetadata{}
}

func (m *mockPersistentConversationRepository) SaveConversation(ctx context.Context) error {
	return nil
}

func (m *mockPersistentConversationRepository) StartNewConversation(title string) error {
	return nil
}

func (m *mockPersistentConversationRepository) GetCurrentConversationID() string {
	return ""
}

func (m *mockPersistentConversationRepository) SetConversationTitle(title string) {
}

func (m *mockPersistentConversationRepository) DeleteSavedConversation(ctx context.Context, conversationID string) error {
	return nil
}

// Mock theme for testing
type mockTheme struct{}

func (t *mockTheme) GetUserColor() string       { return "" }
func (t *mockTheme) GetAssistantColor() string  { return "" }
func (t *mockTheme) GetErrorColor() string      { return "" }
func (t *mockTheme) GetSuccessColor() string    { return "" }
func (t *mockTheme) GetStatusColor() string     { return "" }
func (t *mockTheme) GetAccentColor() string     { return "" }
func (t *mockTheme) GetDimColor() string        { return "" }
func (t *mockTheme) GetBorderColor() string     { return "" }
func (t *mockTheme) GetDiffAddColor() string    { return "" }
func (t *mockTheme) GetDiffRemoveColor() string { return "" }

// Mock theme service for testing
type mockThemeService struct{}

func (s *mockThemeService) GetCurrentTheme() domain.Theme {
	return &mockTheme{}
}

func (s *mockThemeService) SetTheme(theme string) error {
	return nil
}

func (s *mockThemeService) GetCurrentThemeName() string {
	return "test-theme"
}

func (s *mockThemeService) ListThemes() []string {
	return []string{"test-theme"}
}
