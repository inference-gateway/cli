// Package browser is the browser-use capability: Playwright-driven (or
// extension-bridged) page automation exposed to the agent as agentdomain.Tool
// implementations. Playwright is imported only by this package.
package browser

import (
	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	browserdomain "github.com/inference-gateway/cli/internal/browser/domain"
	utils "github.com/inference-gateway/cli/internal/platform/utils"
)

// NewTools builds the browser tool set against one shared driver and rate
// limiter. The caller (the container) owns the driver's lifecycle.
func NewTools(cfg *config.Config, driver browserdomain.BrowserDriver) map[string]agentdomain.Tool {
	rateLimiter := utils.NewRateLimiter(cfg.BrowserUse.RateLimit)
	return map[string]agentdomain.Tool{
		"BrowserNavigate":   NewBrowserNavigateTool(cfg, rateLimiter, driver),
		"BrowserClick":      NewBrowserClickTool(cfg, rateLimiter, driver),
		"BrowserType":       NewBrowserTypeTool(cfg, rateLimiter, driver),
		"BrowserRead":       NewBrowserReadTool(cfg, rateLimiter, driver),
		"BrowserScreenshot": NewBrowserScreenshotTool(cfg, rateLimiter, driver),
		"BrowserTabs":       NewBrowserTabsTool(cfg, rateLimiter, driver),
	}
}
