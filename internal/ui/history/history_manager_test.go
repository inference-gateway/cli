package history

import (
	"os"
	"path/filepath"
	"testing"
)

// MockShellHistoryProvider for testing
type MockShellHistoryProvider struct {
	commands    []string
	historyFile string
	saveError   error
	loadError   error
}

func (m *MockShellHistoryProvider) LoadHistory() ([]string, error) {
	if m.loadError != nil {
		return nil, m.loadError
	}
	return m.commands, nil
}

func (m *MockShellHistoryProvider) SaveToHistory(command string) error {
	if m.saveError != nil {
		return m.saveError
	}
	m.commands = append(m.commands, command)
	return nil
}

func (m *MockShellHistoryProvider) GetHistoryFile() string {
	return m.historyFile
}

func TestHistoryManager_AddToHistory(t *testing.T) {
	mock := &MockShellHistoryProvider{
		commands:    []string{"existing command"},
		historyFile: "/tmp/test_history",
	}

	hm := &HistoryManager{
		shellHistory:    mock,
		inMemoryHistory: make([]string, 0, 5),
		maxInMemory:     5,
		historyIndex:    -1,
	}

	// Load initial history
	err := hm.loadCombinedHistory()
	if err != nil {
		t.Fatalf("Failed to load initial history: %v", err)
	}

	// Add a new command
	err = hm.AddToHistory("new command")
	if err != nil {
		t.Fatalf("AddToHistory failed: %v", err)
	}

	// Check that command was added to shell history
	expectedShellCommands := []string{"existing command", "new command"}
	if len(mock.commands) != len(expectedShellCommands) {
		t.Errorf("Expected %d shell commands, got %d", len(expectedShellCommands), len(mock.commands))
	}

	// Check combined history
	if len(hm.allHistory) != 2 {
		t.Errorf("Expected 2 commands in combined history, got %d", len(hm.allHistory))
	}

	// Navigation should be reset
	if hm.historyIndex != -1 {
		t.Errorf("History index should be reset to -1, got %d", hm.historyIndex)
	}
}

func TestHistoryManager_NavigateUp(t *testing.T) {
	mock := &MockShellHistoryProvider{
		commands:    []string{"command1", "command2", "command3"},
		historyFile: "/tmp/test_history",
	}

	hm := &HistoryManager{
		shellHistory:    mock,
		inMemoryHistory: make([]string, 0, 5),
		maxInMemory:     5,
		historyIndex:    -1,
	}

	// Load initial history
	err := hm.loadCombinedHistory()
	if err != nil {
		t.Fatalf("Failed to load initial history: %v", err)
	}

	currentText := "current input"

	// First navigation should go to the newest command
	result := hm.NavigateUp(currentText)
	if result != "command3" {
		t.Errorf("Expected 'command3', got '%s'", result)
	}

	// Second navigation should go to the previous command
	result = hm.NavigateUp("")
	if result != "command2" {
		t.Errorf("Expected 'command2', got '%s'", result)
	}

	// Third navigation should go to the oldest command
	result = hm.NavigateUp("")
	if result != "command1" {
		t.Errorf("Expected 'command1', got '%s'", result)
	}

	// Fourth navigation should stay at oldest command
	result = hm.NavigateUp("")
	if result != "command1" {
		t.Errorf("Expected to stay at 'command1', got '%s'", result)
	}

	// Verify we're still at the first command
	if hm.historyIndex != 0 {
		t.Errorf("Expected historyIndex to be 0 (oldest command), got %d", hm.historyIndex)
	}
}

func TestHistoryManager_NavigateDown(t *testing.T) {
	mock := &MockShellHistoryProvider{
		commands:    []string{"command1", "command2", "command3"},
		historyFile: "/tmp/test_history",
	}

	hm := &HistoryManager{
		shellHistory:    mock,
		inMemoryHistory: make([]string, 0, 5),
		maxInMemory:     5,
		historyIndex:    -1,
	}

	// Load initial history
	err := hm.loadCombinedHistory()
	if err != nil {
		t.Fatalf("Failed to load initial history: %v", err)
	}

	currentText := "current input"

	// Navigate up first to establish history position
	hm.NavigateUp(currentText) // command3
	hm.NavigateUp("")          // command2
	hm.NavigateUp("")          // command1

	// Now navigate down
	result := hm.NavigateDown("")
	if result != "command2" {
		t.Errorf("Expected 'command2', got '%s'", result)
	}

	result = hm.NavigateDown("")
	if result != "command3" {
		t.Errorf("Expected 'command3', got '%s'", result)
	}

	// Navigate down from newest should return to current input
	result = hm.NavigateDown("")
	if result != currentText {
		t.Errorf("Expected current input '%s', got '%s'", currentText, result)
	}

	// Further navigation down should return current input (no change)
	result = hm.NavigateDown("new current")
	if result != "new current" {
		t.Errorf("Expected 'new current', got '%s'", result)
	}
}

func TestHistoryManager_ResetNavigation(t *testing.T) {
	hm := &HistoryManager{
		historyIndex: 5,
		currentInput: "test input",
	}

	hm.ResetNavigation()

	if hm.historyIndex != -1 {
		t.Errorf("Expected historyIndex to be -1, got %d", hm.historyIndex)
	}

	if hm.currentInput != "" {
		t.Errorf("Expected currentInput to be empty, got '%s'", hm.currentInput)
	}
}

func TestRemoveDuplicates(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "no duplicates",
			input:    []string{"a", "b", "c"},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "with duplicates",
			input:    []string{"a", "b", "a", "c", "b"},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "empty slice",
			input:    []string{},
			expected: []string{},
		},
		{
			name:     "all duplicates",
			input:    []string{"a", "a", "a"},
			expected: []string{"a"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := removeDuplicates(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("Expected length %d, got %d", len(tt.expected), len(result))
				return
			}

			for i, cmd := range result {
				if cmd != tt.expected[i] {
					t.Errorf("Position %d: expected '%s', got '%s'", i, tt.expected[i], cmd)
				}
			}
		})
	}
}

func TestNewHistoryManager_Integration(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "history_manager_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Errorf("Failed to clean up temp directory: %v", err)
		}
	}()

	// Create a test history file
	historyFile := filepath.Join(tempDir, ".bash_history")
	content := "git status\nls -la\npwd\n"

	if err := os.WriteFile(historyFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test history file: %v", err)
	}

	// Temporarily set HOME to our test directory
	originalHome := os.Getenv("HOME")
	if err := os.Setenv("HOME", tempDir); err != nil {
		t.Fatalf("Failed to set HOME environment variable: %v", err)
	}
	defer func() {
		if err := os.Setenv("HOME", originalHome); err != nil {
			t.Errorf("Failed to restore HOME environment variable: %v", err)
		}
	}()

	// Create history manager
	hm, err := NewHistoryManager(5)
	if err != nil {
		t.Fatalf("NewHistoryManager failed: %v", err)
	}

	if hm == nil {
		t.Fatal("History manager should not be nil")
	}

	// Check that shell history was loaded
	if hm.GetHistoryCount() == 0 {
		t.Error("Expected history to be loaded")
	}

	// Test adding a new command
	err = hm.AddToHistory("echo test")
	if err != nil {
		t.Fatalf("AddToHistory failed: %v", err)
	}

	// Test navigation
	result := hm.NavigateUp("current")
	if result == "current" {
		t.Error("NavigateUp should return a history command, not current input")
	}
}
