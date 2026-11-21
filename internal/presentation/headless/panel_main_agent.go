package headless

import (
	"encoding/json"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
)

// The panel frame a client switches the main agent of a worker's conversation
// with, carrying the options the daemon resolved for the named agent.
const (
	inboundSelectAgent = "select_agent"
	outboundAgent      = "agent"
)

// agentFrame answers select_agent with the agent the conversation runs as and
// its model, or why the switch was refused.
type agentFrame struct {
	Type  string `json:"type"`
	Agent string `json:"agent"`
	Model string `json:"model,omitempty"`
	Error string `json:"error,omitempty"`
}

// mainAgents switches the agent the worker's conversation runs as.
type mainAgents struct {
	write    func(any)
	selector agentdomain.MainAgentSelector
	models   convdomain.ModelService
}

// selectAgent applies the agent a select_agent line carries and answers with
// the agent the conversation runs as afterwards.
func (m *mainAgents) selectAgent(line []byte) {
	if m.selector == nil {
		m.write(agentFrame{Type: outboundAgent, Error: "this worker cannot switch its main agent"})
		return
	}
	var agent agentdomain.MainAgent
	if err := json.Unmarshal(line, &agent); err != nil {
		m.reply(err)
		return
	}
	m.reply(m.selector.SelectMainAgent(agent))
}

func (m *mainAgents) reply(err error) {
	frame := agentFrame{Type: outboundAgent, Agent: m.selector.MainAgent().Name}
	if m.models != nil {
		frame.Model = m.models.GetCurrentModel()
	}
	if err != nil {
		frame.Error = err.Error()
	}
	m.write(frame)
}
