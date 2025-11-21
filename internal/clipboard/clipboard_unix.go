//go:build darwin && !test

package clipboard

import (
	xclipboard "golang.design/x/clipboard"
)

// Init initializes the clipboard
func Init() error {
	return xclipboard.Init()
}

// Read reads data from clipboard in the specified format
func Read(format Format) []byte {
	return xclipboard.Read(xclipboard.Format(format))
}

// Write writes data to clipboard in the specified format
func Write(format Format, data []byte) {
	xclipboard.Write(xclipboard.Format(format), data)
}

// Format represents clipboard data format
type Format int

const (
	// FmtText is the text format
	FmtText Format = Format(xclipboard.FmtText)
	// FmtImage is the image format
	FmtImage Format = Format(xclipboard.FmtImage)
)
