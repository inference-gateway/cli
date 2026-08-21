package container

import (
	"context"
	"fmt"
	statemanager "github.com/inference-gateway/cli/internal/presentation/tui/statemanager"
	toolformatter "github.com/inference-gateway/cli/internal/presentation/tui/toolformatter"
	"net"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	containerruntime "github.com/inference-gateway/cli/internal/platform/container"
	scheduler "github.com/inference-gateway/cli/internal/scheduler"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"

	sdk "github.com/inference-gateway/sdk"
	mockgateway "github.com/inference-gateway/tokenless/gateway"

	config "github.com/inference-gateway/cli/config"
	agent "github.com/inference-gateway/cli/internal/agent"
	agentapp "github.com/inference-gateway/cli/internal/agent/application"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	tools "github.com/inference-gateway/cli/internal/agent/tools"
	audio "github.com/inference-gateway/cli/internal/audio"
	browser "github.com/inference-gateway/cli/internal/browser"
	browserdomain "github.com/inference-gateway/cli/internal/browser/domain"
	browserinfra "github.com/inference-gateway/cli/internal/browser/infrastructure"
	computer "github.com/inference-gateway/cli/internal/computer"
	clipboardtext "github.com/inference-gateway/cli/internal/computer/infrastructure/clipboard/text"
	vlm "github.com/inference-gateway/cli/internal/computer/infrastructure/vlm"
	conversation "github.com/inference-gateway/cli/internal/conversation"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	gateway "github.com/inference-gateway/cli/internal/gateway"
	githubissues "github.com/inference-gateway/cli/internal/github/issues"
	githubsetup "github.com/inference-gateway/cli/internal/github/setup"
	mcp "github.com/inference-gateway/cli/internal/mcp"
	adapters "github.com/inference-gateway/cli/internal/platform/adapters"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	memory "github.com/inference-gateway/cli/internal/platform/memory"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
	telemetry "github.com/inference-gateway/cli/internal/platform/telemetry"
	shortcuts "github.com/inference-gateway/cli/internal/presentation/shortcuts"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	a2acoord "github.com/inference-gateway/cli/internal/presentation/tui/a2acoord"
	approvalcoord "github.com/inference-gateway/cli/internal/presentation/tui/approvalcoord"
	chatcompletion "github.com/inference-gateway/cli/internal/presentation/tui/chatcompletion"
	directexec "github.com/inference-gateway/cli/internal/presentation/tui/directexec"
	eventlistener "github.com/inference-gateway/cli/internal/presentation/tui/eventlistener"
	styles "github.com/inference-gateway/cli/internal/presentation/tui/styles"
	toolcoordinator "github.com/inference-gateway/cli/internal/presentation/tui/toolcoordinator"
	githubscheduler "github.com/inference-gateway/cli/internal/scheduler/githubscheduler"
	jobs "github.com/inference-gateway/cli/internal/scheduler/jobs"
	services "github.com/inference-gateway/cli/internal/services"
	skills "github.com/inference-gateway/cli/internal/skills"
)

// RetryNotifier, when set, receives a short human-readable notice for each
// SDK-internal HTTP retry (e.g. "⏳ HTTP 502 - retrying in 10s (attempt 2)").
// The headless agent points it at its stdout notification stream so remote
// channels (Telegram) see progress during backoff; the chat TUI leaves it nil.
var RetryNotifier func(message string)

// ServiceContainer manages all application dependencies
type ServiceContainer struct {
	// Session
	sessionID convdomain.SessionID

	// Container runtime
	containerRuntime containerruntime.ContainerRuntime

	// Configuration
	config *config.Config

	// Domain services
	conversationRepo       convdomain.ConversationRepository
	conversationOptimizer  convdomain.ConversationOptimizer
	sessionRolloverManager *conversation.SessionRolloverManager
	modelService           convdomain.ModelService
	agent                  agentdomain.AgentService
	toolService            agentdomain.ToolService
	fileService            agentdomain.FileService
	imageService           agentdomain.ImageService
	imageAnnotator         agentdomain.ImageAnnotator
	pricingService         convdomain.PricingService
	telemetryRecorder      *telemetry.Recorder
	a2aAgentService        agentapp.A2AAgentService
	skillsService          agentdomain.SkillsService
	githubIssueService     agentdomain.GitHubIssueService
	gitHubSetupService     agentdomain.GitHubSetupService
	messageQueue           convdomain.MessageQueue
	// backgroundTaskRegistry is the single unified tracker for both A2A
	// tasks and background bash shells. The narrower agentdomain.A2ATaskTracker
	// and scheddomain.ShellTracker views are accessed via the same instance.
	backgroundTaskRegistry scheddomain.BackgroundTaskRegistry
	jobSupervisor          *jobs.Supervisor
	taskRetentionService   scheddomain.TaskRetentionService
	backgroundTaskService  scheddomain.BackgroundTaskService
	gatewayManager         *gateway.Manager
	mockGateway            *http.Server
	agentManager           agentdomain.AgentManager

	// Services
	stateManager *statemanager.StateManager

	// Background services
	titleGenerator         *conversation.ConversationTitleGenerator
	backgroundJobManager   *scheduler.BackgroundJobManager
	backgroundShellService *scheduler.BackgroundShellService
	memoryBackend          memory.MemoryBackend
	storage                storage.ConversationStorage
	stores                 *storage.Stores

	// Token polyfill - used by /context, conversation optimizer, and the
	// session rollover manager. Created unconditionally so any surface can
	// fall back to it when the provider does not return usage metrics.
	tokenizer *conversation.TokenizerService

	// UI components
	themeService tui.ThemeService

	// Extensibility
	shortcutRegistry *shortcuts.Registry

	// Tool registry
	toolRegistry *tools.Registry
	mcpManager   agentdomain.MCPManager
	// mcpStartupCancel aborts the async MCP server startup on Shutdown.
	mcpStartupCancel context.CancelFunc

	// Chat orchestration services - extracted from internal/handlers/chat_handler.go.
	// Constructed unconditionally; A2A-specific deps inside the
	// services are nil-safe when A2A is disabled.
	chatEventListener        tui.ChatEventListener
	a2aTaskCoordinator       tui.A2ATaskCoordinator
	approvalCoordinator      tui.ApprovalCoordinator
	chatCompletionRunner     *chatcompletion.Runner
	directExecutionService   tui.DirectExecutionService
	toolExecutionCoordinator tui.ToolExecutionCoordinator
	uiNotifier               *uiNotifierHolder
	extensionBridge          *browserinfra.ExtensionBridge
	browserDriver            browserdomain.BrowserDriver
}

