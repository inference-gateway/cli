package computer

import (
	"context"
	"fmt"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	agentinfra "github.com/inference-gateway/cli/internal/agent/infrastructure"
	computerdomain "github.com/inference-gateway/cli/internal/computer/domain"
	display "github.com/inference-gateway/cli/internal/computer/infrastructure/display"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

// MouseMoveTool moves the mouse cursor to specified coordinates
type MouseMoveTool struct {
	config          *config.Config
	enabled         bool
	formatter       agentinfra.BaseFormatter
	rateLimiter     rateLimiter
	displayProvider display.Provider
	stateManager    agentdomain.EventBridgeManager
}

// NewMouseMoveTool creates a new mouse move tool
func NewMouseMoveTool(cfg *config.Config, rateLimiter rateLimiter, displayProvider display.Provider, stateManager agentdomain.EventBridgeManager) *MouseMoveTool {
	return &MouseMoveTool{
		config:          cfg,
		enabled:         cfg.ComputerUse.Enabled && cfg.ComputerUse.Tools.MouseMove.Enabled,
		formatter:       agentinfra.NewBaseFormatter("MouseMove"),
		rateLimiter:     rateLimiter,
		displayProvider: displayProvider,
		stateManager:    stateManager,
	}
}

// Definition returns the tool definition for the LLM
func (t *MouseMoveTool) Definition() sdk.ChatCompletionTool {
	description := t.config.Prompts.Tools.MouseMove.Description
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
						"description": "X coordinate in the screenshot coordinate space (same space as GetLatestFrame bounding boxes), scaled to the real screen on execution",
					},
					"y": map[string]any{
						"type":        "integer",
						"description": "Y coordinate in the screenshot coordinate space (same space as GetLatestFrame bounding boxes), scaled to the real screen on execution",
					},
				},
				"required": []string{"x", "y"},
			},
		},
	}
}

// Execute runs the mouse move tool with given arguments
func (t *MouseMoveTool) Execute(ctx context.Context, args map[string]any) (*agentdomain.ToolExecutionResult, error) {
	start := time.Now()

	if err := t.rateLimiter.CheckAndRecord("MouseMove"); err != nil {
		return &agentdomain.ToolExecutionResult{
			ToolName:  "MouseMove",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     err.Error(),
		}, nil
	}

	if err := acquireScreenLock(); err != nil {
		return &agentdomain.ToolExecutionResult{
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
		return &agentdomain.ToolExecutionResult{
			ToolName:  "MouseMove",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "x and y coordinates are required",
		}, nil
	}

	if t.displayProvider == nil {
		return &agentdomain.ToolExecutionResult{
			ToolName:  "MouseMove",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "no compatible display platform detected",
		}, nil
	}

	controller, err := t.displayProvider.GetController()
	if err != nil {
		return &agentdomain.ToolExecutionResult{
			ToolName:  "MouseMove",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     fmt.Sprintf("failed to get platform controller: %v", err),
		}, nil
	}
	defer func() {
		if closeErr := controller.Close(); closeErr != nil {
			logger.Warn("failed to close controller", "error", closeErr)
		}
	}()

	targetX, targetY, err := t.scaleCoordinates(ctx, controller, int(x), int(y))
	if err != nil {
		return &agentdomain.ToolExecutionResult{
			ToolName:  "MouseMove",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     fmt.Sprintf("failed to move mouse: %v", err),
		}, nil
	}

	fromX, fromY, _ := controller.GetCursorPosition(ctx)

	if err := controller.MoveMouse(ctx, targetX, targetY); err != nil {
		return &agentdomain.ToolExecutionResult{
			ToolName:  "MouseMove",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     fmt.Sprintf("failed to move mouse: %v", err),
		}, nil
	}

	t.broadcastMoveEvent(fromX, fromY, targetX, targetY)

	result := computerdomain.MouseMoveToolResult{
		FromX:  fromX,
		FromY:  fromY,
		ToX:    targetX,
		ToY:    targetY,
		Method: t.displayProvider.GetDisplayInfo().Name,
	}

	return &agentdomain.ToolExecutionResult{
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
func (t *MouseMoveTool) FormatResult(result *agentdomain.ToolExecutionResult, formatType agentdomain.FormatterType) string {
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
func (t *MouseMoveTool) FormatPreview(result *agentdomain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return "Mouse move failed"
	}
	data, ok := result.Data.(computerdomain.MouseMoveToolResult)
	if !ok {
		return "Mouse moved"
	}
	return fmt.Sprintf("Moved mouse to (%d, %d)", data.ToX, data.ToY)
}

// FormatForLLM formats the result for LLM consumption
func (t *MouseMoveTool) FormatForLLM(result *agentdomain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return fmt.Sprintf("Error: %s", result.Error)
	}
	data, ok := result.Data.(computerdomain.MouseMoveToolResult)
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
func (t *MouseMoveTool) scaleCoordinates(ctx context.Context, controller display.DisplayController, x, y int) (int, int, error) {
	if isDirectExec := ctx.Value(agentdomain.DirectExecutionKey); isDirectExec != nil && isDirectExec.(bool) {
		return x, y, nil
	}

	screenWidth, screenHeight, err := controller.GetScreenDimensions(ctx)
	if err != nil {
		logger.Warn("failed to get screen dimensions", "error", err)
		return x, y, nil
	}

	apiWidth, apiHeight := t.config.ComputerUse.Screenshot.FitDims(screenWidth, screenHeight)
	return ScaleAPIToScreen(x, y, apiWidth, apiHeight, screenWidth, screenHeight)
}

// broadcastMoveEvent broadcasts a visual move indicator event for user feedback
func (t *MouseMoveTool) broadcastMoveEvent(fromX, fromY, toX, toY int) {
	if t.stateManager == nil {
		return
	}

	controller, err := t.displayProvider.GetController()
	if err != nil {
		logger.Warn("failed to get controller for move indicator", "error", err)
		return
	}
	defer func() {
		if closeErr := controller.Close(); closeErr != nil {
			logger.Warn("failed to close controller", "error", closeErr)
		}
	}()

	_, screenHeight, err := controller.GetScreenDimensions(context.Background())
	if err != nil {
		logger.Warn("failed to get screen dimensions for move indicator", "error", err)
		screenHeight = 1117
	}

	macosFromY := screenHeight - fromY
	macosToY := screenHeight - toY

	moveEvent := agentdomain.MoveIndicatorEvent{
		BaseChatEvent: agentdomain.BaseChatEvent{
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
