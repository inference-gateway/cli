package githubscheduler

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	logger "github.com/inference-gateway/cli/internal/logger"
)

// ArtifactPollerOptions bundles dependencies for NewArtifactPoller.
type ArtifactPollerOptions struct {
	Runner           CommandRunner
	Repo             string // "<owner>/<name>", already resolved
	ConversationsDir string // local dir jsonl files are extracted into
	ArtifactsDir     string // local dir non-jsonl files (images, reports) are extracted into; empty skips them
	StatePath        string // cursor/attempts persistence file
	Interval         time.Duration
	InitialDelay     time.Duration
	MaxAttempts      int           // per artifact; then skipped permanently
	RateLimitBackoff time.Duration // pause after a rate-limited API call
}

// CommandRunner matches githubsetup.CommandRunner; redeclared locally so the
// poller can be constructed without importing githubsetup.
type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// ArtifactPoller periodically downloads conversation artifacts uploaded by
// GitHub-backed job runs and drops their jsonl files into local conversation
// storage. Lifecycle mirrors the heartbeat service.
// ponytail: raw jsonl drop only works for storage.type jsonl; parse-and-save via ConversationStorage if sqlite/postgres users ask.
type ArtifactPoller struct {
	opts ArtifactPollerOptions

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	running     atomic.Int32
	pausedUntil time.Time // rate-limit backoff deadline
	state       pollerState

	started bool
	mu      sync.Mutex
}

// pollerState is persisted across daemon restarts. Cursor is the highest
// artifact ID ever listed; Pending tracks download attempts for artifacts that
// have not been fully extracted yet.
type pollerState struct {
	Cursor  int64         `json:"cursor"`
	Pending map[int64]int `json:"pending,omitempty"`
}

type artifactList struct {
	Artifacts []artifactInfo `json:"artifacts"`
}

type artifactInfo struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Expired bool   `json:"expired"`
}

// NewArtifactPoller validates options and constructs a poller.
func NewArtifactPoller(opts ArtifactPollerOptions) (*ArtifactPoller, error) {
	if opts.Interval <= 0 {
		return nil, errors.New("artifact poller: interval must be > 0")
	}
	if opts.Repo == "" {
		return nil, errors.New("artifact poller: repo is required")
	}
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = 3
	}
	return &ArtifactPoller{opts: opts, state: pollerState{Pending: map[int64]int{}}}, nil
}

// Start launches the ticker goroutine. Calling Start more than once is a no-op.
func (p *ArtifactPoller) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.started {
		return nil
	}
	p.ctx, p.cancel = context.WithCancel(ctx)
	p.started = true
	p.loadState()

	logger.Info("github artifact poller started",
		"repo", p.opts.Repo,
		"interval", p.opts.Interval.String(),
		"conversations_dir", p.opts.ConversationsDir,
		"artifacts_dir", p.opts.ArtifactsDir,
	)

	p.wg.Add(1)
	go p.run()
	return nil
}

// Stop cancels the ticker and waits for an in-flight tick to finish.
func (p *ArtifactPoller) Stop(ctx context.Context) error {
	p.mu.Lock()
	if !p.started {
		p.mu.Unlock()
		return nil
	}
	p.cancel()
	p.mu.Unlock()

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		logger.Info("github artifact poller stopped")
		return nil
	case <-ctx.Done():
		return fmt.Errorf("artifact poller stop: %w", ctx.Err())
	}
}

func (p *ArtifactPoller) run() {
	defer p.wg.Done()

	if p.opts.InitialDelay > 0 {
		select {
		case <-time.After(p.opts.InitialDelay):
		case <-p.ctx.Done():
			return
		}
	}

	p.tickGuarded()

	ticker := time.NewTicker(p.opts.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.tickGuarded()
		case <-p.ctx.Done():
			return
		}
	}
}

func (p *ArtifactPoller) tickGuarded() {
	if !p.running.CompareAndSwap(0, 1) {
		return
	}
	defer p.running.Store(0)

	if time.Now().Before(p.pausedUntil) {
		return
	}
	if err := p.tick(p.ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		logger.Error("github artifact poll failed", "error", err)
	}
}

