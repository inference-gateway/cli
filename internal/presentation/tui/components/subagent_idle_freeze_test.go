package components

import (
	"context"
	"strings"
	"testing"
	"time"

	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"
	schedmocks "github.com/inference-gateway/cli/tests/mocks/scheduler"

	tea "charm.land/bubbletea/v2"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	scheduler "github.com/inference-gateway/cli/internal/scheduler"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
	jobs "github.com/inference-gateway/cli/internal/scheduler/jobs"
)

// An idle keep-alive headless subagent (turn done, waiting on stdin for a
// follow-up) reads as completed at its turn end, renders a check mark over a
// frozen elapsed, lingers out with its refresh tick and wakes back up when the
// parent sends a follow-up.
func TestIdleSubagentJobFreezesElapsedAndLingersOut(t *testing.T) {
	sup := jobs.NewSupervisor(&convmocks.FakeMessageQueue{}, &convmocks.FakeConversationRepository{}, nil)
	started := make(chan struct{})
	finish := make(chan struct{})
	keeper := &schedmocks.FakeJobIdleReporter{}
	keeper.MetaReturns(scheddomain.JobMeta{ID: "keeper", Kind: scheddomain.JobKindSubagent, Label: "reviewer", Detail: "headless", StartedAt: time.Now().Add(-90 * time.Second), HoldsSession: true})
	keeper.RunStub = func(context.Context, func(scheddomain.JobSignal)) agentdomain.ToolExecutionResult {
		close(started)
		<-finish
		return agentdomain.ToolExecutionResult{Success: true}
	}
	sup.Submit(keeper)
	<-started
	keeper.IdleReturns(true)
	turnEnd := time.Now()
	keeper.IdleSinceReturns(turnEnd)

	snap := sup.Snapshot()[0]
	if snap.Status != scheddomain.JobCompleted || snap.CompletedAt == nil || !snap.CompletedAt.Equal(turnEnd) {
		t.Fatalf("an idle job must read completed at its turn end, got status=%s completedAt=%v", snap.Status, snap.CompletedAt)
	}

	list := NewSubagentList(createMockStyleProviderForStatus())
	cfg := &config.Config{}
	cfg.Chat.StatusBar.Indicators.Subagents = true
	cfg.Chat.StatusBar.SubagentLingerSeconds = 1
	list.SetConfig(cfg)
	list.SetRegistry(scheduler.NewBackgroundTaskRegistry(0, sup))
	list.Update(tea.WindowSizeMsg{Width: 80, Height: 10})

	if list.maybeRefreshTick() == nil {
		t.Fatal("a visible row must arm the refresh tick")
	}
	first := list.Render()
	time.Sleep(500 * time.Millisecond)
	if frozen := list.Render(); frozen != first {
		t.Errorf("the completed row changed between renders (elapsed still ticking): %q vs %q", strings.Split(first, "\n")[0], strings.Split(frozen, "\n")[0])
	}
	time.Sleep(700 * time.Millisecond)
	if gone := list.Render(); gone != "" {
		t.Errorf("the completed row must drop after the linger window, still %q", strings.Split(gone, "\n")[0])
	}
	if list.maybeRefreshTick() != nil {
		t.Error("the refresh tick chain must die once no row is visible")
	}

	keeper.IdleReturns(false)
	keeper.IdleSinceReturns(time.Time{})
	if busy := sup.Snapshot()[0]; busy.Status != scheddomain.JobRunning || busy.CompletedAt != nil {
		t.Errorf("a follow-up must read as running with no completion time, got status=%s completedAt=%v", busy.Status, busy.CompletedAt)
	}

	close(finish)
	sup.Stop()
}
