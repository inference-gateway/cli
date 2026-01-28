//go:build darwin

package macos

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework ApplicationServices
#include <ApplicationServices/ApplicationServices.h>

bool checkAccessibilityPermissions() {
    return AXIsProcessTrusted();
}
*/
import "C"

import (
	"context"
	"fmt"
	"image"
	"os"
	"runtime"

	display "github.com/inference-gateway/cli/internal/display"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// Controller implements display.DisplayController for macOS using RobotGo
type Controller struct {
	client *MacOSClient
}

var _ display.DisplayController = (*Controller)(nil)
var _ display.FocusManager = (*Controller)(nil)

func (c *Controller) CaptureScreenBytes(ctx context.Context, region *display.Region) ([]byte, error) {
	if region == nil {
		return c.client.CaptureScreenBytes(0, 0, 0, 0)
	}
	return c.client.CaptureScreenBytes(region.X, region.Y, region.Width, region.Height)
}

func (c *Controller) CaptureScreen(ctx context.Context, region *display.Region) (image.Image, error) {
	if region == nil {
		return c.client.CaptureScreen(0, 0, 0, 0)
	}
	return c.client.CaptureScreen(region.X, region.Y, region.Width, region.Height)
}

func (c *Controller) GetScreenDimensions(ctx context.Context) (width, height int, err error) {
	w, h := c.client.GetScreenDimensions()
	return w, h, nil
}

func (c *Controller) GetCursorPosition(ctx context.Context) (x, y int, err error) {
	return c.client.GetCursorPosition()
}

func (c *Controller) MoveMouse(ctx context.Context, x, y int) error {
	return c.client.MoveMouse(x, y)
}

func (c *Controller) ClickMouse(ctx context.Context, button display.MouseButton, clicks int) error {
	return c.client.ClickMouse(button.String(), clicks)
}

func (c *Controller) ScrollMouse(ctx context.Context, clicks int, direction string) error {
	return c.client.ScrollMouse(clicks, direction)
}

func (c *Controller) TypeText(ctx context.Context, text string, delayMs int) error {
	return c.client.TypeText(text, delayMs)
}

func (c *Controller) SendKeyCombo(ctx context.Context, combo string) error {
	return c.client.SendKeyCombo(combo)
}

func (c *Controller) Close() error {
	c.client.Close()
	return nil
}

// FocusManager implementation for macOS

func (c *Controller) GetFrontmostApp(ctx context.Context) (string, error) {
	appID := c.client.GetFrontmostApp()
	if appID == "" {
		return "", fmt.Errorf("no frontmost application found")
	}
	return appID, nil
}

func (c *Controller) ActivateApp(ctx context.Context, appIdentifier string) error {
	return c.client.ActivateApp(appIdentifier)
}

func (c *Controller) GetTerminalApp(ctx context.Context) (string, error) {
	terminalID := c.client.GetTerminalApp()
	if terminalID == "" {
		return "", fmt.Errorf("no terminal application found")
	}
	return terminalID, nil
}

func (c *Controller) SwitchToTerminal(ctx context.Context) error {
	return c.client.SwitchToTerminal()
}

// Provider implements the display.Provider interface for macOS
type Provider struct{}

var _ display.Provider = (*Provider)(nil)

func NewProvider() *Provider {
	return &Provider{}
}

func (p *Provider) GetController() (display.DisplayController, error) {
	if os.Getenv("SSH_CONNECTION") != "" {
		return nil, fmt.Errorf("macOS display not available in SSH session")
	}

	if !hasAccessibilityPermissions() {
		return nil, fmt.Errorf("accessibility permissions required. Grant access in System Settings > Privacy & Security > Accessibility (or System Preferences > Security & Privacy > Privacy > Accessibility on older macOS)")
	}

	client, err := NewMacOSClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create macOS client: %w", err)
	}

	return &Controller{client: client}, nil
}

// hasAccessibilityPermissions checks if the app has accessibility permissions
// Uses native macOS AXIsProcessTrusted() API for reliable detection
func hasAccessibilityPermissions() bool {
	trusted := C.checkAccessibilityPermissions()
	hasPerm := bool(trusted)

	if !hasPerm {
		logger.Debug("Accessibility permissions not granted")
	}

	return hasPerm
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
	display.Register(NewProvider())
	logger.Debug("Registered macOS display provider")
}
