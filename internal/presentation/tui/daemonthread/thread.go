package daemonthread

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"slices"
	"strings"
	"sync"
	"time"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	daemon "github.com/inference-gateway/cli/internal/daemon"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
	sessionsdomain "github.com/inference-gateway/cli/internal/sessions/domain"
)

// clientKind is what the chat TUI declares in its hello.
const clientKind = "tui"

// requestTimeout bounds one request to the daemon, long enough for the
// daemon to launch a worker or reach a remote workspace first.
const requestTimeout = 2 * time.Minute

// maxBackoff caps the wait between two reconnect attempts.
const maxBackoff = 30 * time.Second

// The markers the thread queues among the run frames when its connection
// drops and comes back, so the request reading them can react.
const (
	markerOffline     = "thread_offline"
	markerReconnected = "thread_reconnected"
)

// ErrOffline is what a request gets while the daemon connection is down.
var ErrOffline = errors.New("lost the connection to infer daemon, reconnecting")

// Options name the project a daemon chat runs its threads in.
type Options struct {
	ProjectDir string
}

// Thread is the chat TUI's connection to the local daemon's binding and to
// the one thread it follows there. Frames never block the socket: run frames
// queue for the request that reads them, replies go to their waiters, and UI
// notifications leave through a queue of their own.
type Thread struct {
	cfg    *config.Config
	opts   Options
	leases agentdomain.LeaseStore

	mu         sync.Mutex
	conn       *agui.Conn
	notifier   agentdomain.UINotifier
	following  string
	live       string
	placement  tui.SessionPlacement
	running    bool
	consumers  int
	unfollowed bool
	inbox      [][]byte
	waiters    []*waiter
	synced     map[string]int
	teleport   string
	generation int
	closed     bool

	wake  chan struct{}
	notes chan struct{}
	queue []any
	done  chan struct{}
}

// waiter is a request waiting for its reply type.
type waiter struct {
	replyType string
	reply     chan reply
}

type reply struct {
	frame []byte
	err   error
}

// header is the slice of a frame the thread routes by.
type header struct {
	Type     string `json:"type"`
	RunID    string `json:"runId"`
	ThreadID string `json:"threadId"`
	Message  string `json:"message"`
	Outcome  struct {
		Type string `json:"type"`
	} `json:"outcome"`
}

// New builds the thread connection for a daemon chat in opts.ProjectDir. It
// reads the session's lease from leases to tell where it runs.
func New(cfg *config.Config, opts Options, leases agentdomain.LeaseStore) *Thread {
	t := &Thread{
		cfg: cfg, opts: opts, leases: leases,
		synced: make(map[string]int),
		wake:   make(chan struct{}, 1),
		notes:  make(chan struct{}, 1),
		done:   make(chan struct{}),
	}
	go t.deliverNotes()
	return t
}

// SetNotifier hands the thread the UI it reports to once the TUI runs.
func (t *Thread) SetNotifier(notifier agentdomain.UINotifier) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.notifier = notifier
}

// Connect dials the local daemon, starting it when nothing listens on the
// binding, and fails fast when the binding cannot authenticate the TUI.
func (t *Thread) Connect(ctx context.Context) error {
	dial, err := daemon.LocalDialConfig(ctx, t.cfg, clientKind)
	if err != nil {
		return err
	}
	if err := daemon.EnsureRunning(ctx, t.cfg.Binding().Port); err != nil {
		return err
	}
	conn, err := agui.Dial(ctx, dial, t)
	if err != nil {
		return err
	}
	t.mu.Lock()
	t.conn = conn
	t.generation++
	t.placement.Daemon = true
	t.placement.Offline = false
	t.placement.Local = conn.AckAttr("workspace")
	t.mu.Unlock()
	return nil
}

// Close stops reconnecting and hangs up.
func (t *Thread) Close() {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return
	}
	t.closed = true
	conn := t.conn
	t.mu.Unlock()
	close(t.done)
	if conn != nil {
		conn.Close()
	}
}

