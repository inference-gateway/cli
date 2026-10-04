package components

import (
	"cmp"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	config "github.com/inference-gateway/cli/config"
	formatting "github.com/inference-gateway/cli/internal/platform/formatting"
	styles "github.com/inference-gateway/cli/internal/presentation/tui/styles"
	icons "github.com/inference-gateway/cli/internal/presentation/tui/styles/icons"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
)

// maxSubagentRows caps the stacked list so a large fan-out cannot push the
// status bar off screen. The cap is a scroll window: rows outside it show as
// "N above" and "+N more" boundary markers and the selection can still reach
// them.
const maxSubagentRows = 5

// subagentLabelMinWidth keeps the label column at least as wide as the fixed
// column this list used before names were fitted, so short names keep their
// shape.
const subagentLabelMinWidth = 13

// rowFixedColumns is the width a row spends outside the label, kind and
// elapsed columns: the 2-column connector, the space after the label and the
// gutters around the status cell.
const rowFixedColumns = 5

// SubagentList renders the stacked list below the composer stretched across its
// width: one row per background job (sub-agent, A2A task, shell, recording)
// with its label, its kind and a live elapsed counter. Finished jobs linger
// with their outcome.
type SubagentList struct {
	registry      scheddomain.BackgroundTaskRegistry
	config        *config.Config
	styleProvider *styles.Provider
	width         int
	focused       bool
	selectedID    string
	viewingID     string
	offset        int
}

// NewSubagentList creates the sub-agent indicator list.
func NewSubagentList(styleProvider *styles.Provider) *SubagentList {
	return &SubagentList{styleProvider: styleProvider}
}

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
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		l.width = size.Width
	}
	return l, nil
}

// HasRows reports whether any row, running or lingering, is on screen. The
// chat's live clock ticks once a second while it holds, which advances the
// elapsed column and drops rows whose linger ran out.
func (l *SubagentList) HasRows() bool {
	return len(l.snapshotRows()) > 0
}

// subagentRow carries one row's render inputs derived from the registry.
type subagentRow struct {
	id      string
	label   string
	kind    string
	running bool
	elapsed time.Duration
	failed  bool
	stats   *scheddomain.SubagentRunStats
	started time.Time
}

// rowWidths are the column widths every row shares, so the connectors, the
// state cell and the duration column never drift between rows.
type rowWidths struct {
	label    int
	kind     int
	state    int
	duration int
}

// measureRows fits the columns to the rows on screen: the kind and duration
// columns grow to their widest value and the label column absorbs the slack up
// to the row width.
func (l *SubagentList) measureRows(rows []subagentRow, blockWidth int) rowWidths {
	widths := rowWidths{label: subagentLabelMinWidth}
	for _, row := range rows {
		widths.label = max(widths.label, l.styleProvider.GetWidth(rowLabel(row)))
		widths.kind = max(widths.kind, l.styleProvider.GetWidth(row.kind))
		if w := l.styleProvider.GetWidth(formatDuration(row.elapsed)); w > widths.duration {
			widths.duration = w
		}
		if !row.running {
			widths.state = l.styleProvider.GetWidth(icons.CheckMark)
		}
	}
	if blockWidth > 0 {
		widths.label = max(subagentLabelMinWidth,
			blockWidth-widths.kind-widths.state-widths.duration-rowFixedColumns)
	}
	return widths
}

// rowLabel is a row's label on one line, or its kind when it has none.
func rowLabel(row subagentRow) string {
	return cmp.Or(strings.Join(strings.Fields(row.label), " "), row.kind)
}

