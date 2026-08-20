package browser

import (
	"context"
	"fmt"
	browserdomain "github.com/inference-gateway/cli/internal/browser/domain"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
)

// BrowserClickTool clicks an element in the shared browser session
type BrowserClickTool struct {
	browserToolBase
	config *config.Config
}

// NewBrowserClickTool creates a new browser click tool
func NewBrowserClickTool(cfg *config.Config, rateLimiter rateLimiter, driver browserdomain.BrowserDriver) *BrowserClickTool {
	return &BrowserClickTool{
		browserToolBase: browserToolBase{
			name:        "BrowserClick",
			enabled:     cfg.BrowserUse.Enabled && cfg.BrowserUse.Tools.Click.Enabled,
			driver:      driver,
			rateLimiter: rateLimiter,
		},
		config: cfg,
	}
}

// Definition returns the tool definition for the LLM
func (t *BrowserClickTool) Definition() sdk.ChatCompletionTool {
	description := t.config.Prompts.Tools.BrowserClick.Description
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "BrowserClick",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"selector": map[string]any{
						"type":        "string",
						"description": "CSS or Playwright selector of the element to click (e.g. 'button.submit', 'text=Sign in'). Provide this OR x/y coordinates.",
					},
					"x": map[string]any{
						"type":        "number",
						"description": "Viewport x coordinate (CSS pixels) to click, from a BrowserScreenshot. Must be paired with y.",
					},
					"y": map[string]any{
						"type":        "number",
						"description": "Viewport y coordinate (CSS pixels) to click, from a BrowserScreenshot. Must be paired with x.",
					},
				},
				"required": []string{},
			},
		},
	}
}

// Execute runs the browser click tool with given arguments
func (t *BrowserClickTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	start := time.Now()

	if err := t.checkRateLimit(); err != nil {
		return t.errorResult(args, start, err.Error()), nil
	}

	if x, y, ok := clickCoords(args); ok {
		result, err := t.driver.ClickAt(ctx, x, y)
		if err != nil {
			return t.errorResult(args, start, err.Error()), nil
		}
		return t.successResult(args, start, result), nil
	}

	selector, err := requireString(args, "selector")
	if err != nil {
		return t.errorResult(args, start, err.Error()), nil
	}

	result, err := t.driver.Click(ctx, selector)
	if err != nil {
		return t.errorResult(args, start, err.Error()), nil
	}
	return t.successResult(args, start, result), nil
}

// Validate checks if the tool arguments are valid: a selector, or an x/y pair.
func (t *BrowserClickTool) Validate(args map[string]any) error {
	_, hasX := numberArg(args, "x")
	_, hasY := numberArg(args, "y")
	if hasX || hasY {
		if !hasX || !hasY {
			return fmt.Errorf("x and y must be provided together")
		}
		return nil
	}
	_, err := requireString(args, "selector")
	return err
}

// clickCoords returns the x/y click target when both are present.
func clickCoords(args map[string]any) (float64, float64, bool) {
	x, hasX := numberArg(args, "x")
	y, hasY := numberArg(args, "y")
	return x, y, hasX && hasY
}

// numberArg reads a numeric argument, tolerating the int/float forms JSON and
// callers may produce.
func numberArg(args map[string]any, key string) (float64, bool) {
	switch v := args[key].(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	default:
		return 0, false
	}
}
