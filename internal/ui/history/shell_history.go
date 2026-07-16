package history

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	storage "github.com/inference-gateway/cli/internal/infra/storage"
)

// ShellHistoryProvider interface defines methods for shell history operations
type ShellHistoryProvider interface {
	LoadHistory() ([]string, error)
	SaveToHistory(command string) error
	GetHistoryFile() string
}

// ShellHistory implements project-specific history backed by a plain file.
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

// StoreShellHistory is a ShellHistoryProvider backed by a storage backend
// (jsonl/sqlite/postgres/redis/memory/d1) instead of a local file.
type StoreShellHistory struct {
	store storage.ShellHistoryStorage
}

// NewStoreShellHistory creates a shell history provider backed by the given
// ShellHistoryStorage.
func NewStoreShellHistory(store storage.ShellHistoryStorage) *StoreShellHistory {
	return &StoreShellHistory{store: store}
}

// LoadHistory loads all commands from the storage backend.
func (sh *StoreShellHistory) LoadHistory() ([]string, error) {
	return sh.store.LoadHistory(context.Background(), 0)
}

// SaveToHistory appends a command to the storage backend.
func (sh *StoreShellHistory) SaveToHistory(command string) error {
	if strings.TrimSpace(command) == "" {
		return nil
	}
	return sh.store.AppendHistory(context.Background(), command)
}

// GetHistoryFile returns "" - the history is not backed by a single local file.
func (sh *StoreShellHistory) GetHistoryFile() string {
	return ""
}

// LoadHistory loads commands from the history file
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
