package tools

import (
	"context"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
)

// BrowserClickTool clicks an element in the shared browser session
type BrowserClickTool struct {
	browserToolBase
	config *config.Config
}

// NewBrowserClickTool creates a new browser click tool
func NewBrowserClickTool(cfg *config.Config, rateLimiter domain.RateLimiter, driver domain.BrowserDriver) *BrowserClickTool {
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
						"description": "CSS or Playwright selector of the element to click (e.g. 'button.submit', 'text=Sign in')",
					},
				},
				"required": []string{"selector"},
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

// Validate checks if the tool arguments are valid
func (t *BrowserClickTool) Validate(args map[string]any) error {
	_, err := requireString(args, "selector")
	return err
}
