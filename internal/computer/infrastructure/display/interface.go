package display

import (
	"context"
	"image"
)

// DisplayController abstracts the desktop input and screen-capture operations.
type DisplayController interface {
	// Screen operations
	CaptureScreenBytes(ctx context.Context, region *Region) ([]byte, error)
	CaptureScreen(ctx context.Context, region *Region) (image.Image, error)
	GetScreenDimensions(ctx context.Context) (width, height int, err error)

	// Mouse operations
	GetCursorPosition(ctx context.Context) (x, y int, err error)
	MoveMouse(ctx context.Context, x, y int) error
	ClickMouse(ctx context.Context, button MouseButton, clicks int) error
	ScrollMouse(ctx context.Context, clicks int, direction string) error

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
	case "middle":
		return MouseButtonMiddle
	case "right":
		return MouseButtonRight
	default:
		return MouseButtonLeft
	}
}

// Provider creates DisplayController instances.
type Provider interface {
	GetController() (DisplayController, error)
	GetDisplayInfo() DisplayInfo
	IsAvailable() bool
}

// DisplayInfo contains metadata about the display backend
type DisplayInfo struct {
	Name              string
	SupportsRegions   bool
	SupportsMouse     bool
	SupportsKeyboard  bool
	MaxTextLength     int
	RequiresElevation bool
}
