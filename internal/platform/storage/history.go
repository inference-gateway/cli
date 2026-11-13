package storage

import (
	"context"
	"strings"
)

// distinctHistory is the input history every client appends to: an entry is
// trimmed, a blank one is dropped and a repeat of the newest one is skipped,
// whatever backend holds the list.
type distinctHistory struct {
	ShellHistoryStorage
}

// NewMemoryHistory is an input history that lives for the process only, with
// the same rules as a persisted one.
func NewMemoryHistory() ShellHistoryStorage {
	return distinctHistory{NewMemoryStorage()}
}

// AppendHistory records a command unless it is blank or repeats the project's
// newest entry.
func (h distinctHistory) AppendHistory(ctx context.Context, project, command string) error {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil
	}
	newest, err := h.LoadHistory(ctx, project, 1)
	if err != nil {
		return err
	}
	if len(newest) > 0 && newest[0] == command {
		return nil
	}
	return h.ShellHistoryStorage.AppendHistory(ctx, project, command)
}
