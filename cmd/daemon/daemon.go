package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	cobra "github.com/spf13/cobra"

	runtime "github.com/inference-gateway/cli/cmd/runtime"
	version "github.com/inference-gateway/cli/cmd/version"
	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	agentinfra "github.com/inference-gateway/cli/internal/agent/infrastructure"
	k8senvironment "github.com/inference-gateway/cli/internal/agent/infrastructure/kubernetes"
	agenttransfer "github.com/inference-gateway/cli/internal/agent/transfer"
	audio "github.com/inference-gateway/cli/internal/audio"
	browserinfra "github.com/inference-gateway/cli/internal/browser/infrastructure"
	channels "github.com/inference-gateway/cli/internal/channels"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	githubsetup "github.com/inference-gateway/cli/internal/github/setup"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
	telemetry "github.com/inference-gateway/cli/internal/platform/telemetry"
	utils "github.com/inference-gateway/cli/internal/platform/utils"
	shortcuts "github.com/inference-gateway/cli/internal/presentation/shortcuts"
	telegram "github.com/inference-gateway/cli/internal/presentation/telegram"
	a2ainfra "github.com/inference-gateway/cli/internal/protocols/a2a/infrastructure"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
	sandbox "github.com/inference-gateway/cli/internal/sandbox"
	scheduler "github.com/inference-gateway/cli/internal/scheduler"
	githubscheduler "github.com/inference-gateway/cli/internal/scheduler/githubscheduler"
	heartbeat "github.com/inference-gateway/cli/internal/scheduler/heartbeat"
	sessions "github.com/inference-gateway/cli/internal/sessions"
	sessionsdomain "github.com/inference-gateway/cli/internal/sessions/domain"
	sessionsinfra "github.com/inference-gateway/cli/internal/sessions/infrastructure"
)

func NewCommand(state *runtime.State) *cobra.Command {
	command := &cobra.Command{
		Use:   "daemon",
		Short: "Start the background daemon: scheduler, channels, heartbeat, and the AG-UI binding",
		Long: `Start a long-running daemon hosting up to four subsystems, whichever are
enabled in config:

  - scheduler:  fires scheduled jobs (tools.schedule.enabled); each fire runs an
    agent and records the run to storage, so job output is consumable even
    without a delivery channel
  - channels:   listens for messages from external platforms (e.g., Telegram)
    and triggers the agent for each incoming message; also delivers scheduled
    job output to the job's channel
  - heartbeat:  periodic agent wake-ups (heartbeat.enabled)
  - binding:    the AG-UI WebSocket binding the opentask extension and the
    desktop app connect to (browser_use.enabled with backend: extension, on
    browser_use.extension.port). Each thread runs in its own session worker
  - a2a server: serves this agent over the A2A protocol (a2a.server.enabled,
    on a2a.server.host:a2a.server.port). Each task it accepts runs as one
    infer headless run, and every caller presents the bearer token from
    a2a.server.token

Each channel message spawns a new agent invocation with a deterministic session
ID per sender, so conversations persist across messages. Channel configuration
lives in ~/.infer/channels.yaml (seeded by 'infer init'), a project
.infer/channels.yaml that replaces it, or INFER_CHANNELS_* environment variables.

Examples:
  # Start the daemon (scheduler and/or channels, per config)
  infer daemon

  # With environment variables
  INFER_CHANNELS_ENABLED=true \
  INFER_CHANNELS_TELEGRAM_ENABLED=true \
  INFER_CHANNELS_TELEGRAM_BOT_TOKEN="your-token" \
  INFER_CHANNELS_TELEGRAM_ALLOWED_USERS="123456789" \
  infer daemon`,
		RunE: func(cmd *cobra.Command, args []string) error {
			state.ConfigureDaemonLogging()
			return RunDaemonCommand(state.Config())
		},
	}
	command.AddCommand(newEnvironmentsCommand(state), newTransferCommand(state), newRunCommand(state))
	return command
}