// uiNotifierHolder is a swap-once, read-many agentdomain.UINotifier. Producers capture
// the *uiNotifierHolder once at construction (never reassigning it) and call Notify
// from their own goroutines; SetUINotifier stores the real program-backed notifier
// exactly once at startup (before program.Run). atomic.Pointer keeps the read
// lock-free and the late swap race-free without a mutex. The stored pointer is
// never nil (newUINotifierHolder seeds a NoopUINotifier), so Notify is always safe.
type uiNotifierHolder struct {
	inner atomic.Pointer[agentdomain.UINotifier]
}

func newUINotifierHolder() *uiNotifierHolder {
	h := &uiNotifierHolder{}
	var noop agentdomain.UINotifier = agentdomain.NoopUINotifier{}
	h.inner.Store(&noop)
	return h
}

func (h *uiNotifierHolder) Notify(event any) {
	(*h.inner.Load()).Notify(event)
}

func (h *uiNotifierHolder) set(n agentdomain.UINotifier) {
	if n == nil {
		n = agentdomain.NoopUINotifier{}
	}
	h.inner.Store(&n)
}

// NewServiceContainer creates a new service container with all dependencies
func NewServiceContainer(cfg *config.Config) *ServiceContainer {
	sessionID := convdomain.GenerateSessionID()

	containerRuntime, err := containerruntime.NewContainerRuntime(
		sessionID,
		containerruntime.RuntimeType(cfg.ContainerRuntime.Type),
	)
	if err != nil {
		logger.Warn("failed to initialize container runtime, continuing without container support", "error", err)
	}

	container := &ServiceContainer{
		sessionID:        sessionID,
		config:           cfg,
		containerRuntime: containerRuntime,
		uiNotifier:       newUINotifierHolder(),
	}

	cfg.SetConfigDir(config.ResolveConfigDir())

	if err := config.EnsureProjectGitignore(); err != nil {
		logger.Warn("failed to ensure project .infer/.gitignore", "error", err)
	}

	if cfg.Gateway.Mock {
		container.startMockGateway()
	}

	container.initializeGatewayManager()
	container.initializeStateManager()
	container.initializeDomainServices()
	container.initializeAgentManager()
	container.initializeServices()
	container.initializeUIComponents()
	container.initializeExtensibility()

	return container
}

// SetUINotifier swaps in the real (program-backed) UI notifier. cmd/chat.go calls
// it once, after tea.NewProgram and before program.Run, so every background
// producer that captured the holder at construction begins pushing into the live
// Bubble Tea loop. Safe to call from any goroutine; before it runs, producers
// push to the no-op default.
func (c *ServiceContainer) SetUINotifier(n agentdomain.UINotifier) {
	c.uiNotifier.set(n)
}

// initializeBrowserTools constructs the browser-use driver (the opentask
// extension bridge, or a lazily-launched Playwright session) and registers the
// browser tools against it. The container owns the driver lifecycle; the
// extension backend also installs an event bridge on the state manager so chat
// events are mirrored to the extension.
// The WS server is NOT started here - every command builds a container and
// short-lived ones (status, tools, ...) must not grab the bridge port.
// Conversation-hosting commands call StartExtensionBridge.
func (c *ServiceContainer) initializeBrowserTools() {
	buCfg := &c.config.BrowserUse
	if !buCfg.Enabled {
		return
	}

	if buCfg.Backend == config.BrowserBackendExtension {
		eventBridge := c.stateManager.GetEventBridge()
		if eventBridge == nil {
			eventBridge = conversation.NewEventBridge()
			c.stateManager.SetEventBridge(eventBridge)
		}

		c.extensionBridge = browserinfra.NewExtensionBridge(buCfg, c.uiNotifier, c.conversationRepo, eventBridge, c.skillsService, string(c.sessionID), c.config.ArtifactsDir())
		c.browserDriver = c.extensionBridge
	} else {
		c.browserDriver = browserinfra.NewSession(buCfg)
	}

	c.toolRegistry.RegisterTools(browser.NewTools(c.config, c.browserDriver))
}

