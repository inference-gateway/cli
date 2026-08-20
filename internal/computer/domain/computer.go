// Package domain holds the computer-use bounded context's contracts: the
// application model and the mouse/keyboard result types its tools produce.
// It is pure - stdlib imports only.
package domain

// Application represents a running application on any platform.
// ID is a stable cross-platform identifier ("pid:N" on macOS and Linux, or
// the app name for name-based lookup).
// Name is the human-readable display name.
// PlatformID is the OS-native identifier (the PID on macOS and Linux, window ID on X11).
type Application struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	PlatformID string `json:"platform_id"`
}

// MouseMoveToolResult represents the result of a mouse move operation
type MouseMoveToolResult struct {
	FromX   int    `json:"from_x"`
	FromY   int    `json:"from_y"`
	ToX     int    `json:"to_x"`
	ToY     int    `json:"to_y"`
	Display string `json:"display"`
	Method  string `json:"method"`
}

// MouseClickToolResult represents the result of a mouse click operation
type MouseClickToolResult struct {
	Button  string `json:"button"`
	Clicks  int    `json:"clicks"`
	X       int    `json:"x"`
	Y       int    `json:"y"`
	Display string `json:"display"`
	Method  string `json:"method"`
}

// KeyboardTypeToolResult represents the result of a keyboard input operation
type KeyboardTypeToolResult struct {
	Text     string `json:"text,omitempty"`
	KeyCombo string `json:"key_combo,omitempty"`
	Display  string `json:"display"`
	Method   string `json:"method"`
}
