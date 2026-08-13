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

// maxBrowserReadChars bounds how much page text a single BrowserRead returns
// so one read cannot flood the conversation context.
const maxBrowserReadChars = 50000

// BrowserReadTool reads page content from the shared browser session
type BrowserReadTool struct {
	browserToolBase
	config *config.Config
}

// NewBrowserReadTool creates a new browser read tool
func NewBrowserReadTool(cfg *config.Config, rateLimiter domain.RateLimiter, session *browserSession) *BrowserReadTool {
	return &BrowserReadTool{
		browserToolBase: browserToolBase{
			name:        "BrowserRead",
			enabled:     cfg.BrowserUse.Enabled && cfg.BrowserUse.Tools.Read.Enabled,
			session:     session,
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
func (t *BrowserReadTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	start := time.Now()

	if err := t.checkRateLimit(); err != nil {
		return t.errorResult(args, start, err.Error()), nil
	}

	page, err := t.session.Page()
	if err != nil {
		return t.errorResult(args, start, err.Error()), nil
	}

	selector, _ := args["selector"].(string)
	target := selector
	if target == "" {
		target = "body"
	}

	content, err := page.Locator(target).First().InnerText(playwright.LocatorInnerTextOptions{
		Timeout: playwright.Float(t.session.timeoutMs()),
	})
	if err != nil {
		return t.errorResult(args, start, fmt.Sprintf("failed to read %q: %v", target, err)), nil
	}
	if len(content) > maxBrowserReadChars {
		content = content[:maxBrowserReadChars] + "\n... (truncated)"
	}

	title, _ := page.Title()
	return t.successResult(args, start, domain.BrowserToolResult{
		Action:   "read",
		Selector: selector,
		URL:      page.URL(),
		Title:    title,
		Content:  content,
		Events:   t.session.DrainEvents(),
	}), nil
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
