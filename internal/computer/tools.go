// Package computer is the computer-use capability: the Computer tool drives
// the mouse, keyboard, and screen through robotgo; GetLatestFrame serves
// frames from the screen ring buffer and configured frame sources.
package computer

import (
	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	utils "github.com/inference-gateway/cli/internal/platform/utils"
)

// FrameSourceLookup resolves named frame sources; the tool registry satisfies it.
type FrameSourceLookup interface {
	FrameSource(name string) (agentdomain.FrameSource, bool)
	FrameSourceNames() []string
}

// rateLimiter is the slice of the shared rate limiter the computer tools use.
type rateLimiter interface {
	CheckAndRecord(toolName string) error
}

// NewTools builds the computer-use tool set. GetLatestFrame is always included
// (it also serves non-screen frame sources such as cameras); the Computer tool
// is gated on computer_use.enabled.
func NewTools(cfg *config.Config, frames FrameSourceLookup, annotator agentdomain.ImageAnnotator) map[string]agentdomain.Tool {
	tools := map[string]agentdomain.Tool{
		"GetLatestFrame": NewGetLatestFrameTool(cfg, frames, annotator),
	}
	if cfg.ComputerUse.Enabled {
		tools["Computer"] = NewComputerTool(cfg, utils.NewRateLimiter(cfg.ComputerUse.RateLimit))
	}
	return tools
}
