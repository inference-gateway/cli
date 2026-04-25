package scheduler

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	uuid "github.com/google/uuid"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"

	fsnotify "github.com/fsnotify/fsnotify"
	cron "github.com/robfig/cron/v3"
)

// ChannelLookupFn returns the registered Channel for a name, or nil if unknown.
type ChannelLookupFn func(name string) domain.Channel

// ExecCommandFn matches exec.CommandContext. Exposed for tests.
type ExecCommandFn func(ctx context.Context, name string, args ...string) *exec.Cmd

// Service runs scheduled jobs inside the channels-manager daemon. Jobs are
// loaded from the on-disk Store, registered with a robfig/cron scheduler, and
// hot-reloaded when the storage directory changes (via fsnotify).
//
// On fire, a fresh `infer agent --session-id <uuid>` subprocess is spawned —
// every fire gets a brand-new session, so no context carries between runs.
// Each assistant line emitted by the agent is forwarded to the configured
// channel/recipient via the in-process channel lookup.
type Service struct {
	store         *Store
	cron          *cron.Cron
	parser        cron.Parser
	channelLookup ChannelLookupFn
	execCmd       ExecCommandFn
	binaryPath    string

	mu       sync.Mutex
	entryIDs map[string]cron.EntryID

	watcher    *fsnotify.Watcher
	watchCtx   context.Context
	watchStop  context.CancelFunc
	watcherWG  sync.WaitGroup
	debounceMu sync.Mutex
	debounce   map[string]*time.Timer

	started bool
}

// Options bundles dependencies and configuration for NewService.
type Options struct {
	Store         *Store
	ChannelLookup ChannelLookupFn
	// ExecCommand defaults to exec.CommandContext when nil.
	ExecCommand ExecCommandFn
	// BinaryPath defaults to os.Args[0] (current binary) when empty.
	BinaryPath string
}

// NewService constructs a Service. Returns an error if required deps are missing.
func NewService(opts Options) (*Service, error) {
	if opts.Store == nil {
		return nil, errors.New("scheduler: Store is required")
	}
	if opts.ChannelLookup == nil {
		return nil, errors.New("scheduler: ChannelLookup is required")
	}
	execFn := opts.ExecCommand
	if execFn == nil {
		execFn = exec.CommandContext
	}
	bin := opts.BinaryPath
	if bin == "" {
		bin = os.Args[0]
	}
	parser := cron.NewParser(
		cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
	)
	return &Service{
		store:         opts.Store,
		parser:        parser,
		channelLookup: opts.ChannelLookup,
		execCmd:       execFn,
		binaryPath:    bin,
		entryIDs:      make(map[string]cron.EntryID),
		debounce:      make(map[string]*time.Timer),
	}, nil
}

// ParseCron exposes the same parser the service uses, so the Schedule tool
// can validate cron expressions identically before persisting them.
func (s *Service) ParseCron(expr string) error {
	_, err := s.parser.Parse(expr)
	return err
}

// ParseCron is a package-level helper for callers that don't have a Service
// instance yet (e.g. validation in the Schedule tool's Validate method).
func ParseCron(expr string) error {
	parser := cron.NewParser(
		cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
	)
	_, err := parser.Parse(expr)
	return err
}

// Start initialises the cron scheduler, loads all jobs from disk, and begins
// watching the storage directory for changes.
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return nil
	}
	s.cron = cron.New(cron.WithParser(s.parser))
	s.started = true
	s.mu.Unlock()

	if err := s.LoadJobs(); err != nil {
		logger.Error("Failed to load scheduled jobs", "error", err)
	}

	s.cron.Start()

	if err := s.startWatcher(ctx); err != nil {
		logger.Error("Schedule watcher failed to start; jobs will only be loaded once", "error", err)
	}

	s.mu.Lock()
	jobCount := len(s.entryIDs)
	s.mu.Unlock()
	logger.Info("Scheduler started", "dir", s.store.Dir(), "jobs", jobCount)
	return nil
}

