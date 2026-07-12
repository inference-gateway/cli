package cmd

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	cobra "github.com/spf13/cobra"

	formatting "github.com/inference-gateway/cli/internal/formatting"
	metrics "github.com/inference-gateway/cli/internal/metrics"
	services "github.com/inference-gateway/cli/internal/services"
)

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show aggregated tool, token, and session metrics",
	Long: `Aggregate the local metrics recorded under <config-dir>/metrics into a summary:
tool calls by name (count, failure rate, p50/p95 duration), token usage and cost
by model, and sessions by mode.

Metrics are recorded locally only - no prompt/response content, never sent
anywhere - when metrics.enabled is true. Use --since to limit the window.

Examples:
  # All recorded metrics
  infer stats

  # Last 7 days
  infer stats --since 7d

  # Last 24 hours, as JSON
  infer stats --since 24h --format json`,
	RunE: runStats,
}

func init() {
	statsCmd.Flags().String("since", "", "Only include metrics newer than this window (e.g. 7d, 24h, 30m); default all time")
	statsCmd.Flags().StringP("format", "f", "text", "Output format (text, json)")
	rootCmd.AddCommand(statsCmd)
}

func runStats(cmd *cobra.Command, _ []string) error {
	sinceStr, _ := cmd.Flags().GetString("since")
	format, _ := cmd.Flags().GetString("format")

	since, err := parseSince(sinceStr)
	if err != nil {
		return err
	}

	dir := filepath.Join(Cfg.GetConfigDir(), "metrics")
	if Cfg.Metrics.RetentionDays > 0 {
		metrics.Prune(dir, time.Now().AddDate(0, 0, -Cfg.Metrics.RetentionDays))
	}

	stats, err := metrics.Aggregate(dir, since, costFunc())
	if err != nil {
		return fmt.Errorf("failed to aggregate metrics: %w", err)
	}

	if format == "json" {
		return renderStatsJSON(stats)
	}
	return renderStatsText(stats)
}

// parseSince converts a window like "7d"/"24h"/"30m" to an absolute cutoff.
// Empty means all time (zero cutoff). time.ParseDuration has no day unit, so
// "Nd" is handled here.
func parseSince(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid --since %q", s)
		}
		return time.Now().AddDate(0, 0, -n), nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid --since %q: %w", s, err)
	}
	return time.Now().Add(-d), nil
}

// costFunc adapts the pricing service to metrics.CostFunc (total cost for a
// model's token counts). Built directly rather than via the full container -
// stats is a read-only command that only needs pricing + the metrics dir.
func costFunc() metrics.CostFunc {
	pricing := services.NewPricingService(&Cfg.Pricing)
	return func(model string, prompt, completion int) float64 {
		_, _, total := pricing.CalculateCost(model, prompt, completion)
		return total
	}
}

func renderStatsJSON(stats metrics.Stats) error {
	out, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal stats: %w", err)
	}
	fmt.Println(string(out))
	return nil
}

func renderStatsText(stats metrics.Stats) error {
	if stats.Empty {
		fmt.Println("No metrics recorded yet.")
		fmt.Println()
		fmt.Println(listHint("Metrics accumulate as you use `infer chat` / `infer agent` (when metrics.enabled)."))
		return nil
	}

	renderToolStats(stats.Tools)
	renderModelStats(stats.Models)
	renderModeStats(stats.Modes)
	return nil
}

func renderToolStats(tools []metrics.ToolStat) {
	if len(tools) == 0 {
		return
	}
	fmt.Println(listTitle("Tool Calls"))
	fmt.Println()
	t := newListTable("Tool", "Calls", "Fail%", "p50", "p95")
	for _, s := range tools {
		t.Row(
			s.Name,
			strconv.Itoa(s.Calls),
			formatFailRate(s.Calls, s.Failures),
			formatMs(s.P50ms),
			formatMs(s.P95ms),
		)
	}
	fmt.Println(t.Render())
	fmt.Println()
}

func renderModelStats(models []metrics.ModelStat) {
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

func renderModeStats(modes []metrics.ModeStat) {
	if len(modes) == 0 {
		return
	}
	fmt.Println(listTitle("Sessions by Mode"))
	fmt.Println()
	t := newListTable("Mode", "Sessions", "Incomplete")
	for _, m := range modes {
		t.Row(m.Mode, strconv.Itoa(m.Count), strconv.Itoa(m.Incomplete))
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

func formatMs(ms int64) string {
	return fmt.Sprintf("%dms", ms)
}
