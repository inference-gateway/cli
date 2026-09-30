package components

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
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

func TestSubagentListRenderLifecycle(t *testing.T) {
	runningStarted := time.Now().Add(-2 * time.Second)
	longAgoStarted := time.Now().Add(-42 * time.Second)
	recentlyDone := time.Now().Add(-2 * time.Second)
	failedStarted := time.Now().Add(-9 * time.Second)
	failedDone := time.Now().Add(-time.Second)
	shellJob := subagentJob("reviewer", scheddomain.JobRunning, runningStarted, nil)
	shellJob.Meta.Kind = scheddomain.JobKindShell

	tests := []struct {
		name        string
		opts        listOpts
		wantEmpty   bool
		wantStrings []string
	}{
		{
			name:        "running row shows its label and live elapsed",
			opts:        listOpts{jobs: []scheddomain.TrackedJob{subagentJob("reviewer", scheddomain.JobRunning, runningStarted, nil)}, linger: 5, indicator: true},
			wantStrings: []string{"reviewer", "2.0s"},
		},
		{
			name:        "finished row lingers with final state and total duration",
			opts:        listOpts{jobs: []scheddomain.TrackedJob{subagentJob("reviewer", scheddomain.JobCompleted, longAgoStarted, &recentlyDone)}, linger: 5, indicator: true},
			wantStrings: []string{"reviewer", "done", "40.0s"},
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
			name:        "failed row shows its final state",
			opts:        listOpts{jobs: []scheddomain.TrackedJob{subagentJob("tester", scheddomain.JobFailed, failedStarted, &failedDone)}, linger: 5, indicator: true},
			wantStrings: []string{"tester", "failed", "8.0s"},
		},
		{
			name:      "shell and a2a jobs never render in the list",
			opts:      listOpts{jobs: []scheddomain.TrackedJob{shellJob}, linger: 5, indicator: true},
			wantEmpty: true,
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
		if visibleWidth(line) != 40 {
			t.Errorf("row %d width = %d, want exactly 40: %q", i, visibleWidth(line), line)
		}
		if !strings.HasPrefix(line, " ") {
			t.Errorf("row %d should be right-aligned across the width: %q", i, line)
		}
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
