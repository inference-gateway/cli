//go:build darwin

package macos

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit
#include <stdlib.h>
#include <string.h>
#import <AppKit/AppKit.h>

// All string-returning functions return strdup'd memory that the Go caller
// must free; UTF8String pointers are owned by autoreleased NSStrings and do
// not survive the autorelease pool.

// Returns the bundle identifier of the frontmost application; "" if none.
char* getFrontmostApp() {
    @autoreleasepool {
        NSRunningApplication *app = [[NSWorkspace sharedWorkspace] frontmostApplication];
        const char *bundleID = app ? [app.bundleIdentifier UTF8String] : NULL;
        return strdup(bundleID ? bundleID : "");
    }
}

// Returns the PID of the frontmost application; -1 if none.
int getFrontmostPID() {
    @autoreleasepool {
        NSRunningApplication *app = [[NSWorkspace sharedWorkspace] frontmostApplication];
        return app ? (int)[app processIdentifier] : -1;
    }
}

// Returns the localized name of the frontmost application; "" if none.
char* getFrontmostName() {
    @autoreleasepool {
        NSRunningApplication *app = [[NSWorkspace sharedWorkspace] frontmostApplication];
        const char *name = app ? [app.localizedName UTF8String] : NULL;
        return strdup(name ? name : "");
    }
}

// Activate application by bundle identifier; returns true on success.
bool activateApp(const char *bundleIdentifier) {
    @autoreleasepool {
        NSString *bundleID = [NSString stringWithUTF8String:bundleIdentifier];
        NSArray *apps = [NSRunningApplication runningApplicationsWithBundleIdentifier:bundleID];
        if ([apps count] == 0) {
            return false;
        }
        NSRunningApplication *app = [apps firstObject];
        return [app activateWithOptions:NSApplicationActivateAllWindows];
    }
}

// Activate application by PID; returns true on success.
bool activateAppByPID(int pid) {
    @autoreleasepool {
        NSArray *apps = [[NSWorkspace sharedWorkspace] runningApplications];
        for (NSRunningApplication *app in apps) {
            if ((int)[app processIdentifier] == pid) {
                return [app activateWithOptions:NSApplicationActivateAllWindows];
            }
        }
        return false;
    }
}

// Returns running app info as pipe-delimited "id|name|pid\n" lines.
// For unbundled apps (nil bundleIdentifier), the id field is empty.
char* listRunningApps() {
    @autoreleasepool {
        NSArray *apps = [[NSWorkspace sharedWorkspace] runningApplications];
        NSMutableString *result = [NSMutableString string];
        for (NSRunningApplication *app in apps) {
            if ([app activationPolicy] == NSApplicationActivationPolicyProhibited) {
                continue; // skip background-only processes (e.g. UI agent helpers)
            }
            NSString *bundleID = [app bundleIdentifier];
            if (bundleID == nil) bundleID = @"";
            NSString *name = [app localizedName];
            if (name == nil) name = @"";
            // Escape pipes and newlines from name
            name = [name stringByReplacingOccurrencesOfString:@"|" withString:@"_"];
            name = [name stringByReplacingOccurrencesOfString:@"\n" withString:@" "];
            [result appendFormat:@"%@|%@|%d\n", bundleID, name, (int)[app processIdentifier]];
        }
        return strdup([result UTF8String]);
    }
}
*/
import "C"

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"strconv"
	"strings"
	"time"
	"unsafe"

	robotgo "github.com/go-vgo/robotgo"

	display "github.com/inference-gateway/cli/internal/display"
	domain "github.com/inference-gateway/cli/internal/domain"
)

// MacOSClient provides macOS screen control operations using RobotGo
type MacOSClient struct {
	screenWidth  int     // Physical pixels (e.g., 2880 on 2x Retina)
	screenHeight int     // Physical pixels (e.g., 1800 on 2x Retina)
	scaleFactor  float64 // Display scale factor (1.0, 2.0, or 3.0)
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
	logicalWidth, logicalHeight := robotgo.GetScreenSize()

	scaleFactor := robotgo.ScaleF()
	if scaleFactor == 0.0 {
		scaleFactor = 1.0
	}

	physicalWidth := int(float64(logicalWidth) * scaleFactor)
	physicalHeight := int(float64(logicalHeight) * scaleFactor)

	return &MacOSClient{
		screenWidth:  physicalWidth,
		screenHeight: physicalHeight,
		scaleFactor:  scaleFactor,
	}, nil
}

// Close closes the macOS client (no-op for RobotGo)
func (c *MacOSClient) Close() {
	// Nothing to close for RobotGo
}

// GetScreenDimensions returns the screen width and height in logical pixels
// This matches the coordinate space used by RobotGo's mouse operations
func (c *MacOSClient) GetScreenDimensions() (int, int) {
	return c.ScalePhysicalToLogical(c.screenWidth, c.screenHeight)
}

// GetScaleFactor returns the display scale factor (1.0, 2.0, or 3.0)
func (c *MacOSClient) GetScaleFactor() float64 {
	return c.scaleFactor
}

