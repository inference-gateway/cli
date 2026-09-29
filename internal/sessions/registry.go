package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"sync"
	"time"

	uuid "github.com/google/uuid"

	logger "github.com/inference-gateway/cli/internal/platform/logger"
	sessionsdomain "github.com/inference-gateway/cli/internal/sessions/domain"
)

const (
	frameNewSession         = "new_session"
	frameResumeConversation = "resume_conversation"
	frameApprovalResponse   = "approval_response"
	frameUserMessage        = "user_message"
	frameToolRequest        = "tool_request"
	frameToolResult         = "tool_result"
	frameMessagesSnapshot   = "MESSAGES_SNAPSHOT"
)

// forwarded are the frames that go to the client's own thread unchanged.
var forwarded = map[string]bool{
	frameUserMessage:         true,
	"interrupt":              true,
	"user_question_response": true,
	"computer_use_control":   true,
}

// replyTypes maps each panel request to the reply frames its worker answers
// with, which go back to the requester instead of the whole thread.
var replyTypes = map[string][]string{
	"list_conversations": {"conversations"},
	"list_history":       {"history"},
	"list_skills":        {"skills"},
	"list_models":        {"models"},
	"select_model":       {"models", "mode"},
	"set_mode":           {"mode"},
	frameToolRequest:     {frameToolResult},
}

var errNoThread = errors.New("open a thread with new_session or resume_conversation first")

type clientFrame struct {
	Type       string `json:"type"`
	ID         string `json:"id"`
	ProjectDir string `json:"project_dir"`
	ToolCallID string `json:"tool_call_id"`
	sessionsdomain.ThreadOptions
}

type workerLine struct {
	Type  string          `json:"type"`
	ID    string          `json:"id"`
	RunID string          `json:"runId"`
	Name  string          `json:"name"`
	Value json.RawMessage `json:"value"`
}

// Registry is the daemon's thread registry. It launches one worker per thread,
// relays client frames to worker stdin and worker lines to the thread's
// clients, lets the first answer to an approval win, ends a run a crashed
// worker left open with RUN_ERROR, and stops idle workers.
type Registry struct {
	launch sessionsdomain.LaunchWorker
	idle   time.Duration

	// ponytail: one lock for the whole registry, per-thread locks if contention shows
	mu        sync.Mutex
	closed    bool
	threads   map[sessionsdomain.ThreadKey]*thread
	subs      map[sessionsdomain.Client]*thread
	approvals map[string]*thread
}

// thread is one project dir plus conversation id and the worker running it.
// A client follows at most one thread, because bare AG-UI events carry no
// thread id. The worker is nil until launched and after it exits.
type thread struct {
	key     sessionsdomain.ThreadKey
	opts    sessionsdomain.ThreadOptions
	worker  sessionsdomain.Worker
	clients map[sessionsdomain.Client]struct{}
	waiting map[string][]sessionsdomain.Client
	runID   string
	active  time.Time
}

// NewRegistry builds a registry that launches workers with launch and stops
// the ones nobody follows once they sat idle for idle.
func NewRegistry(launch sessionsdomain.LaunchWorker, idle time.Duration) *Registry {
	return &Registry{
		launch:    launch,
		idle:      idle,
		threads:   make(map[sessionsdomain.ThreadKey]*thread),
		subs:      make(map[sessionsdomain.Client]*thread),
		approvals: make(map[string]*thread),
	}
}

// Handle routes one client frame. new_session and resume_conversation make
// the client follow that thread, panel requests run on a worker in their
// project dir, and approval_response answers the approval it names once.
func (r *Registry) Handle(c sessionsdomain.Client, frame []byte) {
	var f clientFrame
	if err := json.Unmarshal(frame, &f); err != nil {
		logger.Debug("sessions dropped an undecodable client frame", "error", err)
		return
	}
	if f.Type == frameApprovalResponse {
		r.answer(c, f.ToolCallID, frame)
		return
	}
	w, err := r.route(c, f)
	if err != nil {
		c.Deliver(runError("", err.Error()))
		return
	}
	if w == nil {
		return
	}
	if err := w.Send(frame); err != nil {
		c.Deliver(runError("", fmt.Sprintf("the session worker did not take the frame: %v", err)))
	}
}