// Stop halts the watcher and waits for in-flight cron entries to finish (up
// to the deadline embedded in ctx, if any).
func (s *Service) Stop(ctx context.Context) error {
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return nil
	}
	s.started = false
	c := s.cron
	s.mu.Unlock()

	if s.watchStop != nil {
		s.watchStop()
	}
	s.watcherWG.Wait()
	if s.watcher != nil {
		_ = s.watcher.Close()
		s.watcher = nil
	}

	if c != nil {
		stopCtx := c.Stop()
		select {
		case <-stopCtx.Done():
		case <-ctx.Done():
		case <-time.After(30 * time.Second):
			logger.Warn("Scheduler stop timed out waiting for in-flight jobs")
		}
	}
	logger.Info("Scheduler stopped")
	return nil
}

// LoadJobs replaces all currently-registered cron entries with the jobs
// currently on disk. Safe to call repeatedly.
func (s *Service) LoadJobs() error {
	jobs, errs := s.store.List()
	for _, e := range errs {
		logger.Warn("Skipping invalid schedule file", "error", e)
	}

	s.mu.Lock()
	for id, eid := range s.entryIDs {
		s.cron.Remove(eid)
		delete(s.entryIDs, id)
	}
	s.mu.Unlock()

	for _, job := range jobs {
		if err := s.registerJob(job); err != nil {
			logger.Warn("Failed to register scheduled job", "id", job.ID, "error", err)
		}
	}
	return nil
}

// registerJob adds (or replaces) a cron entry for the given job. The closure
// captures job by value so concurrent edits to the file don't race the
// running fire.
func (s *Service) registerJob(job *domain.ScheduledJob) error {
	if job == nil || job.ID == "" || job.CronExpression == "" {
		return errors.New("invalid job: missing ID or cron expression")
	}

	id := job.ID
	captured := *job
	eid, err := s.cron.AddFunc(job.CronExpression, func() {
		s.fire(captured)
	})
	if err != nil {
		return fmt.Errorf("cron parse: %w", err)
	}

	s.mu.Lock()
	if old, ok := s.entryIDs[id]; ok {
		s.cron.Remove(old)
	}
	s.entryIDs[id] = eid
	s.mu.Unlock()
	logger.Info("Scheduled job registered", "id", id, "cron", job.CronExpression, "channel", job.Channel)
	return nil
}

// removeJob unregisters the cron entry for a job ID, if any.
func (s *Service) removeJob(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if eid, ok := s.entryIDs[id]; ok {
		s.cron.Remove(eid)
		delete(s.entryIDs, id)
		logger.Info("Scheduled job removed", "id", id)
	}
}

// fire runs a single execution of the job: spawns `infer agent`, streams
// stdout into channel messages, and persists run metadata back to the store.
func (s *Service) fire(job domain.ScheduledJob) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	logger.Info("Firing scheduled job", "id", job.ID, "channel", job.Channel)

	now := time.Now().UTC()
	job.LastRun = &now

	ch := s.channelLookup(job.Channel)
	if ch == nil {
		job.LastError = fmt.Sprintf("channel %q is not registered", job.Channel)
		logger.Error("Scheduled job: channel not found", "id", job.ID, "channel", job.Channel)
		s.persistRun(&job)
		return
	}

	sendFn := func(content string) {
		if content == "" {
			return
		}
		out := domain.OutboundMessage{
			ChannelName: job.Channel,
			RecipientID: job.RecipientID,
			Content:     content,
			Timestamp:   time.Now(),
		}
		if err := ch.Send(ctx, out); err != nil {
			logger.Error("Failed to send scheduled-job output", "id", job.ID, "channel", job.Channel, "error", err)
		}
	}

	if err := s.runAgent(ctx, job, sendFn); err != nil {
		job.LastError = err.Error()
		logger.Error("Scheduled job execution failed", "id", job.ID, "error", err)
		sendFn(fmt.Sprintf("⚠️ Scheduled task %q failed: %v", displayName(&job), err))
	} else {
		job.LastError = ""
	}
	s.persistRun(&job)
}

// persistRun writes the updated LastRun/LastError back to disk. Errors are
// only logged — a failed metadata write should not crash the daemon.
func (s *Service) persistRun(job *domain.ScheduledJob) {
	current, err := s.store.Load(job.ID)
	if err != nil {
		// Job may have been deleted concurrently; ignore.
		return
	}
	current.LastRun = job.LastRun
	current.LastError = job.LastError
	if err := s.store.Save(current); err != nil {
		logger.Warn("Failed to persist scheduled job run state", "id", job.ID, "error", err)
	}
}

