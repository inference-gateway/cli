package x11

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"strings"
	"time"

	xgb "github.com/BurntSushi/xgb"
	xproto "github.com/BurntSushi/xgb/xproto"
	xtest "github.com/BurntSushi/xgb/xtest"
	xgbutil "github.com/BurntSushi/xgbutil"
	keybind "github.com/BurntSushi/xgbutil/keybind"
	xgraphics "github.com/BurntSushi/xgbutil/xgraphics"

	logger "github.com/inference-gateway/cli/internal/logger"
)

// X11Client wraps X11 connection and provides screen control operations
type X11Client struct {
	xu      *xgbutil.XUtil
	conn    *xgb.Conn
	screen  *xproto.ScreenInfo
	display string
}

// Character mapping tables for X11 key names
var (
	shiftChars = map[rune]string{
		'!': "exclam", '@': "at", '#': "numbersign", '$': "dollar",
		'%': "percent", '^': "asciicircum", '&': "ampersand", '*': "asterisk",
		'(': "parenleft", ')': "parenright", '_': "underscore", '+': "plus",
		'{': "braceleft", '}': "braceright", '|': "bar", ':': "colon",
		'"': "quotedbl", '<': "less", '>': "greater", '?': "question",
		'~': "asciitilde",
	}

	punctuationChars = map[rune]string{
		'.': "period", ',': "comma", ';': "semicolon", '\'': "apostrophe",
		'/': "slash", '\\': "backslash", '-': "minus", '=': "equal",
		'[': "bracketleft", ']': "bracketright", '`': "grave",
	}
)

// NewX11Client creates a new X11 client connection
func NewX11Client(display string) (*X11Client, error) {

	oldStderr := os.Stderr
	devNull, devErr := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if devErr == nil {
		os.Stderr = devNull
	}

	xu, err := xgbutil.NewConnDisplay(display)

	if devErr == nil {
		os.Stderr = oldStderr
		_ = devNull.Close()
	}

	if err != nil {
		logger.Error("Failed to connect to X11 display", "display", display, "error", err)
		return nil, fmt.Errorf("failed to connect to X11 display %s: %w", display, err)
	}

	if err := xtest.Init(xu.Conn()); err != nil {
		logger.Error("Failed to initialize XTEST extension", "error", err)
		return nil, fmt.Errorf("failed to initialize XTEST extension: %w", err)
	}

	keybind.Initialize(xu)

	return &X11Client{
		xu:      xu,
		conn:    xu.Conn(),
		screen:  xproto.Setup(xu.Conn()).DefaultScreen(xu.Conn()),
		display: display,
	}, nil
}

// Close closes the X11 connection
func (c *X11Client) Close() {
	if c.conn != nil {
		c.conn.Close()
	}
}

// GetScreenDimensions returns the screen width and height
func (c *X11Client) GetScreenDimensions() (int, int) {
	return int(c.screen.WidthInPixels), int(c.screen.HeightInPixels)
}

// CaptureScreen captures a screenshot of the entire screen or a region
func (c *X11Client) CaptureScreen(x, y, width, height int) (image.Image, error) {
	if width == 0 || height == 0 {
		width = int(c.screen.WidthInPixels)
		height = int(c.screen.HeightInPixels)
		x = 0
		y = 0
	}

	root := c.screen.Root

	ximg, err := xgraphics.NewDrawable(c.xu, xproto.Drawable(root))
	if err != nil {
		return nil, fmt.Errorf("failed to create drawable: %w", err)
	}

	subImg := ximg.SubImage(image.Rect(x, y, x+width, y+height))

	return subImg, nil
}

// CaptureScreenBytes captures a screenshot and returns it as PNG bytes
func (c *X11Client) CaptureScreenBytes(x, y, width, height int) ([]byte, error) {
	img, err := c.CaptureScreen(x, y, width, height)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("failed to encode PNG: %w", err)
	}

	return buf.Bytes(), nil
}

// GetCursorPosition returns the current cursor position
func (c *X11Client) GetCursorPosition() (int, int, error) {
	root := c.screen.Root

	pointer, err := xproto.QueryPointer(c.conn, root).Reply()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to query pointer: %w", err)
	}

	return int(pointer.RootX), int(pointer.RootY), nil
}

