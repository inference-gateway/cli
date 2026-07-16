package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	uuid "github.com/google/uuid"
	cron "github.com/robfig/cron/v3"
	yaml "gopkg.in/yaml.v3"

	domain "github.com/inference-gateway/cli/internal/domain"
	storage "github.com/inference-gateway/cli/internal/infra/storage"
	logger "github.com/inference-gateway/cli/internal/logger"
	agentrunner "github.com/inference-gateway/cli/internal/services/agentrunner"
)

// ChannelLookupFn returns the registered Channel for a name, or nil if unknown.
type ChannelLookupFn func(name string) domain.Channel

// Service runs scheduled jobs inside the channels-manager daemon. Jobs are
// loaded from the configured ScheduledJobStorage, registered with a robfig/cron
// scheduler, and hot-reloaded by polling the storage and diffing (reconcile).
//
// On fire, a fresh `infer agent --session-id <uuid>` subprocess is spawned -
// every fire gets a brand-new session, so no context carries between runs.
// Each assistant line emitted by the agent is forwarded to the configured
// channel/recipient via the in-process channel lookup.
type Service struct {
	store         storage.ScheduledJobStorage
	cron          *cron.Cron
	parser        cron.Parser
	channelLookup ChannelLookupFn
	execCmd       agentrunner.ExecFunc
	binaryPath    string

	mu       sync.Mutex
	entryIDs map[string]cron.EntryID

	pollStop context.CancelFunc
	pollWG   sync.WaitGroup

	started bool
}

// pollInterval is how often the scheduler reconciles its cron entries against
// the jobs in storage.
const pollInterval = 2 * time.Second

// Options bundles dependencies and configuration for NewService.
type Options struct {
	Store         storage.ScheduledJobStorage
	ChannelLookup ChannelLookupFn
	// ExecCommand defaults to exec.CommandContext when nil.
	ExecCommand agentrunner.ExecFunc
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
	parser := cron.NewParser(
		cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
	)
	return &Service{
		store:         opts.Store,
		parser:        parser,
		channelLookup: opts.ChannelLookup,
		execCmd:       opts.ExecCommand,
		binaryPath:    opts.BinaryPath,
		entryIDs:      make(map[string]cron.EntryID),
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

// Start initialises the cron scheduler, loads all jobs from storage, and begins
// polling storage for changes.
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return nil
	}
	s.cron = cron.New(cron.WithParser(s.parser))
	s.started = true
	s.mu.Unlock()

	s.cron.Start()
	s.startPoller(ctx)

	s.mu.Lock()
	jobCount := len(s.entryIDs)
	s.mu.Unlock()
	logger.Info("scheduler started", "jobs", jobCount)
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

	if s.pollStop != nil {
		s.pollStop()
	}
	s.pollWG.Wait()

	if c != nil {
		stopCtx := c.Stop()
		select {
		case <-stopCtx.Done():
		case <-ctx.Done():
		case <-time.After(30 * time.Second):
			logger.Warn("scheduler stop timed out waiting for in-flight jobs")
		}
	}
	logger.Info("scheduler stopped")
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
	logger.Info("scheduled job registered", "id", id, "cron", job.CronExpression, "channel", job.Channel)
	return nil
}

// removeJob unregisters the cron entry for a job ID, if any.
func (s *Service) removeJob(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if eid, ok := s.entryIDs[id]; ok {
		s.cron.Remove(eid)
		delete(s.entryIDs, id)
		logger.Info("scheduled job removed", "id", id)
	}
}

