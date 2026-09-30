package components

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
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

func TestSubagentListCapsRowsAndShowsOverflow(t *testing.T) {
	now := time.Now()
	jobs := make([]scheddomain.TrackedJob, 0, 7)
	for i := range 7 {
		jobs = append(jobs, subagentJob(fmt.Sprintf("w%d", i), scheddomain.JobRunning, now.Add(-time.Duration(7-i)*time.Second), nil))
	}

	got := plain(newList(listOpts{jobs: jobs, linger: 5, indicator: true}).Render())
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
}

func TestSubagentListRightAlignsWithinWidth(t *testing.T) {
	jobs := []scheddomain.TrackedJob{
		subagentJob("reviewer", scheddomain.JobRunning, time.Now().Add(-12*time.Second), nil),
		subagentJob("tester", scheddomain.JobRunning, time.Now().Add(-9*time.Second), nil),
	}

	list := newList(listOpts{jobs: jobs, linger: 5, indicator: true})
	if _, cmd := list.Update(tea.WindowSizeMsg{Width: 40, Height: 10}); cmd != nil {
		t.Error("expected no command from a plain resize")
	}
	for i, line := range strings.Split(plain(list.Render()), "\n") {
		wantWidth := 40 - versionRightInset
		if visibleWidth(line) != wantWidth {
			t.Errorf("row %d width = %d, want exactly %d (version column edge): %q", i, visibleWidth(line), wantWidth, line)
		}
		if !strings.HasPrefix(line, " ") {
			t.Errorf("row %d should be right-aligned across the width: %q", i, line)
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

// TestSubagentListFitsLabelColumnToWidestName: long names used to be cut at 12
// columns, so an agent could not be told apart from its neighbours.
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

	capped := strings.Repeat("x", subagentLabelCap+10)
	got = plain(newList(listOpts{
		jobs:      []scheddomain.TrackedJob{subagentJob(capped, scheddomain.JobRunning, now.Add(-2*time.Second), nil)},
		linger:    5,
		indicator: true,
	}).Render())
	if strings.Contains(got, capped) {
		t.Errorf("expected the label column to stay capped at %d columns, got %q", subagentLabelCap, got)
	}
	if !strings.Contains(got, "...") {
		t.Errorf("expected an over-long label to be truncated with an ellipsis, got %q", got)
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
