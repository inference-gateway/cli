package domain

import (
	"context"

	"github.com/inference-gateway/sdk"
)

// ToolService handles tool execution
type ToolService interface {
	ListTools() []sdk.ChatCompletionTool
	ListToolsForMode(mode AgentMode) []sdk.ChatCompletionTool
	ListAvailableTools() []string
	ExecuteTool(ctx context.Context, tool sdk.ChatCompletionMessageToolCallFunction) (*ToolExecutionResult, error)
	ExecuteToolDirect(ctx context.Context, tool sdk.ChatCompletionMessageToolCallFunction) (*ToolExecutionResult, error)
	IsToolEnabled(name string) bool
	ValidateTool(name string, args map[string]any) error
	GetTaskTracker() TaskTracker
}