// RunDaemonCommand starts the daemon. It hosts up to five subsystems -
// channels, scheduler, heartbeat, the AG-UI binding, and the A2A server - and
// starts whichever are enabled. At least one must be enabled or the daemon refuses to boot
// (otherwise it would just sleep forever).
func RunDaemonCommand(cfg *config.Config) error { //nolint:gocyclo,cyclop,funlen
	if !cfg.Channels.Enabled && !cfg.Tools.Schedule.Enabled && !cfg.Heartbeat.Enabled && !cfg.Binding().Enabled && !cfg.A2A.Server.Enabled {
		return fmt.Errorf("nothing to run: enable at least one of channels, scheduler, heartbeat, the AG-UI binding (browser_use with backend: extension), or the A2A server (a2a.server) in .infer/")
	}

	release, err := acquireDaemonLock()
	if err != nil {
		return err
	}
	defer release()

	telemetry.ExecutionMode = telemetry.ExecDaemon
	sessionID := convdomain.GenerateSessionID()
	tel := telemetry.New(telemetry.Options{
		Enabled:           cfg.Telemetry.Enabled,
		Dir:               config.TelemetryDir(),
		SessionID:         string(sessionID),
		OTLPEndpoint:      cfg.Telemetry.OTLP.Endpoint,
		OTLPHeaders:       cfg.Telemetry.OTLP.Headers,
		OTLPInterval:      time.Duration(cfg.Telemetry.OTLP.Interval) * time.Second,
		AttrSessionIDKey:  cfg.Telemetry.AttrSessionIDKey,
		AttrToolCallIDKey: cfg.Telemetry.AttrToolCallIDKey,
	})

	cm := telegram.NewChannelManagerService(cfg.Channels, tel)

	var channelCommands []channels.ChannelCommand
	if cfg.Channels.Enabled {
		if reg, conv, groups := buildCommandSupport(cfg); reg != nil {
			cm.SetCommandSupport(reg, conv, groups)
			channelCommands = supportedChannelCommands(reg)
		}
		if err := registerChannels(cm, cfg, channelCommands); err != nil {
			return err
		}
	}

	logger.Info("starting daemon", "version", version.Value())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	threads, err := startThreads(ctx, cfg, cm)
	if err != nil {
		return err
	}
	stopThreads := threads.stop

	if cfg.Channels.Enabled {
		logger.Info("starting channel listener...")
		if err := cm.Start(ctx); err != nil {
			return fmt.Errorf("failed to start channels: %w", err)
		}
	}

	sched, err := startScheduler(ctx, cm, cfg)
	if err != nil {
		_ = cm.Stop()
		return fmt.Errorf("failed to start scheduler: %w", err)
	}

	hb, err := startHeartbeat(ctx, cfg)
	if err != nil {
		if sched != nil {
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
			_ = sched.Stop(stopCtx)
			stopCancel()
		}
		_ = cm.Stop()
		return fmt.Errorf("failed to start heartbeat: %w", err)
	}

	a2aSrv, err := startA2AServer(ctx, cfg)
	if err != nil {
		if hb != nil {
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
			_ = hb.Stop(stopCtx)
			stopCancel()
		}
		if sched != nil {
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
			_ = sched.Stop(stopCtx)
			stopCancel()
		}
		_ = cm.Stop()
		return fmt.Errorf("failed to start the A2A server: %w", err)
	}

	poller, err := startArtifactPoller(ctx, cfg)
	if err != nil {
		logger.Error("failed to start github artifact poller", "error", err)
	}

	logger.Info("daemon ready. Press Ctrl+C to stop.")

	select {
	case <-sigChan:
	case <-threads.idle:
		logger.Info("the daemon idled out", "idle_exit", cfg.Daemon.IdleExit)
	}
	logger.Info("shutting down...")
	cancel()
	stopThreads()

	if poller != nil {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
		if err := poller.Stop(stopCtx); err != nil {
			logger.Error("failed to stop github artifact poller", "error", err)
		}
		stopCancel()
	}

	if a2aSrv != nil {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
		if a2aStopErr := a2aSrv.Stop(stopCtx); a2aStopErr != nil {
			logger.Error("failed to stop the A2A server", "error", a2aStopErr)
		}
		stopCancel()
	}

	if hb != nil {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
		if err := hb.Stop(stopCtx); err != nil {
			logger.Error("failed to stop heartbeat", "error", err)
		}
		stopCancel()
	}

	if sched != nil {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
		if err := sched.Stop(stopCtx); err != nil {
			logger.Error("failed to stop scheduler", "error", err)
		}
		stopCancel()
	}

	stopErr := cm.Stop()

	telCtx, telCancel := context.WithTimeout(context.Background(), 10*time.Second)
	tel.Shutdown(telCtx)
	telCancel()

	if stopErr != nil {
		return fmt.Errorf("failed to stop channels: %w", stopErr)
	}

	logger.Info("daemon stopped.")
	return nil
}

