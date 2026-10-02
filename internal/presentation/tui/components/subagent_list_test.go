package components

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tuimocks "github.com/inference-gateway/cli/tests/mocks/tui"

	tea "charm.land/bubbletea/v2"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	styles "github.com/inference-gateway/cli/internal/presentation/tui/styles"
	icons "github.com/inference-gateway/cli/internal/presentation/tui/styles/icons"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
)

// fakeRegistry satisfies the whole BackgroundTaskRegistry surface the list
// depends on; the embedded nil interfaces stand in for the projections the
// list never calls.
type fakeRegistry struct {
	scheddomain.ShellTracker
	scheddomain.SubagentTracker
	jobs []scheddomain.TrackedJob
}

func (f *fakeRegistry) HasPending() bool { return false }

func (f *fakeRegistry) Submit(_ scheddomain.BackgroundJob) {}

func (f *fakeRegistry) Snapshot() []scheddomain.TrackedJob { return f.jobs }

func (f *fakeRegistry) CountRunningJobs(_ scheddomain.JobKind) int { return 0 }

func (f *fakeRegistry) IsJobRunning(_ string) bool { return false }

func (f *fakeRegistry) WindJob(_ string, _ scheddomain.WindSignal) error { return nil }

// subagentJob builds one sub-agent entry for a registry snapshot.
func subagentJob(label string, status scheddomain.JobStatus, started time.Time, completed *time.Time) scheddomain.TrackedJob {
	return scheddomain.TrackedJob{
		Meta:        scheddomain.JobMeta{ID: "job-" + label, Kind: scheddomain.JobKindSubagent, Label: label, StartedAt: started},
		Status:      status,
		CompletedAt: completed,
	}
}

// a2aJob builds one A2A task entry, labelled by task ID like the real job.
func a2aJob(taskID, agentURL string, status scheddomain.JobStatus, started time.Time, completed *time.Time) scheddomain.TrackedJob {
	return scheddomain.TrackedJob{
		Meta:        scheddomain.JobMeta{ID: taskID, Kind: scheddomain.JobKindA2A, Label: taskID, Detail: agentURL, StartedAt: started},
		Status:      status,
		CompletedAt: completed,
	}
}

// shellJob builds one background shell entry, labelled by shell ID like the real job.
func shellJob(shellID, command string, status scheddomain.JobStatus, started time.Time) scheddomain.TrackedJob {
	return scheddomain.TrackedJob{
		Meta:   scheddomain.JobMeta{ID: shellID, Kind: scheddomain.JobKindShell, Label: shellID, Detail: command, StartedAt: started},
		Status: status,
	}
}

// newList assembles a SubagentList wired to the given snapshot rows.
func newList(opts listOpts) *SubagentList {
	list := NewSubagentList(createMockStyleProviderForStatus())
	cfg := &config.Config{}
	cfg.Chat.StatusBar.Indicators.Subagents = opts.indicator
	cfg.Chat.StatusBar.SubagentLingerSeconds = opts.linger
	list.SetConfig(cfg)
	list.SetRegistry(&fakeRegistry{jobs: opts.jobs})
	return list
}

type listOpts struct {
	jobs      []scheddomain.TrackedJob
	linger    int
	indicator bool
}

