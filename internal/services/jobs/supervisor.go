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
	"sync"
	"time"

	sdk "github.com/inference-gateway/sdk"

	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// Supervisor owns and monitors all background jobs for a session.
type Supervisor struct {
	messageQueue     domain.MessageQueue
	conversationRepo domain.ConversationRepository

	mu   sync.RWMutex
	jobs map[string]*supervised
	sink *requestSink

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
}

// requestSink is where monitors deliver chat events and agent wake-ups for the
// request that is currently bound. It is nil between requests, so a completion
// that lands after a request tore down drops its event instead of sending on a
// closed channel. See BindRequest.
type requestSink struct {
	eventChan      chan<- domain.ChatEvent
	agentEventChan chan<- domain.AgentEvent
	requestID      string
}

// NewSupervisor constructs a supervisor. messageQueue and conversationRepo are
// long-lived singletons used to deliver finished jobs' results back onto the
// conversation; either may be nil (the supervisor degrades to event-only).
func NewSupervisor(messageQueue domain.MessageQueue, conversationRepo domain.ConversationRepository) *Supervisor {
	return &Supervisor{
		messageQueue:     messageQueue,
		conversationRepo: conversationRepo,
		jobs:             make(map[string]*supervised),
		stopCleanup:      make(chan struct{}),
	}
}

