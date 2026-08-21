package web

import (
	"slices"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

// TestBuildLocalSessionCommandSelfReapsTmuxSession guards the tmux teardown. Its
// regression mode is silent: drop destroy-unattached and every web chat outlives
// its browser session as a child of the tmux daemon, holding a
// ~/.infer/run/pids entry that defers the gateway shutdown of every later
// session - with nothing logged anywhere.
func TestBuildLocalSessionCommandSelfReapsTmuxSession(t *testing.T) {
	if !tmuxInstalled() {
		t.Skip("tmux not installed")
	}

	args := buildLocalSessionCommand(&config.Config{Web: config.WebConfig{Tmux: true}}, "/usr/local/bin/infer").Args

	opt := slices.Index(args, "destroy-unattached")
	if opt < 0 {
		t.Fatalf("destroy-unattached missing from tmux command: %v", args)
	}
	if args[opt+1] != "on" {
		t.Errorf("destroy-unattached = %q, want \"on\"", args[opt+1])
	}

	newSession := slices.Index(args, "new-session")
	if newSession < 0 {
		t.Fatalf("new-session missing from tmux command: %v", args)
	}
	if opt > newSession {
		t.Errorf("destroy-unattached set at %d, after new-session at %d: the session must never be briefly unprotected", opt, newSession)
	}
}

func TestBuildLocalSessionCommandWithoutTmux(t *testing.T) {
	args := buildLocalSessionCommand(&config.Config{}, "/usr/local/bin/infer").Args

	want := []string{"/usr/local/bin/infer", "chat"}
	if !slices.Equal(args, want) {
		t.Errorf("args = %v, want %v", args, want)
	}
}
