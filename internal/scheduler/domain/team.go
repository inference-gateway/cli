package domain

import "context"

// TeamMessageType names a team message on the wire: the type of the stdout line
// a subprocess teammate prints and the name of the CUSTOM event a daemon
// teammate emits.
const TeamMessageType = "agent_message"

// TeamLead is the name a teammate addresses its lead by.
const TeamLead = "lead"

// TeamMessage is one message a teammate sends to its lead or, through the lead,
// to another teammate named To. The lead stamps who sent it from the stream
// the message arrived on, so a teammate cannot speak for another.
type TeamMessage struct {
	To   string `json:"to"`
	Text string `json:"text"`
}

// TeamMessageLine is a TeamMessage as the stdout line a subprocess teammate
// prints for its lead.
type TeamMessageLine struct {
	Type string `json:"type"`
	TeamMessage
}

// TeamChannel is how a teammate reaches its lead: Post hands the lead a
// message, which the lead keeps or relays.
type TeamChannel interface {
	Post(msg TeamMessage) error
}

type teamChannelKey struct{}

// WithTeamChannel returns a context whose tool calls reach the lead through ch.
func WithTeamChannel(ctx context.Context, ch TeamChannel) context.Context {
	return context.WithValue(ctx, teamChannelKey{}, ch)
}

// TeamChannelFrom returns the channel to the lead, nil when no lead spawned
// this agent or it cannot send, as in an interactive pane.
func TeamChannelFrom(ctx context.Context) TeamChannel {
	ch, _ := ctx.Value(teamChannelKey{}).(TeamChannel)
	return ch
}