// tick lists artifacts, downloads new conversation artifacts (up to
// MaxAttempts each), and persists the advanced cursor.
func (p *ArtifactPoller) tick(ctx context.Context) error {
	out, err := p.runGH(ctx, ghCommandTimeout,
		"api", fmt.Sprintf("repos/%s/actions/artifacts?per_page=100", p.opts.Repo))
	if err != nil {
		if isRateLimited(out) {
			p.pausedUntil = time.Now().Add(p.opts.RateLimitBackoff)
			logger.Warn("github artifact poll rate-limited, backing off",
				"until", p.pausedUntil.Format(time.RFC3339))
			return nil
		}
		return fmt.Errorf("list artifacts: %s: %w", strings.TrimSpace(string(out)), err)
	}

	var list artifactList
	if err := json.Unmarshal(out, &list); err != nil {
		return fmt.Errorf("parse artifact list: %w", err)
	}

	changed := false
	for _, a := range list.Artifacts {
		if !strings.HasPrefix(a.Name, ArtifactNamePrefix) || a.Expired {
			continue
		}
		if a.ID > p.state.Cursor {
			p.state.Cursor = a.ID
			p.state.Pending[a.ID] = 0
			changed = true
		}
	}

	for id, attempts := range p.state.Pending {
		if attempts >= p.opts.MaxAttempts {
			logger.Warn("giving up on artifact after max attempts", "artifact_id", id, "attempts", attempts)
			delete(p.state.Pending, id)
			changed = true
			continue
		}
		rateLimited, err := p.download(ctx, id)
		if rateLimited {
			p.pausedUntil = time.Now().Add(p.opts.RateLimitBackoff)
			logger.Warn("github artifact download rate-limited, backing off",
				"until", p.pausedUntil.Format(time.RFC3339))
			break
		}
		changed = true
		if err != nil {
			p.state.Pending[id]++
			logger.Warn("artifact download failed", "artifact_id", id, "attempt", p.state.Pending[id], "error", err)
			continue
		}
		delete(p.state.Pending, id)
	}

	if changed {
		p.saveState()
	}
	return nil
}

// download fetches one artifact zip and extracts its files. The bool
// reports a rate-limited failure (which must not consume an attempt).
func (p *ArtifactPoller) download(ctx context.Context, id int64) (bool, error) {
	out, err := p.runGH(ctx, ghNetworkTimeout,
		"api", fmt.Sprintf("repos/%s/actions/artifacts/%d/zip", p.opts.Repo, id))
	if err != nil {
		if isRateLimited(out) {
			return true, err
		}
		return false, fmt.Errorf("download: %s: %w", firstLine(out), err)
	}
	return false, p.extract(out)
}

// extract writes every *.jsonl entry of the zip into the conversations dir
// and every non-jsonl entry into the artifacts dir (when set), skipping files
// that already exist so re-polls stay idempotent.
func (p *ArtifactPoller) extract(zipData []byte) error {
	r, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return fmt.Errorf("open artifact zip: %w", err)
	}
	if err := os.MkdirAll(p.opts.ConversationsDir, 0755); err != nil {
		return err
	}
	if p.opts.ArtifactsDir != "" {
		if err := os.MkdirAll(p.opts.ArtifactsDir, 0755); err != nil {
			return err
		}
	}
	for _, f := range r.File {
		name := filepath.Base(f.Name)
		if strings.HasSuffix(name, ".jsonl") {
			if err := p.extractTo(f, name, p.opts.ConversationsDir); err != nil {
				return err
			}
		} else if p.opts.ArtifactsDir != "" {
			if err := p.extractTo(f, name, p.opts.ArtifactsDir); err != nil {
				return err
			}
		}
	}
	return nil
}

// extractTo extracts one zip entry to destDir with zip-slip containment check
// and idempotency (skip if dest already exists).
func (p *ArtifactPoller) extractTo(f *zip.File, name, destDir string) error {
	dest := filepath.Join(destDir, filepath.Base(name))
	if !strings.HasPrefix(dest, filepath.Clean(destDir)+string(os.PathSeparator)) {
		return nil
	}
	if _, err := os.Stat(dest); err == nil {
		return nil
	}
	return extractFile(f, dest)
}

func extractFile(f *zip.File, dest string) error {
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("open %s in artifact zip: %w", f.Name, err)
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(rc)
	if err != nil {
		return fmt.Errorf("read %s from artifact zip: %w", f.Name, err)
	}
	return os.WriteFile(dest, data, 0644)
}

func (p *ArtifactPoller) runGH(ctx context.Context, timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return p.opts.Runner.Run(ctx, "gh", args...)
}

// loadState restores the cursor file; a missing or corrupt file starts fresh.
func (p *ArtifactPoller) loadState() {
	if p.opts.StatePath == "" {
		return
	}
	data, err := os.ReadFile(p.opts.StatePath)
	if err != nil {
		return
	}
	var st pollerState
	if err := json.Unmarshal(data, &st); err != nil {
		return
	}
	if st.Pending == nil {
		st.Pending = map[int64]int{}
	}
	p.state = st
}

// saveState persists the cursor atomically (tmp+rename); failures are logged,
// not fatal - the worst case is re-downloading already-extracted artifacts.
func (p *ArtifactPoller) saveState() {
	if p.opts.StatePath == "" {
		return
	}
	data, err := json.Marshal(p.state)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p.opts.StatePath), 0755); err != nil {
		logger.Warn("persist artifact poller state failed", "error", err)
		return
	}
	tmp := p.opts.StatePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		logger.Warn("persist artifact poller state failed", "error", err)
		return
	}
	if err := os.Rename(tmp, p.opts.StatePath); err != nil {
		logger.Warn("persist artifact poller state failed", "error", err)
	}
}

// isRateLimited sniffs a gh api failure for GitHub rate-limit responses.
func isRateLimited(out []byte) bool {
	s := strings.ToLower(string(out))
	return strings.Contains(s, "rate limit") || strings.Contains(s, "http 429")
}

func firstLine(out []byte) string {
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	return line
}
