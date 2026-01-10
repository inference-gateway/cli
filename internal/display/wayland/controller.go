package wayland

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"os"

	display "github.com/inference-gateway/cli/internal/display"
)

// Controller wraps the existing WaylandClient to implement the display.DisplayController interface
type Controller struct {
	client *WaylandClient
}

var _ display.DisplayController = (*Controller)(nil)

// CaptureScreenBytes captures a screenshot and returns PNG bytes
func (c *Controller) CaptureScreenBytes(ctx context.Context, region *display.Region) ([]byte, error) {
	if region == nil {
		return c.client.CaptureScreenBytes(0, 0, 0, 0)
	}
	return c.client.CaptureScreenBytes(region.X, region.Y, region.Width, region.Height)
}

// CaptureScreen captures a screenshot and returns an image.Image
func (c *Controller) CaptureScreen(ctx context.Context, region *display.Region) (image.Image, error) {
	var imgBytes []byte
	var err error

	if region == nil {
		imgBytes, err = c.client.CaptureScreenBytes(0, 0, 0, 0)
	} else {
		imgBytes, err = c.client.CaptureScreenBytes(region.X, region.Y, region.Width, region.Height)
	}

	if err != nil {
		return nil, err
	}

	img, err := png.Decode(bytes.NewReader(imgBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to decode screenshot: %w", err)
	}

	return img, nil
}

// GetScreenDimensions returns the screen width and height
func (c *Controller) GetScreenDimensions(ctx context.Context) (width, height int, err error) {
	return c.client.GetScreenDimensions()
}

// GetCursorPosition returns the current cursor position
// Note: Wayland doesn't provide a standard way to get cursor position
func (c *Controller) GetCursorPosition(ctx context.Context) (x, y int, err error) {
	// Wayland doesn't expose cursor position for security reasons
	// Return an error indicating this is not supported
	return 0, 0, fmt.Errorf("getting cursor position is not supported on Wayland")
}

// MoveMouse moves the cursor to the specified coordinates
func (c *Controller) MoveMouse(ctx context.Context, x, y int) error {
	return c.client.MoveMouse(x, y)
}

// ClickMouse clicks the specified mouse button
func (c *Controller) ClickMouse(ctx context.Context, button display.MouseButton, clicks int) error {
	return c.client.ClickMouse(button.String(), clicks)
}

// ScrollMouse scrolls the mouse wheel
func (c *Controller) ScrollMouse(ctx context.Context, clicks int, direction string) error {
	return c.client.ScrollMouse(clicks, direction)
}

// TypeText types the given text with the specified delay between keystrokes
func (c *Controller) TypeText(ctx context.Context, text string, delayMs int) error {
	return c.client.TypeText(text, delayMs)
}

// SendKeyCombo sends a key combination (e.g., "ctrl+c")
func (c *Controller) SendKeyCombo(ctx context.Context, combo string) error {
	return c.client.SendKeyCombo(combo)
}

// Close closes the Wayland client
func (c *Controller) Close() error {
	c.client.Close()
	return nil
}

// Provider implements the display.Provider interface for Wayland
type Provider struct{}

var _ display.Provider = (*Provider)(nil)

// NewProvider creates a new Wayland provider
func NewProvider() *Provider {
	return &Provider{}
}

// GetController creates a new DisplayController (auto-detects display from $WAYLAND_DISPLAY env var)
func (p *Provider) GetController() (display.DisplayController, error) {
	displayName := os.Getenv("WAYLAND_DISPLAY")

	client, err := NewWaylandClient(displayName)
	if err != nil {
		return nil, err
	}
	return &Controller{client: client}, nil
}

// GetDisplayInfo returns information about the Wayland platform
func (p *Provider) GetDisplayInfo() display.DisplayInfo {
	return display.DisplayInfo{
		Name:              "wayland",
		SupportsRegions:   true,
		SupportsMouse:     true,
		SupportsKeyboard:  true,
		MaxTextLength:     0,
		RequiresElevation: false,
	}
}

// IsAvailable returns true if Wayland is available on the current system
func (p *Provider) IsAvailable() bool {
	return os.Getenv("WAYLAND_DISPLAY") != ""
}

// Register the Wayland provider in the global registry
// Note: init() runs before X11's init() due to alphabetical ordering of package names
func init() {
	display.Register(NewProvider())
}
