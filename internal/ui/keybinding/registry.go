package keybinding

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"sync"

	key "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	config "github.com/inference-gateway/cli/config"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// Registry holds all key binding actions and resolves key presses against
// their bubbles/key Bindings. View-scoped actions win over global ones;
// within a scope, registration order breaks ties.
type Registry struct {
	actions map[string]*KeyAction
	ordered []*KeyAction
	mutex   sync.RWMutex
}

// NewRegistry creates a registry whose action Bindings are resolved from the
// keybindings config: built-in defaults merged with any keybindings.yaml
// overrides. Dispatch and help both read these same Bindings.
func NewRegistry(cfg *config.Config) *Registry {
	registry := &Registry{
		actions: make(map[string]*KeyAction),
	}

	var kbCfg config.KeybindingsConfig
	if cfg != nil {
		kbCfg = cfg.Chat.Keybindings
	}

	resolved, unknown := config.ResolveKeybindings(kbCfg)
	for _, id := range unknown {
		logger.Warn("unknown keybinding action in config, ignoring", "action", id)
	}

	for _, action := range defaultActions() {
		entry, ok := resolved[action.ID]
		if !ok {
			logger.Warn("keybinding action has no default config entry, skipping", "action", action.ID)
			continue
		}
		action.Category = entry.Category
		action.Binding = newBindingFromEntry(entry)
		if err := registry.Register(action); err != nil {
			logger.Warn("failed to register keybinding action", "action", action.ID, "error", err)
		}
	}

	return registry
}

// newBindingFromEntry builds the dispatch/help binding for a resolved config
// entry. Key tokens are normalized to the tea.KeyPressMsg.String() vocabulary
// for matching ("space" → " "), while help keeps the human-readable token.
func newBindingFromEntry(entry config.KeyBindingEntry) key.Binding {
	matchKeys := make([]string, len(entry.Keys))
	for i, k := range entry.Keys {
		if k == "space" {
			k = " "
		}
		matchKeys[i] = k
	}

	displayKey := ""
	if len(entry.Keys) > 0 {
		sorted := slices.Clone(entry.Keys)
		slices.Sort(sorted)
		displayKey = sorted[0]
	}

	b := key.NewBinding(key.WithKeys(matchKeys...), key.WithHelp(displayKey, entry.Description))
	if entry.Enabled != nil && !*entry.Enabled {
		b.SetEnabled(false)
	}
	return b
}

// Register adds a key binding action to the registry.
// Multiple actions can share the same key - view context resolves conflicts.
func (r *Registry) Register(action *KeyAction) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if action.ID == "" {
		return fmt.Errorf("key action ID cannot be empty")
	}

	if len(action.Binding.Keys()) == 0 {
		return fmt.Errorf("key action must have at least one key")
	}

	for _, k := range action.Binding.Keys() {
		keyCount := strings.Count(k, ",") + 1
		if keyCount > 2 {
			return fmt.Errorf("key sequence '%s' exceeds maximum length of 2 keys (has %d keys)", k, keyCount)
		}
	}

	if _, exists := r.actions[action.ID]; !exists {
		r.ordered = append(r.ordered, action)
	}
	r.actions[action.ID] = action

	return nil
}

// Resolve finds the action bound to a pressed key via key.Matches.
// View-scoped actions are consulted before global ones, mirroring the old
// layer priorities.
func (r *Registry) Resolve(msg tea.KeyPressMsg, app KeyHandlerContext) *KeyAction {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	for _, viewScoped := range []bool{true, false} {
		for _, action := range r.ordered {
			if (len(action.Context.Views) > 0) != viewScoped {
				continue
			}
			if key.Matches(msg, action.Binding) && r.canExecuteAction(action, app) {
				return action
			}
		}
	}

	return nil
}

