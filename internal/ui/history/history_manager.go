package history

import (
	"fmt"
	"strings"

	"github.com/inference-gateway/cli/internal/logger"
)

// HistoryManager manages both in-memory and shell history
type HistoryManager struct {
	shellHistory    ShellHistoryProvider
	inMemoryHistory []string
	maxInMemory     int
	historyIndex    int
	currentInput    string
	allHistory      []string
}

// NewHistoryManager creates a new history manager
func NewHistoryManager(maxInMemory int) (*HistoryManager, error) {
	return NewHistoryManagerWithDir(maxInMemory, ".infer")
}

// NewHistoryManagerWithDir creates a new history manager with a custom config directory
func NewHistoryManagerWithDir(maxInMemory int, configDir string) (*HistoryManager, error) {
	shellHistory, err := NewShellHistoryWithDir(configDir)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize shell history: %w", err)
	}

	hm := &HistoryManager{
		shellHistory:    shellHistory,
		inMemoryHistory: make([]string, 0, maxInMemory),
		maxInMemory:     maxInMemory,
		historyIndex:    -1,
		currentInput:    "",
	}

	if err := hm.loadCombinedHistory(); err != nil {
		hm.allHistory = make([]string, 0)
	}

	return hm, nil
}

// NewMemoryOnlyHistoryManager creates a history manager that only uses in-memory storage
func NewMemoryOnlyHistoryManager(maxInMemory int) *HistoryManager {
	return &HistoryManager{
		shellHistory:    &MemoryOnlyShellHistory{},
		inMemoryHistory: make([]string, 0, maxInMemory),
		maxInMemory:     maxInMemory,
		historyIndex:    -1,
		currentInput:    "",
		allHistory:      make([]string, 0),
	}
}

// loadCombinedHistory loads history from shell and combines with in-memory history
func (hm *HistoryManager) loadCombinedHistory() error {
	shellCommands, err := hm.shellHistory.LoadHistory()
	if err != nil {
		return err
	}

	hm.allHistory = make([]string, 0, len(shellCommands)+len(hm.inMemoryHistory))
	hm.allHistory = append(hm.allHistory, shellCommands...)
	hm.allHistory = append(hm.allHistory, hm.inMemoryHistory...)

	hm.allHistory = removeDuplicates(hm.allHistory)

	return nil
}

// AddToHistory adds a command to both in-memory and shell history
func (hm *HistoryManager) AddToHistory(command string) error {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil
	}

	hm.addToInMemoryHistory(command)

	if err := hm.shellHistory.SaveToHistory(command); err != nil {
		logger.Warn("Could not save to shell history", "error", err)
	}

	if err := hm.loadCombinedHistory(); err != nil {
		logger.Warn("Could not reload combined history", "error", err)
	}

	hm.historyIndex = -1
	hm.currentInput = ""

	return nil
}

// addToInMemoryHistory adds a command to in-memory history with size limit
func (hm *HistoryManager) addToInMemoryHistory(command string) {
	if len(hm.inMemoryHistory) > 0 && hm.inMemoryHistory[len(hm.inMemoryHistory)-1] == command {
		return
	}

	hm.inMemoryHistory = append(hm.inMemoryHistory, command)

	if len(hm.inMemoryHistory) > hm.maxInMemory {
		hm.inMemoryHistory = hm.inMemoryHistory[1:]
	}
}

// NavigateUp moves up in history (to older commands)
func (hm *HistoryManager) NavigateUp(currentText string) string {
	if len(hm.allHistory) == 0 {
		return currentText
	}

	if hm.historyIndex == -1 {
		hm.currentInput = currentText
		hm.historyIndex = len(hm.allHistory) - 1
	} else if hm.historyIndex > 0 {
		hm.historyIndex--
	} else {
		return hm.allHistory[hm.historyIndex]
	}

	return hm.allHistory[hm.historyIndex]
}

// NavigateDown moves down in history (to newer commands)
func (hm *HistoryManager) NavigateDown(currentText string) string {
	if hm.historyIndex == -1 {
		return currentText
	}

	if hm.historyIndex < len(hm.allHistory)-1 {
		hm.historyIndex++
		return hm.allHistory[hm.historyIndex]
	} else {
		hm.historyIndex = -1
		result := hm.currentInput
		hm.currentInput = ""
		return result
	}
}

// ResetNavigation resets history navigation state
func (hm *HistoryManager) ResetNavigation() {
	hm.historyIndex = -1
	hm.currentInput = ""
}

// GetHistoryCount returns the total number of commands in history
func (hm *HistoryManager) GetHistoryCount() int {
	return len(hm.allHistory)
}

// GetShellHistoryFile returns the shell history file path
func (hm *HistoryManager) GetShellHistoryFile() string {
	return hm.shellHistory.GetHistoryFile()
}

// removeDuplicates removes duplicate commands while preserving order
func removeDuplicates(commands []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(commands))

	for _, command := range commands {
		if !seen[command] {
			seen[command] = true
			result = append(result, command)
		}
	}

	return result
}

// MemoryOnlyShellHistory provides a no-op shell history provider for testing
type MemoryOnlyShellHistory struct{}

func (m *MemoryOnlyShellHistory) LoadHistory() ([]string, error) {
	return []string{}, nil
}

func (m *MemoryOnlyShellHistory) SaveToHistory(command string) error {
	return nil
}

func (m *MemoryOnlyShellHistory) GetHistoryFile() string {
	return ""
}
