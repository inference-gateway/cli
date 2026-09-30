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

// Client is one connected client of a thread, such as a WebSocket connection.
// Deliver hands it one frame.
type Client interface {
	Deliver(frame []byte)
}

// IdleTimeout is how long a thread nobody follows may sit idle before
// the registry stops its worker, and how long a channel chat keeps
// following its thread after its last frame before it detaches so the
// reap can fire.
// ponytail: a const, a config key when someone needs to tune it.
const IdleTimeout = 10 * time.Minute

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
