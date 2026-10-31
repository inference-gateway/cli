package components

import (
	"strings"
	"testing"

	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"
	tuimocks "github.com/inference-gateway/cli/tests/mocks/tui"

	tea "charm.land/bubbletea/v2"

	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	styles "github.com/inference-gateway/cli/internal/presentation/tui/styles"
)

func TestConversationSelectorImpl_Reset(t *testing.T) {
	mockRepo := &convmocks.FakePersistentConversationRepository{}
	fakeTheme := &tuimocks.FakeTheme{}
	fakeTheme.GetDimColorReturns("#888888")

	fakeThemeService := &tuimocks.FakeThemeService{}
	fakeThemeService.GetCurrentThemeReturns(fakeTheme)

	styleProvider := styles.NewProvider(fakeThemeService)

	selector := NewConversationSelector(mockRepo, styleProvider)

	selector.done = true
	selector.cancelled = true
	selector.table.SetCursor(5)
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
	if selector.table.Cursor() > 0 {
		t.Errorf("Expected cursor at the top after reset, got %d", selector.table.Cursor())
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
	mockRepo := &convmocks.FakePersistentConversationRepository{}
	fakeTheme := &tuimocks.FakeTheme{}
	fakeTheme.GetDimColorReturns("#888888")

	fakeThemeService := &tuimocks.FakeThemeService{}
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
	selector.filteredConversations = []convdomain.ConversationSummary{
		{ID: "test", Title: "Test"},
	}

	if !selector.IsSelected() {
		t.Error("Expected selector to be selected after second use")
	}
}

func TestConversationSelectorShowsWhereEachSessionRuns(t *testing.T) {
	tests := []struct {
		name    string
		listed  convdomain.ConversationSummary
		located bool
	}{
		{name: "a daemon chat's listing", listed: convdomain.ConversationSummary{ID: "c1", Workspace: "k8s-dev"}, located: true},
		{name: "an in-process listing", listed: convdomain.ConversationSummary{ID: "c1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeThemeService := &tuimocks.FakeThemeService{}
			fakeThemeService.GetCurrentThemeReturns(&tuimocks.FakeTheme{})
			selector := NewConversationSelector(&convmocks.FakePersistentConversationRepository{}, styles.NewProvider(fakeThemeService))
			selector.SetWidth(200)

			selector.Update(tui.ConversationsLoadedEvent{Conversations: []convdomain.ConversationSummary{tt.listed}})

			view := selector.table.View()
			if located := strings.Contains(view, "Workspace") && strings.Contains(view, "k8s-dev"); located != tt.located {
				t.Errorf("the table names the workspace: %v, want %v\n%s", located, tt.located, view)
			}
		})
	}
}

// TestConversationSelectorConfirmIssuesOneDelete verifies the dialog closes as
// the delete starts, so a repeated Y cannot delete the same row twice, and the
// row leaves the list once the result arrives.
func TestConversationSelectorConfirmIssuesOneDelete(t *testing.T) {
	repo := &convmocks.FakePersistentConversationRepository{}
	selector := NewConversationSelector(repo, nil)
	selector.Update(tui.ConversationsLoadedEvent{Conversations: []convdomain.ConversationSummary{
		{ID: "conv-1", Title: "Conversation 1"},
		{ID: "conv-2", Title: "Conversation 2"},
	}})

	selector.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	_, deleteCmd := selector.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if deleteCmd == nil {
		t.Fatal("expected the confirmation to return the delete command")
	}
	if _, again := selector.Update(tea.KeyPressMsg{Code: 'y', Text: "y"}); again != nil {
		t.Fatal("expected a repeated Y to issue no second delete")
	}

	selector.Update(deleteCmd())

	if got := repo.DeleteSavedConversationCallCount(); got != 1 {
		t.Errorf("expected one delete, got %d", got)
	}
	if len(selector.conversations) != 1 || len(selector.filteredConversations) != 1 || selector.filteredConversations[0].ID != "conv-2" {
		t.Errorf("expected only conv-2 left, got %v and %v", selector.conversations, selector.filteredConversations)
	}
	if selector.deleteError != nil {
		t.Errorf("expected no delete error, got %v", selector.deleteError)
	}
}