// MoveMouse moves the cursor to the specified absolute coordinates
func (c *X11Client) MoveMouse(x, y int) error {
	root := c.screen.Root

	err := xproto.WarpPointerChecked(
		c.conn,
		xproto.WindowNone,
		root,
		0, 0,
		0, 0,
		int16(x), int16(y),
	).Check()

	if err != nil {
		return fmt.Errorf("failed to move mouse: %w", err)
	}

	c.conn.Sync()

	return nil
}

// ClickMouse performs a mouse click at the current cursor position
func (c *X11Client) ClickMouse(button string, clicks int) error {
	root := c.screen.Root

	var buttonCode byte
	switch button {
	case "left":
		buttonCode = 1
	case "middle":
		buttonCode = 2
	case "right":
		buttonCode = 3
	default:
		return fmt.Errorf("invalid button: %s (must be 'left', 'middle', or 'right')", button)
	}

	for i := 0; i < clicks; i++ {
		cookie := xtest.FakeInputChecked(c.conn, xproto.ButtonPress, buttonCode, 0, root, 0, 0, 0)
		if err := cookie.Check(); err != nil {
			return fmt.Errorf("failed to send button press: %w", err)
		}
		time.Sleep(50 * time.Millisecond)

		cookie = xtest.FakeInputChecked(c.conn, xproto.ButtonRelease, buttonCode, 0, root, 0, 0, 0)
		if err := cookie.Check(); err != nil {
			return fmt.Errorf("failed to send button release: %w", err)
		}

		if i < clicks-1 {
			time.Sleep(100 * time.Millisecond)
		}
	}

	c.conn.Sync()
	return nil
}

// ScrollMouse scrolls the mouse wheel
// For X11: button 4 = scroll up, button 5 = scroll down
//
//	button 6 = scroll left, button 7 = scroll right
func (c *X11Client) ScrollMouse(clicks int, direction string) error {
	root := c.screen.Root

	var buttonCode byte
	absClicks := clicks
	if clicks < 0 {
		absClicks = -clicks
	}

	if direction == "horizontal" {
		buttonCode = 7
		if clicks < 0 {
			buttonCode = 6
		}
	} else {
		buttonCode = 5
		if clicks < 0 {
			buttonCode = 4
		}
	}

	absClicks = absClicks * 100

	for i := 0; i < absClicks; i++ {
		cookie := xtest.FakeInputChecked(c.conn, xproto.ButtonPress, buttonCode, 0, root, 0, 0, 0)
		if err := cookie.Check(); err != nil {
			return fmt.Errorf("failed to send scroll press: %w", err)
		}

		cookie = xtest.FakeInputChecked(c.conn, xproto.ButtonRelease, buttonCode, 0, root, 0, 0, 0)
		if err := cookie.Check(); err != nil {
			return fmt.Errorf("failed to send scroll release: %w", err)
		}

		if i < absClicks-1 {
			time.Sleep(50 * time.Millisecond)
		}
	}

	c.conn.Sync()
	return nil
}

// charToKeyInfo maps a character to its X11 key string and shift requirement
type charToKeyInfo struct {
	keyStr     string
	needsShift bool
}

// mapCharToKey converts a character to its X11 key name and shift requirement
func mapCharToKey(char rune) charToKeyInfo {
	if char >= 'A' && char <= 'Z' {
		return charToKeyInfo{
			keyStr:     strings.ToLower(string(char)),
			needsShift: true,
		}
	}

	if shiftChar, ok := shiftChars[char]; ok {
		return charToKeyInfo{
			keyStr:     shiftChar,
			needsShift: true,
		}
	}

	if punctChar, ok := punctuationChars[char]; ok {
		return charToKeyInfo{
			keyStr:     punctChar,
			needsShift: false,
		}
	}

	switch char {
	case '\n':
		return charToKeyInfo{keyStr: "Return", needsShift: false}
	case '\t':
		return charToKeyInfo{keyStr: "Tab", needsShift: false}
	case ' ':
		return charToKeyInfo{keyStr: "space", needsShift: false}
	default:
		return charToKeyInfo{keyStr: string(char), needsShift: false}
	}
}

