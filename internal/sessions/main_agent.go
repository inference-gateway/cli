package sessions

import (
	"encoding/json"
	"errors"

	sessionsdomain "github.com/inference-gateway/cli/internal/sessions/domain"
)

// select_agent switches the main agent of the client's thread. The registry
// resolves the agent it names, so the worker - here or on another workspace -
// needs no agent files, and the reply comes from the worker as agent.
const (
	frameSelectAgent = "select_agent"
	frameAgent       = "agent"
)

var (
	errSwitchDuringRun = errors.New("finish or stop the running turn before switching the main agent")
	errNoAgentNamed    = errors.New("select_agent needs the agent to switch to")
	errNoAgentResolver = errors.New("this daemon resolves no agents")
)

// selectAgentFrame is a select_agent frame with the options its agent
// resolved to, as the thread's worker applies them.
type selectAgentFrame struct {
	Type       string `json:"type"`
	ID         string `json:"id,omitempty"`
	ProjectDir string `json:"project_dir,omitempty"`
	sessionsdomain.ThreadOptions
}

// resolveSelectAgent rewrites a select_agent frame with the options of the
// agent it names, refusing while the thread runs a turn. The agent's files are
// read outside the lock.
func (r *Registry) resolveSelectAgent(c sessionsdomain.Client, f clientFrame) ([]byte, error) {
	if f.Agent == "" {
		return nil, errNoAgentNamed
	}
	if r.agents == nil {
		return nil, errNoAgentResolver
	}
	r.mu.Lock()
	t, err := r.pickLocked(c, f)
	var dir string
	busy := false
	if err == nil {
		dir, busy = t.key.ProjectDir, t.runID != ""
	}
	r.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if busy {
		return nil, errSwitchDuringRun
	}
	opts, err := r.agents(dir, sessionsdomain.ThreadOptions{Agent: f.Agent})
	if err != nil {
		return nil, err
	}
	return json.Marshal(selectAgentFrame{Type: frameSelectAgent, ID: f.ID, ProjectDir: f.ProjectDir, ThreadOptions: opts})
}

// agentRefusal answers a select_agent the registry refused, in the worker's
// reply shape.
func agentRefusal(agent string, err error) []byte {
	data, _ := json.Marshal(map[string]string{"type": frameAgent, "agent": agent, "error": err.Error()})
	return data
}
