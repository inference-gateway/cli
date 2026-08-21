package computer

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"time"

	config "github.com/inference-gateway/cli/config"
	computerdomain "github.com/inference-gateway/cli/internal/computer/domain"
	display "github.com/inference-gateway/cli/internal/computer/infrastructure/display"
)

const maxTypeTextLength = 10000

// Executor performs computer-use actions through the display controller.
// Action and observation coordinates are in the frame coordinate space (the
// space screenshots and their annotations use).
type Executor struct {
	cfg *config.Config
}

// NewExecutor creates an Executor.
func NewExecutor(cfg *config.Config) *Executor {
	return &Executor{cfg: cfg}
}

// Do executes one action and returns the resulting observation.
func (e *Executor) Do(ctx context.Context, a computerdomain.Action) (*computerdomain.Observation, error) {
	if err := acquireScreenLock(); err != nil {
		return nil, err
	}
	provider, err := display.DetectDisplay()
	if err != nil {
		return nil, err
	}
	controller, err := provider.GetController()
	if err != nil {
		return nil, err
	}
	defer func() { _ = controller.Close() }()

	screenW, screenH, err := controller.GetScreenDimensions(ctx)
	if err != nil {
		return nil, err
	}
	if screenW <= 0 || screenH <= 0 {
		return nil, fmt.Errorf("no usable display (reported %dx%d)", screenW, screenH)
	}
	frameW, frameH := e.cfg.ComputerUse.Screenshot.FitDims(screenW, screenH)
	obs := &computerdomain.Observation{Width: frameW, Height: frameH}

	switch a.Kind {
	case computerdomain.ActionScreenshot:
		err = e.screenshot(ctx, controller, a.Target, obs, screenW, screenH)
	case computerdomain.ActionCursor:
		var x, y int
		if x, y, err = controller.GetCursorPosition(ctx); err == nil {
			obs.CursorX, obs.CursorY = x*frameW/screenW, y*frameH/screenH
			obs.Message = fmt.Sprintf("cursor at (%d, %d) in the %dx%d frame space", obs.CursorX, obs.CursorY, frameW, frameH)
		}
	case computerdomain.ActionMove, computerdomain.ActionClick, computerdomain.ActionDoubleClick, computerdomain.ActionTripleClick:
		err = e.pointer(ctx, controller, a, obs, screenW, screenH)
	case computerdomain.ActionScroll:
		amount := a.Amount
		if amount == 0 {
			amount = 3
		}
		direction := a.Direction
		if direction == "" {
			direction = "vertical"
		}
		if err = controller.ScrollMouse(ctx, amount, direction); err == nil {
			obs.Message = fmt.Sprintf("scrolled %s by %d", direction, amount)
		}
	case computerdomain.ActionType:
		if a.Text == "" {
			return nil, fmt.Errorf("type action requires text")
		}
		if len(a.Text) > maxTypeTextLength {
			return nil, fmt.Errorf("text exceeds the %d character limit", maxTypeTextLength)
		}
		if err = controller.TypeText(ctx, a.Text, 0); err == nil {
			obs.Message = fmt.Sprintf("typed %d characters", len(a.Text))
		}
	case computerdomain.ActionKey:
		if a.Combo == "" {
			return nil, fmt.Errorf("key action requires combo")
		}
		if err = controller.SendKeyCombo(ctx, a.Combo); err == nil {
			obs.Message = "pressed " + a.Combo
		}
	default:
		return nil, fmt.Errorf("unknown action %q", a.Kind)
	}
	if err != nil {
		return nil, err
	}
	return obs, nil
}

