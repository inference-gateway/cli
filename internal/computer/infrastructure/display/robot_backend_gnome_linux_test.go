package display

import (
	"image"
	"testing"
)

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
		direction string
		axis      uint32
		steps     int32
	}{
		{direction: "down", axis: verticalAxis, steps: 3},
		{direction: "up", axis: verticalAxis, steps: -3},
		{direction: "right", axis: horizontalAxis, steps: 3},
		{direction: "left", axis: horizontalAxis, steps: -3},
	}
	for _, tt := range tests {
		t.Run(tt.direction, func(t *testing.T) {
			axis, steps := wheelSteps(3, []any{tt.direction})
			if axis != tt.axis || steps != tt.steps {
				t.Errorf("wheelSteps(3, %q) = %d, %d, want %d, %d", tt.direction, axis, steps, tt.axis, tt.steps)
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

// TestGnomeSessionIntegration drives a live GNOME Wayland session: it runs only
// inside one, such as the computer image.
func TestGnomeSessionIntegration(t *testing.T) {
	if !useGNOME() {
		t.Skip("needs a GNOME Wayland session")
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
