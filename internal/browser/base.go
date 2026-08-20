package browser

import (
	"fmt"
	"strings"
	"time"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	browserdomain "github.com/inference-gateway/cli/internal/browser/domain"
)

// rateLimiter is the slice of the shared rate limiter the browser tools use.
type rateLimiter interface {
	CheckAndRecord(toolName string) error
}

// browserToolBase carries the pieces every browser tool shares: the driver,
// rate limiting, enablement, and result formatting.
type browserToolBase struct {
	name        string
	enabled     bool
	driver      browserdomain.BrowserDriver
	rateLimiter rateLimiter
}

// IsEnabled returns whether this tool is enabled
func (b *browserToolBase) IsEnabled() bool {
	return b.enabled
}

// ShouldCollapseArg determines if an argument should be collapsed in display
func (b *browserToolBase) ShouldCollapseArg(string) bool {
	return false
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (b *browserToolBase) ShouldAlwaysExpand() bool {
	return false
}

// FormatResult formats tool execution results for different contexts
func (b *browserToolBase) FormatResult(result *agentdomain.ToolExecutionResult, formatType agentdomain.FormatterType) string {
	if formatType == agentdomain.FormatterShort {
		return b.FormatPreview(result)
	}
	return b.FormatForLLM(result)
}

// FormatPreview returns a short preview of the result for UI display
func (b *browserToolBase) FormatPreview(result *agentdomain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return fmt.Sprintf("%s failed", b.name)
	}
	data, ok := result.Data.(browserdomain.BrowserToolResult)
	if !ok {
		return fmt.Sprintf("%s succeeded", b.name)
	}
	target := data.Selector
	if target == "" {
		target = data.URL
	}
	return strings.TrimSpace(fmt.Sprintf("%s %s", data.Action, target))
}

// FormatForLLM formats the result for LLM consumption
func (b *browserToolBase) FormatForLLM(result *agentdomain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return fmt.Sprintf("Error: %s", result.Error)
	}
	data, ok := result.Data.(browserdomain.BrowserToolResult)
	if !ok {
		return fmt.Sprintf("%s succeeded", b.name)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Performed %s", data.Action)
	if data.Selector != "" {
		fmt.Fprintf(&sb, " on %q", data.Selector)
	}
	if data.URL != "" {
		fmt.Fprintf(&sb, " - page: %s", data.URL)
	}
	if data.Title != "" {
		fmt.Fprintf(&sb, " (%s)", data.Title)
	}
	if data.Content != "" {
		fmt.Fprintf(&sb, "\n\nPage content:\n%s", data.Content)
	}
	if len(data.Events) > 0 {
		fmt.Fprintf(&sb, "\n\nBrowser events:\n%s", strings.Join(data.Events, "\n"))
	}
	return sb.String()
}

// checkRateLimit applies the shared browser rate limit.
func (b *browserToolBase) checkRateLimit() error {
	return b.rateLimiter.CheckAndRecord(b.name)
}

func (b *browserToolBase) errorResult(args map[string]any, start time.Time, errorMsg string) *agentdomain.ToolExecutionResult {
	return &agentdomain.ToolExecutionResult{
		ToolName:  b.name,
		Arguments: args,
		Success:   false,
		Duration:  time.Since(start),
		Error:     errorMsg,
	}
}

func (b *browserToolBase) successResult(args map[string]any, start time.Time, data browserdomain.BrowserToolResult) *agentdomain.ToolExecutionResult {
	return &agentdomain.ToolExecutionResult{
		ToolName:  b.name,
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data:      data,
	}
}

func requireString(args map[string]any, key string) (string, error) {
	v, ok := args[key].(string)
	if !ok || strings.TrimSpace(v) == "" {
		return "", fmt.Errorf("%s is required and must be a non-empty string", key)
	}
	return v, nil
}
