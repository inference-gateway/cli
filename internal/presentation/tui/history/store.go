package history

import (
	"context"
	"slices"

	logger "github.com/inference-gateway/cli/internal/platform/logger"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
	projects "github.com/inference-gateway/cli/internal/projects"
)

// Store walks the project's input history for arrow-up recall. The storage
// backend owns the entries and their rules, the store only navigates them.
type Store struct {
	history      storage.ShellHistoryStorage
	historyIndex int
	currentInput string
	allHistory   []string
}

// NewMemoryOnlyStore creates a history store that forgets at exit.
func NewMemoryOnlyStore() *Store {
	return NewStore(storage.NewMemoryHistory())
}

// NewStore creates a history store on top of the storage backend, loaded with
// the current project's entries.
func NewStore(history storage.ShellHistoryStorage) *Store {
	hm := &Store{history: history, historyIndex: -1}
	hm.reload()
	return hm
}

// reload takes the project's entries from the backend, which is what every
// other client sees too. A backend that cannot answer leaves the last list.
func (hm *Store) reload() {
	loaded, err := hm.history.LoadHistory(context.Background(), projects.Path(), 0)
	if err != nil {
		logger.Warn("could not load input history", "error", err)
		return
	}
	hm.allHistory = append([]string{}, loaded...)
}

// AddToHistory records a submitted input and leaves navigation at the newest
// entry.
func (hm *Store) AddToHistory(command string) error {
	if err := hm.history.AppendHistory(context.Background(), projects.Path(), command); err != nil {
		logger.Warn("could not save input history", "error", err)
	}
	hm.reload()
	hm.historyIndex = -1
	hm.currentInput = ""
	return nil
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