// TestSubagentListTagsEveryJobKind: one list carries every background job,
// each row named readably and tagged with its kind and, for A2A, its origin.
func TestSubagentListTagsEveryJobKind(t *testing.T) {
	now := time.Now()
	recentlyDone := now.Add(-2 * time.Second)
	local := a2aJob("task-local", "http://calendar-agent:8080", scheddomain.JobRunning, now.Add(-3*time.Second), nil)
	local.Meta.Origin = "local"
	external := a2aJob("task-external", "https://weather.example.com/a2a", scheddomain.JobCompleted, now.Add(-42*time.Second), &recentlyDone)
	external.Meta.Origin = "external"
	recording := subagentJob("demo.mp4", scheddomain.JobRunning, now.Add(-time.Second), nil)
	recording.Meta.Kind = scheddomain.JobKindRecording

	list := newList(listOpts{linger: 5, indicator: true, jobs: []scheddomain.TrackedJob{
		recording,
		shellJob("shell-1", "npm run\n  build", scheddomain.JobRunning, now.Add(-2*time.Second)),
		local,
		a2aJob("task-without-url", "", scheddomain.JobRunning, now.Add(-4*time.Second), nil),
		subagentJob("reviewer", scheddomain.JobRunning, now.Add(-5*time.Second), nil),
		external,
	}})
	list.Update(tea.WindowSizeMsg{Width: 120, Height: 10})
	rows := list.snapshotRows()

	want := []subagentRow{
		{label: "demo.mp4", kind: "recording"},
		{label: "npm run\n  build", kind: "shell"},
		{label: "calendar-agent:8080", kind: "a2a local"},
		{label: "task-without-url", kind: "a2a"},
		{label: "reviewer", kind: "subagent"},
		{label: "weather.example.com", kind: "a2a external"},
	}
	if len(rows) != len(want) {
		t.Fatalf("expected %d rows, got %+v", len(want), rows)
	}
	for i, w := range want {
		if rows[i].label != w.label || rows[i].kind != w.kind {
			t.Errorf("row %d = {label: %q, kind: %q}, want {label: %q, kind: %q}", i, rows[i].label, rows[i].kind, w.label, w.kind)
		}
	}

	got := plain(list.Render())
	if !strings.Contains(got, "npm run build") {
		t.Errorf("expected a multi-line shell command on one row, got %q", got)
	}
	if !strings.Contains(got, "+1 more") {
		t.Errorf("expected the sixth job behind the overflow row, got %q", got)
	}

	list.config.Chat.StatusBar.SubagentLingerSeconds = 1
	if rows := list.snapshotRows(); len(rows) != len(want)-1 {
		t.Errorf("a finished A2A row must drop once the linger window passed, got %+v", rows)
	}
}

func TestSubagentListRenderLifecycle(t *testing.T) {
	runningStarted := time.Now().Add(-2 * time.Second)
	longAgoStarted := time.Now().Add(-42 * time.Second)
	recentlyDone := time.Now().Add(-2 * time.Second)
	failedStarted := time.Now().Add(-9 * time.Second)
	failedDone := time.Now().Add(-time.Second)

	tests := []struct {
		name        string
		opts        listOpts
		wantEmpty   bool
		wantStrings []string
	}{
		{
			name:        "running row shows its label and live elapsed",
			opts:        listOpts{jobs: []scheddomain.TrackedJob{subagentJob("reviewer", scheddomain.JobRunning, runningStarted, nil)}, linger: 5, indicator: true},
			wantStrings: []string{"reviewer", "subagent", "2.0s"},
		},
		{
			name:        "finished row lingers with a checkmark and total duration",
			opts:        listOpts{jobs: []scheddomain.TrackedJob{subagentJob("reviewer", scheddomain.JobCompleted, longAgoStarted, &recentlyDone)}, linger: 5, indicator: true},
			wantStrings: []string{"reviewer", icons.CheckMark, "40.0s"},
		},
		{
			name:      "finished row drops once the linger window passed",
			opts:      listOpts{jobs: []scheddomain.TrackedJob{subagentJob("reviewer", scheddomain.JobCompleted, longAgoStarted, &recentlyDone)}, linger: 1, indicator: true},
			wantEmpty: true,
		},
		{
			name:      "zero linger drops finished rows immediately",
			opts:      listOpts{jobs: []scheddomain.TrackedJob{subagentJob("reviewer", scheddomain.JobCompleted, longAgoStarted, &recentlyDone)}, linger: 0, indicator: true},
			wantEmpty: true,
		},
		{
			name:        "failed row shows a cross",
			opts:        listOpts{jobs: []scheddomain.TrackedJob{subagentJob("tester", scheddomain.JobFailed, failedStarted, &failedDone)}, linger: 5, indicator: true},
			wantStrings: []string{"tester", icons.CrossMark, "8.0s"},
		},
		{
			name:      "renders nothing when no sub-agents run or linger",
			opts:      listOpts{linger: 5, indicator: true},
			wantEmpty: true,
		},
		{
			name:      "the subagents indicator toggle hides the whole list",
			opts:      listOpts{jobs: []scheddomain.TrackedJob{subagentJob("reviewer", scheddomain.JobRunning, runningStarted, nil)}, linger: 5, indicator: false},
			wantEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := plain(newList(tt.opts).Render())
			if tt.wantEmpty {
				if got != "" {
					t.Errorf("expected an empty render, got %q", got)
				}
				return
			}
			for _, want := range tt.wantStrings {
				if !strings.Contains(got, want) {
					t.Errorf("expected the render to contain %q, got %q", want, got)
				}
			}
			if lines := strings.Split(got, "\n"); len(lines) != 1 {
				t.Errorf("expected exactly one row for one job, got %q", got)
			}
		})
	}
}