// StartExtensionBridge starts the WebSocket server the opentask extension
// dials into. Chat and headless call it eagerly - the extension must be able
// to connect before the first tool call. No-op when the extension backend is
// not selected. Errors are logged; tool calls surface them too.
func (c *ServiceContainer) StartExtensionBridge() {
	if c.extensionBridge == nil {
		return
	}
	if err := c.extensionBridge.Start(); err != nil {
		logger.Warn("extension bridge failed to start - browser tools will report the error", "error", err)
	}
}

// initializeGatewayManager creates the gateway manager (but does not start it)
// Commands that need the gateway should call gatewayManager.EnsureStarted() explicitly
func (c *ServiceContainer) initializeGatewayManager() {
	c.gatewayManager = gateway.NewManager(c.sessionID, c.config, c.containerRuntime)
}

// startMockGateway serves a scenario library (github.com/inference-gateway/tokenless)
// on an ephemeral localhost port, rewriting Gateway.URL to it and forcing
// Gateway.Run off. INFER_GATEWAY_MOCK_SCENARIOS overrides the embedded library
// with an app-owned scenarios YAML file, so non-Go apps that spawn infer can
// ship their own test scenarios.
func (c *ServiceContainer) startMockGateway() {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(fmt.Sprintf("mock gateway mode: failed to listen: %v", err))
	}

	defs := mockgateway.Default()
	if path := os.Getenv("INFER_GATEWAY_MOCK_SCENARIOS"); path != "" {
		defs, err = mockgateway.LoadFile(path)
		if err != nil {
			panic(fmt.Sprintf("mock gateway mode: INFER_GATEWAY_MOCK_SCENARIOS: %v", err))
		}
	}

	c.mockGateway = &http.Server{Handler: mockgateway.New(defs)}
	go func() { _ = c.mockGateway.Serve(ln) }()

	c.config.Gateway.URL = "http://" + ln.Addr().String()
	c.config.Gateway.Run = false
	logger.Info("mock gateway mode enabled", "url", c.config.Gateway.URL)
}

// initializeAgentManager creates and starts the agent manager if A2A is enabled
func (c *ServiceContainer) initializeAgentManager() {
	if !c.config.IsA2AToolsEnabled() {
		return
	}

	agentsPath := config.ResolveAgentsPath()
	agentsConfig, err := config.LoadAgents(agentsPath)
	if err != nil {
		logger.Warn("failed to load agents configuration", "error", err)
		return
	}

	agentCount := 0
	for _, agent := range agentsConfig.Agents {
		if agent.Run {
			agentCount++
		}
	}

	if len(c.config.A2A.Agents) > 0 {
		agentCount += len(c.config.A2A.Agents)
	}

	if agentCount > 0 {
		c.stateManager.InitializeAgentReadiness(agentCount)
	}

	c.agentManager = agentapp.NewAgentManager(c.sessionID, c.config, agentsConfig, c.containerRuntime, c.a2aAgentService)

	c.agentManager.SetStatusCallback(func(agentName string, state agentdomain.AgentState, message string, url string, image string) {
		c.stateManager.UpdateAgentStatus(agentName, state, message, url, image)
		c.uiNotifier.Notify(tui.AgentStatusUpdateEvent{
			AgentName: agentName,
			State:     state,
			Message:   message,
			URL:       url,
			Image:     image,
		})
	})

	c.agentManager.SetPullProgressCallback(func(name string, done, total int) {
		c.stateManager.UpdateAgentPullProgress(name, done, total)
		c.uiNotifier.Notify(tui.AgentStatusUpdateEvent{AgentName: name, State: agentdomain.AgentStatePullingImage})
	})

	ctx := context.Background()
	if err := c.agentManager.StartAgents(ctx); err != nil {
		logger.Warn("failed to start agents in background", "error", err)
	}
}

// initializeMCPManager creates and starts MCP manager if enabled
func (c *ServiceContainer) initializeMCPManager() {
	if !c.config.MCP.Enabled {
		return
	}

	c.mcpManager = mcp.NewManager(c.sessionID, &c.config.MCP, c.containerRuntime, c.uiNotifier)

	hasServersToStart := c.hasAutoStartMCPServers()
	if !hasServersToStart {
		return
	}

	logger.Info("starting MCP servers in background...")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	c.mcpStartupCancel = cancel
	go func() {
		defer cancel()

		if err := c.mcpManager.StartServers(ctx); err != nil {
			logger.Warn("some MCP servers failed to start", "error", err)
		}
	}()
}

// hasAutoStartMCPServers checks if any MCP servers are configured for auto-start
func (c *ServiceContainer) hasAutoStartMCPServers() bool {
	for _, server := range c.config.MCP.Servers {
		if server.Run && server.Enabled {
			return true
		}
	}
	return false
}

