package display

import (
	"image"
	"os"
	"testing"
)

const driveSessionEnv = "INFER_TEST_DRIVE_GNOME_SESSION"

func TestUseGNOME(t *testing.T) {
	tests := []struct {
		name    string
		wayland string
		desktop string
		want    bool
	}{
		{name: "ubuntu session", wayland: "wayland-0", desktop: "ubuntu:GNOME", want: true},
		{name: "gnome session", wayland: "wayland-0", desktop: "GNOME", want: true},
		{name: "wlroots session", wayland: "wayland-0", desktop: "sway", want: false},
		{name: "x11 session", desktop: "GNOME", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("WAYLAND_DISPLAY", tt.wayland)
			t.Setenv("XDG_CURRENT_DESKTOP", tt.desktop)
			if got := useGNOME(); got != tt.want {
				t.Errorf("useGNOME() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestKeyKeysym(t *testing.T) {
	tests := []struct {
		key     string
		want    uint32
		wantErr bool
	}{
		{key: "enter", want: 0xff0d},
		{key: "f12", want: 0xffc9},
		{key: "l", want: 'l'},
		{key: "é", want: 0xe9},
		{key: "€", want: 0x010020ac},
		{key: "nosuchkey", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got, err := keyKeysym(tt.key)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Errorf("keyKeysym(%q) = %#x, %v, want %#x", tt.key, got, err, tt.want)
			}
		})
	}
}

func TestWheelSteps(t *testing.T) {
	tests := []struct {
		name      string
		direction []any
		axis      uint32
		steps     int32
		wantErr   bool
	}{
		{name: "down", direction: []any{"down"}, axis: verticalAxis, steps: 3},
		{name: "up", direction: []any{"up"}, axis: verticalAxis, steps: -3},
		{name: "right", direction: []any{"right"}, axis: horizontalAxis, steps: 3},
		{name: "left", direction: []any{"left"}, axis: horizontalAxis, steps: -3},
		{name: "default is down", axis: verticalAxis, steps: 3},
		{name: "unknown direction", direction: []any{"sideways"}, wantErr: true},
		{name: "not a name", direction: []any{7}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			axis, steps, err := wheelSteps(3, tt.direction)
			if (err != nil) != tt.wantErr || axis != tt.axis || steps != tt.steps {
				t.Errorf("wheelSteps(3, %v) = %d, %d, %v, want %d, %d", tt.direction, axis, steps, err, tt.axis, tt.steps)
			}
		})
	}
}

func TestClickArguments(t *testing.T) {
	tests := []struct {
		name    string
		args    []any
		button  int32
		double  bool
		wantErr bool
	}{
		{name: "default is one left click", button: evdevLeftButton},
		{name: "right", args: []any{"right", false}, button: evdevRightButton},
		{name: "double middle", args: []any{"center", true}, button: evdevMiddleButton, double: true},
		{name: "unknown button", args: []any{"thumb"}, wantErr: true},
		{name: "button is not a name", args: []any{1}, wantErr: true},
		{name: "double is not a boolean", args: []any{"left", "twice"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			button, double, err := clickArguments(tt.args)
			if (err != nil) != tt.wantErr || button != tt.button || double != tt.double {
				t.Errorf("clickArguments(%v) = %#x, %v, %v, want %#x, %v", tt.args, button, double, err, tt.button, tt.double)
			}
		})
	}
}

func TestToggleArguments(t *testing.T) {
	tests := []struct {
		name    string
		args    []any
		button  int32
		pressed bool
		wantErr bool
	}{
		{name: "default presses the left button", button: evdevLeftButton, pressed: true},
		{name: "a button without a state goes down", args: []any{"right"}, button: evdevRightButton, pressed: true},
		{name: "down", args: []any{"center", "down"}, button: evdevMiddleButton, pressed: true},
		{name: "up", args: []any{"left", "up"}, button: evdevLeftButton},
		{name: "unknown button", args: []any{"thumb", "down"}, wantErr: true},
		{name: "argument is not a name", args: []any{1}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			button, pressed, err := toggleArguments(tt.args)
			if (err != nil) != tt.wantErr || button != tt.button || pressed != tt.pressed {
				t.Errorf("toggleArguments(%v) = %#x, %v, %v, want %#x, %v", tt.args, button, pressed, err, tt.button, tt.pressed)
			}
		})
	}
}

func TestParsePair(t *testing.T) {
	x, y, err := parsePair("[1280,800]")
	if err != nil || x != 1280 || y != 800 {
		t.Errorf("parsePair = %d, %d, %v, want 1280, 800", x, y, err)
	}
	if _, _, err := parsePair(""); err == nil {
		t.Error("parsePair of an empty answer should fail")
	}
}

// TestGnomeSessionIntegration clicks and types in a live GNOME Wayland session, so it
// runs only when asked to, inside a throwaway one such as the computer image.
func TestGnomeSessionIntegration(t *testing.T) {
	if os.Getenv(driveSessionEnv) == "" || !useGNOME() {
		t.Skipf("set %s=1 inside a throwaway GNOME Wayland session", driveSessionEnv)
	}
	robot := gnomeRobot()
	width, height := robot.GetScreenSize()
	if width == 0 || height == 0 {
		t.Fatalf("screen size = %dx%d", width, height)
	}
	if err := robot.Move(width/2, height/2); err != nil {
		t.Fatalf("move: %v", err)
	}
	if x, y := robot.Location(); x != width/2 || y != height/2 {
		t.Errorf("pointer at %d,%d, want %d,%d", x, y, width/2, height/2)
	}
	img, err := robot.CaptureImg(0, 0, width, height)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if got := img.Bounds().Size(); got != (image.Point{X: width, Y: height}) {
		t.Errorf("captured %v, want %dx%d", got, width, height)
	}
	if typed := robot.Type("hi"); typed != 2 {
		t.Errorf("typed %d of 2 characters", typed)
	}
	if err := robot.KeyTap("esc"); err != nil {
		t.Errorf("key tap: %v", err)
	}
	if err := robot.ScrollDir(1, "down"); err != nil {
		t.Errorf("scroll: %v", err)
	}
	if err := robot.Click("left", false); err != nil {
		t.Errorf("click: %v", err)
	}
}
