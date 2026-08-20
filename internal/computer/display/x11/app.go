//go:build linux

package x11

import (
	"context"
	"fmt"
	"os"
	"strings"

	xgb "github.com/BurntSushi/xgb"
	xproto "github.com/BurntSushi/xgb/xproto"

	display "github.com/inference-gateway/cli/internal/computer/display"
	domain "github.com/inference-gateway/cli/internal/domain"
)

// x11AppProvider implements display.AppProvider for X11. It is stateless:
// each call opens its own short-lived X connection, so the registered
// singleton holds no resources between calls.
type x11AppProvider struct{}

var _ display.AppProvider = (*x11AppProvider)(nil)

func (x11AppProvider) ListRunning(ctx context.Context) ([]domain.Application, error) {
	s, err := newX11Session()
	if err != nil {
		return nil, err
	}
	defer s.close()
	return s.listRunning()
}

func (x11AppProvider) Activate(ctx context.Context, id string) error {
	s, err := newX11Session()
	if err != nil {
		return err
	}
	defer s.close()
	return s.activate(id)
}

func (x11AppProvider) GetFocused(ctx context.Context) (*domain.Application, error) {
	s, err := newX11Session()
	if err != nil {
		return nil, err
	}
	defer s.close()
	return s.getFocused()
}

// x11Session is one X connection used for a single AppProvider call.
type x11Session struct {
	conn   *xgb.Conn
	screen *xproto.ScreenInfo
}

func newX11Session() (*x11Session, error) {
	conn, err := xgb.NewConnDisplay(os.Getenv("DISPLAY"))
	if err != nil {
		return nil, fmt.Errorf("connect to X11: %w", err)
	}
	return &x11Session{
		conn:   conn,
		screen: xproto.Setup(conn).DefaultScreen(conn),
	}, nil
}

func (s *x11Session) close() {
	s.conn.Close()
}

// listRunning enumerates top-level windows via _NET_CLIENT_LIST, reads
// _NET_WM_PID for each to associate windows with processes, and deduplicates
// by PID. The stable ID for each app is "pid:<PID>".
func (s *x11Session) listRunning() ([]domain.Application, error) {
	windows, err := s.getClientList()
	if err != nil {
		return nil, fmt.Errorf("get client list: %w", err)
	}

	seen := make(map[int]bool)
	var apps []domain.Application

	for _, w := range windows {
		pid := s.getWindowPID(w)
		if pid <= 0 || seen[pid] {
			continue
		}
		seen[pid] = true

		apps = append(apps, domain.Application{
			ID:         fmt.Sprintf("pid:%d", pid),
			Name:       s.getWindowName(w),
			PlatformID: fmt.Sprintf("%d", pid),
		})
	}

	return apps, nil
}

// activate brings the application identified by id ("pid:<PID>" from
// listRunning, or a name for direct lookup) to the foreground by finding its
// first top-level window and sending _NET_ACTIVE_WINDOW.
func (s *x11Session) activate(id string) error {
	targetPID := -1

	if strings.HasPrefix(id, "pid:") {
		if n, _ := fmt.Sscanf(id, "pid:%d", &targetPID); n != 1 {
			targetPID = -1
		}
	} else {
		targetPID = s.lookupPIDByName(strings.ToLower(id))
	}

	if targetPID < 0 {
		return fmt.Errorf("application %q: %w", id, display.ErrAppNotFound)
	}

	window, ok := s.findWindowByPID(targetPID)
	if !ok {
		return fmt.Errorf("application %q: %w", id, display.ErrAppNotFound)
	}

	return s.activateWindow(window)
}

// getFocused returns the currently focused (active) application, or nil when
// no window is focused.
func (s *x11Session) getFocused() (*domain.Application, error) {
	window, err := s.getActiveWindow()
	if err != nil || window == 0 {
		return nil, nil
	}

	pid := s.getWindowPID(window)
	if pid <= 0 {
		return nil, nil
	}

	return &domain.Application{
		ID:         fmt.Sprintf("pid:%d", pid),
		Name:       s.getWindowName(window),
		PlatformID: fmt.Sprintf("%d", pid),
	}, nil
}

// --- X11 atom / property helpers ---

func (s *x11Session) getAtom(name string) (xproto.Atom, error) {
	reply, err := xproto.InternAtom(s.conn, false, uint16(len(name)), name).Reply()
	if err != nil {
		return 0, err
	}
	return reply.Atom, nil
}

