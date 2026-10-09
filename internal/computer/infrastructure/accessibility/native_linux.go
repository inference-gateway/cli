//go:build linux

package accessibility

import (
	"context"
	"fmt"
	"iter"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	xproto "github.com/jezek/xgb/xproto"
	xgbutil "github.com/jezek/xgbutil"
	ewmh "github.com/jezek/xgbutil/ewmh"
	icccm "github.com/jezek/xgbutil/icccm"

	computerdomain "github.com/inference-gateway/cli/internal/computer/domain"
)

const (
	busDeadline        = 6 * time.Second
	nullPath           = "/org/a11y/atspi/null"
	componentInterface = "org.a11y.atspi.Component"
	actionInterface    = "org.a11y.atspi.Action"
	stateChecked       = 4
	stateEnabled       = 8
	stateFocused       = 12
	stateSelected      = 23
)

// pressActions are the action names press prefers, most preferred first.
var pressActions = []string{"press", "click", "activate"}

var reportedStates = []struct {
	bit  uint
	word string
}{{stateFocused, "focused"}, {stateSelected, "selected"}, {stateChecked, "checked"}}

// accessible references one AT-SPI object: the bus name that serves it and its path.
type accessible struct {
	Bus  string
	Path string
}

// rect is an AT-SPI Component rectangle in screen coordinates.
type rect struct {
	X, Y, Width, Height int32
}

// atspiBus is the part of AT-SPI2 the provider reads and acts on, so tests can fake it.
type atspiBus interface {
	applications(ctx context.Context) ([]accessible, error)
	processID(ctx context.Context, bus string) (uint32, error)
	children(ctx context.Context, node accessible) ([]accessible, error)
	roleName(ctx context.Context, node accessible) (string, error)
	interfaces(ctx context.Context, node accessible) ([]string, error)
	name(ctx context.Context, node accessible) (string, error)
	states(ctx context.Context, node accessible) (uint64, error)
	extents(ctx context.Context, node accessible) (rect, error)
	actionNames(ctx context.Context, node accessible) ([]string, error)
	doAction(ctx context.Context, node accessible, index int) (bool, error)
}

func runNative(req request) ([]computerdomain.UIElement, error) {
	if !x11Session() {
		return nil, fmt.Errorf("%w: AT-SPI needs an X11 session, Wayland is not supported yet", ErrUnsupported)
	}
	pid, err := targetPID(req.Target)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), busDeadline)
	defer cancel()
	bus, err := dialA11yBus(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: no AT-SPI bus (needs at-spi2-core and a session D-Bus): %v", ErrUnavailable, err)
	}
	defer bus.close()
	return run(ctx, bus, pid, req)
}

// x11Session reports an X11-only session, the rule the display backend uses. Under
// Wayland, GetExtents returns window-relative coordinates that pointer actions cannot use.
func x11Session() bool {
	return os.Getenv("WAYLAND_DISPLAY") == "" && os.Getenv("DISPLAY") != ""
}

func run(ctx context.Context, bus atspiBus, pid uint32, req request) ([]computerdomain.UIElement, error) {
	app, err := applicationOf(ctx, bus, pid)
	if err != nil {
		return nil, err
	}
	switch req.Action {
	case "elements":
		return collect(ctx, bus, app), nil
	case "press":
		return nil, press(ctx, bus, app, req.Label)
	default:
		return nil, fmt.Errorf("%w: unknown helper action %q", ErrUnavailable, req.Action)
	}
}

// targetPID resolves a Computer target to the process that owns its window.
func targetPID(target string) (uint32, error) {
	target = strings.TrimSpace(target)
	if value, ok := strings.CutPrefix(target, "pid:"); ok {
		pid, err := strconv.ParseUint(value, 10, 32)
		if err != nil || pid == 0 {
			return 0, fmt.Errorf("%w: invalid target %q", ErrUnavailable, target)
		}
		return uint32(pid), nil
	}
	if target == "dock" || target == "menubar" {
		return 0, fmt.Errorf("%w: target %q exists only on macOS", ErrUnsupported, target)
	}
	x, err := xgbutil.NewConn()
	if err != nil {
		return 0, fmt.Errorf("%w: connect to X11 display: %v", ErrUnavailable, err)
	}
	defer x.Conn().Close()
	window := targetWindow(x, target)
	if window == 0 {
		return 0, fmt.Errorf("%w: no visible window for target %q", ErrUnavailable, target)
	}
	pid, err := ewmh.WmPidGet(x, window)
	if err != nil {
		return 0, fmt.Errorf("%w: the window of target %q has no _NET_WM_PID: %v", ErrUnavailable, target, err)
	}
	return uint32(pid), nil
}

