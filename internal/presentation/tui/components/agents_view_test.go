package components

import (
	"errors"
	"testing"

	tuimocks "github.com/inference-gateway/cli/tests/mocks/tui"

	tea "charm.land/bubbletea/v2"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	styles "github.com/inference-gateway/cli/internal/presentation/tui/styles"
)

// fakeSubagentCatalog is a canned SubagentCatalog for view tests.
type fakeSubagentCatalog struct {
	infos []agentdomain.SubagentInfo
}

func (f *fakeSubagentCatalog) ListMarkdownSubagents() []agentdomain.SubagentInfo {
	return f.infos
}

// newAgentsViewForTest builds the merged agents view over a real
// ApplicationState reconstructed from the given readiness plus canned presets.
func newAgentsViewForTest(readiness *tui.AgentReadinessState, infos []agentdomain.SubagentInfo) (*AgentsView, *tui.ApplicationState) {
	fakeTheme := &tuimocks.FakeTheme{}
	fakeTheme.GetAccentColorReturns("#ff9e64")
	fakeTheme.GetDimColorReturns("#888888")
	fakeTheme.GetStatusColorReturns("#e0af68")
	fakeTheme.GetErrorColorReturns("#f7768e")
	themeService := &tuimocks.FakeThemeService{}
	themeService.GetCurrentThemeReturns(fakeTheme)

	stateManager := reconstructReadiness(readiness)
	view := NewAgentsView(stateManager, &fakeSubagentCatalog{infos: infos}, styles.NewProvider(themeService))
	return view, stateManager
}

// reconstructReadiness rebuilds a real ApplicationState from a readiness value,
// preserving per-agent state and failure details.
func reconstructReadiness(readiness *tui.AgentReadinessState) *tui.ApplicationState {
	st := tui.NewApplicationState()
	if readiness == nil {
		return st
	}
	st.InitializeAgentReadiness(readiness.TotalAgents)
	for _, a := range readiness.Agents {
		if a.State == agentdomain.AgentStateFailed && a.Error != "" {
			st.SetAgentError(a.Name, errors.New(a.Error))
			continue
		}
		st.UpdateAgentStatus(a.Name, a.State, a.Message, a.URL, a.Image)
	}
	return st
}

func TestAgentsView_MergesA2AAndLocalRows(t *testing.T) {
	view, _ := newAgentsViewForTest(
		&tui.AgentReadinessState{
			TotalAgents: 2,
			ReadyAgents: 1,
			Agents: map[string]*tui.AgentStatus{
				"writer": {Name: "writer", URL: "http://localhost:8081", State: agentdomain.AgentStateReady},
				"coder":  {Name: "coder", URL: "http://localhost:8082", State: agentdomain.AgentStateFailed, Error: "connection refused"},
			},
		},
		[]agentdomain.SubagentInfo{
			{Name: "explorer", Description: "Read-only code explorer", Tools: []string{"Grep", "Read"}, ReadOnly: true, Source: "project"},
		},
	)

	items := view.list.Items()
	if len(items) != 3 {
		t.Fatalf("expected a2a and local rows merged, got %d", len(items))
	}

	first := items[0].(agentItem)
	if first.kind != agentKindA2A || first.name != "coder" || !first.failed || first.detail != "connection refused" {
		t.Errorf("rows must be merged and sorted by name, got %+v", first)
	}
	second := items[1].(agentItem)
	if second.kind != agentKindLocal || second.name != "explorer" || second.state != "read-only" || second.detail != "tools: Grep, Read | model: inherit | project" {
		t.Errorf("unexpected local row: %+v", second)
	}
	last := items[2].(agentItem)
	if last.kind != agentKindA2A || last.name != "writer" || last.state != "ready" || last.detail != "http://localhost:8081" {
		t.Errorf("unexpected a2a row: %+v", last)
	}

	if view.list.Title != "Agents (3)" {
		t.Errorf("title = %q, want the merged count", view.list.Title)
	}
}

func TestAgentsView_NilPortsAreSafe(t *testing.T) {
	view, _ := newAgentsViewForTest(nil, nil)

	if got := len(view.list.Items()); got != 0 {
		t.Fatalf("expected no items without readiness and presets, got %d", got)
	}
	if view.list.Title != "Agents (0)" {
		t.Errorf("title = %q, want an empty summary", view.list.Title)
	}
}

func TestAgentsView_EscCancelsEnterDoesNot(t *testing.T) {
	view, _ := newAgentsViewForTest(nil, []agentdomain.SubagentInfo{{Name: "explorer"}})

	model, _ := view.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	view = model.(*AgentsView)
	if view.IsCancelled() {
		t.Fatal("enter is a no-op in the read-only agents view")
	}

	model, _ = view.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	view = model.(*AgentsView)
	if !view.IsCancelled() {
		t.Fatal("esc should cancel the agents view")
	}
}

func TestAgentsView_LiveUpdatesOnAgentStatusEvent(t *testing.T) {
	view, stateManager := newAgentsViewForTest(
		&tui.AgentReadinessState{
			TotalAgents: 1,
			ReadyAgents: 0,
			Agents:      map[string]*tui.AgentStatus{"writer": {Name: "writer", State: agentdomain.AgentStatePullingImage, Message: "Pulling image: img"}},
		},
		nil,
	)
	view.Reset()

	stateManager.UpdateAgentPullProgress("writer", 3, 7)
	model, _ := view.Update(tui.AgentStatusUpdateEvent{AgentName: "writer", State: agentdomain.AgentStatePullingImage})
	view = model.(*AgentsView)

	if got := view.list.Items()[0].(agentItem); got.detail != "Pulling image: img (3/7 layers)" {
		t.Errorf("event should refresh pull progress in the detail, got %+v", got)
	}

	stateManager.UpdateAgentStatus("writer", agentdomain.AgentStateReady, "", "", "")
	model, _ = view.Update(tui.AgentStatusUpdateEvent{AgentName: "writer", State: agentdomain.AgentStateReady})
	view = model.(*AgentsView)

	if view.list.Title != "Agents (1)" {
		t.Errorf("title = %q, want the refreshed count", view.list.Title)
	}
	if got := view.list.Items()[0].(agentItem); got.state != "ready" {
		t.Errorf("event should re-read agent state, got %+v", got)
	}
}

func TestAgentsView_InheritedToolsAndModelShown(t *testing.T) {
	caps := subagentCapabilities(agentdomain.SubagentInfo{Name: "runner", Description: "Runs tests", Source: "project"})

	if caps != "tools: all tools (inherited) | model: inherit | project" {
		t.Errorf("capabilities = %q, want the inherited placeholders", caps)
	}
}
