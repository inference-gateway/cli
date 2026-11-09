package display

import (
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

	keyPause       = 10 * time.Millisecond
	verticalAxis   = uint32(0)
	horizontalAxis = uint32(1)
	unicodeKeysym  = 0x01000000
)

var (
	gnomeButtons = map[string]int32{"left": 0x110, "right": 0x111, "center": 0x112}

	gnomeModifiers = map[string]uint32{"cmd": 0xffeb, "ctrl": 0xffe3, "alt": 0xffe9, "shift": 0xffe1}

	gnomeKeys = map[string]uint32{
		"enter": 0xff0d, "tab": 0xff09, "space": 0x20, "backspace": 0xff08, "delete": 0xffff, "esc": 0xff1b,
		"up": 0xff52, "down": 0xff54, "left": 0xff51, "right": 0xff53,
		"home": 0xff50, "end": 0xff57, "pageup": 0xff55, "pagedown": 0xff56,
		"f1": 0xffbe, "f2": 0xffbf, "f3": 0xffc0, "f4": 0xffc1, "f5": 0xffc2, "f6": 0xffc3,
		"f7": 0xffc4, "f8": 0xffc5, "f9": 0xffc6, "f10": 0xffc7, "f11": 0xffc8, "f12": 0xffc9,
	}
)

// useGNOME reports a GNOME Wayland session. Mutter implements none of the
// wlroots protocols robotgo's Wayland backend speaks, so it gets its own backend.
func useGNOME() bool {
	desktop := strings.ToUpper(os.Getenv("XDG_CURRENT_DESKTOP"))
	return os.Getenv("WAYLAND_DISPLAY") != "" && strings.Contains(desktop, "GNOME")
}

// gnomeSession drives a GNOME Wayland session over the session D-Bus: input
// through mutter's remote desktop session, the screen through the shell.
// The shell answers Eval and Screenshot only in unsafe mode.
type gnomeSession struct {
	open   sync.Once
	err    error
	conn   *dbus.Conn
	remote dbus.BusObject
	stream dbus.ObjectPath
}

func gnomeRobot() robotBackend {
	session := &gnomeSession{}
	return robotBackend{
		GetScreenSize: session.screenSize,
		GetScaleSize:  func(...int) (int, int) { return session.screenSize() },
		CaptureImg:    session.capture,
		Location:      session.location,
		Move:          func(x, y int, _ ...int) error { return session.move(x, y) },
		Click:         session.click,
		ScrollDir:     session.scroll,
		Type:          func(text string, _ ...int) int { return session.typeText(text) },
		KeyTap:        session.keyTap,
	}
}

func (s *gnomeSession) connect() error {
	s.open.Do(func() { s.err = s.start() })
	return s.err
}

func (s *gnomeSession) start() error {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return fmt.Errorf("connect to the session bus: %w", err)
	}
	var remotePath dbus.ObjectPath
	manager := conn.Object(remoteDesktopName, remoteDesktopPath)
	if err := manager.Call(remoteDesktopName+".CreateSession", 0).Store(&remotePath); err != nil {
		return fmt.Errorf("create a remote desktop session: %w", err)
	}
	remote := conn.Object(remoteDesktopName, remotePath)
	sessionID, err := remote.GetProperty(remoteDesktopSession + ".SessionId")
	if err != nil {
		return fmt.Errorf("read the remote desktop session id: %w", err)
	}
	var castPath dbus.ObjectPath
	properties := map[string]dbus.Variant{"remote-desktop-session-id": sessionID}
	cast := conn.Object(screenCastName, screenCastPath)
	if err := cast.Call(screenCastName+".CreateSession", 0, properties).Store(&castPath); err != nil {
		return fmt.Errorf("create a screen cast session: %w", err)
	}
	var stream dbus.ObjectPath
	recording := conn.Object(screenCastName, castPath)
	noOptions := map[string]dbus.Variant{}
	if err := recording.Call(screenCastSession+".RecordMonitor", 0, "", noOptions).Store(&stream); err != nil {
		return fmt.Errorf("record the monitor: %w", err)
	}
	if err := remote.Call(remoteDesktopSession+".Start", 0).Err; err != nil {
		return fmt.Errorf("start the remote desktop session: %w", err)
	}
	s.conn, s.remote, s.stream = conn, remote, stream
	return wakeKeyboard(remote)
}

// wakeKeyboard taps Shift once, as mutter drops the first key a new session sends.
func wakeKeyboard(remote dbus.BusObject) error {
	for _, pressed := range []bool{true, false} {
		shift := gnomeModifiers["shift"]
		if err := remote.Call(remoteDesktopSession+".NotifyKeyboardKeysym", 0, shift, pressed).Err; err != nil {
			return fmt.Errorf("wake the keyboard: %w", err)
		}
		time.Sleep(keyPause)
	}
	return nil
}

