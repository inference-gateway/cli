//go:build darwin

package macos

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"strconv"
	"strings"
	"time"

	robotgo "github.com/go-vgo/robotgo"

	display "github.com/inference-gateway/cli/internal/computer/display"
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
	logicalWidth, _ := robotgo.GetScreenSize()
	physicalWidth, physicalHeight := robotgo.GetScaleSize()

	scaleFactor := 1.0
	if logicalWidth > 0 && physicalWidth > 0 {
		scaleFactor = float64(physicalWidth) / float64(logicalWidth)
	}

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

	img, err := robotgo.CaptureImg(x, y, width, height)
	if err != nil {
		return nil, fmt.Errorf("failed to capture screen: %w", err)
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

// --- app focus via robotgo (cgo-free), used by macosAppProvider (app_darwin.go) ---

// listRunningApps returns the running processes. robotgo lists the whole
// process table rather than only GUI applications, so the result is noisier
// than the old NSWorkspace enumeration.
// ponytail: full process table; filter to GUI apps if it proves too noisy.
func listRunningApps() ([]domain.Application, error) {
	procs, err := robotgo.Process()
	if err != nil {
		return nil, fmt.Errorf("failed to list processes: %w", err)
	}
	apps := make([]domain.Application, 0, len(procs))
	for _, p := range procs {
		apps = append(apps, domain.Application{
			ID:         fmt.Sprintf("pid:%d", p.Pid),
			Name:       p.Name,
			PlatformID: strconv.Itoa(p.Pid),
		})
	}
	return apps, nil
}

// frontmostApp returns the focused application. Returns nil with no error when
// no app is focused (headless).
func frontmostApp() (*domain.Application, error) {
	pid := robotgo.GetPid()
	if pid <= 0 {
		return nil, nil
	}
	name, _ := robotgo.FindName(pid)
	return &domain.Application{
		ID:         fmt.Sprintf("pid:%d", pid),
		Name:       name,
		PlatformID: strconv.Itoa(pid),
	}, nil
}

// activateApp brings an application to the foreground by "pid:N" or by name.
// The cgo-free robotgo backend activates by window name, so a pid is resolved
// to its process name first.
func activateApp(id string) error {
	name := id
	if pidStr, ok := strings.CutPrefix(id, "pid:"); ok {
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			return fmt.Errorf("invalid app id %q: %w", id, display.ErrAppNotFound)
		}
		name, err = robotgo.FindName(pid)
		if err != nil || name == "" {
			return fmt.Errorf("app %q not found: %w", id, display.ErrAppNotFound)
		}
	}
	if err := robotgo.ActiveName(name); err != nil {
		return fmt.Errorf("failed to activate app %q: %w", id, display.ErrAppNotFound)
	}
	return nil
}
