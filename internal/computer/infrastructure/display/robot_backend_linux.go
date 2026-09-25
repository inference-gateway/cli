package display

import (
	"os"

	rgwayland "github.com/go-vgo/robotgo/wayland"
	rgx11 "github.com/go-vgo/robotgo/x11"
)

var robot = linuxRobot()

func linuxRobot() robotBackend {
	if useX11() {
		return robotBackend{
			GetScreenSize: rgx11.GetScreenSize,
			GetScaleSize:  rgx11.GetScaleSize,
			CaptureImg:    rgx11.CaptureImg,
			Location:      rgx11.Location,
			Move:          rgx11.Move,
			Click:         rgx11.Click,
			ScrollDir:     rgx11.ScrollDir,
			Type:          rgx11.Type,
			KeyTap:        rgx11.KeyTap,
		}
	}
	return robotBackend{
		GetScreenSize: rgwayland.GetScreenSize,
		GetScaleSize:  rgwayland.GetScaleSize,
		CaptureImg:    rgwayland.CaptureImg,
		Location:      rgwayland.Location,
		Move:          rgwayland.Move,
		Click:         rgwayland.Click,
		ScrollDir:     rgwayland.ScrollDir,
		Type:          rgwayland.Type,
		KeyTap:        rgwayland.KeyTap,
	}
}

// useX11 reports an X11-only session. XWayland also sets DISPLAY, so any
// WAYLAND_DISPLAY keeps robotgo's native Wayland backend (the old default).
func useX11() bool {
	return os.Getenv("WAYLAND_DISPLAY") == "" && os.Getenv("DISPLAY") != ""
}
