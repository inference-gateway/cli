package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
	xdraw "golang.org/x/image/draw"
)

// ScreenshotTool captures screenshots of the display
type ScreenshotTool struct {
	config       *config.Config
	enabled      bool
	formatter    domain.BaseFormatter
	imageService domain.ImageService
	rateLimiter  *RateLimiter
}

// NewScreenshotTool creates a new screenshot tool
func NewScreenshotTool(cfg *config.Config, imageService domain.ImageService, rateLimiter *RateLimiter) *ScreenshotTool {
	return &ScreenshotTool{
		config:       cfg,
		enabled:      cfg.ComputerUse.Enabled && cfg.ComputerUse.Screenshot.Enabled,
		formatter:    domain.NewBaseFormatter("Screenshot"),
		imageService: imageService,
		rateLimiter:  rateLimiter,
	}
}

// Definition returns the tool definition for the LLM
func (t *ScreenshotTool) Definition() sdk.ChatCompletionTool {
	description := "Captures a screenshot of the display. This is a read-only operation that does NOT require approval. Can capture the entire screen or a specific region."
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "Screenshot",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"region": map[string]any{
						"type":        "object",
						"description": "Optional region to capture. If not specified, captures the entire screen.",
						"properties": map[string]any{
							"x": map[string]any{
								"type":        "integer",
								"description": "X coordinate of the top-left corner",
							},
							"y": map[string]any{
								"type":        "integer",
								"description": "Y coordinate of the top-left corner",
							},
							"width": map[string]any{
								"type":        "integer",
								"description": "Width of the region",
							},
							"height": map[string]any{
								"type":        "integer",
								"description": "Height of the region",
							},
						},
					},
					"display": map[string]any{
						"type":        "string",
						"description": "Display to capture from (e.g., ':0'). Defaults to ':0'.",
						"default":     ":0",
					},
				},
			},
		},
	}
}

// Execute runs the screenshot tool with given arguments
func (t *ScreenshotTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	start := time.Now()

	if t.rateLimiter != nil {
		if err := t.rateLimiter.CheckAndRecord("Screenshot"); err != nil {
			return &domain.ToolExecutionResult{
				ToolName:  "Screenshot",
				Arguments: args,
				Success:   false,
				Duration:  time.Since(start),
				Error:     err.Error(),
			}, nil
		}
	}

	display := t.config.ComputerUse.Display
	if displayArg, ok := args["display"].(string); ok && displayArg != "" {
		display = displayArg
	}

	region, x, y, width, height := parseRegionArgs(args)

	displayServer := DetectDisplayServer()
	method := displayServer.String()

	var imageBytes []byte
	var captureWidth, captureHeight int
	var err error

	switch displayServer {
	case DisplayServerX11:
		imageBytes, captureWidth, captureHeight, err = t.captureX11(display, x, y, width, height)
	case DisplayServerWayland:
		imageBytes, captureWidth, captureHeight, err = t.captureWayland(display, x, y, width, height)
	default:
		return &domain.ToolExecutionResult{
			ToolName:  "Screenshot",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "no display server detected (neither X11 nor Wayland)",
		}, nil
	}

	if err != nil {
		return &domain.ToolExecutionResult{
			ToolName:  "Screenshot",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     err.Error(),
		}, nil
	}

	optimized, err := t.optimizeScreenshot(imageBytes)
	if err != nil {
		return &domain.ToolExecutionResult{
			ToolName:  "Screenshot",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     fmt.Sprintf("failed to optimize screenshot: %v", err),
		}, nil
	}

	base64Data := base64.StdEncoding.EncodeToString(optimized)

	mimeType := "image/" + t.config.ComputerUse.Screenshot.Format
	imageAttachment := domain.ImageAttachment{
		Data:        base64Data,
		MimeType:    mimeType,
		DisplayName: fmt.Sprintf("screenshot-%s", display),
	}

	result := domain.ScreenshotToolResult{
		Display: display,
		Region:  region,
		Width:   captureWidth,
		Height:  captureHeight,
		Format:  t.config.ComputerUse.Screenshot.Format,
		Method:  method,
	}

	return &domain.ToolExecutionResult{
		ToolName:  "Screenshot",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data:      result,
		Images:    []domain.ImageAttachment{imageAttachment},
	}, nil
}

// captureX11 captures a screenshot using X11
// parseRegionArgs extracts region parameters from tool arguments
func parseRegionArgs(args map[string]any) (*domain.ScreenRegion, int, int, int, int) {
	var region *domain.ScreenRegion
	var x, y, width, height int

	regionArg, ok := args["region"].(map[string]any)
	if !ok {
		return nil, 0, 0, 0, 0
	}

	region = &domain.ScreenRegion{}
	if xVal, ok := regionArg["x"].(float64); ok {
		region.X = int(xVal)
		x = int(xVal)
	}
	if yVal, ok := regionArg["y"].(float64); ok {
		region.Y = int(yVal)
		y = int(yVal)
	}
	if wVal, ok := regionArg["width"].(float64); ok {
		region.Width = int(wVal)
		width = int(wVal)
	}
	if hVal, ok := regionArg["height"].(float64); ok {
		region.Height = int(hVal)
		height = int(hVal)
	}

	return region, x, y, width, height
}

func (t *ScreenshotTool) captureX11(display string, x, y, width, height int) ([]byte, int, int, error) {
	client, err := NewX11Client(display)
	if err != nil {
		return nil, 0, 0, err
	}
	defer client.Close()

	if width == 0 || height == 0 {
		width, height = client.GetScreenDimensions()
		x, y = 0, 0
	}

	imageBytes, err := client.CaptureScreenBytes(x, y, width, height)
	if err != nil {
		return nil, 0, 0, err
	}

	return imageBytes, width, height, nil
}

