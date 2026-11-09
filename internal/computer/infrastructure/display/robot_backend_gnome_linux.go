package display

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	dbus "github.com/godbus/dbus/v5"
)

const (
	remoteDesktopName    = "org.gnome.Mutter.RemoteDesktop"
	remoteDesktopPath    = "/org/gnome/Mutter/RemoteDesktop"
	remoteDesktopSession = remoteDesktopName + ".Session"
	screenCastName       = "org.gnome.Mutter.ScreenCast"
	screenCastPath       = "/org/gnome/Mutter/ScreenCast"
	screenCastSession    = screenCastName + ".Session"
	shellName            = "org.gnome.Shell"
	shellPath            = "/org/gnome/Shell"
	screenshotName       = "org.gnome.Shell.Screenshot"
	screenshotPath       = "/org/gnome/Shell/Screenshot"

	monitorSizeScript = "(g => [g.width, g.height])(global.display.get_monitor_geometry(0))"
	pointerScript     = "global.get_pointer().slice(0, 2)"

	shellTimeout   = 5 * time.Second
	keyPause       = 10 * time.Millisecond
	verticalAxis   = uint32(0)
	horizontalAxis = uint32(1)
	unicodeKeysym  = 0x01000000

	evdevLeftButton   = int32(0x110)
	evdevRightButton  = int32(0x111)
	evdevMiddleButton = int32(0x112)
)

var (
	gnomeButtons = map[string]int32{
		"left":   evdevLeftButton,
		"right":  evdevRightButton,
		"center": evdevMiddleButton,
	}

	gnomeModifiers = map[string]uint32{"cmd": 0xffeb, "ctrl": 0xffe3, "alt": 0xffe9, "shift": 0xffe1}

	gnomeKeys = map[string]uint32{
		"enter": 0xff0d, "tab": 0xff09, "space": 0x20, "backspace": 0xff08, "delete": 0xffff, "esc": 0xff1b,
		"up": 0xff52, "down": 0xff54, "left": 0xff51, "right": 0xff53,
		"home": 0xff50, "end": 0xff57, "pageup": 0xff55, "pagedown": 0xff56,
		"f1": 0xffbe, "f2": 0xffbf, "f3": 0xffc0, "f4": 0xffc1, "f5": 0xffc2, "f6": 0xffc3,
		"f7": 0xffc4, "f8": 0xffc5, "f9": 0xffc6, "f10": 0xffc7, "f11": 0xffc8, "f12": 0xffc9,
	}

	wheelTurns = map[string]struct {
		axis uint32
		sign int32
	}{
		"down":  {verticalAxis, 1},
		"up":    {verticalAxis, -1},
		"right": {horizontalAxis, 1},
		"left":  {horizontalAxis, -1},
	}
)

// useGNOME reports a GNOME Wayland session. Mutter implements none of the
// wlroots protocols robotgo's Wayland backend speaks, so it gets its own backend.
func useGNOME() bool {
	desktop := strings.ToUpper(os.Getenv("XDG_CURRENT_DESKTOP"))
	return os.Getenv("WAYLAND_DISPLAY") != "" && strings.Contains(desktop, "GNOME")
}

// gnomeLink is one live connection to the session: the bus, mutter's remote
// desktop session and the screen cast stream absolute pointer motion names.
type gnomeLink struct {
	conn   *dbus.Conn
	remote dbus.BusObject
	stream dbus.ObjectPath
}

// gnomeDesktop drives a GNOME Wayland session over the session D-Bus: input
// through mutter's remote desktop session, the screen through the shell, which
// answers only in unsafe mode. It lives as long as the process: a controller
// is made per action, so closing one must not end the session.
type gnomeDesktop struct {
	linking sync.Mutex
	link    *gnomeLink
	typing  sync.Mutex
}