// Follow makes the connection follow the conversation id, creating it on the
// worker when it does not exist yet. opts apply when the daemon starts the
// thread's worker, and a session that runs remotely is followed there.
func (t *Thread) Follow(ctx context.Context, id string, opts sessionsdomain.ThreadOptions) error {
	t.mu.Lock()
	placement := t.placement
	t.mu.Unlock()
	if placement.Remote() {
		opts.Workspace, opts.WorkspaceDir = placement.Workspace, placement.WorkspaceDir
	}
	frame := followFrame{Type: "resume_conversation", ProjectDir: t.opts.ProjectDir, ID: id, ThreadOptions: opts}
	if _, err := t.Request(ctx, frame, "MESSAGES_SNAPSHOT"); err != nil {
		return err
	}
	t.mu.Lock()
	t.following, t.live = id, id
	t.mu.Unlock()
	if !placement.Remote() {
		t.locate(ctx, id)
	}
	return nil
}

// SessionProject is the project dir of a session this machine's daemon runs,
// from its lease, or "" when another workspace runs it or no lease names one.
func (t *Thread) SessionProject(ctx context.Context, id string) string {
	if t.leases == nil {
		return ""
	}
	lease, found, err := t.leases.GetLease(ctx, id)
	t.mu.Lock()
	local := t.placement.Local
	t.mu.Unlock()
	if err != nil || !found || lease.Workspace != local {
		return ""
	}
	return lease.WorkspaceDir
}

// MoveTo makes this a daemon chat in projectDir. It is for attaching, before
// the thread follows a session, since every request names the project dir.
func (t *Thread) MoveTo(projectDir string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.opts.ProjectDir = projectDir
}

// followFrame opens or resumes the thread of one conversation.
type followFrame struct {
	Type       string `json:"type"`
	ProjectDir string `json:"project_dir"`
	ID         string `json:"id"`
	sessionsdomain.ThreadOptions
}

// locate reads where the session of id runs from its lease, so a session
// another workspace owns shows as running there.
func (t *Thread) locate(ctx context.Context, id string) {
	workspace, workspaceDir := "", ""
	if t.leases != nil {
		if lease, found, err := t.leases.GetLease(ctx, id); err == nil && found {
			workspace, workspaceDir = lease.Workspace, lease.WorkspaceDir
		}
	}
	t.mu.Lock()
	if workspace == "" || workspace == t.placement.Local {
		workspace, workspaceDir = t.placement.Local, t.opts.ProjectDir
	}
	t.placement.Workspace, t.placement.WorkspaceDir = workspace, workspaceDir
	placement := t.placement
	t.mu.Unlock()
	t.post(tui.SessionPlacementChangedEvent{Placement: placement})
}

// Follows reports whether the connection follows the conversation id, which
// it also does once a rollover or an accepted plan moved the thread on to id.
func (t *Thread) Follows(id string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return id != "" && (id == t.following || id == t.live)
}

// Live is the conversation the followed thread holds now.
func (t *Thread) Live() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.live != "" {
		return t.live
	}
	return t.following
}

// Placement is where the followed session runs.
func (t *Thread) Placement() tui.SessionPlacement {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.placement
}

// Synced is how many entries of conversation id the worker already has, so a
// turn sends only the user's new messages.
func (t *Thread) Synced(id string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.synced[id]
}

// SetSynced records that the worker has the first n entries of id.
func (t *Thread) SetSynced(id string, n int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.synced[id] = n
}

// Generation counts the connections to the daemon, since a daemon that
// restarted meanwhile runs a fresh worker behind the same thread.
func (t *Thread) Generation() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.generation
}

// Running reports whether the followed thread has an open run.
func (t *Thread) Running() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.running
}

// Pending reports whether run frames wait to be read.
func (t *Thread) Pending() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.inbox) > 0
}

// Send writes one frame to the daemon.
func (t *Thread) Send(frame any) error {
	t.mu.Lock()
	conn := t.conn
	t.mu.Unlock()
	if conn == nil {
		return ErrOffline
	}
	data, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	return conn.Send(data)
}

// Request sends frame and returns the first reply of replyType, or the error a
// RUN_ERROR without a run carries in its place.
func (t *Thread) Request(ctx context.Context, frame any, replyType string) ([]byte, error) {
	w := &waiter{replyType: replyType, reply: make(chan reply, 1)}
	t.mu.Lock()
	if t.conn == nil {
		t.mu.Unlock()
		return nil, ErrOffline
	}
	t.waiters = append(t.waiters, w)
	t.mu.Unlock()
	if err := t.Send(frame); err != nil {
		t.dropWaiter(w)
		return nil, err
	}
	select {
	case r := <-w.reply:
		return r.frame, r.err
	case <-ctx.Done():
		t.dropWaiter(w)
		return nil, ctx.Err()
	}
}

