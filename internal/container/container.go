package container

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	filewriterdomain "github.com/inference-gateway/cli/internal/domain/filewriter"
	adapters "github.com/inference-gateway/cli/internal/infra/adapters"
	storage "github.com/inference-gateway/cli/internal/infra/storage"
	logger "github.com/inference-gateway/cli/internal/logger"
	services "github.com/inference-gateway/cli/internal/services"
	filewriterservice "github.com/inference-gateway/cli/internal/services/filewriter"
	tools "github.com/inference-gateway/cli/internal/services/tools"
	shortcuts "github.com/inference-gateway/cli/internal/shortcuts"
	utils "github.com/inference-gateway/cli/internal/utils"
	sdk "github.com/inference-gateway/sdk"
	viper "github.com/spf13/viper"
)

// ServiceContainer manages all application dependencies
type ServiceContainer struct {
	// Session
	sessionID domain.SessionID

	// Container runtime
	containerRuntime domain.ContainerRuntime

	// Configuration
	viper         *viper.Viper
	config        *config.Config
	configService *services.ConfigService

	// Domain services
	conversationRepo      domain.ConversationRepository
	conversationOptimizer domain.ConversationOptimizerService
	modelService          domain.ModelService
	chatService           domain.ChatService
	agentService          domain.AgentService
	toolService           domain.ToolService
	fileService           domain.FileService
	imageService          domain.ImageService
	pricingService        domain.PricingService
	a2aAgentService       domain.A2AAgentService
	messageQueue          domain.MessageQueue
	taskTrackerService    domain.TaskTracker
	taskRetentionService  domain.TaskRetentionService
	backgroundTaskService domain.BackgroundTaskService
	gatewayManager        domain.GatewayManager
	agentManager          domain.AgentManager

	// Services
	stateManager domain.StateManager

	// Background services
	titleGenerator         *services.ConversationTitleGenerator
	backgroundJobManager   *services.BackgroundJobManager
	backgroundShellService *services.BackgroundShellService
	shellTracker           domain.ShellTracker
	storage                storage.ConversationStorage
	agentsConfigService    *services.AgentsConfigService

	// UI components
	themeService domain.ThemeService

	// Extensibility
	shortcutRegistry *shortcuts.Registry

	// Tool registry
	toolRegistry *tools.Registry
	mcpManager   domain.MCPManager

	// File writing services
	pathValidator  filewriterdomain.PathValidator
	backupManager  filewriterdomain.BackupManager
	fileWriter     filewriterdomain.FileWriter
	chunkManager   filewriterdomain.ChunkManager
	paramExtractor *tools.ParameterExtractor
}

// NewServiceContainer creates a new service container with all dependencies
func NewServiceContainer(cfg *config.Config, v ...*viper.Viper) *ServiceContainer {
	sessionID := domain.GenerateSessionID()

	containerRuntime, err := services.NewContainerRuntime(
		sessionID,
		services.RuntimeType(cfg.ContainerRuntime.Type),
	)
	if err != nil {
		logger.Warn("Failed to initialize container runtime, continuing without container support", "error", err)
	}

	container := &ServiceContainer{
		sessionID:        sessionID,
		config:           cfg,
		containerRuntime: containerRuntime,
	}

	if len(v) > 0 && v[0] != nil {
		container.viper = v[0]
		container.configService = services.NewConfigService(v[0], cfg)
	}

	cfg.SetConfigDir(container.determineConfigDirectory())

	container.initializeGatewayManager()
	container.initializeFileWriterServices()
	container.initializeStateManager()
	container.initializeDomainServices()
	container.initializeAgentManager()
	container.initializeServices()
	container.initializeUIComponents()
	container.initializeExtensibility()

	return container
}

// initializeGatewayManager creates the gateway manager (but does not start it)
// Commands that need the gateway should call gatewayManager.EnsureStarted() explicitly
func (c *ServiceContainer) initializeGatewayManager() {
	c.gatewayManager = services.NewGatewayManager(c.sessionID, c.config, c.containerRuntime)
}