// workerIdleTimeout is how long a session worker nobody follows may sit
// idle before the daemon stops it. One value with the sessions context,
// which also owns the chat-side idle grace.
const workerIdleTimeout = sessionsdomain.IdleTimeout

// threadsEnabled reports whether the daemon hosts session threads: the AG-UI
// binding, whose clients follow threads, or the channels, whose chats drive
// threads of their own.
func threadsEnabled(cfg *config.Config) bool {
	return cfg.Binding().Enabled || cfg.Channels.Enabled
}

// bindingHandler puts the binding's consumers behind its one handler. The
// extension relay claims the browser frames, the transfer service the
// environments and transfer frames, and the thread registry gets the rest.
type bindingHandler struct {
	relay     *browserinfra.ExtensionRelay
	transfers *agenttransfer.Service
	registry  *sessions.Registry
}

func (h bindingHandler) Attach(conn *agui.Conn) {
	if h.relay != nil {
		h.relay.Attach(conn)
	}
	h.registry.Attach(conn)
}

func (h bindingHandler) Handle(conn *agui.Conn, frame []byte) {
	if h.relay != nil && h.relay.Handle(conn, frame) {
		return
	}
	if h.transfers.Handle(conn, frame) {
		return
	}
	if key, ok := stoppedThread(frame); ok {
		h.transfers.StopSession(context.Background(), key)
	}
	h.registry.Handle(conn, frame)
}

// stoppedThread reads the thread a stop_thread frame names.
func stoppedThread(frame []byte) (sessionsdomain.ThreadKey, bool) {
	var f struct {
		Type       string `json:"type"`
		ProjectDir string `json:"project_dir"`
		ID         string `json:"id"`
	}
	if json.Unmarshal(frame, &f) != nil || f.Type != "stop_thread" || f.ProjectDir == "" || f.ID == "" {
		return sessionsdomain.ThreadKey{}, false
	}
	return sessionsdomain.ThreadKey{ProjectDir: f.ProjectDir, ConversationID: f.ID}, true
}

func (h bindingHandler) Detach(conn *agui.Conn) {
	if h.relay != nil {
		h.relay.Detach(conn)
	}
	h.registry.Detach(conn)
}

// threadHost is what startThreads hands back: how to stop the threads and a
// channel that closes when the daemon idled past daemon.idle_exit.
type threadHost struct {
	stop func()
	idle <-chan struct{}
}

