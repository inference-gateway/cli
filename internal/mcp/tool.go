package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	agentinfra "github.com/inference-gateway/cli/internal/agent/infrastructure"
	mcpdomain "github.com/inference-gateway/cli/internal/mcp/domain"
)

var _ agentdomain.Tool = (*MCPTool)(nil)

// MCPTool is the agent's view of one tool on an MCP server: it presents the
// server's tool to the LLM under its namespaced name and calls it through the
// server's Client.
type MCPTool struct {
	serverName  string
	toolName    string
	description string
	inputSchema map[string]any
	client      mcpdomain.Client
	config      *config.MCPConfig
	formatter   agentinfra.BaseFormatter
}

// NewMCPTool creates a new MCP tool wrapper
func NewMCPTool(
	serverName, toolName, description string,
	inputSchema map[string]any,
	client mcpdomain.Client,
	mcpConfig *config.MCPConfig,
) *MCPTool {
	return &MCPTool{
		serverName:  serverName,
		toolName:    toolName,
		description: description,
		inputSchema: inputSchema,
		client:      client,
		config:      mcpConfig,
		formatter:   agentinfra.NewBaseFormatter(mcpdomain.ToolName(serverName, toolName)),
	}
}

// newTools wraps a server's discovered tools as agent tools keyed by their
// namespaced name, the set the registry takes (like browser.NewTools).
func newTools(found []mcpdomain.Tool, client mcpdomain.Client, cfg *config.MCPConfig) map[string]agentdomain.Tool {
	tools := make(map[string]agentdomain.Tool, len(found))
	for _, tool := range found {
		tools[mcpdomain.ToolName(tool.Server, tool.Name)] = NewMCPTool(tool.Server, tool.Name, tool.Description, tool.InputSchema, client, cfg)
	}
	return tools
}

// Definition returns the tool definition for the LLM
func (t *MCPTool) Definition() sdk.ChatCompletionTool {
	fullToolName := mcpdomain.ToolName(t.serverName, t.toolName)

	// Enhance description with server context
	enhancedDescription := fmt.Sprintf("[MCP:%s] %s", t.serverName, t.description)

	var parameters *sdk.FunctionParameters
	if t.inputSchema != nil {
		params := sdk.FunctionParameters(t.inputSchema)
		parameters = &params
	}

	// Fallback to basic schema when the server sent none
	if parameters == nil {
		defaultParams := sdk.FunctionParameters{
			"type":       "object",
			"properties": map[string]any{},
		}
		parameters = &defaultParams
	}

	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        fullToolName,
			Description: &enhancedDescription,
			Parameters:  parameters,
		},
	}
}

// Execute runs the MCP tool with given arguments
func (t *MCPTool) Execute(ctx context.Context, args map[string]any) (*agentdomain.ToolExecutionResult, error) {
	start := time.Now()

	response, err := t.client.CallTool(ctx, t.toolName, args)
	if err == nil && response.IsError {
		err = fmt.Errorf("tool reported an error: %s", response.Content)
	}

	toolData := &mcpdomain.ToolResult{
		ServerName: t.serverName,
		ToolName:   t.toolName,
	}
	result := &agentdomain.ToolExecutionResult{
		ToolName:  mcpdomain.ToolName(t.serverName, t.toolName),
		Arguments: args,
		Success:   err == nil,
		Duration:  time.Since(start),
		Data:      toolData,
	}

	if err != nil {
		toolData.Error = err.Error()
		result.Error = fmt.Sprintf("MCP tool execution failed: %v", err)
	} else {
		toolData.Content = response.Content
	}

	return result, nil
}

// Validate checks if the tool arguments are valid
func (t *MCPTool) Validate(args map[string]any) error {
	// Basic validation - check if args is not nil
	if args == nil {
		return fmt.Errorf("arguments cannot be nil")
	}

	if t.inputSchema == nil {
		return nil
	}

	if err := t.validateRequiredFields(t.inputSchema, args); err != nil {
		return err
	}

	return t.validatePropertyTypes(t.inputSchema, args)
}

// validateRequiredFields checks that all required fields are present
func (t *MCPTool) validateRequiredFields(schema map[string]any, args map[string]any) error {
	requiredFields, ok := schema["required"].([]any)
	if !ok {
		return nil
	}

	for _, field := range requiredFields {
		fieldName, ok := field.(string)
		if !ok {
			continue
		}
		if _, exists := args[fieldName]; !exists {
			return fmt.Errorf("required field %q is missing", fieldName)
		}
	}
	return nil
}

