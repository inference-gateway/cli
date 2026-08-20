package tools

import (
	"context"
	"errors"
	"fmt"
	"math"

	config "github.com/inference-gateway/cli/config"
	display "github.com/inference-gateway/cli/internal/display"
	domain "github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
)

// axTargets is the set of accessibility-tree roots the AX tools accept.
var axTargets = map[string]bool{"": true, "frontmost": true, "dock": true, "menubar": true}

const (
	axNoProviderMessage = "UI element inspection is not available on this platform. " +
		"Use GetLatestFrame (optionally with a region) to inspect the screen visually."
	axNoPermissionMessage = "The accessibility permission is not granted to this process " +
		"(System Settings > Privacy & Security > Accessibility). " +
		"Use GetLatestFrame with a region to inspect the UI visually instead."
)

// GetUIElementsTool lists the pressable elements in a target's accessibility
// tree, with bounding boxes mapped into the frame coordinate space. Read-only:
// it does not move the cursor and does not take the screen lock, so background
// sessions can use it for non-invasive UI inspection.
type GetUIElementsTool struct {
	config *config.Config
}

// NewGetUIElementsTool creates a new GetUIElements tool
func NewGetUIElementsTool(cfg *config.Config) *GetUIElementsTool {
	return &GetUIElementsTool{config: cfg}
}

// Definition returns the tool definition for GetUIElements
func (t *GetUIElementsTool) Definition() sdk.ChatCompletionTool {
	description := t.config.Prompts.Tools.GetUIElements.Description
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "GetUIElements",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"target": map[string]any{
						"type":        "string",
						"enum":        []string{"frontmost", "dock", "menubar"},
						"description": "Which accessibility tree to read: 'frontmost' (default) for the focused application, 'dock' for the macOS Dock / taskbar, 'menubar' for the frontmost app's menu bar titles.",
					},
				},
				"required": []string{},
			},
		},
	}
}

// Validate validates GetUIElements arguments
func (t *GetUIElementsTool) Validate(args map[string]any) error {
	target, _ := args["target"].(string)
	if !axTargets[target] {
		return fmt.Errorf("invalid target %q: must be one of frontmost, dock, menubar", target)
	}
	return nil
}

// Execute executes the GetUIElements tool
func (t *GetUIElementsTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	target, _ := args["target"].(string)
	if target == "" {
		target = "frontmost"
	}

	provider, err := display.DetectAXProvider()
	if err != nil {
		return axDegradeResult("GetUIElements", axNoProviderMessage), nil
	}

	elements, err := provider.ListElements(ctx, target)
	if errors.Is(err, display.ErrNoAXPermission) {
		return axDegradeResult("GetUIElements", axNoPermissionMessage), nil
	}
	if err != nil {
		return nil, err
	}

	if len(elements) == 0 {
		return axDegradeResult("GetUIElements", fmt.Sprintf(
			"The accessibility tree of %s exposes no pressable elements (some apps draw their own UI). "+
				"Use GetLatestFrame with a region to inspect it visually instead.", target)), nil
	}

	frameW, frameH, err := t.scaleToFrameSpace(ctx, elements)
	if err != nil {
		return nil, err
	}

	summary := fmt.Sprintf("%d pressable UI elements of %s, coordinates in the %dx%d frame space", len(elements), target, frameW, frameH)
	if len(elements) >= 200 {
		summary = "first " + summary
	}
	annotation := &domain.ImageAnnotation{Summary: summary, Elements: elements}
	message := domain.AnnotationText(annotation) +
		"\nPress an element by its title with PressUIElement, or click its center coordinate with MouseClick."

	return &domain.ToolExecutionResult{
		ToolName: "GetUIElements",
		Success:  true,
		Data: map[string]any{
			"target":  target,
			"count":   len(elements),
			"message": message,
		},
	}, nil
}

// scaleToFrameSpace converts element bboxes from logical screen points into
// the frame coordinate space (the same space GetLatestFrame images and
// MouseClick coordinates use), mutating elements in place. Returns the frame
// dimensions.
func (t *GetUIElementsTool) scaleToFrameSpace(ctx context.Context, elements []domain.AnnotatedElement) (int, int, error) {
	displayProvider, err := display.DetectDisplay()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to detect display: %w", err)
	}
	controller, err := displayProvider.GetController()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get display controller: %w", err)
	}
	defer func() { _ = controller.Close() }()

	screenW, screenH, err := controller.GetScreenDimensions(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get screen dimensions: %w", err)
	}

	frameW, frameH := t.config.ComputerUse.Screenshot.FitDims(screenW, screenH)
	if frameW != screenW && screenW > 0 {
		scale := float64(frameW) / float64(screenW)
		for i := range elements {
			for j := range elements[i].BBox {
				elements[i].BBox[j] = int(math.Round(float64(elements[i].BBox[j]) * scale))
			}
		}
	}
	return frameW, frameH, nil
}

// axDegradeResult builds the graceful-degradation result shared by the AX
// tools: a successful result whose message steers the model to the vision
// pipeline instead of failing hard.
func axDegradeResult(toolName, message string) *domain.ToolExecutionResult {
	return &domain.ToolExecutionResult{
		ToolName: toolName,
		Success:  true,
		Data:     map[string]any{"message": message},
	}
}

// IsEnabled returns whether the tool is enabled
func (t *GetUIElementsTool) IsEnabled() bool {
	return t.config.ComputerUse.Enabled && t.config.ComputerUse.Tools.GetUIElements.Enabled
}

// FormatPreview formats the result for display preview
func (t *GetUIElementsTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return "Failed to get UI elements"
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		return "Got UI elements"
	}
	if count, ok := data["count"].(int); ok {
		return fmt.Sprintf("%d UI elements (%v)", count, data["target"])
	}
	return "No UI elements"
}

// FormatForLLM formats the result for LLM consumption
func (t *GetUIElementsTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	return axMessageForLLM(result, "Successfully retrieved UI elements")
}

// axMessageForLLM extracts the message from a Data map result, shared by the
// AX tools.
func axMessageForLLM(result *domain.ToolExecutionResult, fallback string) string {
	if result == nil || !result.Success {
		return fmt.Sprintf("Error: %s", result.Error)
	}
	if data, ok := result.Data.(map[string]any); ok {
		if message, ok := data["message"].(string); ok {
			return message
		}
	}
	return fallback
}

// ShouldCollapseArg determines if an argument should be collapsed in display
func (t *GetUIElementsTool) ShouldCollapseArg(key string) bool {
	return false
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *GetUIElementsTool) ShouldAlwaysExpand() bool {
	return false
}

// FormatResult formats the result based on the requested format type
func (t *GetUIElementsTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
	switch formatType {
	case domain.FormatterLLM:
		return t.FormatForLLM(result)
	case domain.FormatterShort:
		return t.FormatPreview(result)
	default:
		return t.FormatForLLM(result)
	}
}
