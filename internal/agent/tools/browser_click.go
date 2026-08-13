package tools

import (
	"context"
	"fmt"
	"time"

	playwright "github.com/playwright-community/playwright-go"

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
func NewBrowserClickTool(cfg *config.Config, rateLimiter domain.RateLimiter, session *browserSession) *BrowserClickTool {
	return &BrowserClickTool{
		browserToolBase: browserToolBase{
			name:        "BrowserClick",
			enabled:     cfg.BrowserUse.Enabled && cfg.BrowserUse.Tools.Click.Enabled,
			session:     session,
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

	page, err := t.session.Page()
	if err != nil {
		return t.errorResult(args, start, err.Error()), nil
	}

	if err := page.Locator(selector).First().Click(playwright.LocatorClickOptions{
		Timeout: playwright.Float(t.session.timeoutMs()),
	}); err != nil {
		return t.errorResult(args, start, fmt.Sprintf("failed to click %q: %v", selector, err)), nil
	}

	title, _ := page.Title()
	return t.successResult(args, start, domain.BrowserToolResult{
		Action:   "click",
		Selector: selector,
		URL:      page.URL(),
		Title:    title,
	}), nil
}

// Validate checks if the tool arguments are valid
func (t *BrowserClickTool) Validate(args map[string]any) error {
	_, err := requireString(args, "selector")
	return err
}
