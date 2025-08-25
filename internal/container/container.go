package container

import (
	"strings"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	filewriterdomain "github.com/inference-gateway/cli/internal/domain/filewriter"
	logger "github.com/inference-gateway/cli/internal/logger"
	services "github.com/inference-gateway/cli/internal/services"
	filewriterservice "github.com/inference-gateway/cli/internal/services/filewriter"
	tools "github.com/inference-gateway/cli/internal/services/tools"
	shortcuts "github.com/inference-gateway/cli/internal/shortcuts"
	ui "github.com/inference-gateway/cli/internal/ui"
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
	conversationRepo domain.ConversationRepository
	modelService     domain.ModelService
	chatService      domain.ChatService
	agentService     domain.AgentService
	toolService      domain.ToolService
	fileService      domain.FileService

	// Services
	stateManager              *services.StateManager
	toolExecutionOrchestrator *services.ToolExecutionOrchestrator

	// UI components
	theme ui.Theme

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

	container.initializeFileWriterServices()
	container.initializeDomainServices()
	container.initializeServices()
	container.initializeUIComponents()
	container.initializeExtensibility()

	return container
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

	c.toolRegistry = tools.NewRegistry(c.config)

	toolFormatterService := services.NewToolFormatterService(c.toolRegistry)
	c.conversationRepo = services.NewInMemoryConversationRepository(toolFormatterService)

	modelClient := c.createSDKClient()
	c.modelService = services.NewHTTPModelService(modelClient)

	if c.config.Tools.Enabled {
		c.toolService = services.NewLLMToolServiceWithRegistry(c.config, c.toolRegistry)
	} else {
		c.toolService = services.NewNoOpToolService()
	}

	var optimizer *services.ConversationOptimizer
	if c.config.Agent.Optimization.Enabled {
		summaryClient := c.createSDKClient()
		optimizer = services.NewConversationOptimizer(services.OptimizerConfig{
			Enabled:                    c.config.Agent.Optimization.Enabled,
			MaxHistory:                 c.config.Agent.Optimization.MaxHistory,
			CompactThreshold:           c.config.Agent.Optimization.CompactThreshold,
			TruncateLargeOutputs:       c.config.Agent.Optimization.TruncateLargeOutputs,
			SkipRedundantConfirmations: c.config.Agent.Optimization.SkipRedundantConfirmations,
			Client:                     summaryClient,
			ModelService:               c.modelService,
			Config:                     c.config,
		})
	}

	agentClient := c.createSDKClient()
	c.agentService = services.NewAgentService(
		agentClient,
		c.toolService,
		c.config.Agent.SystemPrompt,
		c.config.Gateway.Timeout,
		c.config.Agent.MaxTokens,
		optimizer,
	)

	c.chatService = services.NewStreamingChatService(c.agentService)
}

// initializeServices creates the new improved services
func (c *ServiceContainer) initializeServices() {
	debugMode := c.config.Logging.Debug
	c.stateManager = services.NewStateManager(debugMode)

	c.toolExecutionOrchestrator = services.NewToolExecutionOrchestrator(
		c.stateManager,
		c.toolService,
		c.conversationRepo,
		c.config,
	)
}

// initializeUIComponents creates UI components and theme
func (c *ServiceContainer) initializeUIComponents() {
	c.theme = ui.NewDefaultTheme()
}

// initializeExtensibility sets up extensible systems
func (c *ServiceContainer) initializeExtensibility() {
	c.shortcutRegistry = shortcuts.NewRegistry()
	c.registerDefaultCommands()
}

// registerDefaultCommands registers the built-in commands
func (c *ServiceContainer) registerDefaultCommands() {
	c.shortcutRegistry.Register(shortcuts.NewClearShortcut(c.conversationRepo))
	c.shortcutRegistry.Register(shortcuts.NewExportShortcut(c.conversationRepo, c.agentService, c.modelService, c.config))
	c.shortcutRegistry.Register(shortcuts.NewExitShortcut())
	c.shortcutRegistry.Register(shortcuts.NewSwitchShortcut(c.modelService))
	c.shortcutRegistry.Register(shortcuts.NewHelpShortcut(c.shortcutRegistry))

	gitCommitClient := c.createSDKClient()
	c.shortcutRegistry.Register(shortcuts.NewGitShortcut(gitCommitClient, c.config))

	if c.configService != nil {
		c.shortcutRegistry.Register(shortcuts.NewConfigShortcut(c.config, c.configService.Reload, c.configService))
	} else {
		c.shortcutRegistry.Register(shortcuts.NewConfigShortcut(c.config, nil, nil))
	}

	if err := c.shortcutRegistry.LoadCustomShortcuts("."); err != nil {
		logger.Error("Failed to load custom shortcuts", "error", err)
	}
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

func (c *ServiceContainer) GetTheme() ui.Theme {
	return c.theme
}

func (c *ServiceContainer) GetShortcutRegistry() *shortcuts.Registry {
	return c.shortcutRegistry
}

// New service getters
func (c *ServiceContainer) GetStateManager() *services.StateManager {
	return c.stateManager
}

func (c *ServiceContainer) GetToolExecutionOrchestrator() *services.ToolExecutionOrchestrator {
	return c.toolExecutionOrchestrator
}

func (c *ServiceContainer) GetAgentService() domain.AgentService {
	return c.agentService
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
