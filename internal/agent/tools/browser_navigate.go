package tools

import (
	"context"
	"fmt"
	"net/url"
	"time"

	playwright "github.com/playwright-community/playwright-go"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
)

// BrowserNavigateTool opens a URL in the shared browser session
type BrowserNavigateTool struct {
	browserToolBase
	config *config.Config
}

// NewBrowserNavigateTool creates a new browser navigate tool
func NewBrowserNavigateTool(cfg *config.Config, rateLimiter domain.RateLimiter, session *browserSession) *BrowserNavigateTool {
	return &BrowserNavigateTool{
		browserToolBase: browserToolBase{
			name:        "BrowserNavigate",
			enabled:     cfg.BrowserUse.Enabled && cfg.BrowserUse.Tools.Navigate.Enabled,
			session:     session,
			rateLimiter: rateLimiter,
		},
		config: cfg,
	}
}

// Definition returns the tool definition for the LLM
func (t *BrowserNavigateTool) Definition() sdk.ChatCompletionTool {
	description := t.config.Prompts.Tools.BrowserNavigate.Description
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "BrowserNavigate",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"url": map[string]any{
						"type":        "string",
						"description": "URL to navigate to, including scheme (e.g. https://example.com)",
					},
				},
				"required": []string{"url"},
			},
		},
	}
}

// Execute runs the browser navigate tool with given arguments
func (t *BrowserNavigateTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	start := time.Now()

	if err := t.checkRateLimit(); err != nil {
		return t.errorResult(args, start, err.Error()), nil
	}

	target, err := requireString(args, "url")
	if err != nil {
		return t.errorResult(args, start, err.Error()), nil
	}

	page, err := t.session.Page()
	if err != nil {
		return t.errorResult(args, start, err.Error()), nil
	}

	if _, err := page.Goto(target, playwright.PageGotoOptions{
		Timeout: playwright.Float(t.session.timeoutMs()),
	}); err != nil {
		return t.errorResult(args, start, fmt.Sprintf("failed to navigate to %s: %v", target, err)), nil
	}

	title, _ := page.Title()
	return t.successResult(args, start, domain.BrowserToolResult{
		Action: "navigate",
		URL:    page.URL(),
		Title:  title,
	}), nil
}

// Validate checks if the tool arguments are valid
func (t *BrowserNavigateTool) Validate(args map[string]any) error {
	target, err := requireString(args, "url")
	if err != nil {
		return err
	}
	parsed, err := url.Parse(target)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("url must be absolute and include a scheme, e.g. https://example.com")
	}
	return nil
}
