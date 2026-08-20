// Package computer is the computer-use capability: mouse, keyboard, screen,
// and accessibility tools exposed to the agent as agentdomain.Tool implementations.
// Display backends (and robotgo) live only under this package tree.
package computer

import (
	"runtime"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	display "github.com/inference-gateway/cli/internal/computer/infrastructure/display"
	_ "github.com/inference-gateway/cli/internal/computer/infrastructure/display/macos"
	_ "github.com/inference-gateway/cli/internal/computer/infrastructure/display/wayland"
	_ "github.com/inference-gateway/cli/internal/computer/infrastructure/display/x11"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	utils "github.com/inference-gateway/cli/internal/utils"
)

// FocusManager handles macOS computer-use focus tracking.
type FocusManager interface {
	SetLastFocusedApp(appID string)
	GetLastFocusedApp() string
	ClearLastFocusedApp()
	SetLastClickCoordinates(x, y int)
	GetLastClickCoordinates() (x, y int)
	ClearLastClickCoordinates()
}

// State is the narrow slice of the state manager the computer-use tools need:
// broadcasting UI events (MouseMove/MouseClick) and recording the focused app
// and last click coordinates (MouseClick).
type State interface {
	domain.EventBridgeManager
	FocusManager
}

// FrameSourceLookup resolves named frame sources; the tool registry satisfies it.
type FrameSourceLookup interface {
	FrameSource(name string) (agentdomain.FrameSource, bool)
	FrameSourceNames() []string
}

// NewTools builds the computer-use tool set. GetLatestFrame is always included
// (it also serves non-screen frame sources such as cameras); the input tools
// are gated on computer_use.enabled and require a supported display platform.
// rateLimiter is the slice of the shared rate limiter the computer tools use.
type rateLimiter interface {
	CheckAndRecord(toolName string) error
}

func NewTools(cfg *config.Config, state State, frames FrameSourceLookup, annotator agentdomain.ImageAnnotator) map[string]agentdomain.Tool {
	tools := map[string]agentdomain.Tool{
		"GetLatestFrame": NewGetLatestFrameTool(cfg, frames, annotator),
	}

	if !cfg.ComputerUse.Enabled {
		return tools
	}
	if runtime.GOOS == "windows" {
		logger.Warn("computer use is not supported on Windows - mouse, keyboard, and screenshot tools will be disabled")
		return tools
	}
	displayProvider, err := display.DetectDisplay()
	if err != nil {
		logger.Warn("no compatible display platform detected, computer use tools will be disabled", "error", err)
		return tools
	}

	rateLimiter := utils.NewRateLimiter(cfg.ComputerUse.RateLimit)
	tools["MouseMove"] = NewMouseMoveTool(cfg, rateLimiter, displayProvider, state)
	tools["MouseClick"] = NewMouseClickTool(cfg, rateLimiter, displayProvider, state)
	tools["MouseScroll"] = NewMouseScrollTool(cfg, rateLimiter, displayProvider)
	tools["KeyboardType"] = NewKeyboardTypeTool(cfg, rateLimiter, displayProvider)
	tools["GetFocusedApp"] = NewGetFocusedAppTool(cfg)
	tools["ActivateApp"] = NewActivateAppTool(cfg)
	tools["GetUIElements"] = NewGetUIElementsTool(cfg)
	tools["PressUIElement"] = NewPressUIElementTool(cfg)
	return tools
}
