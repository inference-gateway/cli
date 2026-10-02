package utils

import (
	"os"
	"path/filepath"
	"strings"
)

// ExpandHome expands a leading ~ or ~/ to the user's home directory. Other
// tilde forms such as ~user cannot be resolved reliably and return unchanged,
// as does the whole input when the home directory is unavailable.
func ExpandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	return path
}
