package shortcuts

import (
	"context"
	"fmt"

	lipgloss "charm.land/lipgloss/v2"

	config "github.com/inference-gateway/cli/config"
	telemetry "github.com/inference-gateway/cli/internal/telemetry"
	colors "github.com/inference-gateway/cli/internal/ui/styles/colors"
)

// traceTreeStyle colors the span tree from the static CLI palette: dim
// connectors, accent durations, red error markers.
var traceTreeStyle = telemetry.TreeStyle{
	Enumerator: lipgloss.NewStyle().Foreground(colors.DimColor.GetLipglossColor()),
	Duration:   lipgloss.NewStyle().Bold(true).Foreground(colors.AccentColor.GetLipglossColor()),
	Error:      lipgloss.NewStyle().Foreground(colors.ErrorColor.GetLipglossColor()),
}

// TracesShortcut renders a session's span tree from the local per-session
// trace file, matching the `infer traces` output.
type TracesShortcut struct{}

// NewTracesShortcut creates a new TracesShortcut.
func NewTracesShortcut() *TracesShortcut {
	return &TracesShortcut{}
}

func (s *TracesShortcut) GetName() string { return "traces" }

func (s *TracesShortcut) GetDescription() string {
	return "Show a session's span tree from local telemetry"
}

func (s *TracesShortcut) GetUsage() string { return "/traces [session-id]" }

func (s *TracesShortcut) CanExecute(args []string) bool {
	return len(args) <= 1
}

func (s *TracesShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	dir := config.TelemetryDir()

	var session string
	if len(args) > 0 {
		session = args[0]
	} else {
		sessions, err := telemetry.TraceSessions(dir)
		if err != nil {
			return ShortcutResult{
				Output:  fmt.Sprintf("Failed to list trace sessions: %v", err),
				Success: false,
			}, nil
		}
		if len(sessions) == 0 {
			return ShortcutResult{
				Output:  "No traces recorded yet.",
				Success: true,
			}, nil
		}
		session = sessions[0].ID
	}

	roots, err := telemetry.LoadTraceTree(dir, session)
	if err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("Failed to load trace for session %s: %v", session, err),
			Success: false,
		}, nil
	}
	if len(roots) == 0 {
		return ShortcutResult{
			Output:  fmt.Sprintf("No spans recorded for session %s.", session),
			Success: true,
		}, nil
	}

	output := fmt.Sprintf("Traces: %s\n\n%s", session, telemetry.RenderTraceTree(roots, traceTreeStyle))

	return ShortcutResult{
		Output:  output,
		Success: true,
	}, nil
}
