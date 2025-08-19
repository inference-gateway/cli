package container

import (
	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/commands"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/services"
	"github.com/inference-gateway/cli/internal/services/tools"
	"github.com/inference-gateway/cli/internal/ui"
)

// ServiceContainer manages all application dependencies
type ServiceContainer struct {
	// Configuration
	config *config.Config

	// Domain services
	conversationRepo domain.ConversationRepository
	modelService     domain.ModelService
	chatService      domain.ChatService
	toolService      domain.ToolService
	fileService      domain.FileService

	// New improved services
	stateManager              *services.StateManager
	debugService              *services.DebugService
	toolExecutionOrchestrator *services.ToolExecutionOrchestrator

	// UI components
	theme ui.Theme

	// Extensibility
	commandRegistry *commands.Registry

	// Tool registry
	toolRegistry *tools.Registry
}

// NewServiceContainer creates a new service container with all dependencies
func NewServiceContainer(cfg *config.Config) *ServiceContainer {
	container := &ServiceContainer{
		config: cfg,
	}

	container.initializeDomainServices()
	container.initializeServices()
	container.initializeUIComponents()
	container.initializeExtensibility()

	return container
}

// initializeDomainServices creates and wires domain service implementations
func (c *ServiceContainer) initializeDomainServices() {
	c.conversationRepo = services.NewInMemoryConversationRepository()

	c.modelService = services.NewHTTPModelService(
		c.config.Gateway.URL,
		c.config.Gateway.APIKey,
	)

	c.fileService = services.NewFileService()

	c.toolRegistry = tools.NewRegistry(c.config)

	if c.config.Tools.Enabled {
		c.toolService = services.NewLLMToolServiceWithRegistry(c.config, c.toolRegistry)
	} else {
		c.toolService = services.NewNoOpToolService()
	}

	c.chatService = services.NewStreamingChatService(
		c.config.Gateway.URL,
		c.config.Gateway.APIKey,
		c.config.Gateway.Timeout,
		c.toolService,
		c.config.Chat.SystemPrompt,
	)
}

// initializeServices creates the new improved services
func (c *ServiceContainer) initializeServices() {
	debugMode := c.config.Output.Debug
	c.stateManager = services.NewStateManager(debugMode)

	outputDir := c.config.Compact.OutputDir
	if outputDir == "" {
		outputDir = ".infer"
	}
	c.debugService = services.NewDebugService(debugMode, c.stateManager, outputDir)

	c.toolExecutionOrchestrator = services.NewToolExecutionOrchestrator(
		c.stateManager,
		c.debugService,
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
	c.commandRegistry = commands.NewRegistry()
	c.registerDefaultCommands()
}

// registerDefaultCommands registers the built-in commands
func (c *ServiceContainer) registerDefaultCommands() {
	c.commandRegistry.Register(commands.NewClearCommand(c.conversationRepo))
	c.commandRegistry.Register(commands.NewExportCommand(c.conversationRepo, c.chatService, c.modelService, c.config))
	c.commandRegistry.Register(commands.NewExitCommand())
	c.commandRegistry.Register(commands.NewHistoryCommand(c.conversationRepo))
	c.commandRegistry.Register(commands.NewModelsCommand(c.modelService))
	c.commandRegistry.Register(commands.NewSwitchCommand(c.modelService))
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

func (c *ServiceContainer) GetCommandRegistry() *commands.Registry {
	return c.commandRegistry
}

// New service getters
func (c *ServiceContainer) GetStateManager() *services.StateManager {
	return c.stateManager
}

func (c *ServiceContainer) GetDebugService() *services.DebugService {
	return c.debugService
}

func (c *ServiceContainer) GetToolExecutionOrchestrator() *services.ToolExecutionOrchestrator {
	return c.toolExecutionOrchestrator
}

// RegisterCommand allows external registration of commands
func (c *ServiceContainer) RegisterCommand(cmd commands.Command) {
	c.commandRegistry.Register(cmd)
}
