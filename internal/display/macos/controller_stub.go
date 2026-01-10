//go:build !darwin

package macos

import (
	"context"
	"fmt"
	"image"

	display "github.com/inference-gateway/cli/internal/display"
)

// Controller is a stub implementation for non-macOS platforms
type Controller struct{}

var _ display.DisplayController = (*Controller)(nil)

func (c *Controller) CaptureScreenBytes(ctx context.Context, region *display.Region) ([]byte, error) {
	return nil, fmt.Errorf("macOS platform not available on this system")
}

func (c *Controller) CaptureScreen(ctx context.Context, region *display.Region) (image.Image, error) {
	return nil, fmt.Errorf("macOS platform not available on this system")
}

func (c *Controller) GetScreenDimensions(ctx context.Context) (width, height int, err error) {
	return 0, 0, fmt.Errorf("macOS platform not available on this system")
}

func (c *Controller) GetCursorPosition(ctx context.Context) (x, y int, err error) {
	return 0, 0, fmt.Errorf("macOS platform not available on this system")
}

func (c *Controller) MoveMouse(ctx context.Context, x, y int) error {
	return fmt.Errorf("macOS platform not available on this system")
}

func (c *Controller) ClickMouse(ctx context.Context, button display.MouseButton, clicks int) error {
	return fmt.Errorf("macOS platform not available on this system")
}

func (c *Controller) ScrollMouse(ctx context.Context, clicks int, direction string) error {
	return fmt.Errorf("macOS platform not available on this system")
}

func (c *Controller) TypeText(ctx context.Context, text string, delayMs int) error {
	return fmt.Errorf("macOS platform not available on this system")
}

func (c *Controller) SendKeyCombo(ctx context.Context, combo string) error {
	return fmt.Errorf("macOS platform not available on this system")
}

func (c *Controller) Close() error {
	return nil
}

// Provider is a stub implementation for non-macOS platforms
type Provider struct{}

var _ display.Provider = (*Provider)(nil)

func NewProvider() *Provider {
	return &Provider{}
}

func (p *Provider) GetController() (display.DisplayController, error) {
	return nil, fmt.Errorf("macOS platform not available on this system")
}

func (p *Provider) GetDisplayInfo() display.DisplayInfo {
	return display.DisplayInfo{
		Name:              "macos",
		SupportsRegions:   false,
		SupportsMouse:     false,
		SupportsKeyboard:  false,
		MaxTextLength:     0,
		RequiresElevation: false,
	}
}

func (p *Provider) IsAvailable() bool {
	return false // Always false on non-macOS systems
}

// No init() - don't register on non-macOS systems
