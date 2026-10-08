//go:build linux

package capture

import (
	"context"
	"errors"
	"fmt"
	"os"

	xproto "github.com/jezek/xgb/xproto"
	xgbutil "github.com/jezek/xgbutil"
	xwindow "github.com/jezek/xgbutil/xwindow"

	accessibility "github.com/inference-gateway/cli/internal/computer/infrastructure/accessibility"
	display "github.com/inference-gateway/cli/internal/computer/infrastructure/display"
)

// Preflight fails outside an X11 session: x11grab cannot capture Wayland.
func Preflight() error {
	return checkX11(os.Getenv("XDG_SESSION_TYPE"), os.Getenv("DISPLAY"))
}

func checkX11(sessionType, displayName string) error {
	if sessionType == "wayland" {
		return errors.New("screen recording needs an X11 session; Wayland is not supported yet")
	}
	if displayName == "" {
		return errors.New("screen recording needs an X11 display, but DISPLAY is not set")
	}
	return nil
}

func connect() (*xgbutil.XUtil, error) {
	x, err := xgbutil.NewConn()
	if err != nil {
		return nil, fmt.Errorf("connect to X11 display %q: %w", os.Getenv("DISPLAY"), err)
	}
	return x, nil
}

// PrimaryScreen returns the X11 root screen, which x11grab captures 1:1.
// It is read from X directly: robotgo's purego build targets Wayland.
func PrimaryScreen(context.Context) (Screen, error) {
	x, err := connect()
	if err != nil {
		return Screen{}, err
	}
	defer x.Conn().Close()
	s := x.Screen()
	w, h := int(s.WidthInPixels), int(s.HeightInPixels)
	return Screen{Width: w, Height: h, NativeWidth: w, NativeHeight: h}, nil
}

// WindowBounds returns the frame (decorations included) of the matching
// top-level window, preferring the topmost one.
func WindowBounds(_ context.Context, window string) (display.Region, error) {
	t, err := parseTarget(window)
	if err != nil {
		return display.Region{}, err
	}
	x, err := connect()
	if err != nil {
		return display.Region{}, err
	}
	defer x.Conn().Close()

	var win xproto.Window
	if t.frontmost {
		win = accessibility.FrontmostWindow(x)
	} else {
		win = accessibility.TopmostWindow(x, t.pid, t.name)
	}
	if win == 0 {
		return display.Region{}, errNoWindow(window)
	}
	g, err := xwindow.New(x, win).DecorGeometry()
	if err != nil {
		return display.Region{}, fmt.Errorf("read window geometry: %w", err)
	}
	return display.Region{X: g.X(), Y: g.Y(), Width: g.Width(), Height: g.Height()}, nil
}
