package display

import (
	"context"
	"image"
)

// DisplayController abstracts display server-specific operations (X11, Wayland, macOS Quartz)
type DisplayController interface {
	// Screen operations
	CaptureScreenBytes(ctx context.Context, region *Region) ([]byte, error)
	CaptureScreen(ctx context.Context, region *Region) (image.Image, error)
	GetScreenDimensions(ctx context.Context) (width, height int, err error)

	// Mouse operations
	GetCursorPosition(ctx context.Context) (x, y int, err error)
	MoveMouse(ctx context.Context, x, y int) error
	ClickMouse(ctx context.Context, button MouseButton, clicks int) error

	// Keyboard operations
	TypeText(ctx context.Context, text string, delayMs int) error
	SendKeyCombo(ctx context.Context, combo string) error

	// Lifecycle
	Close() error
}

// Region represents a rectangular area on the screen
type Region struct {
	X      int
	Y      int
	Width  int
	Height int
}

// MouseButton represents a mouse button
type MouseButton int

const (
	MouseButtonLeft MouseButton = iota
	MouseButtonMiddle
	MouseButtonRight
)

// String returns the string representation of a mouse button
func (b MouseButton) String() string {
	switch b {
	case MouseButtonLeft:
		return "left"
	case MouseButtonMiddle:
		return "middle"
	case MouseButtonRight:
		return "right"
	default:
		return "unknown"
	}
}

// ParseMouseButton parses a string into a MouseButton
func ParseMouseButton(s string) MouseButton {
	switch s {
	case "left":
		return MouseButtonLeft
	case "middle":
		return MouseButtonMiddle
	case "right":
		return MouseButtonRight
	default:
		return MouseButtonLeft
	}
}

// Provider creates DisplayController instances for a specific display server/protocol
type Provider interface {
	// GetController creates a new DisplayController for the specified display
	GetController(display string) (DisplayController, error)

	// GetDisplayInfo returns information about the display server/protocol
	GetDisplayInfo() DisplayInfo

	// IsAvailable returns true if this display server is available on the current system
	IsAvailable() bool
}

// DisplayInfo contains metadata about a display server or protocol
type DisplayInfo struct {
	Name              string // "x11", "wayland", "macos"
	SupportsRegions   bool
	SupportsMouse     bool
	SupportsKeyboard  bool
	MaxTextLength     int
	RequiresElevation bool
}