// initializeDomainServices creates and wires domain service implementations
func (c *ServiceContainer) initializeDomainServices() {
	c.fileService = services.NewFileService()
	c.imageService = services.NewImageService(c.config, c.createRawSDKClient())
	c.messageQueue = conversation.NewMessageQueueService()

	c.initializeMCPManager()

	c.ensureBackgroundTaskRegistry()
	c.memoryBackend = memory.NewMemoryBackend(c.config)

	storageConfig := storage.NewStorageFromConfig(c.config)
	stores, err := storage.NewStorage(storageConfig)
	if stores != nil && c.config.Scheduler.Backend == config.SchedulerBackendGitHub {
		stores.ScheduledJobs = githubscheduler.NewStore(stores.ScheduledJobs, &githubsetup.RealRunner{}, c.config.Scheduler.GitHub, c.config.Agent.Model)
	}
	c.stores = stores

	c.imageAnnotator = c.createImageAnnotator()
	c.toolRegistry = tools.NewRegistry(c.config, c.imageService, c.mcpManager, c.BackgroundShellService(), c.imageAnnotator, c.backgroundTaskRegistry, stores)
	c.toolRegistry.RegisterTools(computer.NewTools(c.config, c.toolRegistry, c.imageAnnotator))
	c.toolRegistry.SetMemoryBackend(c.memoryBackend)

	for name, srcCfg := range c.config.Vision.Sources {
		c.toolRegistry.RegisterFrameSource(name, services.NewDirectoryFrameSource(name, srcCfg, c.imageService))
	}

	styleProvider := styles.NewProvider(c.themeService)
	toolFormatterService := toolformatter.NewToolFormatterService(c.toolRegistry, styleProvider)
	toolFormatterService.SetMaxResultBytes(c.config.Tools.MaxResultBytes)

	groupStore := c.initializeStorageBackend(stores, storageConfig, toolFormatterService, err)

	if c.jobSupervisor != nil {
		c.jobSupervisor.SetConversationRepo(c.conversationRepo)
	}

	skillsSvc := skills.New(c.config)
	if err := skillsSvc.Load(context.Background()); err != nil {
		logger.Warn("failed to load skills", "error", err)
	}
	c.skillsService = skillsSvc

	c.initializeBrowserTools()

	modelClient := c.createRawSDKClient()
	c.modelService = conversation.NewHTTPModelService(modelClient)

	c.telemetryRecorder = telemetry.New(telemetry.Options{
		Enabled:           c.config.Telemetry.Enabled,
		Dir:               config.TelemetryDir(),
		SessionID:         string(c.sessionID),
		OTLPEndpoint:      c.config.Telemetry.OTLP.Endpoint,
		OTLPHeaders:       c.config.Telemetry.OTLP.Headers,
		OTLPInterval:      time.Duration(c.config.Telemetry.OTLP.Interval) * time.Second,
		ReceiverAddress:   c.config.Telemetry.ReceiverAddress,
		Cost:              c.GetPricingService().CalculateCost,
		AttrSessionIDKey:  c.config.Telemetry.AttrSessionIDKey,
		AttrToolCallIDKey: c.config.Telemetry.AttrToolCallIDKey,
	})

	if c.config.Tools.Enabled || c.config.IsA2AToolsEnabled() {
		llmToolService := agent.NewLLMToolServiceWithRegistry(c.config, c.toolRegistry)
		llmToolService.SetCurrentModelFn(c.modelService.GetCurrentModel)
		c.toolService = llmToolService
	} else {
		c.toolService = agent.NewNoOpToolService()
	}
	if c.telemetryRecorder != nil {
		c.toolService = telemetry.NewToolService(c.toolService, c.telemetryRecorder)
	}

	if c.tokenizer == nil {
		c.tokenizer = conversation.NewTokenizerService(conversation.DefaultTokenizerConfig())
	}

	summaryClient := c.createRawSDKClient()
	c.conversationOptimizer = conversation.NewConversationOptimizer(conversation.OptimizerConfig{
		Enabled:           c.config.Compact.Enabled,
		AutoAt:            c.config.Compact.AutoAt,
		BufferSize:        2,
		KeepFirstMessages: c.config.Compact.KeepFirstMessages,
		Client:            summaryClient,
		Config:            c.config,
		Tokenizer:         c.tokenizer,
		Repo:              c.conversationRepo,
	})

	if c.config.Compact.Enabled {
		if persistentRepo, ok := c.conversationRepo.(*conversation.PersistentConversationRepository); ok {
			c.sessionRolloverManager = conversation.NewSessionRolloverManager(
				c.config,
				c.conversationOptimizer,
				persistentRepo,
				c.tokenizer,
				groupStore,
			)
		}
	}

	c.a2aAgentService = agentapp.NewA2AAgentService(c.config)

	c.githubIssueService = githubissues.New()

	agentClient := adapters.NewAnthropicMessages(c.createRawSDKClient())
	agentImpl := agent.NewAgent(
		agentClient,
		c.toolService,
		c.config,
		c.conversationRepo,
		c.a2aAgentService,
		c.skillsService,
		c.messageQueue,
		c.stateManager,
		c.config.Gateway.Timeout,
		c.conversationOptimizer,
		c.backgroundTaskRegistry,
		c.sessionRolloverManager,
	)
	agentImpl.SetMemoryBackend(c.memoryBackend)
	agentImpl.SetTelemetryRecorder(c.telemetryRecorder)
	c.agent = agentImpl
}

