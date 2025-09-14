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

// A2AQueryTool handles A2A agent queries
type A2AQueryTool struct {
	config    *config.Config
	formatter domain.CustomFormatter
}

// A2AQueryResult represents the result of an A2A query operation
type A2AQueryResult struct {
	AgentName string         `json:"agent_name"`
	Query     string         `json:"query"`
	Response  *adk.AgentCard `json:"response"`
	Success   bool           `json:"success"`
	Message   string         `json:"message"`
	Duration  time.Duration  `json:"duration"`
}

// NewA2AQueryTool creates a new A2A query tool
func NewA2AQueryTool(cfg *config.Config) *A2AQueryTool {
	return &A2AQueryTool{
		config: cfg,
		formatter: domain.NewCustomFormatter("Query", func(key string) bool {
			return key == "metadata"
		}),
	}
}

// Definition returns the tool definition for the LLM
func (t *A2AQueryTool) Definition() sdk.ChatCompletionTool {
	description := "Send a query to an Agent-to-Agent (A2A) server and get a response."
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "Query",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]interface{}{
					"agent_url": map[string]interface{}{
						"type":        "string",
						"description": "URL of the A2A agent to query",
					},
				},
				"required": []string{"agent_url"},
			},
		},
	}
}

// Execute runs the tool with given arguments
func (t *A2AQueryTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	startTime := time.Now()

	if !t.IsEnabled() {
		return &domain.ToolExecutionResult{
			ToolName:  "Query",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(startTime),
			Error:     "A2A connections are disabled in configuration",
			Data: A2AQueryResult{
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
		ToolName:  "Query",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(startTime),
		Data: A2AQueryResult{
			AgentName: agentURL,
			Query:     "card",
			Response:  response,
			Success:   true,
			Message:   fmt.Sprintf("Query sent to agent at %s successfully", agentURL),
			Duration:  time.Since(startTime),
		},
	}, nil
}

// errorResult creates an error result
func (t *A2AQueryTool) errorResult(args map[string]any, startTime time.Time, errorMsg string) (*domain.ToolExecutionResult, error) {
	return &domain.ToolExecutionResult{
		ToolName:  "Query",
		Arguments: args,
		Success:   false,
		Duration:  time.Since(startTime),
		Error:     errorMsg,
		Data: A2AQueryResult{
			Success: false,
			Message: errorMsg,
		},
	}, nil
}

// Validate checks if the tool arguments are valid
func (t *A2AQueryTool) Validate(args map[string]any) error {
	if _, ok := args["agent_url"].(string); !ok {
		return fmt.Errorf("agent_url parameter is required and must be a string")
	}
	return nil
}

// FormatResult formats tool execution results for different contexts
func (t *A2AQueryTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
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

// FormatForLLM formats the result for LLM consumption with detailed information
func (t *A2AQueryTool) FormatForLLM(result *domain.ToolExecutionResult) string {
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

// FormatForUI formats the result for UI display
func (t *A2AQueryTool) FormatForUI(result *domain.ToolExecutionResult) string {
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

// FormatPreview returns a short preview of the result for UI display
func (t *A2AQueryTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result.Data == nil {
		return result.Error
	}

	if data, ok := result.Data.(A2AQueryResult); ok {
		return fmt.Sprintf("A2A Query: %s", data.Message)
	}

	return "A2A query operation completed"
}

// ShouldCollapseArg determines if an argument should be collapsed in display
func (t *A2AQueryTool) ShouldCollapseArg(key string) bool {
	return t.formatter.ShouldCollapseArg(key)
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *A2AQueryTool) ShouldAlwaysExpand() bool {
	return false
}

// IsEnabled returns whether the query tool is enabled
func (t *A2AQueryTool) IsEnabled() bool {
	return t.config.Tools.Query.Enabled
}
