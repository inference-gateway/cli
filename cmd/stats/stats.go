package stats

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	cobra "github.com/spf13/cobra"

	output "github.com/inference-gateway/cli/cmd/output"
	runtime "github.com/inference-gateway/cli/cmd/runtime"
	config "github.com/inference-gateway/cli/config"
	formatting "github.com/inference-gateway/cli/internal/platform/formatting"
	telemetry "github.com/inference-gateway/cli/internal/platform/telemetry"
)

type command struct {
	state    *runtime.State
	renderer *output.Renderer
}

// NewCommand constructs the stats command.
func NewCommand(state *runtime.State, renderer *output.Renderer) *cobra.Command {
	c := &command{state: state, renderer: renderer}
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show aggregated tool, token, and session telemetry",
		Long: `Aggregate the local telemetry recorded under <config-dir>/telemetry into a
summary: tool calls by name (count, failure rate, avg duration), token usage and
cost by model, and sessions by execution and agent mode.

Telemetry is recorded locally (OTLP/semconv, no prompt/response content) when
telemetry.enabled is true, and optionally also pushed to an OTLP collector. Use
--since to limit the window.

Examples:
  # All recorded telemetry
  infer stats

  # Last 7 days
  infer stats --since 7d

  # Last 24 hours, as JSON
  infer stats --since 24h --format json`,
		RunE: c.runStats,
	}
	cmd.Flags().String("since", "", "Only include telemetry newer than this window (e.g. 7d, 24h, 30m); default all time")
	cmd.Flags().StringP("format", "f", "text", "Output format (text, json)")
	return cmd
}

func (c *command) runStats(cmd *cobra.Command, _ []string) error {
	sinceStr, _ := cmd.Flags().GetString("since")
	format, _ := cmd.Flags().GetString("format")

	since, err := telemetry.ParseSince(sinceStr)
	if err != nil {
		return err
	}

	dir := config.TelemetryDir()
	if retentionDays := c.state.Config().Telemetry.RetentionDays; retentionDays > 0 {
		telemetry.Archive(dir, time.Now().AddDate(0, 0, -retentionDays))
	}

	stats, err := telemetry.Aggregate(dir, since, "")
	if err != nil {
		return fmt.Errorf("failed to aggregate telemetry: %w", err)
	}

	if format == "json" {
		return renderStatsJSON(stats)
	}
	return c.renderStatsText(stats)
}

func renderStatsJSON(stats telemetry.Stats) error {
	out, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal stats: %w", err)
	}
	fmt.Println(string(out))
	return nil
}

func (c *command) renderStatsText(stats telemetry.Stats) error {
	if stats.Empty {
		fmt.Println("No telemetry recorded yet.")
		fmt.Println()
		fmt.Println(c.renderer.Hint("Telemetry accumulates as you use `infer chat` / `infer headless` (when telemetry.enabled)."))
		return nil
	}

	c.renderToolStats(stats.Tools)
	c.renderModelStats(stats.Models)
	c.renderSessionStats(stats.Sessions)
	return nil
}

func (c *command) renderToolStats(tools []telemetry.ToolStat) {
	if len(tools) == 0 {
		return
	}
	fmt.Println(c.renderer.Title("Tool Calls"))
	fmt.Println()
	t := c.renderer.NewListTable("Tool", "Calls", "Fail%", "Avg")
	for _, s := range tools {
		t.Row(
			s.Name,
			strconv.Itoa(s.Calls),
			formatFailRate(s.Calls, s.Failures),
			fmt.Sprintf("%dms", s.AvgMs),
		)
	}
	fmt.Println(t.Render())
	fmt.Println()
}

func (c *command) renderModelStats(models []telemetry.ModelStat) {
	if len(models) == 0 {
		return
	}
	fmt.Println(c.renderer.Title("Token Usage"))
	fmt.Println()
	t := c.renderer.NewListTable("Model", "Prompt", "Cached", "Completion", "Total", "Cost")
	for _, m := range models {
		t.Row(
			m.Model,
			strconv.Itoa(m.Prompt),
			strconv.Itoa(m.Cached),
			strconv.Itoa(m.Completion),
			strconv.Itoa(m.Total),
			formatting.FormatCost(m.Cost),
		)
	}
	fmt.Println(t.Render())
	fmt.Println()
}

func (c *command) renderSessionStats(sessions []telemetry.SessionStat) {
	if len(sessions) == 0 {
		return
	}
	fmt.Println(c.renderer.Title("Sessions"))
	fmt.Println()
	t := c.renderer.NewListTable("Execution", "Mode", "Sessions")
	for _, s := range sessions {
		t.Row(s.Execution, s.Mode, strconv.Itoa(s.Count))
	}
	fmt.Println(t.Render())
	fmt.Println()
}

func formatFailRate(calls, failures int) string {
	if calls == 0 {
		return "0%"
	}
	return fmt.Sprintf("%.0f%%", 100*float64(failures)/float64(calls))
}