// captureWayland captures a screenshot using Wayland tools
func (t *ScreenshotTool) captureWayland(display string, x, y, width, height int) ([]byte, int, int, error) {
	client, err := NewWaylandClient(display)
	if err != nil {
		return nil, 0, 0, err
	}
	defer client.Close()

	if width == 0 || height == 0 {
		w, h, err := client.GetScreenDimensions()
		if err != nil {
			w, h = 1920, 1080
		}
		width, height = w, h
		x, y = 0, 0
	}

	imageBytes, err := client.CaptureScreenBytes(x, y, width, height)
	if err != nil {
		return nil, 0, 0, err
	}

	return imageBytes, width, height, nil
}

// optimizeScreenshot optimizes the screenshot image by resizing and compressing
func (t *ScreenshotTool) optimizeScreenshot(imageBytes []byte) ([]byte, error) {
	img, format, err := image.Decode(bytes.NewReader(imageBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}

	img = t.resizeIfNeeded(img)

	return t.encodeImage(img, format)
}

// resizeIfNeeded resizes the image if it exceeds max dimensions
func (t *ScreenshotTool) resizeIfNeeded(img image.Image) image.Image {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	maxWidth := t.config.ComputerUse.Screenshot.MaxWidth
	maxHeight := t.config.ComputerUse.Screenshot.MaxHeight

	if maxWidth <= 0 && maxHeight <= 0 {
		return img
	}

	needsResize := false
	newWidth := width
	newHeight := height

	if maxWidth > 0 && width > maxWidth {
		needsResize = true
		ratio := float64(maxWidth) / float64(width)
		newWidth = maxWidth
		newHeight = int(float64(height) * ratio)
	}

	if maxHeight > 0 && newHeight > maxHeight {
		needsResize = true
		ratio := float64(maxHeight) / float64(newHeight)
		newHeight = maxHeight
		newWidth = int(float64(newWidth) * ratio)
	}

	if !needsResize {
		return img
	}

	dst := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))
	xdraw.NearestNeighbor.Scale(dst, dst.Bounds(), img, bounds, draw.Src, nil)
	return dst
}

// encodeImage encodes the image to bytes based on configuration
func (t *ScreenshotTool) encodeImage(img image.Image, originalFormat string) ([]byte, error) {
	var buf bytes.Buffer
	format := t.config.ComputerUse.Screenshot.Format
	quality := t.config.ComputerUse.Screenshot.Quality

	if quality <= 0 || quality > 100 {
		quality = 85
	}

	switch format {
	case "jpeg", "jpg":
		err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality})
		if err != nil {
			return nil, fmt.Errorf("failed to encode jpeg: %w", err)
		}
	case "png":
		encoder := png.Encoder{
			CompressionLevel: png.DefaultCompression,
		}
		if err := encoder.Encode(&buf, img); err != nil {
			return nil, fmt.Errorf("failed to encode png: %w", err)
		}
	default:
		if err := png.Encode(&buf, img); err != nil {
			return nil, fmt.Errorf("failed to encode default png: %w", err)
		}
	}

	return buf.Bytes(), nil
}

// Validate checks if the tool arguments are valid
func (t *ScreenshotTool) Validate(args map[string]any) error {
	regionArg, ok := args["region"].(map[string]any)
	if !ok {
		return nil
	}

	x, xOk := regionArg["x"].(float64)
	y, yOk := regionArg["y"].(float64)
	width, wOk := regionArg["width"].(float64)
	height, hOk := regionArg["height"].(float64)

	if xOk && x < 0 {
		return fmt.Errorf("region x must be >= 0")
	}
	if yOk && y < 0 {
		return fmt.Errorf("region y must be >= 0")
	}
	if wOk && width <= 0 {
		return fmt.Errorf("region width must be > 0")
	}
	if hOk && height <= 0 {
		return fmt.Errorf("region height must be > 0")
	}

	return nil
}

// IsEnabled returns whether this tool is enabled
func (t *ScreenshotTool) IsEnabled() bool {
	if t.config.ComputerUse.Screenshot.StreamingEnabled {
		return false
	}
	return t.enabled
}

// FormatResult formats tool execution results for different contexts
func (t *ScreenshotTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
	switch formatType {
	case domain.FormatterLLM:
		return t.FormatForLLM(result)
	case domain.FormatterShort:
		return t.FormatPreview(result)
	default:
		return t.FormatForLLM(result)
	}
}

// FormatPreview returns a short preview of the result for UI display
func (t *ScreenshotTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return "Screenshot capture failed"
	}
	data, ok := result.Data.(domain.ScreenshotToolResult)
	if !ok {
		return "Screenshot captured"
	}
	return fmt.Sprintf("Screenshot captured: %dx%d (%s)", data.Width, data.Height, data.Method)
}

// FormatForLLM formats the result for LLM consumption
func (t *ScreenshotTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return fmt.Sprintf("Error: %s", result.Error)
	}
	data, ok := result.Data.(domain.ScreenshotToolResult)
	if !ok {
		return "Screenshot captured successfully. Image is attached."
	}
	regionStr := "full screen"
	if data.Region != nil {
		regionStr = fmt.Sprintf("region x=%d y=%d w=%d h=%d", data.Region.X, data.Region.Y, data.Region.Width, data.Region.Height)
	}
	return fmt.Sprintf("Screenshot captured successfully (%s, %dx%d, format: %s, method: %s). Image is attached.",
		regionStr, data.Width, data.Height, data.Format, data.Method)
}

// ShouldCollapseArg determines if an argument should be collapsed in display
func (t *ScreenshotTool) ShouldCollapseArg(key string) bool {
	return false
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *ScreenshotTool) ShouldAlwaysExpand() bool {
	return false
}
