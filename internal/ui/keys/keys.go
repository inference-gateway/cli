package keys

import (
	"slices"

	tea "charm.land/bubbletea/v2"
)

// InputHandlerKeys are keys that can be handled by text input components
var InputHandlerKeys = []string{
	"space", "tab", "enter", "alt+enter", "shift+enter", "backspace", "delete",
	"up", "down", "left", "right", "home", "end",
	"ctrl+a", "ctrl+e", "ctrl+u", "ctrl+k", "ctrl+w", "ctrl+l",
	"ctrl+z", "ctrl+y",
}

// AllKnownKeys is a comprehensive list of all known keyboard combinations
var AllKnownKeys = []string{
	// Basic navigation and editing
	"up", "down", "left", "right",
	"shift+up", "shift+down", "shift+left", "shift+right",
	"alt+left", "alt+right", "ctrl+left", "ctrl+right",
	"enter", "shift+enter", "backspace", "delete", "tab", "shift+tab", "space",
	"home", "end", "pgup", "pgdn", "pgdown", "page_up", "page_down",
	"esc", "escape",

	// Ctrl combinations
	"ctrl+a", "ctrl+b", "ctrl+c", "ctrl+d", "ctrl+e", "ctrl+f", "ctrl+g",
	"ctrl+h", "ctrl+i", "ctrl+j", "ctrl+k", "ctrl+l", "ctrl+m", "ctrl+n",
	"ctrl+o", "ctrl+p", "ctrl+q", "ctrl+r", "ctrl+s", "ctrl+t", "ctrl+u",
	"ctrl+v", "ctrl+w", "ctrl+x", "ctrl+y", "ctrl+z",
	"ctrl+enter", "ctrl+backspace", "ctrl+delete",

	// Ctrl+Shift combinations
	"ctrl+shift+a", "ctrl+shift+b", "ctrl+shift+c", "ctrl+shift+d",
	"ctrl+shift+e", "ctrl+shift+f", "ctrl+shift+g", "ctrl+shift+h",
	"ctrl+shift+i", "ctrl+shift+j", "ctrl+shift+k", "ctrl+shift+l",
	"ctrl+shift+m", "ctrl+shift+n", "ctrl+shift+o", "ctrl+shift+p",
	"ctrl+shift+q", "ctrl+shift+r", "ctrl+shift+s", "ctrl+shift+t",
	"ctrl+shift+u", "ctrl+shift+v", "ctrl+shift+w", "ctrl+shift+x",
	"ctrl+shift+y", "ctrl+shift+z",

	// Alt combinations
	"alt+a", "alt+b", "alt+c", "alt+d", "alt+e", "alt+f", "alt+g",
	"alt+h", "alt+i", "alt+j", "alt+k", "alt+l", "alt+m", "alt+n",
	"alt+o", "alt+p", "alt+q", "alt+r", "alt+s", "alt+t", "alt+u",
	"alt+v", "alt+w", "alt+x", "alt+y", "alt+z",
	"alt+enter", "alt+backspace", "alt+delete",

	// Function keys
	"f1", "f2", "f3", "f4", "f5", "f6", "f7", "f8", "f9", "f10", "f11", "f12",
}

// IsPrintableCharacter checks if a key string represents a single printable character
func IsPrintableCharacter(keyStr string) bool {
	return len(keyStr) == 1 && keyStr[0] >= ' ' && keyStr[0] <= '~'
}

// IsKnownKey checks if a key string represents a known keyboard key combination
// or a single printable character
func IsKnownKey(keyStr string) bool {
	return slices.Contains(AllKnownKeys, keyStr) || IsPrintableCharacter(keyStr)
}

// PrintableText returns the literal characters represented by a key press, or
// "" if the key has no printable text (Enter, Backspace, arrow keys, modifier
// combos, etc.). In Bubble Tea v2 the printable text lives in KeyPressMsg.Text;
// KeyMsg.String() returns the human-readable key *name* ("space", "ctrl+c"),
// which is what keybinding lookups want but is not what should be inserted
// into a text buffer. Use this helper when you need the literal input.
func PrintableText(key tea.KeyMsg) string {
	if press, ok := key.(tea.KeyPressMsg); ok {
		return press.Text
	}
	return ""
}

// CanInputHandle checks if a key can be handled by input components
func CanInputHandle(key tea.KeyMsg) bool {
	if PrintableText(key) != "" {
		return true
	}
	return slices.Contains(InputHandlerKeys, key.String())
}