func gnomeRobot() robotBackend {
	desktop := &gnomeDesktop{}
	return robotBackend{
		GetScreenSize: desktop.screenSize,
		GetScaleSize:  func(...int) (int, int) { return desktop.screenSize() },
		CaptureImg:    desktop.capture,
		Location:      desktop.location,
		Move:          func(x, y int, _ ...int) error { return desktop.move(x, y) },
		Click:         desktop.click,
		ScrollDir:     desktop.scroll,
		Type:          func(text string, _ ...int) int { return desktop.typeText(text) },
		KeyTap:        desktop.keyTap,
	}
}

// call sends one D-Bus method and gives a frozen shell shellTimeout to answer.
func call(object dbus.BusObject, method string, args ...any) *dbus.Call {
	ctx, cancel := context.WithTimeout(context.Background(), shellTimeout)
	defer cancel()
	return object.CallWithContext(ctx, method, 0, args...)
}

// linked answers the live link, opening one when there is none. A failed
// opening is not kept, so the next action tries again once the shell is up.
func (d *gnomeDesktop) linked() (*gnomeLink, error) {
	d.linking.Lock()
	defer d.linking.Unlock()
	if d.link != nil {
		return d.link, nil
	}
	link, err := openGnomeLink()
	if err != nil {
		return nil, err
	}
	d.link = link
	return link, nil
}

// drop forgets a link whose session the shell no longer serves, as after a
// shell restart, so the next action opens a new one.
func (d *gnomeDesktop) drop(link *gnomeLink) {
	d.linking.Lock()
	defer d.linking.Unlock()
	if d.link != link {
		return
	}
	d.link = nil
	_ = link.conn.Close()
}

func openGnomeLink() (*gnomeLink, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("connect to the session bus: %w", err)
	}
	link, err := startRemoteDesktop(conn)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return link, nil
}

func startRemoteDesktop(conn *dbus.Conn) (*gnomeLink, error) {
	var remotePath dbus.ObjectPath
	manager := conn.Object(remoteDesktopName, remoteDesktopPath)
	if err := call(manager, remoteDesktopName+".CreateSession").Store(&remotePath); err != nil {
		return nil, fmt.Errorf("create a remote desktop session: %w", err)
	}
	remote := conn.Object(remoteDesktopName, remotePath)
	sessionID, err := remote.GetProperty(remoteDesktopSession + ".SessionId")
	if err != nil {
		return nil, fmt.Errorf("read the remote desktop session id: %w", err)
	}
	stream, err := recordMonitor(conn, sessionID)
	if err != nil {
		return nil, err
	}
	if err := call(remote, remoteDesktopSession+".Start").Err; err != nil {
		return nil, fmt.Errorf("start the remote desktop session: %w", err)
	}
	link := &gnomeLink{conn: conn, remote: remote, stream: stream}
	return link, link.wakeKeyboard()
}

func recordMonitor(conn *dbus.Conn, sessionID dbus.Variant) (dbus.ObjectPath, error) {
	var castPath dbus.ObjectPath
	properties := map[string]dbus.Variant{"remote-desktop-session-id": sessionID}
	cast := conn.Object(screenCastName, screenCastPath)
	if err := call(cast, screenCastName+".CreateSession", properties).Store(&castPath); err != nil {
		return "", fmt.Errorf("create a screen cast session: %w", err)
	}
	var stream dbus.ObjectPath
	recording := conn.Object(screenCastName, castPath)
	noOptions := map[string]dbus.Variant{}
	if err := call(recording, screenCastSession+".RecordMonitor", "", noOptions).Store(&stream); err != nil {
		return "", fmt.Errorf("record the monitor: %w", err)
	}
	return stream, nil
}

func (l *gnomeLink) notify(method string, args ...any) error {
	return call(l.remote, remoteDesktopSession+"."+method, args...).Err
}

// wakeKeyboard taps Shift once: the first key a new session sent was dropped
// in every run against mutter 50.
func (l *gnomeLink) wakeKeyboard() error {
	if err := l.tap("NotifyKeyboardKeysym", gnomeModifiers["shift"]); err != nil {
		return fmt.Errorf("wake the keyboard: %w", err)
	}
	return nil
}

