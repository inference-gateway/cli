//go:build !linux

package display

import (
	robotgo "github.com/go-vgo/robotgo"
)

var robot = robotBackend{
	GetScreenSize: robotgo.GetScreenSize,
	GetScaleSize:  robotgo.GetScaleSize,
	CaptureImg:    robotgo.CaptureImg,
	Location:      robotgo.Location,
	Move:          robotgo.Move,
	Click:         robotgo.Click,
	Toggle:        robotgo.Toggle,
	ScrollDir:     robotgo.ScrollDir,
	Type:          robotgo.Type,
	KeyTap:        robotgo.KeyTap,
}
