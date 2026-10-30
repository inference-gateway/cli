package traces

import (
	"fmt"
	"io"

	cobra "github.com/spf13/cobra"

	output "github.com/inference-gateway/cli/cmd/output"
	config "github.com/inference-gateway/cli/config"
	telemetry "github.com/inference-gateway/cli/internal/platform/telemetry"
	styles "github.com/inference-gateway/cli/internal/presentation/tui/styles"
)

type command struct {
	renderer *output.Renderer
}

// NewCommand constructs the traces command.
func NewCommand(renderer *output.Renderer) *cobra.Command {
	c := &command{renderer: renderer}
	cmd := &cobra.Command{
		Use:   "traces [session-id]",
		Short: "Render a session's span tree from local or remote telemetry",
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
		RunE: c.runTraces,
	}
	cmd.Flags().Bool("list", false, "List sessions that have trace files instead of rendering a tree")
	output.AddFormatFlag(cmd, "text", "json")
	return cmd
}

func (c *command) runTraces(cmd *cobra.Command, args []string) error {
	list, _ := cmd.Flags().GetBool("list")
	format, _ := cmd.Flags().GetString("format")
	dir := config.TelemetryDir()

	sessions, err := telemetry.TraceSessions(dir)
	if err != nil {
		return fmt.Errorf("failed to list trace sessions: %w", err)
	}

	if list {
		return c.renderTraceSessions(cmd.OutOrStdout(), sessions, format)
	}

	w := cmd.OutOrStdout()

	var session string
	switch {
	case len(args) == 1:
		session = args[0]
	case len(sessions) > 0:
		session = sessions[0].ID
	default:
		fmt.Fprintln(w, "No traces recorded yet.")
		fmt.Fprintln(w)
		fmt.Fprintln(w, c.renderer.Hint("Traces accumulate as you use `infer chat` / `infer headless` (when telemetry.enabled)."))
		return nil
	}

	roots, err := telemetry.LoadTraceTree(dir, session)
	if err != nil {
		return fmt.Errorf("failed to load trace for session %s: %w", session, err)
	}

	if format == "json" {
		return output.PrintJSON(w, map[string]any{"session": session, "spans": roots})
	}
	if len(roots) == 0 {
		fmt.Fprintf(w, "No spans recorded for session %s.\n", session)
		return nil
	}
	fmt.Fprintln(w, c.renderer.Field("Session", session))
	fmt.Fprintln(w)
	fmt.Fprint(w, styles.RenderTraceTree(roots, c.renderer.TraceTreeStyle()))
	return nil
}

func (c *command) renderTraceSessions(w io.Writer, sessions []telemetry.TraceSession, format string) error {
	if format == "json" {
		return output.PrintJSON(w, sessions)
	}
	if len(sessions) == 0 {
		fmt.Fprintln(w, "No traces recorded yet.")
		return nil
	}
	fmt.Fprintln(w, c.renderer.Title("Trace Sessions"))
	fmt.Fprintln(w)
	t := c.renderer.NewListTable("Session", "Modified")
	for _, s := range sessions {
		t.Row(s.ID, s.Modified.Format("2006-01-02 15:04:05"))
	}
	fmt.Fprintln(w, t.Render())
	return nil
}
