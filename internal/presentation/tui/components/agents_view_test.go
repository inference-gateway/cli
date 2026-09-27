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

func TestAgentsView_GroupsA2AOnTopAndLocalsBelow(t *testing.T) {
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
	if len(items) != 5 {
		t.Fatalf("expected two section headers around the a2a and local rows, got %d", len(items))
	}

	if items[0].(agentSection).title != a2aSectionTitle {
		t.Errorf("the a2a group must come first, got %+v", items[0])
	}
	coder := items[1].(agentItem)
	if coder.kind != agentKindA2A || coder.name != "coder" || !coder.failed || coder.detail != "connection refused" {
		t.Errorf("a2a rows must be sorted by name, got %+v", coder)
	}
	writer := items[2].(agentItem)
	if writer.kind != agentKindA2A || writer.name != "writer" || writer.state != "ready" || writer.detail != "http://localhost:8081" {
		t.Errorf("unexpected a2a row: %+v", writer)
	}

	if items[3].(agentSection).title != markdownSectionTitle {
		t.Errorf("the markdown group must follow the a2a one, got %+v", items[3])
	}
	explorer := items[4].(agentItem)
	if explorer.kind != agentKindLocal || explorer.name != "explorer" || explorer.state != "read-only" || explorer.detail != "tools: Grep, Read | model: inherit | project" {
		t.Errorf("unexpected local row: %+v", explorer)
	}

	if view.list.Title != "Agents (3)" {
		t.Errorf("title = %q, want the count of agents without section rows", view.list.Title)
	}
}

func TestAgentsView_SingleGroupGetsNoEmptySection(t *testing.T) {
	view, _ := newAgentsViewForTest(nil, []agentdomain.SubagentInfo{{Name: "explorer"}})

	items := view.list.Items()
	if len(items) != 2 || items[0].(agentSection).title != markdownSectionTitle {
		t.Fatalf("expected only the markdown section, got %d items", len(items))
	}
	if view.list.Title != "Agents (1)" {
		t.Errorf("title = %q, want the markdown agent count", view.list.Title)
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

	if got := view.list.Items()[1].(agentItem); got.detail != "Pulling image: img (3/7 layers)" {
		t.Errorf("event should refresh pull progress in the detail, got %+v", got)
	}

	stateManager.UpdateAgentStatus("writer", agentdomain.AgentStateReady, "", "", "")
	model, _ = view.Update(tui.AgentStatusUpdateEvent{AgentName: "writer", State: agentdomain.AgentStateReady})
	view = model.(*AgentsView)

	if view.list.Title != "Agents (1)" {
		t.Errorf("title = %q, want the refreshed count", view.list.Title)
	}
	if got := view.list.Items()[1].(agentItem); got.state != "ready" {
		t.Errorf("event should re-read agent state, got %+v", got)
	}
}

func TestAgentsView_InheritedToolsAndModelShown(t *testing.T) {
	caps := subagentCapabilities(agentdomain.SubagentInfo{Name: "runner", Description: "Runs tests", Source: "project"})

	if caps != "tools: all tools (inherited) | model: inherit | project" {
		t.Errorf("capabilities = %q, want the inherited placeholders", caps)
	}
}
