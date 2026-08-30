package computer

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	agentinfra "github.com/inference-gateway/cli/internal/agent/infrastructure"
	display "github.com/inference-gateway/cli/internal/computer/infrastructure/display"
)

// Vision image limits of the annotator model class (long edge px, total px);
// region crops above them are downscaled uniformly to fit.
const (
	annotatorMaxLongEdge = 1568.0
	annotatorMaxPixels   = 1_100_000.0
)

// frameSourceLookup is the narrow slice of the Registry the tool needs.
type frameSourceLookup interface {
	FrameSource(name string) (agentdomain.FrameSource, bool)
	FrameSourceNames() []string
}

// GetLatestFrameTool retrieves the latest frame from a named frame source
// (the screen ring buffer or a configured directory source), either as a raw
// image or as annotated text for models without vision.
type GetLatestFrameTool struct {
	config          *config.Config
	formatter       agentinfra.BaseFormatter
	sources         frameSourceLookup
	annotator       agentdomain.ImageAnnotator
	lastCallMu      sync.Mutex
	lastCallTimes   map[string]time.Time
	minCallInterval time.Duration
}

// NewGetLatestFrameTool creates a new tool that reads from the frame sources
func NewGetLatestFrameTool(cfg *config.Config, sources frameSourceLookup, annotator agentdomain.ImageAnnotator) *GetLatestFrameTool {
	minInterval := time.Duration(cfg.ComputerUse.Screenshot.CaptureInterval) * time.Second
	if minInterval < 2*time.Second {
		minInterval = 2 * time.Second
	}

	return &GetLatestFrameTool{
		config:          cfg,
		formatter:       agentinfra.NewBaseFormatter("GetLatestFrame"),
		sources:         sources,
		annotator:       annotator,
		lastCallTimes:   make(map[string]time.Time),
		minCallInterval: minInterval,
	}
}

// Definition returns the tool definition for the LLM
func (t *GetLatestFrameTool) Definition() sdk.ChatCompletionTool {
	description := t.config.Prompts.Tools.GetLatestFrame.Description
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "GetLatestFrame",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"source": map[string]any{
						"type":        "string",
						"description": fmt.Sprintf("Frame source name. Configured sources: %s. Defaults to the only source, or \"screen\" when present.", strings.Join(t.sources.FrameSourceNames(), ", ")),
					},
					"format": map[string]any{
						"type":        "string",
						"enum":        []string{"regular", "annotated"},
						"description": "\"regular\" returns the raw image; \"annotated\" returns a text summary + element list instead of the image. Omitted: annotated when an annotator is configured, regular otherwise.",
					},
					"region": map[string]any{
						"type":        "object",
						"description": "Zoom into a sub-region of the screen (screen source only): the region is re-captured at native resolution, so small UI (Dock icons, dense toolbars) becomes readable. Coordinates are in the same frame space as MouseClick and previous annotations; returned element coordinates are translated back into that space.",
						"properties": map[string]any{
							"x":      map[string]any{"type": "integer"},
							"y":      map[string]any{"type": "integer"},
							"width":  map[string]any{"type": "integer"},
							"height": map[string]any{"type": "integer"},
						},
						"required": []string{"x", "y", "width", "height"},
					},
				},
			},
		},
	}
}

// Execute retrieves the latest frame from the requested source
func (t *GetLatestFrameTool) Execute(ctx context.Context, args map[string]any) (*agentdomain.ToolExecutionResult, error) {
	start := time.Now()
	fail := func(msg string) (*agentdomain.ToolExecutionResult, error) {
		return &agentdomain.ToolExecutionResult{
			ToolName:  "GetLatestFrame",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     msg,
		}, nil
	}

	sourceName, src, errMsg := t.resolveSource(args)
	if errMsg != "" {
		return fail(errMsg)
	}

	if region, ok := parseRegion(args); ok {
		if sourceName != "screen" {
			return fail(fmt.Sprintf("region zoom is only supported for the \"screen\" source, not %q", sourceName))
		}
		if waitMsg := t.checkRateLimit("screen-region"); waitMsg != "" {
			return fail(waitMsg)
		}
		return t.regionResult(ctx, args, region, start, fail)
	}

	if waitMsg := t.checkRateLimit(sourceName); waitMsg != "" {
		return fail(waitMsg)
	}

	frame, err := src.GetLatestFrame()
	if err != nil {
		return fail(err.Error())
	}
	t.recordCall(sourceName)

	attachment := agentdomain.ImageAttachment{
		Data:        frame.Data,
		MimeType:    "image/" + frame.Format,
		DisplayName: "frame-" + sourceName,
		SourcePath:  frame.Path,
	}
	result := agentdomain.FrameToolResult{
		Source: sourceName,
		Width:  frame.Width,
		Height: frame.Height,
		Format: frame.Format,
		Method: frame.Method,
	}

	if t.wantAnnotated(args) {
		return t.annotatedResult(ctx, args, sourceName, attachment, result, start)
	}

	return &agentdomain.ToolExecutionResult{
		ToolName:  "GetLatestFrame",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data:      result,
		Images:    []agentdomain.ImageAttachment{attachment},
	}, nil
}

