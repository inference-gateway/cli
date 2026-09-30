package sessions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
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
	frameRunAgentInput      = "run_agent_input"
	frameToolRequest        = "tool_request"
	frameToolResult         = "tool_result"
	frameMessagesSnapshot   = "MESSAGES_SNAPSHOT"

	outboundBrowserCommand = "browser_command"
)

// forwarded are the frames that go to the client's own thread unchanged: the
// run input that starts and continues its runs, the frame that stops one, and
// the channels' direct prompt and question answers.
var forwarded = map[string]bool{
	frameRunAgentInput:       true,
	frameUserMessage:         true,
	"interrupt":              true,
	"user_question_response": true,
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
	Type    string          `json:"type"`
	ID      string          `json:"id"`
	RunID   string          `json:"runId"`
	Name    string          `json:"name"`
	Value   json.RawMessage `json:"value"`
	Outcome struct {
		Type       string            `json:"type"`
		Interrupts []workerInterrupt `json:"interrupts"`
	} `json:"outcome"`
}

// workerInterrupt is one interrupt a suspended run's terminal line waits on.
type workerInterrupt struct {
	ID string `json:"id"`
}

// Registry is the daemon's thread registry. It launches one worker per thread,
// relays client frames to worker stdin and worker lines to the thread's
// clients, tracks the interrupts a suspended run waits on so the first resume
// wins and an incomplete one is refused with RUN_ERROR, lets the first answer
// to an approval win, ends a run a crashed worker left open with RUN_ERROR,
// routes worker browser_command frames to the connected browser extension, and
// stops idle workers.
type Registry struct {
	launch sessionsdomain.LaunchWorker
	idle   time.Duration
	// browser, wired by the daemon, relays worker browser_command frames to the
	// extension connection on the AG-UI binding. It is nil until wired.
	browser sessionsdomain.BrowserRelay

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
	// runID is the thread's open run, empty once it ended, and interrupts are
	// the ones a suspended run waits on.
	runID      string
	interrupts map[string]struct{}
	active     time.Time
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

// RouteBrowser wires the relay worker browser_command frames take to the
// extension connection. Call it once before any worker launches, which is what
// lets pump read the field without the registry lock.
func (r *Registry) RouteBrowser(relay sessionsdomain.BrowserRelay) {
	r.browser = relay
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
	t, w, err := r.route(c, f, frame)
	if err != nil {
		logRoutingFailure(f, t, err)
		c.Deliver(runError("", err.Error()))
		return
	}
	if w == nil {
		return
	}
	if err := w.Send(frame); err != nil {
		logger.Warn("sessions could not deliver a frame to the thread's worker", append(t.tags(), "frame", f.Type, "error", err)...)
		c.Deliver(runError("", fmt.Sprintf("the session worker did not take the frame: %v", err)))
	}
}

// logRoutingFailure logs a frame the registry could not route, with the
// thread's tags when the frame reached one and the frame's own project dir
// and conversation id otherwise, as far as the frame declared them.
func logRoutingFailure(f clientFrame, t *thread, err error) {
	if t != nil {
		logger.Warn("sessions could not route a client frame to its thread", append(t.tags(), "frame", f.Type, "error", err)...)
		return
	}
	args := []any{"frame", f.Type, "project_dir", f.ProjectDir, "error", err}
	if f.Type == frameResumeConversation {
		args = append(args, "conversation_id", f.ID)
	}
	logger.Warn("sessions has no thread for a client frame", args...)
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

// route picks the frame's thread and returns it along with the worker to
// send to, nil worker when the frame has nowhere to go.
func (r *Registry) route(c sessionsdomain.Client, f clientFrame, frame []byte) (*thread, sessionsdomain.Worker, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch {
	case f.Type == frameNewSession || f.Type == frameResumeConversation:
		t, err := r.openLocked(c, f)
		if err != nil {
			return nil, nil, err
		}
		t.wait(frameMessagesSnapshot, c)
		w, err := r.workerLocked(t)
		return t, w, err
	case forwarded[f.Type]:
		t := r.subs[c]
		if t == nil {
			return nil, nil, errNoThread
		}
		if f.Type == frameRunAgentInput {
			verdict, err := resumePolicyLocked(t, frame)
			if err != nil {
				return nil, nil, err
			}
			if verdict == resumeDrop {
				return t, nil, nil
			}
		}
		if t.worker == nil && f.Type != frameUserMessage && f.Type != frameRunAgentInput {
			return t, nil, nil
		}
		w, err := r.workerLocked(t)
		return t, w, err
	case replyTypes[f.Type] != nil:
		t, err := r.pickLocked(c, f.ProjectDir)
		if err != nil {
			return nil, nil, err
		}
		for _, reply := range replyTypes[f.Type] {
			t.wait(replyKey(reply, f.ID), c)
		}
		w, err := r.workerLocked(t)
		return t, w, err
	}
	return nil, nil, nil
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

// resumeVerdict is what a thread's open interrupts decide for one run input.
type resumeVerdict int

const (
	resumeForward resumeVerdict = iota
	resumeDrop
)

// resumePolicyLocked checks one run input's resume entries against the open
// interrupts the thread's last suspended run waits on. The first resume wins:
// it forwards, and the RUN_STARTED of the continuation run tells the thread's
// other clients the answer. A resume that answers nothing open, one a client
// sent after that wait, is dropped. A resume that misses one of the open
// interrupts is refused, the resume contract's every-interrupt rule.
func resumePolicyLocked(t *thread, frame []byte) (resumeVerdict, error) {
	answered := resumeIDs(frame)
	if len(answered) == 0 {
		return resumeForward, nil
	}
	if len(t.interrupts) == 0 {
		return resumeDrop, nil
	}
	var missing []string
	for id := range t.interrupts {
		if !slices.Contains(answered, id) {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		slices.Sort(missing)
		return resumeForward, fmt.Errorf("the resume must answer every open interrupt of the run: missing %s", strings.Join(missing, ", "))
	}
	clear(t.interrupts)
	return resumeForward, nil
}

// resumeIDs reads the interrupt ids one run_agent_input frame's resume answers.
func resumeIDs(frame []byte) []string {
	var f struct {
		Input struct {
			Resume []struct {
				InterruptID string `json:"interruptId"`
			} `json:"resume"`
		} `json:"input"`
	}
	if json.Unmarshal(frame, &f) != nil {
		return nil
	}
	ids := make([]string, 0, len(f.Input.Resume))
	for _, entry := range f.Input.Resume {
		if entry.InterruptID != "" && !slices.Contains(ids, entry.InterruptID) {
			ids = append(ids, entry.InterruptID)
		}
	}
	return ids
}

// openInterrupts names the interrupts a suspended run waits on, nil when the
// run ended without them.
func openInterrupts(l workerLine) map[string]struct{} {
	if l.Outcome.Type != "interrupt" {
		return nil
	}
	ids := make(map[string]struct{}, len(l.Outcome.Interrupts))
	for _, interrupt := range l.Outcome.Interrupts {
		ids[interrupt.ID] = struct{}{}
	}
	return ids
}

// pump delivers each worker line to its targets until the worker exits.
// browser_command lines do not reach clients: they route through the browser
// relay to the extension connection, which answers with a browser_result the
// worker's Browser tools resolve.
func (r *Registry) pump(t *thread, w sessionsdomain.Worker) {
	for line := range w.Lines() {
		if r.browser != nil && isBrowserCommand(line) {
			go r.relayBrowserCommand(t, w, line)
			continue
		}
		for _, c := range r.targets(t, line) {
			c.Deliver(line)
		}
	}
	r.exited(t, w)
}

// relayBrowserCommand sends one worker browser_command to the extension and
// writes the browser_result carrying the same id back to the worker's stdin, so
// the worker's Browser tools resolve without binding a port. A failed command
// is the thread's log record, since the relay itself never knows the thread.
func (r *Registry) relayBrowserCommand(t *thread, w sessionsdomain.Worker, command []byte) {
	result := r.browser(context.Background(), command)
	if failure := browserResultFailure(result); failure != "" {
		logger.Warn("a browser command failed for the thread", append(t.tags(), "error", failure)...)
	}
	if err := w.Send(result); err != nil {
		logger.Debug("sessions could not answer a browser_command", "error", err)
	}
	r.mu.Lock()
	if r.threads[t.key] == t {
		t.active = time.Now()
	}
	r.mu.Unlock()
}

// browserResultFailure extracts the error a failed browser_result carries,
// empty when the relay answered a result.
func browserResultFailure(result []byte) string {
	var res struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(result, &res)
	return res.Error
}

// isBrowserCommand reports whether a worker line is a browser_command frame,
// which routes to the extension instead of the thread's clients. The substring
// check spares the decode on every streamed line that cannot be one.
func isBrowserCommand(line []byte) bool {
	if !bytes.Contains(line, []byte(outboundBrowserCommand)) {
		return false
	}
	var msg struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(line, &msg) != nil {
		return false
	}
	return msg.Type == outboundBrowserCommand
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
		t.interrupts = nil
	case "RUN_FINISHED":
		t.runID = ""
		t.interrupts = openInterrupts(l)
	case "RUN_ERROR":
		t.interrupts = nil
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
	t.interrupts = nil
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
		key:        key,
		opts:       opts,
		clients:    make(map[sessionsdomain.Client]struct{}),
		waiting:    make(map[string][]sessionsdomain.Client),
		active:     time.Now(),
		runID:      "",
		interrupts: make(map[string]struct{}),
	}
}

// tags are the log tags carrying the thread's identity.
func (t *thread) tags() []any {
	return []any{"project_dir", t.key.ProjectDir, "conversation_id", t.key.ConversationID}
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
