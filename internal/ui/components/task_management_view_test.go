package components

import (
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
	ui "github.com/inference-gateway/cli/internal/ui"
	schedmocks "github.com/inference-gateway/cli/tests/mocks/scheduler"
	"strings"
	"testing"
	"time"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
	uimocks "github.com/inference-gateway/cli/tests/mocks/ui"
)

func createMockStyleProviderForTasks() *styles.Provider {
	fakeTheme := &uimocks.FakeTheme{}
	fakeThemeService := &uimocks.FakeThemeService{}
	fakeThemeService.GetCurrentThemeReturns(fakeTheme)
	return styles.NewProvider(fakeThemeService)
}

func loadTaskRows(t *testing.T, tm *TaskManagerImpl) ui.TasksLoadedEvent {
	t.Helper()
	msg := tm.loadTasksCmd()()
	ev, ok := msg.(ui.TasksLoadedEvent)
	if !ok {
		t.Fatalf("loadTasksCmd returned %T, want TasksLoadedEvent", msg)
	}
	if ev.Error != nil {
		t.Fatalf("loadTasksCmd error: %v", ev.Error)
	}
	return ev
}

func countKinds(rows []any) map[scheddomain.JobKind]int {
	out := map[scheddomain.JobKind]int{}
	for _, r := range rows {
		if ti, ok := r.(TaskInfo); ok {
			out[normalizeKind(ti.Kind)]++
		}
	}
	return out
}

// TestLoadTasksCmd_SkipsA2AFromSnapshotAndSplitsByStatus: shell/subagent rows
// come from the supervisor snapshot (running → active, terminal → completed),
// while A2A jobs in the snapshot are dropped so they aren't double-listed
// alongside the A2A poller's own rows.
func TestLoadTasksCmd_SkipsA2AFromSnapshotAndSplitsByStatus(t *testing.T) {
	bg := &schedmocks.FakeBackgroundTaskService{}
	bg.GetBackgroundTasksReturns([]agentdomain.TaskPollingState{
		{TaskID: "a2a-1", AgentURL: "http://agent", StartedAt: time.Now()},
	})
	done := time.Now()
	reg := &schedmocks.FakeBackgroundTaskRegistry{}
	reg.SnapshotReturns([]scheddomain.TrackedJob{
		// A2A in the snapshot must be skipped (already listed via the poller).
		{Meta: scheddomain.JobMeta{ID: "a2a-1", Kind: scheddomain.JobKindA2A}, Status: scheddomain.JobRunning},
		{Meta: scheddomain.JobMeta{ID: "shell-1", Kind: scheddomain.JobKindShell, Label: "shell-1", Detail: "npm run build", StartedAt: time.Now()}, Status: scheddomain.JobRunning},
		{Meta: scheddomain.JobMeta{ID: "sub-1", Kind: scheddomain.JobKindSubagent, Label: "refactor", Detail: "headless", StartedAt: time.Now()}, Status: scheddomain.JobCompleted, CompletedAt: &done},
	})

	tm := &TaskManagerImpl{backgroundTaskService: bg, backgroundJobRegistry: reg, currentView: TaskViewAll}
	ev := loadTaskRows(t, tm)

	active := countKinds(ev.ActiveTasks)
	if active[scheddomain.JobKindA2A] != 1 {
		t.Fatalf("active A2A rows = %d, want 1 (no snapshot double-list)", active[scheddomain.JobKindA2A])
	}
	if active[scheddomain.JobKindShell] != 1 {
		t.Fatalf("active shell rows = %d, want 1", active[scheddomain.JobKindShell])
	}
	if len(ev.ActiveTasks) != 2 {
		t.Fatalf("active rows = %d, want 2", len(ev.ActiveTasks))
	}

	completed := countKinds(ev.CompletedTasks)
	if completed[scheddomain.JobKindSubagent] != 1 || len(ev.CompletedTasks) != 1 {
		t.Fatalf("completed rows = %v (len %d), want exactly 1 subagent", completed, len(ev.CompletedTasks))
	}
}

