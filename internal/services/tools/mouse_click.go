package tools

import (
	"context"
	"fmt"
	"time"

	config "github.com/inference-gateway/cli/config"
	display "github.com/inference-gateway/cli/internal/display"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	sdk "github.com/inference-gateway/sdk"
)

// MouseClickTool performs mouse clicks
type MouseClickTool struct {
	config          *config.Config
	enabled         bool
	formatter       domain.BaseFormatter
	rateLimiter     domain.RateLimiter
	displayProvider display.Provider
	stateManager    domain.StateManager
}

// NewMouseClickTool creates a new mouse click tool
func NewMouseClickTool(cfg *config.Config, rateLimiter domain.RateLimiter, displayProvider display.Provider, stateManager domain.StateManager) *MouseClickTool {
	return &MouseClickTool{
		config:          cfg,
		enabled:         cfg.ComputerUse.Enabled && cfg.ComputerUse.MouseClick.Enabled,
		formatter:       domain.NewBaseFormatter("MouseClick"),
		rateLimiter:     rateLimiter,
		displayProvider: displayProvider,
		stateManager:    stateManager,
	}
}

// Definition returns the tool definition for the LLM
func (t *MouseClickTool) Definition() sdk.ChatCompletionTool {
	description := "Performs a mouse click. Can click at current position or move to coordinates first. Supports left, right, and middle buttons. Requires user approval unless in auto-accept mode."
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "MouseClick",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"button": map[string]any{
						"type":        "string",
						"description": "Mouse button to click",
						"enum":        []string{"left", "right", "middle"},
						"default":     "left",
					},
					"clicks": map[string]any{
						"type":        "integer",
						"description": "Number of clicks (1=single, 2=double, 3=triple)",
						"enum":        []int{1, 2, 3},
						"default":     1,
					},
					"x": map[string]any{
						"type":        "integer",
						"description": "Optional: X coordinate to move to before clicking",
					},
					"y": map[string]any{
						"type":        "integer",
						"description": "Optional: Y coordinate to move to before clicking",
					},
				},
				"required": []string{"button"},
			},
		},
	}
}

// Execute runs the mouse click tool with given arguments
func (t *MouseClickTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	start := time.Now()

	if err := t.rateLimiter.CheckAndRecord("MouseClick"); err != nil {
		return t.errorResult(args, start, err.Error()), nil
	}

	button := t.getButton(args)
	clicks := t.getClicks(args)
	finalX, finalY, shouldMove := t.getCoordinates(args)

	if t.displayProvider == nil {
		return t.errorResult(args, start, "no compatible display platform detected"), nil
	}

	controller, err := t.displayProvider.GetController()
	if err != nil {
		return t.errorResult(args, start, fmt.Sprintf("failed to get platform controller: %v", err)), nil
	}
	defer func() {
		if closeErr := controller.Close(); closeErr != nil {
			logger.Warn("Failed to close controller", "error", closeErr)
		}
	}()

	finalX, finalY, err = t.handleMovement(ctx, controller, shouldMove, finalX, finalY)
	if err != nil {
		return t.errorResult(args, start, err.Error()), nil
	}

	mouseButton := display.ParseMouseButton(button)
	if err := controller.ClickMouse(ctx, mouseButton, clicks); err != nil {
		return t.errorResult(args, start, fmt.Sprintf("failed to click mouse: %v", err)), nil
	}

	t.updateStateAfterClick(ctx, controller, finalX, finalY)

	result := domain.MouseClickToolResult{
		Button: button,
		Clicks: clicks,
		X:      finalX,
		Y:      finalY,
		Method: t.displayProvider.GetDisplayInfo().Name,
	}

	return &domain.ToolExecutionResult{
		ToolName:  "MouseClick",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data:      result,
	}, nil
}

func (t *MouseClickTool) getButton(args map[string]any) string {
	button, ok := args["button"].(string)
	if !ok {
		return "left"
	}
	return button
}

func (t *MouseClickTool) getClicks(args map[string]any) int {
	if clicksArg, ok := args["clicks"].(float64); ok {
		return int(clicksArg)
	}
	return 1
}

func (t *MouseClickTool) getCoordinates(args map[string]any) (int, int, bool) {
	if xArg, xOk := args["x"].(float64); xOk {
		if yArg, yOk := args["y"].(float64); yOk {
			return int(xArg), int(yArg), true
		}
	}
	return 0, 0, false
}

func (t *MouseClickTool) errorResult(args map[string]any, start time.Time, errorMsg string) *domain.ToolExecutionResult {
	return &domain.ToolExecutionResult{
		ToolName:  "MouseClick",
		Arguments: args,
		Success:   false,
		Duration:  time.Since(start),
		Error:     errorMsg,
	}
}

func (t *MouseClickTool) handleMovement(ctx context.Context, controller display.DisplayController, shouldMove bool, x, y int) (int, int, error) {
	if !shouldMove {
		cursorX, cursorY, _ := controller.GetCursorPosition(ctx)
		return cursorX, cursorY, nil
	}

	targetX, targetY := t.scaleCoordinates(ctx, controller, x, y)

	if err := controller.MoveMouse(ctx, targetX, targetY); err != nil {
		return 0, 0, fmt.Errorf("failed to move mouse: %w", err)
	}

	return targetX, targetY, nil
}