//nolint:gocyclo,cyclop
func TestSubagentListCapsRowsAndShowsOverflow(t *testing.T) {
	now := time.Now()
	jobs := make([]scheddomain.TrackedJob, 0, 7)
	for i := range 7 {
		jobs = append(jobs, subagentJob(fmt.Sprintf("w%d", i), scheddomain.JobRunning, now.Add(-time.Duration(7-i)*time.Second), nil))
	}

	list := newList(listOpts{jobs: jobs, linger: 5, indicator: true})
	got := plain(list.Render())
	lines := strings.Split(got, "\n")
	if len(lines) != maxSubagentRows+1 {
		t.Fatalf("expected %d rows (cap + overflow), got %d: %q", maxSubagentRows+1, len(lines), got)
	}
	if !strings.Contains(got, "+2 more") {
		t.Errorf("expected a +2 more overflow row, got %q", got)
	}
	if !strings.Contains(got, "w6") {
		t.Errorf("expected the newest row w6 on top, got %q", got)
	}
	for _, hidden := range []string{"w0", "w1"} {
		if strings.Contains(got, hidden) {
			t.Errorf("expected the oldest row %q to be cut by the cap, got %q", hidden, got)
		}
	}
	last := lines[len(lines)-1]
	if rowWidth := visibleWidth(lines[0]); visibleWidth(last) != rowWidth {
		t.Errorf("expected the overflow row to match the row width %d, got %d: %q", rowWidth, visibleWidth(last), last)
	}

	if !list.Focus() {
		t.Fatal("expected the list to take focus while rows are visible")
	}
	for range maxSubagentRows {
		if !list.SelectNext() {
			t.Fatal("expected the selection to keep moving onto the rows behind the overflow marker")
		}
	}
	got = plain(list.Render())
	lines = strings.Split(got, "\n")
	if len(lines) != maxSubagentRows+3 {
		t.Fatalf("expected %d lines (window, both markers, hint), got %d: %q", maxSubagentRows+3, len(lines), got)
	}
	if !strings.Contains(got, "1 above") {
		t.Errorf("expected the scrolled window to mark the row hidden above it, got %q", got)
	}
	if !strings.Contains(got, "+1 more") {
		t.Errorf("expected the last hidden row behind the scrolled window, got %q", got)
	}
	if !strings.Contains(got, "\u276f w1") {
		t.Errorf("expected the selection on the previously hidden row w1, got %q", got)
	}
	if strings.Contains(got, "w6") {
		t.Errorf("expected the newest row w6 scrolled out of the window, got %q", got)
	}
	if w := visibleWidth(lines[1]); visibleWidth(lines[0]) != w {
		t.Errorf("expected the above marker to match the row width %d, got %d: %q", w, visibleWidth(lines[0]), lines[0])
	}
	if job, ok := list.SelectedJob(); !ok || job.Meta.Label != "w1" {
		t.Errorf("expected the selection to open the previously hidden w1, got %q ok=%v", job.Meta.Label, ok)
	}

	if !list.SelectNext() {
		t.Fatal("expected one more step down onto the oldest row")
	}
	got = plain(list.Render())
	if !strings.Contains(got, "2 above") || strings.Contains(got, "more") {
		t.Errorf("expected the window resting on the bottom of the stack, got %q", got)
	}
	if !strings.Contains(got, "\u276f w0") {
		t.Errorf("expected the selection on the oldest row w0, got %q", got)
	}
	if list.SelectNext() {
		t.Error("the last row has no next row")
	}

	for range maxSubagentRows + 1 {
		if !list.SelectPrev() {
			t.Fatal("expected the selection to walk back up over every row")
		}
	}
	got = plain(list.Render())
	if !strings.Contains(got, "w6") || strings.Contains(got, "above") || !strings.Contains(got, "+2 more") {
		t.Errorf("expected the window scrolled back onto the newest row, got %q", got)
	}
	if list.SelectPrev() {
		t.Error("the first row has no previous row")
	}
	list.Blur()
	got = plain(list.Render())
	if strings.Contains(got, "above") || strings.Contains(got, "\u276f") || !strings.Contains(got, "w6") {
		t.Errorf("expected blur to return the window to the newest rows, got %q", got)
	}
}