// initializeAgentManager creates the agent manager if A2A is enabled
// Note: This does NOT start agents. Caller must explicitly call agentManager.StartAgents(ctx).
func (c *ServiceContainer) initializeAgentManager() {
	agentsPath := filepath.Join(config.ConfigDirName, config.AgentsFileName)
	c.agentsConfigService = services.NewAgentsConfigService(agentsPath)

	if !c.config.IsA2AToolsEnabled() {
		return
	}

	agentsConfig, err := c.agentsConfigService.Load()
	if err != nil {
		logger.Warn("Failed to load agents configuration", "error", err)
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

	c.agentManager = services.NewAgentManager(c.sessionID, c.config, agentsConfig, c.containerRuntime, c.a2aAgentService)

	c.agentManager.SetStatusCallback(func(agentName string, state domain.AgentState, message string, url string, image string) {
		c.stateManager.UpdateAgentStatus(agentName, state, message, url, image)
	})
}

// initializeFileWriterServices creates the new file writer architecture services
func (c *ServiceContainer) initializeFileWriterServices() {
	c.pathValidator = filewriterservice.NewPathValidator(c.config)
	c.backupManager = filewriterservice.NewBackupManager(".")
	c.fileWriter = filewriterservice.NewSafeFileWriter(c.pathValidator, c.backupManager)
	c.chunkManager = filewriterservice.NewStreamingChunkManager("./.infer/tmp", c.fileWriter)
	c.paramExtractor = tools.NewParameterExtractor()
}

// initializeMCPManager creates the MCP manager if enabled
// Note: This does NOT start MCP servers. Caller must explicitly call mcpManager.StartServers(ctx).
func (c *ServiceContainer) initializeMCPManager() {
	if !c.config.MCP.Enabled {
		return
	}

	c.mcpManager = services.NewMCPManager(c.sessionID, &c.config.MCP, c.containerRuntime)
}

// initializeDomainServices creates and wires domain service implementations
func (c *ServiceContainer) initializeDomainServices() {
	c.fileService = services.NewFileService()
	c.imageService = services.NewImageService(c.config)
	c.messageQueue = services.NewMessageQueueService()

	c.initializeMCPManager()

	c.toolRegistry = tools.NewRegistry(c.config, c.imageService, c.mcpManager, c.BackgroundShellService())
	c.taskTrackerService = c.toolRegistry.GetTaskTracker()

	toolFormatterService := services.NewToolFormatterService(c.toolRegistry)

	storageConfig := storage.NewStorageFromConfig(c.config)
	storageBackend, err := storage.NewStorage(storageConfig)
	if err != nil {
		isExplicitStorage := c.config.Storage.Enabled && storageConfig.Type != "memory"

		if isExplicitStorage {
			logger.Error("Storage backend initialization failed",
				"error", err,
				"type", storageConfig.Type,
				"enabled", c.config.Storage.Enabled)
			logger.Error("Storage backend '%s' is not available. "+
				"Either fix the configuration or disable storage by setting 'storage.enabled: false'",
				storageConfig.Type)
			panic(fmt.Sprintf("Failed to initialize storage backend '%s': %v\n\n"+
				"To use in-memory storage instead, set:\n"+
				"  storage.enabled: false\n\n"+
				"Or use an alternative storage backend:\n"+
				"  storage.type: postgres  # or redis", storageConfig.Type, err))
		}

		logger.Warn("Using in-memory conversation storage (conversations will not be persisted)")
		c.conversationRepo = services.NewInMemoryConversationRepository(toolFormatterService, c.PricingService())
	} else {
		c.storage = storageBackend
		persistentRepo := services.NewPersistentConversationRepository(toolFormatterService, c.PricingService(), storageBackend)
		c.conversationRepo = persistentRepo
		logger.Info("Initialized conversation storage", "type", storageConfig.Type)

		titleClient := c.createSDKClient()
		c.titleGenerator = services.NewConversationTitleGenerator(titleClient, storageBackend, c.config)
		c.backgroundJobManager = services.NewBackgroundJobManager(c.titleGenerator, c.config)

		persistentRepo.SetTitleGenerator(c.titleGenerator)
		persistentRepo.SetTaskTracker(c.taskTrackerService)
	}

	modelClient := c.createSDKClient()
	c.modelService = services.NewHTTPModelService(modelClient)

	if c.config.Tools.Enabled || c.config.IsA2AToolsEnabled() {
		c.toolService = services.NewLLMToolServiceWithRegistry(c.config, c.toolRegistry)
	} else {
		c.toolService = services.NewNoOpToolService()
	}

	if c.config.Compact.Enabled {
		summaryClient := c.createSDKClient()
		tokenizer := services.NewTokenizerService(services.DefaultTokenizerConfig())
		c.conversationOptimizer = services.NewConversationOptimizer(services.OptimizerConfig{
			Enabled:           c.config.Compact.Enabled,
			AutoAt:            c.config.Compact.AutoAt,
			BufferSize:        2,
			KeepFirstMessages: c.config.Compact.KeepFirstMessages,
			Client:            summaryClient,
			Config:            c.config,
			Tokenizer:         tokenizer,
		})
	}

	c.a2aAgentService = services.NewA2AAgentService(c.config)

	agentClient := c.createSDKClient()
	c.agentService = services.NewAgentService(
		agentClient,
		c.toolService,
		c.config,
		c.conversationRepo,
		c.a2aAgentService,
		c.messageQueue,
		c.stateManager,
		c.config.Gateway.Timeout,
		c.conversationOptimizer,
	)

	c.chatService = services.NewStreamingChatService(c.agentService)
}

// initializeStateManager creates the state manager before domain services need it
func (c *ServiceContainer) initializeStateManager() {
	debugMode := c.config.Logging.Debug
	c.stateManager = services.NewStateManager(debugMode)
}

// initializeServices creates the new improved services
func (c *ServiceContainer) initializeServices() {
	if c.config.IsA2AToolsEnabled() {
		maxTaskRetention := c.config.A2A.Task.CompletedTaskRetention
		c.taskRetentionService = services.NewTaskRetentionService(maxTaskRetention)

		c.backgroundTaskService = services.NewBackgroundTaskService(c.taskTrackerService)
	}
}

// initializeUIComponents creates UI components and theme
func (c *ServiceContainer) initializeUIComponents() {
	themeProvider := domain.NewThemeProvider()

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
	c.shortcutRegistry.Register(shortcuts.NewClearShortcut(c.conversationRepo, c.taskTrackerService))
	c.shortcutRegistry.Register(shortcuts.NewCompactShortcut(c.conversationRepo))
	c.shortcutRegistry.Register(shortcuts.NewContextShortcut(c.conversationRepo, c.modelService))
	c.shortcutRegistry.Register(shortcuts.NewCostShortcut(c.conversationRepo))
	c.shortcutRegistry.Register(shortcuts.NewExitShortcut())
	c.shortcutRegistry.Register(shortcuts.NewSwitchShortcut(c.modelService))
	c.shortcutRegistry.Register(shortcuts.NewModelShortcut(c.modelService))
	c.shortcutRegistry.Register(shortcuts.NewThemeShortcut(c.themeService))
	c.shortcutRegistry.Register(shortcuts.NewHelpShortcut(c.shortcutRegistry))

	if persistentRepo, ok := c.conversationRepo.(*services.PersistentConversationRepository); ok {
		adapter := adapters.NewPersistentConversationAdapter(persistentRepo)
		c.shortcutRegistry.Register(shortcuts.NewConversationSelectShortcut(adapter))
		c.shortcutRegistry.Register(shortcuts.NewNewShortcut(adapter, c.taskTrackerService))
	}

	c.shortcutRegistry.Register(shortcuts.NewInitGithubActionShortcut())
	c.shortcutRegistry.Register(shortcuts.NewInitShortcut(c.config))

	if c.config.IsA2AToolsEnabled() {
		c.shortcutRegistry.Register(shortcuts.NewA2ATaskManagementShortcut(c.config))
	}

	configDir := c.determineConfigDirectory()
	customShortcutClient := c.createSDKClient()
	if err := c.shortcutRegistry.LoadCustomShortcuts(configDir, customShortcutClient, c.modelService, c.imageService, c.toolService); err != nil {
		logger.Error("Failed to load custom shortcuts", "error", err, "config_dir", configDir)
	}
}

// determineConfigDirectory returns the directory where configuration and related files should be stored
func (c *ServiceContainer) determineConfigDirectory() string {
	configDir := ".infer"
	if c.viper != nil {
		if configFile := c.viper.ConfigFileUsed(); configFile != "" {
			configDir = filepath.Dir(configFile)
		}
	}
	return configDir
}

func (c *ServiceContainer) GetConfig() *config.Config {
	return c.config
}

func (c *ServiceContainer) GetConversationRepository() domain.ConversationRepository {
	return c.conversationRepo
}

func (c *ServiceContainer) GetConversationOptimizer() domain.ConversationOptimizerService {
	return c.conversationOptimizer
}

func (c *ServiceContainer) GetModelService() domain.ModelService {
	return c.modelService
}

func (c *ServiceContainer) GetChatService() domain.ChatService {
	return c.chatService
}

func (c *ServiceContainer) GetToolService() domain.ToolService {
	return c.toolService
}

func (c *ServiceContainer) GetToolRegistry() *tools.Registry {
	return c.toolRegistry
}

func (c *ServiceContainer) GetFileService() domain.FileService {
	return c.fileService
}

func (c *ServiceContainer) GetImageService() domain.ImageService {
	return c.imageService
}

func (c *ServiceContainer) GetPricingService() domain.PricingService {
	return c.PricingService()
}

func (c *ServiceContainer) PricingService() domain.PricingService {
	if c.pricingService == nil {
		c.pricingService = services.NewPricingService(&c.config.Pricing)
	}
	return c.pricingService
}

func (c *ServiceContainer) GetTheme() domain.Theme {
	return c.themeService.GetCurrentTheme()
}

func (c *ServiceContainer) GetThemeService() domain.ThemeService {
	return c.themeService
}

func (c *ServiceContainer) GetShortcutRegistry() *shortcuts.Registry {
	return c.shortcutRegistry
}

// GetA2AAgentService returns the A2A agent service
func (c *ServiceContainer) GetA2AAgentService() domain.A2AAgentService {
	return c.a2aAgentService
}

// New service getters
func (c *ServiceContainer) GetStateManager() domain.StateManager {
	return c.stateManager
}

func (c *ServiceContainer) GetAgentManager() domain.AgentManager {
	return c.agentManager
}

func (c *ServiceContainer) GetAgentService() domain.AgentService {
	return c.agentService
}

func (c *ServiceContainer) GetMessageQueue() domain.MessageQueue {
	return c.messageQueue
}

// GetTaskTrackerService returns the task tracker service
func (c *ServiceContainer) GetTaskTrackerService() domain.TaskTracker {
	return c.taskTrackerService
}

// GetTaskRetentionService returns the task retention service (may be nil if A2A is not enabled)
func (c *ServiceContainer) GetTaskRetentionService() domain.TaskRetentionService {
	return c.taskRetentionService
}

// GetBackgroundTaskService returns the background task service (may be nil if A2A is not enabled)
func (c *ServiceContainer) GetBackgroundTaskService() domain.BackgroundTaskService {
	return c.backgroundTaskService
}

// GetMCPManager returns the MCP manager (may be nil if MCP is not enabled)
func (c *ServiceContainer) GetMCPManager() domain.MCPManager {
	return c.mcpManager
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
			logger.Error("Retrying HTTP request",
				"attempt", attempt,
				"error", err.Error(),
				"delay", delay.String())
			if originalOnRetry != nil {
				originalOnRetry(attempt, err, delay)
			}
		}
	}

	return retryConfig
}

