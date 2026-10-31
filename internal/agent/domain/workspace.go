package domain

import (
	"context"
	"crypto/tls"
	"fmt"
	"path/filepath"
)

// WorkspaceKind names where an agent runs: this daemon, a standalone daemon
// reachable over TLS, or a Kubernetes Sandbox per session.
type WorkspaceKind string

const (
	WorkspaceKindLocal      WorkspaceKind = "local"
	WorkspaceKindDaemon     WorkspaceKind = "daemon"
	WorkspaceKindKubernetes WorkspaceKind = "kubernetes"
)

// Session names one conversation and the project it belongs to. Slug is the
// project's stable name, which a receiving workspace restores it under.
type Session struct {
	ProjectDir     string `json:"project_dir"`
	ConversationID string `json:"conversation_id"`
	Slug           string `json:"slug"`
}

// Validate checks the slug and the conversation id are each one plain path
// element, because a receiving workspace restores the session under
// <projects_root>/<slug>/<conversation id> and clears that directory first.
func (s Session) Validate() error {
	for _, element := range []string{s.Slug, s.ConversationID} {
		if element == "." || element != filepath.Base(element) || !filepath.IsLocal(element) {
			return fmt.Errorf("session path element %q is not a plain name", element)
		}
	}
	return nil
}

// Endpoint is a reachable daemon binding on a workspace. Close releases what
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

// WorkspaceStatus is what list_workspaces reports per workspace.
type WorkspaceStatus struct {
	Name         string            `json:"name"`
	Kind         WorkspaceKind     `json:"kind"`
	Reachable    bool              `json:"reachable"`
	Error        string            `json:"error,omitempty"`
	Capabilities map[string]string `json:"capabilities,omitempty"`
}

// Workspace is a place sessions run. Ensure makes the workspace's daemon for a
// session reachable, creating or resuming what runs it, and Reach only resumes
// what already runs it. Probe reports whether the workspace can be reached
// at all. Suspend parks a session's runtime and Delete removes it.
type Workspace interface {
	Name() string
	Kind() WorkspaceKind
	Ensure(ctx context.Context, session Session) (Endpoint, error)
	Reach(ctx context.Context, session Session) (Endpoint, error)
	Probe(ctx context.Context) WorkspaceStatus
	Suspend(ctx context.Context, session Session) error
	Delete(ctx context.Context, session Session) error
}