// pointer handles move and the click actions: scale the frame-space target to
// screen space, move there, then click when requested.
func (e *Executor) pointer(ctx context.Context, controller display.DisplayController, a computerdomain.Action, obs *computerdomain.Observation, screenW, screenH int) error {
	if a.Target == nil {
		return fmt.Errorf("action %q requires x and y", a.Kind)
	}
	x, y, err := ScaleAPIToScreen(a.Target.X, a.Target.Y, obs.Width, obs.Height, screenW, screenH)
	if err != nil {
		return err
	}
	if err := controller.MoveMouse(ctx, x, y); err != nil {
		return err
	}
	obs.CursorX, obs.CursorY = a.Target.X, a.Target.Y

	if a.Kind == computerdomain.ActionMove {
		obs.Message = fmt.Sprintf("moved cursor to (%d, %d)", a.Target.X, a.Target.Y)
		return nil
	}

	clicks := 1
	switch a.Kind {
	case computerdomain.ActionDoubleClick:
		clicks = 2
	case computerdomain.ActionTripleClick:
		clicks = 3
	}
	button := display.ParseMouseButton(a.Button)
	if err := controller.ClickMouse(ctx, button, clicks); err != nil {
		return err
	}
	obs.Message = fmt.Sprintf("%s at (%d, %d) with the %s button", a.Kind, a.Target.X, a.Target.Y, button)
	return nil
}

// captureRegion re-captures a frame-space rectangle at native resolution,
// downscaled only when it exceeds the annotator image limits.
func captureRegion(ctx context.Context, controller display.DisplayController, r *computerdomain.Region, frameW, frameH, screenW, screenH int) (image.Image, error) {
	if r.Width <= 0 || r.Height <= 0 || r.X < 0 || r.Y < 0 || r.X+r.Width > frameW || r.Y+r.Height > frameH {
		return nil, fmt.Errorf("region [x=%d y=%d w=%d h=%d] is outside the %dx%d frame space", r.X, r.Y, r.Width, r.Height, frameW, frameH)
	}
	crop := display.Region{
		X:      r.X * screenW / frameW,
		Y:      r.Y * screenH / frameH,
		Width:  int(math.Ceil(float64(r.Width) * float64(screenW) / float64(frameW))),
		Height: int(math.Ceil(float64(r.Height) * float64(screenH) / float64(frameH))),
	}
	crop.Width = min(crop.Width, screenW-crop.X)
	crop.Height = min(crop.Height, screenH-crop.Y)

	img, err := controller.CaptureScreen(ctx, &crop)
	if err != nil {
		return nil, err
	}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	if s := math.Min(annotatorMaxLongEdge/float64(max(w, h)), math.Sqrt(annotatorMaxPixels/float64(w*h))); s < 1 {
		img = display.ResizeImage(img, int(float64(w)*s), int(float64(h)*s))
	}
	return img, nil
}

// screenshot captures the screen (or a frame-space region of it at native
// resolution), stores a JPEG on disk for non-vision models, and attaches it
// to the observation.
func (e *Executor) screenshot(ctx context.Context, controller display.DisplayController, target *computerdomain.Target, obs *computerdomain.Observation, screenW, screenH int) error {
	frameW, frameH := obs.Width, obs.Height

	var img image.Image
	var err error
	if target != nil && target.Region != nil {
		img, err = captureRegion(ctx, controller, target.Region, frameW, frameH, screenW, screenH)
		obs.Message = fmt.Sprintf("captured region [x=%d y=%d w=%d h=%d] of the %dx%d frame space", target.Region.X, target.Region.Y, target.Region.Width, target.Region.Height, frameW, frameH)
	} else {
		img, err = controller.CaptureScreen(ctx, nil)
		if err == nil && (img.Bounds().Dx() != frameW || img.Bounds().Dy() != frameH) {
			img = display.ResizeImage(img, frameW, frameH)
		}
		obs.Message = fmt.Sprintf("captured the %dx%d frame", frameW, frameH)
	}
	if err != nil {
		return err
	}

	quality := e.cfg.ComputerUse.Screenshot.Quality
	if quality <= 0 || quality > 100 {
		quality = 85
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return fmt.Errorf("failed to encode screenshot: %w", err)
	}

	path := filepath.Join(e.cfg.GetConfigDir(), "tmp", "screenshots", fmt.Sprintf("computer-%d.jpeg", time.Now().UnixNano()))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		path = ""
	} else if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		path = ""
	}

	obs.Image = &computerdomain.Image{
		Data:     base64.StdEncoding.EncodeToString(buf.Bytes()),
		MimeType: "image/jpeg",
		Path:     path,
	}
	return nil
}
