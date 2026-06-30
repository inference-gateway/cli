// Package jobs provides the Supervisor: the single, long-lived owner of every
// background-work monitor goroutine in an agent session. It is the unified
// replacement for the per-request A2ATaskPoller and SubagentPoller and the
// per-shell monitor that used to live in BackgroundShellService. Submit spawns
// one monitor goroutine per job at creation (no discovery ticker), runs the
// shared finish-once logic, and tracks live and recently-finished jobs so the
// task view and status line can report them. It lives in a services subpackage
// (not top-level services) so the tools package can submit jobs to it without an
// import cycle - it depends only on domain.
package jobs

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	sdk "github.com/inference-gateway/sdk"

	constants "github.com/inference-gateway/cli/internal/constants"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// Supervisor owns and monitors all background jobs for a session.
type Supervisor struct {
	messageQueue     domain.MessageQueue
	conversationRepo domain.ConversationRepository
	notifier         domain.UINotifier

	mu              sync.RWMutex
	jobs            map[string]*supervised
	retentionByKind map[domain.JobKind]int

	wg          sync.WaitGroup
	stopOnce    sync.Once
	stopCleanup chan struct{}
	stopped     bool
}

// supervised is the supervisor's per-job bookkeeping: the job plus its current
// status and the cancel that stops its Run goroutine.
type supervised struct {
	job         domain.BackgroundJob
	meta        domain.JobMeta
	status      domain.JobStatus
	completedAt *time.Time
	lastNote    string
	cancel      context.CancelFunc
	warnedLong  bool
}

// NewSupervisor constructs a supervisor. messageQueue and conversationRepo are
// long-lived singletons used to deliver finished jobs' results back onto the
// conversation; either may be nil (the supervisor degrades to event-only).
// notifier is the single UI ingress used to push DrainQueueEvent (so landed work
// drains promptly) and BackgroundTasksChangedEvent (so the task view refreshes);
// a nil notifier degrades to no UI pushes.
func NewSupervisor(messageQueue domain.MessageQueue, conversationRepo domain.ConversationRepository, notifier domain.UINotifier) *Supervisor {
	if notifier == nil {
		notifier = domain.NoopUINotifier{}
	}
	return &Supervisor{
		messageQueue:     messageQueue,
		conversationRepo: conversationRepo,
		notifier:         notifier,
		jobs:             make(map[string]*supervised),
		retentionByKind:  make(map[domain.JobKind]int),
		stopCleanup:      make(chan struct{}),
	}
}

// notify pushes an event to the UI loop. NewSupervisor defaults a nil notifier to
// NoopUINotifier, so s.notifier is always non-nil here.
func (s *Supervisor) notify(event any) {
	s.notifier.Notify(event)
}

// SetConversationRepo wires the conversation repository used to format finished
// jobs' results. It is set after construction because the repo is built later in
// the container than the supervisor.
func (s *Supervisor) SetConversationRepo(repo domain.ConversationRepository) {
	s.mu.Lock()
	s.conversationRepo = repo
	s.mu.Unlock()
}

// SetRetentionCount caps how many terminal jobs of a kind are kept for the task
// view. When a job finishes, the oldest terminal jobs of its kind beyond max are
// reaped immediately (their Close teardown runs). max <= 0 means unbounded -
// terminal jobs are kept until the time-based Cleanup sweep. Set once per kind at
// startup, before jobs are submitted.
func (s *Supervisor) SetRetentionCount(kind domain.JobKind, max int) {
	s.mu.Lock()
	s.retentionByKind[kind] = max
	s.mu.Unlock()
}

// Start launches the periodic cleanup that reaps finished jobs older than
// retention. interval is how often to sweep. Call once; Stop ends it.
func (s *Supervisor) Start(interval, retention time.Duration) {
	if interval <= 0 {
		interval = 10 * time.Minute
	}
	if retention <= 0 {
		retention = 10 * time.Minute
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-s.stopCleanup:
				return
			case <-ticker.C:
				if reaped := s.Cleanup(retention); reaped > 0 {
					logger.Info("reaped finished background jobs", "count", reaped, "retention", retention)
				}
			}
		}
	}()
}

// Submit registers a job and spawns its monitor goroutine. Duplicate IDs and
// submissions after Stop are ignored.
func (s *Supervisor) Submit(job domain.BackgroundJob) {
	if job == nil {
		return
	}
	meta := job.Meta()

	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	var toClose domain.BackgroundJob
	if existing, exists := s.jobs[meta.ID]; exists {
		if !existing.status.IsTerminal() {
			s.mu.Unlock()
			logger.Warn("background job already running, ignoring duplicate submit", "id", meta.ID, "kind", meta.Kind)
			return
		}

		toClose = existing.job
		delete(s.jobs, meta.ID)
	}
	ctx, cancel := context.WithCancel(context.Background())
	sj := &supervised{job: job, meta: meta, status: domain.JobRunning, cancel: cancel}
	s.jobs[meta.ID] = sj
	s.wg.Add(1)
	s.mu.Unlock()

	if toClose != nil {
		toClose.Close()
	}

	go s.monitor(ctx, sj)
	s.notify(domain.BackgroundTasksChangedEvent{})
}

