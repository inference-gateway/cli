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

// ToolService handles tool execution
type ToolService interface {
	ListTools() []sdk.ChatCompletionTool
	ListToolsForMode(mode AgentMode) []sdk.ChatCompletionTool
	ListAvailableTools() []string
	ExecuteTool(ctx context.Context, tool sdk.ChatCompletionMessageToolCallFunction) (*ToolExecutionResult, error)
	ExecuteToolDirect(ctx context.Context, tool sdk.ChatCompletionMessageToolCallFunction) (*ToolExecutionResult, error)
	IsToolEnabled(name string) bool
	ValidateTool(name string, args map[string]any) error
	GetTool(name string) (Tool, error)
}