// startThreads starts the daemon's thread registry for every session worker
// it hosts, and serves the AG-UI WebSocket binding on its own switch, host,
// port, token and TLS. The browser extension relay attaches only with the
// extension backend, the channels drive their chats as clients of the
// registry, and the transfer service decides where each thread runs. stop,
// called after ctx ends, closes the binding and waits for every session
// worker to exit. The daemon is the only infer process that binds the binding
// port, so a port that is already held fails the boot.
func startThreads(ctx context.Context, cfg *config.Config, cm *telegram.ChannelManagerService) (threadHost, error) { //nolint:funlen
	if !threadsEnabled(cfg) {
		return threadHost{stop: func() {}}, nil
	}

	var relay *browserinfra.ExtensionRelay
	if cfg.BrowserUse.Enabled && cfg.BrowserUse.Backend == config.BrowserBackendExtension {
		relay = browserinfra.NewExtensionRelay(cfg.BrowserUse.Extension, nil)
	}

	transfers, tlsProvider, err := buildTransfers(cfg)
	if err != nil {
		return threadHost{}, err
	}
	registry := sessions.NewRegistry(transfers.LaunchWorker, workerIdleTimeout)
	transfers.AttachRegistry(registry)
	if relay != nil {
		registry.RouteBrowser(relay.Relay)
	}
	if cfg.Channels.Enabled {
		projectDir, err := os.Getwd()
		if err != nil {
			return threadHost{}, fmt.Errorf("resolving the working directory: %w", err)
		}
		cm.SetThreadDriver(registry, projectDir, sessionsdomain.ThreadOptions{
			SystemPrompt: cfg.Prompts.Agent.SystemPromptRemote,
		})
	}

	var binding *agui.Binding
	if b := cfg.Binding(); b.Enabled {
		if b.Token == "" && !b.TLS.RequiresClientAuth() {
			return threadHost{}, errors.New("the AG-UI binding token is empty - set daemon.binding.token, or browser_use.extension.token as the fallback in browser_use.yaml, or require TLS client certificates")
		}
		serverTLS, err := tlsProvider.Server(ctx, b.TLS)
		if err != nil {
			return threadHost{}, fmt.Errorf("configuring the AG-UI binding's TLS: %w", err)
		}
		binding = agui.NewBinding(agui.BindingConfig{
			Host:        b.ListenHost(),
			Port:        b.Port,
			Token:       b.Token,
			TLS:         serverTLS,
			AckAttrs:    transfers.AckAttrs(),
			Handshake:   browserinfra.BindingHandshake,
			AllowOrigin: browserinfra.AllowExtensionOrigin,
			Routes:      map[string]http.Handler{"/artifacts/": newArtifactsHandler()},
		}, bindingHandler{relay: relay, transfers: transfers, registry: registry})
		if err := binding.Start(); err != nil {
			return threadHost{}, fmt.Errorf("failed to start the AG-UI binding: %w", err)
		}
	}

	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		registry.Run(ctx)
	}()
	idle := watchIdle(ctx, cfg.Daemon.IdleExit, registry, transfers)
	return threadHost{
		stop: func() {
			if binding != nil {
				binding.Close()
			}
			<-stopped
			tlsProvider.Close()
		},
		idle: idle,
	}, nil
}

// idleCheck is how often the daemon looks at whether it idled out.
const idleCheck = 30 * time.Second

// watchIdle closes the returned channel once the registry has had no thread
// and no client for idleExit, after parking this daemon's own runtime. A zero
// idleExit never closes it.
func watchIdle(ctx context.Context, idleExit time.Duration, registry *sessions.Registry, transfers *agenttransfer.Service) <-chan struct{} {
	idle := make(chan struct{})
	if idleExit <= 0 {
		return idle
	}
	go func() {
		ticker := time.NewTicker(idleCheck)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			if since, isIdle := registry.Idle(); isIdle && time.Since(since) >= idleExit {
				transfers.Suspend(ctx)
				close(idle)
				return
			}
		}
	}()
	return idle
}

// buildTransfers wires the transfer service: the lease store on the
// configured storage, the daemon CA when a mode needs it, the TLS provider,
// and one environment per daemon.yaml entry plus this daemon itself.
func buildTransfers(cfg *config.Config) (*agenttransfer.Service, *agentinfra.TLSProvider, error) {
	identity := cfg.Daemon.Identity()
	storageConfig := storage.NewStorageFromConfig(cfg)
	stores, err := storage.NewStorage(storageConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("opening storage for the session leases: %w", err)
	}
	var pki *agentinfra.PKI
	if needsPKI(cfg) {
		if pki, err = agentinfra.LoadOrCreatePKI(filepath.Join(config.UserSpaceConfigDir(), "daemon", "pki")); err != nil {
			return nil, nil, fmt.Errorf("opening the daemon CA: %w", err)
		}
	}
	tlsProvider := agentinfra.NewTLSProvider(identity, pki, cfg.Daemon.Binding.TLS.SPIFFE)
	dial := agentinfra.PeerDialer{Me: identity, Handshake: browserinfra.BindingHandshake}
	environments := []agentdomain.Environment{agentinfra.NewLocalEnvironment(identity, map[string]string{
		agentinfra.AttrVersion: version.Value(), "storage": string(storageConfig.Type),
	}, selfSuspend())}
	for _, entry := range cfg.Daemon.Environments {
		environment, err := buildEnvironment(entry, identity, tlsProvider, pki, dial)
		if err != nil {
			return nil, nil, err
		}
		if environment != nil {
			environments = append(environments, environment)
		}
	}
	service := agenttransfer.New(agenttransfer.Config{
		Identity:      identity,
		WorkspaceRoot: cfg.Daemon.WorkspaceDir(),
		IdleExit:      cfg.Daemon.IdleExit,
		Version:       version.Value(),
		StorageID:     storageIdentity(storageConfig),
		Handshake:     browserinfra.BindingHandshake,
		Entries:       cfg.Daemon.Environments,
		Denies:        func(projectDir, absPath string) bool { return sandbox.Denies(cfg, projectDir, absPath) },
	}, agenttransfer.Deps{
		Leases:       stores.Leases,
		Environments: environments,
		Keyring:      tlsProvider.Keyring(),
		Launch:       sessionsinfra.LaunchWorker,
	})
	return service, tlsProvider, nil
}

