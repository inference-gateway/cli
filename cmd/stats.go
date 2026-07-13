package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	cobra "github.com/spf13/cobra"

	config "github.com/inference-gateway/cli/config"
	formatting "github.com/inference-gateway/cli/internal/formatting"
	telemetry "github.com/inference-gateway/cli/internal/telemetry"
)

var statsCmd = &cobra.Command{
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
	RunE: runStats,
}

func init() {
	statsCmd.Flags().String("since", "", "Only include telemetry newer than this window (e.g. 7d, 24h, 30m); default all time")
	statsCmd.Flags().StringP("format", "f", "text", "Output format (text, json)")
	rootCmd.AddCommand(statsCmd)
}

func runStats(cmd *cobra.Command, _ []string) error {
	sinceStr, _ := cmd.Flags().GetString("since")
	format, _ := cmd.Flags().GetString("format")

	since, err := telemetry.ParseSince(sinceStr)
	if err != nil {
		return err
	}

	dir := config.TelemetryDir()
	if Cfg.Telemetry.RetentionDays > 0 {
		telemetry.Archive(dir, time.Now().AddDate(0, 0, -Cfg.Telemetry.RetentionDays))
	}

	stats, err := telemetry.Aggregate(dir, since)
	if err != nil {
		return fmt.Errorf("failed to aggregate telemetry: %w", err)
	}

	if format == "json" {
		return renderStatsJSON(stats)
	}
	return renderStatsText(stats)
}

func renderStatsJSON(stats telemetry.Stats) error {
	out, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal stats: %w", err)
	}
	fmt.Println(string(out))
	return nil
}

func renderStatsText(stats telemetry.Stats) error {
	if stats.Empty {
		fmt.Println("No telemetry recorded yet.")
		fmt.Println()
		fmt.Println(listHint("Telemetry accumulates as you use `infer chat` / `infer agent` (when telemetry.enabled)."))
		return nil
	}

	renderToolStats(stats.Tools)
	renderModelStats(stats.Models)
	renderSessionStats(stats.Sessions)
	return nil
}

func renderToolStats(tools []telemetry.ToolStat) {
	if len(tools) == 0 {
		return
	}
	fmt.Println(listTitle("Tool Calls"))
	fmt.Println()
	t := newListTable("Tool", "Calls", "Fail%", "Avg")
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

func renderModelStats(models []telemetry.ModelStat) {
	if len(models) == 0 {
		return
	}
	fmt.Println(listTitle("Token Usage"))
	fmt.Println()
	t := newListTable("Model", "Prompt", "Completion", "Total", "Cost")
	for _, m := range models {
		t.Row(
			m.Model,
			strconv.Itoa(m.Prompt),
			strconv.Itoa(m.Completion),
			strconv.Itoa(m.Total),
			formatting.FormatCost(m.Cost),
		)
	}
	fmt.Println(t.Render())
	fmt.Println()
}

func renderSessionStats(sessions []telemetry.SessionStat) {
	if len(sessions) == 0 {
		return
	}
	fmt.Println(listTitle("Sessions"))
	fmt.Println()
	t := newListTable("Execution", "Mode", "Sessions")
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
