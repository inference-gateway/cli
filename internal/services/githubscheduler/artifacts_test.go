package githubscheduler

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func makeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func newTestPoller(t *testing.T, runner CommandRunner) *ArtifactPoller {
	t.Helper()
	dir := t.TempDir()
	p, err := NewArtifactPoller(ArtifactPollerOptions{
		Runner:           runner,
		Repo:             "me/.routines",
		ConversationsDir: filepath.Join(dir, "conversations"),
		ArtifactsDir:     filepath.Join(dir, "artifacts"),
		StatePath:        filepath.Join(dir, "state.json"),
		Interval:         time.Minute,
		MaxAttempts:      3,
		RateLimitBackoff: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	p.ctx = context.Background()
	return p
}

const listJSON = `{"artifacts":[
	{"id":11,"name":"infer-conversations-100","expired":false},
	{"id":12,"name":"other-artifact","expired":false},
	{"id":13,"name":"infer-conversations-101","expired":true}
]}`

func TestPollerDownloadsAndIsIdempotent(t *testing.T) {
	zipData := makeZip(t, map[string]string{
		"abc.jsonl":                   `{"role":"user"}`,
		"nested/x.jsonl":              `{"role":"assistant"}`,
		"notes.txt":                   "ignore me",
		".artifacts/sess-1/image.png": "png-bytes",
	})
	downloads := 0
	runner := &scriptRunner{respond: func(name string, args []string) ([]byte, error) {
		switch {
		case strings.Contains(args[1], "/zip"):
			downloads++
			return zipData, nil
		default:
			return []byte(listJSON), nil
		}
	}}
	p := newTestPoller(t, runner)

	if err := p.tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	if downloads != 1 {
		t.Fatalf("downloads = %d, want 1", downloads)
	}
	for _, f := range []string{"abc.jsonl", "x.jsonl"} {
		if _, err := os.Stat(filepath.Join(p.opts.ConversationsDir, f)); err != nil {
			t.Errorf("missing extracted file %s: %v", f, err)
		}
	}
	if _, err := os.Stat(filepath.Join(p.opts.ArtifactsDir, "notes.txt")); err != nil {
		t.Errorf("non-jsonl file must be extracted to artifacts dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(p.opts.ArtifactsDir, "sess-1", "image.png")); err != nil {
		t.Errorf("session subdir must be preserved under artifacts dir: %v", err)
	}

	if err := p.tick(context.Background()); err != nil {
		t.Fatalf("second tick: %v", err)
	}
	if downloads != 1 {
		t.Fatalf("second tick re-downloaded: downloads = %d", downloads)
	}

	// State survives a restart (fresh poller, same state path).
	p2 := newTestPoller(t, runner)
	p2.opts.StatePath = p.opts.StatePath
	p2.loadState()
	if p2.state.Cursor != 11 {
		t.Fatalf("cursor = %d, want 11", p2.state.Cursor)
	}
}

func TestPollerRetriesThenGivesUp(t *testing.T) {
	runner := &scriptRunner{respond: func(name string, args []string) ([]byte, error) {
		if strings.Contains(args[1], "/zip") {
			return []byte("boom"), fmt.Errorf("exit 1")
		}
		return []byte(listJSON), nil
	}}
	p := newTestPoller(t, runner)

	for range 3 {
		if err := p.tick(context.Background()); err != nil {
			t.Fatalf("tick: %v", err)
		}
	}
	if p.state.Pending[11] != 3 {
		t.Fatalf("attempts = %d, want 3", p.state.Pending[11])
	}

	if err := p.tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, pending := p.state.Pending[11]; pending {
		t.Fatalf("artifact should be dropped after max attempts")
	}
	before := len(runner.commands)
	if err := p.tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range runner.commands[before:] {
		if strings.Contains(cmd, "/zip") {
			t.Fatalf("no download expected after giving up: %v", cmd)
		}
	}
}

func TestPollerRateLimitBackoff(t *testing.T) {
	calls := 0
	runner := &scriptRunner{respond: func(name string, args []string) ([]byte, error) {
		calls++
		return []byte("API rate limit exceeded for user"), errors.New("exit 1")
	}}
	p := newTestPoller(t, runner)

	if err := p.tick(context.Background()); err != nil {
		t.Fatalf("rate-limited tick must not error: %v", err)
	}
	if p.pausedUntil.IsZero() || time.Until(p.pausedUntil) < 30*time.Minute {
		t.Fatalf("expected ~1h pause, got %v", p.pausedUntil)
	}

	p.tickGuarded()
	if calls != 1 {
		t.Fatalf("paused poller must not call gh: calls = %d", calls)
	}
}

func TestPollerRateLimitDoesNotConsumeAttempts(t *testing.T) {
	rateLimit := false
	runner := &scriptRunner{respond: func(name string, args []string) ([]byte, error) {
		if strings.Contains(args[1], "/zip") {
			if rateLimit {
				return []byte("API rate limit exceeded"), errors.New("exit 1")
			}
			return []byte("boom"), errors.New("exit 1")
		}
		return []byte(listJSON), nil
	}}
	p := newTestPoller(t, runner)

	if err := p.tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if p.state.Pending[11] != 1 {
		t.Fatalf("attempts = %d, want 1", p.state.Pending[11])
	}

	rateLimit = true
	p.pausedUntil = time.Time{}
	if err := p.tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if p.state.Pending[11] != 1 {
		t.Fatalf("rate-limited attempt must not count: attempts = %d", p.state.Pending[11])
	}
	if p.pausedUntil.IsZero() {
		t.Fatalf("expected backoff after rate-limited download")
	}
}