func buildEnvironment(entry config.EnvironmentEntry, identity string, tlsProvider *agentinfra.TLSProvider, pki *agentinfra.PKI, dial agentinfra.PeerDialer) (agentdomain.Environment, error) {
	switch entry.Kind {
	case config.EnvironmentKindDaemon:
		environment, err := agentinfra.NewDaemonEnvironment(entry, tlsProvider, dial)
		if err != nil {
			return nil, fmt.Errorf("environment %s: %w", entry.Name, err)
		}
		return environment, nil
	case config.EnvironmentKindKubernetes:
		environment, err := k8senvironment.New(entry, identity, tlsProvider, pki)
		if err != nil {
			return nil, fmt.Errorf("environment %s: %w", entry.Name, err)
		}
		return environment, nil
	default:
		return nil, nil
	}
}

// needsPKI reports whether any configured mode relies on the daemon CA.
func needsPKI(cfg *config.Config) bool {
	if cfg.Daemon.Binding.TLS.Mode == config.TLSModeCA {
		return true
	}
	for _, entry := range cfg.Daemon.Environments {
		if entry.Kind == config.EnvironmentKindKubernetes && entry.EffectiveAuth() == config.EnvironmentAuthCA {
			return true
		}
	}
	return false
}

// selfSuspend is how a pod daemon parks its own Sandbox, nil outside one.
func selfSuspend() func(ctx context.Context) error {
	name, namespace := os.Getenv(k8senvironment.EnvSandboxName), os.Getenv(k8senvironment.EnvSandboxNamespace)
	if name == "" || namespace == "" {
		return nil
	}
	suspend, err := k8senvironment.SelfSuspend(name, namespace)
	if err != nil {
		logger.Warn("this daemon cannot suspend its own Sandbox", "error", err)
		return nil
	}
	return suspend
}

// storageIdentity names a storage backend so two daemons can tell whether
// they share one and skip copying conversations.
func storageIdentity(cfg storage.StorageConfig) string {
	var identity string
	switch cfg.Type {
	case config.StorageTypePostgres:
		identity = fmt.Sprintf("postgres://%s:%d/%s", cfg.Postgres.Host, cfg.Postgres.Port, cfg.Postgres.Database)
	case config.StorageTypeRedis:
		identity = fmt.Sprintf("redis://%s:%d/%d", cfg.Redis.Host, cfg.Redis.Port, cfg.Redis.Database)
	case config.StorageTypeD1:
		identity = "d1://" + cfg.D1.AccountID + "/" + cfg.D1.DatabaseID
	default:
		return ""
	}
	sum := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(sum[:])
}

// acquireDaemonLock enforces one daemon per machine via a PID file in
// ~/.infer/run/daemon.pid (next to gateway.pid). Stale files (dead PID) are
// overwritten. The returned release func removes the file.
// ponytail: check-then-write race; flock if two daemons ever start in the same instant.
func acquireDaemonLock() (func(), error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return func() {}, nil
	}
	pidPath := filepath.Join(home, config.ConfigDirName, "run", "daemon.pid")
	if data, err := os.ReadFile(pidPath); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid != os.Getpid() && utils.ProcessAlive(pid) {
			return nil, fmt.Errorf("daemon already running (pid %d); stop it or remove %s", pid, pidPath)
		}
	}
	if err := os.MkdirAll(filepath.Dir(pidPath), 0755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
		return nil, err
	}
	return func() { _ = os.Remove(pidPath) }, nil
}

