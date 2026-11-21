package tools

import (
	"cmp"
	"context"
	"fmt"
	"strings"

	logger "github.com/inference-gateway/cli/internal/platform/logger"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
)

// maxTeamMessages caps the messages a lead relays for its teammates, so two of
// them cannot keep each other talking forever.
// shortcut: a fixed cap per lead, make it tools.agent config if teams need more.
const maxTeamMessages = 100

// defaultTeammateName names a teammate spawned with neither a label nor an agent.
const defaultTeammateName = "agent"

// relayTeamMessage handles a message a teammate posted: the lead keeps one
// addressed to it as a note in its own chat, and delivers any other to the
// teammate it names. The sender hears back when a message cannot arrive.
func (t *AgentTool) relayTeamMessage(from *scheddomain.SubagentState, msg scheddomain.TeamMessage, emit func(scheddomain.JobSignal)) {
	if n := t.relayed.Add(1); n > maxTeamMessages {
		if n == maxTeamMessages+1 {
			emit(scheddomain.JobSignal{Note: fmt.Sprintf("[Team message limit reached]\n\nYour teammates used all %d team messages, so no more are relayed between them.", maxTeamMessages), Enqueue: true})
		}
		t.bounce(from, msg.To, fmt.Sprintf("the team has used all of its %d messages", maxTeamMessages))
		return
	}
	note := teamNote(teammateName(from), msg.Text)
	if msg.To == scheddomain.TeamLead {
		emit(scheddomain.JobSignal{Note: note, Enqueue: true})
		return
	}
	to := t.findSubagent(msg.To, from.ID)
	if to == nil {
		t.bounce(from, msg.To, "no teammate has that name")
		return
	}
	if _, err := t.deliver(context.Background(), to, note, nil, true); err != nil {
		t.bounce(from, msg.To, err.Error())
	}
}

// bounce tells a teammate that its message to `to` did not arrive, and whom it
// can reach instead.
func (t *AgentTool) bounce(from *scheddomain.SubagentState, to, why string) {
	team := append([]string{scheddomain.TeamLead}, t.teamNames(from.ID)...)
	note := fmt.Sprintf("[Message to %s not delivered: %s. Your team: %s]", to, why, strings.Join(team, ", "))
	if _, err := t.deliver(context.Background(), from, note, nil, true); err != nil {
		logger.Warn("an undelivered team message could not be reported back", "from", teammateName(from), "to", to, "error", err)
	}
}

// teamNote is how a team message reaches its recipient: who sent it, the text,
// and how to answer.
func teamNote(sender, text string) string {
	return fmt.Sprintf("[Message from %s]\n\n%s\n\nReply with Agent(to: %q, message: \"...\").", sender, strings.TrimSpace(text), sender)
}

// findSubagent returns the subagent this agent spawned that `to` names, by id,
// session id or teammate name, skipping the one with id except.
func (t *AgentTool) findSubagent(to, except string) *scheddomain.SubagentState {
	if t.tracker == nil {
		return nil
	}
	for _, s := range t.tracker.GetAllSubagents() {
		if s.ID != except && (s.ID == to || s.SessionID == to || teammateName(s) == to) {
			return s
		}
	}
	return nil
}

// teamNames are the names of the subagents this agent spawned, but the one
// with id except.
func (t *AgentTool) teamNames(except string) []string {
	var names []string
	if t.tracker == nil {
		return nil
	}
	for _, s := range t.tracker.GetAllSubagents() {
		if s.ID != except {
			names = append(names, teammateName(s))
		}
	}
	return names
}

// teammateName is the name a subagent goes by on its team: its label, else a
// short form of its session id.
func teammateName(s *scheddomain.SubagentState) string {
	return labelOrSession(s.Label, s.SessionID)
}

// formTeam names each new subagent uniquely on the team, after its label or
// agent, and gives each headless one with teammates, which it can message, the
// team note its task ends with. A lone subagent reaches only its lead.
func (t *AgentTool) formTeam(specs []AgentTaskSpec, mode scheddomain.SubagentMode) {
	taken := map[string]bool{scheddomain.TeamLead: true}
	var running []string
	if t.tracker != nil {
		for _, s := range t.tracker.GetAllSubagents() {
			name := teammateName(s)
			taken[name] = true
			running = append(running, name)
		}
	}
	for i := range specs {
		specs[i].Label = uniqueName(cmp.Or(specs[i].Label, specs[i].Agent, defaultTeammateName), taken)
	}
	if mode == scheddomain.SubagentModeInteractive {
		return
	}
	for i := range specs {
		mates := append([]string(nil), running...)
		for j := range specs {
			if j != i {
				mates = append(mates, specs[j].Label)
			}
		}
		if len(mates) > 0 {
			specs[i].Team = teamBriefing(specs[i].Label, mates)
		}
	}
}

// uniqueName is base, or base with the first free numeric suffix, marked taken.
func uniqueName(base string, taken map[string]bool) string {
	name := base
	for n := 2; taken[name]; n++ {
		name = fmt.Sprintf("%s-%d", base, n)
	}
	taken[name] = true
	return name
}

// teamBriefing tells a teammate who it is, who else is on its team and how to
// reach them.
func teamBriefing(name string, mates []string) string {
	quoted := make([]string, 0, len(mates))
	for _, mate := range mates {
		quoted = append(quoted, fmt.Sprintf("%q", mate))
	}
	return fmt.Sprintf("[Team]\nYou are %q, working with your lead %q and your teammates %s. Send any of them a message with Agent(to: \"<name>\", message: \"...\") to share findings, ask a question or split the work. "+
		"Their messages reach you as [Message from <name>]. Your final answer still goes to the lead as usual.", name, scheddomain.TeamLead, strings.Join(quoted, ", "))
}
