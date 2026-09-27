package browser

import (
	"context"
	"fmt"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	browserdomain "github.com/inference-gateway/cli/internal/browser/domain"
)

// BrowserTypeTool types text into an element in the shared browser session
type BrowserTypeTool struct {
	browserToolBase
	config *config.Config
}

// NewBrowserTypeTool creates a new browser type tool
func NewBrowserTypeTool(cfg *config.Config, rateLimiter rateLimiter, driver browserdomain.BrowserDriver) *BrowserTypeTool {
	return &BrowserTypeTool{
		browserToolBase: browserToolBase{
			name:        ToolType,
			enabled:     cfg.BrowserUse.Enabled && cfg.BrowserUse.Tools.Type.Enabled,
			driver:      driver,
			rateLimiter: rateLimiter,
		},
		config: cfg,
	}
}

// Manifest returns the tool's manifest.
func (t *BrowserTypeTool) Manifest() agentdomain.ToolManifest {
	return toolManifests.MustGet(ToolType)
}

// Definition returns the tool definition for the LLM
func (t *BrowserTypeTool) Definition() sdk.ChatCompletionTool {
	return t.Manifest().Definition()
}

// Execute runs the browser type tool with given arguments
func (t *BrowserTypeTool) Execute(ctx context.Context, args map[string]any) (*agentdomain.ToolExecutionResult, error) {
	start := time.Now()

	if err := t.checkRateLimit(); err != nil {
		return t.errorResult(args, start, err.Error()), nil
	}

	selector, err := requireString(args, "selector")
	if err != nil {
		return t.errorResult(args, start, err.Error()), nil
	}
	text, ok := args["text"].(string)
	if !ok {
		return t.errorResult(args, start, "text is required and must be a string"), nil
	}
	pressEnter, _ := args["press_enter"].(bool)

	result, err := t.driver.Type(ctx, selector, text, pressEnter)
	if err != nil {
		return t.errorResult(args, start, err.Error()), nil
	}
	return t.successResult(args, start, result), nil
}

// Validate checks if the tool arguments are valid
func (t *BrowserTypeTool) Validate(args map[string]any) error {
	if _, err := requireString(args, "selector"); err != nil {
		return err
	}
	if _, ok := args["text"].(string); !ok {
		return fmt.Errorf("text is required and must be a string")
	}
	return nil
}
