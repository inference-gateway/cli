package keybinding

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/inference-gateway/cli/internal/domain"
)

// Registry implements the KeyRegistry interface
type Registry struct {
	actions map[string]*KeyAction
	keyMap  map[string]*KeyAction
	layers  []*KeyLayer
	mutex   sync.RWMutex
}

// NewRegistry creates a new key binding registry
func NewRegistry() *Registry {
	registry := &Registry{
		actions: make(map[string]*KeyAction),
		keyMap:  make(map[string]*KeyAction),
		layers:  make([]*KeyLayer, 0),
	}

	registry.initializeLayers()
	registry.registerDefaultBindings()

	return registry
}

// Register adds a new key binding action to the registry
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
		if existing, exists := r.keyMap[key]; exists {
			return fmt.Errorf("key '%s' already bound to action '%s'", key, existing.ID)
		}
	}

	r.actions[action.ID] = action

	for _, key := range action.Keys {
		r.keyMap[key] = action
	}

	// Automatically add to appropriate layer based on context
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
		shortcuts = append(shortcuts, HelpShortcut{
			Key:         strings.Join(action.Keys, "/"),
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
		Name:     "approval",
		Priority: 250,
		Bindings: make(map[string]*KeyAction),
		Matcher: func(app KeyHandlerContext) bool {
			return app.HasPendingApproval()
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
		case domain.ViewStateToolApproval:
			return "approval"
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
