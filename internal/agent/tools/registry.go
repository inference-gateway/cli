package tools

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	config "github.com/inference-gateway/cli/config"
	display "github.com/inference-gateway/cli/internal/display"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	utils "github.com/inference-gateway/cli/internal/utils"
	sdk "github.com/inference-gateway/sdk"

	_ "github.com/inference-gateway/cli/internal/display/macos"
	_ "github.com/inference-gateway/cli/internal/display/wayland"
	_ "github.com/inference-gateway/cli/internal/display/x11"
)

// Note: this file deliberately does NOT call DiscoverTools synchronously at
// construction time. MCP tool discovery is handled asynchronously by the
// liveness probe loop in MCPManager.StartMonitoring (see
// internal/services/mcp_manager.go) which emits MCPServerStatusUpdateEvent
// once a server is reachable, and ChatApplication.handleMCPStatusUpdate
// (internal/app/chat.go) then invokes RegisterMCPServerTools below to
// install the discovered tools.
//
// Calling DiscoverTools here would block container construction (and
// therefore the bubbletea TUI startup) on sequential HTTP round trips to
// every configured MCP server - see issue #523.

// Registry manages all available tools
type Registry struct {
	config             *config.Config
	tools              map[string]domain.Tool
	readToolUsed       bool
	readFiles          map[string]fileReadSnapshot
	readFilesMu        sync.Mutex
	taskTracker        domain.A2ATaskTracker
	subagentTracker    domain.SubagentTracker
	jobSubmitter       domain.JobSubmitter
	jobStopper         domain.JobStopper
	imageService       domain.ImageService
	mcpManager         domain.MCPManager
	shellService       domain.BackgroundShellService
	stateManager       domain.StateManager
	screenshotProvider domain.ScreenshotProvider
}

// NewRegistry creates a new tool registry with self-contained tools.
// taskTracker must be provided by the caller (typically the container, which
// constructs the unified BackgroundTaskRegistry and passes its A2A view in
// here so all tools observe the same tracker the agent's wait loop does).
func NewRegistry(cfg *config.Config, imageService domain.ImageService, mcpManager domain.MCPManager, shellService domain.BackgroundShellService, stateManager domain.StateManager, screenshotProvider domain.ScreenshotProvider, taskTracker domain.A2ATaskTracker) *Registry {
	if taskTracker == nil {
		taskTracker = utils.NewA2ATaskTracker()
	}
	registry := &Registry{
		config:             cfg,
		tools:              make(map[string]domain.Tool),
		shellService:       shellService,
		readToolUsed:       false,
		readFiles:          make(map[string]fileReadSnapshot),
		taskTracker:        taskTracker,
		imageService:       imageService,
		mcpManager:         mcpManager,
		stateManager:       stateManager,
		screenshotProvider: screenshotProvider,
	}
	if st, ok := taskTracker.(domain.SubagentTracker); ok {
		registry.subagentTracker = st
	}
	if js, ok := taskTracker.(domain.JobSubmitter); ok {
		registry.jobSubmitter = js
	}
	if jst, ok := taskTracker.(domain.JobStopper); ok {
		registry.jobStopper = jst
	}

	registry.registerTools()
	return registry
}

// SetScreenshotProvider updates the screenshot provider for tools that need it
func (r *Registry) SetScreenshotProvider(provider domain.ScreenshotProvider) {
	r.screenshotProvider = provider

	cfg := r.config
	if cfg.ComputerUse.Enabled {
		displayProvider, err := display.DetectDisplay()
		if err == nil {
			rateLimiter := utils.NewRateLimiter(cfg.ComputerUse.RateLimit)
			r.tools["MouseClick"] = NewMouseClickTool(cfg, rateLimiter, displayProvider, r.stateManager)
		}
	}
}

