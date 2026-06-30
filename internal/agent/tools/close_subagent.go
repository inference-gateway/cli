package tools

import (
	"context"
	"fmt"
	"os"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
)

// CloseSubagentTool closes a subagent. For an interactive subagent it harvests
// its last assistant message (result file) and kills the tmux pane; for a
// headless one it cancels the running subprocess. The subagent analogue of KillShell.
type CloseSubagentTool struct {
	config   *config.Config
	tracker  domain.SubagentTracker
	stopJob  domain.JobStopper
	killPane func(ctx context.Context, paneID string) error
}

// NewCloseSubagentTool creates a new CloseSubagent tool over the session's
// SubagentTracker. stopJob ends the supervised monitor of a closed interactive
// subagent (may be nil, e.g. when the supervisor is unavailable, in which case
// the tool falls back to killing the pane directly).
func NewCloseSubagentTool(cfg *config.Config, tracker domain.SubagentTracker, stopJob domain.JobStopper) *CloseSubagentTool {
	return &CloseSubagentTool{
		config:   cfg,
		tracker:  tracker,
		stopJob:  stopJob,
		killPane: tmuxKillPane,
	}
}

// Definition returns the tool definition for the LLM.
func (t *CloseSubagentTool) Definition() sdk.ChatCompletionTool {
	description := t.config.Prompts.Tools.CloseSubagent.Description
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "CloseSubagent",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"subagent_id": map[string]any{
						"type":        "string",
						"description": "The subagent id from ListSubagents",
					},
				},
				"required":             []string{"subagent_id"},
				"additionalProperties": false,
			},
		},
	}
}

// Execute closes the named subagent.
func (t *CloseSubagentTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	if err := t.Validate(args); err != nil {
		return nil, err
	}

	subagentID, _ := args["subagent_id"].(string)
	s := t.tracker.GetSubagent(subagentID)
	if s == nil {
		return &domain.ToolExecutionResult{
			ToolName:  "CloseSubagent",
			Arguments: args,
			Success:   false,
			Error:     fmt.Sprintf("Subagent not found: %s (it may have already been closed or completed).", subagentID),
		}, nil
	}

	if s.Mode == domain.SubagentModeInteractive {
		return t.closeInteractive(ctx, args, s), nil
	}
	return t.closeHeadless(args, s), nil
}

// closeInteractive harvests a final tail of the pane, kills it, and untracks the
// subagent. The harvested output is returned so the subagent's last work folds
// back into the conversation.
func (t *CloseSubagentTool) closeInteractive(ctx context.Context, args map[string]any, s *domain.SubagentState) *domain.ToolExecutionResult {
	output := readSubagentResultMessage(s.SessionID)
	if t.stopJob != nil {
		_ = t.stopJob.WindJob(s.ID, domain.WindStop)
	} else {
		_ = t.killPane(ctx, s.PaneID)
	}
	_ = os.Remove(subagentResultFilePath(s.SessionID))
	_ = t.tracker.RemoveSubagent(s.ID)
	return &domain.ToolExecutionResult{
		ToolName:  "CloseSubagent",
		Arguments: args,
		Success:   true,
		Data: map[string]any{
			"subagent_id":  s.ID,
			"label":        s.Label,
			"pane_id":      s.PaneID,
			"message":      fmt.Sprintf("Closed interactive subagent %s (pane %s).", labelOrSession(s.Label, s.SessionID), s.PaneID),
			"final_output": output,
		},
	}
}

// closeHeadless cancels a running headless subagent. The supervised
// headlessSubagentJob delivers the cancellation outcome and removes it from
// tracking on reap, so this does not remove it directly.
func (t *CloseSubagentTool) closeHeadless(args map[string]any, s *domain.SubagentState) *domain.ToolExecutionResult {
	if s.CancelFunc != nil {
		s.CancelFunc()
	}
	return &domain.ToolExecutionResult{
		ToolName:  "CloseSubagent",
		Arguments: args,
		Success:   true,
		Data: map[string]any{
			"subagent_id": s.ID,
			"label":       s.Label,
			"message":     fmt.Sprintf("Requested cancellation of headless subagent %s.", labelOrSession(s.Label, s.SessionID)),
		},
	}
}

// Validate checks the tool arguments.
func (t *CloseSubagentTool) Validate(args map[string]any) error {
	if id, ok := args["subagent_id"].(string); !ok || id == "" {
		return fmt.Errorf("subagent_id is required and must be a non-empty string")
	}
	return nil
}

// IsEnabled reports whether the tool is enabled.
func (t *CloseSubagentTool) IsEnabled() bool {
	return t.config.IsAgentToolEnabled() && t.tracker != nil
}

// FormatResult formats tool execution results for different contexts.
func (t *CloseSubagentTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
	switch formatType {
	case domain.FormatterShort:
		return t.FormatPreview(result)
	default:
		return t.FormatForLLM(result)
	}
}

// FormatPreview returns a short preview of the result for UI display.
func (t *CloseSubagentTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return "Failed to close subagent"
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		return "Subagent closed"
	}
	id, _ := data["subagent_id"].(string)
	return fmt.Sprintf("Closed subagent %s", id)
}

// FormatForLLM formats the result for LLM consumption.
func (t *CloseSubagentTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return fmt.Sprintf("Error: %s", result.Error)
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		return "Subagent closed"
	}
	msg, _ := data["message"].(string)
	if output, _ := data["final_output"].(string); output != "" {
		return fmt.Sprintf("%s\n\nFinal output:\n%s", msg, output)
	}
	return msg
}

// ShouldCollapseArg returns whether an argument should be collapsed.
func (t *CloseSubagentTool) ShouldCollapseArg(key string) bool {
	return false
}

// ShouldAlwaysExpand returns whether results should always be expanded.
func (t *CloseSubagentTool) ShouldAlwaysExpand() bool {
	return false
}
