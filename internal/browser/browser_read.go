package browser

import (
	"context"
	"fmt"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	browserdomain "github.com/inference-gateway/cli/internal/browser/domain"
	"time"

	config "github.com/inference-gateway/cli/config"
	sdk "github.com/inference-gateway/sdk"
)

// BrowserReadTool reads page content from the shared browser session
type BrowserReadTool struct {
	browserToolBase
	config *config.Config
}

// NewBrowserReadTool creates a new browser read tool
func NewBrowserReadTool(cfg *config.Config, rateLimiter rateLimiter, driver browserdomain.BrowserDriver) *BrowserReadTool {
	return &BrowserReadTool{
		browserToolBase: browserToolBase{
			name:        "BrowserRead",
			enabled:     cfg.BrowserUse.Enabled && cfg.BrowserUse.Tools.Read.Enabled,
			driver:      driver,
			rateLimiter: rateLimiter,
		},
		config: cfg,
	}
}

// Definition returns the tool definition for the LLM
func (t *BrowserReadTool) Definition() sdk.ChatCompletionTool {
	description := t.config.Prompts.Tools.BrowserRead.Description
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "BrowserRead",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"selector": map[string]any{
						"type":        "string",
						"description": "Optional CSS or Playwright selector to read a specific element; omit to read the whole page body",
					},
				},
				"required": []string{},
			},
		},
	}
}

// Execute runs the browser read tool with given arguments
func (t *BrowserReadTool) Execute(ctx context.Context, args map[string]any) (*agentdomain.ToolExecutionResult, error) {
	start := time.Now()

	if err := t.checkRateLimit(); err != nil {
		return t.errorResult(args, start, err.Error()), nil
	}

	selector, _ := args["selector"].(string)

	result, err := t.driver.Read(ctx, selector)
	if err != nil {
		return t.errorResult(args, start, err.Error()), nil
	}
	return t.successResult(args, start, result), nil
}

// Validate checks if the tool arguments are valid
func (t *BrowserReadTool) Validate(args map[string]any) error {
	if raw, exists := args["selector"]; exists {
		if _, ok := raw.(string); !ok {
			return fmt.Errorf("selector must be a string")
		}
	}
	return nil
}
