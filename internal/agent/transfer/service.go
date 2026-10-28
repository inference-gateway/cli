package transfer

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"sync"
	"time"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	agentinfra "github.com/inference-gateway/cli/internal/agent/infrastructure"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	projects "github.com/inference-gateway/cli/internal/projects"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
	sessions "github.com/inference-gateway/cli/internal/sessions"
	sessionsdomain "github.com/inference-gateway/cli/internal/sessions/domain"
)

// The lease cadence: an owned row is renewed every heartbeat and lapses
// ownedLeaseTTL after the last one, unless the daemon is a long-lived
// executor whose idle exit says how long its sessions stay its own.
const (
	leaseHeartbeat = 30 * time.Second
	ownedLeaseTTL  = 90 * time.Second
	leaseGrace     = 5 * time.Minute
	probeTimeout   = 5 * time.Second
)

// Config is what the daemon tells the service about itself.
type Config struct {
	Identity      string
	WorkspaceRoot string
	IdleExit      time.Duration
	Version       string
	StorageID     string
	Handshake     agui.Handshake
	Entries       []config.ExecutorEntry
	// Denies reports whether the sandbox policy keeps a project file out of a
	// checkpoint.
	Denies func(projectDir, absPath string) bool
}

// Deps are the collaborators the service orchestrates.
type Deps struct {
	Leases    agentdomain.LeaseStore
	Executors []agentdomain.Executor
	Keyring   *agentinfra.Keyring
	Launch    sessionsdomain.LaunchWorker
}

// Service runs agents on executors: it decides where a thread's worker runs
// (the launch seam the registry calls), moves sessions between executors, and
// receives the sessions other daemons move here.
type Service struct {
	cfg       Config
	me        string
	leases    agentdomain.LeaseStore
	executors map[string]agentdomain.Executor
	entries   map[string]config.ExecutorEntry
	keyring   *agentinfra.Keyring
	launch    sessionsdomain.LaunchWorker
	dial      agentinfra.PeerDialer
	registry  *sessions.Registry

	mu          sync.Mutex
	transfers   map[string]context.CancelFunc
	imports     map[string]*pendingImport
	installed   map[string]agentdomain.Lease
	credentials map[string][]string
}

// New builds the service. AttachRegistry must follow once the registry exists,
// since the registry itself is built over LaunchWorker.
func New(cfg Config, deps Deps) *Service {
	s := &Service{
		cfg:         cfg,
		me:          cfg.Identity,
		leases:      deps.Leases,
		executors:   make(map[string]agentdomain.Executor, len(deps.Executors)),
		entries:     make(map[string]config.ExecutorEntry, len(cfg.Entries)),
		keyring:     deps.Keyring,
		launch:      deps.Launch,
		dial:        agentinfra.PeerDialer{Me: cfg.Identity, Handshake: cfg.Handshake},
		transfers:   make(map[string]context.CancelFunc),
		imports:     make(map[string]*pendingImport),
		installed:   make(map[string]agentdomain.Lease),
		credentials: make(map[string][]string),
	}
	for _, executor := range deps.Executors {
		s.executors[executor.Name()] = executor
	}
	for _, entry := range cfg.Entries {
		s.entries[entry.Name] = entry
	}
	return s
}

// AttachRegistry hands the service the thread registry its transfers quiesce,
// ask and notify.
func (s *Service) AttachRegistry(registry *sessions.Registry) {
	s.registry = registry
}

// AckAttrs are the facts the binding's ack declares about this daemon.
func (s *Service) AckAttrs() map[string]any {
	return map[string]any{
		agentinfra.AttrExecutor:  s.me,
		agentinfra.AttrVersion:   s.cfg.Version,
		agentinfra.AttrStorageID: s.cfg.StorageID,
	}
}

// Handle claims the executors and transfers frames: the client requests, and
// the peer frames when the connection is another daemon's.
func (s *Service) Handle(conn *agui.Conn, frame []byte) bool {
	var f clientFrame
	if json.Unmarshal(frame, &f) != nil {
		return false
	}
	switch f.Type {
	case frameListExecutors:
		go s.listExecutors(conn)
	case frameTransferSession:
		go s.startTransfer(conn, f)
	case frameTransferCancel:
		s.cancelTransfer(f.TransferID)
	case frameTransferChunk, frameTransferImport, frameTransferAbort:
		if conn.Kind() != agentinfra.PeerClientKind {
			logger.Warn("transfer refused a peer frame from a client that is not a daemon", "client", conn.Kind(), "frame", f.Type)
			return true
		}
		s.receive(conn, f.Type, frame)
	default:
		return false
	}
	return true
}

