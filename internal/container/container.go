package container

import (
	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/commands"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/handlers"
	"github.com/inference-gateway/cli/internal/services"
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

	// UI components
	theme  ui.Theme
	layout ui.Layout

	// Extensibility
	commandRegistry *commands.Registry
	messageRouter   *handlers.MessageRouter

	// Component factory
	componentFactory *ui.ComponentFactory
}

// NewServiceContainer creates a new service container with all dependencies
func NewServiceContainer(cfg *config.Config) *ServiceContainer {
	container := &ServiceContainer{
		config: cfg,
	}

	container.initializeDomainServices()
	container.initializeUIComponents()
	container.initializeExtensibility()

	return container
}

// initializeDomainServices creates and wires domain service implementations
func (c *ServiceContainer) initializeDomainServices() {
	// Create conversation repository
	c.conversationRepo = services.NewInMemoryConversationRepository()

	// Create model service
	c.modelService = services.NewHTTPModelService(
		c.config.Gateway.URL,
		c.config.Gateway.APIKey,
	)

	// Create tool service first (needed by chat service)
	if c.config.Tools.Enabled {
		c.toolService = services.NewLLMToolService(c.config)
	} else {
		c.toolService = services.NewNoOpToolService()
	}

	// Create chat service with tool service
	c.chatService = services.NewStreamingChatService(
		c.config.Gateway.URL,
		c.config.Gateway.APIKey,
		c.config.Gateway.Timeout,
		c.toolService,
	)

	// Create file service
	c.fileService = services.NewLocalFileService()
}

// initializeUIComponents creates UI components and theme
func (c *ServiceContainer) initializeUIComponents() {
	// Create theme based on configuration
	c.theme = ui.NewDefaultTheme()

	// Create layout manager
	c.layout = ui.NewDefaultLayout()

	// Create component factory
	c.componentFactory = ui.NewComponentFactory(c.theme, c.layout, c.modelService)
}

// initializeExtensibility sets up extensible systems
func (c *ServiceContainer) initializeExtensibility() {
	c.commandRegistry = commands.NewRegistry()
	c.registerDefaultCommands()

	c.componentFactory.SetCommandRegistry(c.commandRegistry)

	c.messageRouter = handlers.NewMessageRouter()
	c.registerMessageHandlers()
}

// registerDefaultCommands registers the built-in commands
func (c *ServiceContainer) registerDefaultCommands() {
	c.commandRegistry.Register(commands.NewClearCommand(c.conversationRepo))
	c.commandRegistry.Register(commands.NewExportCommand(c.conversationRepo, c.chatService, c.modelService, c.config))
	c.commandRegistry.Register(commands.NewHelpCommand(c.commandRegistry))
	c.commandRegistry.Register(commands.NewExitCommand())
	c.commandRegistry.Register(commands.NewHistoryCommand(c.conversationRepo))
	c.commandRegistry.Register(commands.NewModelsCommand(c.modelService))
	c.commandRegistry.Register(commands.NewSwitchCommand(c.modelService))
}

// registerMessageHandlers registers the message handlers
func (c *ServiceContainer) registerMessageHandlers() {
	c.messageRouter.AddHandler(handlers.NewChatMessageHandler(
		c.chatService,
		c.conversationRepo,
		c.modelService,
		c.commandRegistry,
	))

	c.messageRouter.AddHandler(handlers.NewFileMessageHandler(c.fileService))

	if c.config.Tools.Enabled {
		c.messageRouter.AddHandler(handlers.NewToolMessageHandler(c.toolService))
	}

	c.messageRouter.AddHandler(handlers.NewUIMessageHandler())
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

func (c *ServiceContainer) GetFileService() domain.FileService {
	return c.fileService
}

func (c *ServiceContainer) GetTheme() ui.Theme {
	return c.theme
}

func (c *ServiceContainer) GetLayout() ui.Layout {
	return c.layout
}

func (c *ServiceContainer) GetCommandRegistry() *commands.Registry {
	return c.commandRegistry
}

func (c *ServiceContainer) GetMessageRouter() *handlers.MessageRouter {
	return c.messageRouter
}

func (c *ServiceContainer) GetComponentFactory() *ui.ComponentFactory {
	return c.componentFactory
}

// RegisterCommand allows external registration of commands
func (c *ServiceContainer) RegisterCommand(cmd commands.Command) {
	c.commandRegistry.Register(cmd)
}

// RegisterMessageHandler allows external registration of message handlers
func (c *ServiceContainer) RegisterMessageHandler(handler handlers.MessageHandler) {
	c.messageRouter.AddHandler(handler)
}