// createSDKClient creates a configured SDK client with retry and timeout settings
func (c *ServiceContainer) createSDKClient() sdk.Client {
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
	})
}

// RegisterCommand allows external registration of commands
func (c *ServiceContainer) RegisterShortcut(shortcut shortcuts.Shortcut) {
	c.shortcutRegistry.Register(shortcut)
}

// File writer service getters
func (c *ServiceContainer) GetPathValidator() filewriterdomain.PathValidator {
	return c.pathValidator
}

func (c *ServiceContainer) GetBackupManager() filewriterdomain.BackupManager {
	return c.backupManager
}

func (c *ServiceContainer) GetFileWriter() filewriterdomain.FileWriter {
	return c.fileWriter
}

func (c *ServiceContainer) GetChunkManager() filewriterdomain.ChunkManager {
	return c.chunkManager
}

func (c *ServiceContainer) GetParameterExtractor() *tools.ParameterExtractor {
	return c.paramExtractor
}

// GetViper returns the Viper instance
func (c *ServiceContainer) GetViper() *viper.Viper {
	return c.viper
}

// GetConfigService returns the config service
func (c *ServiceContainer) GetConfigService() *services.ConfigService {
	return c.configService
}

// GetTitleGenerator returns the conversation title generator
func (c *ServiceContainer) GetTitleGenerator() *services.ConversationTitleGenerator {
	return c.titleGenerator
}

