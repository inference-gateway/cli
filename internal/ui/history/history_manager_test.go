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

	err := hm.loadCombinedHistory()
	if err != nil {
		t.Fatalf("Failed to load initial history: %v", err)
	}

	err = hm.AddToHistory("new command")
	if err != nil {
		t.Fatalf("AddToHistory failed: %v", err)
	}

	expectedShellCommands := []string{"existing command", "new command"}
	if len(mock.commands) != len(expectedShellCommands) {
		t.Errorf("Expected %d shell commands, got %d", len(expectedShellCommands), len(mock.commands))
	}

	if len(hm.allHistory) != 2 {
		t.Errorf("Expected 2 commands in combined history, got %d", len(hm.allHistory))
	}

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

	err := hm.loadCombinedHistory()
	if err != nil {
		t.Fatalf("Failed to load initial history: %v", err)
	}

	currentText := "current input"

	result := hm.NavigateUp(currentText)
	if result != "command3" {
		t.Errorf("Expected 'command3', got '%s'", result)
	}

	result = hm.NavigateUp("")
	if result != "command2" {
		t.Errorf("Expected 'command2', got '%s'", result)
	}

	result = hm.NavigateUp("")
	if result != "command1" {
		t.Errorf("Expected 'command1', got '%s'", result)
	}

	result = hm.NavigateUp("")
	if result != "command1" {
		t.Errorf("Expected to stay at 'command1', got '%s'", result)
	}

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

	err := hm.loadCombinedHistory()
	if err != nil {
		t.Fatalf("Failed to load initial history: %v", err)
	}

	currentText := "current input"

	hm.NavigateUp(currentText)
	hm.NavigateUp("")
	hm.NavigateUp("")

	result := hm.NavigateDown("")
	if result != "command2" {
		t.Errorf("Expected 'command2', got '%s'", result)
	}

	result = hm.NavigateDown("")
	if result != "command3" {
		t.Errorf("Expected 'command3', got '%s'", result)
	}

	result = hm.NavigateDown("")
	if result != currentText {
		t.Errorf("Expected current input '%s', got '%s'", currentText, result)
	}

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
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}

	tempDir, err := os.MkdirTemp("", "history_manager_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.Chdir(originalWd); err != nil {
			t.Errorf("Failed to restore working directory: %v", err)
		}
		if err := os.RemoveAll(tempDir); err != nil {
			t.Errorf("Failed to clean up temp directory: %v", err)
		}
	}()

	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	inferDir := ".infer"
	if err := os.MkdirAll(inferDir, 0755); err != nil {
		t.Fatalf("Failed to create .infer directory: %v", err)
	}

	historyFile := filepath.Join(inferDir, "history")
	content := "git status\nls -la\npwd\n"
	if err := os.WriteFile(historyFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test history file: %v", err)
	}

	hm, err := NewHistoryManager(5)
	if err != nil {
		t.Fatalf("NewHistoryManager failed: %v", err)
	}

	if hm == nil {
		t.Fatal("History manager should not be nil")
	}

	historyCount := hm.GetHistoryCount()
	if historyCount == 0 {
		t.Errorf("Expected history to be loaded, got count: %d, history file: %s", historyCount, hm.GetShellHistoryFile())
	}

	err = hm.AddToHistory("echo test")
	if err != nil {
		t.Fatalf("AddToHistory failed: %v", err)
	}

	result := hm.NavigateUp("current")
	if result == "current" {
		t.Error("NavigateUp should return a history command, not current input")
	}
}
