//go:build darwin

package macos

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit
#import <AppKit/AppKit.h>

// Get the bundle identifier of the frontmost application
const char* getFrontmostApp() {
    NSRunningApplication *app = [[NSWorkspace sharedWorkspace] frontmostApplication];
    if (app == nil) {
        return "";
    }
    const char *bundleID = [app.bundleIdentifier UTF8String];
    return bundleID ? bundleID : "";
}

// Activate application by bundle identifier
bool activateApp(const char *bundleIdentifier) {
    @autoreleasepool {
        NSString *bundleID = [NSString stringWithUTF8String:bundleIdentifier];
        NSArray *apps = [NSRunningApplication runningApplicationsWithBundleIdentifier:bundleID];
        if ([apps count] == 0) {
            return false;
        }
        NSRunningApplication *app = [apps firstObject];
        // Use activate instead of activateWithOptions (deprecated in macOS 14+)
        return [app activateWithOptions:NSApplicationActivateAllWindows];
    }
}

// Get the terminal app bundle ID (Terminal.app, iTerm2, VS Code, etc.)
const char* getTerminalApp() {
    @autoreleasepool {
        // Common terminal applications
        NSArray *terminalBundles = @[
            @"com.apple.Terminal",           // Terminal.app
            @"com.googlecode.iterm2",        // iTerm2
            @"com.microsoft.VSCode",         // VS Code
            @"com.sublimetext.4",            // Sublime Text
            @"com.jetbrains.goland",         // GoLand
            @"com.jetbrains.intellij",       // IntelliJ IDEA
            @"org.alacritty",                // Alacritty
            @"net.kovidgoyal.kitty",         // Kitty
        ];

        for (NSString *bundleID in terminalBundles) {
            NSArray *apps = [NSRunningApplication runningApplicationsWithBundleIdentifier:bundleID];
            if ([apps count] > 0) {
                return [bundleID UTF8String];
            }
        }

        return "";
    }
}
*/
import "C"

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"strings"
	"time"
	"unsafe"

	robotgo "github.com/go-vgo/robotgo"
)

// MacOSClient provides macOS screen control operations using RobotGo
type MacOSClient struct {
	screenWidth  int
	screenHeight int
}

// Modifier and key mapping tables
var (
	modifierMap = map[string]string{
		"super":   "cmd",
		"command": "cmd",
		"cmd":     "cmd",
		"ctrl":    "ctrl",
		"control": "ctrl",
		"alt":     "alt",
		"option":  "alt",
		"shift":   "shift",
	}

	specialKeyMap = map[string]string{
		"enter":     "enter",
		"return":    "enter",
		"tab":       "tab",
		"space":     "space",
		"backspace": "backspace",
		"delete":    "delete",
		"del":       "delete",
		"esc":       "esc",
		"escape":    "esc",
		"up":        "up",
		"down":      "down",
		"left":      "left",
		"right":     "right",
		"home":      "home",
		"end":       "end",
		"pageup":    "pageup",
		"pagedown":  "pagedown",
		"f1":        "f1",
		"f2":        "f2",
		"f3":        "f3",
		"f4":        "f4",
		"f5":        "f5",
		"f6":        "f6",
		"f7":        "f7",
		"f8":        "f8",
		"f9":        "f9",
		"f10":       "f10",
		"f11":       "f11",
		"f12":       "f12",
	}
)

// NewMacOSClient creates a new macOS client
func NewMacOSClient() (*MacOSClient, error) {
	// Get screen dimensions
	width, height := robotgo.GetScreenSize()

	return &MacOSClient{
		screenWidth:  width,
		screenHeight: height,
	}, nil
}

// Close closes the macOS client (no-op for RobotGo)
func (c *MacOSClient) Close() {
	// Nothing to close for RobotGo
}

// GetScreenDimensions returns the screen width and height
func (c *MacOSClient) GetScreenDimensions() (int, int) {
	return c.screenWidth, c.screenHeight
}

// CaptureScreen captures a screenshot and returns it as an image.Image
func (c *MacOSClient) CaptureScreen(x, y, width, height int) (image.Image, error) {
	if width == 0 || height == 0 {
		width = c.screenWidth
		height = c.screenHeight
	}

	if x < 0 || y < 0 || x+width > c.screenWidth || y+height > c.screenHeight {
		return nil, fmt.Errorf("invalid region: (%d,%d,%d,%d) exceeds screen bounds (%d,%d)",
			x, y, width, height, c.screenWidth, c.screenHeight)
	}

	bitmap := robotgo.CaptureScreen(x, y, width, height)
	if bitmap == nil {
		return nil, fmt.Errorf("failed to capture screen")
	}

	img := robotgo.ToImage(bitmap)
	if img == nil {
		return nil, fmt.Errorf("failed to convert bitmap to image")
	}

	return img, nil
}

