package display

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"strings"
	"time"

	robotgo "github.com/go-vgo/robotgo"
)

// Modifier and key mapping tables for robotgo key names
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

// robotController implements DisplayController on robotgo's cgo-free backends
// (selected per platform by the `purego` build tag). Mouse/keyboard
// coordinates are in logical pixels; on scaled displays (Retina, HiDPI)
// physical dimensions are divided by the scale factor.
type robotController struct {
	screenWidth  int // physical pixels
	screenHeight int // physical pixels
	scaleFactor  float64
}

var _ DisplayController = (*robotController)(nil)

func newRobotController() *robotController {
	logicalWidth, _ := robotgo.GetScreenSize()
	physicalWidth, physicalHeight := robotgo.GetScaleSize()

	scaleFactor := 1.0
	if logicalWidth > 0 && physicalWidth > 0 {
		scaleFactor = float64(physicalWidth) / float64(logicalWidth)
	}

	return &robotController{
		screenWidth:  physicalWidth,
		screenHeight: physicalHeight,
		scaleFactor:  scaleFactor,
	}
}

// scalePhysicalToLogical converts physical pixel coordinates to the logical
// coordinate space used by robotgo's mouse and capture APIs.
func (c *robotController) scalePhysicalToLogical(x, y int) (int, int) {
	if c.scaleFactor == 1.0 {
		return x, y
	}
	return int(float64(x) / c.scaleFactor), int(float64(y) / c.scaleFactor)
}

func (c *robotController) GetScreenDimensions(ctx context.Context) (width, height int, err error) {
	w, h := c.scalePhysicalToLogical(c.screenWidth, c.screenHeight)
	return w, h, nil
}

func (c *robotController) CaptureScreen(ctx context.Context, region *Region) (image.Image, error) {
	logicalWidth, logicalHeight := c.scalePhysicalToLogical(c.screenWidth, c.screenHeight)

	x, y, width, height := 0, 0, logicalWidth, logicalHeight
	if region != nil && region.Width > 0 && region.Height > 0 {
		x, y, width, height = region.X, region.Y, region.Width, region.Height
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

func (c *robotController) CaptureScreenBytes(ctx context.Context, region *Region) ([]byte, error) {
	img, err := c.CaptureScreen(ctx, region)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("failed to encode image to PNG: %w", err)
	}
	return buf.Bytes(), nil
}

func (c *robotController) GetCursorPosition(ctx context.Context) (x, y int, err error) {
	x, y = robotgo.Location()
	return x, y, nil
}

func (c *robotController) MoveMouse(ctx context.Context, x, y int) error {
	logicalWidth, logicalHeight := c.scalePhysicalToLogical(c.screenWidth, c.screenHeight)
	if x < 0 || y < 0 || x > logicalWidth || y > logicalHeight {
		return fmt.Errorf("invalid coordinates: (%d,%d) exceeds screen bounds (%d,%d)",
			x, y, logicalWidth, logicalHeight)
	}

	robotgo.Move(x, y)
	return nil
}

func (c *robotController) ClickMouse(ctx context.Context, button MouseButton, clicks int) error {
	robotButton := button.String()
	if robotButton == "middle" {
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
		return robotgo.Click(robotButton, false)
	case 2:
		return robotgo.Click(robotButton, true)
	default:
		if err := robotgo.Click(robotButton, true); err != nil {
			return err
		}
		time.Sleep(100 * time.Millisecond)
		return robotgo.Click(robotButton, false)
	}
}

func (c *robotController) ScrollMouse(ctx context.Context, clicks int, direction string) error {
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

func (c *robotController) TypeText(ctx context.Context, text string, delayMs int) error {
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

func (c *robotController) SendKeyCombo(ctx context.Context, combo string) error {
	if combo == "" {
		return fmt.Errorf("key combo cannot be empty")
	}

	parts := strings.Split(combo, "+")
	key := strings.ToLower(strings.TrimSpace(parts[len(parts)-1]))
	var modifiers []any

	for i := 0; i < len(parts)-1; i++ {
		mod := strings.ToLower(strings.TrimSpace(parts[i]))
		mappedMod, ok := modifierMap[mod]
		if !ok {
			return fmt.Errorf("unknown modifier: %s", mod)
		}
		modifiers = append(modifiers, mappedMod)
	}

	if mappedKey, ok := specialKeyMap[key]; ok {
		key = mappedKey
	}

	if err := robotgo.KeyTap(key, modifiers...); err != nil {
		return fmt.Errorf("failed to send key combo: %w", err)
	}
	return nil
}

func (c *robotController) Close() error {
	return nil
}

// RobotProvider implements the display.Provider interface on robotgo.
type RobotProvider struct{}

var _ Provider = (*RobotProvider)(nil)

func (p *RobotProvider) GetController() (DisplayController, error) {
	return newRobotController(), nil
}

func (p *RobotProvider) GetDisplayInfo() DisplayInfo {
	return DisplayInfo{
		Name: "robotgo",
	}
}

func (p *RobotProvider) IsAvailable() bool {
	return true
}

func init() {
	Register(&RobotProvider{})
}
