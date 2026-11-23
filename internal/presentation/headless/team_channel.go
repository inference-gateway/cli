package headless

import (
	"context"
	"encoding/json"
	"io"
	"os"

	agui "github.com/inference-gateway/cli/internal/protocols/agui"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
)

// stdoutTeamChannel posts a teammate's messages on the stdout its lead reads:
// an agent_message line from a subprocess, a CUSTOM event from a daemon worker.
// Every post is one write, which the file serializes with the run's own lines.
type stdoutTeamChannel struct {
	out    io.Writer
	custom bool
}

// Post writes the message for the lead.
func (c stdoutTeamChannel) Post(msg scheddomain.TeamMessage) error {
	if c.custom {
		agui.WriteCustom(c.out, scheddomain.TeamMessageType, msg)
		return nil
	}
	data, err := json.Marshal(scheddomain.TeamMessageLine{Type: scheddomain.TeamMessageType, TeamMessage: msg})
	if err != nil {
		return err
	}
	_, err = c.out.Write(append(data, '\n'))
	return err
}

// withTeamChannel gives the runs of a subagent a channel to the lead that
// spawned it, and those of an agent another agent messaged a channel to that
// sender. Any other run gets none.
func withTeamChannel(ctx context.Context, out io.Writer, serve bool) context.Context {
	if !hasLead() {
		return ctx
	}
	return scheddomain.WithTeamChannel(ctx, stdoutTeamChannel{out: out, custom: serve})
}

// hasLead reports whether another session follows this worker as its lead: the
// orchestrator that spawned it as a subagent, or the agent whose message started it.
func hasLead() bool {
	return os.Getenv(scheddomain.EnvSubagentParentSession) != "" || os.Getenv(scheddomain.EnvSentBy) != ""
}
