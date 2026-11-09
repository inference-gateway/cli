package accessibility

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	dbus "github.com/godbus/dbus/v5"

	computerdomain "github.com/inference-gateway/cli/internal/computer/domain"
)

const (
	shellName     = "org.gnome.Shell"
	shellPath     = "/org/gnome/Shell"
	windowsScript = `global.get_window_actors().map(a => a.meta_window).filter(w => !w.minimized).map(w => {
		const r = w.get_frame_rect();
		return {pid: w.get_pid(), app: w.get_wm_class() || "", x: r.x, y: r.y, focused: w.has_focus()};
	})`
)

// gnomeWindow is one window as the shell places it, bottom of the stack first.
type gnomeWindow struct {
	PID     uint32 `json:"pid"`
	App     string `json:"app"`
	X       int    `json:"x"`
	Y       int    `json:"y"`
	Focused bool   `json:"focused"`
}

// gnomeSession reports a GNOME Wayland session, the rule the display backend uses.
func gnomeSession() bool {
	desktop := strings.ToUpper(os.Getenv("XDG_CURRENT_DESKTOP"))
	return os.Getenv("WAYLAND_DISPLAY") != "" && strings.Contains(desktop, "GNOME")
}

// gnomeWindows asks the shell for its windows, which it answers only in unsafe mode.
func gnomeWindows() ([]gnomeWindow, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("connect to the session bus: %w", err)
	}
	defer func() { _ = conn.Close() }()
	var ok bool
	var result string
	if err := conn.Object(shellName, shellPath).Call(shellName+".Eval", 0, windowsScript).Store(&ok, &result); err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("the shell refused Eval, it needs unsafe mode")
	}
	var windows []gnomeWindow
	if err := json.Unmarshal([]byte(result), &windows); err != nil {
		return nil, fmt.Errorf("unexpected shell answer %q: %w", result, err)
	}
	return windows, nil
}

// pickWindow resolves a Computer target to its topmost window. A pid without a
// window stands at the screen origin, which is where the shell's own tree lives.
func pickWindow(windows []gnomeWindow, target string) (gnomeWindow, error) {
	target = strings.TrimSpace(target)
	if target == "dock" || target == "menubar" {
		return gnomeWindow{}, fmt.Errorf("%w: target %q exists only on macOS", ErrUnsupported, target)
	}
	matches := windowMatcher(target)
	if value, ok := strings.CutPrefix(target, "pid:"); ok {
		pid, err := strconv.ParseUint(value, 10, 32)
		if err != nil || pid == 0 {
			return gnomeWindow{}, fmt.Errorf("%w: invalid target %q", ErrUnavailable, target)
		}
		matches = func(window gnomeWindow) bool { return window.PID == uint32(pid) }
		windows = append([]gnomeWindow{{PID: uint32(pid)}}, windows...)
	}
	for i := len(windows) - 1; i >= 0; i-- {
		if matches(windows[i]) {
			return windows[i], nil
		}
	}
	return gnomeWindow{}, fmt.Errorf("%w: no visible window for target %q", ErrUnavailable, target)
}

func windowMatcher(target string) func(gnomeWindow) bool {
	if target == "" || target == "frontmost" {
		return func(window gnomeWindow) bool { return window.Focused }
	}
	name, _ := strings.CutPrefix(target, "app:")
	name = strings.ToLower(strings.TrimSpace(name))
	return func(window gnomeWindow) bool {
		return name != "" && strings.Contains(strings.ToLower(window.App), name)
	}
}

// placeOnScreen moves window-relative boxes, which is what AT-SPI answers under
// Wayland, to where the window stands on the screen.
// ponytail: one origin per application, so a dialog or popover of its own surface is
// off by its offset. Match each AT-SPI frame to its shell window when that bites.
func placeOnScreen(elements []computerdomain.UIElement, window gnomeWindow) []computerdomain.UIElement {
	for i := range elements {
		box := &elements[i].BBox
		box[0], box[2] = box[0]+window.X, box[2]+window.X
		box[1], box[3] = box[1]+window.Y, box[3]+window.Y
	}
	return elements
}
