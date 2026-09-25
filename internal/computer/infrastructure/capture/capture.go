package capture

import (
	"fmt"
	"strconv"
	"strings"
)

// Screen is the primary display: its logical size and the size in pixels
// the platform screen grabber captures (larger on scaled/HiDPI displays).
type Screen struct {
	Width, Height             int
	NativeWidth, NativeHeight int
}

// target is a parsed window target: frontmost, pid:<n>, app:<name>, or a
// bare application name (the Computer tool's target syntax).
type target struct {
	frontmost bool
	pid       int
	name      string
}

func parseTarget(s string) (target, error) {
	s = strings.TrimSpace(s)
	switch s {
	case "", "frontmost":
		return target{frontmost: true}, nil
	case "dock", "menubar":
		return target{}, fmt.Errorf("window %q is not a window; use frontmost, pid:<number>, app:<name>, or an application name", s)
	}
	if v, ok := strings.CutPrefix(s, "pid:"); ok {
		pid, err := strconv.Atoi(v)
		if err != nil || pid <= 0 {
			return target{}, fmt.Errorf("invalid window %q: pid must be a positive number", s)
		}
		return target{pid: pid}, nil
	}
	name, _ := strings.CutPrefix(s, "app:")
	if name = strings.TrimSpace(name); name == "" {
		return target{}, fmt.Errorf("invalid window %q: application name is empty", s)
	}
	return target{name: name}, nil
}

func errNoWindow(window string) error {
	return fmt.Errorf("no visible window matches %q; check the name with the Computer tool's accessibility action, or record mode=region instead", window)
}