func targetWindow(x *xgbutil.XUtil, target string) xproto.Window {
	if target == "" || target == "frontmost" {
		return FrontmostWindow(x)
	}
	name, _ := strings.CutPrefix(target, "app:")
	if name = strings.TrimSpace(name); name == "" {
		return 0
	}
	return TopmostWindow(x, 0, name)
}

// FrontmostWindow returns the active X11 window, or the topmost client window when no
// window manager publishes _NET_ACTIVE_WINDOW.
func FrontmostWindow(x *xgbutil.XUtil) xproto.Window {
	if active, err := ewmh.ActiveWindowGet(x); err == nil && active != 0 {
		return active
	}
	if windows := clientWindows(x); len(windows) > 0 {
		return windows[0]
	}
	return 0
}

// TopmostWindow returns the topmost client window owned by pid or, when pid is zero,
// whose WM_CLASS matches the application name.
func TopmostWindow(x *xgbutil.XUtil, pid int, name string) xproto.Window {
	for _, window := range clientWindows(x) {
		if windowMatches(x, window, pid, name) {
			return window
		}
	}
	return 0
}

func windowMatches(x *xgbutil.XUtil, window xproto.Window, pid int, name string) bool {
	if pid != 0 {
		owner, err := ewmh.WmPidGet(x, window)
		return err == nil && int(owner) == pid
	}
	class, err := icccm.WmClassGet(x, window)
	return err == nil && (ApplicationNamesMatch(class.Class, name) || ApplicationNamesMatch(class.Instance, name))
}

// clientWindows lists the top-level client windows, topmost first: the window manager's
// stacking list, or the root's mapped children when no window manager keeps one.
func clientWindows(x *xgbutil.XUtil) []xproto.Window {
	windows, err := ewmh.ClientListStackingGet(x)
	if err != nil || len(windows) == 0 {
		windows = unmanagedClientWindows(x)
	}
	slices.Reverse(windows)
	return windows
}

// unmanagedClientWindows returns the viewable root children a window manager would
// manage, skipping override-redirect windows such as popups and toolkit internals.
func unmanagedClientWindows(x *xgbutil.XUtil) []xproto.Window {
	tree, err := xproto.QueryTree(x.Conn(), x.RootWin()).Reply()
	if err != nil {
		return nil
	}
	return slices.DeleteFunc(tree.Children, func(window xproto.Window) bool {
		attributes, err := xproto.GetWindowAttributes(x.Conn(), window).Reply()
		return err != nil || attributes.MapState != xproto.MapStateViewable || attributes.OverrideRedirect
	})
}

// applicationOf returns the AT-SPI application whose bus connection belongs to pid.
func applicationOf(ctx context.Context, bus atspiBus, pid uint32) (accessible, error) {
	apps, err := bus.applications(ctx)
	if err != nil {
		return accessible{}, fmt.Errorf("%w: list AT-SPI applications: %v", ErrUnavailable, err)
	}
	for _, app := range apps {
		if owner, err := bus.processID(ctx, app.Bus); err == nil && owner == pid {
			return app, nil
		}
	}
	return accessible{}, fmt.Errorf("%w: no AT-SPI application for pid %d (NO_AT_BRIDGE=1 or started before the accessibility bus?)", ErrUnavailable, pid)
}

// descendants walks the tree under root depth first. Each reference carries its own bus
// name, so the walk crosses into other processes, such as WebKit's web process.
func descendants(ctx context.Context, bus atspiBus, root accessible) iter.Seq[accessible] {
	return func(yield func(accessible) bool) {
		visited := make(map[accessible]bool)
		var walk func(accessible, int) bool
		walk = func(node accessible, depth int) bool {
			if depth > maxTreeDepth || node.Bus == "" || node.Path == nullPath || visited[node] {
				return true
			}
			visited[node] = true
			if ctx.Err() != nil || !yield(node) {
				return false
			}
			children, _ := bus.children(ctx, node)
			for _, child := range children {
				if !walk(child, depth+1) {
					return false
				}
			}
			return true
		}
		walk(root, 0)
	}
}

// collect returns the compact elements under app in document order.
// ponytail: one D-Bus round trip per attribute. Fan out per child, or read
// org.a11y.atspi.Cache, if big web trees run into busDeadline.
func collect(ctx context.Context, bus atspiBus, app accessible) []computerdomain.UIElement {
	elements := make([]computerdomain.UIElement, 0, 64)
	for node := range descendants(ctx, bus, app) {
		if element, ok := compactElement(ctx, bus, node); ok {
			elements = append(elements, element)
		}
		if len(elements) >= maxTreeElements {
			break
		}
	}
	return elements
}