// TestApplyFilters_GroupsByKind: the All view orders rows into contiguous
// per-kind groups (A2A, then shells, then subagents), with running rows before
// completed rows within a kind.
func TestApplyFilters_GroupsByKind(t *testing.T) {
	tm := &TaskManagerImpl{currentView: TaskViewAll}
	tm.activeTasks = []TaskInfo{
		{TaskPollingState: agentdomain.TaskPollingState{TaskID: "sub-run"}, Kind: scheddomain.JobKindSubagent, Status: "Running"},
		{TaskPollingState: agentdomain.TaskPollingState{TaskID: "a2a-run"}, Kind: scheddomain.JobKindA2A, Status: "Running"},
		{TaskPollingState: agentdomain.TaskPollingState{TaskID: "shell-run"}, Kind: scheddomain.JobKindShell, Status: "Running"},
	}
	tm.completedTasks = []TaskInfo{
		{TaskPollingState: agentdomain.TaskPollingState{TaskID: "shell-done"}, Kind: scheddomain.JobKindShell, Status: "Completed"},
	}

	tm.applyFilters()

	wantOrder := []string{"a2a-run", "shell-run", "shell-done", "sub-run"}
	if len(tm.filteredTasks) != len(wantOrder) {
		t.Fatalf("filtered len = %d, want %d", len(tm.filteredTasks), len(wantOrder))
	}
	for i, id := range wantOrder {
		if tm.filteredTasks[i].TaskID != id {
			t.Fatalf("row %d = %q, want %q (kinds must group A2A→shell→subagent, running before done)", i, tm.filteredTasks[i].TaskID, id)
		}
	}
}

// TestApplyFilters_CompletedIncludesFailed: the Completed tab shows both
// Completed and Failed terminal rows, but not Canceled.
func TestApplyFilters_CompletedIncludesFailed(t *testing.T) {
	tm := &TaskManagerImpl{currentView: TaskViewCompleted}
	tm.completedTasks = []TaskInfo{
		{TaskPollingState: agentdomain.TaskPollingState{TaskID: "shell-ok"}, Kind: scheddomain.JobKindShell, Status: "Completed"},
		{TaskPollingState: agentdomain.TaskPollingState{TaskID: "shell-bad"}, Kind: scheddomain.JobKindShell, Status: "Failed"},
		{TaskPollingState: agentdomain.TaskPollingState{TaskID: "a2a-cancel"}, Kind: scheddomain.JobKindA2A, Status: "Canceled"},
	}

	tm.applyFilters()

	ids := map[string]bool{}
	for _, r := range tm.filteredTasks {
		ids[r.TaskID] = true
	}
	if !ids["shell-ok"] || !ids["shell-bad"] {
		t.Fatalf("Completed tab should include Completed and Failed rows, got %v", ids)
	}
	if ids["a2a-cancel"] {
		t.Fatalf("Completed tab must not include Canceled rows")
	}
}

// TestApplyFilters_CanceledTabMatchesA2A guards the Canceled-spelling fix: the
// tab filter and mapTaskStatus must agree on "Canceled".
func TestApplyFilters_CanceledTabMatchesA2A(t *testing.T) {
	tm := &TaskManagerImpl{currentView: TaskViewCanceled}
	tm.completedTasks = []TaskInfo{
		{TaskPollingState: agentdomain.TaskPollingState{TaskID: "a2a-cancel"}, Kind: scheddomain.JobKindA2A, Status: "Canceled"},
		{TaskPollingState: agentdomain.TaskPollingState{TaskID: "shell-fail"}, Kind: scheddomain.JobKindShell, Status: "Failed"},
	}

	tm.applyFilters()

	if len(tm.filteredTasks) != 1 || tm.filteredTasks[0].TaskID != "a2a-cancel" {
		t.Fatalf("Canceled tab = %+v, want only a2a-cancel", tm.filteredTasks)
	}
}

func TestJobToTaskInfo(t *testing.T) {
	start := time.Now().Add(-30 * time.Second)
	done := start.Add(10 * time.Second)
	row := jobToTaskInfo(scheddomain.TrackedJob{
		Meta:        scheddomain.JobMeta{ID: "shell-1", Kind: scheddomain.JobKindShell, Label: "shell-1", Detail: "go test ./...", StartedAt: start},
		Status:      scheddomain.JobCompleted,
		CompletedAt: &done,
	})

	if row.Kind != scheddomain.JobKindShell || row.TaskID != "shell-1" || row.Label != "shell-1" || row.Detail != "go test ./..." {
		t.Fatalf("row = %+v", row)
	}
	if row.Status != "Completed" {
		t.Fatalf("status = %q, want Completed", row.Status)
	}
	if row.ElapsedTime != 10*time.Second {
		t.Fatalf("elapsed = %v, want 10s (completedAt - startedAt)", row.ElapsedTime)
	}
}

