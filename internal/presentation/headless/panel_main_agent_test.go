package headless

import (
	"errors"
	"slices"
	"testing"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"
	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

func TestPanelSelectAgentAppliesTheResolvedAgent(t *testing.T) {
	agents := &agentdomainmocks.FakeAgentService{}
	agents.MainAgentReturns(agentdomain.MainAgent{Name: "explore"})
	models := &convmocks.FakeModelService{}
	models.GetCurrentModelReturns("deepseek/deepseek-v4-flash")
	deps := panelDeps()
	deps.MainAgents, deps.Models = agents, models
	p, sink := startPanel(deps)

	consumed := handle(t, p, map[string]any{
		"type": "select_agent", "agent": "explore", "system_prompt": "Read-only.", "tools": []string{"Read", "Grep"}, "mcp_servers": []string{},
	})

	reply := readFrame(t, sink, "agent")
	if !consumed || reply["agent"] != "explore" || reply["model"] != "deepseek/deepseek-v4-flash" || reply["error"] != nil {
		t.Fatalf("reply = %v, want explore on the current model", reply)
	}
	got := agents.SelectMainAgentArgsForCall(0)
	if got.Name != "explore" || got.Instructions != "Read-only." || !slices.Equal(got.Tools, []string{"Read", "Grep"}) || got.MCPServers == nil {
		t.Fatalf("applied %+v, want the options the frame carried, an empty server list kept empty", got)
	}
}

func TestPanelSelectAgentReportsARefusal(t *testing.T) {
	agents := &agentdomainmocks.FakeAgentService{}
	agents.SelectMainAgentReturns(errors.New("finish or stop the running turn before switching the main agent"))
	agents.MainAgentReturns(agentdomain.MainAgent{Name: "main"})
	deps := panelDeps()
	deps.MainAgents = agents
	p, sink := startPanel(deps)

	handle(t, p, map[string]any{"type": "select_agent", "agent": "explore"})

	reply := readFrame(t, sink, "agent")
	if reply["agent"] != "main" || reply["error"] != "finish or stop the running turn before switching the main agent" {
		t.Fatalf("reply = %v, want the refusal and the agent the chat still runs as", reply)
	}
}