// resolveSource picks the frame source: explicit name, the only one
// configured, or "screen" when several exist.
func (t *GetLatestFrameTool) resolveSource(args map[string]any) (string, agentdomain.FrameSource, string) {
	names := t.sources.FrameSourceNames()
	if len(names) == 0 {
		return "", nil, "no frame sources available: enable computer_use screenshot streaming or configure vision.sources"
	}

	name, _ := args["source"].(string)
	switch {
	case name != "":
	case len(names) == 1:
		name = names[0]
	default:
		name = "screen"
	}

	src, ok := t.sources.FrameSource(name)
	if !ok {
		return "", nil, fmt.Sprintf("unknown frame source %q: available sources are %s", name, strings.Join(names, ", "))
	}
	return name, src, ""
}

// checkRateLimit enforces the per-source minimum call interval; it returns a
// wait message when the call is too soon. Only successful frame fetches count
// as calls (see recordCall), so a cold-buffer failure never throttles the retry.
func (t *GetLatestFrameTool) checkRateLimit(source string) string {
	t.lastCallMu.Lock()
	defer t.lastCallMu.Unlock()
	if last, ok := t.lastCallTimes[source]; ok {
		if since := time.Since(last); since < t.minCallInterval {
			wait := time.Duration(math.Ceil((t.minCallInterval - since).Seconds())) * time.Second
			return fmt.Sprintf("please wait %v before requesting another frame from %q (last called %v ago)", wait, source, since.Round(time.Second))
		}
	}
	return ""
}

// recordCall stamps the per-source rate limit after a successful frame fetch.
func (t *GetLatestFrameTool) recordCall(source string) {
	t.lastCallMu.Lock()
	defer t.lastCallMu.Unlock()
	t.lastCallTimes[source] = time.Now()
}

// wantAnnotated resolves the format: explicit wins; omitted defaults to
// annotated whenever an annotator is configured.
func (t *GetLatestFrameTool) wantAnnotated(args map[string]any) bool {
	switch format, _ := args["format"].(string); format {
	case "regular":
		return false
	case "annotated":
		return true
	default:
		return t.annotator != nil
	}
}

// annotatedResult annotates the frame and builds a text-only result (the
// annotation replaces the image). Annotation problems never fail the call:
// they degrade to a regular frame with a note.
func (t *GetLatestFrameTool) annotatedResult(ctx context.Context, args map[string]any, sourceName string, attachment agentdomain.ImageAttachment, result agentdomain.FrameToolResult, start time.Time) (*agentdomain.ToolExecutionResult, error) {
	degrade := func(note string) (*agentdomain.ToolExecutionResult, error) {
		result.Note = note
		return &agentdomain.ToolExecutionResult{
			ToolName:  "GetLatestFrame",
			Arguments: args,
			Success:   true,
			Duration:  time.Since(start),
			Data:      result,
			Images:    []agentdomain.ImageAttachment{attachment},
		}, nil
	}

	if t.annotator == nil {
		return degrade("annotation unavailable: set vision.annotator in config.yaml; returned the regular frame")
	}

	annotation, err := t.annotator.AnnotateImage(ctx, attachment, agentdomain.AnnotateOptions{
		Prompt: t.annotationPrompt(sourceName),
		Width:  result.Width,
		Height: result.Height,
	})
	if err != nil {
		return degrade(fmt.Sprintf("annotation failed: %v; returned the regular frame", err))
	}

	result.Annotated = true
	result.Annotation = annotation
	return &agentdomain.ToolExecutionResult{
		ToolName:  "GetLatestFrame",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data:      result,
	}, nil
}

