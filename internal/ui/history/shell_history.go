package history

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ShellHistoryProvider interface defines methods for shell history operations
type ShellHistoryProvider interface {
	LoadHistory() ([]string, error)
	SaveToHistory(command string) error
	GetHistoryFile() string
}

// ShellHistory implements project-specific history
type ShellHistory struct {
	historyFile string
}

// NewShellHistory creates a new shell history provider
func NewShellHistory() (*ShellHistory, error) {
	return NewShellHistoryWithName(".infer", "")
}

// NewShellHistoryWithName creates a new shell history provider with a custom config
// directory and an optional name. When name is empty, the history file is stored at
// <configDir>/history/history (the main agent). When name is non-empty, the history
// file is stored at <configDir>/history/history-<name> (e.g. for subagents).
func NewShellHistoryWithName(configDir, name string) (*ShellHistory, error) {
	historyDir := filepath.Join(configDir, "history")
	var historyFile string
	if name == "" {
		historyFile = filepath.Join(historyDir, "history")
	} else {
		historyFile = filepath.Join(historyDir, "history-"+name)
	}

	return &ShellHistory{
		historyFile: historyFile,
	}, nil
}

// LoadHistory loads commands from history file
func (sh *ShellHistory) LoadHistory() ([]string, error) {
	file, err := os.Open(sh.historyFile)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("failed to open history file %s: %w", sh.historyFile, err)
	}
	defer func() {
		_ = file.Close()
	}()

	var commands []string
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		unescaped := strings.ReplaceAll(line, "\\n", "\n")
		commands = append(commands, unescaped)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading history file: %w", err)
	}

	return commands, nil
}

// SaveToHistory appends a command to the history file
func (sh *ShellHistory) SaveToHistory(command string) error {
	if strings.TrimSpace(command) == "" {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(sh.historyFile), 0755); err != nil {
		return fmt.Errorf("failed to create history directory: %w", err)
	}

	file, err := os.OpenFile(sh.historyFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open history file for writing: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	escaped := strings.ReplaceAll(command, "\n", "\\n")
	entry := escaped + "\n"

	if _, err := file.WriteString(entry); err != nil {
		return fmt.Errorf("failed to write to history file: %w", err)
	}

	return nil
}

// GetHistoryFile returns the current history file path
func (sh *ShellHistory) GetHistoryFile() string {
	return sh.historyFile
}
