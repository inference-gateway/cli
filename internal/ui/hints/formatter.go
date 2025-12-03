package hints

// KeyAction represents a minimal key action interface for hint formatting
type KeyAction interface {
	GetKeys() []string
	IsEnabled() bool
}

// KeyRegistry provides access to registered key actions
type KeyRegistry interface {
	GetAction(id string) KeyAction
}

// Formatter provides methods to format keybinding hints for UI display
type Formatter struct {
	registry KeyRegistry
}

// NewFormatter creates a new key hint formatter with a registry
func NewFormatter(registry KeyRegistry) *Formatter {
	return &Formatter{
		registry: registry,
	}
}

// GetKeyHint returns a formatted hint string for a given action ID
// Format: "Press <key> to <action>"
// Example: "Press ctrl+o to expand" or "Press esc to cancel"
func (f *Formatter) GetKeyHint(actionID string, actionVerb string) string {
	if f.registry == nil {
		return ""
	}

	action := f.registry.GetAction(actionID)
	if action == nil || !action.IsEnabled() {
		return ""
	}

	keys := action.GetKeys()
	if len(keys) == 0 {
		return ""
	}

	return "Press " + keys[0] + " to " + actionVerb
}

// GetKeyOnly returns just the key binding without "Press" or action verb
// Useful for inline hints like "ctrl+o"
func (f *Formatter) GetKeyOnly(actionID string) string {
	if f.registry == nil {
		return ""
	}

	action := f.registry.GetAction(actionID)
	if action == nil || !action.IsEnabled() {
		return ""
	}

	keys := action.GetKeys()
	if len(keys) == 0 {
		return ""
	}

	return keys[0]
}