// ResolveKey resolves a raw key string, used for comma-joined sequences
// ("esc,esc") that never arrive as a single tea.KeyPressMsg — bubbles/key has
// no chord support, so sequences stay string-matched.
func (r *Registry) ResolveKey(keyStr string, app KeyHandlerContext) *KeyAction {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	for _, viewScoped := range []bool{true, false} {
		for _, action := range r.ordered {
			if (len(action.Context.Views) > 0) != viewScoped {
				continue
			}
			if action.Binding.Enabled() && slices.Contains(action.Binding.Keys(), keyStr) && r.canExecuteAction(action, app) {
				return action
			}
		}
	}

	return nil
}

// GetAction retrieves an action by ID
func (r *Registry) GetAction(id string) *KeyAction {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	return r.actions[id]
}

// GetActiveActions returns all currently active actions
func (r *Registry) GetActiveActions(app KeyHandlerContext) []*KeyAction {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	var actions []*KeyAction
	for _, action := range r.ordered {
		if action.Binding.Enabled() && r.canExecuteAction(action, app) {
			actions = append(actions, action)
		}
	}

	slices.SortFunc(actions, func(a, b *KeyAction) int {
		if c := cmp.Compare(a.Category, b.Category); c != 0 {
			return c
		}
		return cmp.Compare(a.Binding.Help().Desc, b.Binding.Help().Desc)
	})

	return actions
}

// GetHelpShortcuts generates help shortcuts for current context
func (r *Registry) GetHelpShortcuts(app KeyHandlerContext) []HelpShortcut {
	actions := r.GetActiveActions(app)

	shortcuts := make([]HelpShortcut, 0, len(actions))
	for _, action := range actions {
		help := action.Binding.Help()
		if help.Key == "" {
			continue
		}

		shortcuts = append(shortcuts, HelpShortcut{
			Key:         help.Key,
			Description: help.Desc,
			Category:    action.Category,
		})
	}

	return shortcuts
}

// canExecuteAction checks if an action can be executed in current context
func (r *Registry) canExecuteAction(action *KeyAction, app KeyHandlerContext) bool {
	if len(action.Context.Views) > 0 {
		currentView := app.GetStateManager().GetCurrentView()
		if !slices.Contains(action.Context.Views, currentView) {
			return false
		}
	}

	if len(action.Context.ExcludeViews) > 0 {
		currentView := app.GetStateManager().GetCurrentView()
		if slices.Contains(action.Context.ExcludeViews, currentView) {
			return false
		}
	}

	for _, condition := range action.Context.Conditions {
		if !condition.Check(app) {
			return false
		}
	}

	return true
}

// HasSequenceWithPrefix checks if any registered action has a key sequence that starts with the given prefix
func (r *Registry) HasSequenceWithPrefix(prefix string, app KeyHandlerContext) bool {
	return r.GetSequenceActionForPrefix(prefix, app) != nil
}

// GetSequenceActionForPrefix returns the action that matches a sequence starting with the given prefix
func (r *Registry) GetSequenceActionForPrefix(prefix string, app KeyHandlerContext) *KeyAction {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	for _, action := range r.ordered {
		if !action.Binding.Enabled() || !r.canExecuteAction(action, app) {
			continue
		}

		for _, k := range action.Binding.Keys() {
			if !strings.Contains(k, ",") {
				continue
			}

			if strings.HasPrefix(k, prefix+",") || k == prefix {
				return action
			}
		}
	}

	return nil
}

// KnownActionIDs returns every action ID the application recognises: the runtime
// registry actions unioned with the default keybindings config IDs (which include
// the namespace-path actions consumed directly via config.ResolveNamespaceBindings).
// The CLI validator shares this notion of "valid" so they agree on what is a
// genuine typo.
func (r *Registry) KnownActionIDs() []string {
	ids := make(map[string]struct{})

	r.mutex.RLock()
	for id := range r.actions {
		ids[id] = struct{}{}
	}
	r.mutex.RUnlock()

	for id := range config.DefaultKeybindingActionIDs() {
		ids[id] = struct{}{}
	}

	out := make([]string, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

// ListAllActions returns all registered actions for debugging/management
func (r *Registry) ListAllActions() []*KeyAction {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	actions := slices.Clone(r.ordered)

	slices.SortFunc(actions, func(a, b *KeyAction) int {
		if c := cmp.Compare(a.Category, b.Category); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	})

	return actions
}