// initializeStorageBackend wires the conversation repository for the configured
// storage backend and returns the matching SessionGroupStorage. When the
// backend fails to initialize, falls back to in-memory conversation storage
// (or panics on an explicit, non-default backend so the user gets a clear
// signal that the configuration is broken).
func (c *ServiceContainer) initializeStorageBackend(
	stores *storage.Stores,
	storageConfig storage.StorageConfig,
	toolFormatterService *toolformatter.ToolFormatterService,
	err error,
) storage.SessionGroupStorage {
	if err != nil {
		c.handleStorageInitFailure(storageConfig, toolFormatterService, err)
		return storage.NewMemorySessionGroupStorage()
	}

	c.storage = stores.Conversations
	persistentRepo := conversation.NewPersistentConversationRepository(toolFormatterService, c.PricingService(), stores.Conversations)
	c.conversationRepo = persistentRepo
	logger.Info("initialized conversation storage", "type", storageConfig.Type)

	titleClient := c.createRawSDKClient()
	c.titleGenerator = conversation.NewConversationTitleGenerator(titleClient, stores.Conversations, c.config)
	c.backgroundJobManager = scheduler.NewBackgroundJobManager(c.titleGenerator, c.config)

	persistentRepo.SetTitleGenerator(c.titleGenerator)
	persistentRepo.SetA2ATaskTracker(c.backgroundTaskRegistry)

	if gs, ok := stores.Conversations.(storage.SessionGroupStorage); ok {
		return gs
	}

	logger.Warn("storage backend does not implement SessionGroupStorage, falling back to in-memory group store",
		"type", storageConfig.Type)
	return storage.NewMemorySessionGroupStorage()
}

// handleStorageInitFailure panics on explicit-backend failure (so the user
// sees a clear configuration error) and falls back to in-memory conversation
// storage on the implicit default path.
func (c *ServiceContainer) handleStorageInitFailure(
	storageConfig storage.StorageConfig,
	toolFormatterService *toolformatter.ToolFormatterService,
	err error,
) {
	if c.config.Storage.Enabled && storageConfig.Type != config.StorageTypeMemory {
		logger.Error("storage backend initialization failed",
			"error", err,
			"type", storageConfig.Type,
			"enabled", c.config.Storage.Enabled)
		logger.Error("storage backend is not available; "+
			"either fix the configuration or disable storage by setting 'storage.enabled: false'",
			"type", storageConfig.Type)
		panic(fmt.Sprintf("Failed to initialize storage backend '%s': %v\n\n"+
			"To use in-memory storage instead, set:\n"+
			"  storage.enabled: false\n\n"+
			"Or use an alternative storage backend:\n"+
			"  storage.type: postgres  # or redis", storageConfig.Type, err))
	}

	logger.Warn("using in-memory conversation storage (conversations will not be persisted)")
	c.conversationRepo = conversation.NewInMemoryConversationRepository(toolFormatterService, c.PricingService())
}

// initializeStateManager creates the state manager before domain services need it
func (c *ServiceContainer) initializeStateManager() {
	debugMode := c.config.Logging.Debug
	stateManager := statemanager.NewStateManager(debugMode)
	stateManager.SetStallThreshold(time.Duration(c.config.Client.StallThresholdSec) * time.Second)
	c.stateManager = stateManager
}

// initializeServices creates the new improved services
func (c *ServiceContainer) initializeServices() {
	if c.config.IsA2AToolsEnabled() {
		maxTaskRetention := c.config.A2A.Task.CompletedTaskRetention
		c.taskRetentionService = scheduler.NewTaskRetentionService(maxTaskRetention)

		if c.jobSupervisor != nil {
			c.jobSupervisor.SetTaskRetention(c.taskRetentionService)
		}

		c.backgroundTaskService = scheduler.NewBackgroundTaskService(c.backgroundTaskRegistry, c.jobSupervisor)
	}

	c.initializeChatOrchestrationServices()

	c.initializeGitHubSetupService()
}

// initializeChatOrchestrationServices wires the services extracted from the
// monolithic ChatHandler (issue #529). All deps from earlier init phases must
// be in place by the time this runs.
func (c *ServiceContainer) initializeChatOrchestrationServices() {
	c.chatEventListener = eventlistener.NewService()

	c.a2aTaskCoordinator = a2acoord.NewService(a2acoord.Options{
		ConversationRepo:     c.conversationRepo,
		StateManager:         c.stateManager,
		TaskRetentionService: c.taskRetentionService,
		Listener:             c.chatEventListener,
	})

	c.approvalCoordinator = approvalcoord.NewService(approvalcoord.Options{
		AgentService:     c.agent,
		ConversationRepo: c.conversationRepo,
		StateManager:     c.stateManager,
	})

	c.chatCompletionRunner = chatcompletion.NewRunner(chatcompletion.Options{
		AgentService:     c.agent,
		ConversationRepo: c.conversationRepo,
		ModelService:     c.modelService,
		StateManager:     c.stateManager,
		Listener:         c.chatEventListener,
	})

	c.directExecutionService = directexec.NewService(directexec.Options{
		ConversationRepo:       c.conversationRepo,
		ToolService:            c.toolService,
		StateManager:           c.stateManager,
		BackgroundShellService: c.BackgroundShellService(),
		Listener:               c.chatEventListener,
	})

	c.toolExecutionCoordinator = toolcoordinator.NewCoordinator(toolcoordinator.Options{
		ConversationRepo: c.conversationRepo,
		StateManager:     c.stateManager,
		DirectExec:       c.directExecutionService,
		Listener:         c.chatEventListener,
	})
}

