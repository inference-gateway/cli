package history

import (
	"context"
	"slices"
	"strings"

	logger "github.com/inference-gateway/cli/internal/platform/logger"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
	projects "github.com/inference-gateway/cli/internal/projects"
)

// Store walks the input history for arrow-up recall: the project's persisted
// entries from the storage backend every client shares, plus this session's.
type Store struct {
	persisted       storage.ShellHistoryStorage
	inMemoryHistory []string
	maxInMemory     int
	historyIndex    int
	currentInput    string
	allHistory      []string
}

// NewMemoryOnlyStore creates a history store that persists nothing.
func NewMemoryOnlyStore(maxInMemory int) *Store {
	return NewStore(maxInMemory, nil)
}

// NewStore creates a history store on top of the storage backend, loaded with
// the current project's entries. A nil backend keeps the history in memory.
func NewStore(maxInMemory int, persisted storage.ShellHistoryStorage) *Store {
	hm := &Store{
		persisted:       persisted,
		inMemoryHistory: make([]string, 0, maxInMemory),
		maxInMemory:     maxInMemory,
		historyIndex:    -1,
		allHistory:      []string{},
	}
	if persisted == nil {
		return hm
	}
	loaded, err := persisted.LoadHistory(context.Background(), projects.Path(), 0)
	if err != nil {
		logger.Warn("could not load input history", "error", err)
		return hm
	}
	hm.allHistory = append(hm.allHistory, loaded...)
	return hm
}

// AddToHistory adds a command to both in-memory and shell history
func (hm *Store) AddToHistory(command string) error {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil
	}

	shouldSave := len(hm.allHistory) == 0 || hm.allHistory[len(hm.allHistory)-1] != command

	if shouldSave {
		hm.addToInMemoryHistory(command)
		hm.allHistory = append(hm.allHistory, command)

		if hm.persisted != nil {
			if err := hm.persisted.AppendHistory(context.Background(), projects.Path(), command); err != nil {
				logger.Warn("could not save input history", "error", err)
			}
		}
	}

	hm.historyIndex = -1
	hm.currentInput = ""

	return nil
}

// addToInMemoryHistory adds a command to in-memory history with size limit
func (hm *Store) addToInMemoryHistory(command string) {
	if len(hm.inMemoryHistory) > 0 && hm.inMemoryHistory[len(hm.inMemoryHistory)-1] == command {
		return
	}

	hm.inMemoryHistory = append(hm.inMemoryHistory, command)

	if len(hm.inMemoryHistory) > hm.maxInMemory {
		hm.inMemoryHistory = hm.inMemoryHistory[1:]
	}
}

// NavigateUp moves up in history (to older commands)
func (hm *Store) NavigateUp(currentText string) string {
	if len(hm.allHistory) == 0 {
		return currentText
	}

	if hm.historyIndex == -1 {
		hm.currentInput = currentText
		hm.historyIndex = len(hm.allHistory) - 1
	} else if hm.historyIndex > 0 {
		hm.historyIndex--
	}

	result := hm.allHistory[hm.historyIndex]
	return result
}

// NavigateDown moves down in history (to newer commands)
func (hm *Store) NavigateDown(currentText string) string {
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
func (hm *Store) ResetNavigation() {
	hm.historyIndex = -1
	hm.currentInput = ""
}

// GetHistoryCount returns the total number of commands in history
func (hm *Store) GetHistoryCount() int {
	return len(hm.allHistory)
}

// GetAllHistory returns a copy of the combined prompt history, oldest to newest.
func (hm *Store) GetAllHistory() []string {
	return slices.Clone(hm.allHistory)
}

// IsNavigating returns true if currently navigating through history
func (hm *Store) IsNavigating() bool {
	return hm.historyIndex != -1
}