// startScheduler initialises the schedule scheduler service when the schedule
// tool is enabled. Returns nil scheduler when disabled.
func startScheduler(ctx context.Context, cm *telegram.ChannelManagerService, cfg *config.Config) (*scheduler.Service, error) {
	if !cfg.Tools.Schedule.Enabled {
		return nil, nil
	}
	if cfg.Scheduler.Backend == config.SchedulerBackendGitHub {
		logger.Info("scheduler backend is github; jobs run on GitHub Actions, local cron disabled")
		return nil, nil
	}

	storageConfig := storage.NewStorageFromConfig(cfg)
	if storageConfig.Type == config.StorageTypeMemory {
		return nil, fmt.Errorf("the scheduler requires persistent storage: set storage.enabled: true and a non-memory storage.type (e.g. jsonl)")
	}
	stores, err := storage.NewStorage(storageConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize storage: %w", err)
	}

	notifier := scheduler.NewScheduleNotifier(cm.GetChannel)
	svc, err := scheduler.NewService(scheduler.Options{
		Store:      stores.ScheduledJobs,
		Runs:       stores.ScheduledRuns,
		OnRunEvent: notifier.Notify,
	})
	if err != nil {
		return nil, err
	}
	if err := svc.Start(ctx); err != nil {
		return nil, err
	}
	return svc, nil
}

// startArtifactPoller starts the GitHub artifact poller when the github
// scheduler backend is active with artifacts enabled. It pulls conversation
// jsonl files uploaded by GitHub-backed job runs into local storage, so it
// only runs on the jsonl storage backend. Returns nil when not applicable.
func startArtifactPoller(ctx context.Context, cfg *config.Config) (*githubscheduler.ArtifactPoller, error) {
	art := cfg.Scheduler.GitHub.Artifacts
	if cfg.Scheduler.Backend != config.SchedulerBackendGitHub || !art.Enabled {
		return nil, nil
	}
	if cfg.Storage.Type != config.StorageTypeJsonl {
		logger.Warn("github artifact polling requires storage.type jsonl; skipping", "type", cfg.Storage.Type)
		return nil, nil
	}

	interval, err := time.ParseDuration(art.PollInterval)
	if err != nil {
		return nil, fmt.Errorf("parse scheduler.github.artifacts.poll_interval %q: %w", art.PollInterval, err)
	}
	var initialDelay time.Duration
	if art.InitialDelay != "" {
		if initialDelay, err = time.ParseDuration(art.InitialDelay); err != nil {
			return nil, fmt.Errorf("parse scheduler.github.artifacts.initial_delay %q: %w", art.InitialDelay, err)
		}
	}
	backoff := time.Hour
	if art.RateLimitBackoff != "" {
		if backoff, err = time.ParseDuration(art.RateLimitBackoff); err != nil {
			return nil, fmt.Errorf("parse scheduler.github.artifacts.rate_limit_backoff %q: %w", art.RateLimitBackoff, err)
		}
	}

	runner := &githubsetup.RealRunner{}
	repo, err := githubscheduler.ResolveRepo(ctx, runner, cfg.Scheduler.GitHub.Repository)
	if err != nil {
		return nil, err
	}

	conversationsDir := storage.NewStorageFromConfig(cfg).Jsonl.Path
	if rest, ok := strings.CutPrefix(conversationsDir, "~"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			conversationsDir = filepath.Join(home, rest)
		}
	}
	statePath := ""
	if home, err := os.UserHomeDir(); err == nil {
		statePath = filepath.Join(home, config.ConfigDirName, "schedules", "github-artifacts-state.json")
	}
	artifactsDir := ""
	if home, err := os.UserHomeDir(); err == nil {
		artifactsDir = filepath.Join(home, config.ConfigDirName, config.ArtifactsDirName)
	}

	svc, err := githubscheduler.NewArtifactPoller(githubscheduler.ArtifactPollerOptions{
		Runner:           runner,
		Repo:             repo,
		ConversationsDir: conversationsDir,
		ArtifactsDir:     artifactsDir,
		StatePath:        statePath,
		Interval:         interval,
		InitialDelay:     initialDelay,
		MaxAttempts:      art.MaxAttempts,
		RateLimitBackoff: backoff,
	})
	if err != nil {
		return nil, err
	}
	if err := svc.Start(ctx); err != nil {
		return nil, err
	}
	return svc, nil
}