func (s *gnomeSession) notify(method string, args ...any) error {
	if err := s.connect(); err != nil {
		return err
	}
	return s.remote.Call(remoteDesktopSession+"."+method, 0, args...).Err
}

// evalPair runs a shell script that answers a JSON pair of numbers.
func (s *gnomeSession) evalPair(script string) (int, int, error) {
	if err := s.connect(); err != nil {
		return 0, 0, err
	}
	var ok bool
	var result string
	if err := s.conn.Object(shellName, shellPath).Call(shellName+".Eval", 0, script).Store(&ok, &result); err != nil {
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

func (s *gnomeSession) screenSize() (int, int) {
	width, height, _ := s.evalPair(monitorSizeScript)
	return width, height
}

func (s *gnomeSession) location() (int, int) {
	x, y, _ := s.evalPair(pointerScript)
	return x, y
}

func (s *gnomeSession) capture(area ...int) (image.Image, error) {
	if err := s.connect(); err != nil {
		return nil, err
	}
	directory, err := os.MkdirTemp("", "infer-screen-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(directory) }()
	filename := filepath.Join(directory, "screen.png")
	if err := s.screenshot(filename, area); err != nil {
		return nil, fmt.Errorf("shell screenshot: %w", err)
	}
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	return png.Decode(file)
}

func (s *gnomeSession) screenshot(filename string, area []int) error {
	shell := s.conn.Object(screenshotName, screenshotPath)
	if len(area) < 4 {
		return shell.Call(screenshotName+".Screenshot", 0, true, false, filename).Err
	}
	x, y, width, height := int32(area[0]), int32(area[1]), int32(area[2]), int32(area[3])
	return shell.Call(screenshotName+".ScreenshotArea", 0, x, y, width, height, false, filename).Err
}

func (s *gnomeSession) move(x, y int) error {
	return s.notify("NotifyPointerMotionAbsolute", string(s.streamPath()), float64(x), float64(y))
}

func (s *gnomeSession) streamPath() dbus.ObjectPath {
	_ = s.connect()
	return s.stream
}

func (s *gnomeSession) click(args ...any) error {
	button, double := clickArguments(args)
	code, ok := gnomeButtons[button]
	if !ok {
		return fmt.Errorf("unknown mouse button %q", button)
	}
	presses := 1
	if double {
		presses = 2
	}
	for range presses {
		if err := s.tap("NotifyPointerButton", code); err != nil {
			return err
		}
	}
	return nil
}

func clickArguments(args []any) (string, bool) {
	button, double := "left", false
	if len(args) > 0 {
		button, _ = args[0].(string)
	}
	if len(args) > 1 {
		double, _ = args[1].(bool)
	}
	return button, double
}

// tap presses and releases one button or key.
func (s *gnomeSession) tap(method string, code any) error {
	if err := s.notify(method, code, true); err != nil {
		return err
	}
	time.Sleep(keyPause)
	err := s.notify(method, code, false)
	time.Sleep(keyPause)
	return err
}

func (s *gnomeSession) scroll(clicks int, direction ...any) error {
	axis, steps := wheelSteps(clicks, direction)
	return s.notify("NotifyPointerAxisDiscrete", axis, steps)
}

func wheelSteps(clicks int, direction []any) (uint32, int32) {
	name := "down"
	if len(direction) > 0 {
		name, _ = direction[0].(string)
	}
	switch name {
	case "up":
		return verticalAxis, int32(-clicks)
	case "left":
		return horizontalAxis, int32(-clicks)
	case "right":
		return horizontalAxis, int32(clicks)
	default:
		return verticalAxis, int32(clicks)
	}
}

func (s *gnomeSession) typeText(text string) int {
	typed := 0
	for _, char := range text {
		if err := s.tap("NotifyKeyboardKeysym", runeKeysym(char)); err != nil {
			return typed
		}
		typed++
	}
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

func (s *gnomeSession) keyTap(key string, modifiers ...any) error {
	keysym, err := keyKeysym(key)
	if err != nil {
		return err
	}
	held, err := modifierKeysyms(modifiers)
	if err != nil {
		return err
	}
	for _, modifier := range held {
		if err := s.notify("NotifyKeyboardKeysym", modifier, true); err != nil {
			return err
		}
	}
	tapErr := s.tap("NotifyKeyboardKeysym", keysym)
	for _, modifier := range held {
		if err := s.notify("NotifyKeyboardKeysym", modifier, false); err != nil {
			return err
		}
	}
	return tapErr
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
			return nil, fmt.Errorf("unknown modifier %q", modifier)
		}
		keysyms = append(keysyms, keysym)
	}
	return keysyms, nil
}
