package tools

import (
	"strings"
	"testing"

	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
	schedinfra "github.com/inference-gateway/cli/internal/scheduler/infrastructure"
)

// inbox records what a headless teammate was sent.
type inbox struct{ got []string }

func (b *inbox) teammate(id, label string) *scheddomain.SubagentState {
	return &scheddomain.SubagentState{ID: id, Label: label, SessionID: "sess-" + id, Mode: scheddomain.SubagentModeHeadless, Input: func(text string) error {
		b.got = append(b.got, text)
		return nil
	}}
}

func TestRelayTeamMessage(t *testing.T) {
	tests := []struct {
		name      string
		to        string
		closed    bool
		wantLead  string
		wantMate  string
		wantSaid  string
		wantNotes int
	}{
		{name: "to the lead", to: "lead", wantLead: "[Message from researcher]\n\nfound it\n\nReply with Agent(to: \"researcher\", message: \"...\").", wantNotes: 1},
		{name: "to a teammate", to: "reviewer", wantMate: "[Message from researcher]\n\nfound it"},
		{name: "to a teammate by id", to: "b", wantMate: "[Message from researcher]"},
		{name: "to nobody", to: "ghost", wantSaid: "[Message to ghost not delivered: no teammate has that name. Your team: lead, reviewer]"},
		{name: "to a closed teammate", to: "reviewer", closed: true, wantSaid: "[Message to reviewer not delivered: subagent reviewer is a blocking or finished headless subagent"},
		{name: "to itself", to: "researcher", wantSaid: "not delivered: no teammate has that name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sender, mate inbox
			tracker := schedinfra.NewSubagentTracker()
			from := sender.teammate("a", "researcher")
			reviewer := mate.teammate("b", "reviewer")
			if tt.closed {
				reviewer.Input = nil
			}
			_ = tracker.AddSubagent(from)
			_ = tracker.AddSubagent(reviewer)
			var notes []scheddomain.JobSignal
			tool := newMessagingAgentTool(tracker)

			tool.relayTeamMessage(from, scheddomain.TeamMessage{To: tt.to, Text: "found it"}, func(sig scheddomain.JobSignal) { notes = append(notes, sig) })

			if len(notes) != tt.wantNotes {
				t.Fatalf("lead got %d notes, want %d: %+v", len(notes), tt.wantNotes, notes)
			}
			if tt.wantLead != "" && (notes[0].Note != tt.wantLead || !notes[0].Enqueue) {
				t.Fatalf("lead note = %+v, want %q enqueued", notes[0], tt.wantLead)
			}
			if tt.wantMate != "" && (len(mate.got) != 1 || !strings.HasPrefix(mate.got[0], tt.wantMate)) {
				t.Fatalf("teammate got %q, want %q", mate.got, tt.wantMate)
			}
			if tt.wantSaid != "" && (len(sender.got) != 1 || !strings.Contains(sender.got[0], tt.wantSaid)) {
				t.Fatalf("sender was told %q, want %q", sender.got, tt.wantSaid)
			}
			if tt.wantSaid == "" && len(sender.got) != 0 {
				t.Fatalf("a delivered message bounced: %q", sender.got)
			}
		})
	}
}

func TestRelayTeamMessageStopsAtTheCap(t *testing.T) {
	var sender, mate inbox
	tracker := schedinfra.NewSubagentTracker()
	from := sender.teammate("a", "researcher")
	_ = tracker.AddSubagent(from)
	_ = tracker.AddSubagent(mate.teammate("b", "reviewer"))
	tool := newMessagingAgentTool(tracker)
	tool.relayed.Store(maxTeamMessages)
	var notes []scheddomain.JobSignal
	emit := func(sig scheddomain.JobSignal) { notes = append(notes, sig) }

	tool.relayTeamMessage(from, scheddomain.TeamMessage{To: "reviewer", Text: "one more"}, emit)
	tool.relayTeamMessage(from, scheddomain.TeamMessage{To: "reviewer", Text: "and another"}, emit)

	if len(mate.got) != 0 {
		t.Fatalf("a message past the cap reached the teammate: %q", mate.got)
	}
	if len(sender.got) != 2 || !strings.Contains(sender.got[0], "used all of its 100 messages") {
		t.Fatalf("the sender must hear each message past the cap bounced, got %q", sender.got)
	}
	if len(notes) != 1 || !strings.Contains(notes[0].Note, "Team message limit reached") {
		t.Fatalf("the lead must hear once that the cap was reached, got %+v", notes)
	}
}

func TestFormTeam(t *testing.T) {
	tracker := schedinfra.NewSubagentTracker()
	_ = tracker.AddSubagent(&scheddomain.SubagentState{ID: "r", Label: "agent", SessionID: "sess-r"})
	tool := newMessagingAgentTool(tracker)
	specs := []AgentTaskSpec{
		{Agent: "explore", Description: "look left"},
		{Agent: "explore", Description: "look right"},
		{Label: "lead", Description: "pose as the lead"},
		{Description: "help"},
	}

	tool.formTeam(specs, scheddomain.SubagentModeHeadless)

	var names []string
	for _, spec := range specs {
		names = append(names, spec.Label)
	}
	if want := "explore explore-2 lead-2 agent-2"; strings.Join(names, " ") != want {
		t.Fatalf("names = %v, want %s: unique, never the lead's, after the label or agent", names, want)
	}
	task := specs[0].task()
	for _, want := range []string{"look left\n\n[Team]", `You are "explore"`, `your lead "lead"`, `"agent", "explore-2", "lead-2", "agent-2"`} {
		if !strings.Contains(task, want) {
			t.Errorf("task %q lacks %q", task, want)
		}
	}
}

func TestFormTeamLeavesALoneSubagentsTaskAsItIs(t *testing.T) {
	specs := []AgentTaskSpec{{Agent: "explore", Description: "look around"}}
	newMessagingAgentTool(schedinfra.NewSubagentTracker()).formTeam(specs, scheddomain.SubagentModeHeadless)
	if specs[0].Label != "explore" || specs[0].task() != "look around" {
		t.Fatalf("spec = %+v: a subagent without teammates reaches only its lead and needs no team note", specs[0])
	}
}

func TestFormTeamGivesInteractiveSubagentsNoNote(t *testing.T) {
	specs := []AgentTaskSpec{{Description: "watch me"}}
	newMessagingAgentTool(schedinfra.NewSubagentTracker()).formTeam(specs, scheddomain.SubagentModeInteractive)
	if specs[0].Label != "agent" || specs[0].task() != "watch me" {
		t.Fatalf("spec = %+v: an interactive subagent cannot send, so its task stays as it is", specs[0])
	}
}
