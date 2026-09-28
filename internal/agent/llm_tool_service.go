package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	tools "github.com/inference-gateway/cli/internal/tools"
)

// LLMToolService implements ToolService with the new tools package architecture
type LLMToolService struct {
	registry  *tools.Registry
	enabled   bool
	config    *config.Config
	allowlist map[string]bool
}

// NewLLMToolServiceWithRegistry creates a new LLM tool service with an existing registry
func NewLLMToolServiceWithRegistry(cfg *config.Config, registry *tools.Registry) *LLMToolService {
	s := &LLMToolService{
		registry: registry,
		enabled:  cfg.Tools.Enabled,
		config:   cfg,
	}
	// A named Markdown subagent receives its tool allowlist through the
	// SubagentToolsEnv channel; when set, only those tools are advertised to
	// the model and accepted for execution.
	if raw := os.Getenv(tools.SubagentToolsEnv); raw != "" {
		s.allowlist = make(map[string]bool)
		for _, name := range strings.Split(raw, ",") {
			if name = strings.TrimSpace(name); name != "" {
				s.allowlist[name] = true
			}
		}
	}
	return s
}

// isToolEnabled checks if a tool should be included based on its type and configuration
func (s *LLMToolService) isToolEnabled(toolName string) bool {
	if s.allowlist != nil && !s.allowlist[toolName] {
		return false
	}
	return (s.enabled || s.isSelfGated(toolName)) && s.registry.IsToolEnabled(toolName)
}

// isSelfGated reports whether the tool's own context, not tools.enabled,
// switches it on.
func (s *LLMToolService) isSelfGated(toolName string) bool {
	tool, err := s.registry.GetTool(toolName)
	if err != nil {
		return false
	}
	gated, ok := tool.(agentdomain.SelfGatedTool)
	return ok && gated.SelfGated()
}

// isToolAdvertised reports whether a tool should be offered to the LLM.
func (s *LLMToolService) isToolAdvertised(toolName string) bool {
	return s.isToolEnabled(toolName)
}

// ListTools returns definitions for all enabled tools
func (s *LLMToolService) ListTools() []sdk.ChatCompletionTool {
	var definitions []sdk.ChatCompletionTool

	allTools := s.registry.GetToolDefinitions()
	for _, tool := range allTools {
		if s.isToolAdvertised(tool.Function.Name) {
			definitions = append(definitions, tool)
		}
	}

	return definitions
}

// ListToolsForMode returns definitions for enabled tools filtered by agent mode
func (s *LLMToolService) ListToolsForMode(mode agentdomain.AgentMode) []sdk.ChatCompletionTool {
	var definitions []sdk.ChatCompletionTool
	for _, tool := range s.registry.GetToolDefinitions() {
		name := tool.Function.Name
		if s.isToolAdvertised(name) && s.registry.Manifest(name).AvailableIn(mode) {
			definitions = append(definitions, tool)
		}
	}
	return definitions
}

// Manifest returns the named tool's manifest, whichever bounded context
// defines the tool.
func (s *LLMToolService) Manifest(name string) agentdomain.ToolManifest {
	return s.registry.Manifest(name)
}

// ListAvailableTools returns names of all enabled tools
func (s *LLMToolService) ListAvailableTools() []string {
	var tools []string

	allTools := s.registry.ListAvailableTools()
	for _, toolName := range allTools {
		if s.isToolAdvertised(toolName) {
			tools = append(tools, toolName)
		}
	}

	return tools
}

// ListMarkdownSubagents returns the Markdown-defined subagent presets loaded
// this session (.infer/agents/*.md and ~/.infer/agents/*.md), for /agents.
func (s *LLMToolService) ListMarkdownSubagents() []agentdomain.SubagentInfo {
	return s.registry.MarkdownSubagents()
}

// toolUnavailableError tells the model that a tool it called does not run in
// the current mode, so it can pick another instead of retrying.
func toolUnavailableError(name string, mode agentdomain.AgentMode) error {
	if mode == agentdomain.AgentModePlan {
		return fmt.Errorf("tool not allowed: %s is disabled in plan mode (read-only) - use %s/%s/%s to research, %s to clarify, and %s to submit the plan; do not retry this tool until the plan is approved",
			name, tools.ToolRead, tools.ToolGrep, tools.ToolTree, tools.ToolAskUserQuestion, tools.ToolRequestPlanApproval)
	}
	return fmt.Errorf("tool not allowed: %s is not available in %s mode", name, mode.ModeKey())
}

