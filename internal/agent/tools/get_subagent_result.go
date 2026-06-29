package tools

import (
	"context"
	"fmt"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
)

// GetSubagentResultTool returns the latest output of an interactive subagent by
// capturing a bounded tail of its tmux pane (the last message or few messages,
// never the whole scrollback). It is the subagent analogue of BashOutput.
type GetSubagentResultTool struct {
	config      *config.Config
	tracker     domain.SubagentTracker
	paneState   func(ctx context.Context, paneID string) paneState
	capturePane func(ctx context.Context, paneID string, maxLines int) string
}

// NewGetSubagentResultTool creates a new GetSubagentResult tool over the
// session's SubagentTracker.
func NewGetSubagentResultTool(cfg *config.Config, tracker domain.SubagentTracker) *GetSubagentResultTool {
	return &GetSubagentResultTool{
		config:      cfg,
		tracker:     tracker,
		paneState:   tmuxPaneState,
		capturePane: tmuxCapturePaneTail,
	}
}

// Definition returns the tool definition for the LLM.
func (t *GetSubagentResultTool) Definition() sdk.ChatCompletionTool {
	description := t.config.Prompts.Tools.GetSubagentResult.Description
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "GetSubagentResult",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"subagent_id": map[string]any{
						"type":        "string",
						"description": "The subagent id from ListSubagents",
					},
					"lines": map[string]any{
						"type":        "integer",
						"description": fmt.Sprintf("Optional number of trailing lines to return (default %d, max %d)", defaultPaneTailLines, maxPaneTailLines),
					},
				},
				"required":             []string{"subagent_id"},
				"additionalProperties": false,
			},
		},
	}
}

// Execute returns the latest output of the named subagent.
func (t *GetSubagentResultTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	if err := t.Validate(args); err != nil {
		return nil, err
	}

	subagentID, _ := args["subagent_id"].(string)
	s := t.tracker.GetSubagent(subagentID)
	if s == nil {
		return &domain.ToolExecutionResult{
			ToolName:  "GetSubagentResult",
			Arguments: args,
			Success:   false,
			Error:     fmt.Sprintf("Subagent not found: %s. It may have completed (headless subagents are removed once their result is delivered) or already been closed.", subagentID),
		}, nil
	}

	if s.Mode != domain.SubagentModeInteractive {
		return &domain.ToolExecutionResult{
			ToolName:  "GetSubagentResult",
			Arguments: args,
			Success:   true,
			Data: map[string]any{
				"subagent_id": s.ID,
				"label":       s.Label,
				"mode":        s.Mode,
				"status":      string(s.Status),
				"message":     "Headless subagents have no live output to fetch; their result is delivered automatically when they complete.",
			},
		}, nil
	}

	output := t.capturePane(ctx, s.PaneID, toInt(args["lines"]))
	return &domain.ToolExecutionResult{
		ToolName:  "GetSubagentResult",
		Arguments: args,
		Success:   true,
		Data: map[string]any{
			"subagent_id": s.ID,
			"label":       s.Label,
			"mode":        s.Mode,
			"pane_id":     s.PaneID,
			"status":      t.paneState(ctx, s.PaneID).status(),
			"output":      output,
		},
	}, nil
}

// Validate checks the tool arguments.
func (t *GetSubagentResultTool) Validate(args map[string]any) error {
	if id, ok := args["subagent_id"].(string); !ok || id == "" {
		return fmt.Errorf("subagent_id is required and must be a non-empty string")
	}
	return nil
}

// IsEnabled reports whether the tool is enabled.
func (t *GetSubagentResultTool) IsEnabled() bool {
	return t.config.IsAgentToolEnabled() && t.tracker != nil
}

// FormatResult formats tool execution results for different contexts.
func (t *GetSubagentResultTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
	switch formatType {
	case domain.FormatterShort:
		return t.FormatPreview(result)
	default:
		return t.FormatForLLM(result)
	}
}

// FormatPreview returns a short preview of the result for UI display.
func (t *GetSubagentResultTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return "Failed to get subagent result"
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		return "Retrieved subagent output"
	}
	id, _ := data["subagent_id"].(string)
	status, _ := data["status"].(string)
	if status == "" {
		return fmt.Sprintf("Subagent %s", id)
	}
	return fmt.Sprintf("Subagent %s %s", id, status)
}

// FormatForLLM formats the result for LLM consumption.
func (t *GetSubagentResultTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return fmt.Sprintf("Error: %s", result.Error)
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		return "Retrieved subagent output"
	}
	if msg, _ := data["message"].(string); msg != "" {
		return msg
	}
	status, _ := data["status"].(string)
	output, _ := data["output"].(string)
	if output == "" {
		output = "(no output captured)"
	}
	return fmt.Sprintf("Subagent status: %s\n\nLatest output:\n%s", status, output)
}

// ShouldCollapseArg returns whether an argument should be collapsed.
func (t *GetSubagentResultTool) ShouldCollapseArg(key string) bool {
	return false
}

// ShouldAlwaysExpand returns whether results should always be expanded.
func (t *GetSubagentResultTool) ShouldAlwaysExpand() bool {
	return false
}