func TestJobToTaskInfo_LabelFallsBackToID(t *testing.T) {
	row := jobToTaskInfo(scheddomain.TrackedJob{
		Meta:   scheddomain.JobMeta{ID: "abc", Kind: scheddomain.JobKindSubagent, StartedAt: time.Now()},
		Status: scheddomain.JobRunning,
	})
	if row.Label != "abc" {
		t.Fatalf("Label = %q, want fallback to ID abc", row.Label)
	}
}

func TestKindRankGroupsKinds(t *testing.T) {
	if kindRank("") != kindRank(scheddomain.JobKindA2A) {
		t.Fatalf("unset kind should rank as A2A")
	}
	if kindRank(scheddomain.JobKindA2A) >= kindRank(scheddomain.JobKindShell) {
		t.Fatalf("A2A must rank before shells")
	}
	if kindRank(scheddomain.JobKindShell) >= kindRank(scheddomain.JobKindSubagent) {
		t.Fatalf("shells must rank before subagents")
	}
}

// TestWriteTaskSections_RendersPerKindTables: a mixed, kind-grouped list renders
// the three section titles and the kind-specific Detail columns.
func TestWriteTaskSections_RendersPerKindTables(t *testing.T) {
	tm := &TaskManagerImpl{
		styleProvider: createMockStyleProviderForTasks(),
		width:         120,
		currentView:   TaskViewAll,
	}
	tm.filteredTasks = []TaskInfo{
		{TaskPollingState: agentdomain.TaskPollingState{TaskID: "a2a-1", AgentURL: "http://agent"}, Kind: scheddomain.JobKindA2A, Status: "Working"},
		{TaskPollingState: agentdomain.TaskPollingState{TaskID: "shell-1"}, Kind: scheddomain.JobKindShell, Label: "shell-1", Detail: "npm run build", Status: "Running"},
		{TaskPollingState: agentdomain.TaskPollingState{TaskID: "sub-1"}, Kind: scheddomain.JobKindSubagent, Label: "refactor", Detail: "interactive", Status: "Running"},
	}

	var b strings.Builder
	tm.writeTaskSections(&b)
	out := b.String()

	for _, want := range []string{"A2A Tasks", "Background Shells", "Subagents", "npm run build", "interactive"} {
		if !strings.Contains(out, want) {
			t.Fatalf("section output missing %q:\n%s", want, out)
		}
	}
}

// TestRefreshTick_ReArmsAndDedups exercises the live-elapsed tick lifecycle: a
// running view keeps the chain alive, an emptied view stops it, a new task
// (BackgroundTasksChangedEvent) restarts a dead chain exactly once, a task
// arriving while the chain is alive never spawns a second chain, and a tick
// stamped with a superseded epoch is dropped so chains never overlap.
func TestRefreshTick_ReArmsAndDedups(t *testing.T) {
	running := []TaskInfo{{TaskPollingState: agentdomain.TaskPollingState{TaskID: "shell-1"}, Kind: scheddomain.JobKindShell, Status: "Running"}}

	tm := &TaskManagerImpl{tickLive: true, tickEpoch: 3, activeTasks: running}
	if _, cmd := tm.Update(taskRefreshTickMsg{epoch: 3}); cmd == nil {
		t.Fatal("running view: tick must re-arm (non-nil cmd)")
	}
	if !tm.tickLive {
		t.Fatal("running view: tickLive must stay true")
	}

	tm = &TaskManagerImpl{tickLive: true, tickEpoch: 3, loading: false}
	if _, cmd := tm.Update(taskRefreshTickMsg{epoch: 3}); cmd != nil {
		t.Fatal("empty view: tick must stop (nil cmd)")
	}
	if tm.tickLive {
		t.Fatal("empty view: tickLive must clear")
	}

	tm = &TaskManagerImpl{tickLive: false, tickEpoch: 3, loading: false}
	if _, cmd := tm.Update(agentdomain.BackgroundTasksChangedEvent{}); cmd == nil {
		t.Fatal("new task while dead: must re-arm (non-nil cmd)")
	}
	if !tm.tickLive || tm.tickEpoch != 4 {
		t.Fatalf("new task: want tickLive=true epoch=4, got %v/%d", tm.tickLive, tm.tickEpoch)
	}

	tm = &TaskManagerImpl{tickLive: true, tickEpoch: 4, loading: false, activeTasks: running}
	if _, cmd := tm.Update(agentdomain.BackgroundTasksChangedEvent{}); cmd == nil {
		t.Fatal("new task while alive: must still reload (non-nil cmd)")
	}
	if tm.tickEpoch != 4 {
		t.Fatalf("new task while alive: epoch must not bump, got %d", tm.tickEpoch)
	}

	tm = &TaskManagerImpl{tickLive: true, tickEpoch: 5, activeTasks: running}
	if _, cmd := tm.Update(taskRefreshTickMsg{epoch: 4}); cmd != nil {
		t.Fatal("stale epoch: tick must be dropped (nil cmd)")
	}
	if !tm.tickLive || tm.tickEpoch != 5 {
		t.Fatalf("stale epoch: state must be untouched, got %v/%d", tm.tickLive, tm.tickEpoch)
	}
}

