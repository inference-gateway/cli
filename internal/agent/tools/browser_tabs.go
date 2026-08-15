package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
)

// BrowserTabsTool lists the browser's open tabs and marks the active one, so
// the agent knows what the user is currently looking at.
type BrowserTabsTool struct {
	browserToolBase
	config *config.Config
}

// NewBrowserTabsTool creates a new browser tabs tool
func NewBrowserTabsTool(cfg *config.Config, rateLimiter domain.RateLimiter, driver domain.BrowserDriver) *BrowserTabsTool {
	return &BrowserTabsTool{
		browserToolBase: browserToolBase{
			name:        "BrowserTabs",
			enabled:     cfg.BrowserUse.Enabled && cfg.BrowserUse.Tools.Tabs.Enabled,
			driver:      driver,
			rateLimiter: rateLimiter,
		},
		config: cfg,
	}
}

// Definition returns the tool definition for the LLM
func (t *BrowserTabsTool) Definition() sdk.ChatCompletionTool {
	description := t.config.Prompts.Tools.BrowserTabs.Description
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "BrowserTabs",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type":       "object",
				"properties": map[string]any{},
				"required":   []string{},
			},
		},
	}
}

// Execute lists the open tabs
func (t *BrowserTabsTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	start := time.Now()

	if err := t.checkRateLimit(); err != nil {
		return t.errorResult(args, start, err.Error()), nil
	}

	tabs, err := t.driver.Tabs(ctx)
	if err != nil {
		return t.errorResult(args, start, err.Error()), nil
	}
	return &domain.ToolExecutionResult{
		ToolName:  t.name,
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data:      tabs,
	}, nil
}

// Validate checks if the tool arguments are valid
func (t *BrowserTabsTool) Validate(map[string]any) error {
	return nil
}

// FormatResult formats tool execution results for different contexts
func (t *BrowserTabsTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
	if formatType == domain.FormatterShort {
		return t.FormatPreview(result)
	}
	return t.FormatForLLM(result)
}

// FormatPreview returns a short preview of the result for UI display
func (t *BrowserTabsTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return "BrowserTabs failed"
	}
	tabs, _ := result.Data.([]domain.BrowserTab)
	return fmt.Sprintf("%d tab(s)", len(tabs))
}

// FormatForLLM formats the tab list for LLM consumption
func (t *BrowserTabsTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return fmt.Sprintf("Error: %s", result.Error)
	}
	tabs, _ := result.Data.([]domain.BrowserTab)
	if len(tabs) == 0 {
		return "No open tabs."
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d open tab(s) (* marks the active tab):\n", len(tabs))
	for _, tab := range tabs {
		marker := " "
		if tab.Active {
			marker = "*"
		}
		title := tab.Title
		if title == "" {
			title = "(untitled)"
		}
		fmt.Fprintf(&sb, "[%s] %d  %s — %s\n", marker, tab.Index, title, tab.URL)
	}
	return strings.TrimRight(sb.String(), "\n")
}
