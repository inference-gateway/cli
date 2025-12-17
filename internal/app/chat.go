package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	formatting "github.com/inference-gateway/cli/internal/formatting"
	handlers "github.com/inference-gateway/cli/internal/handlers"
	adapters "github.com/inference-gateway/cli/internal/infra/adapters"
	logger "github.com/inference-gateway/cli/internal/logger"
	services "github.com/inference-gateway/cli/internal/services"
	tools "github.com/inference-gateway/cli/internal/services/tools"
	shortcuts "github.com/inference-gateway/cli/internal/shortcuts"
	ui "github.com/inference-gateway/cli/internal/ui"
	autocomplete "github.com/inference-gateway/cli/internal/ui/autocomplete"
	components "github.com/inference-gateway/cli/internal/ui/components"
	factory "github.com/inference-gateway/cli/internal/ui/components/factory"
	keybinding "github.com/inference-gateway/cli/internal/ui/keybinding"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
	sdk "github.com/inference-gateway/sdk"
)

// ChatApplication represents the main application model using state management
type ChatApplication struct {
	// Dependencies
	configService         *config.Config
	agentService          domain.AgentService
	conversationRepo      domain.ConversationRepository
	conversationOptimizer domain.ConversationOptimizerService
	modelService          domain.ModelService
	toolService           domain.ToolService
	fileService           domain.FileService
	imageService          domain.ImageService
	pricingService        domain.PricingService
	shortcutRegistry      *shortcuts.Registry
	themeService          domain.ThemeService
	toolRegistry          *tools.Registry
	mcpManager            domain.MCPManager
	taskRetentionService  domain.TaskRetentionService
	backgroundTaskService domain.BackgroundTaskService

	// State management
	stateManager domain.StateManager
	messageQueue domain.MessageQueue
	mouseEnabled bool

	// UI components
	conversationView     ui.ConversationRenderer
	inputView            ui.InputComponent
	autocomplete         ui.AutocompleteComponent
	inputStatusBar       ui.InputStatusBarComponent
	statusView           ui.StatusComponent
	modeIndicator        *components.ModeIndicator
	helpBar              ui.HelpBarComponent
	queueBoxView         *components.QueueBoxView
	todoBoxView          *components.TodoBoxView
	modelSelector        *components.ModelSelectorImpl
	themeSelector        *components.ThemeSelectorImpl
	conversationSelector *components.ConversationSelectorImpl
	fileSelectionView    *components.FileSelectionView
	taskManager          *components.TaskManagerImpl
	toolCallRenderer     *components.ToolCallRenderer
	initGithubActionView *components.InitGithubActionView
	messageHistoryView   *components.MessageHistorySelector

	// Presentation layer
	applicationViewRenderer *components.ApplicationViewRenderer
	fileSelectionHandler    *components.FileSelectionHandler

	// Event handling
	chatHandler           handlers.EventHandler
	messageHistoryHandler *handlers.MessageHistoryHandler

	// Current active component for key handling
	focusedComponent ui.InputComponent

	// Key binding system
	keyBindingManager *keybinding.KeyBindingManager

	// Track last key handled by keybinding action to prevent double-handling
	lastHandledKey string

	// Available models
	availableModels []string

	// Configuration
	configDir string
}

// nolint: funlen // NewChatApplication creates a new chat application
func NewChatApplication(
	models []string,
	defaultModel string,
	agentService domain.AgentService,
	conversationRepo domain.ConversationRepository,
	conversationOptimizer domain.ConversationOptimizerService,
	modelService domain.ModelService,
	configService *config.Config,
	toolService domain.ToolService,
	fileService domain.FileService,
	imageService domain.ImageService,
	pricingService domain.PricingService,
	shortcutRegistry *shortcuts.Registry,
	stateManager domain.StateManager,
	messageQueue domain.MessageQueue,
	themeService domain.ThemeService,
	toolRegistry *tools.Registry,
	mcpManager domain.MCPManager,
	taskRetentionService domain.TaskRetentionService,
	backgroundTaskService domain.BackgroundTaskService,
	agentManager domain.AgentManager,
	configPath string,
	versionInfo domain.VersionInfo,
) *ChatApplication {
	initialView := domain.ViewStateModelSelection
	if defaultModel != "" {
		initialView = domain.ViewStateChat
	}

	app := &ChatApplication{
		agentService:          agentService,
		conversationRepo:      conversationRepo,
		conversationOptimizer: conversationOptimizer,
		modelService:          modelService,
		configService:         configService,
		toolService:           toolService,
		fileService:           fileService,
		imageService:          imageService,
		pricingService:        pricingService,
		shortcutRegistry:      shortcutRegistry,
		themeService:          themeService,
		toolRegistry:          toolRegistry,
		mcpManager:            mcpManager,
		taskRetentionService:  taskRetentionService,
		backgroundTaskService: backgroundTaskService,
		availableModels:       models,
		stateManager:          stateManager,
		messageQueue:          messageQueue,
		mouseEnabled:          true,
	}

	if err := app.stateManager.TransitionToView(initialView); err != nil {
		logger.Error("Failed to transition to initial view", "error", err)
	}

	styleProvider := styles.NewProvider(app.themeService)

	app.toolCallRenderer = components.NewToolCallRenderer(styleProvider)
	app.conversationView = factory.CreateConversationView(app.themeService)
	toolFormatterService := services.NewToolFormatterService(app.toolRegistry)

	if cv, ok := app.conversationView.(*components.ConversationView); ok {
		cv.SetToolFormatter(toolFormatterService)
		cv.SetConfigPath(configPath)
		cv.SetVersionInfo(versionInfo)
		cv.SetToolCallRenderer(app.toolCallRenderer)
		cv.SetStateManager(app.stateManager)
	}

	configDir := ".infer"
	if configPath != "" {
		configDir = filepath.Dir(configPath)
	}
	app.configDir = configDir

	app.inputView = factory.CreateInputViewWithConfigDir(app.modelService, configDir)
	if iv, ok := app.inputView.(*components.InputView); ok {
		iv.SetThemeService(app.themeService)
		iv.SetStateManager(app.stateManager)
		iv.SetImageService(app.imageService)
		iv.SetConfigService(app.configService)
		iv.SetConversationRepo(app.conversationRepo)
	}

	app.autocomplete = factory.CreateAutocomplete(app.shortcutRegistry, app.toolService, app.modelService, app.pricingService)
	if ac, ok := app.autocomplete.(*autocomplete.AutocompleteImpl); ok {
		ac.SetStateManager(app.stateManager)
	}

	app.inputStatusBar = factory.CreateInputStatusBar(app.themeService)
	if isb, ok := app.inputStatusBar.(*components.InputStatusBar); ok {
		isb.SetModelService(app.modelService)
		isb.SetThemeService(app.themeService)
		isb.SetStateManager(app.stateManager)
		isb.SetConfigService(app.configService)
		isb.SetConversationRepo(app.conversationRepo)
		isb.SetToolService(app.toolService)
		isb.SetTokenEstimator(services.NewTokenizerService(services.DefaultTokenizerConfig()))
		isb.SetBackgroundShellService(app.toolRegistry.GetBackgroundShellService())
		isb.SetBackgroundTaskService(app.backgroundTaskService)
	}

	app.statusView = factory.CreateStatusView(app.themeService)
	app.modeIndicator = components.NewModeIndicator(styleProvider)
	app.modeIndicator.SetStateManager(app.stateManager)
	app.helpBar = factory.CreateHelpBar(app.themeService)
	app.queueBoxView = components.NewQueueBoxView(styleProvider)
	app.todoBoxView = components.NewTodoBoxView(styleProvider)

	app.fileSelectionView = components.NewFileSelectionView(styleProvider)

	app.applicationViewRenderer = components.NewApplicationViewRenderer(styleProvider)
	app.fileSelectionHandler = components.NewFileSelectionHandler(styleProvider)

	app.keyBindingManager = keybinding.NewKeyBindingManager(app, app.configService)
	app.updateHelpBarShortcuts()

	keyHintFormatter := app.keyBindingManager.GetHintFormatter()
	if cv, ok := app.conversationView.(*components.ConversationView); ok {
		cv.SetKeyHintFormatter(keyHintFormatter)
	}
	if sv, ok := app.statusView.(*components.StatusView); ok {
		sv.SetKeyHintFormatter(keyHintFormatter)
	}

	app.toolCallRenderer.SetKeyHintFormatter(keyHintFormatter)
	app.modelSelector = components.NewModelSelector(models, app.modelService, app.pricingService, styleProvider)
	app.themeSelector = components.NewThemeSelector(app.themeService, styleProvider)
	app.initGithubActionView = components.NewInitGithubActionView(styleProvider)

	app.initGithubActionView.SetSecretsExistChecker(func(appID string) bool {
		repo, err := app.getCurrentRepo()
		if err != nil {
			return false
		}

		isOrg, err := app.isOrgRepo(repo)
		if err != nil || !isOrg {
			return false
		}

		orgName := strings.Split(repo, "/")[0]
		secretsExist, err := app.checkOrgSecretsExist(orgName)
		return err == nil && secretsExist
	})

	if persistentRepo, ok := app.conversationRepo.(*services.PersistentConversationRepository); ok {
		adapter := adapters.NewPersistentConversationAdapter(persistentRepo)
		app.conversationSelector = components.NewConversationSelector(adapter, styleProvider)
	} else {
		app.conversationSelector = nil
	}

	app.taskManager = nil

	if initialView == domain.ViewStateChat {
		app.focusedComponent = app.inputView
	} else {
		app.focusedComponent = nil
	}

	app.chatHandler = handlers.NewChatHandler(
		app.agentService,
		app.conversationRepo,
		app.conversationOptimizer,
		app.modelService,
		app.configService,
		app.toolService,
		app.fileService,
		app.imageService,
		app.shortcutRegistry,
		app.stateManager,
		messageQueue,
		app.taskRetentionService,
		app.backgroundTaskService,
		app.toolRegistry.GetBackgroundShellService(),
		agentManager,
		configService,
	)

	app.messageHistoryHandler = handlers.NewMessageHistoryHandler(
		app.stateManager,
		app.conversationRepo,
	)

	return app
}

