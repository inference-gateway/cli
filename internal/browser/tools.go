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
		ToolNavigate:   NewBrowserNavigateTool(cfg, rateLimiter, driver),
		ToolClick:      NewBrowserClickTool(cfg, rateLimiter, driver),
		ToolType:       NewBrowserTypeTool(cfg, rateLimiter, driver),
		ToolRead:       NewBrowserReadTool(cfg, rateLimiter, driver),
		ToolScreenshot: NewBrowserScreenshotTool(cfg, rateLimiter, driver),
		ToolTabs:       NewBrowserTabsTool(cfg, rateLimiter, driver),
	}
}
