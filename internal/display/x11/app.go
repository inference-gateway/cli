//go:build linux

package x11

import (
	"context"
	"fmt"
	"os"
	"strings"

	xgb "github.com/BurntSushi/xgb"
	xproto "github.com/BurntSushi/xgb/xproto"

	display "github.com/inference-gateway/cli/internal/display"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// x11AppProvider implements display.AppProvider for X11.
// It opens its own X11 connection for app management operations so it is
// independent of the display controller's session.
type x11AppProvider struct {
	conn   *xgb.Conn
	screen *xproto.ScreenInfo
}

var _ display.AppProvider = (*x11AppProvider)(nil)

func newX11AppProvider() (*x11AppProvider, error) {
	displayName := os.Getenv("DISPLAY")
	if displayName == "" {
		displayName = ":0"
	}

	conn, err := xgb.NewConnDisplay(displayName)
	if err != nil {
		return nil, fmt.Errorf("connect to X11: %w", err)
	}

	return &x11AppProvider{
		conn:   conn,
		screen: xproto.Setup(conn).DefaultScreen(conn),
	}, nil
}

// ListRunning enumerates all top-level windows via _NET_CLIENT_LIST, reads
// _NET_WM_PID for each to associate windows with processes, and deduplicates
// by PID.  The stable ID for each app is "pid:<PID>" so unbundled processes
// are addressable like any other.
func (p *x11AppProvider) ListRunning(ctx context.Context) ([]domain.Application, error) {
	root := p.screen.Root

	// 1. Get the list of client windows via _NET_CLIENT_LIST
	windows, err := p.getClientList(root)
	if err != nil {
		return nil, fmt.Errorf("get client list: %w", err)
	}

	// 2. For each window extract PID + name, dedup by PID
	seen := make(map[int]bool)
	var apps []domain.Application

	for _, w := range windows {
		pid := p.getWindowPID(w)
		if pid <= 0 || seen[pid] {
			continue
		}
		seen[pid] = true

		name := p.getWindowName(w)
		apps = append(apps, domain.Application{
			ID:         fmt.Sprintf("pid:%d", pid),
			Name:       name,
			PlatformID: fmt.Sprintf("%d", pid),
		})
	}

	return apps, nil
}

// Activate brings the application identified by id to the foreground.
// The id should be in "pid:<PID>" format (from ListRunning) or a plain name
// for direct lookup.  It finds the first top-level window belonging to the app
// and sends _NET_ACTIVE_WINDOW.
func (p *x11AppProvider) Activate(ctx context.Context, id string) error {
	root := p.screen.Root

	targetPID := -1
	targetName := ""

	if strings.HasPrefix(id, "pid:") {
		if n, _ := fmt.Sscanf(id, "pid:%d", &targetPID); n != 1 {
			targetPID = -1
		}
	} else {
		targetName = strings.ToLower(id)
	}

	// For name-based lookup we first scan to find the PID
	if targetPID < 0 && targetName != "" {
		targetPID = p.lookupPIDByName(root, targetName)
	}

	if targetPID < 0 {
		return fmt.Errorf("application %q: %w", id, display.ErrAppNotFound)
	}

	window, ok := p.findWindowByPID(root, targetPID)
	if !ok {
		return fmt.Errorf("application %q: %w", id, display.ErrAppNotFound)
	}

	return p.activateWindow(root, window)
}

// GetFocused returns the currently focused (active) application.
func (p *x11AppProvider) GetFocused(ctx context.Context) (*domain.Application, error) {
	root := p.screen.Root

	window, err := p.getActiveWindow(root)
	if err != nil {
		return nil, nil // headless - no focused window
	}
	if window == 0 {
		return nil, nil
	}

	pid := p.getWindowPID(window)
	if pid <= 0 {
		return nil, nil
	}

	name := p.getWindowName(window)

	return &domain.Application{
		ID:         fmt.Sprintf("pid:%d", pid),
		Name:       name,
		PlatformID: fmt.Sprintf("%d", pid),
	}, nil
}

// Close closes the X11 connection.
func (p *x11AppProvider) Close() error {
	p.conn.Close()
	return nil
}

// --- X11 atom / property helpers ---

// getAtom resolves an atom name to an Atom value.
func (p *x11AppProvider) getAtom(name string) (xproto.Atom, error) {
	reply, err := xproto.InternAtom(p.conn, false, uint16(len(name)), name).Reply()
	if err != nil {
		return 0, err
	}
	return reply.Atom, nil
}

// getClientList reads the _NET_CLIENT_LIST property from the root window.
func (p *x11AppProvider) getClientList(root xproto.Window) ([]xproto.Window, error) {
	atom, err := p.getAtom("_NET_CLIENT_LIST")
	if err != nil {
		return nil, err
	}

	reply, err := xproto.GetProperty(p.conn, false, root, atom, 0, 0, 1024).Reply()
	if err != nil {
		return nil, err
	}
	if reply.Format != 32 {
		return nil, nil
	}

	// Each 4 bytes is one window (32-bit format)
	windows := make([]xproto.Window, reply.BytesAfter/4+reply.ValueLen)
	for i := range windows {
		if i < int(reply.ValueLen) {
			windows[i] = xproto.Window(readUint32LE(reply.Value, i*4))
		}
	}
	return windows, nil
}

// getWindowPID reads the _NET_WM_PID for a window. Returns -1 on failure.
func (p *x11AppProvider) getWindowPID(window xproto.Window) int {
	atom, err := p.getAtom("_NET_WM_PID")
	if err != nil {
		return -1
	}

	reply, err := xproto.GetProperty(p.conn, false, window, atom, 0, 0, 4).Reply()
	if err != nil || reply.Format != 32 || reply.ValueLen == 0 {
		return -1
	}

	return int(readUint32LE(reply.Value, 0))
}

// getWindowName reads the window title from _NET_WM_VISIBLE_NAME or _NET_WM_NAME.
func (p *x11AppProvider) getWindowName(window xproto.Window) string {
	// Try _NET_WM_VISIBLE_NAME first
	name := p.getUTF8Property(window, "_NET_WM_VISIBLE_NAME")
	if name != "" {
		return name
	}

	// Fall back to _NET_WM_NAME
	name = p.getUTF8Property(window, "_NET_WM_NAME")
	if name != "" {
		return name
	}

	// Last resort: localised WM_NAME (legacy)
	atom, err := p.getAtom("WM_NAME")
	if err != nil {
		return ""
	}
	reply, err := xproto.GetProperty(p.conn, false, window, atom, 0, 0, 256).Reply()
	if err != nil || reply.ValueLen == 0 {
		return ""
	}
	return string(reply.Value[:reply.ValueLen])
}

// getUTF8Property reads a UTF-8 string property from the window.
func (p *x11AppProvider) getUTF8Property(window xproto.Window, propName string) string {
	atom, err := p.getAtom(propName)
	if err != nil {
		return ""
	}
	reply, err := xproto.GetProperty(p.conn, false, window, atom, 0, 0, 1024).Reply()
	if err != nil || reply.ValueLen == 0 {
		return ""
	}
	return strings.TrimRight(string(reply.Value[:reply.ValueLen]), "\x00")
}

// getActiveWindow reads the _NET_ACTIVE_WINDOW atom from the root window.
func (p *x11AppProvider) getActiveWindow(root xproto.Window) (xproto.Window, error) {
	atom, err := p.getAtom("_NET_ACTIVE_WINDOW")
	if err != nil {
		return 0, err
	}

	reply, err := xproto.GetProperty(p.conn, false, root, atom, 0, 0, 4).Reply()
	if err != nil {
		return 0, err
	}
	if reply.Format != 32 || reply.ValueLen == 0 {
		return 0, nil
	}

	return xproto.Window(readUint32LE(reply.Value, 0)), nil
}

// lookupPIDByName scans _NET_CLIENT_LIST for a window whose name matches
// targetName (case-insensitive substring match). Returns the first matching
// PID, or -1 if none found.
func (p *x11AppProvider) lookupPIDByName(root xproto.Window, targetName string) int {
	windows, err := p.getClientList(root)
	if err != nil {
		return -1
	}
	for _, w := range windows {
		name := strings.ToLower(p.getWindowName(w))
		if name == targetName || strings.Contains(name, targetName) {
			if pid := p.getWindowPID(w); pid > 0 {
				return pid
			}
		}
	}
	return -1
}

// findWindowByPID scans _NET_CLIENT_LIST for a window belonging to the given PID.
func (p *x11AppProvider) findWindowByPID(root xproto.Window, pid int) (xproto.Window, bool) {
	windows, err := p.getClientList(root)
	if err != nil {
		return 0, false
	}
	for _, w := range windows {
		if w == 0 {
			continue
		}
		if p.getWindowPID(w) == pid {
			return w, true
		}
	}
	return 0, false
}

// activateWindow sends _NET_ACTIVE_WINDOW to the root window for the given
// client window, asking the window manager to bring it to the foreground.
func (p *x11AppProvider) activateWindow(root, window xproto.Window) error {
	atom, err := p.getAtom("_NET_ACTIVE_WINDOW")
	if err != nil {
		// Fallback: attempt XSetInputFocus directly
		cookie := xproto.SetInputFocusChecked(p.conn, xproto.InputFocusPointerRoot, window, xproto.TimeCurrentTime)
		return cookie.Check()
	}

	// Build a ClientMessage event (32 bytes).
	//  Byte 0: type = 33 (ClientMessage)
	//  Byte 1: format = 32
	//  Bytes 2-3: sequence (0, filled by server)
	//  Bytes 4-7: window receiving the event (root)
	//  Bytes 8-11: message_type atom (_NET_ACTIVE_WINDOW)
	//  Bytes 12-31: data (5 * 4 bytes)
	//    data[0] = 1 (source indication: application)
	//    data[1] = 0 (timestamp, CurrentTime)
	//    data[2] = window (the window to activate)
	//    data[3] = 0
	//    data[4] = 0
	event := [32]byte{}
	event[0] = 33 // ClientMessage
	event[1] = 32 // format
	writeUint32LE(event[:], 4, uint32(root))
	writeUint32LE(event[:], 8, uint32(atom))
	writeUint32LE(event[:], 12, 1)              // source: application
	writeUint32LE(event[:], 16, 0)              // timestamp: CurrentTime
	writeUint32LE(event[:], 20, uint32(window)) // target window

	cookie := xproto.SendEventChecked(p.conn, false, root,
		0x00FFFFFF, // SubstructureRedirect | SubstructureNotify
		string(event[:]))
	return cookie.Check()
}

// --- byte-order helpers ---

func readUint32LE(buf []byte, offset int) uint32 {
	return uint32(buf[offset]) | uint32(buf[offset+1])<<8 | uint32(buf[offset+2])<<16 | uint32(buf[offset+3])<<24
}

func writeUint32LE(buf []byte, offset int, v uint32) {
	buf[offset] = byte(v)
	buf[offset+1] = byte(v >> 8)
	buf[offset+2] = byte(v >> 16)
	buf[offset+3] = byte(v >> 24)
}

// Register the X11 app provider.
//
//goland:noinspection GoUnusedExportedFunction
func init() {
	// Only register if DISPLAY is set and Wayland is not active.
	if os.Getenv("DISPLAY") == "" || os.Getenv("WAYLAND_DISPLAY") != "" {
		return
	}

	provider, err := newX11AppProvider()
	if err != nil {
		logger.Debug("x11 app provider unavailable", "error", err)
		return
	}

	display.RegisterAppProvider(provider)
	logger.Debug("registered X11 app provider")
}