// updateHelpBarShortcuts updates the help bar with essential keyboard shortcuts
func (app *ChatApplication) updateHelpBarShortcuts() {
	var shortcuts []ui.KeyShortcut

	var keyBindingShortcuts []keybinding.HelpShortcut
	if app.keyBindingManager != nil {
		keyBindingShortcuts = app.keyBindingManager.GetHelpShortcuts()
	}

	shortcuts = append(shortcuts, ui.KeyShortcut{Key: "!", Description: "for bash mode"})
	shortcuts = append(shortcuts, ui.KeyShortcut{Key: "!!", Description: "for tools mode"})
	shortcuts = append(shortcuts, ui.KeyShortcut{Key: "/", Description: "for shortcuts"})
	shortcuts = append(shortcuts, ui.KeyShortcut{Key: "@", Description: "for file paths"})
	shortcuts = append(shortcuts, ui.KeyShortcut{Key: "#", Description: "to memorize(not implemented)"})

	for _, kbShortcut := range keyBindingShortcuts {
		shortcuts = append(shortcuts, ui.KeyShortcut{
			Key:         kbShortcut.Key,
			Description: kbShortcut.Description,
		})
	}

	app.helpBar.SetShortcuts(shortcuts)
}

// Init initializes the application
func (app *ChatApplication) Init() tea.Cmd {
	var cmds []tea.Cmd

	cmds = append(cmds, tea.ClearScreen)

	if cmd := app.conversationView.(tea.Model).Init(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if cmd := app.inputView.(tea.Model).Init(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if cmd := app.statusView.(tea.Model).Init(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if cmd := app.modelSelector.Init(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if app.conversationSelector != nil {
		if cmd := app.conversationSelector.Init(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	if readiness := app.stateManager.GetAgentReadiness(); readiness != nil && readiness.TotalAgents > 0 {
		cmds = append(cmds, func() tea.Msg {
			time.Sleep(500 * time.Millisecond)
			return domain.AgentStatusUpdateEvent{}
		})
	}

	if app.mcpManager != nil {
		app.inputStatusBar.UpdateMCPStatus(&domain.MCPServerStatus{
			TotalServers:     app.mcpManager.GetTotalServers(),
			ConnectedServers: 0,
			TotalTools:       0,
		})

		ctx := context.Background()
		statusChan := app.mcpManager.StartMonitoring(ctx)
		cmds = append(cmds, waitForMCPStatusUpdate(statusChan))
	}

	return tea.Batch(cmds...)
}

// waitForMCPStatusUpdate waits for a status update from the MCP manager channel
// The channel is captured in the closure and passed along with each event
func waitForMCPStatusUpdate(statusChan <-chan domain.MCPServerStatusUpdateEvent) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-statusChan
		if !ok {
			return nil
		}

		return mcpStatusUpdateWithChannel{
			event:   event,
			channel: statusChan,
		}
	}
}

// mcpStatusUpdateWithChannel wraps the event with the channel for continuation
type mcpStatusUpdateWithChannel struct {
	event   domain.MCPServerStatusUpdateEvent
	channel <-chan domain.MCPServerStatusUpdateEvent
}

// Update handles all application messages using the state management system
func (app *ChatApplication) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	if _, ok := msg.(domain.TriggerGithubActionSetupEvent); ok {
		cmds = append(cmds, app.handleGithubActionSetupTrigger()...)
		return app, tea.Batch(cmds...)
	}

	if cmd := app.chatHandler.Handle(msg); cmd != nil {
		cmds = append(cmds, cmd)
	}

	switch m := msg.(type) {
	case domain.NavigateBackInTimeEvent:
		if cmd := app.messageHistoryHandler.HandleNavigateBackInTime(m); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case domain.MessageHistoryRestoreEvent:
		if cmd := app.messageHistoryHandler.HandleRestore(m); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	cmds = append(cmds, app.handleViewSpecificMessages(msg)...)

	cmds = append(cmds, app.updateUIComponentsForUIMessages(msg)...)

	if mcpStatusUpdate, ok := msg.(mcpStatusUpdateWithChannel); ok {
		if cmd := app.handleMCPStatusUpdate(mcpStatusUpdate.event); cmd != nil {
			cmds = append(cmds, cmd)
		}
		cmds = append(cmds, waitForMCPStatusUpdate(mcpStatusUpdate.channel))
	}

	return app, tea.Batch(cmds...)
}

// handleMCPStatusUpdate processes MCP server connection status changes
func (app *ChatApplication) handleMCPStatusUpdate(event domain.MCPServerStatusUpdateEvent) tea.Cmd {
	app.inputStatusBar.UpdateMCPStatus(&domain.MCPServerStatus{
		TotalServers:     event.TotalServers,
		ConnectedServers: event.ConnectedServers,
		TotalTools:       event.TotalTools,
	})

	if app.toolRegistry == nil {
		return nil
	}

	if event.Connected && len(event.Tools) > 0 {
		count := app.toolRegistry.RegisterMCPServerTools(event.ServerName, event.Tools)
		logger.Debug("Registered MCP tools", "server", event.ServerName, "count", count)
	}

	if !event.Connected {
		count := app.toolRegistry.UnregisterMCPServerTools(event.ServerName)
		logger.Debug("Unregistered MCP tools", "server", event.ServerName, "count", count)
	}

	if app.autocomplete != nil {
		app.autocomplete.RefreshToolsList()
		return func() tea.Msg {
			return domain.RefreshAutocompleteEvent{}
		}
	}

	return nil
}

func (app *ChatApplication) handleViewSpecificMessages(msg tea.Msg) []tea.Cmd {
	currentView := app.stateManager.GetCurrentView()

	if inputView, ok := app.inputView.(*components.InputView); ok {
		if app.stateManager.GetApprovalUIState() != nil || app.stateManager.GetPlanApprovalUIState() != nil {
			inputView.SetDisabled(true)
		} else {
			inputView.SetDisabled(false)
		}
	}

	switch currentView {
	case domain.ViewStateModelSelection:
		return app.handleModelSelectionView(msg)
	case domain.ViewStateChat:
		return app.handleChatView(msg)
	case domain.ViewStateFileSelection:
		return app.handleFileSelectionView(msg)
	case domain.ViewStateConversationSelection:
		return app.handleConversationSelectionView(msg)
	case domain.ViewStateThemeSelection:
		return app.handleThemeSelectionView(msg)
	case domain.ViewStateA2ATaskManagement:
		return app.handleA2ATaskManagementView(msg)
	case domain.ViewStateGithubActionSetup:
		return app.handleInitGithubActionView(msg)
	case domain.ViewStateMessageHistory:
		return app.handleMessageHistoryView(msg)
	default:
		return nil
	}
}

func (app *ChatApplication) handleModelSelectionView(msg tea.Msg) []tea.Cmd {
	var cmds []tea.Cmd

	if model, cmd := app.modelSelector.Update(msg); cmd != nil {
		cmds = append(cmds, cmd)
		app.modelSelector = model.(*components.ModelSelectorImpl)

		return app.handleModelSelection(cmds)
	}

	return cmds
}

func (app *ChatApplication) handleModelSelection(cmds []tea.Cmd) []tea.Cmd {
	if app.modelSelector.IsSelected() {
		if err := app.stateManager.TransitionToView(domain.ViewStateChat); err != nil {
			return []tea.Cmd{tea.Quit}
		}
		app.focusedComponent = app.inputView
	} else if app.modelSelector.IsCancelled() {
		cmds = append(cmds, tea.Quit)
	}
	return cmds
}

func (app *ChatApplication) handleChatView(msg tea.Msg) []tea.Cmd {
	var cmds []tea.Cmd

	if approvalEvent, ok := msg.(domain.PlanApprovalResponseEvent); ok {
		approvalState := app.stateManager.GetPlanApprovalUIState()
		if approvalState != nil && approvalState.ResponseChan != nil {
			approvalState.ResponseChan <- approvalEvent.Action
			app.stateManager.ClearPlanApprovalUIState()
		}
		return cmds
	}

	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return cmds
	}

	return app.handleChatViewKeyPress(keyMsg)
}

func (app *ChatApplication) handleChatViewKeyPress(keyMsg tea.KeyMsg) []tea.Cmd {
	var cmds []tea.Cmd

	isHandledByAction := app.keyBindingManager.IsKeyHandledByAction(keyMsg)

	if cmd := app.keyBindingManager.ProcessKey(keyMsg); cmd != nil {
		cmds = append(cmds, cmd)
	}

	if isHandledByAction {
		app.lastHandledKey = keyMsg.String()
	}

	return cmds
}

func (app *ChatApplication) handleFileSelectionView(msg tea.Msg) []tea.Cmd {
	var cmds []tea.Cmd

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if cmd := app.handleFileSelectionKeys(keyMsg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return cmds
}

// View renders the current application view using state management
func (app *ChatApplication) View() string {
	currentView := app.stateManager.GetCurrentView()

	switch currentView {
	case domain.ViewStateModelSelection:
		return app.renderModelSelection()
	case domain.ViewStateChat:
		return app.renderChatInterface()
	case domain.ViewStateFileSelection:
		return app.renderFileSelection()
	case domain.ViewStateConversationSelection:
		return app.renderConversationSelection()
	case domain.ViewStateThemeSelection:
		return app.renderThemeSelection()
	case domain.ViewStateA2ATaskManagement:
		return app.renderA2ATaskManagement()
	case domain.ViewStateGithubActionSetup:
		return app.renderGithubActionSetup()
	case domain.ViewStateMessageHistory:
		return app.renderMessageHistory()
	default:
		return fmt.Sprintf("Unknown view state: %v", currentView)
	}
}

func (app *ChatApplication) handleInitGithubActionView(msg tea.Msg) []tea.Cmd {
	var cmds []tea.Cmd

	model, cmd := app.initGithubActionView.Update(msg)
	app.initGithubActionView = model.(*components.InitGithubActionView)

	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	if app.initGithubActionView.IsDone() {
		if app.initGithubActionView.IsCancelled() {
			return app.handleInitGithubActionCancelled(cmds)
		}
		return app.handleInitGithubActionCompleted(cmds)
	}

	return cmds
}

func (app *ChatApplication) handleInitGithubActionCompleted(cmds []tea.Cmd) []tea.Cmd {
	appID, privateKeyPath, err := app.initGithubActionView.GetResult()

	if err != nil {
		cmds = append(cmds, func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Init Github Action setup failed: %v", err),
				Sticky: false,
			}
		})
	} else {
		cmds = append(cmds, func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    "Setting up Init Github Action...",
				Spinner:    true,
				StatusType: domain.StatusDefault,
			}
		})

		cmds = append(cmds, app.performGithubActionSetup(appID, privateKeyPath))
	}

	app.initGithubActionView.Reset()
	if cmd := app.initGithubActionView.Init(); cmd != nil {
		cmds = append(cmds, cmd)
	}

	if err := app.stateManager.TransitionToView(domain.ViewStateChat); err != nil {
		cmds = append(cmds, func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Failed to return to chat: %v", err),
				Sticky: false,
			}
		})
	}

	app.focusedComponent = app.inputView
	return cmds
}

