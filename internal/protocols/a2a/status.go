package a2a

import (
	"context"
	"sync"
	"time"

	config "github.com/inference-gateway/cli/config"
	a2adomain "github.com/inference-gateway/cli/internal/protocols/a2a/domain"
	a2ainfra "github.com/inference-gateway/cli/internal/protocols/a2a/infrastructure"
)

// AgentStatus is the probe result for one configured A2A agent.
type AgentStatus struct {
	Name  string `json:"name"`
	URL   string `json:"url"`
	State string `json:"state"`
	Error string `json:"error,omitempty"`
}

// AgentsStatusReport mirrors the TUI's "A2A: ready/total" indicator plus a
// per-agent breakdown.
type AgentsStatusReport struct {
	TotalAgents int           `json:"total_agents"`
	ReadyAgents int           `json:"ready_agents"`
	Agents      []AgentStatus `json:"agents"`
}

// ProbeAgents fetches each agent's card once, the same check the liveness
// probes use, and reports what a fresh session would see. Local run:true
// agents are only reachable while a session (or a detached container) has
// them running.
func ProbeAgents(ctx context.Context, agents []config.AgentEntry, externalURLs []string) AgentsStatusReport {
	report := AgentsStatusReport{Agents: make([]AgentStatus, 0, len(agents)+len(externalURLs))}
	for _, agent := range agents {
		report.Agents = append(report.Agents, AgentStatus{Name: agent.Name, URL: agent.URL})
	}
	for _, url := range externalURLs {
		report.Agents = append(report.Agents, AgentStatus{Name: AgentNameFromURL(url), URL: url})
	}
	report.TotalAgents = len(report.Agents)

	var wg sync.WaitGroup
	for i := range report.Agents {
		wg.Add(1)
		go func(status *AgentStatus) {
			defer wg.Done()
			probeAgentCard(ctx, status)
		}(&report.Agents[i])
	}
	wg.Wait()

	for _, agent := range report.Agents {
		if agent.State == a2adomain.AgentStateReady.String() {
			report.ReadyAgents++
		}
	}
	return report
}

func probeAgentCard(ctx context.Context, status *AgentStatus) {
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if _, err := a2ainfra.NewClient(status.URL).GetAgentCard(checkCtx); err != nil {
		status.State = a2adomain.AgentStateFailed.String()
		status.Error = err.Error()
		if message, rejected := a2ainfra.AuthFailure(status.URL, err); rejected {
			status.Error = message
		}
		return
	}
	status.State = a2adomain.AgentStateReady.String()
}