// registerTools initializes and registers all available tools
func (r *Registry) registerTools() {
	cfg := r.config

	r.tools["Bash"] = NewBashTool(cfg, r.shellService)

	if cfg.Tools.Bash.BackgroundShells.Enabled && r.shellService != nil {
		r.tools["BashOutput"] = NewBashOutputTool(cfg, r.shellService)
		r.tools["KillShell"] = NewKillShellTool(cfg, r.shellService)
		r.tools["ListShells"] = NewListShellsTool(cfg, r.shellService)
	}

	r.tools["Read"] = NewReadTool(cfg)
	r.tools["Write"] = NewWriteTool(cfg)
	r.tools["Edit"] = NewEditToolWithRegistry(cfg, r)
	r.tools["MultiEdit"] = NewMultiEditToolWithRegistry(cfg, r)
	r.tools["Delete"] = NewDeleteTool(cfg)
	r.tools["Grep"] = NewGrepTool(cfg)
	r.tools["Tree"] = NewTreeTool(cfg)
	r.tools["TodoWrite"] = NewTodoWriteTool(cfg)
	r.tools["RequestPlanApproval"] = NewRequestPlanApprovalTool(cfg)

	if cfg.Tools.AskUserQuestion.Enabled {
		r.tools["AskUserQuestion"] = NewAskUserQuestionTool(cfg)
	}

	if cfg.Tools.Schedule.Enabled {
		r.tools["Schedule"] = NewScheduleTool(cfg)
	}

	if cfg.IsAgentToolEnabled() && r.subagentTracker != nil {
		r.tools["Agent"] = NewAgentTool(cfg, r.subagentTracker, r.jobSubmitter)
		r.tools["ListSubagents"] = NewListSubagentsTool(cfg, r.subagentTracker)
		r.tools["GetSubagentResult"] = NewGetSubagentResultTool(cfg, r.subagentTracker)
		r.tools["CloseSubagent"] = NewCloseSubagentTool(cfg, r.subagentTracker, r.jobStopper)
		r.tools["ReadSubagentScreen"] = NewReadSubagentScreenTool(cfg, r.subagentTracker)
		r.tools["SendSubagentInput"] = NewSendSubagentInputTool(cfg, r.subagentTracker)
		r.tools["ApproveSubagent"] = NewApproveSubagentTool(cfg, r.subagentTracker)
	}

	if cfg.Tools.WebFetch.Enabled {
		r.tools["WebFetch"] = NewWebFetchTool(cfg)
	}

	if cfg.Tools.WebSearch.Enabled {
		r.tools["WebSearch"] = NewWebSearchTool(cfg)
	}

	if cfg.IsA2AToolsEnabled() {
		r.tools["A2A_QueryAgent"] = NewA2AQueryAgentTool(cfg)
		r.tools["A2A_QueryTask"] = NewA2AQueryTaskTool(cfg, r.taskTracker)
		r.tools["A2A_SubmitTask"] = NewA2ASubmitTaskTool(cfg, r.taskTracker, r.jobSubmitter)
	}

	if cfg.ComputerUse.Enabled {
		displayProvider, err := display.DetectDisplay()
		if err != nil {
			logger.Warn("no compatible display platform detected, computer use tools will be disabled", "error", err)
		} else {
			rateLimiter := utils.NewRateLimiter(cfg.ComputerUse.RateLimit)
			r.tools["MouseMove"] = NewMouseMoveTool(cfg, rateLimiter, displayProvider, r.stateManager)
			r.tools["MouseClick"] = NewMouseClickTool(cfg, rateLimiter, displayProvider, r.stateManager)
			r.tools["MouseScroll"] = NewMouseScrollTool(cfg, rateLimiter, displayProvider)
			r.tools["KeyboardType"] = NewKeyboardTypeTool(cfg, rateLimiter, displayProvider)
			r.tools["GetFocusedApp"] = NewGetFocusedAppTool(r.config)
			r.tools["ActivateApp"] = NewActivateAppTool(r.config)
		}
	}

	if cfg.Memory.Enabled {
		r.tools["Memory"] = NewMemoryTool(cfg)
	}
}