// parseRegion extracts the optional region argument (frame coordinate space).
func parseRegion(args map[string]any) (agentdomain.ScreenRegion, bool) {
	raw, ok := args["region"].(map[string]any)
	if !ok {
		return agentdomain.ScreenRegion{}, false
	}
	num := func(key string) int {
		f, _ := raw[key].(float64)
		return int(f)
	}
	return agentdomain.ScreenRegion{X: num("x"), Y: num("y"), Width: num("width"), Height: num("height")}, true
}

// regionResult re-captures a sub-region of the screen at native resolution.
// Small UI (Dock icons, dense toolbars) is unreadable in the downscaled full
// frame, so the annotator gets the crop at full detail; element coordinates
// are translated back into the standard frame space before returning.
func (t *GetLatestFrameTool) regionResult(ctx context.Context, args map[string]any, region agentdomain.ScreenRegion, start time.Time, fail func(string) (*agentdomain.ToolExecutionResult, error)) (*agentdomain.ToolExecutionResult, error) {
	displayProvider, err := display.DetectDisplay()
	if err != nil {
		return fail(fmt.Sprintf("no compatible display platform detected: %v", err))
	}
	controller, err := displayProvider.GetController()
	if err != nil {
		return fail(fmt.Sprintf("failed to get platform controller: %v", err))
	}
	defer func() { _ = controller.Close() }()

	screenW, screenH, err := controller.GetScreenDimensions(ctx)
	if err != nil {
		return fail(err.Error())
	}
	frameW, frameH := t.config.ComputerUse.Screenshot.FitDims(screenW, screenH)
	if region.Width <= 0 || region.Height <= 0 || region.X < 0 || region.Y < 0 ||
		region.X+region.Width > frameW || region.Y+region.Height > frameH {
		return fail(fmt.Sprintf("region [x=%d y=%d w=%d h=%d] is outside the %dx%d frame coordinate space", region.X, region.Y, region.Width, region.Height, frameW, frameH))
	}

	scale := float64(screenW) / float64(frameW)
	crop := display.Region{
		X:      int(float64(region.X) * scale),
		Y:      int(float64(region.Y) * scale),
		Width:  int(math.Ceil(float64(region.Width) * scale)),
		Height: int(math.Ceil(float64(region.Height) * scale)),
	}
	crop.Width = min(crop.Width, screenW-crop.X)
	crop.Height = min(crop.Height, screenH-crop.Y)

	img, err := controller.CaptureScreen(ctx, &crop)
	if err != nil {
		return fail(fmt.Sprintf("failed to capture region: %v", err))
	}

	cropW, cropH := img.Bounds().Dx(), img.Bounds().Dy()
	if s := math.Min(annotatorMaxLongEdge/float64(max(cropW, cropH)), math.Sqrt(annotatorMaxPixels/float64(cropW*cropH))); s < 1 {
		cropW, cropH = int(float64(cropW)*s), int(float64(cropH)*s)
		img = display.ResizeImage(img, cropW, cropH)
	}

	quality := t.config.ComputerUse.Screenshot.Quality
	if quality <= 0 || quality > 100 {
		quality = 85
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return fail(fmt.Sprintf("failed to encode region: %v", err))
	}
	t.recordCall("screen-region")

	cropPath := filepath.Join(config.ProjectTmpDir(), "screenshots", fmt.Sprintf("region-%d.jpeg", time.Now().UnixNano()))
	if err := os.MkdirAll(filepath.Dir(cropPath), 0755); err != nil {
		cropPath = ""
	} else if err := os.WriteFile(cropPath, buf.Bytes(), 0644); err != nil {
		cropPath = ""
	}

	attachment := agentdomain.ImageAttachment{
		Data:        base64.StdEncoding.EncodeToString(buf.Bytes()),
		MimeType:    "image/jpeg",
		DisplayName: "frame-screen-region",
		SourcePath:  cropPath,
	}
	result := agentdomain.FrameToolResult{
		Source: "screen",
		Width:  frameW,
		Height: frameH,
		Format: "jpeg",
		Method: "region",
		Region: &region,
	}
	success := func() (*agentdomain.ToolExecutionResult, error) {
		return &agentdomain.ToolExecutionResult{
			ToolName:  "GetLatestFrame",
			Arguments: args,
			Success:   true,
			Duration:  time.Since(start),
			Data:      result,
			Images:    []agentdomain.ImageAttachment{attachment},
		}, nil
	}

	if !t.wantAnnotated(args) || t.annotator == nil {
		result.Note = fmt.Sprintf("attached crop is %dx%d px covering frame region [x=%d y=%d w=%d h=%d]; to click something seen in the crop: frame_x = %d + crop_x*%d/%d, frame_y = %d + crop_y*%d/%d", cropW, cropH, region.X, region.Y, region.Width, region.Height, region.X, region.Width, cropW, region.Y, region.Height, cropH)
		return success()
	}

	annotation, err := t.annotator.AnnotateImage(ctx, attachment, agentdomain.AnnotateOptions{
		Prompt: t.annotationPrompt("screen"),
		Width:  cropW,
		Height: cropH,
	})
	if err != nil {
		result.Note = fmt.Sprintf("annotation failed: %v; returned the regular region crop", err)
		return success()
	}

	for i := range annotation.Elements {
		b := &annotation.Elements[i].BBox
		b[0] = region.X + b[0]*region.Width/cropW
		b[2] = region.X + b[2]*region.Width/cropW
		b[1] = region.Y + b[1]*region.Height/cropH
		b[3] = region.Y + b[3]*region.Height/cropH
	}
	result.Annotated = true
	result.Annotation = annotation
	return success()
}