// TestIsCancellable: active A2A rows and running shell/subagent rows can be
// cancelled; retained A2A rows and terminal rows cannot.
func TestIsCancellable(t *testing.T) {
	if !isCancellable(TaskInfo{Kind: scheddomain.JobKindA2A}) {
		t.Fatal("active A2A row must be cancellable")
	}
	if isCancellable(TaskInfo{Kind: scheddomain.JobKindA2A, TaskRef: &scheddomain.TaskInfo{}}) {
		t.Fatal("retained A2A row must not be cancellable")
	}
	if !isCancellable(TaskInfo{Kind: scheddomain.JobKindShell, Status: "Running"}) {
		t.Fatal("running shell row must be cancellable")
	}
	if isCancellable(TaskInfo{Kind: scheddomain.JobKindShell, Status: "Completed"}) {
		t.Fatal("completed shell row must not be cancellable")
	}
	if !isCancellable(TaskInfo{Kind: scheddomain.JobKindSubagent, Status: "Running"}) {
		t.Fatal("running subagent row must be cancellable")
	}
	if isCancellable(TaskInfo{Kind: scheddomain.JobKindSubagent, Status: "Failed"}) {
		t.Fatal("failed subagent row must not be cancellable")
	}
}

// TestCancelTask_DispatchesByKind: an A2A row cancels through
// CancelBackgroundTask (which also cancels the remote task); shell and subagent
// rows wind their supervised job down with WindStop.
func TestCancelTask_DispatchesByKind(t *testing.T) {
	bg := &schedmocks.FakeBackgroundTaskService{}
	reg := &schedmocks.FakeBackgroundTaskRegistry{}
	tm := &TaskManagerImpl{backgroundTaskService: bg, backgroundJobRegistry: reg}

	if err := tm.cancelTask(TaskInfo{TaskPollingState: agentdomain.TaskPollingState{TaskID: "a2a-1"}, Kind: scheddomain.JobKindA2A}); err != nil {
		t.Fatalf("cancelTask(a2a): %v", err)
	}
	if bg.CancelBackgroundTaskCallCount() != 1 || bg.CancelBackgroundTaskArgsForCall(0) != "a2a-1" {
		t.Fatalf("A2A cancel must go through CancelBackgroundTask(a2a-1)")
	}
	if reg.WindJobCallCount() != 0 {
		t.Fatalf("A2A cancel must not use WindJob")
	}

	if err := tm.cancelTask(TaskInfo{TaskPollingState: agentdomain.TaskPollingState{TaskID: "shell-1"}, Kind: scheddomain.JobKindShell, Status: "Running"}); err != nil {
		t.Fatalf("cancelTask(shell): %v", err)
	}
	id, sig := reg.WindJobArgsForCall(0)
	if reg.WindJobCallCount() != 1 || id != "shell-1" || sig != scheddomain.WindStop {
		t.Fatalf("WindJob(%q, %v) x%d, want one WindJob(shell-1, WindStop)", id, sig, reg.WindJobCallCount())
	}

	if err := tm.cancelTask(TaskInfo{TaskPollingState: agentdomain.TaskPollingState{TaskID: "sub-1"}, Kind: scheddomain.JobKindSubagent, Status: "Running"}); err != nil {
		t.Fatalf("cancelTask(subagent): %v", err)
	}
	if reg.WindJobCallCount() != 2 {
		t.Fatalf("subagent cancel must wind the supervised job")
	}
}