// GetTool retrieves a tool by name
func (r *Registry) GetTool(name string) (domain.Tool, error) {
	tool, exists := r.tools[name]
	if !exists {
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
	return tool, nil
}

// ListAvailableTools returns names of all available and enabled tools
func (r *Registry) ListAvailableTools() []string {
	var tools []string
	for name, tool := range r.tools {
		if tool.IsEnabled() {
			tools = append(tools, name)
		}
	}
	return tools
}

// GetToolDefinitions returns definitions for all enabled tools
func (r *Registry) GetToolDefinitions() []sdk.ChatCompletionTool {
	var definitions []sdk.ChatCompletionTool
	for _, tool := range r.tools {
		if tool.IsEnabled() {
			definitions = append(definitions, tool.Definition())
		}
	}
	return definitions
}

// IsToolEnabled checks if a specific tool is enabled
func (r *Registry) IsToolEnabled(name string) bool {
	tool, exists := r.tools[name]
	if !exists {
		return false
	}
	return tool.IsEnabled()
}

// RegisterMCPServerTools dynamically registers tools from an MCP server.
// The serverName must match a client registered with the MCPManager - the
// lookup is O(1) via MCPManager.GetClient and performs no network I/O.
func (r *Registry) RegisterMCPServerTools(serverName string, tools []domain.MCPDiscoveredTool) int {
	if r.mcpManager == nil {
		return 0
	}

	targetClient := r.mcpManager.GetClient(serverName)
	if targetClient == nil {
		logger.Warn("could not find MCP client for server", "server", serverName)
		return 0
	}

	toolCount := 0
	cfg := r.config

	for _, tool := range tools {
		fullToolName := fmt.Sprintf("MCP_%s_%s", serverName, tool.Name)

		mcpTool := NewMCPTool(
			serverName,
			tool.Name,
			tool.Description,
			tool.InputSchema,
			targetClient,
			&cfg.MCP,
		)

		r.tools[fullToolName] = mcpTool
		toolCount++

		logger.Info("dynamically registered MCP tool",
			"tool", fullToolName,
			"server", serverName,
			"description", tool.Description)
	}

	r.mcpManager.UpdateToolCount(serverName, toolCount)

	return toolCount
}

// UnregisterMCPServerTools removes all tools from a specific MCP server
func (r *Registry) UnregisterMCPServerTools(serverName string) int {
	removedCount := 0
	prefix := fmt.Sprintf("MCP_%s_", serverName)

	for toolName := range r.tools {
		if strings.HasPrefix(toolName, prefix) {
			delete(r.tools, toolName)
			removedCount++
		}
	}

	if removedCount > 0 {
		logger.Debug("unregistered MCP tools from disconnected server", "server", serverName, "count", removedCount)
		r.mcpManager.ClearToolCount(serverName)
	}

	return removedCount
}

// SetScreenshotServer dynamically registers the GetLatestScreenshot tool
// This should be called after the screenshot server is started
func (r *Registry) SetScreenshotServer(provider domain.ScreenshotProvider) {
	cfg := r.config
	if !cfg.ComputerUse.Enabled || !cfg.ComputerUse.Screenshot.StreamingEnabled {
		logger.Debug("screenshot streaming not enabled, skipping GetLatestScreenshot tool registration")
		return
	}

	if provider == nil {
		logger.Warn("screenshot provider is nil, cannot register GetLatestScreenshot tool")
		return
	}

	r.SetScreenshotProvider(provider)

	getLatestTool := NewGetLatestScreenshotTool(cfg, provider)
	r.tools["GetLatestScreenshot"] = getLatestTool

	logger.Info("dynamically registered GetLatestScreenshot tool for streaming mode")
}

// SetReadToolUsed marks that the Read tool has been used
func (r *Registry) SetReadToolUsed() {
	r.readToolUsed = true
}

// IsReadToolUsed returns whether the Read tool has been used
func (r *Registry) IsReadToolUsed() bool {
	return r.readToolUsed
}

// fileReadSnapshot captures a file's state the last time the agent read or wrote it, so a later
// edit can detect that the file changed underneath it.
type fileReadSnapshot struct {
	modTime time.Time
	size    int64
}

// RecordFileRead snapshots a file's modtime/size, keyed by its absolute path. Called when the
// Read tool reads a file and refreshed after Edit/MultiEdit/Write so the agent's own writes do
// not look like external modifications.
func (r *Registry) RecordFileRead(path string, modTime time.Time, size int64) {
	key := normalizeReadPath(path)
	r.readFilesMu.Lock()
	defer r.readFilesMu.Unlock()
	r.readFiles[key] = fileReadSnapshot{modTime: modTime, size: size}
}

// LastReadInfo returns the snapshot recorded for path (by absolute path) and whether one exists.
func (r *Registry) LastReadInfo(path string) (time.Time, int64, bool) {
	key := normalizeReadPath(path)
	r.readFilesMu.Lock()
	defer r.readFilesMu.Unlock()
	snap, ok := r.readFiles[key]
	return snap.modTime, snap.size, ok
}

// normalizeReadPath resolves path to an absolute, cleaned form so read and edit sites agree on
// the map key regardless of whether the model passed a relative or absolute path.
func normalizeReadPath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return filepath.Clean(path)
}

// GetA2ATaskTracker returns the task tracker instance
func (r *Registry) GetA2ATaskTracker() domain.A2ATaskTracker {
	return r.taskTracker
}

// GetBackgroundShellService returns the background shell service instance
func (r *Registry) GetBackgroundShellService() domain.BackgroundShellService {
	return r.shellService
}

// IsComputerUseTool returns true if the given tool name is a computer use tool
// Computer use tools operate directly on the computer (mouse, keyboard, screenshot)
// and bypass the standard approval flow
func IsComputerUseTool(toolName string) bool {
	computerUseTools := map[string]bool{
		"MouseClick":          true,
		"MouseMove":           true,
		"MouseScroll":         true,
		"KeyboardType":        true,
		"ActivateApp":         true,
		"GetFocusedApp":       true,
		"GetLatestScreenshot": true,
	}
	return computerUseTools[toolName]
}
