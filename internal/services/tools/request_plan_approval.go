package tools

import (
	"context"
	"fmt"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
)

// RequestPlanApprovalTool handles requesting plan approval from the user
type RequestPlanApprovalTool struct {
	config    *config.Config
	enabled   bool
	formatter domain.BaseFormatter
}

// NewRequestPlanApprovalTool creates a new RequestPlanApproval tool
func NewRequestPlanApprovalTool(cfg *config.Config) *RequestPlanApprovalTool {
	return &RequestPlanApprovalTool{
		config:    cfg,
		enabled:   true, // Always enabled when in plan mode
		formatter: domain.NewBaseFormatter("RequestPlanApproval"),
	}
}

// Definition returns the tool definition for the LLM
func (t *RequestPlanApprovalTool) Definition() sdk.ChatCompletionTool {
	description := `Submit your completed plan for user approval.

What happens:
- Your plan will be displayed to the user
- User can approve or reject
- If approved, you'll switch to execution mode with full tool access
- If rejected, user will provide feedback

Include your complete plan in the 'plan' parameter.`

	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "RequestPlanApproval",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"$schema":              "http://json-schema.org/draft-07/schema#",
				"additionalProperties": false,
				"type":                 "object",
				"required":             []string{"plan"},
				"properties": map[string]any{
					"plan": map[string]any{
						"type":        "string",
						"description": "The complete, detailed plan to be executed",
					},
				},
			},
		},
	}
}

// Execute runs the RequestPlanApproval tool with given arguments
func (t *RequestPlanApprovalTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	start := time.Now()

	plan, ok := args["plan"].(string)
	if !ok || plan == "" {
		return &domain.ToolExecutionResult{
			ToolName:  "RequestPlanApproval",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "plan parameter is required and must be a non-empty string",
		}, nil
	}

	result := &domain.ToolExecutionResult{
		ToolName:  "RequestPlanApproval",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data: map[string]any{
			"plan":    plan,
			"message": "Plan approval requested - waiting for user decision",
		},
	}

	return result, nil
}

// Validate checks if the RequestPlanApproval tool arguments are valid
func (t *RequestPlanApprovalTool) Validate(args map[string]any) error {
	plan, ok := args["plan"].(string)
	if !ok {
		return fmt.Errorf("plan parameter is required and must be a string")
	}

	if plan == "" {
		return fmt.Errorf("plan cannot be empty")
	}

	return nil
}

// IsEnabled returns whether the RequestPlanApproval tool is enabled
func (t *RequestPlanApprovalTool) IsEnabled() bool {
	return t.enabled
}

// FormatResult formats tool execution results for different contexts
func (t *RequestPlanApprovalTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
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
func (t *RequestPlanApprovalTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	if result.Success {
		return "Plan approval requested"
	}

	return "Plan approval request failed"
}

// FormatForUI formats the result for UI display
func (t *RequestPlanApprovalTool) FormatForUI(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	statusIcon := t.formatter.FormatStatusIcon(result.Success)
	return fmt.Sprintf("RequestPlanApproval(...)\n└─ %s Plan submitted for approval", statusIcon)
}

// FormatForLLM formats the result for LLM consumption
func (t *RequestPlanApprovalTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	if result.Success {
		return "Plan approval requested. The user will review your plan and decide whether to accept, reject, or enable auto-approve mode."
	}

	return fmt.Sprintf("Failed to request plan approval: %s", result.Error)
}

// ShouldCollapseArg determines if an argument should be collapsed in display
func (t *RequestPlanApprovalTool) ShouldCollapseArg(key string) bool {
	return key == "plan"
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *RequestPlanApprovalTool) ShouldAlwaysExpand() bool {
	return false
}
