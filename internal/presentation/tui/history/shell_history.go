package history

import (
	"context"
	"strings"

	storage "github.com/inference-gateway/cli/internal/platform/storage"
	projects "github.com/inference-gateway/cli/internal/projects"
)

// ShellHistoryProvider is the persisted side of the input history.
type ShellHistoryProvider interface {
	LoadHistory() ([]string, error)
	SaveToHistory(command string) error
}

// StoreShellHistory persists the current project's input history in the
// storage backend every client shares.
type StoreShellHistory struct {
	store storage.ShellHistoryStorage
}

// NewStoreShellHistory creates a shell history provider backed by the given
// ShellHistoryStorage.
func NewStoreShellHistory(store storage.ShellHistoryStorage) *StoreShellHistory {
	return &StoreShellHistory{store: store}
}

// LoadHistory loads the project's commands from the storage backend.
func (sh *StoreShellHistory) LoadHistory() ([]string, error) {
	return sh.store.LoadHistory(context.Background(), projects.Path(), 0)
}

// SaveToHistory appends a command to the project's history in the storage backend.
func (sh *StoreShellHistory) SaveToHistory(command string) error {
	if strings.TrimSpace(command) == "" {
		return nil
	}
	return sh.store.AppendHistory(context.Background(), projects.Path(), command)
}
