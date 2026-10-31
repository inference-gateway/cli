package teleport

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
// workspace whose idle exit says how long its sessions stay its own.
const (
	leaseHeartbeat = 30 * time.Second
	ownedLeaseTTL  = 90 * time.Second
	leaseGrace     = 5 * time.Minute
	probeTimeout   = 5 * time.Second
)

// Config is what the daemon tells the service about itself.
type Config struct {
	Identity     string
	ProjectsRoot string
	IdleExit     time.Duration
	Version      string
	StorageID    string
	// LeaseBackend names the storage type the lease store lives on, to warn
	// when two daemons cannot see each other's rows.
	LeaseBackend config.StorageType
	Handshake    agui.Handshake
	Entries      []config.WorkspaceEntry
	// StateDir keeps what the daemon remembers about the sessions it teleported
	// away, such as the checkout each one left. Empty remembers nothing.
	StateDir string
	// Denies reports whether the sandbox policy keeps a project file out of a
	// checkpoint.
	Denies func(projectDir, absPath string) bool
}

// Deps are the collaborators the service orchestrates.
type Deps struct {
	Leases     agentdomain.LeaseStore
	Workspaces []agentdomain.Workspace
	Keyring    *agentinfra.Keyring
	Launch     sessionsdomain.LaunchWorker
}

// Service runs agents on workspaces: it decides where a thread's worker runs
// (the launch seam the registry calls), teleports sessions between workspaces, and
// receives the sessions other daemons teleport here.
type Service struct {
	cfg        Config
	me         string
	leases     agentdomain.LeaseStore
	workspaces map[string]agentdomain.Workspace
	entries    map[string]config.WorkspaceEntry
	keyring    *agentinfra.Keyring
	launch     sessionsdomain.LaunchWorker
	dial       agentinfra.PeerDialer
	registry   *sessions.Registry

	mu            sync.Mutex
	teleports     map[string]context.CancelFunc
	conversations map[string]string
	imports       map[string]*pendingImport
	installed     map[string]agentdomain.Lease
	returns       map[string]sessionsdomain.ThreadKey
	credentials   map[string][]string
}

// New builds the service. AttachRegistry must follow once the registry exists,
// since the registry itself is built over LaunchWorker.
func New(cfg Config, deps Deps) *Service {
	s := &Service{
		cfg:           cfg,
		me:            cfg.Identity,
		leases:        deps.Leases,
		workspaces:    make(map[string]agentdomain.Workspace, len(deps.Workspaces)),
		entries:       make(map[string]config.WorkspaceEntry, len(cfg.Entries)),
		keyring:       deps.Keyring,
		launch:        deps.Launch,
		dial:          agentinfra.PeerDialer{Me: cfg.Identity, Handshake: cfg.Handshake},
		teleports:     make(map[string]context.CancelFunc),
		conversations: make(map[string]string),
		imports:       make(map[string]*pendingImport),
		installed:     make(map[string]agentdomain.Lease),
		returns:       make(map[string]sessionsdomain.ThreadKey),
		credentials:   make(map[string][]string),
	}
	for _, workspace := range deps.Workspaces {
		s.workspaces[workspace.Name()] = workspace
	}
	for _, entry := range cfg.Entries {
		s.entries[entry.Name] = entry
	}
	return s
}

// AttachRegistry hands the service the thread registry its teleports quiesce,
// ask and notify.
func (s *Service) AttachRegistry(registry *sessions.Registry) {
	s.registry = registry
}

// AckAttrs are the facts the binding's ack declares about this daemon.
func (s *Service) AckAttrs() map[string]any {
	return map[string]any{
		agentinfra.AttrWorkspace: s.me,
		agentinfra.AttrVersion:   s.cfg.Version,
		agentinfra.AttrStorageID: s.cfg.StorageID,
	}
}

// Handle claims the workspaces and teleports frames: the client requests, and
// the peer frames when the connection is another daemon's.
func (s *Service) Handle(conn *agui.Conn, frame []byte) bool {
	var f clientFrame
	if json.Unmarshal(frame, &f) != nil {
		return false
	}
	switch f.Type {
	case frameListWorkspaces:
		go s.listWorkspaces(conn)
	case frameTeleportSession:
		go s.startTeleport(conn, f)
	case frameTeleportCancel:
		s.cancelTeleport(f.TeleportID)
	case frameTeleportChunk, frameTeleportImport, frameTeleportAbort, frameTeleportReturn, frameTeleportReturned:
		if conn.Kind() != agentinfra.PeerClientKind {
			logger.Warn("teleport refused a peer frame from a client that is not a daemon", "client", conn.Kind(), "frame", f.Type)
			return true
		}
		s.receive(conn, f.Type, frame)
	default:
		return false
	}
	return true
}

// listWorkspaces answers list_workspaces with every workspace's reachability,
// this daemon first, probing the others in parallel.
func (s *Service) listWorkspaces(conn *agui.Conn) {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout+time.Second)
	defer cancel()
	names := slices.Sorted(maps.Keys(s.workspaces))
	statuses := make([]agentdomain.WorkspaceStatus, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Go(func() { statuses[i] = s.workspaces[name].Probe(ctx) })
	}
	wg.Wait()
	slices.SortStableFunc(statuses, func(a, b agentdomain.WorkspaceStatus) int {
		return boolRank(b.Kind == agentdomain.WorkspaceKindLocal) - boolRank(a.Kind == agentdomain.WorkspaceKindLocal)
	})
	conn.Deliver(encode(workspacesFrame{Type: frameWorkspaces, Workspaces: statuses}))
}

func boolRank(b bool) int {
	if b {
		return 1
	}
	return 0
}

// StopSession handles a client dropping a session: when a remote workspace owns
// it, the session's runtime there is deleted and the lease released, so the
// session starts here next time. The client gets thread_stopped, naming the
// workspace whose runtime went or the error that kept it.
func (s *Service) StopSession(ctx context.Context, to requester, key sessionsdomain.ThreadKey) {
	stopped := threadStoppedFrame{Type: frameThreadStopped, ProjectDir: key.ProjectDir, ID: key.ConversationID}
	workspace, err := s.deleteRemote(ctx, key)
	stopped.Workspace = workspace
	if err != nil {
		logger.Warn("teleport could not delete the session's runtime on its workspace", "conversation_id", key.ConversationID, "error", err)
		stopped.Error = err.Error()
	}
	to.Deliver(encode(stopped))
}

// deleteRemote deletes the session's runtime on the remote workspace that
// owns it and releases the lease, returning that workspace, or "" when the
// session is this daemon's or nobody's.
func (s *Service) deleteRemote(ctx context.Context, key sessionsdomain.ThreadKey) (string, error) {
	lease, found, err := s.leases.GetLease(ctx, key.ConversationID)
	if err != nil {
		return "", describe(err, "reading the session's lease")
	}
	if !found || lease.Workspace == s.me {
		return "", nil
	}
	workspace, ok := s.workspaces[lease.Workspace]
	if !ok {
		return "", fmt.Errorf("workspace %s is not registered in daemon.yaml", lease.Workspace)
	}
	if err := workspace.Delete(ctx, s.session(key)); err != nil {
		return "", describe(err, "deleting the session's runtime on %s", lease.Workspace)
	}
	if err := s.leases.ReleaseLease(ctx, key.ConversationID, lease.Workspace); err != nil {
		return "", describe(err, "releasing the session's lease")
	}
	return lease.Workspace, nil
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

func (s *Service) workspaceDirFor(session agentdomain.Session) string {
	return filepath.Join(s.cfg.ProjectsRoot, session.Slug, session.ConversationID)
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