// getClientList reads the _NET_CLIENT_LIST property from the root window.
func (s *x11Session) getClientList() ([]xproto.Window, error) {
	atom, err := s.getAtom("_NET_CLIENT_LIST")
	if err != nil {
		return nil, err
	}

	reply, err := xproto.GetProperty(s.conn, false, s.screen.Root, atom, xproto.AtomWindow, 0, 1024).Reply()
	if err != nil {
		return nil, err
	}
	if reply.Format != 32 {
		return nil, nil
	}

	windows := make([]xproto.Window, reply.ValueLen)
	for i := range windows {
		windows[i] = xproto.Window(xgb.Get32(reply.Value[i*4:]))
	}
	return windows, nil
}

// getWindowPID reads _NET_WM_PID for a window. Returns -1 on failure.
func (s *x11Session) getWindowPID(window xproto.Window) int {
	atom, err := s.getAtom("_NET_WM_PID")
	if err != nil {
		return -1
	}

	reply, err := xproto.GetProperty(s.conn, false, window, atom, xproto.AtomCardinal, 0, 1).Reply()
	if err != nil || reply.Format != 32 || reply.ValueLen == 0 {
		return -1
	}

	return int(xgb.Get32(reply.Value))
}

// getWindowName reads the window title from _NET_WM_VISIBLE_NAME,
// _NET_WM_NAME, or legacy WM_NAME, in that order.
func (s *x11Session) getWindowName(window xproto.Window) string {
	if name := s.getStringProperty(window, "_NET_WM_VISIBLE_NAME"); name != "" {
		return name
	}
	if name := s.getStringProperty(window, "_NET_WM_NAME"); name != "" {
		return name
	}
	return s.getStringProperty(window, "WM_NAME")
}

func (s *x11Session) getStringProperty(window xproto.Window, propName string) string {
	atom, err := s.getAtom(propName)
	if err != nil {
		return ""
	}
	reply, err := xproto.GetProperty(s.conn, false, window, atom, xproto.GetPropertyTypeAny, 0, 1024).Reply()
	if err != nil || reply.ValueLen == 0 {
		return ""
	}
	return strings.TrimRight(string(reply.Value[:reply.ValueLen]), "\x00")
}

// getActiveWindow reads _NET_ACTIVE_WINDOW from the root window.
func (s *x11Session) getActiveWindow() (xproto.Window, error) {
	atom, err := s.getAtom("_NET_ACTIVE_WINDOW")
	if err != nil {
		return 0, err
	}

	reply, err := xproto.GetProperty(s.conn, false, s.screen.Root, atom, xproto.AtomWindow, 0, 1).Reply()
	if err != nil {
		return 0, err
	}
	if reply.Format != 32 || reply.ValueLen == 0 {
		return 0, nil
	}

	return xproto.Window(xgb.Get32(reply.Value)), nil
}

// lookupPIDByName scans _NET_CLIENT_LIST for a window whose name contains
// targetName (case-insensitive). Returns the first matching PID, or -1.
func (s *x11Session) lookupPIDByName(targetName string) int {
	windows, err := s.getClientList()
	if err != nil {
		return -1
	}
	for _, w := range windows {
		if strings.Contains(strings.ToLower(s.getWindowName(w)), targetName) {
			if pid := s.getWindowPID(w); pid > 0 {
				return pid
			}
		}
	}
	return -1
}

// findWindowByPID scans _NET_CLIENT_LIST for a window belonging to the given PID.
func (s *x11Session) findWindowByPID(pid int) (xproto.Window, bool) {
	windows, err := s.getClientList()
	if err != nil {
		return 0, false
	}
	for _, w := range windows {
		if w != 0 && s.getWindowPID(w) == pid {
			return w, true
		}
	}
	return 0, false
}

// activateWindow asks the window manager to bring the window to the
// foreground via an EWMH _NET_ACTIVE_WINDOW client message sent to the root.
func (s *x11Session) activateWindow(window xproto.Window) error {
	atom, err := s.getAtom("_NET_ACTIVE_WINDOW")
	if err != nil {
		return xproto.SetInputFocusChecked(s.conn, xproto.InputFocusPointerRoot, window, xproto.TimeCurrentTime).Check()
	}

	event := xproto.ClientMessageEvent{
		Format: 32,
		Window: window,
		Type:   atom,
		Data: xproto.ClientMessageDataUnionData32New([]uint32{
			1, // source indication: application
			0, // timestamp: CurrentTime
			0, // requestor's currently active window: none
			0,
			0,
		}),
	}

	return xproto.SendEventChecked(s.conn, false, s.screen.Root,
		xproto.EventMaskSubstructureRedirect|xproto.EventMaskSubstructureNotify,
		string(event.Bytes())).Check()
}

func init() {
	// Register only when X11 is the active display server. The provider is
	// stateless, so no connection is opened until a tool actually uses it.
	if os.Getenv("DISPLAY") == "" || os.Getenv("WAYLAND_DISPLAY") != "" {
		return
	}
	display.RegisterAppProvider(x11AppProvider{})
}
