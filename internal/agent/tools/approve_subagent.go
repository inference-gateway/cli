package tools

import (
	"context"
	"fmt"
	"os"
	"strings"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
)

// ApproveSubagentTool relays the user's approve/reject decision to an interactive
// subagent that is blocked on a tool-approval prompt (the parent is notified with
// a "[Subagent Awaiting Approval: ...]" message). approve presses Enter on the
// pane's default-focused [Approve] button; reject presses Escape. The tool is
// always approval-gated so the human confirms the relay in the MAIN chat.
type ApproveSubagentTool struct {
	config    *config.Config
	tracker   domain.SubagentTracker
	sendKeys  func(ctx context.Context, paneID, text string, keys []string) error
	paneState func(ctx context.Context, paneID string) paneState
}

// NewApproveSubagentTool creates a new ApproveSubagent tool over the session's
// SubagentTracker.
func NewApproveSubagentTool(cfg *config.Config, tracker domain.SubagentTracker) *ApproveSubagentTool {
	return &ApproveSubagentTool{
		config:    cfg,
		tracker:   tracker,
		sendKeys:  tmuxSendKeys,
		paneState: tmuxPaneState,
	}
}

// Definition returns the tool definition for the LLM.
func (t *ApproveSubagentTool) Definition() sdk.ChatCompletionTool {
	description := t.config.Prompts.Tools.ApproveSubagent.Description
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "ApproveSubagent",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"subagent_id": map[string]any{
						"type":        "string",
						"description": "The interactive subagent id from ListSubagents",
					},
					"decision": map[string]any{
						"type":        "string",
						"enum":        []string{"approve", "reject"},
						"description": "approve lets the subagent run the pending tool; reject declines it",
					},
				},
				"required":             []string{"subagent_id", "decision"},
				"additionalProperties": false,
			},
		},
	}
}

// Execute relays the decision to the named subagent's pane.
func (t *ApproveSubagentTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	if err := t.Validate(args); err != nil {
		return nil, err
	}

	subagentID, _ := args["subagent_id"].(string)
	decision := strings.ToLower(strings.TrimSpace(optionalString(args, "decision")))
	s := t.tracker.GetSubagent(subagentID)
	if s == nil {
		return t.fail(args, fmt.Sprintf("Subagent not found: %s (it may have been closed).", subagentID)), nil
	}
	if s.Mode != domain.SubagentModeInteractive || s.PaneID == "" {
		return t.fail(args, fmt.Sprintf("Subagent %s is headless and has no approval prompt to answer.", labelOrSession(s.Label, s.SessionID))), nil
	}
	if t.paneState(ctx, s.PaneID) == paneGone {
		return t.fail(args, fmt.Sprintf("Subagent %s's pane no longer exists.", labelOrSession(s.Label, s.SessionID))), nil
	}

	key := "Enter"
	if decision == "reject" {
		key = "Escape"
	}
	if err := t.sendKeys(ctx, s.PaneID, "", []string{key}); err != nil {
		return t.fail(args, fmt.Sprintf("Failed to relay decision to subagent %s: %v", labelOrSession(s.Label, s.SessionID), err)), nil
	}
	_ = os.Remove(subagentApprovalFilePath(s.SessionID))

	return &domain.ToolExecutionResult{
		ToolName:  "ApproveSubagent",
		Arguments: args,
		Success:   true,
		Data: map[string]any{
			"subagent_id": s.ID,
			"label":       s.Label,
			"decision":    decision,
			"message":     fmt.Sprintf("Relayed '%s' to subagent %s. It will continue and notify you when it finishes.", decision, labelOrSession(s.Label, s.SessionID)),
		},
	}, nil
}

func (t *ApproveSubagentTool) fail(args map[string]any, msg string) *domain.ToolExecutionResult {
	return &domain.ToolExecutionResult{
		ToolName:  "ApproveSubagent",
		Arguments: args,
		Success:   false,
		Error:     msg,
	}
}

// Validate checks the tool arguments.
func (t *ApproveSubagentTool) Validate(args map[string]any) error {
	if id, ok := args["subagent_id"].(string); !ok || id == "" {
		return fmt.Errorf("subagent_id is required and must be a non-empty string")
	}
	switch strings.ToLower(strings.TrimSpace(optionalString(args, "decision"))) {
	case "approve", "reject":
		return nil
	default:
		return fmt.Errorf("decision is required and must be \"approve\" or \"reject\"")
	}
}

// IsEnabled reports whether the tool is enabled.
func (t *ApproveSubagentTool) IsEnabled() bool {
	return t.config.IsAgentToolEnabled() && t.tracker != nil
}

// FormatResult formats tool execution results for different contexts.
func (t *ApproveSubagentTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
	switch formatType {
	case domain.FormatterShort:
		return t.FormatPreview(result)
	default:
		return t.FormatForLLM(result)
	}
}

// FormatPreview returns a short preview of the result for UI display.
func (t *ApproveSubagentTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return "Failed to relay subagent decision"
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		return "Relayed subagent decision"
	}
	id, _ := data["subagent_id"].(string)
	decision, _ := data["decision"].(string)
	return fmt.Sprintf("%s subagent %s", decision, id)
}

// FormatForLLM formats the result for LLM consumption.
func (t *ApproveSubagentTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return fmt.Sprintf("Error: %s", result.Error)
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		return "Relayed subagent decision"
	}
	msg, _ := data["message"].(string)
	return msg
}

// ShouldCollapseArg returns whether an argument should be collapsed.
func (t *ApproveSubagentTool) ShouldCollapseArg(key string) bool {
	return false
}

// ShouldAlwaysExpand returns whether results should always be expanded.
func (t *ApproveSubagentTool) ShouldAlwaysExpand() bool {
	return false
}
