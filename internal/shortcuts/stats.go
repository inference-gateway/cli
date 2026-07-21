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
// When conversationID is set the aggregate is scoped to that conversation.
type StatsShortcut struct {
	conversationID string
}

// NewStatsShortcut creates a new StatsShortcut aggregating all telemetry.
func NewStatsShortcut() *StatsShortcut {
	return &StatsShortcut{}
}

// WithConversation scopes the aggregate to a single conversation id (used by
// channel /stats). An empty id leaves it daemon-global.
func (s *StatsShortcut) WithConversation(id string) *StatsShortcut {
	s.conversationID = id
	return s
}

func (s *StatsShortcut) GetName() string { return "stats" }

func (s *StatsShortcut) GetDescription() string {
	return "Show aggregated tool, token, and session telemetry (add 'vertical' for a phone-friendly list)"
}

func (s *StatsShortcut) GetUsage() string { return "/stats [since] [vertical|table]" }

func (s *StatsShortcut) CanExecute(args []string) bool {
	nonFlag := 0
	for _, a := range args {
		switch strings.ToLower(a) {
		case "vertical", "table", "tables":
		default:
			nonFlag++
		}
	}
	return nonFlag <= 1
}

// ParseStatsArgs splits /stats arguments into the raw since-window token and
// which view was requested, order-independent and case-insensitive. A
// "vertical" token forces the list view, "table"/"tables" forces tables, and
// when neither is present vertical falls back to defaultVertical (tables in the
// TUI, the phone-friendly list on channels).
func ParseStatsArgs(args []string, defaultVertical bool) (since string, vertical bool) {
	vertical = defaultVertical
	for _, a := range args {
		switch strings.ToLower(a) {
		case "vertical":
			vertical = true
		case "table", "tables":
			vertical = false
		default:
			since = a
		}
	}
	return since, vertical
}

func (s *StatsShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	sinceStr, vertical := ParseStatsArgs(args, false)

	since, err := telemetry.ParseSince(sinceStr)
	if err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("Invalid --since window %q: %v", sinceStr, err),
			Success: false,
		}, nil
	}

	dir := config.TelemetryDir()
	stats, err := telemetry.Aggregate(dir, since, s.conversationID)
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
	if vertical {
		renderToolStatsVertical(&output, stats.Tools)
		renderModelStatsVertical(&output, stats.Models)
		renderSessionStatsVertical(&output, stats.Sessions)
	} else {
		renderToolStats(&output, stats.Tools)
		renderModelStats(&output, stats.Models)
		renderSessionStats(&output, stats.Sessions)
	}

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
	w.WriteString("| Model | Prompt | Cached | Completion | Total | Cost |\n")
	w.WriteString("|-------|--------|--------|------------|-------|------|\n")
	for _, m := range models {
		fmt.Fprintf(w, "| %s | %s | %s | %s | %s | %s |\n",
			m.Model,
			strconv.Itoa(m.Prompt),
			strconv.Itoa(m.Cached),
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

// renderVerticalEntry writes one telemetry record as a vertical block: a bold
// "Label: value" title line, then plain "Label: value" lines, then a blank line.
// This is the phone-friendly /stats view — wide markdown tables fold on Telegram.
func renderVerticalEntry(w *strings.Builder, pairs [][2]string) {
	for i, p := range pairs {
		if i == 0 {
			fmt.Fprintf(w, "**%s: %s**\n", p[0], p[1])
			continue
		}
		fmt.Fprintf(w, "%s: %s\n", p[0], p[1])
	}
	w.WriteString("\n")
}

func renderToolStatsVertical(w *strings.Builder, tools []telemetry.ToolStat) {
	if len(tools) == 0 {
		return
	}
	w.WriteString("### Tool Calls\n\n")
	for _, t := range tools {
		renderVerticalEntry(w, [][2]string{
			{"Tool", t.Name},
			{"Calls", strconv.Itoa(t.Calls)},
			{"Fail%", formatFailRate(t.Calls, t.Failures)},
			{"Avg", fmt.Sprintf("%dms", t.AvgMs)},
		})
	}
}

func renderModelStatsVertical(w *strings.Builder, models []telemetry.ModelStat) {
	if len(models) == 0 {
		return
	}
	w.WriteString("### Token Usage\n\n")
	for _, m := range models {
		renderVerticalEntry(w, [][2]string{
			{"Model", m.Model},
			{"Prompt", strconv.Itoa(m.Prompt)},
			{"Cached", strconv.Itoa(m.Cached)},
			{"Completion", strconv.Itoa(m.Completion)},
			{"Total", strconv.Itoa(m.Total)},
			{"Cost", formatting.FormatCost(m.Cost)},
		})
	}
}

func renderSessionStatsVertical(w *strings.Builder, sessions []telemetry.SessionStat) {
	if len(sessions) == 0 {
		return
	}
	w.WriteString("### Sessions\n\n")
	for _, s := range sessions {
		renderVerticalEntry(w, [][2]string{
			{"Execution", s.Execution},
			{"Mode", s.Mode},
			{"Sessions", strconv.Itoa(s.Count)},
		})
	}
}