// monitor runs one job to completion and then delivers its outcome. A panicking
// job is converted to a failure so one bad job can never take down the session.
func (s *Supervisor) monitor(ctx context.Context, sj *supervised) {
	defer s.wg.Done()

	result := func() (r domain.ToolExecutionResult) {
		defer func() {
			if p := recover(); p != nil {
				logger.Error("background job panicked", "id", sj.meta.ID, "kind", sj.meta.Kind, "panic", p)
				r = domain.ToolExecutionResult{Success: false, Error: fmt.Sprintf("job panicked: %v", p)}
			}
		}()
		return sj.job.Run(ctx, func(sig domain.JobSignal) { s.onSignal(sj, sig) })
	}()

	s.finish(sj, result)
}

// onSignal records a job's intermediate status and, when the signal asks for it,
// lands the note on the shared queue. enqueue pushes a DrainQueueEvent so the
// idle agent picks the note up; the supervisor never touches a per-request
// channel. A BackgroundTasksChangedEvent refreshes the task view's last-note.
func (s *Supervisor) onSignal(sj *supervised, sig domain.JobSignal) {
	if sig.Note != "" {
		s.mu.Lock()
		sj.lastNote = sig.Note
		s.mu.Unlock()
		s.notify(domain.BackgroundTasksChangedEvent{})
	}

	if sig.Enqueue && sig.Note != "" {
		s.enqueue(sig.Note)
	}
}

// finish marks a job terminal and lands its result on the shared queue (unless
// the job is Silent and delivered its own per-turn notes). The entry is KEPT
// (terminal) so the task view can show it; Cleanup reaps it after the retention
// window. enqueue pushes a DrainQueueEvent so the agent picks up the note, and a
// BackgroundTasksChangedEvent refreshes the task view's status.
func (s *Supervisor) finish(sj *supervised, result domain.ToolExecutionResult) {
	now := time.Now()
	status := domain.JobCompleted
	if !result.Success {
		status = domain.JobFailed
	}

	s.mu.Lock()
	sj.status = status
	sj.completedAt = &now
	evicted := s.evictOverCapLocked(sj.meta.Kind)
	s.mu.Unlock()

	for _, v := range evicted {
		v.job.Close()
	}

	s.notify(domain.BackgroundTasksChangedEvent{})

	if !sj.meta.Silent {
		res := result
		s.enqueue(s.formatResult(sj.job, sj.meta, &res))
	}
}

// evictOverCapLocked drops the oldest terminal jobs of kind beyond its retention
// count and returns them for teardown. The caller MUST hold s.mu and MUST call
// Close on each returned job AFTER releasing the lock (Close can kill a pane or
// touch the filesystem). A retention count of 0 (or negative) means unbounded.
// Running jobs are never candidates, so a live interactive subagent is never
// reaped by a burst of finished headless ones.
func (s *Supervisor) evictOverCapLocked(kind domain.JobKind) []*supervised {
	max := s.retentionByKind[kind]
	if max <= 0 {
		return nil
	}

	terminal := make([]*supervised, 0, len(s.jobs))
	for _, sj := range s.jobs {
		if sj.meta.Kind == kind && sj.status.IsTerminal() {
			terminal = append(terminal, sj)
		}
	}
	if len(terminal) <= max {
		return nil
	}

	slices.SortFunc(terminal, func(a, b *supervised) int {
		return terminalTime(a).Compare(terminalTime(b))
	})

	evicted := terminal[:len(terminal)-max]
	for _, sj := range evicted {
		delete(s.jobs, sj.meta.ID)
	}
	return evicted
}

// terminalTime is when a job reached a terminal state, used to order retention
// eviction and time-based cleanup (oldest first). It falls back to StartedAt
// when a job has no recorded completion time.
func terminalTime(sj *supervised) time.Time {
	if sj.completedAt != nil {
		return *sj.completedAt
	}
	return sj.meta.StartedAt
}

// formatResult renders a finished job's outcome as the user-role message the
// agent will read on its next turn: a uniform "[Kind verb: label]" header plus a
// body the job formats itself (JobNotifier) or, by default, the domain-formatted
// tool result.
func (s *Supervisor) formatResult(job domain.BackgroundJob, meta domain.JobMeta, result *domain.ToolExecutionResult) string {
	var body string
	switch n := asNotifier(job); {
	case n != nil:
		body = n.Notification(*result)
	case s.conversationRepo != nil:
		body = s.conversationRepo.FormatToolResultForLLM(result)
	default:
		body = result.Error
	}
	label := meta.Label
	if label == "" {
		label = meta.ID
	}
	verb := "Completed"
	if !result.Success {
		verb = "Failed"
	}
	return fmt.Sprintf("[%s %s: %s]\n\n%s", kindLabel(meta.Kind), verb, label, body)
}

// asNotifier returns the job as a JobNotifier if it implements one.
func asNotifier(job domain.BackgroundJob) domain.JobNotifier {
	if n, ok := job.(domain.JobNotifier); ok {
		return n
	}
	return nil
}

