package shortcuts

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

// warnedShadowed records the built-in names a shadowed custom shortcut has
// already warned about. ponytail: process-wide and never reset, so each name
// warns once per process and a reload from a different file stays silent.
var warnedShadowed = new(sync.Map)

// Registry manages all available shortcuts
type Registry struct {
	shortcuts map[string]Shortcut
	mutex     sync.RWMutex
}

// NewRegistry creates a new shortcut registry
func NewRegistry() *Registry {
	return &Registry{
		shortcuts: make(map[string]Shortcut),
	}
}

// LoadCustomShortcuts loads user-defined shortcuts from each base directory in
// turn, so config.ConfigLookupDirs() can pass the userspace baseline followed by
// the project override: later directories overlay earlier ones by shortcut name
// rather than replacing the whole set. A custom shortcut never shadows a
// built-in one (registered first), which keeps an init-seeded file like
// ~/.infer/shortcuts/a2a.yaml from hiding /agents. The shadow warning fires
// once per process per name, not once per registry.
func (r *Registry) LoadCustomShortcuts(baseDirs []string, client sdk.Client, modelService convdomain.ModelService, imageService agentdomain.ImageService, toolService agentdomain.ToolService) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	for _, baseDir := range baseDirs {
		customShortcuts, err := LoadCustomShortcuts(baseDir, client, modelService, imageService, toolService)
		if err != nil {
			return fmt.Errorf("failed to load custom shortcuts from %s: %w", baseDir, err)
		}

		for _, shortcut := range customShortcuts {
			name := shortcut.GetName()
			if existing, exists := r.shortcuts[name]; exists {
				if _, isCustom := existing.(*CustomShortcut); !isCustom {
					if _, dup := warnedShadowed.LoadOrStore(name, struct{}{}); !dup {
						logger.Warn("ignoring custom shortcut that shadows a built-in shortcut, delete or rename the file", "name", name, "file", customShortcutSourceFile(shortcut))
					}
					continue
				}
			}
			r.shortcuts[name] = shortcut
		}
	}

	return nil
}

// customShortcutSourceFile names the YAML file a custom shortcut was loaded
// from, for warnings; empty for anything else.
func customShortcutSourceFile(shortcut Shortcut) string {
	if cs, ok := shortcut.(*CustomShortcut); ok {
		return cs.sourceFile
	}
	return ""
}

// Register adds a shortcut to the registry
func (r *Registry) Register(shortcut Shortcut) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.shortcuts[shortcut.GetName()] = shortcut
}

// Unregister removes a shortcut from the registry
func (r *Registry) Unregister(name string) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	delete(r.shortcuts, name)
}

// Get retrieves a shortcut by name
func (r *Registry) Get(name string) (Shortcut, bool) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	shortcut, exists := r.shortcuts[name]
	return shortcut, exists
}

// List returns all registered shortcut names
func (r *Registry) List() []string {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	names := make([]string, 0, len(r.shortcuts))
	for name := range r.shortcuts {
		names = append(names, name)
	}

	slices.Sort(names)
	return names
}

// GetAll returns all registered shortcuts
func (r *Registry) GetAll() []Shortcut {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	shortcuts := make([]Shortcut, 0, len(r.shortcuts))
	for _, shortcut := range r.shortcuts {
		shortcuts = append(shortcuts, shortcut)
	}

	slices.SortFunc(shortcuts, func(a, b Shortcut) int {
		return cmp.Compare(a.GetName(), b.GetName())
	})

	return shortcuts
}

// Execute executes a shortcut by name with arguments
func (r *Registry) Execute(ctx context.Context, name string, args []string) (ShortcutResult, error) {
	shortcut, exists := r.Get(name)
	if !exists {
		return ShortcutResult{}, fmt.Errorf("unknown shortcut: %s", name)
	}

	if !shortcut.CanExecute(args) {
		return ShortcutResult{
			Output:  fmt.Sprintf("Invalid arguments for shortcut '%s'. Usage: %s", name, shortcut.GetUsage()),
			Success: false,
		}, nil
	}

	return shortcut.Execute(ctx, args)
}

// ParseShortcut parses a shortcut line input into shortcut name and arguments
// Handles quoted strings properly
func (r *Registry) ParseShortcut(input string) (string, []string, error) {
	input = strings.TrimSpace(input)
	if !strings.HasPrefix(input, "/") {
		return "", nil, fmt.Errorf("shortcuts must start with '/'")
	}

	input = input[1:]

	parts, err := parseShortcutLine(input)
	if err != nil {
		return "", nil, err
	}

	if len(parts) == 0 {
		return "", nil, fmt.Errorf("empty shortcut")
	}

	shortcut := parts[0]
	args := parts[1:]

	return shortcut, args, nil
}

// parseShortcutLine parses a shortcut line string into arguments, handling quoted strings
func parseShortcutLine(input string) ([]string, error) {
	var args []string
	var current strings.Builder
	inQuotes := false
	quoteChar := byte(0)

	for i := 0; i < len(input); i++ {
		char := input[i]

		switch {
		case !inQuotes && (char == '"' || char == '\''):
			inQuotes = true
			quoteChar = char
		case inQuotes && char == quoteChar:
			inQuotes = false
			quoteChar = 0
		case !inQuotes && (char == ' ' || char == '\t' || char == '\n' || char == '\r'):
			if current.Len() > 0 {
				arg := current.String()
				args = append(args, arg)
				current.Reset()
			}

			for i+1 < len(input) && (input[i+1] == ' ' || input[i+1] == '\t' || input[i+1] == '\n' || input[i+1] == '\r') {
				i++
			}
		case inQuotes && char == '\\' && i+1 < len(input):
			next := input[i+1]
			if next == quoteChar || next == '\\' {
				current.WriteByte(next)
				i++
			} else {
				current.WriteByte(char)
			}
		default:
			current.WriteByte(char)
		}
	}

	if current.Len() > 0 {
		arg := current.String()
		args = append(args, arg)
	}

	return args, nil
}

// GetShortcuts returns a map of shortcut names to descriptions
func (r *Registry) GetShortcuts() map[string]string {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	shortcuts := make(map[string]string)
	for name, shortcut := range r.shortcuts {
		shortcuts[name] = shortcut.GetDescription()
	}

	return shortcuts
}

// GetShortcutsStartingWith returns shortcuts that start with the given prefix
func (r *Registry) GetShortcutsStartingWith(prefix string) []string {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	var matches []string
	for name := range r.shortcuts {
		if strings.HasPrefix(name, prefix) {
			matches = append(matches, name)
		}
	}

	slices.Sort(matches)
	return matches
}