func TestSubagentListStretchesWithinWidth(t *testing.T) {
	jobs := []scheddomain.TrackedJob{
		subagentJob("reviewer", scheddomain.JobRunning, time.Now().Add(-12*time.Second), nil),
		subagentJob("tester", scheddomain.JobRunning, time.Now().Add(-9*time.Second), nil),
	}

	list := newList(listOpts{jobs: jobs, linger: 5, indicator: true})
	if _, cmd := list.Update(tea.WindowSizeMsg{Width: 40, Height: 10}); cmd != nil {
		t.Error("expected no command from a plain resize")
	}
	wantWidth := 40 - versionRightInset
	for i, line := range strings.Split(plain(list.Render()), "\n") {
		if visibleWidth(line) != wantWidth {
			t.Errorf("row %d width = %d, want exactly %d (version column edge): %q", i, visibleWidth(line), wantWidth, line)
		}
		if strings.HasPrefix(line, " ") {
			t.Errorf("row %d should start on the left edge like the composer: %q", i, line)
		}
		if strings.HasSuffix(line, " ") {
			t.Errorf("row %d should end flush on the version column edge: %q", i, line)
		}
	}
}

func TestSubagentListRowsAlignInColumns(t *testing.T) {
	now := time.Now()
	auditorDone := now.Add(-time.Second)
	jobs := []scheddomain.TrackedJob{
		subagentJob("reviewer", scheddomain.JobRunning, now.Add(-2*time.Second), nil),
		subagentJob("tester", scheddomain.JobRunning, now.Add(-time.Minute-time.Second), nil),
		subagentJob("auditor", scheddomain.JobCompleted, now.Add(-2*time.Minute), &auditorDone),
	}

	list := newList(listOpts{jobs: jobs, linger: 5, indicator: true})
	if _, cmd := list.Update(tea.WindowSizeMsg{Width: 40, Height: 10}); cmd != nil {
		t.Error("expected no command from a plain resize")
	}

	lines := strings.Split(plain(list.Render()), "\n")
	if len(lines) != len(jobs) {
		t.Fatalf("expected %d rows, got %q", len(jobs), lines)
	}
	branch := strings.Index(lines[0], "┌")
	if branch < 0 || branch != strings.Index(lines[1], "│") || branch != strings.Index(lines[2], "└") {
		t.Errorf("expected tree connectors in one column, got %q", lines)
	}
	for i, line := range lines {
		if w := visibleWidth(line); w != visibleWidth(lines[0]) {
			t.Errorf("row %d width %d must equal row 0 width %d: %q", i, w, visibleWidth(lines[0]), line)
		}
		if strings.HasSuffix(line, " ") {
			t.Errorf("row %d should end flush on the version column edge: %q", i, line)
		}
	}
}

// TestSubagentListFitsLabelColumnToWidestName: short names keep their natural
// column, and a name too long for the block is cut with an ellipsis while the
// row still spans the full width.
func TestSubagentListFitsLabelColumnToWidestName(t *testing.T) {
	now := time.Now()
	name := "frontend-refactor-and-docs"

	got := plain(newList(listOpts{
		jobs:      []scheddomain.TrackedJob{subagentJob(name, scheddomain.JobRunning, now.Add(-2*time.Second), nil)},
		linger:    5,
		indicator: true,
	}).Render())
	if !strings.Contains(got, name) {
		t.Errorf("expected the whole label %q to fit the column, got %q", name, got)
	}

	long := strings.Repeat("x", 80)
	list := newList(listOpts{
		jobs:      []scheddomain.TrackedJob{subagentJob(long, scheddomain.JobRunning, now.Add(-2*time.Second), nil)},
		linger:    5,
		indicator: true,
	})
	list.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	got = plain(list.Render())
	if strings.Contains(got, long) {
		t.Errorf("expected the label to stay within the block width, got %q", got)
	}
	if !strings.Contains(got, "...") {
		t.Errorf("expected an over-long label to be truncated with an ellipsis, got %q", got)
	}
	if want := 40 - versionRightInset; visibleWidth(got) != want {
		t.Errorf("expected the row to span %d columns, got %d: %q", want, visibleWidth(got), got)
	}
}