// initializeUIComponents creates UI components and theme
func (c *ServiceContainer) initializeUIComponents() {
	themeProvider := styles.NewThemeProvider()

	if configuredTheme := c.config.GetTheme(); configuredTheme != "" {
		if err := themeProvider.SetTheme(configuredTheme); err != nil {
			logger.Warn("failed to set configured theme, using default", "theme", configuredTheme, "error", err)
		}
	}

	c.themeService = themeProvider
}

// initializeExtensibility sets up extensible systems
func (c *ServiceContainer) initializeExtensibility() {
	c.shortcutRegistry = shortcuts.NewRegistry()
	c.registerDefaultCommands()
}

// registerDefaultCommands registers the built-in commands
func (c *ServiceContainer) registerDefaultCommands() {
	c.shortcutRegistry.Register(shortcuts.NewClearShortcut(c.conversationRepo, c.backgroundTaskRegistry))
	c.shortcutRegistry.Register(shortcuts.NewCompactShortcut(c.conversationRepo))
	c.shortcutRegistry.Register(shortcuts.NewCopyShortcut(c.conversationRepo, clipboardtext.NewWriter()))
	c.shortcutRegistry.Register(shortcuts.NewContextShortcut(c.conversationRepo, c.modelService, c.tokenizer))
	c.shortcutRegistry.Register(shortcuts.NewCostShortcut(c.conversationRepo))
	c.shortcutRegistry.Register(shortcuts.NewExitShortcut())
	c.shortcutRegistry.Register(shortcuts.NewEffortShortcut(c.agent, c.modelService))
	c.shortcutRegistry.Register(shortcuts.NewSwitchShortcut(c.modelService))
	c.shortcutRegistry.Register(shortcuts.NewThemeShortcut(c.themeService))
	c.shortcutRegistry.Register(shortcuts.NewToolsShortcut())
	c.shortcutRegistry.Register(shortcuts.NewHelpShortcut(c.shortcutRegistry))
	c.shortcutRegistry.Register(shortcuts.NewDiffShortcut())
	c.shortcutRegistry.Register(shortcuts.NewExplorerShortcut())
	c.shortcutRegistry.Register(shortcuts.NewReleaseNotesShortcut())
	c.shortcutRegistry.Register(shortcuts.NewStatsShortcut())
	c.shortcutRegistry.Register(shortcuts.NewTracesShortcut())

	if persistentRepo, ok := c.conversationRepo.(*conversation.PersistentConversationRepository); ok {
		c.shortcutRegistry.Register(shortcuts.NewConversationSelectShortcut(persistentRepo))
		c.shortcutRegistry.Register(shortcuts.NewNewShortcut(persistentRepo, c.backgroundTaskRegistry))
	}

	c.shortcutRegistry.Register(shortcuts.NewInitGithubActionShortcut())
	c.shortcutRegistry.Register(shortcuts.NewInitShortcut(c.config))

	if c.config.IsA2AToolsEnabled() {
		c.shortcutRegistry.Register(shortcuts.NewA2ATaskManagementShortcut(c.config))
		c.shortcutRegistry.Register(shortcuts.NewA2AAgentsShortcut())
	}

	if c.config.IsSpeechToTextEnabled() {
		c.shortcutRegistry.Register(shortcuts.NewVoiceShortcut(
			c.config.SpeechToText,
			audio.NewRecorder(c.config.SpeechToText),
			audio.NewWhisperTranscriber(c.config.SpeechToText),
		))
	}

	configDir := c.config.GetConfigDir()
	customShortcutClient := c.createRawSDKClient()
	if err := c.shortcutRegistry.LoadCustomShortcuts(configDir, customShortcutClient, c.modelService, c.imageService, c.toolService); err != nil {
		logger.Error("failed to load custom shortcuts", "error", err, "config_dir", configDir)
	}
}

func (c *ServiceContainer) GetConversationRepository() convdomain.ConversationRepository {
	return c.conversationRepo
}

func (c *ServiceContainer) GetConversationOptimizer() convdomain.ConversationOptimizer {
	return c.conversationOptimizer
}

func (c *ServiceContainer) GetSessionRolloverManager() *conversation.SessionRolloverManager {
	return c.sessionRolloverManager
}

func (c *ServiceContainer) GetModelService() convdomain.ModelService {
	return c.modelService
}

func (c *ServiceContainer) GetToolService() agentdomain.ToolService {
	return c.toolService
}

// GetTelemetryRecorder returns the telemetry recorder, or nil when telemetry is
// disabled. The returned *Recorder is nil-safe, so the chat/headless
// session-lifecycle taps can call its methods directly without a nil check.
func (c *ServiceContainer) GetTelemetryRecorder() *telemetry.Recorder {
	return c.telemetryRecorder
}

func (c *ServiceContainer) GetToolRegistry() *tools.Registry {
	return c.toolRegistry
}

// GetMemoryBackend returns the shared memory sync backend (local no-op or git),
// used by the headless AgentSession to sync memory at run start/finish.
func (c *ServiceContainer) GetMemoryBackend() memory.MemoryBackend {
	return c.memoryBackend
}

func (c *ServiceContainer) GetFileService() agentdomain.FileService {
	return c.fileService
}

func (c *ServiceContainer) GetImageService() agentdomain.ImageService {
	return c.imageService
}