// fire runs a single execution of the job: spawns `infer agent`, streams
// stdout into channel messages, and persists run metadata back to the store.
func (s *Service) fire(job domain.ScheduledJob) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	logger.Info("firing scheduled job", "id", job.ID, "channel", job.Channel)

	now := time.Now().UTC()
	job.LastRun = &now

	ch := s.channelLookup(job.Channel)
	if ch == nil {
		job.LastError = fmt.Sprintf("channel %q is not registered", job.Channel)
		logger.Error("scheduled job: channel not found", "id", job.ID, "channel", job.Channel)
		s.persistRun(&job)
		return
	}

	var firstSendErr error
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
			logger.Error("failed to send scheduled-job output", "id", job.ID, "channel", job.Channel, "error", err)
			if firstSendErr == nil {
				firstSendErr = err
			}
		}
	}

	switch err := s.runAgent(ctx, job, sendFn); {
	case err != nil:
		job.LastError = err.Error()
		logger.Error("scheduled job execution failed", "id", job.ID, "error", err)
		sendFn(fmt.Sprintf("⚠️ Scheduled task %q failed: %v", displayName(&job), err))
	case firstSendErr != nil:
		job.LastError = fmt.Sprintf("delivery to %s/%s failed: %v", job.Channel, job.RecipientID, firstSendErr)
	default:
		job.LastError = ""
	}

	if job.RunOnce {
		if err := s.store.DeleteJob(context.Background(), job.ID); err != nil {
			logger.Warn("failed to delete one-off scheduled job after fire", "id", job.ID, "error", err)
		} else {
			logger.Info("one-off scheduled job consumed and deleted", "id", job.ID)
		}
		return
	}
	s.persistRun(&job)
}

// persistRun writes the updated LastRun/LastError back to disk. Errors are
// only logged - a failed metadata write should not crash the daemon.
func (s *Service) persistRun(job *domain.ScheduledJob) {
	current, err := s.store.LoadJob(context.Background(), job.ID)
	if err != nil {
		// Job may have been deleted concurrently; ignore.
		return
	}
	current.LastRun = job.LastRun
	current.LastError = job.LastError
	if err := s.store.SaveJob(context.Background(), current); err != nil {
		logger.Warn("failed to persist scheduled job run state", "id", job.ID, "error", err)
	}
}

// runAgent spawns `infer agent --session-id <new-uuid> <prompt>` (via the shared
// agentrunner) and forwards each formatted assistant line through sendFn.
func (s *Service) runAgent(ctx context.Context, job domain.ScheduledJob, sendFn func(string)) error {
	res, err := agentrunner.Run(ctx, agentrunner.Options{
		BinaryPath: s.binaryPath,
		Exec:       s.execCmd,
		SessionID:  uuid.New().String(),
		Prompt:     job.Prompt,
		Model:      job.Model,
		OnLine: func(line []byte) {
			if msg := formatAgentLine(line); msg != "" {
				sendFn(msg)
			}
		},
	})
	if err != nil {
		if res.Stderr != "" {
			return fmt.Errorf("%w: %s", err, strings.TrimSpace(res.Stderr))
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

// startPoller reconciles cron entries against storage once synchronously (so
// jobs are registered when Start returns), then keeps reconciling every
// pollInterval until ctx is cancelled or Stop is called.
//
// ponytail: 2s list-and-diff poll across all backends; per-backend push
// notifications if anything ever needs sub-second reload (cron granularity is
// one minute).
func (s *Service) startPoller(ctx context.Context) {
	pctx, cancel := context.WithCancel(ctx)
	s.pollStop = cancel

	known := make(map[string]string)
	s.reconcile(pctx, known)

	s.pollWG.Go(func() {
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-pctx.Done():
				return
			case <-ticker.C:
				s.reconcile(pctx, known)
			}
		}
	})
}

// reconcile diffs the jobs in storage against the known fingerprints:
// new/changed jobs are (re-)registered, vanished jobs are removed. The
// fingerprint is the job's marshaled bytes, so hand-edited YAML files on the
// jsonl backend are picked up too; unchanged jobs are never re-registered, so
// the poll does not reset @every schedules.
func (s *Service) reconcile(ctx context.Context, known map[string]string) {
	jobs, err := s.store.ListJobs(ctx)
	if err != nil {
		logger.Warn("failed to list scheduled jobs", "error", err)
		return
	}

	seen := make(map[string]bool, len(jobs))
	for _, job := range jobs {
		data, err := yaml.Marshal(job)
		if err != nil {
			logger.Warn("failed to fingerprint scheduled job", "id", job.ID, "error", err)
			continue
		}
		fingerprint := string(data)
		seen[job.ID] = true
		if known[job.ID] == fingerprint {
			continue
		}
		if err := s.registerJob(job); err != nil {
			logger.Warn("failed to register scheduled job", "id", job.ID, "error", err)
			continue
		}
		known[job.ID] = fingerprint
	}

	for id := range known {
		if !seen[id] {
			s.removeJob(id)
			delete(known, id)
		}
	}
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