func TestSubagentListRefreshTick(t *testing.T) {
	running := []scheddomain.TrackedJob{subagentJob("reviewer", scheddomain.JobRunning, time.Now().Add(-2*time.Second), nil)}

	list := newList(listOpts{jobs: running, linger: 5, indicator: true})
	if _, cmd := list.Update(agentdomain.BackgroundTasksChangedEvent{}); cmd == nil {
		t.Error("expected the list to arm a refresh tick while rows are visible")
	}
	if list.tickEpoch != 1 {
		t.Errorf("tickEpoch = %d, want 1 after arming one tick", list.tickEpoch)
	}
	if cmd := list.handleRefreshTick(list.tickEpoch + 1); cmd != nil {
		t.Error("expected a stale epoch to stop the tick chain")
	}
	if cmd := list.handleRefreshTick(list.tickEpoch); cmd == nil {
		t.Error("expected the current epoch to keep the chain alive while rows are visible")
	}
	if _, cmd := newList(listOpts{linger: 5, indicator: true}).Update(agentdomain.BackgroundTasksChangedEvent{}); cmd != nil {
		t.Error("expected no ticker to be armed without visible rows")
	}
}

// TestSubagentListShowsRunStatsUnderRows: a sub-agent with stats grows a child
// line, while it runs and once it finished. One without stats does not.
func TestSubagentListShowsRunStatsUnderRows(t *testing.T) {
	now := time.Now()
	done := now.Add(-time.Second)
	finished := subagentJob("reviewer", scheddomain.JobCompleted, now.Add(-41*time.Second), &done)
	finished.Stats = &scheddomain.SubagentRunStats{ToolsSucceeded: 12, ToolsFailed: 1, InputTokens: 60448, OutputTokens: 745}
	running := subagentJob("tester", scheddomain.JobRunning, now.Add(-2*time.Second), nil)
	running.Stats = &scheddomain.SubagentRunStats{ToolsSucceeded: 2, InputTokens: 900, OutputTokens: 50, CachedTokens: 800}
	silent := subagentJob("pane", scheddomain.JobRunning, now.Add(-time.Second), nil)

	list := newList(listOpts{jobs: []scheddomain.TrackedJob{silent, running, finished}, linger: 5, indicator: true})
	list.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	lines := strings.Split(plain(list.Render()), "\n")
	if len(lines) != 5 {
		t.Fatalf("expected three rows and two stats lines, got %q", lines)
	}
	if !strings.Contains(lines[1], "tester") || !strings.Contains(lines[3], "reviewer") {
		t.Fatalf("expected the running row second and the finished row fourth, got %q", lines)
	}
	wantLive := "└ 2 " + icons.CheckMark + " 0 " + icons.CrossMark + " · 950 tokens C.800"
	if !strings.Contains(lines[2], wantLive) {
		t.Errorf("expected the live stats line %q under the running row, got %q", wantLive, lines[2])
	}
	want := "└ 12 " + icons.CheckMark + " 1 " + icons.CrossMark + " · 61.2k tokens"
	if !strings.Contains(lines[4], want) {
		t.Errorf("expected the stats line %q under the finished row, got %q", want, lines[4])
	}
	if strings.Contains(lines[4], "C.") {
		t.Errorf("expected no cached figure on a run that reported none, got %q", lines[4])
	}
	for i, line := range lines {
		if visibleWidth(line) != visibleWidth(lines[0]) {
			t.Errorf("line %d width %d must equal line 0 width %d: %q", i, visibleWidth(line), visibleWidth(lines[0]), line)
		}
	}
}