// createImageAnnotator builds the configured annotation engine: the local
// llama.cpp subprocess by default, or a gateway side-call (title-generator
// style).
func (c *ServiceContainer) createImageAnnotator() agentdomain.ImageAnnotator {
	if !c.config.Vision.AnnotatorReady() {
		return nil
	}
	return vlm.NewGatewayAnnotator(c.createRawSDKClient(), c.config)
}

func (c *ServiceContainer) GetSkillsService() agentdomain.SkillsService {
	return c.skillsService
}

func (c *ServiceContainer) GetGitHubIssueService() agentdomain.GitHubIssueService {
	return c.githubIssueService
}

func (c *ServiceContainer) initializeGitHubSetupService() {
	if c.gitHubSetupService != nil {
		return
	}
	c.gitHubSetupService = githubsetup.NewService(&githubsetup.RealRunner{})
}

func (c *ServiceContainer) GetGitHubSetupService() agentdomain.GitHubSetupService {
	c.initializeGitHubSetupService()
	return c.gitHubSetupService
}

func (c *ServiceContainer) GetPricingService() convdomain.PricingService {
	return c.PricingService()
}

func (c *ServiceContainer) PricingService() convdomain.PricingService {
	if c.pricingService == nil {
		c.pricingService = conversation.NewPricingService(&c.config.Pricing)
	}
	return c.pricingService
}

func (c *ServiceContainer) GetThemeService() tui.ThemeService {
	return c.themeService
}

func (c *ServiceContainer) GetShortcutRegistry() *shortcuts.Registry {
	return c.shortcutRegistry
}

func (c *ServiceContainer) GetStateManager() *statemanager.StateManager {
	return c.stateManager
}

func (c *ServiceContainer) GetAgentManager() agentdomain.AgentManager {
	return c.agentManager
}

func (c *ServiceContainer) GetAgentService() agentdomain.AgentService {
	return c.agent
}

func (c *ServiceContainer) GetMessageQueue() convdomain.MessageQueue {
	return c.messageQueue
}

// GetBackgroundTaskRegistry returns the unified background task registry
// (the single tracker that owns both A2A tasks and background bash shells).
// Callers that need only the narrower A2A or shell view can use the
// returned value as a agentdomain.A2ATaskTracker or scheddomain.ShellTracker.
func (c *ServiceContainer) GetBackgroundTaskRegistry() scheddomain.BackgroundTaskRegistry {
	return c.backgroundTaskRegistry
}

// GetTaskRetentionService returns the task retention service (may be nil if A2A is not enabled)
func (c *ServiceContainer) GetTaskRetentionService() scheddomain.TaskRetentionService {
	return c.taskRetentionService
}

// GetBackgroundTaskService returns the background task service (may be nil if A2A is not enabled)
func (c *ServiceContainer) GetBackgroundTaskService() scheddomain.BackgroundTaskService {
	return c.backgroundTaskService
}

// GetMCPManager returns the MCP manager (may be nil if MCP is not enabled)
func (c *ServiceContainer) GetMCPManager() agentdomain.MCPManager {
	return c.mcpManager
}

// GetA2ATaskCoordinator returns the A2A task lifecycle event coordinator.
func (c *ServiceContainer) GetA2ATaskCoordinator() tui.A2ATaskCoordinator {
	return c.a2aTaskCoordinator
}

// GetApprovalCoordinator returns the plan-approval / computer-use pause-resume
// coordinator.
func (c *ServiceContainer) GetApprovalCoordinator() tui.ApprovalCoordinator {
	return c.approvalCoordinator
}

// GetChatCompletionRunner returns the LLM streaming lifecycle runner.
func (c *ServiceContainer) GetChatCompletionRunner() tui.ChatCompletionRunner {
	return c.chatCompletionRunner
}

// GetDirectExecutionService returns the user-typed !command / !!Tool(...)
// execution service. Also satisfies BashDetachChannelHolder.
func (c *ServiceContainer) GetDirectExecutionService() tui.DirectExecutionService {
	return c.directExecutionService
}

// GetToolExecutionCoordinator returns the tool round-trip coordinator (tool
// approval, streaming-status, execution progress, active-tool tracking).
func (c *ServiceContainer) GetToolExecutionCoordinator() tui.ToolExecutionCoordinator {
	return c.toolExecutionCoordinator
}

// createRetryConfig creates a retry config with logging callback
func (c *ServiceContainer) createRetryConfig() *sdk.RetryConfig {
	retryConfig := &sdk.RetryConfig{
		Enabled:              c.config.Client.Retry.Enabled,
		MaxAttempts:          c.config.Client.Retry.MaxAttempts,
		InitialBackoffSec:    c.config.Client.Retry.InitialBackoffSec,
		MaxBackoffSec:        c.config.Client.Retry.MaxBackoffSec,
		BackoffMultiplier:    c.config.Client.Retry.BackoffMultiplier,
		RetryableStatusCodes: c.config.Client.Retry.RetryableStatusCodes,
	}

	if retryConfig.Enabled {
		originalOnRetry := retryConfig.OnRetry
		retryConfig.OnRetry = func(attempt int, err error, delay time.Duration) {
			logger.Error("retrying HTTP request",
				"attempt", attempt,
				"error", err.Error(),
				"delay", delay.String())
			if notify := RetryNotifier; notify != nil {
				notify(fmt.Sprintf("⏳ %s - retrying in %s (attempt %d)", err.Error(), delay, attempt))
			}
			if originalOnRetry != nil {
				originalOnRetry(attempt, err, delay)
			}
		}
	}

	return retryConfig
}

