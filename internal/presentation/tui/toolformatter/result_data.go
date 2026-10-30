package toolformatter

import (
	"encoding/json"
	"strings"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	computer "github.com/inference-gateway/cli/internal/computer"
	computerdomain "github.com/inference-gateway/cli/internal/computer/domain"
	mcpdomain "github.com/inference-gateway/cli/internal/protocols/mcp/domain"
	tools "github.com/inference-gateway/cli/internal/tools"
)

// typedResultData builds, per tool, the concrete Data its formatter asserts.
var typedResultData = map[string]func() any{
	tools.ToolBash:        func() any { return &agentdomain.BashToolResult{} },
	tools.ToolRead:        func() any { return &agentdomain.FileReadToolResult{} },
	tools.ToolWrite:       func() any { return &agentdomain.FileWriteToolResult{} },
	tools.ToolEdit:        func() any { return &agentdomain.EditToolResult{} },
	tools.ToolMultiEdit:   func() any { return &agentdomain.MultiEditToolResult{} },
	tools.ToolTree:        func() any { return &agentdomain.TreeToolResult{} },
	tools.ToolDelete:      func() any { return &agentdomain.DeleteToolResult{} },
	tools.ToolWebFetch:    func() any { return &agentdomain.FetchResult{} },
	tools.ToolWebSearch:   func() any { return &agentdomain.WebSearchResponse{} },
	tools.ToolTodoWrite:   func() any { return &agentdomain.TodoWriteToolResult{} },
	tools.ToolSkill:       func() any { return &agentdomain.SkillToolResult{} },
	computer.ToolComputer: func() any { return &computerdomain.Observation{} },
}

// typed returns result with its Data back in the concrete type the tool's
// formatter asserts. A result decoded from JSON, from storage or a daemon
// thread, carries a generic map there, which would degrade its card. Any
// other result is returned as it is.
func typed(result *agentdomain.ToolExecutionResult) *agentdomain.ToolExecutionResult {
	if result == nil {
		return nil
	}
	if _, generic := result.Data.(map[string]any); !generic {
		return result
	}
	build := typedResultData[result.ToolName]
	if build == nil && strings.HasPrefix(result.ToolName, mcpdomain.ToolNamePrefix) {
		build = func() any { return &mcpdomain.ToolResult{} }
	}
	if build == nil {
		return result
	}
	raw, err := json.Marshal(result.Data)
	if err != nil {
		return result
	}
	data := build()
	if json.Unmarshal(raw, data) != nil {
		return result
	}
	rehydrated := *result
	rehydrated.Data = data
	return &rehydrated
}
