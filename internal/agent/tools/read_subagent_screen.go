package tools

import (
	"context"
	"fmt"
	"strings"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
)

// ReadSubagentScreenTool returns the raw terminal screen of an interactive
// subagent's tmux pane - the rendered TUI, chrome included. Unlike
// GetSubagentResult (which returns the clean last assistant message and refuses
// while the subagent is running) this is for inspecting/driving the live TUI
// (e.g. TUI testing), so it works regardless of the subagent's status. Read-only.
type ReadSubagentScreenTool struct {
	config  *config.Config
	tracker domain.SubagentTracker
	// capture returns the pane content; lines>0 bounds it to the last N lines,
	// else the full visible screen. Injection point for tests.
	capture func(ctx context.Context, paneID string, lines int) string
}

// NewReadSubagentScreenTool creates a new ReadSubagentScreen tool over the
// session's SubagentTracker.
func NewReadSubagentScreenTool(cfg *config.Config, tracker domain.SubagentTracker) *ReadSubagentScreenTool {
	return &ReadSubagentScreenTool{
		config:  cfg,
		tracker: tracker,
		capture: func(ctx context.Context, paneID string, lines int) string {
			if lines > 0 {
				return tmuxCapturePaneTail(ctx, paneID, lines)
			}
			return tmuxCapturePane(ctx, paneID)
		},
	}
}

// Definition returns the tool definition for the LLM.
func (t *ReadSubagentScreenTool) Definition() sdk.ChatCompletionTool {
	description := t.config.Prompts.Tools.ReadSubagentScreen.Description
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "ReadSubagentScreen",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"subagent_id": map[string]any{
						"type":        "string",
						"description": "The interactive subagent id from ListSubagents",
					},
					"lines": map[string]any{
						"type":        "integer",
						"description": "Optional: return only the last N lines of the screen (default: the full visible screen)",
					},
				},
				"required":             []string{"subagent_id"},
				"additionalProperties": false,
			},
		},
	}
}

// Execute captures the named subagent's terminal screen.
func (t *ReadSubagentScreenTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	if err := t.Validate(args); err != nil {
		return nil, err
	}

	subagentID, _ := args["subagent_id"].(string)
	s := t.tracker.GetSubagent(subagentID)
	if s == nil {
		return &domain.ToolExecutionResult{
			ToolName:  "ReadSubagentScreen",
			Arguments: args,
			Success:   false,
			Error:     fmt.Sprintf("Subagent not found: %s (it may have been closed).", subagentID),
		}, nil
	}

	if s.Mode != domain.SubagentModeInteractive || s.PaneID == "" {
		return &domain.ToolExecutionResult{
			ToolName:  "ReadSubagentScreen",
			Arguments: args,
			Success:   false,
			Error:     fmt.Sprintf("Subagent %s is headless and has no terminal screen. Only interactive (tmux-pane) subagents have a TUI; use GetSubagentResult for a subagent's result.", labelOrSession(s.Label, s.SessionID)),
		}, nil
	}

	screen := t.capture(ctx, s.PaneID, toInt(args["lines"]))
	return &domain.ToolExecutionResult{
		ToolName:  "ReadSubagentScreen",
		Arguments: args,
		Success:   true,
		Data: map[string]any{
			"subagent_id": s.ID,
			"label":       s.Label,
			"pane_id":     s.PaneID,
			"status":      string(s.Status),
			"screen":      screen,
		},
	}, nil
}

// Validate checks the tool arguments.
func (t *ReadSubagentScreenTool) Validate(args map[string]any) error {
	if id, ok := args["subagent_id"].(string); !ok || id == "" {
		return fmt.Errorf("subagent_id is required and must be a non-empty string")
	}
	return nil
}

// IsEnabled reports whether the tool is enabled.
func (t *ReadSubagentScreenTool) IsEnabled() bool {
	return t.config.IsAgentToolEnabled() && t.tracker != nil
}

// FormatResult formats tool execution results for different contexts.
func (t *ReadSubagentScreenTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
	switch formatType {
	case domain.FormatterShort:
		return t.FormatPreview(result)
	default:
		return t.FormatForLLM(result)
	}
}

// FormatPreview returns a short preview of the result for UI display.
func (t *ReadSubagentScreenTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return "Failed to read subagent screen"
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		return "Read subagent screen"
	}
	id, _ := data["subagent_id"].(string)
	return fmt.Sprintf("Read screen of subagent %s", id)
}

// FormatForLLM formats the result for LLM consumption.
func (t *ReadSubagentScreenTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return fmt.Sprintf("Error: %s", result.Error)
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		return "Read subagent screen"
	}
	screen, _ := data["screen"].(string)
	if strings.TrimSpace(screen) == "" {
		return "Subagent screen is empty (the pane may be blank or no longer available)."
	}
	return fmt.Sprintf("Subagent terminal screen:\n```\n%s\n```", screen)
}

// ShouldCollapseArg returns whether an argument should be collapsed.
func (t *ReadSubagentScreenTool) ShouldCollapseArg(key string) bool {
	return false
}

// ShouldAlwaysExpand returns whether results should always be expanded.
func (t *ReadSubagentScreenTool) ShouldAlwaysExpand() bool {
	return false
}