// validatePropertyTypes validates the types of provided arguments
func (t *MCPTool) validatePropertyTypes(schema map[string]any, args map[string]any) error {
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		return nil
	}

	for key, value := range args {
		propSchema, exists := properties[key]
		if !exists {
			continue // Allow extra fields
		}

		propMap, ok := propSchema.(map[string]any)
		if !ok {
			continue
		}

		expectedType, ok := propMap["type"].(string)
		if !ok {
			continue
		}

		actualType := t.getJSONType(value)
		isValidType := actualType == expectedType || (expectedType == "integer" && actualType == "number")
		if !isValidType {
			return fmt.Errorf("field %q has invalid type: expected %s, got %s", key, expectedType, actualType)
		}
	}
	return nil
}

// getJSONType returns the JSON type name for a Go value
func (t *MCPTool) getJSONType(value any) string {
	switch value.(type) {
	case string:
		return "string"
	case bool:
		return "boolean"
	case float64, int, int32, int64:
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	case nil:
		return "null"
	default:
		return "unknown"
	}
}

// IsEnabled returns whether this MCP tool is enabled
func (t *MCPTool) IsEnabled() bool {
	if t.config == nil || !t.config.Enabled {
		return false
	}

	// Check if the specific server is enabled
	for _, server := range t.config.Servers {
		if server.Name == t.serverName {
			if !server.Enabled {
				return false
			}

			// Check tool filtering
			return server.ShouldIncludeTool(t.toolName)
		}
	}

	return false
}

// FormatResult formats tool execution results for different contexts
func (t *MCPTool) FormatResult(result *agentdomain.ToolExecutionResult, formatType agentdomain.FormatterType) string {
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

// FormatPreview returns a short preview of the result for UI display
func (t *MCPTool) FormatPreview(result *agentdomain.ToolExecutionResult) string {
	if result == nil {
		return "MCP tool execution result unavailable"
	}

	mcpResult, ok := result.Data.(*mcpdomain.ToolResult)
	if !ok {
		if result.Success {
			return "MCP tool executed successfully"
		}
		return "MCP tool execution failed"
	}

	if mcpResult.Error != "" {
		return fmt.Sprintf("MCP Error from %s: %s", mcpResult.ServerName, mcpResult.Error)
	}

	if mcpResult.Content != "" {
		content := strings.TrimSpace(mcpResult.Content)
		lines := strings.Split(content, "\n")

		if len(lines) <= 3 {
			return content
		}

		preview := strings.Join(lines[:3], "\n")
		return preview + "\n..."
	}

	return "MCP tool completed"
}

// FormatForUI formats the result for UI display
func (t *MCPTool) FormatForUI(result *agentdomain.ToolExecutionResult) string {
	if result == nil {
		return "MCP tool execution result unavailable"
	}

	toolCall := t.formatter.FormatToolCall(result.Arguments, false)
	statusIcon := t.formatter.FormatStatusIcon(result.Success)
	preview := t.FormatPreview(result)

	var output strings.Builder
	fmt.Fprintf(&output, "%s\n", toolCall)

	previewLines := strings.Split(preview, "\n")
	for i, line := range previewLines {
		if i == 0 {
			fmt.Fprintf(&output, "└─ %s %s", statusIcon, line)
		} else {
			fmt.Fprintf(&output, "\n     %s", line)
		}
	}

	return output.String()
}

// FormatForLLM formats the result for LLM consumption with detailed information
func (t *MCPTool) FormatForLLM(result *agentdomain.ToolExecutionResult) string {
	if result == nil {
		return "MCP tool execution result unavailable"
	}

	var dataContent string
	if result.Data != nil {
		dataContent = t.formatMCPData(result.Data)
	}
	return t.formatter.FormatExpanded(result, dataContent)
}

// formatMCPData formats MCP-specific data for display
func (t *MCPTool) formatMCPData(data any) string {
	mcpResult, ok := data.(*mcpdomain.ToolResult)
	if !ok {
		return "Invalid MCP data format"
	}

	var output strings.Builder
	fmt.Fprintf(&output, "Server: %s\n", mcpResult.ServerName)
	fmt.Fprintf(&output, "Tool: %s\n", mcpResult.ToolName)

	if mcpResult.Error != "" {
		output.WriteString("Status: Error\n")
		fmt.Fprintf(&output, "Error: %s\n", mcpResult.Error)
	} else {
		output.WriteString("Status: Success\n")
	}

	if mcpResult.Content != "" {
		output.WriteString("\nContent:\n")
		output.WriteString(mcpResult.Content)
	}

	return output.String()
}

// ShouldCollapseArg determines if an argument should be collapsed in display
func (t *MCPTool) ShouldCollapseArg(key string) bool {
	return key == "content" || key == "data" || key == "text"
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *MCPTool) ShouldAlwaysExpand() bool {
	return false
}
