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

// MouseMoveTool moves the mouse cursor to specified coordinates
type MouseMoveTool struct {
	config          *config.Config
	enabled         bool
	formatter       domain.BaseFormatter
	rateLimiter     domain.RateLimiter
	displayProvider display.Provider
	stateManager    domain.StateManager
}

// NewMouseMoveTool creates a new mouse move tool
func NewMouseMoveTool(cfg *config.Config, rateLimiter domain.RateLimiter, displayProvider display.Provider, stateManager domain.StateManager) *MouseMoveTool {
	return &MouseMoveTool{
		config:          cfg,
		enabled:         cfg.ComputerUse.Enabled && cfg.ComputerUse.MouseMove.Enabled,
		formatter:       domain.NewBaseFormatter("MouseMove"),
		rateLimiter:     rateLimiter,
		displayProvider: displayProvider,
		stateManager:    stateManager,
	}
}

// Definition returns the tool definition for the LLM
func (t *MouseMoveTool) Definition() sdk.ChatCompletionTool {
	description := "Moves the mouse cursor to absolute screen coordinates. Requires user approval unless in auto-accept mode."
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "MouseMove",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"x": map[string]any{
						"type":        "integer",
						"description": "X coordinate (absolute position from left edge of screen)",
					},
					"y": map[string]any{
						"type":        "integer",
						"description": "Y coordinate (absolute position from top edge of screen)",
					},
				},
				"required": []string{"x", "y"},
			},
		},
	}
}

// Execute runs the mouse move tool with given arguments
func (t *MouseMoveTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	start := time.Now()

	if err := t.rateLimiter.CheckAndRecord("MouseMove"); err != nil {
		return &domain.ToolExecutionResult{
			ToolName:  "MouseMove",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     err.Error(),
		}, nil
	}

	x, xOk := args["x"].(float64)
	y, yOk := args["y"].(float64)

	if !xOk || !yOk {
		return &domain.ToolExecutionResult{
			ToolName:  "MouseMove",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "x and y coordinates are required",
		}, nil
	}

	if t.displayProvider == nil {
		return &domain.ToolExecutionResult{
			ToolName:  "MouseMove",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "no compatible display platform detected",
		}, nil
	}

	controller, err := t.displayProvider.GetController()
	if err != nil {
		return &domain.ToolExecutionResult{
			ToolName:  "MouseMove",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     fmt.Sprintf("failed to get platform controller: %v", err),
		}, nil
	}
	defer func() {
		if closeErr := controller.Close(); closeErr != nil {
			logger.Warn("Failed to close controller", "error", closeErr)
		}
	}()

	targetX, targetY := t.scaleCoordinates(ctx, controller, int(x), int(y))

	fromX, fromY, _ := controller.GetCursorPosition(ctx)

	if err := controller.MoveMouse(ctx, targetX, targetY); err != nil {
		return &domain.ToolExecutionResult{
			ToolName:  "MouseMove",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     fmt.Sprintf("failed to move mouse: %v", err),
		}, nil
	}

	t.broadcastMoveEvent(fromX, fromY, targetX, targetY)

	result := domain.MouseMoveToolResult{
		FromX:  fromX,
		FromY:  fromY,
		ToX:    targetX,
		ToY:    targetY,
		Method: t.displayProvider.GetDisplayInfo().Name,
	}

	return &domain.ToolExecutionResult{
		ToolName:  "MouseMove",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data:      result,
	}, nil
}

// Validate checks if the tool arguments are valid
func (t *MouseMoveTool) Validate(args map[string]any) error {
	x, xOk := args["x"].(float64)
	y, yOk := args["y"].(float64)

	if !xOk {
		return fmt.Errorf("x coordinate is required")
	}
	if !yOk {
		return fmt.Errorf("y coordinate is required")
	}
	if x < 0 {
		return fmt.Errorf("x coordinate must be >= 0")
	}
	if y < 0 {
		return fmt.Errorf("y coordinate must be >= 0")
	}

	return nil
}

// IsEnabled returns whether this tool is enabled
func (t *MouseMoveTool) IsEnabled() bool {
	return t.enabled
}

// FormatResult formats tool execution results for different contexts
func (t *MouseMoveTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
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
func (t *MouseMoveTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return "Mouse move failed"
	}
	data, ok := result.Data.(domain.MouseMoveToolResult)
	if !ok {
		return "Mouse moved"
	}
	return fmt.Sprintf("Moved mouse to (%d, %d)", data.ToX, data.ToY)
}

// FormatForLLM formats the result for LLM consumption
func (t *MouseMoveTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return fmt.Sprintf("Error: %s", result.Error)
	}
	data, ok := result.Data.(domain.MouseMoveToolResult)
	if !ok {
		return "Mouse moved successfully"
	}
	return fmt.Sprintf("Mouse moved from (%d, %d) to (%d, %d) using %s",
		data.FromX, data.FromY, data.ToX, data.ToY, data.Method)
}

// ShouldCollapseArg determines if an argument should be collapsed in display
func (t *MouseMoveTool) ShouldCollapseArg(key string) bool {
	return false
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *MouseMoveTool) ShouldAlwaysExpand() bool {
	return false
}

// scaleCoordinates converts API coordinates to screen coordinates using Anthropic's proportional scaling.
// This follows the official computer-use-demo implementation strategy.
func (t *MouseMoveTool) scaleCoordinates(ctx context.Context, controller display.DisplayController, x, y int) (int, int) {
	if isDirectExec := ctx.Value(domain.DirectExecutionKey); isDirectExec != nil && isDirectExec.(bool) {
		return x, y
	}

	screenWidth, screenHeight, err := controller.GetScreenDimensions(ctx)
	if err != nil {
		logger.Warn("Failed to get screen dimensions", "error", err)
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

// broadcastMoveEvent broadcasts a visual move indicator event for user feedback
func (t *MouseMoveTool) broadcastMoveEvent(fromX, fromY, toX, toY int) {
	if t.stateManager == nil {
		return
	}

	controller, err := t.displayProvider.GetController()
	if err != nil {
		logger.Warn("Failed to get controller for move indicator", "error", err)
		return
	}
	defer func() {
		if closeErr := controller.Close(); closeErr != nil {
			logger.Warn("Failed to close controller", "error", closeErr)
		}
	}()

	_, screenHeight, err := controller.GetScreenDimensions(context.Background())
	if err != nil {
		logger.Warn("Failed to get screen dimensions for move indicator", "error", err)
		screenHeight = 1117
	}

	macosFromY := screenHeight - fromY
	macosToY := screenHeight - toY

	moveEvent := domain.MoveIndicatorEvent{
		BaseChatEvent: domain.BaseChatEvent{
			RequestID: "move-indicator",
			Timestamp: time.Now(),
		},
		FromX:         fromX,
		FromY:         macosFromY,
		ToX:           toX,
		ToY:           macosToY,
		MoveIndicator: true,
	}

	t.stateManager.BroadcastEvent(moveEvent)
}
