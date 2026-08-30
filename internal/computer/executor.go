package computer

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"time"

	config "github.com/inference-gateway/cli/config"
	computerdomain "github.com/inference-gateway/cli/internal/computer/domain"
	accessibility "github.com/inference-gateway/cli/internal/computer/infrastructure/accessibility"
	display "github.com/inference-gateway/cli/internal/computer/infrastructure/display"
)

const maxTypeTextLength = 10000

// Executor performs computer-use actions through the display controller.
// Action and observation coordinates are in the frame coordinate space (the
// space screenshots and their annotations use).
type Executor struct {
	cfg           *config.Config
	accessibility accessibility.Provider
}

// NewExecutor creates an Executor.
func NewExecutor(cfg *config.Config) *Executor {
	return newExecutor(cfg, accessibility.NewProvider())
}

func newExecutor(cfg *config.Config, provider accessibility.Provider) *Executor {
	return &Executor{cfg: cfg, accessibility: provider}
}

// Do executes one action and returns the resulting observation.
//
//nolint:cyclop // The single switch keeps all Computer action dispatch in one auditable place.
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
	case computerdomain.ActionAccessibility:
		e.observeAccessibility(ctx, a.Scope, obs, screenW, screenH)
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
	case computerdomain.ActionPress:
		e.pressAccessibility(ctx, a.Scope, a.Label, obs)
	default:
		return nil, fmt.Errorf("unknown action %q", a.Kind)
	}
	if err != nil {
		return nil, err
	}
	return obs, nil
}

func (e *Executor) observeAccessibility(ctx context.Context, target string, obs *computerdomain.Observation, screenW, screenH int) {
	target = defaultAccessibilityTarget(target)
	elements, err := e.accessibility.Elements(ctx, target)
	if err != nil {
		obs.Message = accessibilityFallback("Accessibility tree unavailable", err)
		return
	}
	if len(elements) == 0 {
		obs.Message = "Accessibility tree returned no useful elements. Use the Computer screenshot action as fallback."
		return
	}
	scaleAccessibilityElements(elements, obs.Width, obs.Height, screenW, screenH)
	obs.Elements = elements
	obs.Message = fmt.Sprintf("accessibility tree for %s: %d elements in the %dx%d frame space", target, len(elements), obs.Width, obs.Height)
}

func (e *Executor) pressAccessibility(ctx context.Context, target, label string, obs *computerdomain.Observation) {
	target = defaultAccessibilityTarget(target)
	if err := e.accessibility.Press(ctx, target, label); err != nil {
		obs.Message = accessibilityFallback("Nothing was pressed", err)
		return
	}
	obs.Message = fmt.Sprintf("pressed accessibility element %q in %s without taking a screenshot", label, target)
}

func defaultAccessibilityTarget(target string) string {
	if target == "" {
		return "frontmost"
	}
	return target
}

func accessibilityFallback(prefix string, err error) string {
	detail := err.Error()
	switch {
	case errors.Is(err, accessibility.ErrPermission):
		detail = "macOS Accessibility permission is not granted to infer"
	case errors.Is(err, accessibility.ErrUnsupported):
		detail = "the platform accessibility provider is not implemented"
	case errors.Is(err, accessibility.ErrElementNotFound):
		detail = "no pressable element matched that label"
	}
	return prefix + ": " + detail + ". Use the Computer screenshot action as fallback."
}

func scaleAccessibilityElements(elements []computerdomain.UIElement, frameW, frameH, screenW, screenH int) {
	if frameW == screenW && frameH == screenH {
		return
	}
	scaleX := float64(frameW) / float64(screenW)
	scaleY := float64(frameH) / float64(screenH)
	for i := range elements {
		elements[i].BBox[0] = int(math.Round(float64(elements[i].BBox[0]) * scaleX))
		elements[i].BBox[1] = int(math.Round(float64(elements[i].BBox[1]) * scaleY))
		elements[i].BBox[2] = int(math.Round(float64(elements[i].BBox[2]) * scaleX))
		elements[i].BBox[3] = int(math.Round(float64(elements[i].BBox[3]) * scaleY))
	}
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

	path := filepath.Join(config.ProjectTmpDir(), "screenshots", fmt.Sprintf("computer-%d.jpeg", time.Now().UnixNano()))
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
