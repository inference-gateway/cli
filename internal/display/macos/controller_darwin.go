//go:build darwin

package macos

import (
	"context"
	"fmt"
	"image"
	"runtime"

	display "github.com/inference-gateway/cli/internal/display"
)

// Controller implements display.DisplayController for macOS
// This is a placeholder for future CGO implementation using:
// - CGDisplayCreateImage for screenshots
// - CGEventPost for mouse/keyboard control
// - Accessibility API for permissions
type Controller struct{}

var _ display.DisplayController = (*Controller)(nil)

func (c *Controller) CaptureScreenBytes(ctx context.Context, region *display.Region) ([]byte, error) {
	// TODO: Implement using CGDisplayCreateImage + CGO
	// Sample code structure:
	// /*
	// #cgo LDFLAGS: -framework CoreGraphics -framework CoreFoundation
	// #include <CoreGraphics/CoreGraphics.h>
	// CGImageRef CGDisplayCreateImage(CGDirectDisplayID displayID);
	// */
	// import "C"
	return nil, fmt.Errorf("macOS screenshot not yet implemented (requires CGO)")
}

func (c *Controller) CaptureScreen(ctx context.Context, region *display.Region) (image.Image, error) {
	return nil, fmt.Errorf("macOS screenshot not yet implemented (requires CGO)")
}

func (c *Controller) GetScreenDimensions(ctx context.Context) (width, height int, err error) {
	// TODO: Implement using CGDisplayBounds
	return 0, 0, fmt.Errorf("macOS screen dimensions not yet implemented (requires CGO)")
}

func (c *Controller) GetCursorPosition(ctx context.Context) (x, y int, err error) {
	// TODO: Implement using CGEventGetLocation
	return 0, 0, fmt.Errorf("macOS cursor position not yet implemented (requires CGO)")
}

func (c *Controller) MoveMouse(ctx context.Context, x, y int) error {
	// TODO: Implement using CGEventCreateMouseEvent + CGEventPost
	return fmt.Errorf("macOS mouse move not yet implemented (requires CGO)")
}

func (c *Controller) ClickMouse(ctx context.Context, button display.MouseButton, clicks int) error {
	// TODO: Implement using CGEventCreateMouseEvent + CGEventPost
	return fmt.Errorf("macOS mouse click not yet implemented (requires CGO)")
}

func (c *Controller) TypeText(ctx context.Context, text string, delayMs int) error {
	// TODO: Implement using CGEventCreateKeyboardEvent + CGEventPost
	return fmt.Errorf("macOS keyboard type not yet implemented (requires CGO)")
}

func (c *Controller) SendKeyCombo(ctx context.Context, combo string) error {
	// TODO: Implement using CGEventCreateKeyboardEvent with modifiers
	return fmt.Errorf("macOS key combo not yet implemented (requires CGO)")
}

func (c *Controller) Close() error {
	return nil
}

// Provider implements the display.Provider interface for macOS
type Provider struct{}

var _ display.Provider = (*Provider)(nil)

func NewProvider() *Provider {
	return &Provider{}
}

func (p *Provider) GetController(display string) (display.DisplayController, error) {
	// TODO: Check Accessibility permissions
	// Sample code:
	// AXIsProcessTrustedWithOptions()
	return nil, fmt.Errorf("macOS provider not yet implemented (requires CGO)")
}

func (p *Provider) GetDisplayInfo() display.DisplayInfo {
	return display.DisplayInfo{
		Name:              "macos",
		SupportsRegions:   true,
		SupportsMouse:     true,
		SupportsKeyboard:  true,
		MaxTextLength:     0,
		RequiresElevation: true,
	}
}

func (p *Provider) IsAvailable() bool {
	// Only available on macOS (darwin)
	// TODO: Also check Accessibility permissions when implemented
	return runtime.GOOS == "darwin"
}

// Register the macOS provider in the global registry (darwin only)
func init() {
	// TODO: Uncomment when implementation is ready
	// display.Register(NewProvider())

	// For now, don't register to avoid false positives
	// The stub implementation will prevent compilation errors
}