func (t *Thread) dropWaiter(w *waiter) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.waiters = slices.DeleteFunc(t.waiters, func(other *waiter) bool { return other == w })
}

// Consume marks a request as the reader of the run frames until the returned
// release runs, so a run it started does not ask the TUI to follow it. A run
// that started while the request was finishing, as an accepted plan's does,
// asks once the release runs. A request may start before the last one's
// release ran, so only the last release does.
func (t *Thread) Consume() func() {
	t.mu.Lock()
	t.consumers++
	t.unfollowed = false
	t.mu.Unlock()
	return func() {
		t.mu.Lock()
		t.consumers--
		started := t.consumers == 0 && slices.ContainsFunc(t.inbox, func(frame []byte) bool { return Marker(frame) == "" && frameName(frame) == "RUN_STARTED" })
		t.unfollowed = t.unfollowed || started
		t.mu.Unlock()
		if started {
			t.post(tui.FollowRunEvent{})
		}
	}
}

// Unfollowed reports whether a run started, or one suspended on a prompt was
// replayed, with no turn reading the thread since, or a run is under way. A
// chat that attached before its UI could take the follow request still owes it.
func (t *Thread) Unfollowed() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.unfollowed || t.running
}

// DiscardSettled drops queued frames of runs that already ended, which the
// reconcile of the next turn's history covers, so a new turn reads only what
// is still to come.
func (t *Thread) DiscardSettled() {
	t.mu.Lock()
	defer t.mu.Unlock()
	last := -1
	for i, frame := range t.inbox {
		var h header
		if json.Unmarshal(frame, &h) == nil && (h.Type == "RUN_FINISHED" || h.Type == "RUN_ERROR") {
			last = i
		}
	}
	t.inbox = t.inbox[last+1:]
}

// Next returns the next run frame, waiting for one.
func (t *Thread) Next(ctx context.Context) ([]byte, error) {
	for {
		t.mu.Lock()
		if len(t.inbox) > 0 {
			frame := t.inbox[0]
			t.inbox = t.inbox[1:]
			t.mu.Unlock()
			return frame, nil
		}
		t.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-t.wake:
		}
	}
}

// Marker reports which connection marker a frame Next returned is, or "".
func Marker(frame []byte) string {
	var h header
	if json.Unmarshal(frame, &h) != nil {
		return ""
	}
	switch h.Type {
	case markerOffline, markerReconnected:
		return h.Type
	}
	return ""
}

func (t *Thread) Attach(*agui.Conn) {}

// Handle routes one daemon frame without blocking the socket.
func (t *Thread) Handle(_ *agui.Conn, data []byte) {
	frame := slices.Clone(data)
	var h header
	if json.Unmarshal(frame, &h) != nil {
		return
	}
	switch h.Type {
	case "thread_activity":
		t.trackActivity(frame)
	case "browser_extension_status":
		t.onExtensionStatus(frame)
	case "teleport_progress":
		t.onProgress(frame)
	case "session_teleported":
		t.onTeleported(frame)
	default:
		if !t.answer(h, frame) && isRunFrame(h.Type) {
			t.push(h, frame)
		}
	}
}

// Detach notes the dropped connection: waiting requests fail, the request
// reading run frames learns of it, and reconnecting starts.
func (t *Thread) Detach(conn *agui.Conn) {
	t.mu.Lock()
	if t.conn != conn {
		t.mu.Unlock()
		return
	}
	t.conn = nil
	t.placement.Offline = true
	waiters := t.waiters
	t.waiters = nil
	closed := t.closed
	placement := t.placement
	t.mu.Unlock()
	for _, w := range waiters {
		w.reply <- reply{err: ErrOffline}
	}
	if closed {
		return
	}
	t.pushMarker(markerOffline)
	t.post(tui.SessionPlacementChangedEvent{Placement: placement})
	go t.reconnect()
}

// reconnect redials with a jittered backoff until the thread is followed
// again, then tells the reading request and the UI.
func (t *Thread) reconnect() {
	delay := time.Second
	for {
		select {
		case <-t.done:
			return
		case <-time.After(delay/2 + rand.N(delay/2+1)):
		}
		if err := t.rejoin(); err != nil {
			logger.Debug("daemon chat could not reconnect yet", "error", err)
			delay = min(delay*2, maxBackoff)
			continue
		}
		t.pushMarker(markerReconnected)
		t.post(tui.SessionPlacementChangedEvent{Placement: t.Placement()})
		return
	}
}

