package utils

import "os"

// FileExists reports whether a path exists on disk, whatever its type.
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
