package scheduler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"

	uuid "github.com/google/uuid"
	cron "github.com/robfig/cron/v3"
	yaml "gopkg.in/yaml.v3"

	agentrunner "github.com/inference-gateway/cli/internal/agent/application/agentrunner"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
)

// Service runs scheduled jobs inside the `infer daemon` process. Jobs are
// loaded from the configured ScheduledJobStorage, registered with a robfig/cron
// scheduler, and hot-reloaded by polling the storage and diffing (reconcile).
//
// On fire, a fresh `infer headless --session-id <uuid>` subprocess is spawned -
// every fire gets a brand-new session, so no context carries between runs.
// A RunRecord is persisted per fire (keyed by that session ID, so the run's
// conversation is discoverable from storage), and progress is emitted through
// the optional OnRunEvent hook. The scheduler knows nothing about channels -
// delivery is a subscriber concern (see services.ScheduleNotifier).
type Service struct {
	store      storage.ScheduledJobStorage
	runs       storage.ScheduledRunStorage
	onRunEvent func(scheddomain.ScheduledJob, scheddomain.RunEvent)
	cron       *cron.Cron
	parser     cron.Parser
	execCmd    agentrunner.ExecFunc
	binaryPath string

	mu       sync.Mutex
	entryIDs map[string]cron.EntryID

	pollStop context.CancelFunc
	pollWG   sync.WaitGroup

	started bool
}

// pollInterval is how often the scheduler reconciles its cron entries against
// the jobs in storage.
const pollInterval = 2 * time.Second

// maxRunRecords caps how many run records are retained across all jobs.
// ponytail: global cap; per-job retention if one chatty job ever starves the rest.
const maxRunRecords = 200

// Options bundles dependencies and configuration for NewService.
type Options struct {
	Store       storage.ScheduledJobStorage
	Runs        storage.ScheduledRunStorage
	OnRunEvent  func(scheddomain.ScheduledJob, scheddomain.RunEvent)
	ExecCommand agentrunner.ExecFunc
	BinaryPath  string
}

// NewService constructs a Service. Returns an error if required deps are missing.
func NewService(opts Options) (*Service, error) {
	if opts.Store == nil {
		return nil, errors.New("scheduler: Store is required")
	}
	if opts.Runs == nil {
		return nil, errors.New("scheduler: Runs is required")
	}
	parser := cron.NewParser(
		cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
	)
	return &Service{
		store:      opts.Store,
		runs:       opts.Runs,
		onRunEvent: opts.OnRunEvent,
		parser:     parser,
		execCmd:    opts.ExecCommand,
		binaryPath: opts.BinaryPath,
		entryIDs:   make(map[string]cron.EntryID),
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
func (s *Service) registerJob(job *scheddomain.ScheduledJob) error {
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
	logger.Info("scheduled job registered", "id", id, "cron", job.CronExpression)
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

// emit forwards a run event to the subscriber, if any.
func (s *Service) emit(job scheddomain.ScheduledJob, e scheddomain.RunEvent) {
	if s.onRunEvent != nil {
		s.onRunEvent(job, e)
	}
}

// fire runs a single execution of the job: persists a RunRecord, spawns
// `infer headless`, streams stdout lines through the OnRunEvent hook, and
// persists the run outcome plus the job's LastRun/LastError metadata.
func (s *Service) fire(job scheddomain.ScheduledJob) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	now := time.Now().UTC()
	job.LastRun = &now
	run := &scheddomain.RunRecord{
		SessionID: uuid.New().String(),
		JobID:     job.ID,
		Status:    scheddomain.RunStatusRunning,
		StartedAt: now,
	}
	logger.Info("firing scheduled job", "id", job.ID, "session_id", run.SessionID)
	s.saveRun(run)

	err := s.runAgent(ctx, job, run.SessionID)
	finished := time.Now().UTC()
	run.FinishedAt = &finished
	if err != nil {
		run.Status = scheddomain.RunStatusFailed
		run.Error = err.Error()
		job.LastError = err.Error()
		logger.Error("scheduled job execution failed", "id", job.ID, "error", err)
	} else {
		run.Status = scheddomain.RunStatusCompleted
		job.LastError = ""
	}
	s.saveRun(run)
	if pruneErr := s.runs.PruneRuns(context.Background(), maxRunRecords); pruneErr != nil {
		logger.Warn("failed to prune run records", "error", pruneErr)
	}
	s.emit(job, scheddomain.RunEvent{Done: true, Err: err})

	if job.RunOnce {
		// Unregister immediately - waiting for the next poll tick would let
		// short intervals (e.g. @every 2s) fire again before the delete lands.
		s.removeJob(job.ID)
		if err := s.store.DeleteJob(context.Background(), job.ID); err != nil {
			logger.Warn("failed to delete one-off scheduled job after fire", "id", job.ID, "error", err)
		} else {
			logger.Info("one-off scheduled job consumed and deleted", "id", job.ID)
		}
		return
	}
	s.persistRun(&job)
}

// saveRun persists a run record. Errors are only logged - a failed record
// write must not abort the run itself.
func (s *Service) saveRun(run *scheddomain.RunRecord) {
	if err := s.runs.SaveRun(context.Background(), run); err != nil {
		logger.Warn("failed to persist run record", "job_id", run.JobID, "session_id", run.SessionID, "error", err)
	}
}

// persistRun writes the updated LastRun/LastError back to disk. Errors are
// only logged - a failed metadata write should not crash the daemon.
func (s *Service) persistRun(job *scheddomain.ScheduledJob) {
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

// runAgent spawns `infer headless --session-id <sessionID> <prompt>` (via the
// shared agentrunner) and forwards each stdout line through the run-event hook.
func (s *Service) runAgent(ctx context.Context, job scheddomain.ScheduledJob, sessionID string) error {
	res, err := agentrunner.Run(ctx, agentrunner.Options{
		BinaryPath: s.binaryPath,
		Exec:       s.execCmd,
		SessionID:  sessionID,
		Prompt:     job.Prompt,
		Model:      job.Model,
		OnLine: func(line []byte) {
			s.emit(job, scheddomain.RunEvent{Line: line})
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
