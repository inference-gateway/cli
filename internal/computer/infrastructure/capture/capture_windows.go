//go:build windows

package capture

import (
	"context"
	"slices"
	"sync"
	"unsafe"

	robotwin "github.com/go-vgo/robotgo/win"
	win "github.com/tailscale/win"
	windows "golang.org/x/sys/windows"

	display "github.com/inference-gateway/cli/internal/computer/infrastructure/display"
)

// Preflight has nothing to check: gdigrab needs no permission.
func Preflight() error { return nil }

// PrimaryScreen returns the primary display. The CLI is not DPI aware, so
// the logical size is DPI-virtualized, while gdigrab captures physical
// pixels (DESKTOPHORZRES/DESKTOPVERTRES).
func PrimaryScreen(ctx context.Context) (Screen, error) {
	w, h, err := controllerSize(ctx)
	if err != nil {
		return Screen{}, err
	}
	hdc := win.GetDC(0)
	defer win.ReleaseDC(0, hdc)
	nw, nh := int(win.GetDeviceCaps(hdc, win.DESKTOPHORZRES)), int(win.GetDeviceCaps(hdc, win.DESKTOPVERTRES))
	if nw <= 0 || nh <= 0 {
		nw, nh = w, h
	}
	return Screen{Width: w, Height: h, NativeWidth: nw, NativeHeight: nh}, nil
}

var (
	enumMu    sync.Mutex
	enumHwnds []windows.HWND
	enumProc  = windows.NewCallback(func(hwnd windows.HWND, _ uintptr) uintptr {
		enumHwnds = append(enumHwnds, hwnd)
		return 1
	})
)

// topLevelWindows returns top-level windows in z-order, topmost first.
func topLevelWindows() []windows.HWND {
	enumMu.Lock()
	defer enumMu.Unlock()
	enumHwnds = enumHwnds[:0]
	_ = windows.EnumWindows(enumProc, nil)
	return slices.Clone(enumHwnds)
}

// WindowBounds returns the visible frame of the matching window, preferring
// the topmost one, in logical pixels.
func WindowBounds(ctx context.Context, window string) (display.Region, error) {
	t, err := parseTarget(window)
	if err != nil {
		return display.Region{}, err
	}
	var pids []int
	if t.name != "" {
		if pids, err = robotwin.FindIds(t.name); err != nil {
			return display.Region{}, err
		}
	}

	match := windows.GetForegroundWindow()
	if !t.frontmost {
		match = topmostWindowOf(t.pid, pids)
	}
	if match == 0 {
		return display.Region{}, errNoWindow(window)
	}

	var r windows.Rect
	if err := windows.DwmGetWindowAttribute(match, windows.DWMWA_EXTENDED_FRAME_BOUNDS, unsafe.Pointer(&r), uint32(unsafe.Sizeof(r))); err != nil {
		return display.Region{}, errNoWindow(window)
	}
	s, err := PrimaryScreen(ctx)
	if err != nil {
		return display.Region{}, err
	}
	toLogicalX := func(v int32) int { return int(v) * s.Width / s.NativeWidth }
	toLogicalY := func(v int32) int { return int(v) * s.Height / s.NativeHeight }
	return display.Region{
		X:      toLogicalX(r.Left),
		Y:      toLogicalY(r.Top),
		Width:  toLogicalX(r.Right) - toLogicalX(r.Left),
		Height: toLogicalY(r.Bottom) - toLogicalY(r.Top),
	}, nil
}

// topmostWindowOf returns the topmost shown window owned by pid or by one of
// pids, or 0.
func topmostWindowOf(pid int, pids []int) windows.HWND {
	for _, hwnd := range topLevelWindows() {
		if !shownWindow(hwnd) {
			continue
		}
		var owner uint32
		if _, err := windows.GetWindowThreadProcessId(hwnd, &owner); err != nil {
			continue
		}
		if int(owner) == pid || slices.Contains(pids, int(owner)) {
			return hwnd
		}
	}
	return 0
}

// shownWindow skips hidden, minimized and cloaked (e.g. suspended UWP)
// windows, which have bounds but nothing on screen to record.
func shownWindow(hwnd windows.HWND) bool {
	if !windows.IsWindowVisible(hwnd) || win.IsIconic(win.HWND(hwnd)) {
		return false
	}
	var cloaked uint32
	err := windows.DwmGetWindowAttribute(hwnd, windows.DWMWA_CLOAKED, unsafe.Pointer(&cloaked), uint32(unsafe.Sizeof(cloaked)))
	return err != nil || cloaked == 0
}
