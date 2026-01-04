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
}

// NewMouseMoveTool creates a new mouse move tool
func NewMouseMoveTool(cfg *config.Config, rateLimiter domain.RateLimiter, displayProvider display.Provider) *MouseMoveTool {
	return &MouseMoveTool{
		config:          cfg,
		enabled:         cfg.ComputerUse.Enabled && cfg.ComputerUse.MouseMove.Enabled,
		formatter:       domain.NewBaseFormatter("MouseMove"),
		rateLimiter:     rateLimiter,
		displayProvider: displayProvider,
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

	fromX, fromY, _ := controller.GetCursorPosition(ctx)

	if err := controller.MoveMouse(ctx, int(x), int(y)); err != nil {
		return &domain.ToolExecutionResult{
			ToolName:  "MouseMove",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     fmt.Sprintf("failed to move mouse: %v", err),
		}, nil
	}

	result := domain.MouseMoveToolResult{
		FromX:  fromX,
		FromY:  fromY,
		ToX:    int(x),
		ToY:    int(y),
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
