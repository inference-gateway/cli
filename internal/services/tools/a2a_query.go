package tools

import (
	"context"
	"fmt"
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
	config *config.Config
	_      domain.BaseFormatter
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
			Error:     "A2A direct connections are disabled in configuration",
			Data: A2AQueryResult{
				Success: false,
				Message: "A2A direct connections are disabled",
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

	logger.Debug("A2A query executed via tool", "agent_url", agentURL, "agent_name", response.Name)

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
	if result.Data == nil {
		return result.Error
	}

	data, ok := result.Data.(A2AQueryResult)
	if !ok {
		return "Invalid A2A query result format"
	}

	switch formatType {
	case domain.FormatterLLM:
		return t.formatForLLM(data)
	case domain.FormatterShort:
		return data.Message
	default:
		return t.formatForUI(data)
	}
}

// formatForLLM formats the result for LLM consumption
func (t *A2AQueryTool) formatForLLM(data A2AQueryResult) string {
	result := fmt.Sprintf("A2A Query to %s: %s", data.AgentName, data.Message)
	if data.Response == nil {
		return result
	}

	result += "\nAgent Card Details:"
	result += fmt.Sprintf("\n- Name: %s", data.Response.Name)
	result += fmt.Sprintf("\n- Version: %s", data.Response.Version)
	result += fmt.Sprintf("\n- Description: %s", data.Response.Description)
	result += fmt.Sprintf("\n- URL: %s", data.Response.URL)
	result += fmt.Sprintf("\n- Protocol Version: %s", data.Response.ProtocolVersion)
	result += fmt.Sprintf("\n- Preferred Transport: %s", data.Response.PreferredTransport)

	result += "\n- Capabilities:"
	if data.Response.Capabilities.Streaming != nil {
		result += fmt.Sprintf("\n  - Streaming: %t", *data.Response.Capabilities.Streaming)
	}
	if data.Response.Capabilities.PushNotifications != nil {
		result += fmt.Sprintf("\n  - Push Notifications: %t", *data.Response.Capabilities.PushNotifications)
	}
	if data.Response.Capabilities.StateTransitionHistory != nil {
		result += fmt.Sprintf("\n  - State Transition History: %t", *data.Response.Capabilities.StateTransitionHistory)
	}

	if len(data.Response.DefaultInputModes) > 0 {
		result += fmt.Sprintf("\n- Default Input Modes: %v", data.Response.DefaultInputModes)
	}
	if len(data.Response.DefaultOutputModes) > 0 {
		result += fmt.Sprintf("\n- Default Output Modes: %v", data.Response.DefaultOutputModes)
	}

	if len(data.Response.Skills) > 0 {
		result += "\n- Skills:"
		for _, skill := range data.Response.Skills {
			result += fmt.Sprintf("\n  - %s: %s", skill.Name, skill.Description)
		}
	}
	return result
}

// formatForUI formats the result for UI display
func (t *A2AQueryTool) formatForUI(data A2AQueryResult) string {
	result := fmt.Sprintf("**A2A Query**: %s", data.Message)

	if data.AgentName != "" {
		result += fmt.Sprintf("\n🤖 **Agent**: %s", data.AgentName)
	}

	if data.Query != "" {
		result += fmt.Sprintf("\n❓ **Query**: %s", data.Query)
	}

	if data.Response == nil {
		if data.Duration > 0 {
			result += fmt.Sprintf("\n⏱️ **Duration**: %v", data.Duration)
		}
		return result
	}

	result += "\n📋 **Agent Card**:"
	result += fmt.Sprintf("\n  - **Name**: %s", data.Response.Name)
	result += fmt.Sprintf("\n  - **Version**: %s", data.Response.Version)
	result += fmt.Sprintf("\n  - **Description**: %s", data.Response.Description)

	if data.Response.URL != "" {
		result += fmt.Sprintf("\n  - **URL**: %s", data.Response.URL)
	}
	if data.Response.ProtocolVersion != "" {
		result += fmt.Sprintf("\n  - **Protocol**: %s", data.Response.ProtocolVersion)
	}
	if data.Response.PreferredTransport != "" {
		result += fmt.Sprintf("\n  - **Transport**: %s", data.Response.PreferredTransport)
	}

	result += "\n  - **Capabilities**:"
	if data.Response.Capabilities.Streaming != nil {
		icon := "❌"
		if *data.Response.Capabilities.Streaming {
			icon = "✅"
		}
		result += fmt.Sprintf("\n    - **Streaming**: %s %t", icon, *data.Response.Capabilities.Streaming)
	}

	if data.Response.Capabilities.PushNotifications != nil {
		icon := "❌"
		if *data.Response.Capabilities.PushNotifications {
			icon = "✅"
		}
		result += fmt.Sprintf("\n    - **Push Notifications**: %s %t", icon, *data.Response.Capabilities.PushNotifications)
	}

	if data.Response.Capabilities.StateTransitionHistory != nil {
		icon := "❌"
		if *data.Response.Capabilities.StateTransitionHistory {
			icon = "✅"
		}
		result += fmt.Sprintf("\n    - **State History**: %s %t", icon, *data.Response.Capabilities.StateTransitionHistory)
	}

	if len(data.Response.DefaultInputModes) > 0 {
		result += fmt.Sprintf("\n  - **Input Modes**: %v", data.Response.DefaultInputModes)
	}
	if len(data.Response.DefaultOutputModes) > 0 {
		result += fmt.Sprintf("\n  - **Output Modes**: %v", data.Response.DefaultOutputModes)
	}

	if len(data.Response.Skills) > 0 {
		result += "\n  - **Skills**:"
		for _, skill := range data.Response.Skills {
			result += fmt.Sprintf("\n    - **%s**: %s", skill.Name, skill.Description)
		}
	}

	if data.Duration > 0 {
		result += fmt.Sprintf("\n⏱️ **Duration**: %v", data.Duration)
	}

	return result
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
	return false
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *A2AQueryTool) ShouldAlwaysExpand() bool {
	return false
}

// IsEnabled returns whether the query tool is enabled
func (t *A2AQueryTool) IsEnabled() bool {
	return t.config.Tools.Query.Enabled
}
