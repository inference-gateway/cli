package history

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewShellHistory(t *testing.T) {
	sh, err := NewShellHistory()
	if err != nil {
		t.Fatalf("Failed to create shell history: %v", err)
	}

	if sh.homeDir == "" {
		t.Error("Home directory should not be empty")
	}

	if sh.shell == "" {
		t.Error("Shell should not be empty")
	}

	if sh.historyFile == "" {
		t.Error("History file should not be empty")
	}
}

func TestDetectShell(t *testing.T) {
	tests := []struct {
		name     string
		shellEnv string
		expected string
	}{
		{"zsh detection", "/usr/bin/zsh", "zsh"},
		{"bash detection", "/bin/bash", "bash"},
		{"empty shell", "", "bash"},
		{"unknown shell", "/usr/bin/fish", "bash"},
	}

	originalShell := os.Getenv("SHELL")
	defer func() {
		if err := os.Setenv("SHELL", originalShell); err != nil {
			t.Errorf("Failed to restore SHELL environment variable: %v", err)
		}
	}()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := os.Setenv("SHELL", tt.shellEnv); err != nil {
				t.Fatalf("Failed to set SHELL environment variable: %v", err)
			}
			result := detectShell()
			if result != tt.expected {
				t.Errorf("detectShell() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetHistoryFile(t *testing.T) {
	homeDir := "/home/test"

	tests := []struct {
		name     string
		shell    string
		expected string
	}{
		{"zsh history", "zsh", "/home/test/.zsh_history"},
		{"bash history", "bash", "/home/test/.bash_history"},
		{"unknown shell", "fish", "/home/test/.bash_history"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getHistoryFile(tt.shell, homeDir)
			if result != tt.expected {
				t.Errorf("getHistoryFile() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestLoadHistory(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "shell_history_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Errorf("Failed to clean up temp directory: %v", err)
		}
	}()

	// Test bash history
	t.Run("bash history", func(t *testing.T) {
		historyFile := filepath.Join(tempDir, ".bash_history")
		content := "echo hello\nls -la\npwd\necho world\n"

		if err := os.WriteFile(historyFile, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to write test history file: %v", err)
		}

		sh := &ShellHistory{
			shell:       "bash",
			historyFile: historyFile,
			homeDir:     tempDir,
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

	// Test zsh history with extended format
	t.Run("zsh history", func(t *testing.T) {
		historyFile := filepath.Join(tempDir, ".zsh_history")
		content := ": 1234567890:0;echo hello\n: 1234567891:0;ls -la\n: 1234567892:0;pwd\n"

		if err := os.WriteFile(historyFile, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to write test history file: %v", err)
		}

		sh := &ShellHistory{
			shell:       "zsh",
			historyFile: historyFile,
			homeDir:     tempDir,
		}

		commands, err := sh.LoadHistory()
		if err != nil {
			t.Fatalf("LoadHistory() failed: %v", err)
		}

		expected := []string{"echo hello", "ls -la", "pwd"}
		if len(commands) != len(expected) {
			t.Errorf("Expected %d commands, got %d", len(expected), len(commands))
		}

		for i, cmd := range commands {
			if cmd != expected[i] {
				t.Errorf("Command %d: expected %q, got %q", i, expected[i], cmd)
			}
		}
	})

	// Test nonexistent file
	t.Run("nonexistent file", func(t *testing.T) {
		sh := &ShellHistory{
			shell:       "bash",
			historyFile: filepath.Join(tempDir, "nonexistent"),
			homeDir:     tempDir,
		}

		commands, err := sh.LoadHistory()
		if err != nil {
			t.Fatalf("LoadHistory() should not fail for nonexistent file: %v", err)
		}

		if len(commands) != 0 {
			t.Errorf("Expected empty slice for nonexistent file, got %d commands", len(commands))
		}
	})
}

func TestSaveToHistory(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "shell_history_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Errorf("Failed to clean up temp directory: %v", err)
		}
	}()

	// Test bash history saving
	t.Run("bash history", func(t *testing.T) {
		historyFile := filepath.Join(tempDir, ".bash_history")

		sh := &ShellHistory{
			shell:       "bash",
			historyFile: historyFile,
			homeDir:     tempDir,
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
			t.Errorf("Expected %q, got %q", expected, string(content))
		}
	})

	// Test zsh history saving
	t.Run("zsh history", func(t *testing.T) {
		historyFile := filepath.Join(tempDir, ".zsh_history")

		sh := &ShellHistory{
			shell:       "zsh",
			historyFile: historyFile,
			homeDir:     tempDir,
		}

		err := sh.SaveToHistory("test command")
		if err != nil {
			t.Fatalf("SaveToHistory() failed: %v", err)
		}

		content, err := os.ReadFile(historyFile)
		if err != nil {
			t.Fatalf("Failed to read history file: %v", err)
		}

		contentStr := string(content)
		if !strings.HasPrefix(contentStr, ": ") {
			t.Error("Zsh history should start with timestamp prefix")
		}

		if !strings.Contains(contentStr, ";test command\n") {
			t.Error("Zsh history should contain the command after semicolon")
		}
	})

	// Test empty command
	t.Run("empty command", func(t *testing.T) {
		historyFile := filepath.Join(tempDir, ".empty_test")

		sh := &ShellHistory{
			shell:       "bash",
			historyFile: historyFile,
			homeDir:     tempDir,
		}

		err := sh.SaveToHistory("")
		if err != nil {
			t.Fatalf("SaveToHistory() should not fail for empty command: %v", err)
		}

		// File should not be created for empty command
		if _, err := os.Stat(historyFile); !os.IsNotExist(err) {
			t.Error("History file should not be created for empty command")
		}
	})
}