// CaptureScreenBytes captures a screenshot and returns it as PNG bytes
func (c *MacOSClient) CaptureScreenBytes(x, y, width, height int) ([]byte, error) {
	img, err := c.CaptureScreen(x, y, width, height)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("failed to encode image to PNG: %w", err)
	}

	return buf.Bytes(), nil
}

// GetCursorPosition returns the current cursor position
func (c *MacOSClient) GetCursorPosition() (int, int, error) {
	x, y := robotgo.Location()
	return x, y, nil
}

// MoveMouse moves the cursor to the specified coordinates (smooth movement)
func (c *MacOSClient) MoveMouse(x, y int) error {
	if x < 0 || y < 0 || x > c.screenWidth || y > c.screenHeight {
		return fmt.Errorf("invalid coordinates: (%d,%d) exceeds screen bounds (%d,%d)",
			x, y, c.screenWidth, c.screenHeight)
	}

	robotgo.Move(x, y)
	return nil
}

// ClickMouse clicks the specified mouse button
func (c *MacOSClient) ClickMouse(button string, clicks int) error {
	robotButton := button
	if button == "middle" {
		robotButton = "center"
	}

	if robotButton != "left" && robotButton != "right" && robotButton != "center" {
		return fmt.Errorf("invalid button: %s (must be left, right, or middle)", button)
	}

	if clicks < 1 || clicks > 3 {
		return fmt.Errorf("invalid click count: %d (must be 1-3)", clicks)
	}

	for i := range clicks {
		if i > 0 {
			time.Sleep(100 * time.Millisecond)
		}
		robotgo.Click(robotButton, false)
	}

	return nil
}

// ScrollMouse scrolls the mouse wheel
func (c *MacOSClient) ScrollMouse(clicks int, direction string) error {
	if clicks == 0 {
		return nil
	}

	scrollAmount := clicks * 100
	absAmount := scrollAmount
	if scrollAmount < 0 {
		absAmount = -scrollAmount
	}

	var scrollDir string
	if direction == "horizontal" {
		scrollDir = "right"
		if scrollAmount < 0 {
			scrollDir = "left"
		}
	} else {
		scrollDir = "down"
		if scrollAmount < 0 {
			scrollDir = "up"
		}
	}

	robotgo.ScrollDir(absAmount, scrollDir)
	return nil
}

// TypeText types the specified text with delay between characters
func (c *MacOSClient) TypeText(text string, delayMs int) error {
	if text == "" {
		return fmt.Errorf("text cannot be empty")
	}

	if delayMs > 0 {
		for _, char := range text {
			robotgo.Type(string(char))
			time.Sleep(time.Duration(delayMs) * time.Millisecond)
		}
	} else {
		robotgo.Type(text)
	}

	return nil
}

// SendKeyCombo sends a key combination (e.g., "ctrl+c", "cmd+shift+t")
func (c *MacOSClient) SendKeyCombo(combo string) error {
	if combo == "" {
		return fmt.Errorf("key combo cannot be empty")
	}

	parts := strings.Split(combo, "+")
	if len(parts) == 0 {
		return fmt.Errorf("invalid key combo: %s", combo)
	}

	key := strings.ToLower(strings.TrimSpace(parts[len(parts)-1]))
	var modifiers []any

	for i := 0; i < len(parts)-1; i++ {
		mod := strings.ToLower(strings.TrimSpace(parts[i]))
		if mappedMod, ok := modifierMap[mod]; ok {
			modifiers = append(modifiers, mappedMod)
		} else {
			return fmt.Errorf("unknown modifier: %s", mod)
		}
	}

	if mappedKey, ok := specialKeyMap[key]; ok {
		key = mappedKey
	}

	if err := robotgo.KeyTap(key, modifiers...); err != nil {
		return fmt.Errorf("failed to send key combo: %w", err)
	}

	return nil
}

// GetFrontmostApp returns the bundle identifier of the currently focused application
func (c *MacOSClient) GetFrontmostApp() string {
	cAppID := C.getFrontmostApp()
	return C.GoString(cAppID)
}

// ActivateApp brings an application to the foreground by bundle identifier
func (c *MacOSClient) ActivateApp(bundleIdentifier string) error {
	cBundleID := C.CString(bundleIdentifier)
	defer C.free(unsafe.Pointer(cBundleID))

	success := C.activateApp(cBundleID)
	if !success {
		return fmt.Errorf("failed to activate app: %s", bundleIdentifier)
	}

	return nil
}

// GetTerminalApp returns the bundle identifier of the running terminal application
func (c *MacOSClient) GetTerminalApp() string {
	cTerminalID := C.getTerminalApp()
	return C.GoString(cTerminalID)
}

// SwitchToTerminal switches focus to the terminal application
func (c *MacOSClient) SwitchToTerminal() error {
	terminalID := c.GetTerminalApp()
	if terminalID == "" {
		return fmt.Errorf("no terminal application found")
	}

	return c.ActivateApp(terminalID)
}