// scaleCoordinates converts API coordinates to screen coordinates using Anthropic's proportional scaling.
// This follows the official computer-use-demo implementation strategy.
func (t *MouseClickTool) scaleCoordinates(ctx context.Context, controller display.DisplayController, x, y int) (int, int) {
	if isDirectExec := ctx.Value(domain.DirectExecutionKey); isDirectExec != nil && isDirectExec.(bool) {
		return x, y
	}

	screenWidth, screenHeight, err := controller.GetScreenDimensions(ctx)
	if err != nil {
		logger.Warn("Failed to get screen dimensions, no scaling", "error", err)
		return x, y
	}

	apiWidth := t.config.ComputerUse.Screenshot.TargetWidth
	apiHeight := t.config.ComputerUse.Screenshot.TargetHeight

	if apiWidth == 0 || apiHeight == 0 {
		return x, y
	}

	screenX, screenY := ScaleAPIToScreen(x, y, apiWidth, apiHeight, screenWidth, screenHeight)

	return screenX, screenY
}

func (t *MouseClickTool) updateStateAfterClick(ctx context.Context, controller display.DisplayController, x, y int) {
	if t.stateManager == nil {
		return
	}

	t.storeFocusedApp(ctx, controller)
	t.stateManager.SetLastClickCoordinates(x, y)
	t.broadcastClickEvent(x, y)
}

func (t *MouseClickTool) storeFocusedApp(ctx context.Context, controller display.DisplayController) {
	focusManager, ok := controller.(display.FocusManager)
	if !ok {
		return
	}

	appID, err := focusManager.GetFrontmostApp(ctx)
	if err != nil {
		logger.Warn("Failed to get frontmost app after click", "error", err)
		return
	}

	t.stateManager.SetLastFocusedApp(appID)
}

func (t *MouseClickTool) broadcastClickEvent(x, y int) {
	controller, err := t.displayProvider.GetController()
	if err != nil {
		logger.Warn("Failed to get controller for click indicator", "error", err)
		return
	}
	defer func() {
		if closeErr := controller.Close(); closeErr != nil {
			logger.Warn("Failed to close controller", "error", closeErr)
		}
	}()

	_, screenHeight, err := controller.GetScreenDimensions(context.Background())
	if err != nil {
		logger.Warn("Failed to get screen dimensions for click indicator", "error", err)
		screenHeight = 1117
	}

	macosY := screenHeight - y

	clickEvent := domain.ClickIndicatorEvent{
		BaseChatEvent: domain.BaseChatEvent{
			RequestID: "click-indicator",
			Timestamp: time.Now(),
		},
		X:              x,
		Y:              macosY,
		ClickIndicator: true,
	}
	t.stateManager.BroadcastEvent(clickEvent)
}

// Validate checks if the tool arguments are valid
func (t *MouseClickTool) Validate(args map[string]any) error {
	button, ok := args["button"].(string)
	if !ok {
		return fmt.Errorf("button is required")
	}

	if button != "left" && button != "right" && button != "middle" {
		return fmt.Errorf("button must be 'left', 'right', or 'middle'")
	}

	if clicksArg, ok := args["clicks"].(float64); ok {
		clicks := int(clicksArg)
		if clicks < 1 || clicks > 3 {
			return fmt.Errorf("clicks must be 1, 2, or 3")
		}
	}

	if xArg, xOk := args["x"].(float64); xOk {
		if _, yOk := args["y"].(float64); !yOk {
			return fmt.Errorf("both x and y must be provided together")
		}
		if xArg < 0 {
			return fmt.Errorf("x coordinate must be >= 0")
		}
	}

	if yArg, yOk := args["y"].(float64); yOk {
		if _, xOk := args["x"].(float64); !xOk {
			return fmt.Errorf("both x and y must be provided together")
		}
		if yArg < 0 {
			return fmt.Errorf("y coordinate must be >= 0")
		}
	}

	return nil
}

// IsEnabled returns whether this tool is enabled
func (t *MouseClickTool) IsEnabled() bool {
	return t.enabled
}

// FormatResult formats tool execution results for different contexts
func (t *MouseClickTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
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
func (t *MouseClickTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return "Mouse click failed"
	}
	data, ok := result.Data.(domain.MouseClickToolResult)
	if !ok {
		return "Mouse clicked"
	}
	var clickType string
	switch data.Clicks {
	case 2:
		clickType = "double-click"
	case 3:
		clickType = "triple-click"
	default:
		clickType = "click"
	}
	return fmt.Sprintf("%s %s at (%d, %d)", data.Button, clickType, data.X, data.Y)
}

// FormatForLLM formats the result for LLM consumption
func (t *MouseClickTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return fmt.Sprintf("Error: %s", result.Error)
	}
	data, ok := result.Data.(domain.MouseClickToolResult)
	if !ok {
		return "Mouse click performed successfully"
	}
	var clickDesc string
	switch data.Clicks {
	case 2:
		clickDesc = "double-click"
	case 3:
		clickDesc = "triple-click"
	default:
		clickDesc = fmt.Sprintf("%d click(s)", data.Clicks)
	}
	return fmt.Sprintf("Performed %s %s at position (%d, %d) using %s",
		data.Button, clickDesc, data.X, data.Y, data.Method)
}

// ShouldCollapseArg determines if an argument should be collapsed in display
func (t *MouseClickTool) ShouldCollapseArg(key string) bool {
	return false
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *MouseClickTool) ShouldAlwaysExpand() bool {
	return false
}