// startHeartbeat initialises the heartbeat service when enabled.
// Returns nil service when disabled. Parses interval/initial_delay as
// time.Duration strings and surfaces parse errors so the daemon fails
// fast on bad config.
func startHeartbeat(ctx context.Context, cfg *config.Config) (*heartbeat.Service, error) {
	if !cfg.Heartbeat.Enabled {
		return nil, nil
	}

	interval, err := time.ParseDuration(cfg.Heartbeat.Interval)
	if err != nil {
		return nil, fmt.Errorf("parse heartbeat.interval %q: %w", cfg.Heartbeat.Interval, err)
	}

	var initialDelay time.Duration
	if cfg.Heartbeat.InitialDelay != "" {
		initialDelay, err = time.ParseDuration(cfg.Heartbeat.InitialDelay)
		if err != nil {
			return nil, fmt.Errorf("parse heartbeat.initial_delay %q: %w", cfg.Heartbeat.InitialDelay, err)
		}
	}

	prompt := cfg.Heartbeat.Prompt
	if prompt == "" {
		prompt = config.DefaultHeartbeatConfig().Prompt
	}

	svc, err := heartbeat.NewService(heartbeat.Options{
		Config: heartbeat.Config{
			Interval:     interval,
			InitialDelay: initialDelay,
			Model:        cfg.Heartbeat.Model,
			Prompt:       prompt,
		},
	})
	if err != nil {
		return nil, err
	}
	if err := svc.Start(ctx); err != nil {
		return nil, err
	}
	return svc, nil
}

// startA2AServer hosts the A2A server when a2a.server is enabled. The bearer
// token is required: the server exposes the coding agent over HTTP, so a
// loopback bind alone is not protection enough against a rogue web page. Start
// blocks, so it runs in a goroutine and this function waits for the health
// endpoint; a boot failure here fails the daemon. The served tasks run in
// the daemon's working directory, each bounded by a2a.task.agent_mode_max_wait_seconds.
func startA2AServer(ctx context.Context, cfg *config.Config) (*a2ainfra.Server, error) {
	srvCfg := cfg.A2A.Server
	if !srvCfg.Enabled {
		return nil, nil
	}
	if srvCfg.Token == "" {
		return nil, errors.New("the A2A server is enabled but a2a.server.token is empty: set it in .infer/config.yaml or INFER_A2A_SERVER_TOKEN")
	}
	if _, ok := agentdomain.ParseAgentMode(srvCfg.Mode); srvCfg.Mode != "" && !ok {
		return nil, fmt.Errorf("invalid a2a.server.mode %q: must be one of standard, plan, auto, auto-with-judge", srvCfg.Mode)
	}

	srv, err := a2ainfra.NewServer(a2ainfra.Settings{
		Host:        srvCfg.Host,
		Port:        srvCfg.Port,
		Token:       srvCfg.Token,
		Mode:        srvCfg.Mode,
		TaskTimeout: time.Duration(cfg.A2A.Task.AgentModeMaxWaitSeconds) * time.Second,
		Version:     version.Value(),
	})
	if err != nil {
		return nil, err
	}

	addr := fmt.Sprintf("%s:%d", srvCfg.Host, srvCfg.Port)
	exited := make(chan error, 1)
	go func() { exited <- srv.Start(ctx) }()
	if err := waitReachable(ctx, addr, exited); err != nil {
		return nil, err
	}
	go logServerExit(exited)
	logger.Info("A2A server listening", "address", addr, "mode", srvCfg.Mode)
	return srv, nil
}

