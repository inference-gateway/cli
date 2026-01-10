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

// MouseScrollTool scrolls the mouse wheel
type MouseScrollTool struct {
	config          *config.Config
	enabled         bool
	formatter       domain.BaseFormatter
	rateLimiter     domain.RateLimiter
	displayProvider display.Provider
}

// NewMouseScrollTool creates a new mouse scroll tool
func NewMouseScrollTool(cfg *config.Config, rateLimiter domain.RateLimiter, displayProvider display.Provider) *MouseScrollTool {
	return &MouseScrollTool{
		config:          cfg,
		enabled:         cfg.ComputerUse.Enabled,
		formatter:       domain.NewBaseFormatter("MouseScroll"),
		rateLimiter:     rateLimiter,
		displayProvider: displayProvider,
	}
}

// Definition returns the tool definition for the LLM
func (t *MouseScrollTool) Definition() sdk.ChatCompletionTool {
	description := "Scrolls the mouse wheel up or down. Useful for navigating web pages, documents, and long content. Positive values scroll down, negative values scroll up."
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "MouseScroll",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"clicks": map[string]any{
						"type":        "integer",
						"description": "Number of scroll clicks. Positive = scroll down, negative = scroll up. Each click scrolls by a few lines. Example: 5 scrolls down, -3 scrolls up.",
					},
					"direction": map[string]any{
						"type":        "string",
						"description": "Scroll direction: 'vertical' (default, up/down) or 'horizontal' (left/right)",
						"enum":        []string{"vertical", "horizontal"},
					},
				},
				"required": []string{"clicks"},
			},
		},
	}
}

// Execute runs the mouse scroll tool with given arguments
func (t *MouseScrollTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	start := time.Now()

	if err := t.rateLimiter.CheckAndRecord("MouseScroll"); err != nil {
		return &domain.ToolExecutionResult{
			ToolName:  "MouseScroll",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     err.Error(),
		}, nil
	}

	clicks, clicksOk := args["clicks"].(float64)
	if !clicksOk {
		return &domain.ToolExecutionResult{
			ToolName:  "MouseScroll",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "clicks must be an integer",
		}, nil
	}

	direction := "vertical"
	if dirVal, ok := args["direction"].(string); ok {
		direction = dirVal
	}

	controller, err := t.displayProvider.GetController()
	if err != nil {
		return &domain.ToolExecutionResult{
			ToolName:  "MouseScroll",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     fmt.Sprintf("failed to get display controller: %v", err),
		}, nil
	}
	defer func() {
		if err := controller.Close(); err != nil {
			logger.Debug("Failed to close display controller", "error", err)
		}
	}()

	clicksInt := int(clicks)

	if err := controller.ScrollMouse(ctx, clicksInt, direction); err != nil {
		return &domain.ToolExecutionResult{
			ToolName:  "MouseScroll",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     fmt.Sprintf("failed to scroll: %v", err),
		}, nil
	}

	scrollDir := "down"
	if clicksInt < 0 {
		scrollDir = "up"
		clicksInt = -clicksInt
	}
	if direction == "horizontal" {
		if int(clicks) > 0 {
			scrollDir = "right"
		} else {
			scrollDir = "left"
		}
	}

	message := fmt.Sprintf("Scrolled %s by %d clicks", scrollDir, clicksInt)
	logger.Info("Mouse scroll executed", "direction", direction, "clicks", clicks)

	return &domain.ToolExecutionResult{
		ToolName:  "MouseScroll",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data: map[string]any{
			"clicks":    clicks,
			"direction": direction,
			"message":   message,
		},
	}, nil
}

// Validate validates the tool arguments
func (t *MouseScrollTool) Validate(args map[string]any) error {
	if _, ok := args["clicks"].(float64); !ok {
		return fmt.Errorf("clicks must be an integer")
	}

	if direction, ok := args["direction"].(string); ok {
		if direction != "vertical" && direction != "horizontal" {
			return fmt.Errorf("direction must be 'vertical' or 'horizontal'")
		}
	}

	return nil
}

// IsEnabled returns whether the tool is enabled
func (t *MouseScrollTool) IsEnabled() bool {
	return t.enabled
}

// FormatPreview formats a short preview of tool execution
func (t *MouseScrollTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if !result.Success {
		return "Scroll failed"
	}

	data, ok := result.Data.(map[string]any)
	if !ok {
		return "Scrolled"
	}

	return fmt.Sprintf("%s", data["message"])
}

// FormatForLLM formats the result for LLM consumption
func (t *MouseScrollTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if !result.Success {
		return fmt.Sprintf("Scroll failed: %s", result.Error)
	}

	data, ok := result.Data.(map[string]any)
	if !ok {
		return "Scrolled successfully"
	}

	return fmt.Sprintf("%s. Use GetLatestScreenshot to see the new content.", data["message"])
}

// ShouldCollapseArg determines if an argument should be collapsed in UI
func (t *MouseScrollTool) ShouldCollapseArg(argName string) bool {
	return false
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *MouseScrollTool) ShouldAlwaysExpand() bool {
	return false
}

// FormatResult formats the result based on the requested format type
func (t *MouseScrollTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
	switch formatType {
	case domain.FormatterLLM:
		return t.FormatForLLM(result)
	case domain.FormatterShort:
		return t.FormatPreview(result)
	default:
		return t.FormatForLLM(result)
	}
}
