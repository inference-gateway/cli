package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	display "github.com/inference-gateway/cli/internal/display"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	utils "github.com/inference-gateway/cli/internal/utils"
	sdk "github.com/inference-gateway/sdk"

	_ "github.com/inference-gateway/cli/internal/display/macos"
	_ "github.com/inference-gateway/cli/internal/display/wayland"
	_ "github.com/inference-gateway/cli/internal/display/x11"
)

// Registry manages all available tools
type Registry struct {
	config       domain.ConfigService
	tools        map[string]domain.Tool
	readToolUsed bool
	taskTracker  domain.TaskTracker
	imageService domain.ImageService
	mcpManager   domain.MCPManager
	shellService domain.BackgroundShellService
}

// NewRegistry creates a new tool registry with self-contained tools
func NewRegistry(cfg domain.ConfigService, imageService domain.ImageService, mcpManager domain.MCPManager, shellService domain.BackgroundShellService) *Registry {
	registry := &Registry{
		config:       cfg,
		tools:        make(map[string]domain.Tool),
		shellService: shellService,
		readToolUsed: false,
		taskTracker:  utils.NewTaskTracker(),
		imageService: imageService,
		mcpManager:   mcpManager,
	}

	registry.registerTools()
	return registry
}

// registerTools initializes and registers all available tools
func (r *Registry) registerTools() {
	cfg := r.config.GetConfig()

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

	if cfg.Tools.WebFetch.Enabled {
		r.tools["WebFetch"] = NewWebFetchTool(cfg)
	}

	if cfg.Tools.WebSearch.Enabled {
		r.tools["WebSearch"] = NewWebSearchTool(cfg)
	}

	if cfg.Tools.Github.Enabled {
		r.tools["Github"] = NewGithubTool(cfg, r.imageService)
	}

	if cfg.IsA2AToolsEnabled() {
		r.tools["A2A_QueryAgent"] = NewA2AQueryAgentTool(cfg)
		r.tools["A2A_QueryTask"] = NewA2AQueryTaskTool(cfg, r.taskTracker)
		r.tools["A2A_SubmitTask"] = NewA2ASubmitTaskTool(cfg, r.taskTracker)
	}

	if cfg.ComputerUse.Enabled {
		displayProvider, err := display.DetectDisplay()
		if err != nil {
			logger.Warn("No compatible display platform detected, computer use tools will be disabled", "error", err)
		} else {
			rateLimiter := utils.NewRateLimiter(cfg.ComputerUse.RateLimit)
			r.tools["MouseMove"] = NewMouseMoveTool(cfg, rateLimiter, displayProvider)
			r.tools["MouseClick"] = NewMouseClickTool(cfg, rateLimiter, displayProvider)
			r.tools["MouseScroll"] = NewMouseScrollTool(cfg, rateLimiter, displayProvider)
			r.tools["KeyboardType"] = NewKeyboardTypeTool(cfg, rateLimiter, displayProvider)
			r.tools["GetFocusedApp"] = NewGetFocusedAppTool(r.config)
			r.tools["ActivateApp"] = NewActivateAppTool(r.config)
		}
	}

	if cfg.MCP.Enabled && r.mcpManager != nil {
		r.registerMCPTools()
	}
}

// registerMCPTools discovers and registers tools from enabled MCP servers
func (r *Registry) registerMCPTools() {
	cfg := r.config.GetConfig()
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.MCP.DiscoveryTimeout)*time.Second)
	defer cancel()

	toolCount := 0
	clients := r.mcpManager.GetClients()

	for _, client := range clients {
		discoveredTools, err := client.DiscoverTools(ctx)
		if err != nil {
			logger.Debug("MCP server not ready yet, will retry via liveness probe", "error", err)
			continue
		}

		for serverName, tools := range discoveredTools {
			count := r.RegisterMCPServerTools(serverName, tools)
			toolCount += count
		}
	}

	if toolCount > 0 {
		logger.Debug("Successfully registered MCP tools", "count", toolCount)
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

// RegisterMCPServerTools dynamically registers tools from an MCP server
func (r *Registry) RegisterMCPServerTools(serverName string, tools []domain.MCPDiscoveredTool) int {
	if r.mcpManager == nil {
		return 0
	}

	var targetClient domain.MCPClient
	for _, client := range r.mcpManager.GetClients() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		discovered, err := client.DiscoverTools(ctx)
		cancel()

		if err == nil {
			for sname := range discovered {
				if sname == serverName {
					targetClient = client
					break
				}
			}
		}
		if targetClient != nil {
			break
		}
	}

	if targetClient == nil {
		logger.Warn("Could not find MCP client for server", "server", serverName)
		return 0
	}

	toolCount := 0
	cfg := r.config.GetConfig()

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

		logger.Info("Dynamically registered MCP tool",
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
		logger.Debug("Unregistered MCP tools from disconnected server", "server", serverName, "count", removedCount)
		r.mcpManager.ClearToolCount(serverName)
	}

	return removedCount
}

// SetScreenshotServer dynamically registers the GetLatestScreenshot tool
// This should be called after the screenshot server is started
func (r *Registry) SetScreenshotServer(provider domain.ScreenshotProvider) {
	cfg := r.config.GetConfig()
	if !cfg.ComputerUse.Enabled || !cfg.ComputerUse.Screenshot.StreamingEnabled {
		logger.Debug("Screenshot streaming not enabled, skipping GetLatestScreenshot tool registration")
		return
	}

	if provider == nil {
		logger.Warn("Screenshot provider is nil, cannot register GetLatestScreenshot tool")
		return
	}

	getLatestTool := NewGetLatestScreenshotTool(cfg, provider)
	r.tools["GetLatestScreenshot"] = getLatestTool

	logger.Info("Dynamically registered GetLatestScreenshot tool for streaming mode")
}

// SetReadToolUsed marks that the Read tool has been used
func (r *Registry) SetReadToolUsed() {
	r.readToolUsed = true
}

// IsReadToolUsed returns whether the Read tool has been used
func (r *Registry) IsReadToolUsed() bool {
	return r.readToolUsed
}

// GetTaskTracker returns the task tracker instance
func (r *Registry) GetTaskTracker() domain.TaskTracker {
	return r.taskTracker
}

// GetBackgroundShellService returns the background shell service instance
func (r *Registry) GetBackgroundShellService() domain.BackgroundShellService {
	return r.shellService
}
