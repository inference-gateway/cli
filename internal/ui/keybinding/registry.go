package keybinding

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// Registry implements the KeyRegistry interface
type Registry struct {
	actions map[string]*KeyAction
	keyMap  map[string]*KeyAction
	layers  []*KeyLayer
	mutex   sync.RWMutex
}

// NewRegistry creates a new key binding registry
func NewRegistry(cfg *config.Config) *Registry {
	registry := &Registry{
		actions: make(map[string]*KeyAction),
		keyMap:  make(map[string]*KeyAction),
		layers:  make([]*KeyLayer, 0),
	}

	registry.initializeLayers()
	registry.registerDefaultBindings()

	if cfg != nil && cfg.Chat.Keybindings.Enabled {
		if err := registry.ApplyConfigOverrides(cfg.Chat.Keybindings); err != nil {
			logger.Warn("Failed to apply keybinding overrides: %v", err)
		}
	}

	return registry
}

// Register adds a new key binding action to the registry
// Multiple actions can share the same key - the layer system resolves conflicts based on context
func (r *Registry) Register(action *KeyAction) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if action.ID == "" {
		return fmt.Errorf("key action ID cannot be empty")
	}

	if len(action.Keys) == 0 {
		return fmt.Errorf("key action must have at least one key")
	}

	for _, key := range action.Keys {
		keyCount := strings.Count(key, ",") + 1
		if keyCount > 2 {
			return fmt.Errorf("key sequence '%s' exceeds maximum length of 2 keys (has %d keys)", key, keyCount)
		}
	}

	r.actions[action.ID] = action

	for _, key := range action.Keys {
		r.keyMap[key] = action
	}

	r.addActionToAppropriateLayer(action)

	return nil
}

// Unregister removes a key binding action from the registry
func (r *Registry) Unregister(id string) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	action, exists := r.actions[id]
	if !exists {
		return fmt.Errorf("action '%s' not found", id)
	}

	for _, key := range action.Keys {
		delete(r.keyMap, key)
	}

	delete(r.actions, id)

	return nil
}