// logServerExit reports an A2A server that dies after a successful boot: Stop
// ends it with http.ErrServerClosed, anything else is a crash worth a log line.
func logServerExit(exited <-chan error) {
	if err := <-exited; err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("the A2A server exited", "error", err)
	}
}

// a2aBootWait bounds how long startA2AServer waits for the server to bind.
const a2aBootWait = 10 * time.Second

// waitReachable polls the server's public health endpoint until it answers or
// the server exits, so a bind conflict fails the boot instead of sleeping.
func waitReachable(ctx context.Context, addr string, exited chan error) error {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	deadline := time.Now().Add(a2aBootWait)
	for {
		if res, probeErr := client.Get(fmt.Sprintf("http://%s/health", addr)); probeErr == nil {
			_ = res.Body.Close()
			return nil
		}
		select {
		case startErr := <-exited:
			return fmt.Errorf("binding %s: %w", addr, startErr)
		default:
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("the A2A server did not bind %s in time", addr)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// buildVoiceRetention returns a retainer for inbound voice/audio files when
// speech_to_text.retain_recordings > 0, or nil to disable retention.
func buildVoiceRetention(cfg config.SpeechToTextConfig) *telegram.FileRetention {
	if cfg.RetainRecordings <= 0 {
		return nil
	}
	dir, err := cfg.ResolveRecordingsDir()
	if err != nil {
		logger.Warn("voice recording retention disabled", "error", err)
		return nil
	}
	logger.Info("retaining inbound voice recordings", "dir", dir, "keep", cfg.RetainRecordings)
	return telegram.NewVoiceRetention(dir, cfg.RetainRecordings)
}

// buildCommandSupport constructs the shortcut registry and storage handles
// backing slash commands. Returns nils (feature disabled) on memory storage —
// channel sessions only persist on real backends, so /clear would be a no-op.
func buildCommandSupport(cfg *config.Config) (*shortcuts.Registry, storage.ConversationStorage, storage.SessionGroupStorage) {
	storageConfig := storage.NewStorageFromConfig(cfg)
	if storageConfig.Type == config.StorageTypeMemory {
		logger.Info("channel slash commands disabled: persistent storage required (set storage.enabled: true)")
		return nil, nil, nil
	}
	stores, err := storage.NewStorage(storageConfig)
	if err != nil {
		logger.Warn("channel slash commands disabled: storage init failed", "error", err)
		return nil, nil, nil
	}
	return shortcuts.NewMetadataRegistry(cfg), stores.Conversations, stores.SessionGroups
}

// supportedChannelCommands lists the commands worth advertising natively in a
// channel's command menu: the daemon built-ins plus custom shortcuts.
func supportedChannelCommands(reg *shortcuts.Registry) []channels.ChannelCommand {
	cmds := append([]channels.ChannelCommand{}, telegram.ChannelBuiltinCommands...)
	for _, sc := range reg.GetAll() {
		if _, isCustom := sc.(*shortcuts.CustomShortcut); isCustom {
			cmds = append(cmds, channels.ChannelCommand{Name: sc.GetName(), Description: sc.GetDescription()})
		}
	}
	return cmds
}

// registerChannels registers enabled channel implementations with the manager
func registerChannels(cm *telegram.ChannelManagerService, cfg *config.Config, commands []channels.ChannelCommand) error {
	registered := 0

	if cfg.Channels.Telegram.Enabled {
		var transcriber telegram.VoiceTranscriber
		var retention *telegram.FileRetention
		if cfg.SpeechToText.Enabled {
			transcriber = audio.NewFileTranscriber(cfg.SpeechToText)
			retention = buildVoiceRetention(cfg.SpeechToText)
			logger.Info("speech-to-text enabled for inbound voice messages", "model", cfg.SpeechToText.Model)
		}
		telegramCh := telegram.NewTelegramChannel(cfg.Channels.Telegram, transcriber, retention)
		telegramCh.SetCommands(commands)
		cm.Register(telegramCh)
		registered++
		logger.Info("registered channel", "channel", "telegram")
	}

	if registered == 0 {
		return fmt.Errorf("no channels are enabled. Enable at least one channel in config")
	}

	return nil
}
