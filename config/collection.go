package config

import "fmt"

// CollectionConfig is implemented by file-backed configs whose payload is a
// named, mutable collection of entries. MCPConfig (collection of
// MCPServerEntry) and AgentsConfig (collection of AgentEntry) implement it.
//
// Methods mutate the file on disk; the in-memory receiver stays in sync.
// Implementations must remember the file path they were loaded from so
// each call can persist back to the same location.
type CollectionConfig[E any] interface {
	CreateEntry(entry E) error
	ReadEntry(name string) (*E, error)
	UpdateEntry(entry E) error
	DeleteEntry(name string) error
	ListEntries() []E
}

// appendEntry returns slice with entry appended, after rejecting duplicates
// by name. kind is used in the duplicate-error message (e.g. "MCP server").
func appendEntry[E any](slice []E, entry E, name string, nameOf func(E) string, kind string) ([]E, error) {
	for _, existing := range slice {
		if nameOf(existing) == name {
			return nil, fmt.Errorf("%s with name '%s' already exists", kind, name)
		}
	}
	return append(slice, entry), nil
}

// replaceEntry returns slice with the entry whose name matches replaced
// by the new entry. Returns an error if no entry with that name exists.
func replaceEntry[E any](slice []E, entry E, name string, nameOf func(E) string, kind string) ([]E, error) {
	for i, existing := range slice {
		if nameOf(existing) == name {
			next := make([]E, len(slice))
			copy(next, slice)
			next[i] = entry
			return next, nil
		}
	}
	return nil, fmt.Errorf("%s with name '%s' not found", kind, name)
}

// removeEntry returns slice with the named entry removed. Returns an error
// if no entry with that name exists.
func removeEntry[E any](slice []E, name string, nameOf func(E) string, kind string) ([]E, error) {
	out := make([]E, 0, len(slice))
	found := false
	for _, existing := range slice {
		if nameOf(existing) == name {
			found = true
			continue
		}
		out = append(out, existing)
	}
	if !found {
		return nil, fmt.Errorf("%s with name '%s' not found", kind, name)
	}
	return out, nil
}

// findEntry returns a copy of the entry whose name matches, or an error if
// no entry with that name exists.
func findEntry[E any](slice []E, name string, nameOf func(E) string, kind string) (*E, error) {
	for _, existing := range slice {
		if nameOf(existing) == name {
			e := existing
			return &e, nil
		}
	}
	return nil, fmt.Errorf("%s with name '%s' not found", kind, name)
}