// runAgent spawns `infer agent --session-id <new-uuid> <prompt>` and forwards
// each formatted assistant line through sendFn.
func (s *Service) runAgent(ctx context.Context, job domain.ScheduledJob, sendFn func(string)) error {
	sessionID := uuid.New().String()
	args := []string{"agent", "--session-id", sessionID}
	if job.Model != "" {
		args = append(args, "--model", job.Model)
	}
	args = append(args, job.Prompt)

	cmd := s.execCmd(ctx, s.binaryPath, args...)
	cmd.Env = os.Environ()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	var stderrBuf bytes.Buffer
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start agent: %w", err)
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		if msg := formatAgentLine(line); msg != "" {
			sendFn(msg)
		}
	}

	if err := cmd.Wait(); err != nil {
		if stderrBuf.Len() > 0 {
			return fmt.Errorf("%w: %s", err, strings.TrimSpace(stderrBuf.String()))
		}
		return err
	}
	return nil
}

// formatAgentLine is a near-duplicate of services.formatAgentMessage. It's
// kept here to avoid an import cycle (services -> services/scheduler ->
// services). Behaviour must stay in sync with the original.
func formatAgentLine(line []byte) string {
	var msg map[string]any
	if err := json.Unmarshal(line, &msg); err != nil {
		return ""
	}
	if _, isStatus := msg["type"]; isStatus {
		return ""
	}
	role, _ := msg["role"].(string)
	switch role {
	case "assistant":
		content, _ := msg["content"].(string)
		if tools, ok := msg["tools"].([]any); ok && len(tools) > 0 {
			toolNames := make([]string, 0, len(tools))
			for _, t := range tools {
				if name, ok := t.(string); ok {
					toolNames = append(toolNames, name)
				}
			}
			toolMsg := fmt.Sprintf("🔧 Using tool: %s", strings.Join(toolNames, ", "))
			if content != "" {
				return content + "\n\n" + toolMsg
			}
			return toolMsg
		}
		if content != "" {
			return content
		}
	case "tool":
		return ""
	}
	return ""
}

func displayName(job *domain.ScheduledJob) string {
	if job.Name != "" {
		return job.Name
	}
	return job.ID
}

// startWatcher begins watching the storage directory and keeps cron entries
// in sync with file changes. Operates with a small debounce per file so
// editor-style "write tmp + rename" sequences only trigger one reload.
func (s *Service) startWatcher(ctx context.Context) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create watcher: %w", err)
	}
	if err := w.Add(s.store.Dir()); err != nil {
		_ = w.Close()
		return fmt.Errorf("watch dir: %w", err)
	}
	s.watcher = w
	wctx, cancel := context.WithCancel(ctx)
	s.watchCtx = wctx
	s.watchStop = cancel

	s.watcherWG.Add(1)
	go func() {
		defer s.watcherWG.Done()
		for {
			select {
			case <-wctx.Done():
				return
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				s.handleFSEvent(ev)
			case err, ok := <-w.Errors:
				if !ok {
					return
				}
				logger.Warn("schedule watcher error", "error", err)
			}
		}
	}()
	return nil
}

func (s *Service) handleFSEvent(ev fsnotify.Event) {
	if filepath.Ext(ev.Name) != ".yaml" {
		return
	}
	id := IDFromPath(ev.Name)
	if id == "" {
		return
	}

	if ev.Op&fsnotify.Remove != 0 {
		s.removeJob(id)
		return
	}

	s.scheduleReload(id, ev.Name)
}

// scheduleReload debounces back-to-back events for the same file.
func (s *Service) scheduleReload(id, path string) {
	s.debounceMu.Lock()
	defer s.debounceMu.Unlock()
	if t, ok := s.debounce[id]; ok {
		t.Stop()
	}
	s.debounce[id] = time.AfterFunc(150*time.Millisecond, func() {
		job, err := s.store.LoadFromPath(path)
		if err != nil {
			logger.Warn("Failed to reload schedule file", "id", id, "error", err)
			return
		}
		if err := s.registerJob(job); err != nil {
			logger.Warn("Failed to register reloaded schedule", "id", id, "error", err)
		}
	})
}

// JobIDs returns the set of currently-registered job IDs (test helper).
func (s *Service) JobIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.entryIDs))
	for id := range s.entryIDs {
		out = append(out, id)
	}
	return out
}