func (app *ChatApplication) handleInitGithubActionCancelled(cmds []tea.Cmd) []tea.Cmd {
	cmds = append(cmds, func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    "Init Github Action setup cancelled",
			Spinner:    false,
			StatusType: domain.StatusDefault,
		}
	})

	app.initGithubActionView.Reset()
	if cmd := app.initGithubActionView.Init(); cmd != nil {
		cmds = append(cmds, cmd)
	}

	if err := app.stateManager.TransitionToView(domain.ViewStateChat); err != nil {
		cmds = append(cmds, func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Failed to return to chat: %v", err),
				Sticky: false,
			}
		})
	}

	app.focusedComponent = app.inputView
	return cmds
}

func (app *ChatApplication) handleGithubActionSetupTrigger() []tea.Cmd {
	var cmds []tea.Cmd

	repo, err := app.getCurrentRepo()
	if err != nil {
		cmds = append(cmds, func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Failed to get repository info: %v", err),
				Sticky: true,
			}
		})
		return cmds
	}

	isOrg, err := app.isOrgRepo(repo)
	if err != nil {
		cmds = append(cmds, func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Failed to check repository type: %v", err),
				Sticky: true,
			}
		})
		return cmds
	}

	owner := strings.Split(repo, "/")[0]

	if isOrg {
		secretsExist, err := app.checkOrgSecretsExist(owner)
		if err != nil {
			cmds = append(cmds, func() tea.Msg {
				return domain.ShowErrorEvent{
					Error:  fmt.Sprintf("Failed to check org secrets: %v", err),
					Sticky: true,
				}
			})
			return cmds
		}

		if secretsExist {
			cmds = append(cmds, func() tea.Msg {
				return domain.SetStatusEvent{
					Message:    "Org secrets found, creating workflow...",
					Spinner:    true,
					StatusType: domain.StatusDefault,
				}
			})

			cmds = append(cmds, app.performGithubActionSetup("", ""))

			return cmds
		}
	}

	app.initGithubActionView.SetRepositoryInfo(owner, isOrg)

	if err := app.stateManager.TransitionToView(domain.ViewStateGithubActionSetup); err != nil {
		cmds = append(cmds, func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Failed to show Init Github Action setup: %v", err),
				Sticky: false,
			}
		})
		return cmds
	}

	cmds = append(cmds, func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    "Setting up Init GitHub Action...",
			Spinner:    false,
			StatusType: domain.StatusDefault,
		}
	})

	return cmds
}

