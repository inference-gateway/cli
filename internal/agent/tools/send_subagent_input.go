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

// allowedSubagentKeys is the set of named tmux keys SendSubagentInput may emit.
// Restricting to this list keeps a model from injecting arbitrary tmux key-specs
// or send-keys options through the `keys` argument.
var allowedSubagentKeys = map[string]bool{
	"Enter": true, "Escape": true, "Tab": true, "Space": true, "BSpace": true,
	"Up": true, "Down": true, "Left": true, "Right": true,
	"Home": true, "End": true, "PageUp": true, "PageDown": true,
}

const allowedSubagentKeyList = "Enter, Escape, Tab, Space, BSpace, Up, Down, Left, Right, Home, End, PageUp, PageDown"

// SendSubagentInputTool types text and/or named keys into an interactive
// subagent's tmux pane - to re-prompt it or drive its TUI. When it submits a
// prompt (submit=true) it re-arms the completion watcher so the main agent is
// notified when the subagent finishes the resulting turn (no polling).
type SendSubagentInputTool struct {
	config    *config.Config
	tracker   domain.SubagentTracker
	sendKeys  func(ctx context.Context, paneID, text string, keys []string) error
	paneState func(ctx context.Context, paneID string) paneState
}

// NewSendSubagentInputTool creates a new SendSubagentInput tool over the
// session's SubagentTracker.
func NewSendSubagentInputTool(cfg *config.Config, tracker domain.SubagentTracker) *SendSubagentInputTool {
	return &SendSubagentInputTool{
		config:    cfg,
		tracker:   tracker,
		sendKeys:  tmuxSendKeys,
		paneState: tmuxPaneState,
	}
}

// Definition returns the tool definition for the LLM.
func (t *SendSubagentInputTool) Definition() sdk.ChatCompletionTool {
	description := t.config.Prompts.Tools.SendSubagentInput.Description
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "SendSubagentInput",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"subagent_id": map[string]any{
						"type":        "string",
						"description": "The interactive subagent id from ListSubagents",
					},
					"text": map[string]any{
						"type":        "string",
						"description": "Literal text to type into the subagent (e.g. a follow-up prompt)",
					},
					"keys": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Named keys to send after the text. Allowed: " + allowedSubagentKeyList,
					},
					"submit": map[string]any{
						"type":        "boolean",
						"description": "Press Enter to submit a prompt and wait for the subagent to finish (default true). Set false to only send keys for TUI navigation - then inspect with ReadSubagentScreen.",
					},
				},
				"required":             []string{"subagent_id"},
				"additionalProperties": false,
			},
		},
	}
}

// Execute sends the input to the named subagent's pane.
func (t *SendSubagentInputTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	if err := t.Validate(args); err != nil {
		return nil, err
	}

	subagentID, _ := args["subagent_id"].(string)
	s := t.tracker.GetSubagent(subagentID)
	if s == nil {
		return t.fail(args, fmt.Sprintf("Subagent not found: %s (it may have been closed).", subagentID)), nil
	}
	if s.Mode != domain.SubagentModeInteractive || s.PaneID == "" {
		return t.fail(args, fmt.Sprintf("Subagent %s is headless and has no TUI to send input to. Only interactive (tmux-pane) subagents accept input.", labelOrSession(s.Label, s.SessionID))), nil
	}
	if t.paneState(ctx, s.PaneID) == paneGone {
		return t.fail(args, fmt.Sprintf("Subagent %s's pane no longer exists; it cannot receive input.", labelOrSession(s.Label, s.SessionID))), nil
	}

	text, _ := args["text"].(string)
	keys := optionalStringSlice(args, "keys")
	submit := true
	if v, ok := args["submit"].(bool); ok {
		submit = v
	}

	send := keys
	if submit {
		send = append(send, "Enter")
	}
	if err := t.sendKeys(ctx, s.PaneID, text, send); err != nil {
		return t.fail(args, fmt.Sprintf("Failed to send input to subagent %s: %v", labelOrSession(s.Label, s.SessionID), err)), nil
	}

	rearmed := false
	if submit && s.Status != domain.SubagentRunning {
		_ = os.Remove(subagentResultFilePath(s.SessionID))
		_ = t.tracker.SetSubagentStatus(s.ID, domain.SubagentRunning)
		rearmed = true
	}

	msg := fmt.Sprintf("Sent input to subagent %s.", labelOrSession(s.Label, s.SessionID))
	if rearmed {
		msg += " It is now running again - you will be notified automatically when it finishes; do not poll."
	} else if !submit {
		msg += " Use ReadSubagentScreen to see the result."
	}
	return &domain.ToolExecutionResult{
		ToolName:  "SendSubagentInput",
		Arguments: args,
		Success:   true,
		Data: map[string]any{
			"subagent_id": s.ID,
			"label":       s.Label,
			"pane_id":     s.PaneID,
			"submitted":   submit,
			"message":     msg,
		},
	}, nil
}

func (t *SendSubagentInputTool) fail(args map[string]any, msg string) *domain.ToolExecutionResult {
	return &domain.ToolExecutionResult{
		ToolName:  "SendSubagentInput",
		Arguments: args,
		Success:   false,
		Error:     msg,
	}
}

// Validate checks the tool arguments.
func (t *SendSubagentInputTool) Validate(args map[string]any) error {
	if id, ok := args["subagent_id"].(string); !ok || id == "" {
		return fmt.Errorf("subagent_id is required and must be a non-empty string")
	}
	text, _ := args["text"].(string)
	keys := optionalStringSlice(args, "keys")
	if strings.TrimSpace(text) == "" && len(keys) == 0 {
		return fmt.Errorf("provide 'text' to type and/or 'keys' to send")
	}
	for _, k := range keys {
		if !allowedSubagentKeys[k] {
			return fmt.Errorf("unsupported key %q; allowed keys: %s", k, allowedSubagentKeyList)
		}
	}
	return nil
}

// IsEnabled reports whether the tool is enabled.
func (t *SendSubagentInputTool) IsEnabled() bool {
	return t.config.IsAgentToolEnabled() && t.tracker != nil
}

// FormatResult formats tool execution results for different contexts.
func (t *SendSubagentInputTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
	switch formatType {
	case domain.FormatterShort:
		return t.FormatPreview(result)
	default:
		return t.FormatForLLM(result)
	}
}

// FormatPreview returns a short preview of the result for UI display.
func (t *SendSubagentInputTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return "Failed to send subagent input"
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		return "Sent subagent input"
	}
	id, _ := data["subagent_id"].(string)
	return fmt.Sprintf("Sent input to subagent %s", id)
}

// FormatForLLM formats the result for LLM consumption.
func (t *SendSubagentInputTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return fmt.Sprintf("Error: %s", result.Error)
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		return "Sent subagent input"
	}
	msg, _ := data["message"].(string)
	return msg
}

// ShouldCollapseArg returns whether an argument should be collapsed.
func (t *SendSubagentInputTool) ShouldCollapseArg(key string) bool {
	return false
}

// ShouldAlwaysExpand returns whether results should always be expanded.
func (t *SendSubagentInputTool) ShouldAlwaysExpand() bool {
	return false
}