// snapshotRows adapts every tracked job to a row, sorted newest first,
// dropping finished jobs whose linger window has passed.
func (l *SubagentList) snapshotRows() []subagentRow {
	if l.registry == nil {
		return nil
	}
	var rows []subagentRow
	for _, job := range l.registry.Snapshot() {
		if !l.shouldShowRow(job) {
			continue
		}
		end := time.Now()
		if job.CompletedAt != nil {
			end = *job.CompletedAt
		}
		row := subagentRow{
			id:      job.Meta.ID,
			label:   jobRowLabel(job),
			kind:    jobRowKind(job),
			elapsed: end.Sub(job.Meta.StartedAt),
			running: job.Status == scheddomain.JobRunning,
			failed:  job.Status == scheddomain.JobFailed,
			stats:   job.Stats,
			started: job.Meta.StartedAt,
		}
		rows = append(rows, row)
	}
	slices.SortFunc(rows, func(a, b subagentRow) int {
		return b.started.Compare(a.started)
	})
	return rows
}

// jobRowLabel names a row. A2A tasks and shells are labelled by a raw ID, so an
// A2A task shows its agent's host and a shell shows its command instead.
func jobRowLabel(job scheddomain.TrackedJob) string {
	switch job.Meta.Kind {
	case scheddomain.JobKindShell:
		return cmp.Or(job.Meta.Detail, job.Meta.Label)
	case scheddomain.JobKindA2A:
		if agent, err := url.Parse(job.Meta.Detail); err == nil && agent.Host != "" {
			return agent.Host
		}
	}
	return job.Meta.Label
}

// selectedRowMarker replaces a row's tree connector while the list holds the
// keyboard selection.
const selectedRowMarker = "❯ "

// selectedRow is the index of the selected job among the given rows. A
// selection that dropped off the list falls back to the newest row.
func selectedRow(rows []subagentRow, selectedID string) int {
	return max(0, slices.IndexFunc(rows, func(row subagentRow) bool { return row.id == selectedID }))
}

// visibleRows are the rows on screen: at most maxSubagentRows of the tracked
// rows, from the window offset. While the list holds focus the window follows
// the selection so rows past the cap stay reachable.
func (l *SubagentList) visibleRows() (all, shown []subagentRow) {
	all = l.snapshotRows()
	l.shiftWindow(all)
	return all, all[l.offset:min(len(all), l.offset+maxSubagentRows)]
}

// shiftWindow keeps the window offset on the selection while the list is
// focused, clamping it to the row range. An unfocused list draws from the
// newest rows.
func (l *SubagentList) shiftWindow(all []subagentRow) {
	offset := 0
	if l.focused {
		sel := selectedRow(all, l.selectedID)
		offset = l.offset
		switch {
		case sel < l.offset:
			offset = sel
		case sel >= l.offset+maxSubagentRows:
			offset = sel - maxSubagentRows + 1
		}
	}
	l.offset = min(max(offset, 0), max(0, len(all)-maxSubagentRows))
}

// Focus gives the list the keyboard selection on its newest row. It reports
// false when there is no row to select.
func (l *SubagentList) Focus() bool {
	rows, _ := l.visibleRows()
	if !l.enabled() || len(rows) == 0 {
		return false
	}
	l.focused = true
	l.selectedID = rows[0].id
	return true
}

// Blur returns the list to display only and stops pinning a viewed row.
func (l *SubagentList) Blur() {
	l.focused = false
	l.viewingID = ""
}

// IsFocused reports whether the list holds the keyboard selection.
func (l *SubagentList) IsFocused() bool { return l.focused }

// SelectNext moves the selection one row down. It reports false on the last row.
func (l *SubagentList) SelectNext() bool { return l.moveSelection(1) }

// padToWidth pads a rendered line with trailing spaces to the row width so
// every line of the list ends on the same edge as its rows.
func (l *SubagentList) padToWidth(line string, blockWidth int) string {
	return line + strings.Repeat(" ", max(0, blockWidth-l.styleProvider.GetWidth(line)))
}

// overflowRow renders a dim list boundary label (like "+2 more") on the same
// width as the rows. It is only produced when rows are scrolled out of view.
func (l *SubagentList) overflowRow(text string, blockWidth int) string {
	return l.padToWidth(l.styleProvider.RenderWithColor(text, l.styleProvider.GetThemeColor("dim")), blockWidth)
}

