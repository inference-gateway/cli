package history

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ShellHistoryProvider interface defines methods for shell history operations
type ShellHistoryProvider interface {
	LoadHistory() ([]string, error)
	SaveToHistory(command string) error
	GetHistoryFile() string
}

// ShellHistory implements shell history integration for bash and zsh
type ShellHistory struct {
	shell       string
	historyFile string
	homeDir     string
}

// NewShellHistory creates a new shell history provider
func NewShellHistory() (*ShellHistory, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	shell := detectShell()
	historyFile := getHistoryFile(shell, homeDir)

	return &ShellHistory{
		shell:       shell,
		historyFile: historyFile,
		homeDir:     homeDir,
	}, nil
}

// detectShell detects the current shell being used
func detectShell() string {
	if shell := os.Getenv("SHELL"); shell != "" {
		if strings.Contains(shell, "zsh") {
			return "zsh"
		}
		if strings.Contains(shell, "bash") {
			return "bash"
		}
	}

	return "bash"
}

// getHistoryFile returns the appropriate history file path for the shell
func getHistoryFile(shell, homeDir string) string {
	switch shell {
	case "zsh":
		if histfile := os.Getenv("HISTFILE"); histfile != "" {
			return histfile
		}
		return filepath.Join(homeDir, ".zsh_history")
	case "bash":
		if histfile := os.Getenv("HISTFILE"); histfile != "" {
			return histfile
		}
		return filepath.Join(homeDir, ".bash_history")
	default:
		return filepath.Join(homeDir, ".bash_history")
	}
}

// LoadHistory loads commands from shell history file
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

		if sh.shell == "zsh" && strings.HasPrefix(line, ":") {
			parts := strings.SplitN(line, ";", 2)
			if len(parts) == 2 {
				line = parts[1]
			}
		}

		if len(commands) == 0 || commands[len(commands)-1] != line {
			commands = append(commands, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading history file: %w", err)
	}

	return commands, nil
}

// SaveToHistory appends a command to the shell history file
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

	var entry string
	if sh.shell == "zsh" {
		timestamp := time.Now().Unix()
		entry = fmt.Sprintf(": %d:0;%s\n", timestamp, command)
	} else {
		entry = command + "\n"
	}

	if _, err := file.WriteString(entry); err != nil {
		return fmt.Errorf("failed to write to history file: %w", err)
	}

	return nil
}

// GetHistoryFile returns the current history file path
func (sh *ShellHistory) GetHistoryFile() string {
	return sh.historyFile
}

// GetShell returns the detected shell type
func (sh *ShellHistory) GetShell() string {
	return sh.shell
}
