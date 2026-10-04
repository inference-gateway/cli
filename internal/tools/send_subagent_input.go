package tools

import (
	"context"
	"fmt"
	"strings"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
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

// SendSubagentInputTool sends a subagent its next turn: a message written to a
// headless subagent's stdin, or text and named keys typed into an interactive
// subagent's tmux pane. A submitted prompt re-arms the completion watcher so
// the main agent is notified when the resulting turn finishes (no polling).
type SendSubagentInputTool struct {
	config    *config.Config
	tracker   scheddomain.SubagentTracker
	sendKeys  func(ctx context.Context, paneID, text string, keys []string) error
	paneState func(ctx context.Context, paneID string) paneState
}

// NewSendSubagentInputTool creates a new SendSubagentInput tool over the
// session's SubagentTracker.
func NewSendSubagentInputTool(cfg *config.Config, tracker scheddomain.SubagentTracker) *SendSubagentInputTool {
	return &SendSubagentInputTool{
		config:    cfg,
		tracker:   tracker,
		sendKeys:  tmuxSendKeys,
		paneState: tmuxPaneState,
	}
}

// Manifest returns the tool's manifest.
func (t *SendSubagentInputTool) Manifest() agentdomain.ToolManifest {
	return toolManifests.MustGet(ToolSendSubagentInput)
}

// Definition returns the tool definition for the LLM.
func (t *SendSubagentInputTool) Definition() sdk.ChatCompletionTool {
	return t.Manifest().Definition()
}

// Execute sends the input to the named subagent's pane.
func (t *SendSubagentInputTool) Execute(ctx context.Context, args map[string]any) (*agentdomain.ToolExecutionResult, error) {
	if err := t.Validate(args); err != nil {
		return nil, err
	}

	subagentID, _ := args["subagent_id"].(string)
	s := t.tracker.GetSubagent(subagentID)
	if s == nil {
		return t.fail(args, fmt.Sprintf("Subagent not found: %s (it may have been closed).", subagentID)), nil
	}

	text, _ := args["text"].(string)
	keys := optionalStringSlice(args, "keys")
	submit := true
	if v, ok := args["submit"].(bool); ok {
		submit = v
	}

	if s.Mode != scheddomain.SubagentModeInteractive || s.PaneID == "" {
		return t.sendHeadless(args, s, text, keys, submit), nil
	}
	if t.paneState(ctx, s.PaneID) == paneGone {
		return t.fail(args, fmt.Sprintf("Subagent %s's pane no longer exists; it cannot receive input.", labelOrSession(s.Label, s.SessionID))), nil
	}

	send := keys
	if submit {
		send = append(send, "Enter")
	}
	if err := t.sendKeys(ctx, s.PaneID, text, send); err != nil {
		return t.fail(args, fmt.Sprintf("Failed to send input to subagent %s: %v", labelOrSession(s.Label, s.SessionID), err)), nil
	}

	rearmed := submit && rearmSubagent(t.tracker, s)

	msg := fmt.Sprintf("Sent input to subagent %s.", labelOrSession(s.Label, s.SessionID))
	if rearmed {
		msg += " It is now running again - you will be notified automatically when it finishes; do not poll."
	} else if !submit {
		msg += " Use ReadSubagentScreen to see the result."
	}
	return &agentdomain.ToolExecutionResult{
		ToolName:  ToolSendSubagentInput,
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

// sendHeadless hands a headless subagent its next turn's message. Keys and
// submit=false drive a TUI, which a headless subagent has none of.
func (t *SendSubagentInputTool) sendHeadless(args map[string]any, s *scheddomain.SubagentState, text string, keys []string, submit bool) *agentdomain.ToolExecutionResult {
	label := labelOrSession(s.Label, s.SessionID)
	if len(keys) > 0 || !submit {
		return t.fail(args, fmt.Sprintf("Subagent %s is headless: 'keys' and submit=false drive an interactive pane's TUI and are interactive-only. Send it a 'text' message instead.", label))
	}
	if strings.TrimSpace(text) == "" {
		return t.fail(args, fmt.Sprintf("Subagent %s is headless and takes a 'text' message.", label))
	}
	if s.Input == nil {
		return t.fail(args, fmt.Sprintf("Subagent %s is a blocking or finished headless subagent and accepts no message.", label))
	}
	if err := s.Input(text); err != nil {
		return t.fail(args, fmt.Sprintf("Failed to send the message to subagent %s: %v", label, err))
	}
	return &agentdomain.ToolExecutionResult{
		ToolName:  ToolSendSubagentInput,
		Arguments: args,
		Success:   true,
		Data: map[string]any{
			"subagent_id": s.ID,
			"label":       s.Label,
			"submitted":   true,
			"message":     fmt.Sprintf("Sent the message to headless subagent %s. It runs it as its next turn - you will be notified automatically when that turn finishes; do not poll.", label),
		},
	}
}

func (t *SendSubagentInputTool) fail(args map[string]any, msg string) *agentdomain.ToolExecutionResult {
	return &agentdomain.ToolExecutionResult{
		ToolName:  ToolSendSubagentInput,
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
func (t *SendSubagentInputTool) FormatResult(result *agentdomain.ToolExecutionResult, formatType agentdomain.FormatterType) string {
	switch formatType {
	case agentdomain.FormatterShort:
		return t.FormatPreview(result)
	default:
		return t.FormatForLLM(result)
	}
}

// FormatPreview returns a short preview of the result for UI display.
func (t *SendSubagentInputTool) FormatPreview(result *agentdomain.ToolExecutionResult) string {
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
func (t *SendSubagentInputTool) FormatForLLM(result *agentdomain.ToolExecutionResult) string {
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
