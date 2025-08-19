package history

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewShellHistory(t *testing.T) {
	sh, err := NewShellHistory()
	if err != nil {
		t.Fatalf("Failed to create shell history: %v", err)
	}

	expectedPath := filepath.Join(".infer", "history")
	if sh.historyFile != expectedPath {
		t.Errorf("Expected history file path %s, got %s", expectedPath, sh.historyFile)
	}
}

func TestLoadHistory(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "shell_history_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Errorf("Failed to clean up temp directory: %v", err)
		}
	}()

	t.Run("load history", func(t *testing.T) {
		historyFile := filepath.Join(tempDir, "history")
		content := "echo hello\nls -la\npwd\necho world\n"

		if err := os.WriteFile(historyFile, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to write test history file: %v", err)
		}

		sh := &ShellHistory{
			historyFile: historyFile,
		}

		commands, err := sh.LoadHistory()
		if err != nil {
			t.Fatalf("LoadHistory() failed: %v", err)
		}

		expected := []string{"echo hello", "ls -la", "pwd", "echo world"}
		if len(commands) != len(expected) {
			t.Errorf("Expected %d commands, got %d", len(expected), len(commands))
		}

		for i, cmd := range commands {
			if cmd != expected[i] {
				t.Errorf("Command %d: expected %q, got %q", i, expected[i], cmd)
			}
		}
	})

	t.Run("empty history file", func(t *testing.T) {
		historyFile := filepath.Join(tempDir, "empty_history")

		sh := &ShellHistory{
			historyFile: historyFile,
		}

		commands, err := sh.LoadHistory()
		if err != nil {
			t.Fatalf("LoadHistory() should not fail for non-existent file: %v", err)
		}

		if len(commands) != 0 {
			t.Errorf("Expected 0 commands for non-existent file, got %d", len(commands))
		}
	})
}

func TestSaveToHistory(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "shell_history_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Errorf("Failed to clean up temp directory: %v", err)
		}
	}()

	t.Run("save command", func(t *testing.T) {
		historyFile := filepath.Join(tempDir, ".infer", "history")

		sh := &ShellHistory{
			historyFile: historyFile,
		}

		err := sh.SaveToHistory("test command")
		if err != nil {
			t.Fatalf("SaveToHistory() failed: %v", err)
		}

		content, err := os.ReadFile(historyFile)
		if err != nil {
			t.Fatalf("Failed to read history file: %v", err)
		}

		expected := "test command\n"
		if string(content) != expected {
			t.Errorf("Expected history file to contain %q, got %q", expected, string(content))
		}
	})

	t.Run("append to existing history", func(t *testing.T) {
		historyFile := filepath.Join(tempDir, ".infer", "history2")

		if err := os.MkdirAll(filepath.Dir(historyFile), 0755); err != nil {
			t.Fatalf("Failed to create directory: %v", err)
		}

		if err := os.WriteFile(historyFile, []byte("first command\n"), 0644); err != nil {
			t.Fatalf("Failed to write initial history: %v", err)
		}

		sh := &ShellHistory{
			historyFile: historyFile,
		}

		err := sh.SaveToHistory("second command")
		if err != nil {
			t.Fatalf("SaveToHistory() failed: %v", err)
		}

		content, err := os.ReadFile(historyFile)
		if err != nil {
			t.Fatalf("Failed to read history file: %v", err)
		}

		expected := "first command\nsecond command\n"
		if string(content) != expected {
			t.Errorf("Expected history file to contain %q, got %q", expected, string(content))
		}
	})

	t.Run("skip empty commands", func(t *testing.T) {
		historyFile := filepath.Join(tempDir, ".infer", "history3")

		sh := &ShellHistory{
			historyFile: historyFile,
		}

		err := sh.SaveToHistory("")
		if err != nil {
			t.Fatalf("SaveToHistory() should not fail for empty command: %v", err)
		}

		if _, err := os.Stat(historyFile); !os.IsNotExist(err) {
			t.Error("History file should not be created for empty command")
		}
	})
}

func TestGetHistoryFile(t *testing.T) {
	sh, err := NewShellHistory()
	if err != nil {
		t.Fatalf("Failed to create shell history: %v", err)
	}

	expectedPath := filepath.Join(".infer", "history")
	if sh.GetHistoryFile() != expectedPath {
		t.Errorf("Expected GetHistoryFile() to return %s, got %s", expectedPath, sh.GetHistoryFile())
	}
}