func (app *ChatApplication) performGithubActionSetup(appID, privateKeyPath string) tea.Cmd {
	return func() tea.Msg {
		repo, err := app.getCurrentRepo()
		if err != nil {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Failed to get repository info: %v", err),
				Sticky: true,
			}
		}

		isOrg, err := app.isOrgRepo(repo)
		if err != nil {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Failed to check repository type: %v", err),
				Sticky: true,
			}
		}

		if !isOrg {
			return app.setupStandardWorkflow(repo)
		}

		return app.setupOrgWorkflow(repo, appID, privateKeyPath)
	}
}

func (app *ChatApplication) setupStandardWorkflow(repo string) tea.Msg {
	workflowContent := app.generateStandardWorkflowContent()
	workflowPath := ".github/workflows/infer.yml"

	if err := app.writeWorkflowFile(workflowPath, workflowContent); err != nil {
		return domain.ShowErrorEvent{
			Error:  fmt.Sprintf("Failed to write workflow file: %v", err),
			Sticky: true,
		}
	}

	prURL, err := app.preparePRCreation(repo, workflowPath)
	if err != nil {
		return domain.ShowErrorEvent{
			Error:  fmt.Sprintf("Failed to prepare PR: %v. You can manually commit and push the changes.", err),
			Sticky: true,
		}
	}

	return app.createSuccessMessage(repo, prURL, "✅ GitHub workflow configured with github-actions[bot]!")
}

func (app *ChatApplication) setupOrgWorkflow(repo, appID, privateKeyPath string) tea.Msg {
	orgName := strings.Split(repo, "/")[0]

	secretsExist, err := app.checkOrgSecretsExist(orgName)
	if err != nil {
		return domain.ShowErrorEvent{
			Error:  fmt.Sprintf("Failed to check org secrets: %v", err),
			Sticky: true,
		}
	}

	if !secretsExist && privateKeyPath != "" {
		if err := app.setupOrgSecrets(orgName, appID, privateKeyPath); err != nil {
			return err
		}
	}

	workflowContent := app.generateGithubActionWorkflowContent()
	workflowPath := ".github/workflows/infer.yml"

	if err := app.writeWorkflowFile(workflowPath, workflowContent); err != nil {
		return domain.ShowErrorEvent{
			Error:  fmt.Sprintf("Failed to write workflow file: %v", err),
			Sticky: true,
		}
	}

	prURL, err := app.preparePRCreation(repo, workflowPath)
	if err != nil {
		return domain.ShowErrorEvent{
			Error:  fmt.Sprintf("Failed to prepare PR: %v. You can manually commit and push the changes.", err),
			Sticky: true,
		}
	}

	return app.createSuccessMessage(repo, prURL, "✅ GitHub App configured with org-level secrets!")
}

func (app *ChatApplication) setupOrgSecrets(orgName, appID, privateKeyPath string) tea.Msg {
	privateKey, err := app.fileService.ReadFile(privateKeyPath)
	if err != nil {
		return domain.ShowErrorEvent{
			Error:  fmt.Sprintf("Failed to read private key: %v", err),
			Sticky: true,
		}
	}

	if err := app.setOrgSecret(orgName, "INFER_APP_ID", appID); err != nil {
		return domain.ShowErrorEvent{
			Error:  fmt.Sprintf("Failed to set org secret INFER_APP_ID: %v", err),
			Sticky: true,
		}
	}

	if err := app.setOrgSecret(orgName, "INFER_APP_PRIVATE_KEY", privateKey); err != nil {
		return domain.ShowErrorEvent{
			Error:  fmt.Sprintf("Failed to set org secret INFER_APP_PRIVATE_KEY: %v", err),
			Sticky: true,
		}
	}

	return nil
}

func (app *ChatApplication) createSuccessMessage(repo, prURL, successMsg string) tea.Msg {
	parts := strings.Split(repo, "/")
	repoOwner := ""
	repoName := ""
	if len(parts) == 2 {
		repoOwner = parts[0]
		repoName = parts[1]
	}
	installURL := app.initGithubActionView.GetInstallationURL(repoOwner, repoName)

	messageText := fmt.Sprintf("%s\n\n"+
		"Next steps:\n"+
		"1. Install the GitHub App on your repository:\n   %s\n\n"+
		"2. Create your pull request here:\n   %s", successMsg, installURL, prURL)
	message, _ := sdk.NewTextMessage(sdk.Assistant, messageText)
	entry := domain.ConversationEntry{
		Message: message,
		Time:    time.Now(),
	}
	if err := app.conversationRepo.AddMessage(entry); err != nil {
		logger.Error("Failed to add pull request creation message to conversation", "error", err)
	}

	return tea.Batch(
		func() tea.Msg {
			return domain.UpdateHistoryEvent{
				History: app.conversationRepo.GetMessages(),
			}
		},
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    "",
				Spinner:    false,
				StatusType: domain.StatusDefault,
			}
		},
	)()
}

