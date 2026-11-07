package domain

import (
	"context"
	"time"
)

// ThreadKey identifies a thread: the project dir its worker runs in and the
// conversation the worker carries.
type ThreadKey struct {
	ProjectDir     string
	ConversationID string
}

// ThreadOptions override the project's config for one thread's worker. They
// apply when the worker launches, and zero values keep the project's config.
type ThreadOptions struct {
	Model              string   `json:"model,omitempty"`
	Mode               string   `json:"mode,omitempty"`
	SystemPrompt       string   `json:"system_prompt,omitempty"`
	CustomInstructions string   `json:"custom_instructions,omitempty"`
	SandboxDirectories []string `json:"sandbox_directories,omitempty"`
	MaxTurns           int      `json:"max_turns,omitempty"`
	// Subagent marks a thread another thread's Agent tool spawned.
	Subagent *Subagent `json:"subagent,omitempty"`
	// Agent names the Markdown agent the thread runs as. The daemon it is
	// opened on resolves it into the model, system prompt, tool lists and pin
	// below, so a relay or a teleport carries those instead of the name.
	Agent           string   `json:"agent,omitempty"`
	Tools           []string `json:"tools,omitempty"`
	DisallowedTools []string `json:"disallowed_tools,omitempty"`
	// PinModel keeps Model for the thread's lifetime, whatever select_model asks.
	PinModel bool `json:"pin_model,omitempty"`
	// Workspace and WorkspaceDir place a thread no lease places: the
	// workspace that holds its conversation and the source code there, as
	// when a client opens a session it listed on a remote workspace.
	Workspace    string `json:"workspace,omitempty"`
	WorkspaceDir string `json:"workspace_dir,omitempty"`
	// Env is extra environment the worker starts with, such as credentials a
	// teleport handed over. It never travels on the wire.
	Env []string `json:"-"`
}

// Portable is what of the options follows a session to another workspace: the
// agent's model, mode, prompts, tools and turn limit. The agent name, placement,
// sandbox paths, the spawn and Env belong to the daemon the thread was opened on.
func (o ThreadOptions) Portable() ThreadOptions {
	return ThreadOptions{
		Model:              o.Model,
		Mode:               o.Mode,
		SystemPrompt:       o.SystemPrompt,
		CustomInstructions: o.CustomInstructions,
		MaxTurns:           o.MaxTurns,
		Tools:              o.Tools,
		DisallowedTools:    o.DisallowedTools,
		PinModel:           o.PinModel,
	}
}

// Subagent is what a spawned thread knows about its spawn: the parent it
// reports to, its depth under the top-level session for the depth guard, and
// the tool allowlist a Markdown agent preset gave it (nil inherits the
// project's tools).
type Subagent struct {
	Parent string   `json:"parent"`
	Depth  int      `json:"depth"`
	Tools  []string `json:"tools,omitempty"`
}

// Worker is one running session worker. Send writes one frame line to its
// stdin, and Lines yields its stdout lines until the worker exits.
type Worker interface {
	Send(frame []byte) error
	Lines() <-chan []byte
	Stop()
}

// LaunchWorker starts the worker for a thread.
type LaunchWorker func(key ThreadKey, opts ThreadOptions) (Worker, error)

// ResolveAgent overlays the Markdown agent opts.Agent names, as the project dir
// and the user define it, onto a new thread's options. It fails for an agent
// neither defines.
type ResolveAgent func(projectDir string, opts ThreadOptions) (ThreadOptions, error)

// Client is one connected client of a thread, such as a WebSocket connection.
// Deliver hands it one frame.
type Client interface {
	Deliver(frame []byte)
}

// IdleTimeout is how long a thread nobody follows may sit on the user's
// turn, with no run, no reply pending and no background job, before the
// registry stops its worker. Long enough to absorb a quick switch back,
// short enough that a left chat does not keep its worker around.
const IdleTimeout = 30 * time.Second

// ChannelFollowTimeout is how long a channel chat keeps following its
// thread after its last frame before it detaches, so a reply minutes
// later still lands on a warm worker.
const ChannelFollowTimeout = 10 * time.Minute

// ThreadRouter is the surface a driving adapter calls per client frame
// and on disconnect. The daemon's thread registry implements it.
type ThreadRouter interface {
	// Handle routes one client frame to its thread's worker.
	Handle(c Client, frame []byte)
	// Detach drops a client that stopped following its thread.
	Detach(c Client)
}

// BrowserRelay forwards one browser_command frame from a session worker to the
// connected browser extension and always answers with the browser_result
// carrying the frame's id, a failure included. The host wires the relay, which
// serializes commands for the one browser.
type BrowserRelay func(ctx context.Context, frame []byte) []byte