// Resolve finds the appropriate key action for a key press
func (r *Registry) Resolve(key string, app KeyHandlerContext) *KeyAction {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	activeLayers := r.getActiveLayers(app)

	for _, layer := range activeLayers {
		if action, exists := layer.Bindings[key]; exists {
			if r.canExecuteAction(action, app) {
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
	seen := make(map[string]bool)

	for _, layer := range r.getActiveLayers(app) {
		for _, action := range layer.Bindings {
			if !seen[action.ID] && r.canExecuteAction(action, app) {
				actions = append(actions, action)
				seen[action.ID] = true
			}
		}
	}

	sort.Slice(actions, func(i, j int) bool {
		if actions[i].Category == actions[j].Category {
			return actions[i].Description < actions[j].Description
		}
		return actions[i].Category < actions[j].Category
	})

	return actions
}

// GetHelpShortcuts generates help shortcuts for current context
func (r *Registry) GetHelpShortcuts(app KeyHandlerContext) []HelpShortcut {
	actions := r.GetActiveActions(app)

	shortcuts := make([]HelpShortcut, 0, len(actions))
	for _, action := range actions {
		if len(action.Keys) == 0 {
			continue
		}

		sortedKeys := make([]string, len(action.Keys))
		copy(sortedKeys, action.Keys)
		sort.Strings(sortedKeys)

		shortcuts = append(shortcuts, HelpShortcut{
			Key:         sortedKeys[0],
			Description: action.Description,
			Category:    action.Category,
		})
	}

	return shortcuts
}

// AddLayer adds a new key layer to the registry
func (r *Registry) AddLayer(layer *KeyLayer) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.layers = append(r.layers, layer)

	sort.Slice(r.layers, func(i, j int) bool {
		return r.layers[i].Priority > r.layers[j].Priority
	})
}

// GetLayers returns all registered layers
func (r *Registry) GetLayers() []*KeyLayer {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	layers := make([]*KeyLayer, len(r.layers))
	copy(layers, r.layers)
	return layers
}

// getActiveLayers returns layers that are currently active
func (r *Registry) getActiveLayers(app KeyHandlerContext) []*KeyLayer {
	var activeLayers []*KeyLayer

	for _, layer := range r.layers {
		if layer.Matcher == nil || layer.Matcher(app) {
			activeLayers = append(activeLayers, layer)
		}
	}

	return activeLayers
}

// canExecuteAction checks if an action can be executed in current context
func (r *Registry) canExecuteAction(action *KeyAction, app KeyHandlerContext) bool {
	if !action.Enabled {
		return false
	}

	if len(action.Context.Views) > 0 {
		currentView := app.GetStateManager().GetCurrentView()
		found := false
		for _, view := range action.Context.Views {
			if view == currentView {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if len(action.Context.ExcludeViews) > 0 {
		currentView := app.GetStateManager().GetCurrentView()
		for _, view := range action.Context.ExcludeViews {
			if view == currentView {
				return false
			}
		}
	}

	for _, condition := range action.Context.Conditions {
		if !condition.Check(app) {
			return false
		}
	}

	return true
}

// initializeLayers sets up the default key binding layers
func (r *Registry) initializeLayers() {
	r.AddLayer(&KeyLayer{
		Name:     "component",
		Priority: 300,
		Bindings: make(map[string]*KeyAction),
		Matcher: func(app KeyHandlerContext) bool {
			return false
		},
	})

	r.AddLayer(&KeyLayer{
		Name:     "file_selection",
		Priority: 200,
		Bindings: make(map[string]*KeyAction),
		Matcher: func(app KeyHandlerContext) bool {
			return app.GetStateManager().GetCurrentView() == domain.ViewStateFileSelection
		},
	})

	r.AddLayer(&KeyLayer{
		Name:     "chat_view",
		Priority: 150,
		Bindings: make(map[string]*KeyAction),
		Matcher: func(app KeyHandlerContext) bool {
			return app.GetStateManager().GetCurrentView() == domain.ViewStateChat
		},
	})

	r.AddLayer(&KeyLayer{
		Name:     "model_selection",
		Priority: 150,
		Bindings: make(map[string]*KeyAction),
		Matcher: func(app KeyHandlerContext) bool {
			return app.GetStateManager().GetCurrentView() == domain.ViewStateModelSelection
		},
	})

	r.AddLayer(&KeyLayer{
		Name:     "plan_approval_view",
		Priority: 150,
		Bindings: make(map[string]*KeyAction),
		Matcher: func(app KeyHandlerContext) bool {
			return app.GetStateManager().GetCurrentView() == domain.ViewStatePlanApproval
		},
	})

	r.AddLayer(&KeyLayer{
		Name:     "global",
		Priority: 100,
		Bindings: make(map[string]*KeyAction),
		Matcher: func(app KeyHandlerContext) bool {
			return true
		},
	})
}

// addActionToLayer adds an action to a specific layer
func (r *Registry) addActionToLayer(layerName string, action *KeyAction) error {
	for _, layer := range r.layers {
		if layer.Name == layerName {
			for _, key := range action.Keys {
				layer.Bindings[key] = action
			}
			return nil
		}
	}
	return fmt.Errorf("layer '%s' not found", layerName)
}

// addActionToAppropriateLayer adds an action to the most appropriate layer based on its context
func (r *Registry) addActionToAppropriateLayer(action *KeyAction) {
	var targetLayer string

	if len(action.Context.Views) == 0 {
		targetLayer = "global"
	} else {
		targetLayer = r.determineTargetLayer(action.Context.Views)
	}

	if targetLayer == "" {
		targetLayer = "global"
	}

	_ = r.addActionToLayer(targetLayer, action)
}

// determineTargetLayer determines the appropriate layer for the given views
func (r *Registry) determineTargetLayer(views []domain.ViewState) string {
	for _, view := range views {
		switch view {
		case domain.ViewStateFileSelection:
			return "file_selection"
		case domain.ViewStateModelSelection:
			return "model_selection"
		case domain.ViewStateConversationSelection:
			return "conversation_selection"
		case domain.ViewStateChat:
			return "chat_view"
		default:
			return "global"
		}
	}
	return "global"
}

// ApplyConfigOverrides applies keybinding configuration from config
// Only processes config bindings. Missing entries automatically use defaults already registered.
func (r *Registry) ApplyConfigOverrides(cfg config.KeybindingsConfig) error {
	if !cfg.Enabled {
		logger.Debug("Keybindings disabled in config, using defaults only")
		return nil
	}

	r.mutex.Lock()
	defer r.mutex.Unlock()

	for actionID, configBinding := range cfg.Bindings {
		action, exists := r.actions[actionID]
		if !exists {
			logger.Warn("Unknown keybinding action '%s' in config, ignoring", actionID)
			continue
		}

		if configBinding.Enabled != nil {
			action.Enabled = *configBinding.Enabled
		}

		if len(configBinding.Keys) > 0 {
			r.updateActionKeysUnsafe(action, configBinding.Keys)
		}
	}

	return nil
}

// updateActionKeysUnsafe updates the keys for an action without validation
// Used at runtime to apply config without blocking on conflicts
func (r *Registry) updateActionKeysUnsafe(action *KeyAction, newKeys []string) {
	for _, oldKey := range action.Keys {
		delete(r.keyMap, oldKey)
		r.removeKeyFromLayers(oldKey, action)
	}

	action.Keys = newKeys

	for _, newKey := range newKeys {
		r.keyMap[newKey] = action
	}

	r.addActionToAppropriateLayer(action)
}

// removeKeyFromLayers removes a key from all layers
func (r *Registry) removeKeyFromLayers(key string, action *KeyAction) {
	for _, layer := range r.layers {
		if layerAction, exists := layer.Bindings[key]; exists && layerAction.ID == action.ID {
			delete(layer.Bindings, key)
		}
	}
}

// HasSequenceWithPrefix checks if any registered action has a key sequence that starts with the given prefix
func (r *Registry) HasSequenceWithPrefix(prefix string, app KeyHandlerContext) bool {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	for _, action := range r.actions {
		if !r.canExecuteAction(action, app) {
			continue
		}

		for _, key := range action.Keys {
			if !strings.Contains(key, ",") {
				continue
			}

			if strings.HasPrefix(key, prefix+",") || key == prefix {
				return true
			}
		}
	}

	return false
}

// GetSequenceActionForPrefix returns the action that matches a sequence starting with the given prefix
func (r *Registry) GetSequenceActionForPrefix(prefix string, app KeyHandlerContext) *KeyAction {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	for _, action := range r.actions {
		if !r.canExecuteAction(action, app) {
			continue
		}

		for _, key := range action.Keys {
			if !strings.Contains(key, ",") {
				continue
			}

			if strings.HasPrefix(key, prefix+",") || key == prefix {
				return action
			}
		}
	}

	return nil
}

// ListAllActions returns all registered actions for debugging/management
func (r *Registry) ListAllActions() []*KeyAction {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	actions := make([]*KeyAction, 0, len(r.actions))
	for _, action := range r.actions {
		actions = append(actions, action)
	}

	sort.Slice(actions, func(i, j int) bool {
		if actions[i].Category == actions[j].Category {
			return actions[i].ID < actions[j].ID
		}
		return actions[i].Category < actions[j].Category
	})

	return actions
}
