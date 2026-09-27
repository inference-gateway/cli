package domain

import (
	"context"

	sdk "github.com/inference-gateway/sdk"
)

// ReadOnlyTools never change state: they make up the ReadOnly subagent
// toolset and may run concurrently within a batch. Every other tool runs in
// batch order.
var ReadOnlyTools = map[string]bool{
	"Read":               true,
	"Grep":               true,
	"Tree":               true,
	"WebFetch":           true,
	"WebSearch":          true,
	"ListSubagents":      true,
	"GetSubagentResult":  true,
	"ReadSubagentScreen": true,
}

// SubagentInfo describes one Markdown-defined subagent preset (.infer/agents/*.md)
// for user-facing listings like /agents. An empty Model means inherit and an
// empty Tools list means the parent's tools are inherited.
type SubagentInfo struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Model       string   `json:"model,omitempty"`
	Tools       []string `json:"tools,omitempty"`
	ReadOnly    bool     `json:"read_only"`
	Source      string   `json:"source"`
}

// ToolService handles tool execution
type ToolService interface {
	ListTools() []sdk.ChatCompletionTool
	ListToolsForMode(mode AgentMode) []sdk.ChatCompletionTool
	ListAvailableTools() []string
	ListMarkdownSubagents() []SubagentInfo
	ExecuteTool(ctx context.Context, tool sdk.ChatCompletionMessageToolCallFunction) (*ToolExecutionResult, error)
	ExecuteToolDirect(ctx context.Context, tool sdk.ChatCompletionMessageToolCallFunction) (*ToolExecutionResult, error)
	IsToolEnabled(name string) bool
	ValidateTool(name string, args map[string]any) error
	GetTool(name string) (Tool, error)
}
