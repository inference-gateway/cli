package shortcuts

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	config "github.com/inference-gateway/cli/config"
	formatting "github.com/inference-gateway/cli/internal/formatting"
	telemetry "github.com/inference-gateway/cli/internal/telemetry"
)

// StatsShortcut renders the telemetry aggregate (tool, token, and session
// tables) from the local telemetry store, matching the `infer stats` output.
type StatsShortcut struct{}

// NewStatsShortcut creates a new StatsShortcut.
func NewStatsShortcut() *StatsShortcut {
	return &StatsShortcut{}
}

func (s *StatsShortcut) GetName() string { return "stats" }

func (s *StatsShortcut) GetDescription() string {
	return "Show aggregated tool, token, and session telemetry"
}

func (s *StatsShortcut) GetUsage() string { return "/stats [since]" }

func (s *StatsShortcut) CanExecute(args []string) bool {
	return len(args) <= 1
}

func (s *StatsShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	sinceStr := ""
	if len(args) > 0 {
		sinceStr = args[0]
	}

	since, err := telemetry.ParseSince(sinceStr)
	if err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("Invalid --since window %q: %v", sinceStr, err),
			Success: false,
		}, nil
	}

	dir := config.TelemetryDir()
	stats, err := telemetry.Aggregate(dir, since)
	if err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("Failed to aggregate telemetry: %v", err),
			Success: false,
		}, nil
	}

	if stats.Empty {
		return ShortcutResult{
			Output:  "No telemetry recorded yet.",
			Success: true,
		}, nil
	}

	var output strings.Builder
	renderToolStats(&output, stats.Tools)
	renderModelStats(&output, stats.Models)
	renderSessionStats(&output, stats.Sessions)

	return ShortcutResult{
		Output:  output.String(),
		Success: true,
	}, nil
}

func renderToolStats(w *strings.Builder, tools []telemetry.ToolStat) {
	if len(tools) == 0 {
		return
	}
	w.WriteString("### Tool Calls\n\n")
	w.WriteString("| Tool | Calls | Fail% | Avg |\n")
	w.WriteString("|------|-------|-------|-----|\n")
	for _, t := range tools {
		fmt.Fprintf(w, "| %s | %d | %s | %dms |\n",
			t.Name, t.Calls, formatFailRate(t.Calls, t.Failures), t.AvgMs)
	}
	w.WriteString("\n")
}

func renderModelStats(w *strings.Builder, models []telemetry.ModelStat) {
	if len(models) == 0 {
		return
	}
	w.WriteString("### Token Usage\n\n")
	w.WriteString("| Model | Prompt | Completion | Total | Cost |\n")
	w.WriteString("|-------|--------|------------|-------|------|\n")
	for _, m := range models {
		fmt.Fprintf(w, "| %s | %s | %s | %s | %s |\n",
			m.Model,
			strconv.Itoa(m.Prompt),
			strconv.Itoa(m.Completion),
			strconv.Itoa(m.Total),
			formatting.FormatCost(m.Cost))
	}
	w.WriteString("\n")
}

func renderSessionStats(w *strings.Builder, sessions []telemetry.SessionStat) {
	if len(sessions) == 0 {
		return
	}
	w.WriteString("### Sessions\n\n")
	w.WriteString("| Execution | Mode | Sessions |\n")
	w.WriteString("|-----------|------|----------|\n")
	for _, s := range sessions {
		fmt.Fprintf(w, "| %s | %s | %d |\n", s.Execution, s.Mode, s.Count)
	}
	w.WriteString("\n")
}

func formatFailRate(calls, failures int) string {
	if calls == 0 {
		return "0%"
	}
	return fmt.Sprintf("%.0f%%", 100*float64(failures)/float64(calls))
}
