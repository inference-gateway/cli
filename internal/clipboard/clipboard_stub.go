//go:build !darwin || test

package clipboard

import (
	"fmt"
)

// Init initializes the clipboard (stub implementation)
func Init() error {
	return nil
}

// Read reads data from clipboard in the specified format (stub implementation)
func Read(format Format) []byte {
	// Stub - return empty data
	return []byte{}
}

// Write writes data to clipboard in the specified format (stub implementation)
func Write(format Format, data []byte) {
	fmt.Println("Clipboard write not supported in this build configuration")
}

// Format represents clipboard data format
type Format int

const (
	// FmtText is the text format
	FmtText Format = 0
	// FmtImage is the image format
	FmtImage Format = 1
)
