package history_test

import (
	"os"
	"path/filepath"
	"testing"

	history "github.com/inference-gateway/cli/internal/ui/history"
)

func TestHistoryManager_PublicAPI(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "history_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Errorf("Failed to remove temp dir: %v", err)
		}
	}()

	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		if err := os.Chdir(originalWd); err != nil {
			t.Errorf("Failed to restore working directory: %v", err)
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
	content := "existing command\n"
	if err := os.WriteFile(historyFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test history file: %v", err)
	}

	hm, err := history.NewHistoryManager(5)
	if err != nil {
		t.Fatalf("NewHistoryManager failed: %v", err)
	}

	if hm == nil {
		t.Fatal("History manager should not be nil")
	}

	initialCount := hm.GetHistoryCount()
	if initialCount == 0 {
		t.Errorf("Expected history to be loaded, got count: %d", initialCount)
	}

	err = hm.AddToHistory("new command")
	if err != nil {
		t.Fatalf("AddToHistory failed: %v", err)
	}

	newCount := hm.GetHistoryCount()
	if newCount != initialCount+1 {
		t.Errorf("Expected count %d, got %d", initialCount+1, newCount)
	}

	currentText := "current input"
	result := hm.NavigateUp(currentText)
	if result == currentText {
		t.Error("NavigateUp should return a history command")
	}

	downResult := hm.NavigateDown("")
	if downResult == "" {
		t.Error("NavigateDown should return a value")
	}

	hm.ResetNavigation()

	histFile := hm.GetShellHistoryFile()
	if histFile == "" {
		t.Error("Expected non-empty history file path")
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

	hm, err := history.NewHistoryManager(5)
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
