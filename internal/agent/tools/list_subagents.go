package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
)

// ListSubagentsTool lists local subagents spawned by the Agent tool. For
// interactive (tmux-pane) subagents it reports the pane's live status so the
// main agent can tell which have finished. It is the subagent analogue of
// ListShells.
type ListSubagentsTool struct {
	config    *config.Config
	tracker   domain.SubagentTracker
	paneState func(ctx context.Context, paneID string) paneState
}

// NewListSubagentsTool creates a new ListSubagents tool over the session's
// SubagentTracker.
func NewListSubagentsTool(cfg *config.Config, tracker domain.SubagentTracker) *ListSubagentsTool {
	return &ListSubagentsTool{
		config:    cfg,
		tracker:   tracker,
		paneState: tmuxPaneState,
	}
}

// Definition returns the tool definition for the LLM.
func (t *ListSubagentsTool) Definition() sdk.ChatCompletionTool {
	description := t.config.Prompts.Tools.ListSubagents.Description
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "ListSubagents",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type":                 "object",
				"properties":           map[string]any{},
				"required":             []string{},
				"additionalProperties": false,
			},
		},
	}
}

// Execute lists all tracked subagents.
func (t *ListSubagentsTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	subagents := t.tracker.GetAllSubagents()
	if len(subagents) == 0 {
		return &domain.ToolExecutionResult{
			ToolName:  "ListSubagents",
			Arguments: args,
			Success:   true,
			Data: map[string]any{
				"subagent_count": 0,
				"message":        "No subagents are currently running or tracked.",
			},
		}, nil
	}

	infos := make([]map[string]any, 0, len(subagents))
	for _, s := range subagents {
		infos = append(infos, t.subagentInfo(ctx, s))
	}
	return &domain.ToolExecutionResult{
		ToolName:  "ListSubagents",
		Arguments: args,
		Success:   true,
		Data: map[string]any{
			"subagent_count": len(infos),
			"subagents":      infos,
		},
	}, nil
}

// subagentInfo builds the per-subagent record. Interactive subagents get their
// live pane status (alive=running, dead=finished, gone=closed); headless ones
// report their tracked status.
func (t *ListSubagentsTool) subagentInfo(ctx context.Context, s *domain.SubagentState) map[string]any {
	status := string(s.Status)
	// While an interactive subagent is still running, refine its status with the
	// live pane state (running / finished / closed). A completed one keeps its
	// tracked "completed" status (its pane is idle but still alive).
	if s.Mode == domain.SubagentModeInteractive && s.Status == domain.SubagentRunning {
		status = t.paneState(ctx, s.PaneID).status()
	}
	// A subagent blocked on a tool-approval prompt reports awaiting_approval. This
	// is a display-only override - its tracked Status stays Running so the poller
	// keeps watching it (and re-notifies once the approval is resolved).
	awaiting := false
	if s.Mode == domain.SubagentModeInteractive {
		if _, ok := readSubagentApproval(s.SessionID); ok {
			status = "awaiting_approval"
			awaiting = true
		}
	}
	info := map[string]any{
		"subagent_id": s.ID,
		"label":       s.Label,
		"mode":        s.Mode,
		"status":      status,
		"session_id":  s.SessionID,
		"started_at":  s.StartedAt.Format("15:04:05"),
		"elapsed":     time.Since(s.StartedAt).Round(time.Second).String(),
	}
	if s.PaneID != "" {
		info["pane_id"] = s.PaneID
	}
	switch {
	case awaiting:
		info["note"] = "blocked on tool approval; review and respond with ApproveSubagent"
	case s.Status == domain.SubagentRunning:
		info["note"] = "notifies automatically when it finishes; do not poll"
	}
	return info
}

// Validate checks the tool arguments (none required for ListSubagents).
func (t *ListSubagentsTool) Validate(args map[string]any) error {
	return nil
}

// IsEnabled reports whether the tool is enabled.
func (t *ListSubagentsTool) IsEnabled() bool {
	return t.config.IsAgentToolEnabled() && t.tracker != nil
}

// FormatResult formats the result for display.
func (t *ListSubagentsTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
	if formatType == domain.FormatterShort {
		return t.FormatPreview(result)
	}
	return t.formatList(result)
}

// FormatPreview returns a short preview.
func (t *ListSubagentsTool) FormatPreview(result *domain.ToolExecutionResult) string {
	data, ok := result.Data.(map[string]any)
	if !ok {
		return "ListSubagents completed"
	}
	count := toInt(data["subagent_count"])
	if count == 0 {
		return "No subagents running"
	}
	return fmt.Sprintf("Found %d subagent(s)", count)
}

func (t *ListSubagentsTool) formatList(result *domain.ToolExecutionResult) string {
	data, ok := result.Data.(map[string]any)
	if !ok {
		return "ListSubagents completed"
	}
	count := toInt(data["subagent_count"])
	if count == 0 {
		return "No subagents are currently running or tracked."
	}

	var out strings.Builder
	fmt.Fprintf(&out, "Subagents (%d):\n\n", count)
	for _, s := range asMapSlice(data["subagents"]) {
		id, _ := s["subagent_id"].(string)
		mode, _ := s["mode"].(string)
		status, _ := s["status"].(string)
		fmt.Fprintf(&out, "- %s [%s] %s\n", id, mode, status)
		if label, _ := s["label"].(string); label != "" {
			fmt.Fprintf(&out, "   label: %s\n", label)
		}
		if pane, _ := s["pane_id"].(string); pane != "" {
			fmt.Fprintf(&out, "   pane: %s\n", pane)
		}
		if elapsed, _ := s["elapsed"].(string); elapsed != "" {
			fmt.Fprintf(&out, "   elapsed: %s\n", elapsed)
		}
	}
	out.WriteString("\nRunning subagents notify you automatically when they finish - do not poll. Use CloseSubagent(subagent_id=\"<id>\") only to stop one early or tidy a finished pane.")
	return out.String()
}

// ShouldCollapseArg returns whether an argument should be collapsed.
func (t *ListSubagentsTool) ShouldCollapseArg(key string) bool {
	return false
}

// ShouldAlwaysExpand returns whether results should always be expanded.
func (t *ListSubagentsTool) ShouldAlwaysExpand() bool {
	return false
}