// Detach drops a disconnected client. Its thread keeps running an open turn
// and is stopped once idle.
func (r *Registry) Detach(c sessionsdomain.Client) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t := r.subs[c]; t != nil {
		delete(t.clients, c)
		t.active = time.Now()
	}
	delete(r.subs, c)
}

// Run stops idle workers until ctx ends, then stops every worker and waits
// for them to exit.
func (r *Registry) Run(ctx context.Context) {
	ticker := time.NewTicker(max(r.idle/2, time.Millisecond))
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			r.stopAll()
			return
		case now := <-ticker.C:
			r.reap(now)
		}
	}
}

// route picks the frame's thread and returns the worker to send it to, nil
// when the frame has nowhere to go.
func (r *Registry) route(c sessionsdomain.Client, f clientFrame) (sessionsdomain.Worker, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch {
	case f.Type == frameNewSession || f.Type == frameResumeConversation:
		t, err := r.openLocked(c, f)
		if err != nil {
			return nil, err
		}
		t.wait(frameMessagesSnapshot, c)
		return r.workerLocked(t)
	case forwarded[f.Type]:
		t := r.subs[c]
		if t == nil {
			return nil, errNoThread
		}
		if t.worker == nil && f.Type != frameUserMessage {
			return nil, nil
		}
		return r.workerLocked(t)
	case replyTypes[f.Type] != nil:
		t, err := r.pickLocked(c, f.ProjectDir)
		if err != nil {
			return nil, err
		}
		for _, reply := range replyTypes[f.Type] {
			t.wait(replyKey(reply, f.ID), c)
		}
		return r.workerLocked(t)
	}
	return nil, nil
}

// openLocked gets or creates the thread a new_session or resume_conversation
// names and makes c follow it. Options apply when the thread's worker launches.
func (r *Registry) openLocked(c sessionsdomain.Client, f clientFrame) (*thread, error) {
	dir, err := projectDir(f.ProjectDir)
	if err != nil {
		return nil, err
	}
	id := f.ID
	if f.Type == frameNewSession {
		id = uuid.NewString()
	}
	if id == "" {
		return nil, errors.New("resume_conversation needs the conversation id")
	}
	key := sessionsdomain.ThreadKey{ProjectDir: dir, ConversationID: id}
	t := r.threads[key]
	if t == nil {
		t = newThread(key, f.ThreadOptions)
		r.threads[key] = t
	}
	if prev := r.subs[c]; prev != nil && prev != t {
		delete(prev.clients, c)
		prev.active = time.Now()
	}
	r.subs[c] = t
	t.clients[c] = struct{}{}
	return t, nil
}

// pickLocked picks the thread a panel request runs on: the client's own when
// it is in the requested project dir, else any live one there, else a fresh
// thread in that dir that nobody follows.
func (r *Registry) pickLocked(c sessionsdomain.Client, requested string) (*thread, error) {
	own := r.subs[c]
	if requested == "" {
		if own == nil {
			return nil, errNoThread
		}
		return own, nil
	}
	dir, err := projectDir(requested)
	if err != nil {
		return nil, err
	}
	if own != nil && own.key.ProjectDir == dir {
		return own, nil
	}
	for _, t := range r.threads {
		if t.key.ProjectDir == dir && t.worker != nil {
			return t, nil
		}
	}
	key := sessionsdomain.ThreadKey{ProjectDir: dir, ConversationID: uuid.NewString()}
	t := newThread(key, sessionsdomain.ThreadOptions{})
	r.threads[key] = t
	return t, nil
}

