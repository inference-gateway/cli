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

// ThreadOptions override the project's config for one thread's worker at
// launch, and zero values keep it. Agent names a Markdown agent the opening
// daemon resolves into the other fields. MCPServers nil keeps the enabled
// servers and empty brings none. Workspace places an unleased thread, and Env
// never travels on the wire.
type ThreadOptions struct {
	Model              string    `json:"model,omitempty"`
	Mode               string    `json:"mode,omitempty"`
	SystemPrompt       string    `json:"system_prompt,omitempty"`
	CustomInstructions string    `json:"custom_instructions,omitempty"`
	SandboxDirectories []string  `json:"sandbox_directories,omitempty"`
	MaxTurns           int       `json:"max_turns,omitempty"`
	Subagent           *Subagent `json:"subagent,omitempty"`
	Agent              string    `json:"agent,omitempty"`
	Tools              []string  `json:"tools,omitempty"`
	DisallowedTools    []string  `json:"disallowed_tools,omitempty"`
	MCPServers         []string  `json:"mcp_servers,omitzero"`
	Workspace          string    `json:"workspace,omitempty"`
	WorkspaceDir       string    `json:"workspace_dir,omitempty"`
	Env                []string  `json:"-"`
}

// Portable is what of the options follows a session to another workspace: the
// agent's model, mode, prompts, tools, MCP servers, turn limit and the spawn, so a
// teammate placed remotely still reports to its lead under the depth guard. The
// agent name, placement, sandbox paths and Env belong to the daemon the thread
// was opened on.
func (o ThreadOptions) Portable() ThreadOptions {
	return ThreadOptions{
		Model:              o.Model,
		Mode:               o.Mode,
		SystemPrompt:       o.SystemPrompt,
		CustomInstructions: o.CustomInstructions,
		MaxTurns:           o.MaxTurns,
		Tools:              o.Tools,
		DisallowedTools:    o.DisallowedTools,
		MCPServers:         o.MCPServers,
		Subagent:           o.Subagent,
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