// tap presses and releases one button or key.
func (l *gnomeLink) tap(method string, code any) error {
	if err := l.notify(method, code, true); err != nil {
		return err
	}
	time.Sleep(keyPause)
	err := l.notify(method, code, false)
	time.Sleep(keyPause)
	return err
}

// input runs one whole pointer or keyboard sequence. Sequences never
// interleave, so a person taking over cannot land a key inside the agent's combo.
func (d *gnomeDesktop) input(sequence func(*gnomeLink) error) error {
	d.typing.Lock()
	defer d.typing.Unlock()
	link, err := d.linked()
	if err != nil {
		return err
	}
	if err := sequence(link); err != nil {
		d.drop(link)
		return err
	}
	return nil
}

// evalPair runs a shell script that answers a JSON pair of numbers.
func (d *gnomeDesktop) evalPair(script string) (int, int, error) {
	link, err := d.linked()
	if err != nil {
		return 0, 0, err
	}
	var ok bool
	var result string
	if err := call(link.conn.Object(shellName, shellPath), shellName+".Eval", script).Store(&ok, &result); err != nil {
		return 0, 0, err
	}
	if !ok {
		return 0, 0, errors.New("the shell refused Eval, it needs unsafe mode")
	}
	return parsePair(result)
}

func parsePair(result string) (int, int, error) {
	var pair [2]float64
	if err := json.Unmarshal([]byte(result), &pair); err != nil {
		return 0, 0, fmt.Errorf("unexpected shell answer %q: %w", result, err)
	}
	return int(pair[0]), int(pair[1]), nil
}

// screenSize answers 0x0 when the shell does not: the backend's robotgo shape
// has no error to return, and the controller treats a zero size as unknown.
func (d *gnomeDesktop) screenSize() (int, int) {
	width, height, _ := d.evalPair(monitorSizeScript)
	return width, height
}

func (d *gnomeDesktop) location() (int, int) {
	x, y, _ := d.evalPair(pointerScript)
	return x, y
}