// TypeText types the given text with a configurable delay between keystrokes (in milliseconds)
func (c *X11Client) TypeText(text string, delayMs int) error {
	root := c.screen.Root
	baseDelay := time.Duration(delayMs) * time.Millisecond

	for _, char := range text {
		keyInfo := mapCharToKey(char)

		keycodes := keybind.StrToKeycodes(c.xu, keyInfo.keyStr)
		if len(keycodes) == 0 {
			logger.Debug("No keycode found for character", "char", string(char), "keyStr", keyInfo.keyStr)
			continue
		}

		keycode := keycodes[0]

		if err := c.typeKeyWithShift(root, keycode, keyInfo.needsShift, baseDelay); err != nil {
			return err
		}
	}

	c.conn.Sync()
	return nil
}

// typeKeyWithShift types a single key, optionally with shift modifier
func (c *X11Client) typeKeyWithShift(root xproto.Window, keycode xproto.Keycode, needsShift bool, delay time.Duration) error {
	if needsShift {
		shiftKeycodes := keybind.StrToKeycodes(c.xu, "Shift_L")
		if len(shiftKeycodes) > 0 {
			_ = xtest.FakeInput(c.conn, xproto.KeyPress, byte(shiftKeycodes[0]), 0, root, 0, 0, 0)
			time.Sleep(delay)
		}
	}

	_ = xtest.FakeInput(c.conn, xproto.KeyPress, byte(keycode), 0, root, 0, 0, 0)
	time.Sleep(delay)

	_ = xtest.FakeInput(c.conn, xproto.KeyRelease, byte(keycode), 0, root, 0, 0, 0)
	time.Sleep(delay)

	if needsShift {
		shiftKeycodes := keybind.StrToKeycodes(c.xu, "Shift_L")
		if len(shiftKeycodes) > 0 {
			_ = xtest.FakeInput(c.conn, xproto.KeyRelease, byte(shiftKeycodes[0]), 0, root, 0, 0, 0)
			time.Sleep(delay)
		}
	}

	return nil
}

// SendKeyCombo sends a key combination (e.g., "ctrl+c", "super+l")
func (c *X11Client) SendKeyCombo(combo string) error {
	root := c.screen.Root

	combo = strings.ReplaceAll(combo, "-", "+")
	parts := strings.Split(combo, "+")

	if len(parts) == 0 {
		return fmt.Errorf("invalid key combination: %s", combo)
	}

	modifiers := parts[:len(parts)-1]
	mainKey := parts[len(parts)-1]

	modifierMap := map[string]string{
		"ctrl":    "Control_L",
		"control": "Control_L",
		"alt":     "Alt_L",
		"shift":   "Shift_L",
		"super":   "Super_L",
		"meta":    "Meta_L",
		"win":     "Super_L",
		"cmd":     "Super_L",
	}

	var modKeycodes []xproto.Keycode
	for _, mod := range modifiers {
		modName := strings.ToLower(strings.TrimSpace(mod))
		xModName, ok := modifierMap[modName]
		if !ok {
			xModName = mod
		}

		keycodes := keybind.StrToKeycodes(c.xu, xModName)
		if len(keycodes) == 0 {
			return fmt.Errorf("no keycode found for modifier: %s", mod)
		}
		modKeycodes = append(modKeycodes, keycodes[0])
	}

	mainKey = strings.TrimSpace(mainKey)
	mainKeycodes := keybind.StrToKeycodes(c.xu, mainKey)
	if len(mainKeycodes) == 0 {
		return fmt.Errorf("no keycode found for key: %s", mainKey)
	}
	mainKeycode := mainKeycodes[0]

	for _, keycode := range modKeycodes {
		_ = xtest.FakeInput(c.conn, xproto.KeyPress, byte(keycode), 0, root, 0, 0, 0)
		time.Sleep(10 * time.Millisecond)
	}

	_ = xtest.FakeInput(c.conn, xproto.KeyPress, byte(mainKeycode), 0, root, 0, 0, 0)
	time.Sleep(50 * time.Millisecond)

	_ = xtest.FakeInput(c.conn, xproto.KeyRelease, byte(mainKeycode), 0, root, 0, 0, 0)
	time.Sleep(10 * time.Millisecond)

	for i := len(modKeycodes) - 1; i >= 0; i-- {
		_ = xtest.FakeInput(c.conn, xproto.KeyRelease, byte(modKeycodes[i]), 0, root, 0, 0, 0)
		time.Sleep(10 * time.Millisecond)
	}

	c.conn.Sync()
	return nil
}
