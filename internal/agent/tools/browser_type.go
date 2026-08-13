package tools

import (
	"context"
	"fmt"
	"time"

	playwright "github.com/mxschmitt/playwright-go"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
)

// BrowserTypeTool types text into an element in the shared browser session
type BrowserTypeTool struct {
	browserToolBase
	config *config.Config
}

// NewBrowserTypeTool creates a new browser type tool
func NewBrowserTypeTool(cfg *config.Config, rateLimiter domain.RateLimiter, session *browserSession) *BrowserTypeTool {
	return &BrowserTypeTool{
		browserToolBase: browserToolBase{
			name:        "BrowserType",
			enabled:     cfg.BrowserUse.Enabled && cfg.BrowserUse.Tools.Type.Enabled,
			session:     session,
			rateLimiter: rateLimiter,
		},
		config: cfg,
	}
}

// Definition returns the tool definition for the LLM
func (t *BrowserTypeTool) Definition() sdk.ChatCompletionTool {
	description := t.config.Prompts.Tools.BrowserType.Description
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "BrowserType",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"selector": map[string]any{
						"type":        "string",
						"description": "CSS or Playwright selector of the input element (e.g. 'input[name=q]')",
					},
					"text": map[string]any{
						"type":        "string",
						"description": "Text to type; replaces the element's current value",
					},
					"press_enter": map[string]any{
						"type":        "boolean",
						"description": "Press Enter after typing to submit (defaults to false)",
						"default":     false,
					},
				},
				"required": []string{"selector", "text"},
			},
		},
	}
}

// Execute runs the browser type tool with given arguments
func (t *BrowserTypeTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
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

	page, err := t.session.Page()
	if err != nil {
		return t.errorResult(args, start, err.Error()), nil
	}

	locator := page.Locator(selector).First()
	if err := locator.Fill(text, playwright.LocatorFillOptions{
		Timeout: playwright.Float(t.session.timeoutMs()),
	}); err != nil {
		return t.errorResult(args, start, fmt.Sprintf("failed to type into %q: %v", selector, err)), nil
	}

	if pressEnter {
		if err := locator.Press("Enter", playwright.LocatorPressOptions{
			Timeout: playwright.Float(t.session.timeoutMs()),
		}); err != nil {
			return t.errorResult(args, start, fmt.Sprintf("failed to press Enter in %q: %v", selector, err)), nil
		}
	}

	title, _ := page.Title()
	return t.successResult(args, start, domain.BrowserToolResult{
		Action:   "type",
		Selector: selector,
		Text:     text,
		URL:      page.URL(),
		Title:    title,
	}), nil
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
