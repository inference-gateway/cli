package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
	mcp "github.com/metoro-io/mcp-golang"
)

// MCPTool wraps an MCP server tool to implement the domain.Tool interface
type MCPTool struct {
	serverName    string
	toolName      string
	description   string
	inputSchema   any
	clientManager domain.MCPClient
	config        *config.MCPConfig
	formatter     domain.BaseFormatter
}

// NewMCPTool creates a new MCP tool wrapper
func NewMCPTool(
	serverName, toolName, description string,
	inputSchema any,
	clientManager domain.MCPClient,
	mcpConfig *config.MCPConfig,
) *MCPTool {
	// Tool name format: MCP_<servername>_<toolname>
	fullToolName := fmt.Sprintf("MCP_%s_%s", serverName, toolName)

	return &MCPTool{
		serverName:    serverName,
		toolName:      toolName,
		description:   description,
		inputSchema:   inputSchema,
		clientManager: clientManager,
		config:        mcpConfig,
		formatter:     domain.NewBaseFormatter(fullToolName),
	}
}

// Definition returns the tool definition for the LLM
func (t *MCPTool) Definition() sdk.ChatCompletionTool {
	// Format: MCP_<servername>_<toolname>
	fullToolName := fmt.Sprintf("MCP_%s_%s", t.serverName, t.toolName)

	// Enhance description with server context
	enhancedDescription := fmt.Sprintf("[MCP:%s] %s", t.serverName, t.description)

	// Convert input schema to FunctionParameters
	var parameters *sdk.FunctionParameters
	if t.inputSchema != nil {
		if schemaMap, ok := t.inputSchema.(map[string]any); ok {
			params := sdk.FunctionParameters(schemaMap)
			parameters = &params
		}
	}

	// Fallback to basic schema if conversion fails
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
func (t *MCPTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	start := time.Now()
	fullToolName := fmt.Sprintf("MCP_%s_%s", t.serverName, t.toolName)

	response, err := t.clientManager.CallTool(ctx, t.serverName, t.toolName, args)

	duration := time.Since(start)
	success := err == nil

	var content string
	var errorMsg string

	if err != nil {
		errorMsg = err.Error()
	} else if response != nil {
		if mcpResp, ok := response.(*mcp.ToolResponse); ok {
			content = t.formatMCPContent(mcpResp)
		} else {
			errorMsg = "unexpected response type from MCP server"
		}
	}

	toolData := &domain.MCPToolResult{
		ServerName: t.serverName,
		ToolName:   t.toolName,
		Content:    content,
		Error:      errorMsg,
	}

	result := &domain.ToolExecutionResult{
		ToolName:  fullToolName,
		Arguments: args,
		Success:   success,
		Duration:  duration,
		Data:      toolData,
	}

	if err != nil {
		result.Error = fmt.Sprintf("MCP tool execution failed: %v", err)
	}

	return result, nil
}

// formatMCPContent formats the MCP response content for display
func (t *MCPTool) formatMCPContent(response *mcp.ToolResponse) string {
	if response == nil {
		return ""
	}

	var contentParts []string
	for _, content := range response.Content {
		if content == nil {
			continue
		}

		// Handle TextContent
		if content.TextContent != nil && content.TextContent.Text != "" {
			contentParts = append(contentParts, content.TextContent.Text)
		}

		// Handle ImageContent
		if content.ImageContent != nil {
			contentParts = append(contentParts, "[Image content]")
		}

		// Handle EmbeddedResource
		if content.EmbeddedResource != nil {
			if content.EmbeddedResource.TextResourceContents != nil && content.EmbeddedResource.TextResourceContents.Text != "" {
				contentParts = append(contentParts, content.EmbeddedResource.TextResourceContents.Text)
			} else if content.EmbeddedResource.BlobResourceContents != nil {
				contentParts = append(contentParts, "[Binary resource content]")
			}
		}
	}

	return strings.Join(contentParts, "\n")
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

	schemaMap, ok := t.inputSchema.(map[string]any)
	if !ok {
		return nil
	}

	if err := t.validateRequiredFields(schemaMap, args); err != nil {
		return err
	}

	return t.validatePropertyTypes(schemaMap, args)
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
func (t *MCPTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
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

// FormatPreview returns a short preview of the result for UI display
func (t *MCPTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "MCP tool execution result unavailable"
	}

	mcpResult, ok := result.Data.(*domain.MCPToolResult)
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
func (t *MCPTool) FormatForUI(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "MCP tool execution result unavailable"
	}

	toolCall := t.formatter.FormatToolCall(result.Arguments, false)
	statusIcon := t.formatter.FormatStatusIcon(result.Success)
	preview := t.FormatPreview(result)

	var output strings.Builder
	output.WriteString(fmt.Sprintf("%s\n", toolCall))

	previewLines := strings.Split(preview, "\n")
	for i, line := range previewLines {
		if i == 0 {
			output.WriteString(fmt.Sprintf("└─ %s %s", statusIcon, line))
		} else {
			output.WriteString(fmt.Sprintf("\n     %s", line))
		}
	}

	return output.String()
}

// FormatForLLM formats the result for LLM consumption with detailed information
func (t *MCPTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "MCP tool execution result unavailable"
	}

	var output strings.Builder

	output.WriteString(t.formatter.FormatExpandedHeader(result))

	if result.Data != nil {
		dataContent := t.formatMCPData(result.Data)
		hasMetadata := len(result.Metadata) > 0
		output.WriteString(t.formatter.FormatDataSection(dataContent, hasMetadata))
	}

	hasDataSection := result.Data != nil
	output.WriteString(t.formatter.FormatExpandedFooter(result, hasDataSection))

	return output.String()
}

// formatMCPData formats MCP-specific data for display
func (t *MCPTool) formatMCPData(data any) string {
	mcpResult, ok := data.(*domain.MCPToolResult)
	if !ok {
		return "Invalid MCP data format"
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("Server: %s\n", mcpResult.ServerName))
	output.WriteString(fmt.Sprintf("Tool: %s\n", mcpResult.ToolName))

	if mcpResult.Error != "" {
		output.WriteString("Status: Error\n")
		output.WriteString(fmt.Sprintf("Error: %s\n", mcpResult.Error))
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
	// Collapse large content fields
	return key == "content" || key == "data" || key == "text"
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *MCPTool) ShouldAlwaysExpand() bool {
	return false
}
