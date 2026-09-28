package a2a

import (
	"context"
	"fmt"
	"strings"
	"time"

	adk "github.com/inference-gateway/adk/types"
	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	agentinfra "github.com/inference-gateway/cli/internal/agent/infrastructure"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	a2ainfra "github.com/inference-gateway/cli/internal/protocols/a2a/infrastructure"
)

type QueryAgentTool struct {
	config    *config.Config
	formatter agentinfra.CustomFormatter
}

type QueryAgentResult struct {
	AgentName string         `json:"agent_name"`
	Query     string         `json:"query"`
	Response  *adk.AgentCard `json:"response"`
	Success   bool           `json:"success"`
	Message   string         `json:"message"`
	Duration  time.Duration  `json:"duration"`
}

func NewQueryAgentTool(cfg *config.Config) *QueryAgentTool {
	return &QueryAgentTool{
		config: cfg,
		formatter: agentinfra.NewCustomFormatter(ToolQueryAgent, func(key string) bool {
			return key == "metadata"
		}),
	}
}

// Manifest returns the tool's manifest with its configured require_approval.
func (t *QueryAgentTool) Manifest() agentdomain.ToolManifest {
	return toolManifests.MustGet(ToolQueryAgent).WithRequireApproval(t.config.A2A.Tools.QueryAgent.RequireApproval)
}

func (t *QueryAgentTool) Definition() sdk.ChatCompletionTool {
	return t.Manifest().Definition()
}

func (t *QueryAgentTool) Execute(ctx context.Context, args map[string]any) (*agentdomain.ToolExecutionResult, error) {
	startTime := time.Now()

	if !t.IsEnabled() {
		return &agentdomain.ToolExecutionResult{
			ToolName:  ToolQueryAgent,
			Arguments: args,
			Success:   false,
			Duration:  time.Since(startTime),
			Error:     "A2A connections are disabled in configuration",
			Data: QueryAgentResult{
				Success: false,
				Message: "A2A connections are disabled",
			},
		}, nil
	}

	agentURL, ok := args["agent_url"].(string)
	if !ok {
		return t.errorResult(args, startTime, "agent_url parameter is required and must be a string")
	}

	adkClient := a2ainfra.NewClient(agentURL)
	response, err := adkClient.GetAgentCard(ctx)
	if err != nil {
		logger.Error("failed to fetch agent card", "agent_url", agentURL, "error", err)
		return t.errorResult(args, startTime, fmt.Sprintf("Failed to fetch agent card: %v", err))
	}

	return &agentdomain.ToolExecutionResult{
		ToolName:  ToolQueryAgent,
		Arguments: args,
		Success:   true,
		Duration:  time.Since(startTime),
		Data: QueryAgentResult{
			AgentName: agentURL,
			Query:     "card",
			Response:  response,
			Success:   true,
			Message:   fmt.Sprintf("QueryAgent sent to agent at %s successfully", agentURL),
			Duration:  time.Since(startTime),
		},
	}, nil
}

func (t *QueryAgentTool) errorResult(args map[string]any, startTime time.Time, errorMsg string) (*agentdomain.ToolExecutionResult, error) {
	return &agentdomain.ToolExecutionResult{
		ToolName:  ToolQueryAgent,
		Arguments: args,
		Success:   false,
		Duration:  time.Since(startTime),
		Error:     errorMsg,
		Data: QueryAgentResult{
			Success: false,
			Message: errorMsg,
		},
	}, nil
}

func (t *QueryAgentTool) Validate(args map[string]any) error {
	if _, ok := args["agent_url"].(string); !ok {
		return fmt.Errorf("agent_url parameter is required and must be a string")
	}
	return nil
}

func (t *QueryAgentTool) FormatResult(result *agentdomain.ToolExecutionResult, formatType agentdomain.FormatterType) string {
	switch formatType {
	case agentdomain.FormatterUI:
		return t.FormatForUI(result)
	case agentdomain.FormatterLLM:
		return t.FormatForLLM(result)
	case agentdomain.FormatterShort:
		return t.FormatPreview(result)
	default:
		return t.FormatForUI(result)
	}
}

func (t *QueryAgentTool) FormatForLLM(result *agentdomain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	var dataContent string
	if result.Data != nil {
		dataContent = t.formatter.FormatAsJSON(result.Data)
	}
	return t.formatter.FormatExpanded(result, dataContent)
}

func (t *QueryAgentTool) FormatForUI(result *agentdomain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	toolCall := t.formatter.FormatToolCall(result.Arguments, false)
	statusIcon := t.formatter.FormatStatusIcon(result.Success)
	preview := t.FormatPreview(result)

	var output strings.Builder
	fmt.Fprintf(&output, "%s\n", toolCall)
	fmt.Fprintf(&output, "└─ %s %s", statusIcon, preview)

	return output.String()
}

func (t *QueryAgentTool) FormatPreview(result *agentdomain.ToolExecutionResult) string {
	if result.Data == nil {
		return result.Error
	}

	if data, ok := result.Data.(QueryAgentResult); ok {
		return fmt.Sprintf("A2A QueryAgent: %s", data.Message)
	}

	return "A2A query agent operation completed"
}

func (t *QueryAgentTool) ShouldCollapseArg(key string) bool {
	return t.formatter.ShouldCollapseArg(key)
}

func (t *QueryAgentTool) ShouldAlwaysExpand() bool {
	return false
}

func (t *QueryAgentTool) IsEnabled() bool {
	return t.config.IsA2AToolsEnabled() && t.config.A2A.Tools.QueryAgent.Enabled
}

// SelfGated implements agentdomain.SelfGatedTool: a2a.enabled, not
// tools.enabled, switches the A2A tools on.
func (t *QueryAgentTool) SelfGated() bool { return true }
