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
	sdk "github.com/inference-gateway/sdk"
	viper "github.com/spf13/viper"
)

// ServiceContainer manages all application dependencies
type ServiceContainer struct {
	// Configuration
	viper         *viper.Viper
	config        *config.Config
	configService *services.ConfigService

	// Domain services
	conversationRepo      domain.ConversationRepository
	modelService          domain.ModelService
	chatService           domain.ChatService
	agentService          domain.AgentService
	toolService           domain.ToolService
	fileService           domain.FileService
	a2aAgentService       domain.A2AAgentService
	messageQueue          domain.MessageQueue
	taskTrackerService    domain.TaskTracker
	taskRetentionService  domain.TaskRetentionService
	backgroundTaskService domain.BackgroundTaskService
	gatewayManager        *services.GatewayManager

	// Services
	stateManager domain.StateManager

	// Background services
	titleGenerator       *services.ConversationTitleGenerator
	backgroundJobManager *services.BackgroundJobManager
	storage              storage.ConversationStorage

	// UI components
	themeService domain.ThemeService

	// Extensibility
	shortcutRegistry *shortcuts.Registry

	// Tool registry
	toolRegistry *tools.Registry

	// File writing services
	pathValidator  filewriterdomain.PathValidator
	backupManager  filewriterdomain.BackupManager
	fileWriter     filewriterdomain.FileWriter
	chunkManager   filewriterdomain.ChunkManager
	paramExtractor *tools.ParameterExtractor
}

// NewServiceContainer creates a new service container with all dependencies
func NewServiceContainer(cfg *config.Config, v ...*viper.Viper) *ServiceContainer {
	container := &ServiceContainer{
		config: cfg,
	}

	if len(v) > 0 && v[0] != nil {
		container.viper = v[0]
		container.configService = services.NewConfigService(v[0], cfg)
	}

	container.initializeGatewayManager()
	container.initializeFileWriterServices()
	container.initializeDomainServices()
	container.initializeServices()
	container.initializeUIComponents()
	container.initializeExtensibility()

	return container
}

// initializeGatewayManager creates and starts the gateway manager if configured
func (c *ServiceContainer) initializeGatewayManager() {
	c.gatewayManager = services.NewGatewayManager(c.config)

	if c.config.Gateway.Run {
		ctx := context.Background()
		if err := c.gatewayManager.Start(ctx); err != nil {
			fmt.Printf("Failed to start gateway: %v\n", err)
			logger.Error("Failed to start gateway", "error", err)
			logger.Warn("Continuing without local gateway - make sure gateway is running manually")
		}
	}
}

// initializeFileWriterServices creates the new file writer architecture services
func (c *ServiceContainer) initializeFileWriterServices() {
	c.pathValidator = filewriterservice.NewPathValidator(c.config)
	c.backupManager = filewriterservice.NewBackupManager(".")
	c.fileWriter = filewriterservice.NewSafeFileWriter(c.pathValidator, c.backupManager)
	c.chunkManager = filewriterservice.NewStreamingChunkManager("./.infer/tmp", c.fileWriter)
	c.paramExtractor = tools.NewParameterExtractor()
}

// initializeDomainServices creates and wires domain service implementations
func (c *ServiceContainer) initializeDomainServices() {
	c.fileService = services.NewFileService()
	c.messageQueue = services.NewMessageQueueService()

	c.toolRegistry = tools.NewRegistry(c.config)
	c.taskTrackerService = c.toolRegistry.GetTaskTracker()

	toolFormatterService := services.NewToolFormatterService(c.toolRegistry)

	storageConfig := storage.NewStorageFromConfig(c.config)
	storageBackend, err := storage.NewStorage(storageConfig)
	if err != nil {
		logger.Error("Failed to initialize storage, using basic in-memory repository", "error", err, "type", storageConfig.Type)
		c.conversationRepo = services.NewInMemoryConversationRepository(toolFormatterService)
	} else {
		c.storage = storageBackend
		persistentRepo := services.NewPersistentConversationRepository(toolFormatterService, storageBackend)
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

	var optimizer *services.ConversationOptimizer
	if c.config.Agent.Optimization.Enabled {
		summaryClient := c.createSDKClient()
		optimizer = services.NewConversationOptimizer(services.OptimizerConfig{
			Enabled:     c.config.Agent.Optimization.Enabled,
			Model:       c.config.Agent.Optimization.Model,
			MinMessages: c.config.Agent.Optimization.MinMessages,
			BufferSize:  c.config.Agent.Optimization.BufferSize,
			Client:      summaryClient,
			Config:      c.config,
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
		c.config.Gateway.Timeout,
		optimizer,
	)

	c.chatService = services.NewStreamingChatService(c.agentService)
}

// initializeServices creates the new improved services
func (c *ServiceContainer) initializeServices() {
	debugMode := c.config.Logging.Debug
	c.stateManager = services.NewStateManager(debugMode)

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
	c.shortcutRegistry.Register(shortcuts.NewExportShortcut(c.conversationRepo, c.agentService, c.modelService, c.config))
	c.shortcutRegistry.Register(shortcuts.NewExitShortcut())
	c.shortcutRegistry.Register(shortcuts.NewSwitchShortcut(c.modelService))
	c.shortcutRegistry.Register(shortcuts.NewThemeShortcut(c.themeService))
	c.shortcutRegistry.Register(shortcuts.NewHelpShortcut(c.shortcutRegistry))

	if persistentRepo, ok := c.conversationRepo.(*services.PersistentConversationRepository); ok {
		adapter := adapters.NewPersistentConversationAdapter(persistentRepo)
		c.shortcutRegistry.Register(shortcuts.NewConversationSelectShortcut(adapter))
		c.shortcutRegistry.Register(shortcuts.NewNewShortcut(adapter, c.taskTrackerService))
	}

	gitCommitClient := c.createSDKClient()
	c.shortcutRegistry.Register(shortcuts.NewGitShortcut(gitCommitClient, c.config))

	c.shortcutRegistry.Register(shortcuts.NewA2AShortcut(c.config, c.a2aAgentService))

	if c.config.IsA2AToolsEnabled() {
		c.shortcutRegistry.Register(shortcuts.NewA2ATaskManagementShortcut(c.config))
	}

	if c.configService != nil {
		c.shortcutRegistry.Register(shortcuts.NewConfigShortcut(c.config, c.configService.Reload, c.configService))
	} else {
		c.shortcutRegistry.Register(shortcuts.NewConfigShortcut(c.config, nil, nil))
	}

	configDir := c.determineConfigDirectory()
	if err := c.shortcutRegistry.LoadCustomShortcuts(configDir); err != nil {
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

// Shutdown gracefully shuts down the service container and its resources
func (c *ServiceContainer) Shutdown(ctx context.Context) error {
	if c.gatewayManager != nil && c.gatewayManager.IsRunning() {
		logger.Info("Shutting down gateway container...")
		if err := c.gatewayManager.Stop(ctx); err != nil {
			logger.Error("Failed to stop gateway container", "error", err)
			return err
		}
	}
	return nil
}