// ExecuteTool executes a tool with the given arguments
func (s *LLMToolService) ExecuteTool(ctx context.Context, toolCall sdk.ChatCompletionMessageToolCallFunction) (*agentdomain.ToolExecutionResult, error) {
	if mode, ok := agentdomain.AgentModeFromContext(ctx); ok && !s.registry.Manifest(toolCall.Name).AvailableIn(mode) {
		return nil, toolUnavailableError(toolCall.Name, mode)
	}

	if !s.isToolEnabled(toolCall.Name) {
		return nil, fmt.Errorf("tool %s is not enabled", toolCall.Name)
	}

	return s.ExecuteToolDirect(ctx, toolCall)
}

// ExecuteToolDirect executes a tool directly without checking if it's enabled
// Used for user-initiated commands where the user explicitly wants to run the tool
func (s *LLMToolService) ExecuteToolDirect(ctx context.Context, toolCall sdk.ChatCompletionMessageToolCallFunction) (*agentdomain.ToolExecutionResult, error) {
	var args map[string]any
	if err := json.Unmarshal([]byte(toolCall.Arguments), &args); err != nil {
		return nil, fmt.Errorf("failed to parse tool arguments: %w", err)
	}

	tool, err := s.registry.GetTool(toolCall.Name)
	if err != nil {
		return nil, err
	}

	result, err := tool.Execute(ctx, args)

	if err == nil && result != nil && result.Success {
		switch toolCall.Name {
		case tools.ToolRead:
			s.registry.SetReadToolUsed()
			s.snapshotFile(args)
		case tools.ToolEdit, tools.ToolMultiEdit, tools.ToolWrite:
			s.snapshotFile(args)
		}
	}

	return result, err
}

// snapshotFile records the current modtime/size of the file named in args so the Edit/MultiEdit
// tools can detect later external modifications. Best-effort: missing/unstattable paths are skipped.
func (s *LLMToolService) snapshotFile(args map[string]any) {
	path, ok := args["file_path"].(string)
	if !ok || path == "" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	s.registry.RecordFileRead(path, info.ModTime(), info.Size())
}

// IsToolEnabled checks if a tool is enabled
func (s *LLMToolService) IsToolEnabled(name string) bool {
	return s.isToolEnabled(name)
}

// ValidateTool validates tool arguments
func (s *LLMToolService) ValidateTool(name string, args map[string]any) error {
	if !s.isToolEnabled(name) {
		return fmt.Errorf("tool %s is not enabled", name)
	}

	tool, err := s.registry.GetTool(name)
	if err != nil {
		return fmt.Errorf("tool '%s' is not available", name)
	}

	return tool.Validate(args)
}

func (s *LLMToolService) GetTool(name string) (agentdomain.Tool, error) {
	return s.registry.GetTool(name)
}

// NoOpToolService implements ToolService as a no-op (when tools are disabled)
type NoOpToolService struct{}

// NewNoOpToolService creates a new no-op tool service
func NewNoOpToolService() *NoOpToolService {
	return &NoOpToolService{}
}

func (s *NoOpToolService) Manifest(name string) agentdomain.ToolManifest {
	return agentdomain.ToolManifest{Name: name}
}

func (s *NoOpToolService) ListTools() []sdk.ChatCompletionTool {
	return []sdk.ChatCompletionTool{}
}

func (s *NoOpToolService) ListToolsForMode(mode agentdomain.AgentMode) []sdk.ChatCompletionTool {
	return []sdk.ChatCompletionTool{}
}

func (s *NoOpToolService) ListAvailableTools() []string {
	return []string{}
}

func (s *NoOpToolService) ListMarkdownSubagents() []agentdomain.SubagentInfo {
	return nil
}

func (s *NoOpToolService) ExecuteTool(ctx context.Context, toolCall sdk.ChatCompletionMessageToolCallFunction) (*agentdomain.ToolExecutionResult, error) {
	return nil, fmt.Errorf("tools are not enabled")
}

func (s *NoOpToolService) ExecuteToolDirect(ctx context.Context, toolCall sdk.ChatCompletionMessageToolCallFunction) (*agentdomain.ToolExecutionResult, error) {
	return nil, fmt.Errorf("tools are not enabled")
}

func (s *NoOpToolService) IsToolEnabled(name string) bool {
	return false
}

func (s *NoOpToolService) ValidateTool(name string, args map[string]any) error {
	return fmt.Errorf("tools are not enabled")
}

func (s *NoOpToolService) GetTool(name string) (agentdomain.Tool, error) {
	return nil, fmt.Errorf("tools are not enabled")
}