// listExecutors answers list_executors with every executor's reachability,
// this daemon first, probing the others in parallel.
func (s *Service) listExecutors(conn *agui.Conn) {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout+time.Second)
	defer cancel()
	names := slices.Sorted(maps.Keys(s.executors))
	statuses := make([]agentdomain.ExecutorStatus, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Go(func() { statuses[i] = s.executors[name].Probe(ctx) })
	}
	wg.Wait()
	slices.SortStableFunc(statuses, func(a, b agentdomain.ExecutorStatus) int {
		return boolRank(b.Kind == agentdomain.ExecutorKindLocal) - boolRank(a.Kind == agentdomain.ExecutorKindLocal)
	})
	conn.Deliver(encode(executorsFrame{Type: frameExecutors, Executors: statuses}))
}

func boolRank(b bool) int {
	if b {
		return 1
	}
	return 0
}

// StopSession handles a client dropping a session: when a remote executor owns
// it, the executor's runtime is deleted and the lease released.
func (s *Service) StopSession(ctx context.Context, key sessionsdomain.ThreadKey) {
	lease, found, err := s.leases.GetLease(ctx, key.ConversationID)
	if err != nil || !found || lease.Executor == s.me {
		return
	}
	executor, ok := s.executors[lease.Executor]
	if !ok {
		return
	}
	if err := executor.Delete(ctx, s.session(key)); err != nil {
		logger.Warn("transfer could not delete the session's runtime on its executor", "executor", lease.Executor, "conversation_id", key.ConversationID, "error", err)
		return
	}
	_ = s.leases.ReleaseLease(ctx, key.ConversationID, lease.Executor)
}

// Suspend parks this daemon's own runtime on its executor when the daemon
// idles out, the way a pod daemon suspends its Sandbox before exiting.
func (s *Service) Suspend(ctx context.Context) {
	for _, executor := range s.executors {
		if executor.Kind() == agentdomain.ExecutorKindLocal {
			if err := executor.Suspend(ctx, agentdomain.Session{}); err != nil {
				logger.Warn("transfer could not suspend this daemon's runtime", "error", err)
			}
		}
	}
}

// session names a thread the way checkpoints and leases do.
func (s *Service) session(key sessionsdomain.ThreadKey) agentdomain.Session {
	return agentdomain.Session{
		ProjectDir:     key.ProjectDir,
		ConversationID: key.ConversationID,
		Slug:           projectSlug(key.ProjectDir),
	}
}

// projectSlug is the stable name a project is restored under: its git remote
// as org-repo when it has one, else its directory name.
func projectSlug(projectDir string) string {
	if slug := projects.Slugify(filepath.Base(projectDir)); slug != "" {
		return slug
	}
	return "project"
}

// ownedTTL is how long this daemon's own lease rows stay live between
// heartbeats. A daemon that idles out on its own keeps them for that long.
func (s *Service) ownedTTL() time.Duration {
	if s.cfg.IdleExit > 0 {
		return s.cfg.IdleExit + leaseGrace
	}
	return ownedLeaseTTL
}

func (s *Service) workspaceFor(session agentdomain.Session) string {
	return filepath.Join(s.cfg.WorkspaceRoot, session.Slug, session.ConversationID)
}

func (s *Service) remember(conversationID string, env map[string]string) {
	pairs := make([]string, 0, len(env))
	for key, value := range env {
		pairs = append(pairs, key+"="+value)
	}
	slices.Sort(pairs)
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(pairs) == 0 {
		delete(s.credentials, conversationID)
		return
	}
	s.credentials[conversationID] = pairs
}

func (s *Service) credentialsFor(conversationID string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.credentials[conversationID])
}

func runError(message string) []byte {
	return encode(map[string]string{"type": frameRunError, "message": message})
}

// describe wraps err with what was being done, keeping the chain for errors.Is.
func describe(err error, format string, args ...any) error {
	return fmt.Errorf(format+": %w", append(args, err)...)
}
