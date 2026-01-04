//go:build !darwin

package macos

import "fmt"

// OverlayWindow is a stub for non-macOS platforms
type OverlayWindow struct{}

// NewOverlayWindow returns an error on non-macOS platforms
func NewOverlayWindow() (*OverlayWindow, error) {
	return nil, fmt.Errorf("overlay window only supported on macOS")
}

// Show is a no-op on non-macOS platforms
func (w *OverlayWindow) Show() error {
	return fmt.Errorf("overlay window only supported on macOS")
}

// Hide is a no-op on non-macOS platforms
func (w *OverlayWindow) Hide() error {
	return fmt.Errorf("overlay window only supported on macOS")
}

// IsVisible always returns false on non-macOS platforms
func (w *OverlayWindow) IsVisible() bool {
	return false
}

// Destroy is a no-op on non-macOS platforms
func (w *OverlayWindow) Destroy() error {
	return fmt.Errorf("overlay window only supported on macOS")
}