// SelectPrev moves the selection one row up. It reports false on the first row.
func (l *SubagentList) SelectPrev() bool { return l.moveSelection(-1) }

func (l *SubagentList) moveSelection(delta int) bool {
	rows, _ := l.visibleRows()
	next := selectedRow(rows, l.selectedID) + delta
	if next < 0 || next >= len(rows) {
		return false
	}
	l.selectedID = rows[next].id
	return true
}

// SelectedJob returns the job under the selection, false when it is gone.
func (l *SubagentList) SelectedJob() (scheddomain.TrackedJob, bool) {
	_, shown := l.visibleRows()
	if !l.focused || len(shown) == 0 {
		return scheddomain.TrackedJob{}, false
	}
	return l.Job(shown[selectedRow(shown, l.selectedID)].id)
}

// Job returns the tracked job with the given ID, false when it was reaped.
func (l *SubagentList) Job(id string) (scheddomain.TrackedJob, bool) {
	if l.registry == nil {
		return scheddomain.TrackedJob{}, false
	}
	jobs := l.registry.Snapshot()
	i := slices.IndexFunc(jobs, func(job scheddomain.TrackedJob) bool { return job.Meta.ID == id })
	if i < 0 {
		return scheddomain.TrackedJob{}, false
	}
	return jobs[i], true
}

// SetViewing marks the job whose transcript is on screen, which pins its row
// past the linger window. An empty ID clears it.
func (l *SubagentList) SetViewing(id string) { l.viewingID = id }

// focusHint is the key legend shown under the rows while the list has focus.
func (l *SubagentList) focusHint() string {
	if l.viewingID != "" {
		return "↑/↓ switch · esc back to chat"
	}
	return "↑/↓ select · enter view · esc back"
}

// jobRowKind is the row's metadata tag: the job kind, followed by where the work
// runs when the job says so (an A2A task is "a2a local" or "a2a external").
func jobRowKind(job scheddomain.TrackedJob) string {
	return strings.TrimSpace(string(job.Meta.Kind) + " " + job.Meta.Origin)
}