func (app *ChatApplication) handleConversationSelectionView(msg tea.Msg) []tea.Cmd {
	var cmds []tea.Cmd

	if app.conversationSelector == nil {
		cmds = append(cmds, func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  "Conversation selection requires persistent storage (SQLite). Current storage type not supported.",
				Sticky: true,
			}
		})
		return cmds
	}

	isDone := app.conversationSelector.IsSelected() || app.conversationSelector.IsCancelled()
	needsInit := app.conversationSelector.NeedsInitialization()
	fromDifferentView := app.stateManager.GetPreviousView() != domain.ViewStateConversationSelection

	if fromDifferentView && (isDone || needsInit) {
		app.conversationSelector.Reset()
		if cmd := app.conversationSelector.Init(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	model, cmd := app.conversationSelector.Update(msg)
	app.conversationSelector = model.(*components.ConversationSelectorImpl)

	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	return app.handleConversationSelection(cmds)
}

func (app *ChatApplication) handleConversationSelection(cmds []tea.Cmd) []tea.Cmd {
	if app.conversationSelector.IsSelected() {
		return app.handleConversationSelected(cmds)
	}

	if app.conversationSelector.IsCancelled() {
		return app.handleConversationCancelled(cmds)
	}

	return cmds
}

func (app *ChatApplication) handleConversationSelected(cmds []tea.Cmd) []tea.Cmd {
	selectedConv := app.conversationSelector.GetSelected()
	if selectedConv.ID != "" {
		cmds = append(cmds, func() tea.Msg {
			return domain.ConversationSelectedEvent{ConversationID: selectedConv.ID}
		})
	}

	if err := app.stateManager.TransitionToView(domain.ViewStateChat); err != nil {
		return []tea.Cmd{tea.Quit}
	}

	app.focusedComponent = app.inputView
	return cmds
}

func (app *ChatApplication) handleConversationCancelled(cmds []tea.Cmd) []tea.Cmd {
	if err := app.stateManager.TransitionToView(domain.ViewStateChat); err != nil {
		return []tea.Cmd{tea.Quit}
	}

	app.focusedComponent = app.inputView
	return cmds
}

func (app *ChatApplication) handleA2ATaskManagementView(msg tea.Msg) []tea.Cmd {
	var cmds []tea.Cmd

	if app.taskManager == nil {
		if !app.configService.A2A.Enabled {
			cmds = append(cmds, func() tea.Msg {
				return domain.ShowErrorEvent{
					Error:  "Task management requires A2A to be enabled in configuration.",
					Sticky: true,
				}
			})
			return cmds
		}

		styleProvider := styles.NewProvider(app.themeService)
		app.taskManager = components.NewTaskManager(app.themeService, styleProvider, app.taskRetentionService, app.backgroundTaskService)
		if cmd := app.taskManager.Init(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	if app.taskManager.IsDone() || app.taskManager.IsCancelled() {
		app.taskManager.Reset()
		if cmd := app.taskManager.Init(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	model, cmd := app.taskManager.Update(msg)
	app.taskManager = model.(*components.TaskManagerImpl)

	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	return app.handleA2ATaskManagement(cmds)
}

func (app *ChatApplication) handleA2ATaskManagement(cmds []tea.Cmd) []tea.Cmd {
	if app.taskManager.IsCancelled() {
		return app.handleA2ATaskManagementCancelled(cmds)
	}

	return cmds
}

func (app *ChatApplication) handleA2ATaskManagementCancelled(cmds []tea.Cmd) []tea.Cmd {
	if err := app.stateManager.TransitionToView(domain.ViewStateChat); err != nil {
		return []tea.Cmd{tea.Quit}
	}

	app.focusedComponent = app.inputView

	return cmds
}

func (app *ChatApplication) handleThemeSelectionView(msg tea.Msg) []tea.Cmd {
	var cmds []tea.Cmd

	if app.themeSelector.IsSelected() || app.themeSelector.IsCancelled() {
		app.themeSelector.Reset()
		if cmd := app.themeSelector.Init(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	model, cmd := app.themeSelector.Update(msg)
	app.themeSelector = model.(*components.ThemeSelectorImpl)

	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	return app.handleThemeSelection(cmds)
}

func (app *ChatApplication) handleThemeSelection(cmds []tea.Cmd) []tea.Cmd {
	if app.themeSelector.IsSelected() {
		return app.handleThemeSelected(cmds)
	}

	if app.themeSelector.IsCancelled() {
		return app.handleThemeCancelled(cmds)
	}

	return cmds
}

func (app *ChatApplication) handleThemeSelected(cmds []tea.Cmd) []tea.Cmd {
	selectedTheme := app.themeSelector.GetSelected()
	if selectedTheme != "" {
		app.updateAllComponentsWithNewTheme()

		cmds = append(cmds, func() tea.Msg {
			return domain.ThemeSelectedEvent{Theme: selectedTheme}
		})
	}

	return app.handleThemeCancelled(cmds)
}

func (app *ChatApplication) handleThemeCancelled(cmds []tea.Cmd) []tea.Cmd {
	if err := app.stateManager.TransitionToView(domain.ViewStateChat); err != nil {
		cmds = append(cmds, func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Failed to return to chat: %v", err),
				Sticky: false,
			}
		})
	}

	app.focusedComponent = app.inputView

	cmds = append(cmds, func() tea.Msg {
		return domain.UpdateHistoryEvent{
			History: app.conversationRepo.GetMessages(),
		}
	})

	return cmds
}

func (app *ChatApplication) updateAllComponentsWithNewTheme() {
	if inputView, ok := app.inputView.(*components.InputView); ok {
		inputView.SetThemeService(app.themeService)
		inputView.SetImageService(app.imageService)
	}

	if conversationView, ok := app.conversationView.(*components.ConversationView); ok {
		conversationView.RefreshTheme()
	}

	styleProvider := styles.NewProvider(app.themeService)
	app.modelSelector = components.NewModelSelector(app.availableModels, app.modelService, app.pricingService, styleProvider)
}

func (app *ChatApplication) renderThemeSelection() string {
	width, height := app.stateManager.GetDimensions()
	app.themeSelector.SetWidth(width)
	app.themeSelector.SetHeight(height)
	return app.themeSelector.View()
}

func (app *ChatApplication) renderConversationSelection() string {
	if app.conversationSelector == nil {
		return "Conversation selection requires persistent storage to be enabled."
	}

	width, height := app.stateManager.GetDimensions()
	app.conversationSelector.SetWidth(width)
	app.conversationSelector.SetHeight(height)
	return app.conversationSelector.View()
}

func (app *ChatApplication) handleMessageHistoryView(msg tea.Msg) []tea.Cmd {
	var cmds []tea.Cmd

	historyState := app.stateManager.GetMessageHistoryState()
	if historyState == nil {
		err := app.stateManager.TransitionToView(domain.ViewStateChat)
		if err != nil {
			logger.Error("Failed to transition to chat view", "error", err)
		}
		return cmds
	}

	if _, ok := msg.(domain.MessageHistoryReadyEvent); ok {
		app.messageHistoryView = nil
	}

	if app.messageHistoryView == nil {
		styleProvider := styles.NewProvider(app.themeService)
		width, height := app.stateManager.GetDimensions()
		app.messageHistoryView = components.NewMessageHistorySelector(
			historyState.Messages,
			styleProvider,
		)

		app.messageHistoryView.SetDimensions(width, height)
		if cmd := app.messageHistoryView.Init(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	model, cmd := app.messageHistoryView.Update(msg)
	app.messageHistoryView = model.(*components.MessageHistorySelector)

	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	if app.messageHistoryView.IsCancelled() {
		app.stateManager.ClearMessageHistoryState()
		app.messageHistoryView = nil
		err := app.stateManager.TransitionToView(domain.ViewStateChat)
		if err != nil {
			logger.Error("Failed to transition to chat view after cancelling history", "error", err)
		}
		return cmds
	}

	if app.messageHistoryView.IsSelected() {
		app.messageHistoryView = nil
		return cmds
	}

	return cmds
}

func (app *ChatApplication) renderMessageHistory() string {
	if app.messageHistoryView == nil {
		return "Loading message history..."
	}

	return app.messageHistoryView.View()
}

func (app *ChatApplication) renderA2ATaskManagement() string {
	if app.taskManager == nil {
		return "Task management requires A2A to be enabled in configuration."
	}

	width, height := app.stateManager.GetDimensions()
	app.taskManager.SetWidth(width)
	app.taskManager.SetHeight(height)
	return app.taskManager.View()
}

func (app *ChatApplication) renderGithubActionSetup() string {
	width, height := app.stateManager.GetDimensions()
	app.initGithubActionView.SetWidth(width)
	app.initGithubActionView.SetHeight(height)
	return app.initGithubActionView.View()
}

func (app *ChatApplication) renderChatInterface() string {
	app.updateHelpBarShortcuts()

	width, height := app.stateManager.GetDimensions()
	queuedMessages := app.messageQueue.GetAll()

	data := components.ChatInterfaceData{
		Width:          width,
		Height:         height,
		ToolExecution:  app.stateManager.GetToolExecution(),
		CurrentView:    app.stateManager.GetCurrentView(),
		QueuedMessages: queuedMessages,
	}

	chatInterface := app.applicationViewRenderer.RenderChatInterface(
		data,
		app.conversationView,
		app.inputView,
		app.autocomplete,
		app.inputStatusBar,
		app.statusView,
		app.modeIndicator,
		app.helpBar,
		app.queueBoxView,
		app.todoBoxView,
	)

	return chatInterface
}

func (app *ChatApplication) renderModelSelection() string {
	width, height := app.stateManager.GetDimensions()
	app.modelSelector.SetWidth(width)
	app.modelSelector.SetHeight(height)
	return app.modelSelector.View()
}

func (app *ChatApplication) renderFileSelection() string {
	fileState := app.stateManager.GetFileSelectionState()
	width, _ := app.stateManager.GetDimensions()

	if fileState == nil {
		return formatting.FormatWarning("No files available for selection")
	}

	data := components.FileSelectionData{
		Width:         width,
		Files:         fileState.Files,
		SearchQuery:   fileState.SearchQuery,
		SelectedIndex: fileState.SelectedIndex,
	}

	return app.fileSelectionHandler.RenderFileSelection(data)
}

func (app *ChatApplication) handleFileSelectionKeys(keyMsg tea.KeyMsg) tea.Cmd {
	fileState := app.stateManager.GetFileSelectionState()
	if fileState == nil {
		return nil
	}

	newSearchQuery, newSelectedIndex, action, selectedFile := app.fileSelectionHandler.HandleKeyEvent(
		keyMsg,
		fileState.Files,
		fileState.SearchQuery,
		fileState.SelectedIndex,
	)

	if newSearchQuery != fileState.SearchQuery {
		app.stateManager.UpdateFileSearchQuery(newSearchQuery)
	}
	if newSelectedIndex != fileState.SelectedIndex {
		app.stateManager.SetFileSelectedIndex(newSelectedIndex)
	}

	switch action {
	case components.FileSelectionActionSelect:
		app.clearFileSelectionState()
		app.updateInputWithSelectedFile(selectedFile)
		return app.fileSelectionHandler.CreateStatusMessage(action, selectedFile)
	case components.FileSelectionActionCancel:
		app.clearFileSelectionState()
		return app.fileSelectionHandler.CreateStatusMessage(action, selectedFile)
	default:
		return nil
	}
}

func (app *ChatApplication) clearFileSelectionState() {
	if err := app.stateManager.TransitionToView(domain.ViewStateChat); err != nil {
		logger.Error("Failed to transition to chat view after file selection", "error", err)
	}
	app.stateManager.ClearFileSelectionState()
}

func (app *ChatApplication) updateInputWithSelectedFile(selectedFile string) {
	if app.imageService != nil && app.imageService.IsImageFile(selectedFile) {
		imageAttachment, err := app.imageService.ReadImageFromFile(selectedFile)
		if err == nil {
			currentInput := app.inputView.GetInput()
			cursor := app.inputView.GetCursor()
			atIndex := app.findAtSymbolBeforeCursor(currentInput, cursor)
			if atIndex >= 0 {
				newInput := currentInput[:atIndex] + currentInput[cursor:]
				app.inputView.SetText(newInput)
				app.inputView.SetCursor(atIndex)
			}
			app.inputView.AddImageAttachment(*imageAttachment)
			return
		}
	}

	currentInput := app.inputView.GetInput()
	cursor := app.inputView.GetCursor()

	newInput, newCursor := app.fileSelectionHandler.UpdateInputWithSelectedFile(currentInput, cursor, selectedFile)

	app.inputView.SetText(newInput)
	app.inputView.SetCursor(newCursor)
}

// findAtSymbolBeforeCursor finds the position of the @ symbol before the cursor
func (app *ChatApplication) findAtSymbolBeforeCursor(input string, cursor int) int {
	for i := cursor - 1; i >= 0; i-- {
		if input[i] == '@' {
			return i
		}
	}
	return -1
}

func (app *ChatApplication) updateUIComponents(msg tea.Msg) []tea.Cmd {
	var cmds []tea.Cmd

	if handled := app.handleWindowAndSetupEvents(msg, &cmds); handled {
		return cmds
	}

	if handled := app.handleDuplicateKeyEvents(msg, &cmds); handled {
		return cmds
	}

	app.updateMainUIComponents(msg, &cmds)

	app.updateOptionalComponents(msg, &cmds)

	app.handleTodoEvents(msg, &cmds)

	app.handleAutocompleteEvents(msg, &cmds)

	return cmds
}

// handleWindowAndSetupEvents handles window size and setup events that may return early
func (app *ChatApplication) handleWindowAndSetupEvents(msg tea.Msg, _ *[]tea.Cmd) bool {
	if windowMsg, ok := msg.(tea.WindowSizeMsg); ok {
		app.stateManager.SetDimensions(windowMsg.Width, windowMsg.Height)
	}

	if setupMsg, ok := msg.(domain.SetupFileSelectionEvent); ok {
		app.stateManager.SetupFileSelection(setupMsg.Files)
		return true
	}

	return false
}

// handleDuplicateKeyEvents handles duplicate key events to prevent double processing
func (app *ChatApplication) handleDuplicateKeyEvents(msg tea.Msg, _ *[]tea.Cmd) bool {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if keyMsg.String() == app.lastHandledKey {
			app.lastHandledKey = ""
			if app.stateManager.GetApprovalUIState() != nil || app.stateManager.GetPlanApprovalUIState() != nil {
				return false
			}
			return true
		}
	}
	return false
}

// updateUIComponentsForUIMessages updates UI components for UI events and framework messages
func (app *ChatApplication) updateUIComponentsForUIMessages(msg tea.Msg) []tea.Cmd {
	switch msg.(type) {
	case tea.WindowSizeMsg, tea.MouseMsg, tea.KeyMsg, tea.FocusMsg, tea.BlurMsg:
		return app.updateUIComponents(msg)
	}

	msgType := fmt.Sprintf("%T", msg)
	if strings.HasPrefix(msgType, "domain.") || strings.Contains(msgType, "spinner.TickMsg") || strings.Contains(msgType, "Tick") {
		return app.updateUIComponents(msg)
	}

	return nil
}

func (app *ChatApplication) getPageSize() int {
	_, height := app.stateManager.GetDimensions()
	conversationHeight := factory.CalculateConversationHeight(height)
	return max(1, conversationHeight-2)
}

// toggleToolResultExpansion toggles expansion of all tool results
func (app *ChatApplication) toggleToolResultExpansion() {
	app.conversationView.ToggleAllToolResultsExpansion()
}

// updateMainUIComponents updates the main UI components (conversation, status, input, help bar)
func (app *ChatApplication) updateMainUIComponents(msg tea.Msg, cmds *[]tea.Cmd) {
	if model, cmd := app.conversationView.(tea.Model).Update(msg); cmd != nil {
		*cmds = append(*cmds, cmd)
		if convModel, ok := model.(ui.ConversationRenderer); ok {
			app.conversationView = convModel
		}
	}

	if model, cmd := app.statusView.(tea.Model).Update(msg); cmd != nil {
		*cmds = append(*cmds, cmd)
		if statusModel, ok := model.(ui.StatusComponent); ok {
			app.statusView = statusModel
		}
	}

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if app.keyBindingManager.IsKeyHandledByAction(keyMsg) {
			return
		}
	}

	if model, cmd := app.inputView.(tea.Model).Update(msg); cmd != nil {
		*cmds = append(*cmds, cmd)
		if inputModel, ok := model.(ui.InputComponent); ok {
			app.inputView = inputModel
		}
	}

	if model, cmd := app.helpBar.(tea.Model).Update(msg); cmd != nil {
		*cmds = append(*cmds, cmd)
		if helpBarModel, ok := model.(ui.HelpBarComponent); ok {
			app.helpBar = helpBarModel
		}
	}

	if model, cmd := app.inputStatusBar.(tea.Model).Update(msg); cmd != nil {
		*cmds = append(*cmds, cmd)
		if statusBarModel, ok := model.(ui.InputStatusBarComponent); ok {
			app.inputStatusBar = statusBarModel
		}
	}

}

// updateOptionalComponents updates optional components (conversation selector, task manager)
func (app *ChatApplication) updateOptionalComponents(msg tea.Msg, cmds *[]tea.Cmd) {
	if app.conversationSelector != nil {
		switch msg.(type) {
		case domain.ConversationsLoadedEvent:
			model, cmd := app.conversationSelector.Update(msg)
			if cmd != nil {
				*cmds = append(*cmds, cmd)
			}
			if convSelectorModel, ok := model.(*components.ConversationSelectorImpl); ok {
				app.conversationSelector = convSelectorModel
			}
		}
	}

	if app.taskManager != nil {
		switch msg.(type) {
		case domain.TasksLoadedEvent, domain.TaskCancelledEvent:
			model, cmd := app.taskManager.Update(msg)
			if cmd != nil {
				*cmds = append(*cmds, cmd)
			}
			if taskManagerModel, ok := model.(*components.TaskManagerImpl); ok {
				app.taskManager = taskManagerModel
			}
		}
	}
}

// handleTodoEvents handles todo-related events
func (app *ChatApplication) handleTodoEvents(msg tea.Msg, cmds *[]tea.Cmd) {
	switch todoMsg := msg.(type) {
	case domain.TodoUpdateEvent:
		if app.todoBoxView != nil {
			app.todoBoxView.SetTodos(todoMsg.Todos)
			app.stateManager.SetTodos(todoMsg.Todos)
			*cmds = append(*cmds, components.ScheduleAutoCollapse())
		}
	case domain.ToggleTodoBoxEvent:
		if app.todoBoxView != nil {
			app.todoBoxView.Toggle()
		}
	case components.AutoCollapseTickMsg:
		if app.todoBoxView != nil {
			app.todoBoxView.AutoCollapse()
		}
	}
}

// handleAutocompleteEvents handles autocomplete-related events
func (app *ChatApplication) handleAutocompleteEvents(msg tea.Msg, cmds *[]tea.Cmd) {
	if app.autocomplete == nil {
		return
	}

	switch acMsg := msg.(type) {
	case domain.AutocompleteUpdateEvent:
		app.autocomplete.Update(acMsg.Text, acMsg.CursorPos)

		if len(acMsg.Text) > 0 && strings.HasSuffix(acMsg.Text, " ") {
			autocompleteHint := app.autocomplete.GetUsageHint()
			currentInputHint := app.inputView.GetUsageHint()
			if autocompleteHint != "" && currentInputHint != autocompleteHint {
				app.inputView.SetUsageHint(autocompleteHint)
			}
		} else if len(acMsg.Text) > 0 {
			currentHint := app.inputView.GetUsageHint()
			if currentHint != "" {
				app.inputView.SetUsageHint("")
			}
		}

	case domain.AutocompleteHideEvent:
		app.autocomplete.Hide()

	case domain.AutocompleteCompleteEvent:
		if acMsg.Completion != "" {
			app.inputView.SetText(acMsg.Completion)
			if idx := strings.Index(acMsg.Completion, `=""`); idx != -1 {
				app.inputView.SetCursor(idx + 2)
			} else {
				app.inputView.SetCursor(len(acMsg.Completion))
			}

			usageHint := app.autocomplete.GetUsageHint()
			app.inputView.SetUsageHint(usageHint)
		}

		if acMsg.ExecuteImmediately {
			app.autocomplete.Hide()
			app.autocomplete.ClearUsageHint()
			app.inputView.SetUsageHint("")
			*cmds = append(*cmds, app.SendMessage())
			return
		}

		text := app.inputView.GetInput()
		cursor := app.inputView.GetCursor()
		app.autocomplete.Update(text, cursor)

	case domain.RefreshAutocompleteEvent:
		text := app.inputView.GetInput()
		cursor := app.inputView.GetCursor()
		app.autocomplete.Update(text, cursor)
		app.inputView.SetUsageHint("")

	case domain.ClearInputEvent:
		app.autocomplete.Hide()
		app.inputView.SetUsageHint("")
	}
}

// GetServices returns the service container
func (app *ChatApplication) GetConversationRepository() domain.ConversationRepository {
	return app.conversationRepo
}

// GetAgentService returns the agent service
func (app *ChatApplication) GetAgentService() domain.AgentService {
	return app.agentService
}

// GetImageService returns the image service
func (app *ChatApplication) GetImageService() domain.ImageService {
	return app.imageService
}

// GetConfig returns the configuration for keybinding context
func (app *ChatApplication) GetConfig() *config.Config {
	return app.configService
}

// GetConfigDir returns the configuration directory path
func (app *ChatApplication) GetConfigDir() string {
	return app.configDir
}

// GetStateManager returns the current state manager
func (app *ChatApplication) GetStateManager() domain.StateManager {
	return app.stateManager
}

// Additional methods needed by keybinding system

// GetConversationView returns the conversation view
func (app *ChatApplication) GetConversationView() ui.ConversationRenderer {
	return app.conversationView
}

// GetInputView returns the input view
func (app *ChatApplication) GetInputView() ui.InputComponent {
	return app.inputView
}

// GetAutocomplete returns the autocomplete component
func (app *ChatApplication) GetAutocomplete() ui.AutocompleteComponent {
	return app.autocomplete
}

// GetStatusView returns the status view
func (app *ChatApplication) GetStatusView() ui.StatusComponent {
	return app.statusView
}

// GetPageSize returns the current page size for scrolling
func (app *ChatApplication) GetPageSize() int {
	return app.getPageSize()
}

// SendMessage sends the current message
func (app *ChatApplication) SendMessage() tea.Cmd {
	if app.inputView == nil {
		return nil
	}

	input := strings.TrimSpace(app.inputView.GetInput())
	images := app.inputView.GetImageAttachments()

	if input == "" && len(images) == 0 {
		return nil
	}

	if err := app.inputView.AddToHistory(input); err != nil {
		logger.Error("Failed to add input to history", "error", err)
	}

	app.inputView.ClearInput()

	app.conversationView.ResetUserScroll()

	for _, img := range images {
		if img.SourcePath != "" {
			if err := os.Remove(img.SourcePath); err != nil {
				logger.Warn("Failed to clean up temporary image file %s: %v", img.SourcePath, err)
			}
		}
	}

	return func() tea.Msg {
		return domain.UserInputEvent{
			Content: input,
			Images:  images,
		}
	}
}

// ToggleToolResultExpansion toggles tool result expansion
func (app *ChatApplication) ToggleToolResultExpansion() {
	app.toggleToolResultExpansion()
}

// ToggleRawFormat toggles between raw and rendered markdown display
func (app *ChatApplication) ToggleRawFormat() {
	app.conversationView.ToggleRawFormat()
}

// GetMouseEnabled returns the current mouse mode state
func (app *ChatApplication) GetMouseEnabled() bool {
	return app.mouseEnabled
}

// SetMouseEnabled sets the mouse mode state
func (app *ChatApplication) SetMouseEnabled(enabled bool) {
	app.mouseEnabled = enabled
}

func (app *ChatApplication) getCurrentRepo() (string, error) {
	cmd := exec.Command("gh", "repo", "view", "--json", "nameWithOwner", "-q", ".nameWithOwner")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get current repository: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

func (app *ChatApplication) writeWorkflowFile(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

func (app *ChatApplication) isOrgRepo(repo string) (bool, error) {
	parts := strings.Split(repo, "/")
	if len(parts) != 2 {
		return false, fmt.Errorf("invalid repo format: %s", repo)
	}
	owner := parts[0]

	cmd := exec.Command("gh", "api", fmt.Sprintf("/orgs/%s", owner))
	if err := cmd.Run(); err != nil {
		return false, nil
	}
	return true, nil
}

func (app *ChatApplication) checkOrgSecretsExist(orgName string) (bool, error) {
	cmd := exec.Command("gh", "secret", "list", "--org", orgName)
	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("failed to list org secrets: %w", err)
	}

	secrets := string(output)
	hasAppID := strings.Contains(secrets, "INFER_APP_ID")
	hasPrivateKey := strings.Contains(secrets, "INFER_APP_PRIVATE_KEY")

	return hasAppID && hasPrivateKey, nil
}

func (app *ChatApplication) setOrgSecret(orgName, name, value string) error {
	cmd := exec.Command("gh", "secret", "set", name, "--org", orgName, "--visibility", "all", "--body", value)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w", string(output), err)
	}
	return nil
}

func (app *ChatApplication) generateStandardWorkflowContent() string {
	return `---
name: Infer

on:
  issues:
    types:
      - opened
      - edited
  issue_comment:
    types:
      - created

permissions:
  issues: write
  contents: write
  pull-requests: write

jobs:
  infer:
    runs-on: ubuntu-24.04
    steps:
      - name: Checkout repository
        uses: actions/checkout@v5

      - name: Run Infer Agent
        uses: inference-gateway/infer-action@main
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
          trigger-phrase: "@infer"
          model: deepseek/deepseek-chat
          max-turns: 50
          anthropic-api-key: ${{ secrets.ANTHROPIC_API_KEY }}
          openai-api-key: ${{ secrets.OPENAI_API_KEY }}
          google-api-key: ${{ secrets.GOOGLE_API_KEY }}
          deepseek-api-key: ${{ secrets.DEEPSEEK_API_KEY }}
          groq-api-key: ${{ secrets.GROQ_API_KEY }}
          mistral-api-key: ${{ secrets.MISTRAL_API_KEY }}
          cloudflare-api-key: ${{ secrets.CLOUDFLARE_API_KEY }}
          cohere-api-key: ${{ secrets.COHERE_API_KEY }}
          ollama-api-key: ${{ secrets.OLLAMA_API_KEY }}
          ollama-cloud-api-key: ${{ secrets.OLLAMA_CLOUD_API_KEY }}
`
}

func (app *ChatApplication) generateGithubActionWorkflowContent() string {
	return `---
name: Infer

on:
  issues:
    types:
      - opened
      - edited
  issue_comment:
    types:
      - created

permissions:
  issues: write
  contents: write
  pull-requests: write

jobs:
  infer:
    runs-on: ubuntu-24.04
    steps:
      - name: Generate GitHub App Token
        uses: actions/create-github-app-token@v2.2.0
        id: app_token
        with:
          app-id: ${{ secrets.INFER_APP_ID }}
          private-key: ${{ secrets.INFER_APP_PRIVATE_KEY }}
          owner: ${{ github.repository_owner }}
          repositories: |
            ${{ github.event.repository.name }}

      - name: Checkout repository
        uses: actions/checkout@v5
        with:
          token: ${{ steps.app_token.outputs.token }}

      - name: Run Infer Agent
        uses: inference-gateway/infer-action@v0.3.1
        with:
          github-token: ${{ steps.app_token.outputs.token }}
          trigger-phrase: "@infer"
          model: deepseek/deepseek-chat
          max-turns: 50
          anthropic-api-key: ${{ secrets.ANTHROPIC_API_KEY }}
          openai-api-key: ${{ secrets.OPENAI_API_KEY }}
          google-api-key: ${{ secrets.GOOGLE_API_KEY }}
          deepseek-api-key: ${{ secrets.DEEPSEEK_API_KEY }}
          groq-api-key: ${{ secrets.GROQ_API_KEY }}
          mistral-api-key: ${{ secrets.MISTRAL_API_KEY }}
          cloudflare-api-key: ${{ secrets.CLOUDFLARE_API_KEY }}
          cohere-api-key: ${{ secrets.COHERE_API_KEY }}
          ollama-api-key: ${{ secrets.OLLAMA_API_KEY }}
          ollama-cloud-api-key: ${{ secrets.OLLAMA_CLOUD_API_KEY }}
`
}

func (app *ChatApplication) preparePRCreation(repo, workflowPath string) (string, error) {
	cmd := exec.Command("git", "symbolic-ref", "refs/remotes/origin/HEAD")
	output, err := cmd.Output()
	var baseBranch string
	if err == nil {
		parts := strings.Split(strings.TrimSpace(string(output)), "/")
		if len(parts) > 0 {
			baseBranch = parts[len(parts)-1]
		}
	}
	if baseBranch == "" {
		baseBranch = "main"
	}

	cmd = exec.Command("git", "branch", "--show-current")
	currentBranch, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get current branch: %w", err)
	}
	branch := strings.TrimSpace(string(currentBranch))

	if branch == "main" || branch == "master" {
		baseBranch = branch
		branch = "ci/setup-infer-github-action"
		cmd = exec.Command("git", "checkout", "-b", branch)
		if output, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("failed to create branch: %s: %w", string(output), err)
		}
	}

	cmd = exec.Command("git", "add", workflowPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to add file: %s: %w", string(output), err)
	}

	cmd = exec.Command("git", "commit", "-m", "feat(ci): Setup infer workflow")
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to commit: %s: %w", string(output), err)
	}

	cmd = exec.Command("git", "push", "-u", "origin", branch)
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to push: %s: %w", string(output), err)
	}

	title := "feat(ci): Setup infer workflow"
	body := `## Summary

This PR sets up the infer workflow for automated code review and assistance.

## Changes

- Added infer workflow configuration
- Configured to trigger on @infer mentions in issues

## Testing

After merging, @infer mentions in issues will trigger the bot.

🤖 Generated with infer`

	cmd = exec.Command("gh", "pr", "create",
		"--base", baseBranch,
		"--head", branch,
		"--title", title,
		"--body", body,
		"--web")

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("failed to open PR creation page: %w", err)
	}

	return fmt.Sprintf("https://github.com/%s/compare/%s...%s", repo, baseBranch, branch), nil
}

// PrintConversationHistory outputs the full conversation history to stdout
// This is called when the application exits to preserve the chat session
func (app *ChatApplication) PrintConversationHistory() {
	entries := app.conversationRepo.GetMessages()
	if len(entries) == 0 {
		return
	}

	if conversationView, ok := app.conversationView.(*components.ConversationView); ok {
		plainTextLines := conversationView.GetPlainTextLines()
		for _, line := range plainTextLines {
			fmt.Println(line)
		}
	}
}