// enqueue lands content on the shared message queue as a user-role message and
// pushes a DrainQueueEvent so an idle agent on the chat view starts a turn to
// read it. The gate (HandleDrainQueueEvent) drops the event when the agent is
// busy or off-chat; that work is then picked up at the next turn completion or on
// returning to chat.
func (s *Supervisor) enqueue(content string) {
	if s.messageQueue == nil {
		return
	}
	s.messageQueue.Enqueue(sdk.Message{
		Role:    sdk.User,
		Content: sdk.NewMessageContent(content),
	}, "system")
	s.notify(domain.DrainQueueEvent{})
}

// Wind delivers a graceful wind-down or hard stop to a single running job by id.
func (s *Supervisor) Wind(id string, sig domain.WindSignal) error {
	s.mu.RLock()
	sj := s.jobs[id]
	s.mu.RUnlock()
	if sj == nil {
		return fmt.Errorf("background job not found: %s", id)
	}
	return s.wind(sj, sig)
}

// WindAll broadcasts a signal to every running job. Graceful shutdown calls
// WindAll(WindWrapUp), waits a grace window, then WindAll(WindStop).
func (s *Supervisor) WindAll(sig domain.WindSignal) {
	s.mu.RLock()
	targets := make([]*supervised, 0, len(s.jobs))
	for _, sj := range s.jobs {
		if sj.status == domain.JobRunning {
			targets = append(targets, sj)
		}
	}
	s.mu.RUnlock()

	for _, sj := range targets {
		if err := s.wind(sj, sig); err != nil {
			logger.Warn("wind signal failed", "id", sj.meta.ID, "signal", sig.String(), "error", err)
		}
	}
}

func (s *Supervisor) wind(sj *supervised, sig domain.WindSignal) error {
	err := sj.job.Wind(context.Background(), sig)
	if sig == domain.WindStop && sj.cancel != nil {
		sj.cancel() // backstop: unblock Run even if the job ignored the signal
	}
	return err
}

// Snapshot returns a copy of all tracked jobs (running and recently finished)
// for the task view.
func (s *Supervisor) Snapshot() []domain.TrackedJob {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]domain.TrackedJob, 0, len(s.jobs))
	for _, sj := range s.jobs {
		out = append(out, domain.TrackedJob{
			Meta:        sj.meta,
			Status:      sj.status,
			CompletedAt: sj.completedAt,
			LastNote:    sj.lastNote,
		})
	}
	return out
}

// CountRunning returns the number of jobs still running, optionally filtered to
// one kind. Pass "" for all kinds.
func (s *Supervisor) CountRunning(kind domain.JobKind) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	n := 0
	for _, sj := range s.jobs {
		if sj.status != domain.JobRunning {
			continue
		}
		if kind != "" && sj.meta.Kind != kind {
			continue
		}
		n++
	}
	return n
}

// Cleanup reaps finished jobs whose terminal timestamp is older than olderThan,
// running each one's Wind(WindStop) teardown (kill pane, remove temp files)
// before dropping it. Running jobs are never reaped. The same sweep emits a
// one-time "job running long" warning for any still-running job that has exceeded
// JobRunningLongThreshold. Returns the number removed.
func (s *Supervisor) Cleanup(olderThan time.Duration) int {
	now := time.Now()
	cutoff := now.Add(-olderThan)
	longCutoff := now.Add(-constants.JobRunningLongThreshold)

	s.mu.Lock()
	var reaped []*supervised
	var longRunning []domain.JobMeta
	for id, sj := range s.jobs {
		if !sj.status.IsTerminal() {
			if !sj.warnedLong && sj.meta.StartedAt.Before(longCutoff) {
				sj.warnedLong = true
				longRunning = append(longRunning, sj.meta)
			}
			continue
		}
		if terminalTime(sj).After(cutoff) {
			continue
		}
		reaped = append(reaped, sj)
		delete(s.jobs, id)
	}
	s.mu.Unlock()

	for _, sj := range reaped {
		sj.job.Close()
	}
	for _, m := range longRunning {
		logger.Warn("job running long",
			"kind", string(m.Kind),
			"label", m.Label,
			"elapsed_s", int(now.Sub(m.StartedAt).Seconds()),
		)
	}
	return len(reaped)
}

// Stop cancels every running job and waits for all monitor goroutines to exit.
// Idempotent.
func (s *Supervisor) Stop() {
	s.stopOnce.Do(func() {
		close(s.stopCleanup)
		s.mu.Lock()
		s.stopped = true
		s.mu.Unlock()
		s.WindAll(domain.WindStop)
	})
	s.wg.Wait()
}

// kindLabel renders a job kind for completion notifications.
func kindLabel(kind domain.JobKind) string {
	switch kind {
	case domain.JobKindA2A:
		return "A2A Task"
	case domain.JobKindShell:
		return "Background Shell"
	case domain.JobKindSubagent:
		return "Subagent"
	default:
		return "Background Job"
	}
}
