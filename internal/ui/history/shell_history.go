package history

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	logger "github.com/inference-gateway/cli/internal/logger"
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

	if err := migrateLegacyMainHistory(historyDir); err != nil {
		logger.Warn("could not migrate legacy history file", "error", err)
	}

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

// migrateLegacyMainHistory upgrades the pre-history/ layout in place. Older
// versions stored the main history at <configDir>/history as a plain file;
// the current layout needs <configDir>/history to be a directory (holding
// history/history and history/history-<name>). When the legacy file is present
// it is moved to history/history so existing history survives and directory
// creation does not fail with ENOTDIR. No-op when the path is absent or is
// already a directory.
//
// TODO: this one-time migration can be removed in a future version once
// existing installs have upgraded past the history/ layout change.
func migrateLegacyMainHistory(historyDir string) error {
	info, err := os.Stat(historyDir)
	if err != nil || info.IsDir() {
		return nil
	}

	staged := historyDir + ".migrating"
	if err := os.Rename(historyDir, staged); err != nil {
		return fmt.Errorf("staging legacy history file: %w", err)
	}
	if err := os.MkdirAll(historyDir, 0755); err != nil {
		_ = os.Rename(staged, historyDir)
		return fmt.Errorf("creating history directory: %w", err)
	}
	if err := os.Rename(staged, filepath.Join(historyDir, "history")); err != nil {
		return fmt.Errorf("moving legacy history into place: %w", err)
	}

	return nil
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
