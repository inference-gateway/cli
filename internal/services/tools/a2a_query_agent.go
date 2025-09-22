package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	client "github.com/inference-gateway/adk/client"
	adk "github.com/inference-gateway/adk/types"
	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	sdk "github.com/inference-gateway/sdk"
)

type A2AQueryAgentTool struct {
	config    *config.Config
	formatter domain.CustomFormatter
}

type A2AQueryAgentResult struct {
	AgentName string         `json:"agent_name"`
	Query     string         `json:"query"`
	Response  *adk.AgentCard `json:"response"`
	Success   bool           `json:"success"`
	Message   string         `json:"message"`
	Duration  time.Duration  `json:"duration"`
}

func NewA2AQueryAgentTool(cfg *config.Config) *A2AQueryAgentTool {
	return &A2AQueryAgentTool{
		config: cfg,
		formatter: domain.NewCustomFormatter("QueryAgent", func(key string) bool {
			return key == "metadata"
		}),
	}
}

func (t *A2AQueryAgentTool) Definition() sdk.ChatCompletionTool {
	description := "Retrieve an A2A agent's metadata card showing its capabilities and configuration. Use ONLY for discovering what an agent can do. For asking questions or requesting work from an agent, use the Task tool instead."
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "QueryAgent",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]interface{}{
					"agent_url": map[string]interface{}{
						"type":        "string",
						"description": "URL of the A2A agent to retrieve metadata from",
					},
				},
				"required": []string{"agent_url"},
			},
		},
	}
}

func (t *A2AQueryAgentTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	startTime := time.Now()

	if !t.IsEnabled() {
		return &domain.ToolExecutionResult{
			ToolName:  "QueryAgent",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(startTime),
			Error:     "A2A connections are disabled in configuration",
			Data: A2AQueryAgentResult{
				Success: false,
				Message: "A2A connections are disabled",
			},
		}, nil
	}

	agentURL, ok := args["agent_url"].(string)
	if !ok {
		return t.errorResult(args, startTime, "agent_url parameter is required and must be a string")
	}

	adkClient := client.NewClient(agentURL)
	response, err := adkClient.GetAgentCard(ctx)
	if err != nil {
		logger.Error("Failed to fetch agent card", "agent_url", agentURL, "error", err)
		return t.errorResult(args, startTime, fmt.Sprintf("Failed to fetch agent card: %v", err))
	}

	return &domain.ToolExecutionResult{
		ToolName:  "QueryAgent",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(startTime),
		Data: A2AQueryAgentResult{
			AgentName: agentURL,
			Query:     "card",
			Response:  response,
			Success:   true,
			Message:   fmt.Sprintf("QueryAgent sent to agent at %s successfully", agentURL),
			Duration:  time.Since(startTime),
		},
	}, nil
}

func (t *A2AQueryAgentTool) errorResult(args map[string]any, startTime time.Time, errorMsg string) (*domain.ToolExecutionResult, error) {
	return &domain.ToolExecutionResult{
		ToolName:  "QueryAgent",
		Arguments: args,
		Success:   false,
		Duration:  time.Since(startTime),
		Error:     errorMsg,
		Data: A2AQueryAgentResult{
			Success: false,
			Message: errorMsg,
		},
	}, nil
}

func (t *A2AQueryAgentTool) Validate(args map[string]any) error {
	if _, ok := args["agent_url"].(string); !ok {
		return fmt.Errorf("agent_url parameter is required and must be a string")
	}
	return nil
}

func (t *A2AQueryAgentTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
	switch formatType {
	case domain.FormatterUI:
		return t.FormatForUI(result)
	case domain.FormatterLLM:
		return t.FormatForLLM(result)
	case domain.FormatterShort:
		return t.FormatPreview(result)
	default:
		return t.FormatForUI(result)
	}
}

func (t *A2AQueryAgentTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	var output strings.Builder

	output.WriteString(t.formatter.FormatExpandedHeader(result))

	if result.Data != nil {
		dataContent := t.formatter.FormatAsJSON(result.Data)
		hasMetadata := len(result.Metadata) > 0
		output.WriteString(t.formatter.FormatDataSection(dataContent, hasMetadata))
	}

	hasDataSection := result.Data != nil
	output.WriteString(t.formatter.FormatExpandedFooter(result, hasDataSection))

	return output.String()
}

func (t *A2AQueryAgentTool) FormatForUI(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	toolCall := t.formatter.FormatToolCall(result.Arguments, false)
	statusIcon := t.formatter.FormatStatusIcon(result.Success)
	preview := t.FormatPreview(result)

	var output strings.Builder
	output.WriteString(fmt.Sprintf("%s\n", toolCall))
	output.WriteString(fmt.Sprintf("└─ %s %s", statusIcon, preview))

	return output.String()
}

func (t *A2AQueryAgentTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result.Data == nil {
		return result.Error
	}

	if data, ok := result.Data.(A2AQueryAgentResult); ok {
		return fmt.Sprintf("A2A QueryAgent: %s", data.Message)
	}

	return "A2A query agent operation completed"
}

func (t *A2AQueryAgentTool) ShouldCollapseArg(key string) bool {
	return t.formatter.ShouldCollapseArg(key)
}

func (t *A2AQueryAgentTool) ShouldAlwaysExpand() bool {
	return false
}

func (t *A2AQueryAgentTool) IsEnabled() bool {
	return t.config.IsA2AToolsEnabled() || t.config.Tools.QueryAgent.Enabled
}