// workerLocked returns t's worker, launching it first when it is not running.
func (r *Registry) workerLocked(t *thread) (sessionsdomain.Worker, error) {
	t.active = time.Now()
	if t.worker != nil {
		return t.worker, nil
	}
	if r.closed {
		return nil, errors.New("the daemon is shutting down")
	}
	w, err := r.launch(t.key, t.opts)
	if err != nil {
		clear(t.waiting)
		if len(t.clients) == 0 {
			delete(r.threads, t.key)
		}
		return nil, fmt.Errorf("launching the session worker in %s: %w", t.key.ProjectDir, err)
	}
	t.worker = w
	go r.pump(t, w)
	return w, nil
}

// answer sends the first approval_response for a pending approval to its
// worker and tells the thread's other clients it is resolved. Later answers
// find nothing pending and are dropped.
func (r *Registry) answer(c sessionsdomain.Client, toolCallID string, frame []byte) {
	r.mu.Lock()
	t := r.approvals[toolCallID]
	delete(r.approvals, toolCallID)
	var w sessionsdomain.Worker
	var others []sessionsdomain.Client
	if t != nil {
		w = t.worker
		others = t.clientsExcept(c)
		t.active = time.Now()
	}
	r.mu.Unlock()

	if w == nil {
		return
	}
	if err := w.Send(frame); err != nil {
		c.Deliver(runError("", fmt.Sprintf("the session worker did not take the approval: %v", err)))
		return
	}
	resolved := approvalResolved(toolCallID)
	for _, other := range others {
		other.Deliver(resolved)
	}
}

// pump delivers each worker line to its targets until the worker exits.
func (r *Registry) pump(t *thread, w sessionsdomain.Worker) {
	for line := range w.Lines() {
		for _, c := range r.targets(t, line) {
			c.Deliver(line)
		}
	}
	r.exited(t, w)
}