// rejoin dials the daemon and follows the thread again. A connection that
// cannot follow is let go without starting another reconnect.
func (t *Thread) rejoin() error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	if err := t.Connect(ctx); err != nil {
		return err
	}
	id := t.Live()
	if id == "" {
		return nil
	}
	if err := t.Follow(ctx, id, sessionsdomain.ThreadOptions{}); err != nil {
		t.mu.Lock()
		conn := t.conn
		t.conn = nil
		t.mu.Unlock()
		if conn != nil {
			conn.Close()
		}
		return err
	}
	return nil
}

// answer hands a reply to the oldest request waiting for its type. A
// RUN_ERROR without a run is the refusal of the oldest request.
func (t *Thread) answer(h header, frame []byte) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	for i, w := range t.waiters {
		refused := h.Type == "RUN_ERROR" && h.RunID == ""
		if !refused && w.replyType != h.Type {
			continue
		}
		t.waiters = slices.Delete(t.waiters, i, i+1)
		if refused {
			w.reply <- reply{err: errors.New(h.Message)}
		} else {
			w.reply <- reply{frame: frame}
		}
		return true
	}
	return false
}

// push queues a run frame. A run that starts, or suspends on a prompt, while
// no request reads asks the TUI to follow it.
func (t *Thread) push(h header, frame []byte) {
	t.mu.Lock()
	switch h.Type {
	case "RUN_STARTED":
		t.running = true
		if h.ThreadID != "" {
			t.live = h.ThreadID
		}
	case "RUN_FINISHED", "RUN_ERROR":
		t.running = false
	}
	t.inbox = append(t.inbox, frame)
	follow := t.consumers == 0 && (h.Type == "RUN_STARTED" || h.Type == "RUN_FINISHED" && h.Outcome.Type == "interrupt")
	t.unfollowed = t.unfollowed || follow
	t.mu.Unlock()
	t.signal()
	if follow {
		t.post(tui.FollowRunEvent{})
	}
}

// pushMarker tells the turn reading the inbox that the connection dropped or
// came back. With no turn reading, there is nobody to tell, and a marker left
// queued would end the next turn before its run starts.
func (t *Thread) pushMarker(marker string) {
	frame, _ := json.Marshal(map[string]string{"type": marker})
	t.mu.Lock()
	if t.consumers == 0 {
		t.mu.Unlock()
		return
	}
	t.inbox = append(t.inbox, frame)
	t.mu.Unlock()
	t.signal()
}

func (t *Thread) signal() {
	select {
	case t.wake <- struct{}{}:
	default:
	}
}

// activeThread is one thread a thread_activity frame names as running.
type activeThread struct {
	ProjectDir     string `json:"project_dir"`
	ConversationID string `json:"conversation_id"`
}

// trackActivity notes from thread_activity whether the followed thread runs.
func (t *Thread) trackActivity(frame []byte) {
	var activity struct {
		Threads []activeThread `json:"threads"`
	}
	if json.Unmarshal(frame, &activity) != nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	followed := activeThread{ProjectDir: t.opts.ProjectDir, ConversationID: t.following}
	t.running = slices.Contains(activity.Threads, followed)
}

func (t *Thread) onExtensionStatus(frame []byte) {
	var status struct {
		Connected bool `json:"connected"`
	}
	if json.Unmarshal(frame, &status) == nil {
		t.post(agentdomain.BrowserExtensionStatusEvent{Connected: status.Connected})
	}
}

// post queues a UI notification for the notifier goroutine.
func (t *Thread) post(event any) {
	t.mu.Lock()
	t.queue = append(t.queue, event)
	t.mu.Unlock()
	select {
	case t.notes <- struct{}{}:
	default:
	}
}

// deliverNotes hands queued notifications to the UI one at a time, since
// the UI may block until it consumed each.
func (t *Thread) deliverNotes() {
	for {
		select {
		case <-t.done:
			return
		case <-t.notes:
		}
		for {
			t.mu.Lock()
			if len(t.queue) == 0 {
				t.mu.Unlock()
				break
			}
			event := t.queue[0]
			t.queue = t.queue[1:]
			notifier := t.notifier
			t.mu.Unlock()
			if notifier != nil {
				notifier.Notify(event)
			}
		}
	}
}

// isRunFrame reports whether a frame type is an AG-UI event, which only runs
// carry; app frames are lowercase.
func isRunFrame(frameType string) bool {
	return frameType != "" && strings.ToUpper(frameType[:1]) == frameType[:1]
}
