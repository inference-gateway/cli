package web

import (
	"testing"
	"time"

	websocket "github.com/gorilla/websocket"

	config "github.com/inference-gateway/cli/config"
)

type stubSession struct{ closed bool }

func (s *stubSession) Start(cols, rows int) error                  { return nil }
func (s *stubSession) HandleConnection(conn *websocket.Conn) error { return nil }
func (s *stubSession) Resize(cols, rows int) error                 { return nil }
func (s *stubSession) Close() error                                { s.closed = true; return nil }

func TestCleanupInactiveSessions(t *testing.T) {
	tests := []struct {
		name          string
		inactivityMin int
		wantRemoved   bool
	}{
		{"zero means never cleanup", 0, false},
		{"negative means never cleanup", -1, false},
		{"positive threshold reaps stale session", 5, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Web.SessionInactivityMins = tt.inactivityMin

			sm := &SessionManager{cfg: cfg, sessions: make(map[string]*SessionEntry), done: make(chan struct{})}
			sm.sessions["s1"] = &SessionEntry{
				session:    &stubSession{},
				lastActive: time.Now().Add(-time.Hour),
			}

			sm.cleanupInactiveSessions()

			if got := sm.ActiveSessionCount() == 0; got != tt.wantRemoved {
				t.Fatalf("removed = %v, want %v", got, tt.wantRemoved)
			}
		})
	}
}