// targets tracks the run and the pending approvals a worker line reveals and
// returns who gets it. A reply goes to the oldest client waiting for it, a
// failure before any run to everyone concerned, and the rest to all clients.
func (r *Registry) targets(t *thread, line []byte) []sessionsdomain.Client {
	var l workerLine
	if err := json.Unmarshal(line, &l); err != nil {
		logger.Debug("sessions dropped an undecodable worker line", "error", err)
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	t.active = time.Now()
	key := l.Type
	switch l.Type {
	case "RUN_STARTED":
		t.runID = l.RunID
	case "RUN_FINISHED":
		t.runID = ""
	case "RUN_ERROR":
		if t.runID == "" {
			concerned := t.takeWaiters()
			maps.Copy(concerned, t.clients)
			return slices.Collect(maps.Keys(concerned))
		}
		t.runID = ""
	case frameToolResult:
		delete(r.approvals, l.ID)
		key = replyKey(frameToolResult, l.ID)
	case "CUSTOM":
		if l.Name == "approval_request" {
			id := approvalToolCallID(l.Value)
			r.approvals[id] = t
			if requester := t.peek(replyKey(frameToolResult, id)); requester != nil {
				return []sessionsdomain.Client{requester}
			}
		}
	}
	if requester := t.pop(key); requester != nil {
		return []sessionsdomain.Client{requester}
	}
	return slices.Collect(maps.Keys(t.clients))
}

// exited forgets a worker that is gone. A run it left open ends with
// RUN_ERROR, and clients still waiting on it get one too, so nobody hangs.
func (r *Registry) exited(t *thread, w sessionsdomain.Worker) {
	r.mu.Lock()
	if t.worker != w {
		r.mu.Unlock()
		return
	}
	t.worker = nil
	for id, owner := range r.approvals {
		if owner == t {
			delete(r.approvals, id)
		}
	}
	concerned := t.takeWaiters()
	message := "the session worker exited before it answered"
	if t.runID != "" {
		maps.Copy(concerned, t.clients)
		message = "the session worker exited mid-run"
	}
	line := runError(t.runID, message)
	t.runID = ""
	if len(t.clients) == 0 && r.threads[t.key] == t {
		delete(r.threads, t.key)
	}
	r.mu.Unlock()

	for c := range concerned {
		c.Deliver(line)
	}
}

// reap stops the workers of threads nobody follows that have no open run, no
// pending reply, and sat idle for the registry's idle timeout.
func (r *Registry) reap(now time.Time) {
	r.mu.Lock()
	var idle []sessionsdomain.Worker
	for key, t := range r.threads {
		if len(t.clients) > 0 || t.runID != "" || len(t.waiting) > 0 || now.Sub(t.active) < r.idle {
			continue
		}
		if t.worker != nil {
			idle = append(idle, t.worker)
			t.worker = nil
		}
		delete(r.threads, key)
	}
	r.mu.Unlock()
	for _, w := range idle {
		go w.Stop()
	}
}

// stopAll stops every worker in parallel and waits for them to exit. No
// worker launches afterwards.
func (r *Registry) stopAll() {
	r.mu.Lock()
	r.closed = true
	var workers []sessionsdomain.Worker
	for key, t := range r.threads {
		if t.worker != nil {
			workers = append(workers, t.worker)
			t.worker = nil
		}
		delete(r.threads, key)
	}
	r.mu.Unlock()

	var wg sync.WaitGroup
	for _, w := range workers {
		wg.Go(w.Stop)
	}
	wg.Wait()
}

func newThread(key sessionsdomain.ThreadKey, opts sessionsdomain.ThreadOptions) *thread {
	return &thread{
		key:     key,
		opts:    opts,
		clients: make(map[sessionsdomain.Client]struct{}),
		waiting: make(map[string][]sessionsdomain.Client),
		active:  time.Now(),
	}
}

func (t *thread) wait(key string, c sessionsdomain.Client) {
	t.waiting[key] = append(t.waiting[key], c)
}

func (t *thread) peek(key string) sessionsdomain.Client {
	if queue := t.waiting[key]; len(queue) > 0 {
		return queue[0]
	}
	return nil
}

func (t *thread) pop(key string) sessionsdomain.Client {
	queue := t.waiting[key]
	if len(queue) == 0 {
		return nil
	}
	if len(queue) == 1 {
		delete(t.waiting, key)
	} else {
		t.waiting[key] = queue[1:]
	}
	return queue[0]
}

// takeWaiters empties every reply queue and returns the clients that were
// waiting, each once.
func (t *thread) takeWaiters() map[sessionsdomain.Client]struct{} {
	waiters := make(map[sessionsdomain.Client]struct{})
	for _, queue := range t.waiting {
		for _, c := range queue {
			waiters[c] = struct{}{}
		}
	}
	clear(t.waiting)
	return waiters
}

func (t *thread) clientsExcept(c sessionsdomain.Client) []sessionsdomain.Client {
	others := make([]sessionsdomain.Client, 0, len(t.clients))
	for other := range t.clients {
		if other != c {
			others = append(others, other)
		}
	}
	return others
}

// replyKey names a reply queue. A tool_result is matched by its request id,
// every other reply by its type.
func replyKey(reply, id string) string {
	if reply == frameToolResult {
		return reply + ":" + id
	}
	return reply
}

func projectDir(dir string) (string, error) {
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("project_dir must be an absolute path, got %q", dir)
	}
	return filepath.Clean(dir), nil
}

func approvalToolCallID(value json.RawMessage) string {
	var v struct {
		ToolCallID string `json:"tool_call_id"`
	}
	_ = json.Unmarshal(value, &v)
	return v.ToolCallID
}

func runError(runID, message string) []byte {
	frame := map[string]string{"type": "RUN_ERROR", "message": message}
	if runID != "" {
		frame["runId"] = runID
	}
	data, _ := json.Marshal(frame)
	return data
}

func approvalResolved(toolCallID string) []byte {
	data, _ := json.Marshal(map[string]any{
		"type":  "CUSTOM",
		"name":  "approval_resolved",
		"value": map[string]string{"tool_call_id": toolCallID},
	})
	return data
}