// ScalePhysicalToLogical converts physical pixel coordinates to logical coordinates
// This is used when passing coordinates to RobotGo APIs (mouse movement, clicks)
// On Retina displays, physical pixels are 2x or 3x logical pixels
func (c *MacOSClient) ScalePhysicalToLogical(x, y int) (int, int) {
	if c.scaleFactor == 1.0 {
		return x, y
	}
	logicalX := int(float64(x) / c.scaleFactor)
	logicalY := int(float64(y) / c.scaleFactor)
	return logicalX, logicalY
}

// CaptureScreen captures a screenshot and returns it as an image.Image
// Coordinates are expected in logical pixels (matching RobotGo's coordinate space)
func (c *MacOSClient) CaptureScreen(x, y, width, height int) (image.Image, error) {
	logicalWidth, logicalHeight := c.ScalePhysicalToLogical(c.screenWidth, c.screenHeight)

	if width == 0 || height == 0 {
		width = logicalWidth
		height = logicalHeight
	}

	if x < 0 || y < 0 || x+width > logicalWidth || y+height > logicalHeight {
		return nil, fmt.Errorf("invalid region: (%d,%d,%d,%d) exceeds screen bounds (%d,%d)",
			x, y, width, height, logicalWidth, logicalHeight)
	}

	bitmap := robotgo.CaptureScreen(x, y, width, height)
	if bitmap == nil {
		return nil, fmt.Errorf("failed to capture screen")
	}

	img := robotgo.ToImage(bitmap)
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
// Returns coordinates in top-left origin (Y=0 at top) to match screenshot coordinates
func (c *MacOSClient) GetCursorPosition() (int, int, error) {
	x, y := robotgo.Location()
	return x, y, nil
}

// MoveMouse moves the cursor to the specified coordinates
// Coordinates should be in logical pixel space (matching GetScreenDimensions)
// Input coordinates use top-left origin (Y=0 at top)
func (c *MacOSClient) MoveMouse(x, y int) error {
	logicalWidth, logicalHeight := c.GetScreenDimensions()
	if x < 0 || y < 0 || x > logicalWidth || y > logicalHeight {
		return fmt.Errorf("invalid coordinates: (%d,%d) exceeds screen bounds (%d,%d)",
			x, y, logicalWidth, logicalHeight)
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

	switch clicks {
	case 1:
		if err := robotgo.Click(robotButton, false); err != nil {
			return err
		}
	case 2:
		if err := robotgo.Click(robotButton, true); err != nil {
			return err
		}
	case 3:
		if err := robotgo.Click(robotButton, true); err != nil {
			return err
		}
		time.Sleep(100 * time.Millisecond)
		if err := robotgo.Click(robotButton, false); err != nil {
			return err
		}
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

// --- NSWorkspace app bridge, used by macosAppProvider (app_darwin.go) ---

// listRunningApps returns all running applications visible to the windowing
// system. For unbundled processes the ID is "pid:N" since there is no bundle
// identifier.
func listRunningApps() ([]domain.Application, error) {
	cStr := C.listRunningApps()
	defer C.free(unsafe.Pointer(cStr))
	s := C.GoString(cStr)

	var apps []domain.Application
	for _, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		parts := strings.SplitN(line, "|", 3)
		if len(parts) != 3 {
			continue
		}
		id, name, pidStr := parts[0], parts[1], parts[2]
		if id == "" {
			id = "pid:" + pidStr
		}

		apps = append(apps, domain.Application{
			ID:         id,
			Name:       name,
			PlatformID: pidStr,
		})
	}

	return apps, nil
}

// frontmostApp returns the currently focused application.
// Returns nil with no error when no app is focused (headless).
func frontmostApp() (*domain.Application, error) {
	pid := int(C.getFrontmostPID())
	if pid <= 0 {
		return nil, nil
	}

	cName := C.getFrontmostName()
	defer C.free(unsafe.Pointer(cName))
	name := C.GoString(cName)

	cBundleID := C.getFrontmostApp()
	defer C.free(unsafe.Pointer(cBundleID))
	bundleID := C.GoString(cBundleID)

	id := bundleID
	if id == "" {
		id = fmt.Sprintf("pid:%d", pid)
	}

	return &domain.Application{
		ID:         id,
		Name:       name,
		PlatformID: strconv.Itoa(pid),
	}, nil
}

// activateApp brings an application to the foreground. It accepts both bundle
// IDs (e.g., "com.google.Chrome") and "pid:N" formatted IDs.
func activateApp(id string) error {
	cStr := C.CString(id)
	defer C.free(unsafe.Pointer(cStr))

	if C.activateApp(cStr) {
		return nil
	}

	if pidStr, ok := strings.CutPrefix(id, "pid:"); ok {
		pid, err := strconv.Atoi(pidStr)
		if err == nil && C.activateAppByPID(C.int(pid)) {
			return nil
		}
	}

	return fmt.Errorf("failed to activate app %q: %w", id, display.ErrAppNotFound)
}