// SetConversationRepo wires the conversation repository used to format finished
// jobs' results. It is set after construction because the repo is built later in
// the container than the supervisor.
func (s *Supervisor) SetConversationRepo(repo domain.ConversationRepository) {
	s.mu.Lock()
	s.conversationRepo = repo
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

// BindRequest points the supervisor's event sink at one request's chat-event and
// agent-event channels and returns a release func. Monitors emit through the
// bound sink; after release() the sink is cleared so later completions drop their
// events. release MUST be called before the request closes its event channels;
// because emit holds the read lock for the duration of its non-blocking send and
// release takes the write lock, no send can be in flight once release returns -
// closing the channels afterwards is race-free.
func (s *Supervisor) BindRequest(
	eventChan chan<- domain.ChatEvent,
	requestID string,
	agentEventChan chan<- domain.AgentEvent,
) (release func()) {
	s.mu.Lock()
	s.sink = &requestSink{eventChan: eventChan, agentEventChan: agentEventChan, requestID: requestID}
	s.mu.Unlock()

	return func() {
		s.mu.Lock()
		if s.sink != nil && s.sink.requestID == requestID {
			s.sink = nil
		}
		s.mu.Unlock()
	}
}

// SetAgentEventChannel updates only the agent wake-up channel on the current
// binding (the agent is constructed after BindRequest, so its event channel
// arrives later). No-op if nothing is bound.
func (s *Supervisor) SetAgentEventChannel(ch chan<- domain.AgentEvent) {
	s.mu.Lock()
	if s.sink != nil {
		s.sink.agentEventChan = ch
	}
	s.mu.Unlock()
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
	if existing, exists := s.jobs[meta.ID]; exists {
		if !existing.status.IsTerminal() {
			s.mu.Unlock()
			logger.Warn("background job already running, ignoring duplicate submit", "id", meta.ID, "kind", meta.Kind)
			return
		}

		existing.job.Close()
		delete(s.jobs, meta.ID)
	}
	ctx, cancel := context.WithCancel(context.Background())
	sj := &supervised{job: job, meta: meta, status: domain.JobRunning, cancel: cancel}
	s.jobs[meta.ID] = sj
	s.wg.Add(1)
	s.mu.Unlock()

	s.dispatch(func(reqID string) domain.ChatEvent {
		return domain.BackgroundJobEvent{
			RequestID: reqID, Timestamp: time.Now(),
			Phase: domain.JobPhaseSubmitted, Kind: meta.Kind, JobID: meta.ID, Label: meta.Label,
		}
	}, false)

	go s.monitor(ctx, sj)
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

// onSignal records and forwards a job's intermediate status.
func (s *Supervisor) onSignal(sj *supervised, sig domain.JobSignal) {
	if sig.Note != "" {
		s.mu.Lock()
		sj.lastNote = sig.Note
		s.mu.Unlock()
	}

	s.dispatch(func(reqID string) domain.ChatEvent {
		return domain.BackgroundJobEvent{
			RequestID: reqID, Timestamp: time.Now(),
			Phase: domain.JobPhaseStatus, Kind: sj.meta.Kind, JobID: sj.meta.ID, Label: sj.meta.Label, Note: sig.Note,
		}
	}, false)

	if sig.Enqueue && sig.Note != "" {
		s.enqueue(sig.Note)
		s.dispatch(nil, true) // wake the agent to read it
	}
}

// finish marks a job terminal, delivers its result onto the conversation, and
// emits the completion/failure event. The entry is KEPT (terminal) so the task
// view can show it; Cleanup reaps it after the retention window.
func (s *Supervisor) finish(sj *supervised, result domain.ToolExecutionResult) {
	now := time.Now()
	status := domain.JobCompleted
	phase := domain.JobPhaseCompleted
	if !result.Success {
		status = domain.JobFailed
		phase = domain.JobPhaseFailed
	}

	s.mu.Lock()
	sj.status = status
	sj.completedAt = &now
	s.mu.Unlock()

	res := result
	if !sj.meta.Silent {
		s.enqueue(s.formatResult(sj.job, sj.meta, &res))
		s.dispatch(nil, true)
	}

	s.dispatch(func(reqID string) domain.ChatEvent {
		return domain.BackgroundJobEvent{
			RequestID: reqID, Timestamp: time.Now(),
			Phase: phase, Kind: sj.meta.Kind, JobID: sj.meta.ID, Label: sj.meta.Label, Result: &res,
		}
	}, false)
}

// formatResult renders a finished job's outcome as the user-role message the
// agent will read on its next turn: a uniform "[Kind verb: label]" header plus a
// body the job formats itself (JobNotifier) or, by default, the domain-formatted
// tool result.
func (s *Supervisor) formatResult(job domain.BackgroundJob, meta domain.JobMeta, result *domain.ToolExecutionResult) string {
	var body string
	switch {
	case asNotifier(job) != nil:
		body = asNotifier(job).Notification(*result)
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

// enqueue lands content on the shared message queue as a user-role message.
func (s *Supervisor) enqueue(content string) {
	if s.messageQueue == nil {
		return
	}
	s.messageQueue.Enqueue(sdk.Message{
		Role:    sdk.User,
		Content: sdk.NewMessageContent(content),
	}, "system")
}

// dispatch delivers a chat event and/or an agent wake-up through the current
// request sink. It holds the read lock across the non-blocking sends so a
// concurrent release() (write lock) cannot clear the sink and let the request
// close its channels mid-send. build may be nil to wake without an event.
func (s *Supervisor) dispatch(build func(reqID string) domain.ChatEvent, wake bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sink := s.sink
	if sink == nil {
		return
	}
	if build != nil && sink.eventChan != nil {
		ev := build(sink.requestID)
		select {
		case sink.eventChan <- ev:
		default:
			logger.Warn("dropped background job event - chat event channel full")
		}
	}
	if wake && sink.agentEventChan != nil {
		select {
		case sink.agentEventChan <- domain.MessageReceivedEvent{}:
		default:
			logger.Warn("dropped agent wake-up - agent event channel full")
		}
	}
}

// Wind delivers a graceful wind-down or hard stop to one job.
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

// HasPending reports whether any running job blocks quiescence - i.e. should keep
// a headless session alive. Interactive subagents (ExcludeFromPending) do not.
func (s *Supervisor) HasPending() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, sj := range s.jobs {
		if sj.status == domain.JobRunning && !sj.meta.ExcludeFromPending {
			return true
		}
	}
	return false
}

// HasActiveWork reports whether any job is still running, including interactive
// subagents - the chat loop uses this so it stays alive while they run.
func (s *Supervisor) HasActiveWork() bool {
	return s.CountRunning("") > 0
}

// Cleanup reaps finished jobs whose terminal timestamp is older than olderThan,
// running each one's Wind(WindStop) teardown (kill pane, remove temp files)
// before dropping it. Running jobs are never reaped. Returns the number removed.
func (s *Supervisor) Cleanup(olderThan time.Duration) int {
	cutoff := time.Now().Add(-olderThan)

	s.mu.Lock()
	var reaped []*supervised
	for id, sj := range s.jobs {
		if !sj.status.IsTerminal() {
			continue
		}
		ref := sj.meta.StartedAt
		if sj.completedAt != nil {
			ref = *sj.completedAt
		}
		if ref.After(cutoff) {
			continue
		}
		reaped = append(reaped, sj)
		delete(s.jobs, id)
	}
	s.mu.Unlock()

	for _, sj := range reaped {
		sj.job.Close()
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