// capture reads the screen from a PNG the shell writes.
// ponytail: one file and one PNG round trip per frame. Read the screen cast's
// PipeWire stream instead once frame rate matters.
func (d *gnomeDesktop) capture(area ...int) (image.Image, error) {
	link, err := d.linked()
	if err != nil {
		return nil, err
	}
	directory, err := os.MkdirTemp("", "infer-screen-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(directory) }()
	filename := filepath.Join(directory, "screen.png")
	if err := screenshot(link.conn, filename, area); err != nil {
		return nil, fmt.Errorf("shell screenshot: %w", err)
	}
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	return png.Decode(file)
}

func screenshot(conn *dbus.Conn, filename string, area []int) error {
	shell := conn.Object(screenshotName, screenshotPath)
	if len(area) < 4 {
		return call(shell, screenshotName+".Screenshot", true, false, filename).Err
	}
	x, y, width, height := int32(area[0]), int32(area[1]), int32(area[2]), int32(area[3])
	return call(shell, screenshotName+".ScreenshotArea", x, y, width, height, false, filename).Err
}

func (d *gnomeDesktop) move(x, y int) error {
	return d.input(func(link *gnomeLink) error {
		return link.notify("NotifyPointerMotionAbsolute", string(link.stream), float64(x), float64(y))
	})
}

func (d *gnomeDesktop) click(args ...any) error {
	button, double, err := clickArguments(args)
	if err != nil {
		return err
	}
	presses := 1
	if double {
		presses = 2
	}
	return d.input(func(link *gnomeLink) error {
		for range presses {
			if err := link.tap("NotifyPointerButton", button); err != nil {
				return err
			}
		}
		return nil
	})
}

// clickArguments reads robotgo's Click arguments: the button name, then whether to double click.
func clickArguments(args []any) (int32, bool, error) {
	name, double := "left", false
	if len(args) > 0 {
		given, ok := args[0].(string)
		if !ok {
			return 0, false, fmt.Errorf("mouse button %v is not a name", args[0])
		}
		name = given
	}
	if len(args) > 1 {
		given, ok := args[1].(bool)
		if !ok {
			return 0, false, fmt.Errorf("double click %v is not a boolean", args[1])
		}
		double = given
	}
	button, ok := gnomeButtons[name]
	if !ok {
		return 0, false, fmt.Errorf("unknown mouse button %q", name)
	}
	return button, double, nil
}

func (d *gnomeDesktop) scroll(clicks int, direction ...any) error {
	axis, steps, err := wheelSteps(clicks, direction)
	if err != nil {
		return err
	}
	return d.input(func(link *gnomeLink) error {
		return link.notify("NotifyPointerAxisDiscrete", axis, steps)
	})
}

// wheelSteps reads robotgo's ScrollDir arguments: a click count and the way the wheel turns.
func wheelSteps(clicks int, direction []any) (uint32, int32, error) {
	name := "down"
	if len(direction) > 0 {
		given, ok := direction[0].(string)
		if !ok {
			return 0, 0, fmt.Errorf("scroll direction %v is not a name", direction[0])
		}
		name = given
	}
	turn, ok := wheelTurns[name]
	if !ok {
		return 0, 0, fmt.Errorf("unknown scroll direction %q", name)
	}
	return turn.axis, turn.sign * int32(clicks), nil
}

func (d *gnomeDesktop) typeText(text string) int {
	typed := 0
	_ = d.input(func(link *gnomeLink) error {
		for _, char := range text {
			if err := link.tap("NotifyKeyboardKeysym", runeKeysym(char)); err != nil {
				return err
			}
			typed++
		}
		return nil
	})
	return typed
}

func runeKeysym(char rune) uint32 {
	switch {
	case char == '\n':
		return gnomeKeys["enter"]
	case char == '\t':
		return gnomeKeys["tab"]
	case char < 0x100:
		return uint32(char)
	default:
		return unicodeKeysym | uint32(char)
	}
}

func (d *gnomeDesktop) keyTap(key string, modifiers ...any) error {
	keysym, err := keyKeysym(key)
	if err != nil {
		return err
	}
	held, err := modifierKeysyms(modifiers)
	if err != nil {
		return err
	}
	return d.input(func(link *gnomeLink) error { return link.combo(keysym, held) })
}

// combo taps a key with modifiers held, and lets go of every modifier it
// pressed even when a step fails, so none stays down in the session.
func (l *gnomeLink) combo(keysym uint32, modifiers []uint32) (err error) {
	pressed := 0
	defer func() {
		for _, modifier := range modifiers[:pressed] {
			err = errors.Join(err, l.notify("NotifyKeyboardKeysym", modifier, false))
		}
	}()
	for _, modifier := range modifiers {
		if err := l.notify("NotifyKeyboardKeysym", modifier, true); err != nil {
			return err
		}
		pressed++
	}
	return l.tap("NotifyKeyboardKeysym", keysym)
}

func keyKeysym(key string) (uint32, error) {
	if keysym, ok := gnomeKeys[key]; ok {
		return keysym, nil
	}
	chars := []rune(key)
	if len(chars) != 1 {
		return 0, fmt.Errorf("unknown key %q", key)
	}
	return runeKeysym(chars[0]), nil
}

func modifierKeysyms(modifiers []any) ([]uint32, error) {
	keysyms := make([]uint32, 0, len(modifiers))
	for _, modifier := range modifiers {
		name, _ := modifier.(string)
		keysym, ok := gnomeModifiers[name]
		if !ok {
			return nil, fmt.Errorf("unknown modifier %v", modifier)
		}
		keysyms = append(keysyms, keysym)
	}
	return keysyms, nil
}
