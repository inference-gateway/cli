package keybinding

import keys "github.com/inference-gateway/cli/internal/ui/keys"

// IsValidKey checks if a key string is a known/valid key
// This is exposed for use in validation commands
func IsValidKey(key string) bool {
	return keys.IsKnownKey(key)
}