// annotationPrompt resolves the task prompt for a source: per-source override,
// the screen prompt for the screen source, the scene prompt otherwise.
func (t *GetLatestFrameTool) annotationPrompt(source string) string {
	if src, ok := t.config.Vision.Sources[source]; ok && strings.TrimSpace(src.Prompt) != "" {
		return src.Prompt
	}
	if source == "screen" {
		return t.config.Prompts.Vision.Annotator.ScreenSystemPrompt
	}
	return t.config.Prompts.Vision.Annotator.SceneSystemPrompt
}

// Validate checks if the tool arguments are valid
func (t *GetLatestFrameTool) Validate(args map[string]any) error {
	if format, ok := args["format"].(string); ok && format != "" && format != "regular" && format != "annotated" {
		return fmt.Errorf("invalid format %q: must be \"regular\" or \"annotated\"", format)
	}
	if raw, ok := args["region"]; ok {
		obj, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("region must be an object with x, y, width, height")
		}
		for _, key := range []string{"x", "y", "width", "height"} {
			if _, ok := obj[key].(float64); !ok {
				return fmt.Errorf("region.%s must be an integer", key)
			}
		}
	}
	return nil
}

// IsEnabled returns whether this tool is enabled
func (t *GetLatestFrameTool) IsEnabled() bool {
	return len(t.sources.FrameSourceNames()) > 0
}

// FormatResult formats tool execution results for different contexts
func (t *GetLatestFrameTool) FormatResult(result *agentdomain.ToolExecutionResult, formatType agentdomain.FormatterType) string {
	switch formatType {
	case agentdomain.FormatterLLM:
		return t.FormatForLLM(result)
	case agentdomain.FormatterShort:
		return t.FormatPreview(result)
	default:
		return t.FormatForLLM(result)
	}
}

// FormatPreview returns a short preview of the result for UI display
func (t *GetLatestFrameTool) FormatPreview(result *agentdomain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return "Failed to get latest frame"
	}
	data, ok := result.Data.(agentdomain.FrameToolResult)
	if !ok {
		return "Latest frame retrieved"
	}
	if data.Annotated {
		return fmt.Sprintf("Annotated frame from %s: %d element(s)", data.Source, len(data.Annotation.Elements))
	}
	return fmt.Sprintf("Latest frame from %s: %dx%d (%s)", data.Source, data.Width, data.Height, data.Method)
}

// FormatForLLM formats the result for LLM consumption
func (t *GetLatestFrameTool) FormatForLLM(result *agentdomain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return fmt.Sprintf("Error: %s", result.Error)
	}
	data, ok := result.Data.(agentdomain.FrameToolResult)
	if !ok {
		return "Latest frame retrieved successfully. Image is attached."
	}

	if data.Annotated {
		text := agentdomain.AnnotationText(data.Annotation)
		if data.Source == "screen" && len(data.Annotation.Elements) > 0 {
			text += "\nTo interact: pass an element's center (x,y) to MouseClick."
		}
		return text
	}

	msg := fmt.Sprintf("Latest frame from source %q retrieved successfully (%dx%d, format: %s, method: %s).", data.Source, data.Width, data.Height, data.Format, data.Method)
	if data.Note != "" {
		return msg + " " + data.Note
	}
	return msg + " Image is attached."
}

// ShouldCollapseArg determines if an argument should be collapsed in display
func (t *GetLatestFrameTool) ShouldCollapseArg(key string) bool {
	return false
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *GetLatestFrameTool) ShouldAlwaysExpand() bool {
	return false
}