func compactElement(ctx context.Context, bus atspiBus, node accessible) (computerdomain.UIElement, bool) {
	role, err := bus.roleName(ctx, node)
	if err != nil || role == "" {
		return computerdomain.UIElement{}, false
	}
	interfaces, _ := bus.interfaces(ctx, node)
	box, ok := boxOf(ctx, bus, node, interfaces)
	if !ok {
		return computerdomain.UIElement{}, false
	}
	label, _ := bus.name(ctx, node)
	label = strings.TrimSpace(label)
	actions := reportedActions(actionsOf(ctx, bus, node, interfaces))
	if label == "" && len(actions) == 0 {
		return computerdomain.UIElement{}, false
	}
	states, _ := bus.states(ctx, node)
	return computerdomain.UIElement{Role: role, Label: label, State: stateText(states, actions), BBox: box}, true
}

// boxOf and actionsOf ask only for interfaces node implements. Asking for a missing one
// logs a critical in a GTK app, and aborts it under G_DEBUG=fatal-criticals.
func boxOf(ctx context.Context, bus atspiBus, node accessible, interfaces []string) ([4]int, bool) {
	if !slices.Contains(interfaces, componentInterface) {
		return [4]int{}, false
	}
	return screenBox(bus.extents(ctx, node))
}

func actionsOf(ctx context.Context, bus atspiBus, node accessible, interfaces []string) []string {
	if !slices.Contains(interfaces, actionInterface) {
		return nil
	}
	actions, _ := bus.actionNames(ctx, node)
	return actions
}

func screenBox(r rect, err error) ([4]int, bool) {
	if err != nil || r.Width <= 0 || r.Height <= 0 {
		return [4]int{}, false
	}
	return [4]int{int(r.X), int(r.Y), int(r.X) + int(r.Width), int(r.Y) + int(r.Height)}, true
}

func stateText(states uint64, actions []string) string {
	parts := []string{"disabled"}
	if hasState(states, stateEnabled) {
		parts[0] = "enabled"
	}
	for _, state := range reportedStates {
		if hasState(states, state.bit) {
			parts = append(parts, state.word)
		}
	}
	if len(actions) > 0 {
		parts = append(parts, "actions="+strings.Join(actions, ","))
	}
	return strings.Join(parts, " ")
}

func hasState(states uint64, bit uint) bool {
	return states&(1<<bit) != 0
}

// pressActionIndex returns the index of the action press performs: the most preferred of
// pressActions, else an element's only named action, such as a WebKit link's "jump". It
// is -1 when there is none.
func pressActionIndex(actions []string) int {
	for _, want := range pressActions {
		if i := slices.IndexFunc(actions, func(name string) bool { return strings.EqualFold(name, want) }); i >= 0 {
			return i
		}
	}
	return onlyNamedAction(actions)
}

// onlyNamedAction returns the index of the single action with a name, or -1. WebKit
// gives an object without a verb one action with an empty name.
func onlyNamedAction(actions []string) int {
	only := -1
	for i, name := range actions {
		if name == "" {
			continue
		}
		if only >= 0 {
			return -1
		}
		only = i
	}
	return only
}

// reportedActions lowercases the action names and reports the one press performs as
// "press", so actions=press means the same as it does on macOS.
func reportedActions(actions []string) []string {
	reported := slices.Clone(actions)
	if i := pressActionIndex(actions); i >= 0 {
		reported[i] = "press"
	}
	for i, name := range reported {
		reported[i] = strings.ToLower(name)
	}
	return slices.DeleteFunc(reported, func(name string) bool { return name == "" })
}

// press performs the press action of the first element under app named exactly label.
func press(ctx context.Context, bus atspiBus, app accessible, label string) error {
	if label == "" {
		return fmt.Errorf("%w: label is empty", ErrElementNotFound)
	}
	for node := range descendants(ctx, bus, app) {
		if name, _ := bus.name(ctx, node); strings.TrimSpace(name) != label {
			continue
		}
		interfaces, _ := bus.interfaces(ctx, node)
		actions := actionsOf(ctx, bus, node, interfaces)
		if index := pressActionIndex(actions); index >= 0 {
			return doAction(ctx, bus, node, label, actions[index], index)
		}
	}
	if ctx.Err() != nil {
		return fmt.Errorf("%w: walking the tree for %q: %v", ErrUnavailable, label, ctx.Err())
	}
	return fmt.Errorf("%w: %q", ErrElementNotFound, label)
}

func doAction(ctx context.Context, bus atspiBus, node accessible, label, action string, index int) error {
	done, err := bus.doAction(ctx, node, index)
	if err != nil {
		return fmt.Errorf("%w: DoAction %q on %q: %v", ErrUnavailable, action, label, err)
	}
	if !done {
		return fmt.Errorf("%w: DoAction %q on %q returned false", ErrUnavailable, action, label)
	}
	return nil
}