// createSDKClient creates a configured SDK client with retry and timeout settings
// createRawSDKClient creates the raw SDK client for services that need it
func (c *ServiceContainer) createRawSDKClient() sdk.Client {
	if c.config == nil {
		panic("ServiceContainer: config is nil when creating SDK client")
	}

	baseURL := c.config.Gateway.URL
	if c.gatewayManager != nil && c.config.Gateway.Run {
		actualURL := c.gatewayManager.GetGatewayURL()
		if actualURL != "" {
			baseURL = actualURL
		}
	}
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}

	if !strings.HasSuffix(baseURL, "/v1") {
		baseURL = strings.TrimSuffix(baseURL, "/") + "/v1"
	}

	timeout := c.config.Client.Timeout
	if timeout == 0 {
		timeout = 200
	}

	return sdk.NewClient(&sdk.ClientOptions{
		BaseURL:     baseURL,
		APIKey:      c.config.Gateway.APIKey,
		Timeout:     time.Duration(timeout) * time.Second,
		RetryConfig: c.createRetryConfig(),
		Transport:   telemetry.PropagationTransport(nil),
	})
}

// GetBackgroundJobManager returns the background job manager
func (c *ServiceContainer) GetBackgroundJobManager() *scheduler.BackgroundJobManager {
	return c.backgroundJobManager
}

// GetStorage returns the conversation storage
func (c *ServiceContainer) GetStorage() storage.ConversationStorage {
	return c.storage
}

// GetShellHistoryStorage returns the shell-history store, or nil when storage
// failed to initialize (callers fall back to the file-based history).
func (c *ServiceContainer) GetShellHistoryStorage() storage.ShellHistoryStorage {
	if c.stores == nil {
		return nil
	}
	return c.stores.ShellHistory
}

// GetGatewayManager returns the gateway manager
func (c *ServiceContainer) GetGatewayManager() *gateway.Manager {
	return c.gatewayManager
}

// BackgroundShellService returns the background shell service
func (c *ServiceContainer) BackgroundShellService() *scheduler.BackgroundShellService {
	if c.backgroundShellService == nil {
		c.ensureBackgroundTaskRegistry()
		c.backgroundShellService = scheduler.NewBackgroundShellService(
			c.backgroundTaskRegistry,
			c.jobSupervisor,
			c.config,
			nil,
		)
	}
	return c.backgroundShellService
}

// ensureBackgroundTaskRegistry lazily constructs the unified registry. Called
// from BackgroundShellService() and from initializeDomainServices() so the
// shell view and the A2A view are guaranteed to be projections of the same
// underlying instance regardless of construction order.
func (c *ServiceContainer) ensureBackgroundTaskRegistry() {
	if c.backgroundTaskRegistry != nil {
		return
	}
	c.jobSupervisor = jobs.NewSupervisor(c.messageQueue, c.conversationRepo, c.uiNotifier)
	c.jobSupervisor.SetRetentionCount(scheddomain.JobKindShell, c.config.Tools.Bash.BackgroundShells.CompletedRetention)
	c.jobSupervisor.SetRetentionCount(scheddomain.JobKindSubagent, c.config.Tools.Agent.CompletedRetention)
	c.jobSupervisor.SetRetentionCount(scheddomain.JobKindA2A, c.config.A2A.Task.CompletedTaskRetention)
	retention := time.Duration(c.config.Tools.Bash.BackgroundShells.RetentionMinutes) * time.Minute
	c.jobSupervisor.Start(10*time.Minute, retention)
	maxConcurrent := c.config.Tools.Bash.BackgroundShells.MaxConcurrent
	c.backgroundTaskRegistry = scheduler.NewBackgroundTaskRegistry(maxConcurrent, c.jobSupervisor)
}

// Shutdown gracefully shuts down the service container and its resources
func (c *ServiceContainer) Shutdown(ctx context.Context) error {
	c.telemetryRecorder.Shutdown(ctx)

	if c.browserDriver != nil {
		c.browserDriver.Close()
	}

	if c.skillsService != nil {
		if err := c.skillsService.CleanupDynamic(ctx); err != nil {
			logger.Warn("failed to clean up dynamic skills", "error", err)
		}
	}

	if c.backgroundShellService != nil {
		logger.Info("stopping background shell service...")
		c.backgroundShellService.Stop()
	}

	if c.jobSupervisor != nil {
		logger.Info("stopping job supervisor...")
		c.jobSupervisor.Stop()
	}

	if c.agentManager != nil && c.agentManager.IsRunning() {
		logger.Info("shutting down agent containers...")
		if err := c.agentManager.StopAgents(ctx); err != nil {
			logger.Error("failed to stop agent containers", "error", err)
		}
	}

	if c.gatewayManager != nil && c.gatewayManager.IsRunning() {
		if err := c.gatewayManager.Stop(ctx); err != nil {
			logger.Error("failed to stop gateway container", "error", err)
			return err
		}
	}

	if c.mockGateway != nil {
		_ = c.mockGateway.Close()
	}

	if c.mcpStartupCancel != nil {
		c.mcpStartupCancel()
	}

	if c.mcpManager != nil {
		if err := c.mcpManager.Close(); err != nil {
			logger.Error("failed to close MCP manager", "error", err)
		}
	}

	return nil
}
