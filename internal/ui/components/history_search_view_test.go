package components

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/inference-gateway/cli/internal/ui/history"
)

func TestHistorySearchView_Creation(t *testing.T) {
	styleProvider := createMockStyleProvider()

	historyManager := history.NewMemoryOnlyHistoryManager(10)

	view := NewHistorySearchView(historyManager, styleProvider)

	if view == nil {
		t.Fatal("Expected view to be created")
	}

	if view.width != 80 {
		t.Errorf("Expected default width 80, got %d", view.width)
	}

	if view.height != 24 {
		t.Errorf("Expected default height 24, got %d", view.height)
	}
}

func TestHistorySearchView_Init(t *testing.T) {
	styleProvider := createMockStyleProvider()
	historyManager := history.NewMemoryOnlyHistoryManager(10)

	// Add some test history
	_ = historyManager.AddToHistory("echo hello")
	_ = historyManager.AddToHistory("ls -la")
	_ = historyManager.AddToHistory("cd /tmp")

	view := NewHistorySearchView(historyManager, styleProvider)
	_ = view.Init()

	if len(view.allHistory) == 0 {
		t.Error("Expected history to be loaded")
	}

	if len(view.filteredHistory) == 0 {
		t.Error("Expected filtered history to be initialized")
	}
}

func TestHistorySearchView_Search(t *testing.T) {
	styleProvider := createMockStyleProvider()
	historyManager := history.NewMemoryOnlyHistoryManager(10)

	// Add test history
	_ = historyManager.AddToHistory("echo hello")
	_ = historyManager.AddToHistory("ls -la")
	_ = historyManager.AddToHistory("echo world")

	view := NewHistorySearchView(historyManager, styleProvider)
	_ = view.Init()

	// Search for "echo"
	view.searchQuery = "echo"
	view.filterHistory()

	if len(view.filteredHistory) != 2 {
		t.Errorf("Expected 2 matches for 'echo', got %d", len(view.filteredHistory))
	}

	// Search for "ls"
	view.searchQuery = "ls"
	view.filterHistory()

	if len(view.filteredHistory) != 1 {
		t.Errorf("Expected 1 match for 'ls', got %d", len(view.filteredHistory))
	}

	// Search for non-existent term
	view.searchQuery = "nonexistent"
	view.filterHistory()

	if len(view.filteredHistory) != 0 {
		t.Errorf("Expected 0 matches for 'nonexistent', got %d", len(view.filteredHistory))
	}
}

func TestHistorySearchView_Navigation(t *testing.T) {
	styleProvider := createMockStyleProvider()
	historyManager := history.NewMemoryOnlyHistoryManager(10)

	_ = historyManager.AddToHistory("command1")
	_ = historyManager.AddToHistory("command2")
	_ = historyManager.AddToHistory("command3")

	view := NewHistorySearchView(historyManager, styleProvider)
	_ = view.Init()

	// Test navigation down
	initialSelected := view.selected
	view.handleNavigationDown()
	if view.selected != initialSelected+1 {
		t.Errorf("Expected selected to increase, got %d", view.selected)
	}

	// Test navigation up
	view.handleNavigationUp()
	if view.selected != initialSelected {
		t.Errorf("Expected selected to return to initial, got %d", view.selected)
	}

	// Test navigation at boundary (should not go below 0)
	view.selected = 0
	view.handleNavigationUp()
	if view.selected != 0 {
		t.Errorf("Expected selected to stay at 0, got %d", view.selected)
	}
}

func TestHistorySearchView_Selection(t *testing.T) {
	styleProvider := createMockStyleProvider()
	historyManager := history.NewMemoryOnlyHistoryManager(10)

	_ = historyManager.AddToHistory("test command")

	view := NewHistorySearchView(historyManager, styleProvider)
	_ = view.Init()

	// Select the first item
	view.selected = 0
	view.handleSelection()

	if !view.IsDone() {
		t.Error("Expected view to be done after selection")
	}

	if view.IsCancelled() {
		t.Error("Expected view not to be cancelled after valid selection")
	}

	selected := view.GetSelected()
	if selected == "" {
		t.Error("Expected selected entry to be returned")
	}
}

func TestHistorySearchView_Cancel(t *testing.T) {
	styleProvider := createMockStyleProvider()
	historyManager := history.NewMemoryOnlyHistoryManager(10)

	view := NewHistorySearchView(historyManager, styleProvider)
	_ = view.Init()

	// Simulate escape key
	msg := tea.KeyMsg{Type: tea.KeyEsc}
	view.handleKeyInput(msg)

	if !view.IsCancelled() {
		t.Error("Expected view to be cancelled after escape")
	}

	if !view.IsDone() {
		t.Error("Expected view to be done after cancel")
	}
}

func TestHistorySearchView_CharacterInput(t *testing.T) {
	styleProvider := createMockStyleProvider()
	historyManager := history.NewMemoryOnlyHistoryManager(10)

	_ = historyManager.AddToHistory("echo test")

	view := NewHistorySearchView(historyManager, styleProvider)
	_ = view.Init()

	// Type "e"
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}}
	view.handleCharacterInput(msg)

	if view.searchQuery != "e" {
		t.Errorf("Expected search query 'e', got '%s'", view.searchQuery)
	}

	// Type "c"
	msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}}
	view.handleCharacterInput(msg)

	if view.searchQuery != "ec" {
		t.Errorf("Expected search query 'ec', got '%s'", view.searchQuery)
	}
}

func TestHistorySearchView_Backspace(t *testing.T) {
	styleProvider := createMockStyleProvider()
	historyManager := history.NewMemoryOnlyHistoryManager(10)

	view := NewHistorySearchView(historyManager, styleProvider)
	_ = view.Init()

	view.searchQuery = "test"
	view.handleBackspace()

	if view.searchQuery != "tes" {
		t.Errorf("Expected search query 'tes' after backspace, got '%s'", view.searchQuery)
	}

	// Test backspace on empty query
	view.searchQuery = ""
	view.handleBackspace()

	if view.searchQuery != "" {
		t.Errorf("Expected empty search query to remain empty, got '%s'", view.searchQuery)
	}
}
