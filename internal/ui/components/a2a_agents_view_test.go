package components

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"
	uimocks "github.com/inference-gateway/cli/tests/mocks/ui"

	domain "github.com/inference-gateway/cli/internal/domain"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

// newA2AAgentsViewForTest builds an agents view backed by a fake state
// manager reporting the given readiness.
func newA2AAgentsViewForTest(readiness *domain.AgentReadinessState) (*A2AAgentsViewImpl, *domainmocks.FakeStateManager) {
	fakeTheme := &uimocks.FakeTheme{}
	fakeTheme.GetAccentColorReturns("#ff9e64")
	fakeTheme.GetDimColorReturns("#888888")
	fakeTheme.GetStatusColorReturns("#e0af68")
	fakeTheme.GetErrorColorReturns("#f7768e")
	themeService := &domainmocks.FakeThemeService{}
	themeService.GetCurrentThemeReturns(fakeTheme)

	stateManager := &domainmocks.FakeStateManager{}
	stateManager.GetAgentReadinessReturns(readiness)

	view := NewA2AAgentsView(stateManager, styles.NewProvider(themeService))
	return view, stateManager
}

func TestA2AAgentsView_ItemsReflectReadiness(t *testing.T) {
	view, _ := newA2AAgentsViewForTest(&domain.AgentReadinessState{
		TotalAgents: 2,
		ReadyAgents: 1,
		Agents: map[string]*domain.AgentStatus{
			"writer": {Name: "writer", URL: "http://localhost:8081", State: domain.AgentStateReady},
			"coder":  {Name: "coder", URL: "http://localhost:8082", State: domain.AgentStateFailed, Error: "connection refused"},
		},
	})

	items := view.list.Items()
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	first := items[0].(a2aAgentItem)
	if first.name != "coder" || !first.failed || first.detail != "connection refused" {
		t.Errorf("items must be sorted by name with failure details, got %+v", first)
	}
	second := items[1].(a2aAgentItem)
	if second.name != "writer" || second.failed || second.state != "ready" {
		t.Errorf("unexpected second item: %+v", second)
	}

	if view.list.Title != "A2A Agents (1/2 ready)" {
		t.Errorf("title = %q, want the readiness summary", view.list.Title)
	}
}

func TestA2AAgentsView_NilReadinessIsSafe(t *testing.T) {
	view, _ := newA2AAgentsViewForTest(nil)

	if got := len(view.list.Items()); got != 0 {
		t.Fatalf("expected no items without readiness, got %d", got)
	}
	if view.list.Title != "A2A Agents (0/0 ready)" {
		t.Errorf("title = %q, want an empty readiness summary", view.list.Title)
	}
}

func TestA2AAgentsView_EscCancelsEnterDoesNot(t *testing.T) {
	view, _ := newA2AAgentsViewForTest(&domain.AgentReadinessState{
		TotalAgents: 1,
		ReadyAgents: 1,
		Agents:      map[string]*domain.AgentStatus{"writer": {Name: "writer", State: domain.AgentStateReady}},
	})

	model, _ := view.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	view = model.(*A2AAgentsViewImpl)
	if view.IsCancelled() {
		t.Fatal("enter is a no-op in the read-only agents view")
	}

	model, _ = view.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	view = model.(*A2AAgentsViewImpl)
	if !view.IsCancelled() {
		t.Fatal("esc should cancel the agents view")
	}
}

func TestA2AAgentsView_ResetRefreshesReadiness(t *testing.T) {
	view, stateManager := newA2AAgentsViewForTest(&domain.AgentReadinessState{
		TotalAgents: 1,
		ReadyAgents: 0,
		Agents:      map[string]*domain.AgentStatus{"writer": {Name: "writer", State: domain.AgentStateStarting}},
	})

	model, _ := view.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	view = model.(*A2AAgentsViewImpl)

	stateManager.GetAgentReadinessReturns(&domain.AgentReadinessState{
		TotalAgents: 1,
		ReadyAgents: 1,
		Agents:      map[string]*domain.AgentStatus{"writer": {Name: "writer", State: domain.AgentStateReady}},
	})
	view.Reset()

	if view.IsCancelled() {
		t.Error("Reset must clear the cancelled flag")
	}
	if view.list.Title != "A2A Agents (1/1 ready)" {
		t.Errorf("title = %q, want the refreshed readiness", view.list.Title)
	}
	if got := view.list.Items()[0].(a2aAgentItem); got.state != "ready" {
		t.Errorf("Reset should re-read agent state, got %+v", got)
	}
}
