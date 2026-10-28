package domain

import (
	"context"
	"crypto/tls"
)

// ExecutorKind names where an agent runs: this daemon, a standalone daemon
// reachable over TLS, or a Kubernetes Sandbox per session.
type ExecutorKind string

const (
	ExecutorKindLocal      ExecutorKind = "local"
	ExecutorKindDaemon     ExecutorKind = "daemon"
	ExecutorKindKubernetes ExecutorKind = "kubernetes"
)

// Session names one conversation and the project it belongs to. Slug is the
// project's stable name, which a receiving executor restores it under.
type Session struct {
	ProjectDir     string `json:"project_dir"`
	ConversationID string `json:"conversation_id"`
	Slug           string `json:"slug"`
}

// Endpoint is a reachable daemon binding on an executor. Close releases what
// made it reachable, such as a port-forward, and may be nil.
type Endpoint struct {
	Addr  string
	TLS   *tls.Config
	Token string
	Close func()
}

// Release closes the endpoint when it holds anything to close.
func (e Endpoint) Release() {
	if e.Close != nil {
		e.Close()
	}
}

// ExecutorStatus is what list_executors reports per executor.
type ExecutorStatus struct {
	Name         string            `json:"name"`
	Kind         ExecutorKind      `json:"kind"`
	Reachable    bool              `json:"reachable"`
	Error        string            `json:"error,omitempty"`
	Capabilities map[string]string `json:"capabilities,omitempty"`
}

// Executor is a place sessions run. Ensure makes the executor's daemon for a
// session reachable, creating or resuming what runs it. Probe reports whether
// the executor can be reached at all. Suspend parks a session's runtime and
// Delete removes it.
type Executor interface {
	Name() string
	Kind() ExecutorKind
	Ensure(ctx context.Context, session Session) (Endpoint, error)
	Probe(ctx context.Context) ExecutorStatus
	Suspend(ctx context.Context, session Session) error
	Delete(ctx context.Context, session Session) error
}
