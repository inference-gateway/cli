package x11

import (
	"context"
	"image"
	"os"

	display "github.com/inference-gateway/cli/internal/display"
)

// Controller wraps the existing X11Client to implement the display.DisplayController interface
type Controller struct {
	client *X11Client
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
	if region == nil {
		return c.client.CaptureScreen(0, 0, 0, 0)
	}
	return c.client.CaptureScreen(region.X, region.Y, region.Width, region.Height)
}

// GetScreenDimensions returns the screen width and height
func (c *Controller) GetScreenDimensions(ctx context.Context) (width, height int, err error) {
	w, h := c.client.GetScreenDimensions()
	return w, h, nil
}

// GetCursorPosition returns the current cursor position
func (c *Controller) GetCursorPosition(ctx context.Context) (x, y int, err error) {
	return c.client.GetCursorPosition()
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

// SendKeyCombo sends a key combination (e.g., "ctrl+c", "super+l")
func (c *Controller) SendKeyCombo(ctx context.Context, combo string) error {
	return c.client.SendKeyCombo(combo)
}

// Close closes the X11 connection
func (c *Controller) Close() error {
	c.client.Close()
	return nil
}

// Provider implements the display.Provider interface for X11
type Provider struct{}

var _ display.Provider = (*Provider)(nil)

// NewProvider creates a new X11 provider
func NewProvider() *Provider {
	return &Provider{}
}

// GetController creates a new DisplayController (auto-detects display from $DISPLAY env var)
func (p *Provider) GetController() (display.DisplayController, error) {
	// Detect display from environment
	displayName := os.Getenv("DISPLAY")
	if displayName == "" {
		displayName = ":0" // Fallback to default
	}

	client, err := NewX11Client(displayName)
	if err != nil {
		return nil, err
	}
	return &Controller{client: client}, nil
}

// GetDisplayInfo returns information about the X11 platform
func (p *Provider) GetDisplayInfo() display.DisplayInfo {
	return display.DisplayInfo{
		Name:              "x11",
		SupportsRegions:   true,
		SupportsMouse:     true,
		SupportsKeyboard:  true,
		MaxTextLength:     0,
		RequiresElevation: false,
	}
}

// IsAvailable returns true if X11 is available on the current system
func (p *Provider) IsAvailable() bool {
	return os.Getenv("DISPLAY") != "" && os.Getenv("WAYLAND_DISPLAY") == ""
}

// Register the X11 provider in the global registry
func init() {
	display.Register(NewProvider())
}