func TestCompactCount(t *testing.T) {
	for n, want := range map[int]string{
		0:         "0",
		950:       "950",
		1200:      "1.2k",
		20000:     "20k",
		61193:     "61.2k",
		312_000:   "312k",
		1_500_000: "1.5M",
	} {
		if got := compactCount(n); got != want {
			t.Errorf("compactCount(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestSubagentListSelection(t *testing.T) {
	now := time.Now()
	done := now.Add(-time.Minute)
	list := newList(listOpts{linger: 5, indicator: true, jobs: []scheddomain.TrackedJob{
		subagentJob("newest", scheddomain.JobRunning, now.Add(-time.Second), nil),
		subagentJob("older", scheddomain.JobRunning, now.Add(-2*time.Second), nil),
		subagentJob("finished", scheddomain.JobCompleted, now.Add(-2*time.Minute), &done),
	}})

	if _, ok := list.SelectedJob(); ok {
		t.Fatal("an unfocused list has no selection")
	}
	if !list.Focus() {
		t.Fatal("expected the list to take focus while rows are visible")
	}
	if job, _ := list.SelectedJob(); job.Meta.Label != "newest" {
		t.Fatalf("focus should select the newest row, got %q", job.Meta.Label)
	}
	if list.SelectPrev() {
		t.Error("the first row has no previous row")
	}
	if !list.SelectNext() || list.SelectNext() {
		t.Error("expected one step down to the last running row and no further")
	}
	lines := strings.Split(plain(list.Render()), "\n")
	if len(lines) != 3 || !strings.Contains(lines[1], "❯ older") || !strings.Contains(lines[2], "enter view") {
		t.Fatalf("expected the marker on the selected row and a key hint below, got %q", lines)
	}

	list.SetViewing("job-finished")
	if got := plain(list.Render()); !strings.Contains(got, "finished") || !strings.Contains(got, "esc back to chat") {
		t.Errorf("a viewed row must stay past its linger window, got %q", got)
	}
	list.Blur()
	if got := plain(list.Render()); strings.Contains(got, "finished") || strings.Contains(got, "❯") {
		t.Errorf("blur should drop the marker and unpin the viewed row, got %q", got)
	}
}

// TestSubagentListStatsIconsDimAtZero: the stats line colors a count's icon only
// once the count is positive, so a zero tick or cross cannot read as an outcome.
func TestSubagentListStatsIconsDimAtZero(t *testing.T) {
	fakeTheme := &tuimocks.FakeTheme{}
	fakeTheme.GetDimColorReturns("#888888")
	fakeTheme.GetSuccessColorReturns("#9ece6a")
	fakeTheme.GetErrorColorReturns("#f7768e")
	themeService := &tuimocks.FakeThemeService{}
	themeService.GetCurrentThemeReturns(fakeTheme)
	provider := styles.NewProvider(themeService)
	list := NewSubagentList(provider)

	dim := provider.GetThemeColor("dim")
	success := provider.GetThemeColor("success")
	failure := provider.GetThemeColor("error")
	tick := func(color string) string { return provider.RenderWithColor(icons.CheckMark, color) }
	cross := func(color string) string { return provider.RenderWithColor(icons.CrossMark, color) }
	if tick(dim) == tick(success) || cross(dim) == cross(failure) {
		t.Skip("this terminal renders no colors, so the assertion would be vacuous")
	}

	tests := []struct {
		name       string
		stats      scheddomain.SubagentRunStats
		tickColor  string
		crossColor string
	}{
		{name: "both counts zero stay dim", tickColor: dim, crossColor: dim},
		{name: "both counts positive take the status color", stats: scheddomain.SubagentRunStats{ToolsSucceeded: 3, ToolsFailed: 1}, tickColor: success, crossColor: failure},
		{name: "zero failures dim the cross", stats: scheddomain.SubagentRunStats{ToolsSucceeded: 3}, tickColor: success, crossColor: dim},
		{name: "zero successes dim the tick", stats: scheddomain.SubagentRunStats{ToolsFailed: 2}, tickColor: dim, crossColor: failure},
	}

	const width = 60
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := fmt.Sprintf("└ %d %s %d %s %s", tt.stats.ToolsSucceeded, tick(tt.tickColor),
				tt.stats.ToolsFailed, cross(tt.crossColor), provider.RenderWithColor("· 0 tokens", dim))
			want += strings.Repeat(" ", width-provider.GetWidth(want))
			if got := list.statsView(tt.stats, 0, 1, width); got != want {
				t.Errorf("statsView() = %q, want %q", got, want)
			}
		})
	}
}