// shouldShowRow reports whether a tracked job earns a row: running jobs and
// the one being viewed always do, finished ones linger then drop (a zero delay
// drops at once).
func (l *SubagentList) shouldShowRow(job scheddomain.TrackedJob) bool {
	if job.Status == scheddomain.JobRunning || job.Meta.ID == l.viewingID {
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

// Render draws the rows stretched across the composer's width, flush with the
// version column edge. Every row spans the same columns so the tree connector
// never drifts when durations gain digits. Empty output when nothing runs or
// lingers reserves no lines.
func (l *SubagentList) Render() string {
	if l.registry == nil || !l.enabled() {
		return ""
	}
	rows, shown := l.visibleRows()
	if len(shown) == 0 {
		return ""
	}
	more := len(rows) - l.offset - len(shown)
	selected := -1
	if l.focused {
		selected = selectedRow(shown, l.selectedID)
	}

	blockWidth := l.width - versionRightInset
	widths := l.measureRows(shown, blockWidth)
	rowWidth := widths.label + widths.kind + widths.state + widths.duration + rowFixedColumns

	lines := make([]string, 0, len(shown)+1)
	for i, row := range shown {
		lines = append(lines, l.rowView(row, i, len(shown), widths, i == selected))
		if row.stats != nil {
			lines = append(lines, l.statsView(*row.stats, i, len(shown), rowWidth))
		}
	}
	if more > 0 {
		lines = append(lines, l.overflowRow(fmt.Sprintf("+%d more", more), rowWidth))
	}
	if l.offset > 0 {
		lines = append([]string{l.overflowRow(fmt.Sprintf("%d above", l.offset), rowWidth)}, lines...)
	}
	if l.focused {
		hint := l.styleProvider.RenderWithColor(l.focusHint(), l.styleProvider.GetThemeColor("dim"))
		lines = append(lines, l.padToWidth(hint, rowWidth))
	}
	return strings.Join(lines, "\n")
}

// statsView draws a sub-agent's run stats as a child line under its row: tool
// calls succeeded and failed, then the tokens used, live while it runs. It is
// padded to the shared row width so the block stays aligned. An icon takes its
// status color only once its count is positive, so a zero reads as idle.
func (l *SubagentList) statsView(stats scheddomain.SubagentRunStats, index, count, width int) string {
	trunk := ""
	if count > 1 {
		trunk = "  "
		if index < count-1 {
			trunk = "│ "
		}
	}
	dim := l.styleProvider.GetThemeColor("dim")
	checkColor, crossColor := dim, dim
	if stats.ToolsSucceeded > 0 {
		checkColor = l.styleProvider.GetThemeColor("success")
	}
	if stats.ToolsFailed > 0 {
		crossColor = l.styleProvider.GetThemeColor("error")
	}
	line := fmt.Sprintf("%s└ %d %s %d %s", trunk,
		stats.ToolsSucceeded, l.styleProvider.RenderWithColor(icons.CheckMark, checkColor),
		stats.ToolsFailed, l.styleProvider.RenderWithColor(icons.CrossMark, crossColor))
	if tokens := statsTokens(stats); tokens != "" {
		line += " " + l.styleProvider.RenderWithColor(tokens, dim)
	}
	return l.padToWidth(line, width)
}

// statsTokens is a run's token figures: the input and output it burned, then
// the cached slice (C.) in the status bar's notation, dropped while the run
// reported no cache hits. It is empty while the run reported no usage.
func statsTokens(stats scheddomain.SubagentRunStats) string {
	if stats.InputTokens+stats.OutputTokens == 0 {
		return ""
	}
	text := "· " + compactCount(stats.InputTokens+stats.OutputTokens) + " tokens"
	if stats.CachedTokens > 0 {
		text += " C." + compactCount(stats.CachedTokens)
	}
	return text
}

// compactCount shortens a count for the narrow list: 950, 1.2k, 312k, 1.5M.
func compactCount(n int) string {
	switch {
	case n >= 1_000_000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1_000_000), ".0") + "M"
	case n >= 1_000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1_000), ".0") + "k"
	}
	return fmt.Sprintf("%d", n)
}

// rowView draws one entry: box-drawing connector, padded label, kind tag, then
// either the running elapsed counter or the outcome icon and total duration.
// Running rows leave the outcome cell blank so finished rows cannot shift the
// shared duration column.
func (l *SubagentList) rowView(row subagentRow, index, count int, widths rowWidths, selected bool) string {
	connector := "  "
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
	if selected {
		connector = l.styleProvider.RenderWithColor(selectedRowMarker, l.styleProvider.GetThemeColor("accent"))
	}
	label := formatting.PadText(rowLabel(row), widths.label) + " " +
		l.styleProvider.RenderWithColor(formatting.PadText(row.kind, widths.kind), l.styleProvider.GetThemeColor("accent"))
	dim := l.styleProvider.GetThemeColor("dim")
	elapsed := formatDuration(row.elapsed)
	elapsedCol := strings.Repeat(" ", widths.duration-l.styleProvider.GetWidth(elapsed)) +
		l.styleProvider.RenderWithColor(elapsed, dim)
	if row.running {
		return fmt.Sprintf("%s%s %s", connector, label, strings.Repeat(" ", widths.state+1)+elapsedCol)
	}
	outcome := l.styleProvider.RenderWithColor(icons.CheckMark, l.styleProvider.GetThemeColor("success"))
	if row.failed {
		outcome = l.styleProvider.RenderWithColor(icons.CrossMark, l.styleProvider.GetThemeColor("error"))
	}
	return fmt.Sprintf("%s%s %s %s", connector, label, outcome, elapsedCol)
}
