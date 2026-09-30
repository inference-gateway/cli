package components

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	formatting "github.com/inference-gateway/cli/internal/platform/formatting"
	styles "github.com/inference-gateway/cli/internal/presentation/tui/styles"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
)

// maxSubagentRows caps the stacked list so a large fan-out cannot push the
// status bar off screen; the overflow shows as a "+N more" row.
const maxSubagentRows = 5

// SubagentList renders the right-aligned stacked list below the composer:
// one row per tracked sub-agent with its label and a live elapsed counter,
// finished jobs lingering with their final state before they drop off.
type SubagentList struct {
	registry      scheddomain.BackgroundTaskRegistry
	config        *config.Config
	styleProvider *styles.Provider
	width         int
	tickEpoch     int
}

// NewSubagentList creates the sub-agent indicator list.
func NewSubagentList(styleProvider *styles.Provider) *SubagentList {
	return &SubagentList{styleProvider: styleProvider}
}

// subagentRefreshTickMsg drives the live elapsed column and the linger
// countdown - the task view's taskRefreshTickMsg pattern, armed only while
// rows are on screen instead of an idle poller.
type subagentRefreshTickMsg struct{ epoch int }

// SetRegistry wires the shared background task registry, the data source
// for every tracked sub-agent.
func (l *SubagentList) SetRegistry(registry scheddomain.BackgroundTaskRegistry) {
	l.registry = registry
}

// SetConfig wires the config so the indicator toggle and linger delay are
// read live at render time.
func (l *SubagentList) SetConfig(cfg *config.Config) { l.config = cfg }

// Init implements tea.Model.
func (l *SubagentList) Init() tea.Cmd { return nil }

// View renders the list wrapped in a tea.View.
func (l *SubagentList) View() tea.View { return tea.NewView(l.Render()) }

func (l *SubagentList) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		l.width = msg.Width
	case agentdomain.BackgroundTasksChangedEvent:
		cmd = l.maybeRefreshTick()
	case subagentRefreshTickMsg:
		cmd = l.handleRefreshTick(msg.epoch)
	}
	return l, cmd
}

func (l *SubagentList) handleRefreshTick(epoch int) tea.Cmd {
	if epoch != l.tickEpoch {
		return nil
	}
	return l.maybeRefreshTick()
}

// maybeRefreshTick keeps exactly one 1s chain alive while any row (running
// or lingering) is visible, letting it die the moment the list empties -
// a bounded animation tick, not an idle poller.
func (l *SubagentList) maybeRefreshTick() tea.Cmd {
	if len(l.snapshotRows()) == 0 {
		return nil
	}
	l.tickEpoch++
	epoch := l.tickEpoch
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return subagentRefreshTickMsg{epoch: epoch} })
}

// subagentRow carries one row's render inputs derived from the registry.
type subagentRow struct {
	label   string
	running bool
	elapsed time.Duration
	state   string // "done" / "failed" on lingered rows
	started time.Time
}

// snapshotRows adapts every tracked sub-agent job to a row, sorted newest
// first, dropping finished jobs whose linger window has passed.
func (l *SubagentList) snapshotRows() []subagentRow {
	if l.registry == nil {
		return nil
	}
	var rows []subagentRow
	for _, job := range l.registry.Snapshot() {
		if job.Meta.Kind != scheddomain.JobKindSubagent {
			continue
		}
		if !l.shouldShowRow(job) {
			continue
		}
		end := time.Now()
		if job.CompletedAt != nil {
			end = *job.CompletedAt
		}
		row := subagentRow{
			label:   job.Meta.Label,
			elapsed: end.Sub(job.Meta.StartedAt),
			running: job.Status == scheddomain.JobRunning,
			started: job.Meta.StartedAt,
		}
		if !row.running {
			row.state = "done"
			if job.Status == scheddomain.JobFailed {
				row.state = "failed"
			}
		}
		rows = append(rows, row)
	}
	slices.SortFunc(rows, func(a, b subagentRow) int {
		return b.started.Compare(a.started)
	})
	return rows
}

// shouldShowRow reports whether a tracked job earns a row: running jobs
// always do, finished ones linger then drop (a zero delay drops at once).
func (l *SubagentList) shouldShowRow(job scheddomain.TrackedJob) bool {
	if job.Status == scheddomain.JobRunning {
		return true
	}
	if l.lingerDelay() == 0 {
		return false
	}
	return job.CompletedAt == nil || time.Since(*job.CompletedAt) < l.lingerDelay()
}

// lingerDelay returns the configured linger window.
func (l *SubagentList) lingerDelay() time.Duration {
	if l.config == nil {
		return 5 * time.Second
	}
	return time.Duration(l.config.Chat.StatusBar.SubagentLingerSeconds) * time.Second
}

// enabled honors the chat.status_bar.indicators.subagents toggle so the
// list can be switched off like every other status-bar element.
func (l *SubagentList) enabled() bool {
	return l.config == nil || l.config.Chat.StatusBar.Indicators.Subagents
}

// Render draws the rows right-aligned below the composer; empty string when
// nothing runs or lingers so no lines are reserved.
func (l *SubagentList) Render() string {
	if l.registry == nil || !l.enabled() {
		return ""
	}
	rows := l.snapshotRows()
	if len(rows) == 0 {
		return ""
	}
	shown := rows[:min(len(rows), maxSubagentRows)]
	more := len(rows) - len(shown)

	lines := make([]string, 0, len(shown)+1)
	for i, row := range shown {
		line := l.rowView(row, i, len(shown))
		if l.width > 0 {
			line = l.styleProvider.PlaceHorizontal(l.width, "", line)
		}
		lines = append(lines, line)
	}
	if more > 0 {
		overflow := fmt.Sprintf("+%d more", more)
		line := l.styleProvider.RenderWithColor(overflow, l.styleProvider.GetThemeColor("dim"))
		if l.width > 0 {
			line = l.styleProvider.PlaceHorizontal(l.width, "", line)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// rowView draws one entry: box-drawing connector, label, and either the
// running elapsed counter or the final state and total duration.
func (l *SubagentList) rowView(row subagentRow, index, count int) string {
	connector := ""
	if count > 1 {
		switch index {
		case 0:
			connector = "┌ "
		case count - 1:
			connector = "└ "
		default:
			connector = "│ "
		}
	}
	label := "subagent"
	if trimmed := strings.TrimSpace(row.label); trimmed != "" {
		label = formatting.TruncateText(trimmed, 12)
	}
	dim := l.styleProvider.GetThemeColor("dim")
	elapsed := l.styleProvider.RenderWithColor(formatDuration(row.elapsed), dim)
	if row.running {
		return fmt.Sprintf("%s%-13s %s", connector, label, elapsed)
	}
	style := "dim"
	if row.state == "failed" {
		style = "error"
	}
	state := l.styleProvider.RenderWithColor(row.state, l.styleProvider.GetThemeColor(style))
	return fmt.Sprintf("%s%-13s %s %s", connector, label, state, elapsed)
}
