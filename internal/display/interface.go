package display

import (
	"context"
	"fmt"
	"image"

	domain "github.com/inference-gateway/cli/internal/domain"
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
	// GetController creates a new DisplayController (display is auto-detected from environment)
	GetController() (DisplayController, error)

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

// AppProvider is the cross-platform application focus management interface.
// Every platform (macOS, X11, Wayland) implements all three methods:
//   - ListRunning: enumerate running applications with stable IDs and names
//   - Activate: bring a running application to the foreground by its stable ID
//   - GetFocused: return the currently focused application
//
// Errors distinguish "application not found" from "OS refused the operation".
// Callers should check both cases when presenting errors.
type AppProvider interface {
	// ListRunning returns all running applications visible to the windowing system.
	// On macOS this includes every NSRunningApplication. On X11 this enumerates
	// top-level windows via _NET_CLIENT_LIST and deduplicates by PID.
	ListRunning(ctx context.Context) ([]domain.Application, error)

	// Activate brings a running application to the foreground by its stable ID
	// (as returned by ListRunning or GetFocused). Returns an error wrapping
	// ErrAppNotFound when no such application is running, or a generic error
	// when the OS refuses the activation.
	Activate(ctx context.Context, id string) error

	// GetFocused returns the currently focused (frontmost) application.
	// Returns nil with a nil error when no application is focused (headless
	// session with no windows), or an error wrapping ErrAppNotFound.
	GetFocused(ctx context.Context) (*domain.Application, error)

	// Close releases any underlying platform resources.
	Close() error
}

// ErrAppNotFound is returned by AppProvider methods when the requested
// application is not running. Callers should use errors.Is to distinguish
// "not found" from other errors.
var ErrAppNotFound = fmt.Errorf("application not found")

// ErrAppUnsupported is returned by AppProvider.ListRunning when the
// platform does not support application enumeration (e.g., Wayland
// without the foreign-toplevel protocol, or running headless without a
// display server). DetectAppProvider skips providers that return this.
var ErrAppUnsupported = fmt.Errorf("application provider not supported on this platform")
