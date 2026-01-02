package tools

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"

	xgb "github.com/BurntSushi/xgb"
	xproto "github.com/BurntSushi/xgb/xproto"
	xgbutil "github.com/BurntSushi/xgbutil"
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

// NewX11Client creates a new X11 client connection
func NewX11Client(display string) (*X11Client, error) {
	if display == "" {
		display = ":0"
	}

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

	logger.Debug("Successfully connected to X11 display", "display", display)

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
	// Note: X11 mouse clicking requires the XTEST extension which is not
	// fully implemented in the pure Go xgb library.
	// For production use, consider using xdotool as a fallback or implementing
	// XTEST extension support.

	return fmt.Errorf("X11 mouse clicking requires xdotool (install with: sudo apt install xdotool). Use Wayland with ydotool for native support, or we can add xdotool fallback")
}

// TypeText types the given text by sending key events
func (c *X11Client) TypeText(text string) error {
	// This is a simplified implementation
	// A full implementation would need to:
	// 1. Map characters to keycodes using the keyboard mapping
	// 2. Handle modifier keys (Shift, Ctrl, etc.)
	// 3. Send KeyPress and KeyRelease events for each character

	// For now, return an error indicating this needs proper keysym mapping
	return fmt.Errorf("text typing via X11 requires keysym mapping (not yet implemented)")
}

// SendKeyCombo sends a key combination (e.g., "ctrl+c")
func (c *X11Client) SendKeyCombo(combo string) error {
	// This is a simplified implementation
	// A full implementation would need to:
	// 1. Parse the combo string to extract modifiers and key
	// 2. Map key names to keycodes
	// 3. Send modifier key presses
	// 4. Send the main key press/release
	// 5. Release modifier keys

	// For now, return an error indicating this needs proper implementation
	return fmt.Errorf("key combinations via X11 require keysym mapping (not yet implemented)")
}
