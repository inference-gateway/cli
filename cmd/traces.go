package cmd

import (
	"encoding/json"
	"fmt"

	lipgloss "charm.land/lipgloss/v2"
	cobra "github.com/spf13/cobra"

	config "github.com/inference-gateway/cli/config"
	telemetry "github.com/inference-gateway/cli/internal/telemetry"
	colors "github.com/inference-gateway/cli/internal/ui/styles/colors"
)

var tracesCmd = &cobra.Command{
	Use:   "traces [session-id]",
	Short: "Render a session's span tree from local telemetry",
	Long: `Render the span tree of a session (root session span -> LLM turns -> tool
calls) with per-span durations, read from the local per-session trace file under
<config-dir>/telemetry. With no argument the most recent session is shown.

Traces are recorded locally (no prompt/response content) when telemetry.enabled
is true; no OTLP collector is required to view them.

Examples:
  # Span tree of the most recent session
  infer traces

  # A specific session
  infer traces 1783977086-aac06edf

  # Sessions that have trace files
  infer traces --list

  # Structured output
  infer traces --format json`,
	Args: cobra.MaximumNArgs(1),
	RunE: runTraces,
}

func init() {
	tracesCmd.Flags().Bool("list", false, "List sessions that have trace files instead of rendering a tree")
	tracesCmd.Flags().StringP("format", "f", "text", "Output format (text, json)")
	rootCmd.AddCommand(tracesCmd)
}

func runTraces(cmd *cobra.Command, args []string) error {
	list, _ := cmd.Flags().GetBool("list")
	format, _ := cmd.Flags().GetString("format")
	dir := config.TelemetryDir()

	sessions, err := telemetry.TraceSessions(dir)
	if err != nil {
		return fmt.Errorf("failed to list trace sessions: %w", err)
	}

	if list {
		return renderTraceSessions(sessions, format)
	}

	var session string
	switch {
	case len(args) == 1:
		session = args[0]
	case len(sessions) > 0:
		session = sessions[0].ID
	default:
		fmt.Println("No traces recorded yet.")
		fmt.Println()
		fmt.Println(listHint("Traces accumulate as you use `infer chat` / `infer agent` (when telemetry.enabled)."))
		return nil
	}

	roots, err := telemetry.LoadTraceTree(dir, session)
	if err != nil {
		return fmt.Errorf("failed to load trace for session %s: %w", session, err)
	}

	if format == "json" {
		return printJSON(map[string]any{"session": session, "spans": roots})
	}
	if len(roots) == 0 {
		fmt.Printf("No spans recorded for session %s.\n", session)
		return nil
	}
	fmt.Println(listField("Session", session))
	fmt.Println()
	fmt.Print(telemetry.RenderTraceTree(roots, traceTreeStyle))
	return nil
}

// traceTreeStyle colors the span tree from the static CLI palette: dim
// connectors, accent durations, red error markers.
var traceTreeStyle = telemetry.TreeStyle{
	Enumerator: listHintStyle,
	Duration:   listTitleStyle,
	Error:      lipgloss.NewStyle().Foreground(colors.ErrorColor.GetLipglossColor()),
}

func renderTraceSessions(sessions []telemetry.TraceSession, format string) error {
	if format == "json" {
		return printJSON(sessions)
	}
	if len(sessions) == 0 {
		fmt.Println("No traces recorded yet.")
		return nil
	}
	fmt.Println(listTitle("Trace Sessions"))
	fmt.Println()
	t := newListTable("Session", "Modified")
	for _, s := range sessions {
		t.Row(s.ID, s.Modified.Format("2006-01-02 15:04:05"))
	}
	fmt.Println(t.Render())
	return nil
}

func printJSON(v any) error {
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal output: %w", err)
	}
	fmt.Println(string(out))
	return nil
}