// GetBackgroundJobManager returns the background job manager
func (c *ServiceContainer) GetBackgroundJobManager() *services.BackgroundJobManager {
	return c.backgroundJobManager
}

// GetStorage returns the conversation storage
func (c *ServiceContainer) GetStorage() storage.ConversationStorage {
	return c.storage
}

// GetGatewayManager returns the gateway manager
func (c *ServiceContainer) GetGatewayManager() domain.GatewayManager {
	return c.gatewayManager
}

// ShellTracker returns the shell tracker
func (c *ServiceContainer) ShellTracker() domain.ShellTracker {
	if c.shellTracker == nil {
		maxConcurrent := c.config.Tools.Bash.BackgroundShells.MaxConcurrent
		c.shellTracker = utils.NewShellTracker(maxConcurrent)
	}
	return c.shellTracker
}

// BackgroundShellService returns the background shell service
func (c *ServiceContainer) BackgroundShellService() *services.BackgroundShellService {
	if c.backgroundShellService == nil {
		c.backgroundShellService = services.NewBackgroundShellService(
			c.ShellTracker(),
			c.config,
			nil,
		)
	}
	return c.backgroundShellService
}

// Shutdown gracefully shuts down the service container and its resources
func (c *ServiceContainer) Shutdown(ctx context.Context) error {
	if c.backgroundShellService != nil {
		logger.Info("Stopping background shell service...")
		c.backgroundShellService.Stop()
	}

	if c.agentManager != nil && c.agentManager.IsRunning() {
		logger.Info("Shutting down agent containers...")
		if err := c.agentManager.StopAgents(ctx); err != nil {
			logger.Error("Failed to stop agent containers", "error", err)
		}
	}

	if c.gatewayManager != nil && c.gatewayManager.IsRunning() {
		if err := c.gatewayManager.Stop(ctx); err != nil {
			logger.Error("Failed to stop gateway container", "error", err)
			return err
		}
	}

	if c.mcpManager != nil {
		if err := c.mcpManager.Close(); err != nil {
			logger.Error("Failed to close MCP manager", "error", err)
		}
	}

	return nil
}
