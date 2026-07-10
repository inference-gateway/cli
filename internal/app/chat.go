package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	tools "github.com/inference-gateway/cli/internal/agent/tools"
	constants "github.com/inference-gateway/cli/internal/constants"
	domain "github.com/inference-gateway/cli/internal/domain"
	formatting "github.com/inference-gateway/cli/internal/formatting"
	handlers "github.com/inference-gateway/cli/internal/handlers"
	adapters "github.com/inference-gateway/cli/internal/infra/adapters"
	logger "github.com/inference-gateway/cli/internal/logger"
	services "github.com/inference-gateway/cli/internal/services"
	gitdiff "github.com/inference-gateway/cli/internal/services/gitdiff"
	shortcuts "github.com/inference-gateway/cli/internal/shortcuts"
	ui "github.com/inference-gateway/cli/internal/ui"
	autocomplete "github.com/inference-gateway/cli/internal/ui/autocomplete"
	components "github.com/inference-gateway/cli/internal/ui/components"
	factory "github.com/inference-gateway/cli/internal/ui/components/factory"
	keybinding "github.com/inference-gateway/cli/internal/ui/keybinding"
	keys "github.com/inference-gateway/cli/internal/ui/keys"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

// actChatFocusAttachments is the chat-namespace action that moves key focus to
// the snippet attachments tree below the input.
var actChatFocusAttachments = config.ActionID(config.NamespaceChat, "focus_attachments")

// ChatApplication represents the main application model using state management
type ChatApplication struct {
	// Dependencies
	config                 *config.Config
	agentService           domain.AgentService
	conversationRepo       domain.ConversationRepository
	conversationOptimizer  domain.ConversationOptimizer
	sessionRolloverManager *services.SessionRolloverManager
	modelService           domain.ModelService
	toolService            domain.ToolService
	fileService            domain.FileService
	imageService           domain.ImageService
	skillsService          domain.SkillsService
	githubIssueService     domain.GitHubIssueService
	pricingService         domain.PricingService
	shortcutRegistry       *shortcuts.Registry
	themeService           domain.ThemeService
	toolRegistry           *tools.Registry
	mcpManager             domain.MCPManager
	taskRetentionService   domain.TaskRetentionService
	backgroundTaskService  domain.BackgroundTaskService
	backgroundTaskRegistry domain.BackgroundTaskRegistry

	// Chat orchestration services
	a2aTaskCoordinator       domain.A2ATaskCoordinator
	approvalCoordinator      domain.ApprovalCoordinator
	chatCompletionRunner     domain.ChatCompletionRunner
	directExecutionService   domain.DirectExecutionService
	toolExecutionCoordinator domain.ToolExecutionCoordinator

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
	approvalBoxView      *components.ApprovalBoxView
	questionFormView     *components.QuestionFormView
	modelSelector        *components.ModelSelectorImpl
	themeSelector        *components.ThemeSelectorImpl
	conversationSelector *components.ConversationSelectorImpl
	fileSelectionView    *components.FileSelectionView
	taskManager          *components.TaskManagerImpl
	toolCallRenderer     *components.ToolCallRenderer
	initGithubActionView *components.InitGithubActionView
	diffViewer           *components.DiffViewerImpl
	fileExplorer         *components.FileExplorerImpl
	helpView             *components.HelpViewImpl
	toolsView            *components.ToolsViewImpl
	a2aAgentsView        *components.A2AAgentsViewImpl

	snippetAttachmentsView *components.SnippetAttachmentsView

	// Presentation layer
	applicationViewRenderer *components.ApplicationViewRenderer
	fileSelectionHandler    *components.FileSelectionHandler

	// Event handling
	chatHandler           domain.ChatHandler
	messageHistoryHandler *handlers.MessageHistoryHandler

	// Current active component for key handling
	focusedComponent ui.InputComponent

	// Pending snippet attachments captured in the file explorer, shown as a tree
	// below the input and sent (then cleared) with the next chat message.
	pendingSnippets    []components.SnippetSelection
	attachmentsFocused bool

	// Keyboard focus on the status-indicator row below the input, entered
	// with arrow-down when input-history navigation is idle.
	statusBarFocused bool

	// Key binding system
	keyBindingManager *keybinding.KeyBindingManager

	// Resolved chat-namespace keybindings (actionID -> keys), used for the
	// snippet-attachments focus shim that runs ahead of the key binding manager.
	chatKeys map[string][]string

	// Track last key handled by keybinding action to prevent double-handling
	lastHandledKey string

	// Available models
	availableModels []string

	// Configuration
	configDir string
}

// nolint: funlen // NewChatApplication creates a new chat application
func NewChatApplication(
	cfg *config.Config,
	models []string,
	defaultModel string,
	versionInfo domain.VersionInfo,
	agentManager domain.AgentManager,
	agentService domain.AgentService,
	backgroundTaskService domain.BackgroundTaskService,
	backgroundTaskRegistry domain.BackgroundTaskRegistry,
	conversationOptimizer domain.ConversationOptimizer,
	conversationRepo domain.ConversationRepository,
	fileService domain.FileService,
	imageService domain.ImageService,
	skillsService domain.SkillsService,
	githubIssueService domain.GitHubIssueService,
	mcpManager domain.MCPManager,
	messageQueue domain.MessageQueue,
	modelService domain.ModelService,
	pricingService domain.PricingService,
	sessionRolloverManager *services.SessionRolloverManager,
	stateManager domain.StateManager,
	taskRetentionService domain.TaskRetentionService,
	themeService domain.ThemeService,
	toolService domain.ToolService,
	shortcutRegistry *shortcuts.Registry,
	toolRegistry *tools.Registry,
	a2aTaskCoordinator domain.A2ATaskCoordinator,
	approvalCoordinator domain.ApprovalCoordinator,
	chatCompletionRunner domain.ChatCompletionRunner,
	directExecutionService domain.DirectExecutionService,
	toolExecutionCoordinator domain.ToolExecutionCoordinator,
) *ChatApplication {
	initialView := domain.ViewStateModelSelection
	if defaultModel != "" {
		initialView = domain.ViewStateChat
	}

	app := &ChatApplication{
		agentService:             agentService,
		conversationRepo:         conversationRepo,
		conversationOptimizer:    conversationOptimizer,
		sessionRolloverManager:   sessionRolloverManager,
		modelService:             modelService,
		config:                   cfg,
		toolService:              toolService,
		fileService:              fileService,
		imageService:             imageService,
		skillsService:            skillsService,
		githubIssueService:       githubIssueService,
		pricingService:           pricingService,
		shortcutRegistry:         shortcutRegistry,
		themeService:             themeService,
		toolRegistry:             toolRegistry,
		mcpManager:               mcpManager,
		taskRetentionService:     taskRetentionService,
		backgroundTaskService:    backgroundTaskService,
		backgroundTaskRegistry:   backgroundTaskRegistry,
		a2aTaskCoordinator:       a2aTaskCoordinator,
		approvalCoordinator:      approvalCoordinator,
		chatCompletionRunner:     chatCompletionRunner,
		directExecutionService:   directExecutionService,
		toolExecutionCoordinator: toolExecutionCoordinator,
		availableModels:          models,
		stateManager:             stateManager,
		messageQueue:             messageQueue,
		mouseEnabled:             true,
	}

	if err := app.stateManager.TransitionToView(initialView); err != nil {
		logger.Error("failed to transition to initial view", "error", err)
	}

	styleProvider := styles.NewProvider(app.themeService)

	app.toolCallRenderer = components.NewToolCallRenderer(styleProvider)
	app.conversationView = factory.CreateConversationView(app.themeService)
	toolFormatterService := services.NewToolFormatterService(app.toolRegistry, styleProvider)

	configDir := cfg.GetConfigDir()
	app.configDir = configDir

	if cv, ok := app.conversationView.(*components.ConversationView); ok {
		cv.SetToolFormatter(toolFormatterService)
		cv.SetConfigPath(filepath.Join(configDir, config.ConfigFileName))
		cv.SetVersionInfo(versionInfo)
		cv.SetToolCallRenderer(app.toolCallRenderer)
		cv.SetStateManager(app.stateManager)
		cv.SetAgentNameResolver(buildAgentNameResolver())
		cv.SetAgentModelResolver(buildAgentModelResolver())
	}

	historyName := os.Getenv(domain.EnvSubagentHistoryName)
	app.inputView = factory.CreateInputViewWithName(app.modelService, configDir, historyName)
	if iv, ok := app.inputView.(*components.InputView); ok {
		iv.SetThemeService(app.themeService)
		iv.SetStateManager(app.stateManager)
		iv.SetImageService(app.imageService)
		iv.SetConfig(app.config)
		iv.SetConversationRepo(app.conversationRepo)
		iv.SetSkillsService(app.skillsService)
		iv.SetShortcutRegistry(app.shortcutRegistry)
		iv.SetFileService(app.fileService)
		iv.SetGitHubIssueService(app.githubIssueService)
		iv.SetMessageQueue(app.messageQueue)
	}

	app.autocomplete = factory.CreateAutocomplete(app.shortcutRegistry, app.toolService, app.modelService, app.pricingService, app.skillsService, app.githubIssueService)
	if ac, ok := app.autocomplete.(*autocomplete.AutocompleteImpl); ok {
		ac.SetStateManager(app.stateManager)
	}

	app.inputStatusBar = factory.CreateInputStatusBar(app.themeService)
	if isb, ok := app.inputStatusBar.(*components.InputStatusBar); ok {
		isb.SetModelService(app.modelService)
		isb.SetThemeService(app.themeService)
		isb.SetStateManager(app.stateManager)
		isb.SetConfig(app.config)
		isb.SetConversationRepo(app.conversationRepo)
		isb.SetToolService(app.toolService)
		isb.SetTokenEstimator(services.NewTokenizerService(services.DefaultTokenizerConfig()))
		isb.SetBackgroundShellService(app.toolRegistry.GetBackgroundShellService())
		isb.SetBackgroundTaskService(app.backgroundTaskService)
		if app.backgroundTaskRegistry != nil {
			isb.SetBackgroundTaskRegistry(app.backgroundTaskRegistry)
		}
	}

	app.statusView = factory.CreateStatusView(app.themeService)
	app.modeIndicator = components.NewModeIndicator(styleProvider)
	app.modeIndicator.SetStateManager(app.stateManager)
	app.helpBar = factory.CreateHelpBar(app.themeService)
	app.helpView = components.NewHelpView(app.themeService, styleProvider)
	app.queueBoxView = components.NewQueueBoxView(styleProvider)
	app.todoBoxView = components.NewTodoBoxView(styleProvider)
	app.snippetAttachmentsView = components.NewSnippetAttachmentsView(styleProvider)
	app.chatKeys = config.ResolveNamespaceBindings(app.config.Chat.Keybindings, config.NamespaceChat)
	app.approvalBoxView = components.NewApprovalBoxView(styleProvider, app.stateManager, toolFormatterService)
	app.questionFormView = components.NewQuestionFormView(styleProvider, app.stateManager)

	app.fileSelectionView = components.NewFileSelectionView(styleProvider)

	app.applicationViewRenderer = components.NewApplicationViewRenderer(styleProvider)
	app.fileSelectionHandler = components.NewFileSelectionHandler(styleProvider)

	app.keyBindingManager = keybinding.NewKeyBindingManager(app, app.config)
	app.updateHelpBarShortcuts()

	keyHintFormatter := app.keyBindingManager.GetHintFormatter()
	toolFormatterService.SetHintFormatter(keyHintFormatter)
	if cv, ok := app.conversationView.(*components.ConversationView); ok {
		cv.SetKeyHintFormatter(keyHintFormatter)
	}
	if sv, ok := app.statusView.(*components.StatusView); ok {
		sv.SetKeyHintFormatter(keyHintFormatter)
	}

	app.toolCallRenderer.SetKeyHintFormatter(keyHintFormatter)
	app.modelSelector = components.NewModelSelector(models, app.modelService, app.pricingService, app.config, styleProvider)
	app.themeSelector = components.NewThemeSelector(app.themeService, styleProvider)
	app.toolsView = components.NewToolsView(app.toolService, app.stateManager, styleProvider)
	app.a2aAgentsView = components.NewA2AAgentsView(app.stateManager, styleProvider)
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
		app.sessionRolloverManager,
		app.modelService,
		app.toolService,
		app.fileService,
		app.imageService,
		app.skillsService,
		app.githubIssueService,
		app.shortcutRegistry,
		app.stateManager,
		messageQueue,
		app.taskRetentionService,
		app.backgroundTaskService,
		app.toolRegistry.GetBackgroundShellService(),
		agentManager,
		app.config,
		app.a2aTaskCoordinator,
		app.approvalCoordinator,
		app.chatCompletionRunner,
		app.directExecutionService,
		app.toolExecutionCoordinator,
	)

	app.messageHistoryHandler = handlers.NewMessageHistoryHandler(
		app.stateManager,
		app.conversationRepo,
	)

	return app
}

// updateHelpBarShortcuts updates the help bar with essential keyboard shortcuts
func (app *ChatApplication) updateHelpBarShortcuts() {
	app.helpBar.SetShortcuts(app.collectKeyShortcuts())
}

// collectKeyShortcuts gathers the input-prefix hints and the active keybinding
// shortcuts into a single list, shared by the help bar and the /help overlay.
func (app *ChatApplication) collectKeyShortcuts() []ui.KeyShortcut {
	shortcuts := []ui.KeyShortcut{
		{Key: "!", Description: "for bash mode"},
		{Key: "!!", Description: "for tools mode"},
		{Key: "/", Description: "for shortcuts"},
		{Key: "@", Description: "for file paths"},
		{Key: "#", Description: "for github issues"},
	}

	if app.keyBindingManager != nil {
		for _, kbShortcut := range app.keyBindingManager.GetHelpShortcuts() {
			shortcuts = append(shortcuts, ui.KeyShortcut{
				Key:         kbShortcut.Key,
				Description: kbShortcut.Description,
			})
		}
	}

	return shortcuts
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

	if app.mcpManager != nil {
		app.inputStatusBar.UpdateMCPStatus(&domain.MCPServerStatus{
			TotalServers:     app.mcpManager.GetTotalServers(),
			ConnectedServers: 0,
			TotalTools:       0,
		})

		app.mcpManager.StartMonitoring(context.Background())
	}

	if msgs := app.conversationRepo.GetMessages(); len(msgs) > 0 {
		cmds = append(cmds, func() tea.Msg {
			return domain.UpdateHistoryEvent{History: msgs}
		})
	}

	return tea.Batch(cmds...)
}

// Update handles all application messages using the state management system. It
// is the single ingress for every message - background producers push through the
// UI notifier (program.Send), so this is the one place to measure handler
// duration: a slow handler in the single-threaded loop is a visible UI freeze.
func (app *ChatApplication) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	start := time.Now()
	defer logSlowUpdate(start, msg)

	viewBefore := app.stateManager.GetCurrentView()

	var cmds []tea.Cmd

	if cmd := app.handleAppEvents(msg); cmd != nil {
		cmds = append(cmds, cmd)
	}

	if isDomainEvent(msg) {
		if cmd := app.chatHandler.Handle(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	cmds = append(cmds, app.handleViewSpecificMessages(msg)...)

	cmds = append(cmds, app.updateUIComponentsForUIMessages(msg)...)

	if event, ok := msg.(domain.MCPServerStatusUpdateEvent); ok {
		if cmd := app.handleMCPStatusUpdate(event); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	if viewBefore != domain.ViewStateChat &&
		app.stateManager.GetCurrentView() == domain.ViewStateChat &&
		!app.messageQueue.IsEmpty() {
		cmds = append(cmds, func() tea.Msg { return domain.DrainQueueEvent{} })
	}

	return app, tea.Batch(cmds...)
}

// logSlowUpdate warns when a single Update dispatch took longer than
// SlowUpdateThreshold. The Update loop is single-threaded, so a slow handler is a
// visible UI freeze - the one ingress is the one place worth measuring.
func logSlowUpdate(start time.Time, msg tea.Msg) {
	if d := time.Since(start); d > constants.SlowUpdateThreshold {
		logger.Warn("slow update", "event", fmt.Sprintf("%T", msg), "ms", d.Milliseconds())
	}
}

// isDomainEvent checks if an event should be handled by ChatHandler (positive filtering).
// This replaces the negative filtering pattern (isUIOnlyEvent) with an explicit declaration
// of what ChatHandler SHOULD handle, not what it shouldn't.
func isDomainEvent(msg tea.Msg) bool {
	switch msg.(type) {
	// User input and interaction
	case domain.UserInputEvent,
		domain.FileSelectionRequestEvent,
		domain.ConversationSelectedEvent:
		return true

	// Chat lifecycle
	case domain.ChatStartEvent,
		domain.ChatChunkEvent,
		domain.ChatCompleteEvent,
		domain.ChatErrorEvent,
		domain.OptimizationStatusEvent,
		domain.RolloverCompletedEvent:
		return true

	// Tool execution
	case domain.ToolCallUpdateEvent,
		domain.ToolCallReadyEvent,
		domain.ToolExecutionStartedEvent,
		domain.ToolExecutionProgressEvent,
		domain.ToolExecutionCompletedEvent:
		return true

	// Tool and plan approval
	case domain.ToolApprovalRequestedEvent,
		domain.ToolApprovalResponseEvent,
		domain.PlanApprovalRequestedEvent,
		domain.PlanApprovalResponseEvent,
		domain.UserQuestionRequestedEvent:
		return true

	// Bash command execution
	case domain.BashOutputChunkEvent,
		domain.BashCommandCompletedEvent,
		domain.BackgroundShellRequestEvent:
		return true

	// A2A (Agent-to-Agent) task management
	case domain.A2AToolCallExecutedEvent,
		domain.A2ATaskSubmittedEvent,
		domain.A2ATaskStatusUpdateEvent,
		domain.A2ATaskCompletedEvent,
		domain.A2ATaskFailedEvent,
		domain.A2ATaskInputRequiredEvent:
		return true

	case domain.SubagentSubmittedEvent,
		domain.SubagentCompletedEvent,
		domain.SubagentFailedEvent:
		return true

	case domain.MessageQueuedEvent,
		domain.ToolCancelledEvent,
		domain.TodoUpdateChatEvent,
		domain.AgentStatusUpdateEvent,
		domain.DrainQueueEvent,
		domain.DrainQueueRetryEvent,
		domain.NavigateBackInTimeEvent,
		domain.MessageHistoryRestoreEvent,
		domain.ComputerUsePausedEvent,
		domain.ComputerUseResumedEvent:
		return true
	}

	return false
}

// handleAppEvents handles application-level events (not component-specific)
func (app *ChatApplication) handleAppEvents(msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case domain.TriggerGithubActionSetupEvent:
		return tea.Batch(app.handleGithubActionSetupTrigger()...)

	case githubSetupCheckedMsg:
		return tea.Batch(app.handleGithubSetupChecked(m)...)

	case domain.TriggerHelpViewEvent:
		return tea.Batch(app.handleHelpViewTrigger()...)

	case domain.MessageHistoryRestoreEvent:
		return app.messageHistoryHandler.HandleRestore(m)

	}

	return nil
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
		app.toolRegistry.RegisterMCPServerTools(event.ServerName, event.Tools)
	}

	if !event.Connected {
		app.toolRegistry.UnregisterMCPServerTools(event.ServerName)
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

	inHistoryMode := false
	if cv, ok := app.conversationView.(*components.ConversationView); ok {
		inHistoryMode = cv.IsInMessageHistoryMode()
	}

	inputBlocked := app.stateManager.GetApprovalUIState() != nil || app.stateManager.GetPlanApprovalUIState() != nil ||
		app.stateManager.GetUserQuestionUIState() != nil ||
		inHistoryMode || currentView == domain.ViewStateDiffViewer ||
		currentView == domain.ViewStateExplorer || currentView == domain.ViewStateHelp

	if inputView, ok := app.inputView.(*components.InputView); ok {
		inputView.SetDisabled(inputBlocked)
	}

	if app.statusBarFocused && (inputBlocked || currentView != domain.ViewStateChat) {
		app.blurStatusBar()
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
	case domain.ViewStateDiffViewer:
		return app.handleDiffViewerView(msg)
	case domain.ViewStateExplorer:
		return app.handleExplorerView(msg)
	case domain.ViewStateHelp:
		return app.handleHelpView(msg)
	case domain.ViewStateToolsList:
		return app.handleToolsListView(msg)
	case domain.ViewStateA2AAgents:
		return app.handleA2AAgentsView(msg)
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

	if navEvent, ok := msg.(domain.NavigateBackInTimeEvent); ok {
		return app.handleNavigateBackInTime(navEvent)
	}

	if readyEvent, ok := msg.(domain.MessageHistoryReadyEvent); ok {
		if cv, ok := app.conversationView.(*components.ConversationView); ok {
			cv.EnterMessageHistoryMode(readyEvent.Messages)

			if iv, ok := app.inputView.(*components.InputView); ok {
				iv.SetCustomHint("Input paused - use ↑/↓ to navigate, enter to restore, esc to cancel")
			}
		}
		return cmds
	}

	if editReadyEvent, ok := msg.(domain.MessageHistoryEditReadyEvent); ok {
		return app.handleEditReady(editReadyEvent)
	}

	if editSubmitEvent, ok := msg.(domain.MessageEditSubmitEvent); ok {
		if cmd := app.messageHistoryHandler.HandleEditSubmit(editSubmitEvent); cmd != nil {
			cmds = append(cmds, cmd)
		}
		return cmds
	}

	if _, ok := msg.(domain.FocusStatusBarEvent); ok {
		if app.inputStatusBar.Focus() {
			app.statusBarFocused = true
		}
		return cmds
	}

	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return cmds
	}

	return app.handleChatViewKeyPress(keyMsg)
}

func (app *ChatApplication) handleChatViewKeyPress(keyMsg tea.KeyPressMsg) []tea.Cmd {
	var cmds []tea.Cmd

	// While an AskUserQuestion form is up it captures all keys (like the
	// tool-approval box). It floats over the chat, so the view stays Chat.
	// ctrl+c falls through so the user can still cancel the whole turn.
	if app.stateManager.GetUserQuestionUIState() != nil && keyMsg.String() != "ctrl+c" {
		return app.handleUserQuestionKeys(keyMsg)
	}

	if cv, ok := app.conversationView.(*components.ConversationView); ok && cv.IsInMessageHistoryMode() {
		return app.handleMessageHistoryKeys(keyMsg)
	}

	if app.attachmentsFocused && keyMsg.String() != "ctrl+c" {
		return app.handleAttachmentsKeys(keyMsg)
	}
	if app.statusBarFocused && keyMsg.String() != "ctrl+c" {
		if cmds, handled := app.handleStatusBarKeys(keyMsg); handled {
			return cmds
		}
	}
	if !app.attachmentsFocused && len(app.pendingSnippets) > 0 && app.matchesFocusAttachments(keyMsg) {
		app.attachmentsFocused = true
		return nil
	}

	isHandledByAction := app.keyBindingManager.IsKeyHandledByAction(keyMsg)

	if cmd := app.keyBindingManager.ProcessKey(keyMsg); cmd != nil {
		cmds = append(cmds, cmd)
	}

	if isHandledByAction && app.keyBindingManager.ShouldSkipInputUpdate(keyMsg) {
		app.lastHandledKey = keyMsg.String()
	}

	return cmds
}

// matchesFocusAttachments reports whether the pressed key is bound to the
// chat-namespace focus-attachments action.
func (app *ChatApplication) matchesFocusAttachments(keyMsg tea.KeyPressMsg) bool {
	return slices.Contains(app.chatKeys[actChatFocusAttachments], keyMsg.String())
}

// handleAttachmentsKeys interprets keys while the snippet attachments tree holds
// focus: navigate, remove one, clear all, or leave. All keys are consumed.
func (app *ChatApplication) handleAttachmentsKeys(keyMsg tea.KeyPressMsg) []tea.Cmd {
	if app.matchesFocusAttachments(keyMsg) {
		app.attachmentsFocused = false
		return nil
	}
	switch keyMsg.String() {
	case "up", "k":
		app.snippetAttachmentsView.MoveCursor(-1)
	case "down", "j":
		app.snippetAttachmentsView.MoveCursor(1)
	case "d", "x", "backspace", "delete":
		app.removeFocusedSnippet()
	case "c":
		app.pendingSnippets = nil
		app.attachmentsFocused = false
	case "esc", "q":
		app.attachmentsFocused = false
	}
	return nil
}

// removeFocusedSnippet drops the snippet under the tree cursor, leaving focus
// only while attachments remain.
func (app *ChatApplication) removeFocusedSnippet() {
	idx := app.snippetAttachmentsView.SelectedIndex()
	if idx < 0 || idx >= len(app.pendingSnippets) {
		return
	}
	app.pendingSnippets = append(app.pendingSnippets[:idx], app.pendingSnippets[idx+1:]...)
	if len(app.pendingSnippets) == 0 {
		app.attachmentsFocused = false
	}
}

// handleStatusBarKeys interprets keys while the status-indicator row holds
// focus. Unhandled keys blur the row and report handled=false so they flow
// on to the normal chain - typing lands back in the input.
func (app *ChatApplication) handleStatusBarKeys(keyMsg tea.KeyPressMsg) ([]tea.Cmd, bool) {
	switch keyMsg.String() {
	case "left", "shift+tab":
		app.inputStatusBar.SelectPrev()
		return nil, true
	case "right", "tab":
		app.inputStatusBar.SelectNext()
		return nil, true
	case "down":
		return nil, true
	case "up", "esc":
		app.blurStatusBar()
		return nil, true
	case "enter":
		return app.activateSelectedIndicator(), true
	default:
		app.blurStatusBar()
		return nil, false
	}
}

// blurStatusBar returns keyboard focus from the indicator row to the input.
func (app *ChatApplication) blurStatusBar() {
	app.statusBarFocused = false
	app.inputStatusBar.Blur()
}

// activateSelectedIndicator opens the view behind the selected indicator,
// mirroring the /model and /tasks shortcut side effects. The task view is
// not gated on A2A - it shows shells and subagents too.
func (app *ChatApplication) activateSelectedIndicator() []tea.Cmd {
	action := app.inputStatusBar.SelectedAction()
	app.blurStatusBar()

	switch action {
	case ui.StatusIndicatorActionModelSelection:
		_ = app.stateManager.TransitionToView(domain.ViewStateModelSelection)
		return []tea.Cmd{func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    "Select a model from the dropdown",
				Spinner:    false,
				StatusType: domain.StatusDefault,
			}
		}}
	case ui.StatusIndicatorActionThemeSelection:
		_ = app.stateManager.TransitionToView(domain.ViewStateThemeSelection)
		return []tea.Cmd{func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    "",
				Spinner:    false,
				StatusType: domain.StatusDefault,
			}
		}}
	case ui.StatusIndicatorActionToolsList:
		_ = app.stateManager.TransitionToView(domain.ViewStateToolsList)
		return []tea.Cmd{func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    "",
				Spinner:    false,
				StatusType: domain.StatusDefault,
			}
		}}
	case ui.StatusIndicatorActionA2AAgents:
		_ = app.stateManager.TransitionToView(domain.ViewStateA2AAgents)
		return []tea.Cmd{func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    "",
				Spinner:    false,
				StatusType: domain.StatusDefault,
			}
		}}
	case ui.StatusIndicatorActionTaskManagement:
		if err := app.stateManager.TransitionToView(domain.ViewStateA2ATaskManagement); err != nil {
			return []tea.Cmd{func() tea.Msg {
				return domain.ShowErrorEvent{
					Error:  fmt.Sprintf("Failed to show task management: %v", err),
					Sticky: false,
				}
			}}
		}
		hasBackgroundTasks := false
		if app.backgroundTaskService != nil {
			hasBackgroundTasks = len(app.backgroundTaskService.GetBackgroundTasks()) > 0
		}
		return []tea.Cmd{func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    "Task management interface",
				Spinner:    hasBackgroundTasks,
				StatusType: domain.StatusDefault,
			}
		}}
	default:
		return nil
	}
}

// handleUserQuestionKeys drives the AskUserQuestion floating form. It mutates
// the form state in place; Bubble Tea re-renders after each key, so no command
// is needed. On the final question's confirm it sends the answers on the
// response channel (unblocking the tool); esc cancels (closing the channel
// without a value, which the tool reads as "dismissed").
func (app *ChatApplication) handleUserQuestionKeys(keyMsg tea.KeyPressMsg) []tea.Cmd {
	sm := app.stateManager
	q := sm.GetUserQuestionUIState()
	if q == nil {
		return nil
	}

	// Free-text entry on the "Other" row consumes most keys as input.
	if q.OtherActive[q.CurrentIndex] {
		app.handleUserQuestionOtherKey(keyMsg)
		return nil
	}

	switch keyMsg.String() {
	case "up", "k":
		app.moveUserQuestionCursor(-1)
	case "down", "j":
		app.moveUserQuestionCursor(1)
	case " ", "space":
		app.toggleUserQuestionAtCursor()
	case "enter":
		app.confirmUserQuestion()
	case "esc":
		sm.ClearUserQuestionUIState()
	}
	return nil
}

// handleUserQuestionOtherKey edits the free-text "Other" buffer for the current
// question. Enter confirms (and advances/submits); esc leaves text entry.
func (app *ChatApplication) handleUserQuestionOtherKey(keyMsg tea.KeyPressMsg) {
	sm := app.stateManager
	switch keyMsg.String() {
	case "enter":
		app.confirmUserQuestion()
	case "esc":
		sm.SetUserQuestionOtherActive(false)
	case "backspace":
		sm.BackspaceUserQuestionOtherText()
	default:
		if text := keys.PrintableText(keyMsg); text != "" {
			sm.AppendUserQuestionOtherText(text)
		}
	}
}

// moveUserQuestionCursor moves the option highlight with wrap-around over the
// real options plus the trailing "Other" row.
func (app *ChatApplication) moveUserQuestionCursor(delta int) {
	q := app.stateManager.GetUserQuestionUIState()
	if q == nil {
		return
	}
	rows := q.OtherRowIndex() + 1
	app.stateManager.SetUserQuestionOptionCursor(((q.OptionCursor+delta)%rows + rows) % rows)
}

// toggleUserQuestionAtCursor toggles the highlighted option, or starts free-text
// entry when the cursor is on the "Other" row.
func (app *ChatApplication) toggleUserQuestionAtCursor() {
	q := app.stateManager.GetUserQuestionUIState()
	if q == nil {
		return
	}
	if q.OnOtherRow() {
		app.stateManager.SetUserQuestionOtherActive(true)
		return
	}

	if q.Questions[q.CurrentIndex].MultiSelect {
		app.stateManager.ToggleUserQuestionOption(q.OptionCursor)
	}
}

// confirmUserQuestion advances to the next question or, on the last one, submits
// all answers (the current single-select choice already follows the cursor).
func (app *ChatApplication) confirmUserQuestion() {
	sm := app.stateManager
	q := sm.GetUserQuestionUIState()
	if q == nil {
		return
	}

	// Enter on the "Other" row starts free-text entry rather than advancing.
	if q.OnOtherRow() && !q.OtherActive[q.CurrentIndex] {
		sm.SetUserQuestionOtherActive(true)
		return
	}

	if !userQuestionAnswered(q) {
		return
	}

	if !sm.AdvanceUserQuestion() {
		return // more questions remain; the next one renders
	}

	answers := sm.BuildUserQuestionAnswers()
	if q.ResponseChan != nil {
		q.ResponseChan <- answers
	}
	sm.ClearUserQuestionUIState()
}

// userQuestionAnswered reports whether the current question has a selection or
// non-empty Other text.
func userQuestionAnswered(q *domain.UserQuestionUIState) bool {
	i := q.CurrentIndex
	if i < 0 || i >= len(q.Questions) {
		return false
	}
	return len(q.Selected[i]) > 0 || strings.TrimSpace(q.OtherText[i]) != ""
}

func (app *ChatApplication) handleFileSelectionView(msg tea.Msg) []tea.Cmd {
	var cmds []tea.Cmd

	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		if cmd := app.handleFileSelectionKeys(keyMsg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return cmds
}

// View renders the current application view using state management.
// Bubble Tea v2 expects tea.View; viewContent keeps the original
// string-composition logic and View wraps it. MouseMode is read from
// the app's mouse-enabled state on every render so the ctrl+s toggle
// actually takes effect - without this, no mouse/wheel events arrive.
func (app *ChatApplication) View() tea.View {
	v := tea.NewView(app.viewContent())
	if app.mouseEnabled {
		v.MouseMode = tea.MouseModeCellMotion
	}
	v.AltScreen = true
	return v
}

func (app *ChatApplication) viewContent() string {
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
	case domain.ViewStateDiffViewer:
		return app.renderDiffViewer()
	case domain.ViewStateExplorer:
		return app.renderExplorer()
	case domain.ViewStateHelp:
		return app.renderHelp()
	case domain.ViewStateToolsList:
		return app.renderToolsList()
	case domain.ViewStateA2AAgents:
		return app.renderA2AAgents()
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

// githubSetupCheckedMsg carries the result of the repository checks that
// precede the GitHub Action setup flow.
type githubSetupCheckedMsg struct {
	repo         string
	isOrg        bool
	secretsExist bool
	err          error
}

func (app *ChatApplication) handleGithubActionSetupTrigger() []tea.Cmd {
	return []tea.Cmd{
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    "Checking repository...",
				Spinner:    true,
				StatusType: domain.StatusDefault,
			}
		},
		app.checkGithubSetupPreconditions(),
	}
}

// checkGithubSetupPreconditions runs the gh CLI checks off the Update loop;
// each check shells out and can take seconds.
func (app *ChatApplication) checkGithubSetupPreconditions() tea.Cmd {
	return func() tea.Msg {
		repo, err := app.getCurrentRepo()
		if err != nil {
			return githubSetupCheckedMsg{err: fmt.Errorf("failed to get repository info: %w", err)}
		}

		isOrg, err := app.isOrgRepo(repo)
		if err != nil {
			return githubSetupCheckedMsg{err: fmt.Errorf("failed to check repository type: %w", err)}
		}

		msg := githubSetupCheckedMsg{repo: repo, isOrg: isOrg}
		if isOrg {
			owner := strings.Split(repo, "/")[0]
			if msg.secretsExist, err = app.checkOrgSecretsExist(owner); err != nil {
				return githubSetupCheckedMsg{err: fmt.Errorf("failed to check org secrets: %w", err)}
			}
		}
		return msg
	}
}

func (app *ChatApplication) handleGithubSetupChecked(msg githubSetupCheckedMsg) []tea.Cmd {
	var cmds []tea.Cmd

	if msg.err != nil {
		cmds = append(cmds, func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("GitHub Action setup failed: %v", msg.err),
				Sticky: true,
			}
		})
		return cmds
	}

	if msg.isOrg && msg.secretsExist {
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

	owner := strings.Split(msg.repo, "/")[0]
	app.initGithubActionView.SetRepositoryInfo(owner, msg.isOrg)
	app.initGithubActionView.Reset()
	if cmd := app.initGithubActionView.Init(); cmd != nil {
		cmds = append(cmds, cmd)
	}

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
		logger.Error("failed to add pull request creation message to conversation", "error", err)
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
		// The task view shows all background work - shells, subagents, and A2A
		// tasks - so it is no longer gated on A2A. Shell/subagent rows come from the
		// unified BackgroundTaskRegistry's supervisor snapshot; A2A rows from the
		// poller/retention service. Either source may simply be empty.
		styleProvider := styles.NewProvider(app.themeService)
		app.taskManager = components.NewTaskManager(app.themeService, styleProvider, app.taskRetentionService, app.backgroundTaskService)
		if app.backgroundTaskRegistry != nil {
			app.taskManager.SetBackgroundTaskRegistry(app.backgroundTaskRegistry)
		}
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

	cmds = append(cmds, func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    "",
			Spinner:    false,
			StatusType: domain.StatusDefault,
		}
	})

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
	app.modelSelector = components.NewModelSelector(app.availableModels, app.modelService, app.pricingService, app.config, styleProvider)
}

func (app *ChatApplication) renderThemeSelection() string {
	width, height := app.stateManager.GetDimensions()
	app.themeSelector.SetWidth(width)
	app.themeSelector.SetHeight(height)
	return app.themeSelector.View().Content
}

// handleToolsListView drives the read-only tools list. A cancelled flag left
// over from the previous visit means we are re-entering: Reset rebuilds the
// items so the list reflects the current agent mode and any MCP tools
// registered since.
func (app *ChatApplication) handleToolsListView(msg tea.Msg) []tea.Cmd {
	var cmds []tea.Cmd

	if app.toolsView.IsCancelled() {
		app.toolsView.Reset()
	}

	model, cmd := app.toolsView.Update(msg)
	app.toolsView = model.(*components.ToolsViewImpl)
	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	if app.toolsView.IsCancelled() {
		if err := app.stateManager.TransitionToView(domain.ViewStateChat); err != nil {
			cmds = append(cmds, func() tea.Msg {
				return domain.ShowErrorEvent{
					Error:  fmt.Sprintf("Failed to return to chat: %v", err),
					Sticky: false,
				}
			})
		}
		app.focusedComponent = app.inputView
	}

	return cmds
}

func (app *ChatApplication) renderToolsList() string {
	width, height := app.stateManager.GetDimensions()
	app.toolsView.SetWidth(width)
	app.toolsView.SetHeight(height)
	return app.toolsView.View().Content
}

// handleA2AAgentsView drives the read-only A2A agents list, mirroring
// handleToolsListView: a leftover cancelled flag means re-entry, so Reset
// rebuilds the items from the latest agent readiness.
func (app *ChatApplication) handleA2AAgentsView(msg tea.Msg) []tea.Cmd {
	var cmds []tea.Cmd

	if app.a2aAgentsView.IsCancelled() {
		app.a2aAgentsView.Reset()
	}

	model, cmd := app.a2aAgentsView.Update(msg)
	app.a2aAgentsView = model.(*components.A2AAgentsViewImpl)
	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	if app.a2aAgentsView.IsCancelled() {
		if err := app.stateManager.TransitionToView(domain.ViewStateChat); err != nil {
			cmds = append(cmds, func() tea.Msg {
				return domain.ShowErrorEvent{
					Error:  fmt.Sprintf("Failed to return to chat: %v", err),
					Sticky: false,
				}
			})
		}
		app.focusedComponent = app.inputView
	}

	return cmds
}

func (app *ChatApplication) renderA2AAgents() string {
	width, height := app.stateManager.GetDimensions()
	app.a2aAgentsView.SetWidth(width)
	app.a2aAgentsView.SetHeight(height)
	return app.a2aAgentsView.View().Content
}

func (app *ChatApplication) renderConversationSelection() string {
	if app.conversationSelector == nil {
		return "Conversation selection requires persistent storage to be enabled."
	}

	width, height := app.stateManager.GetDimensions()
	app.conversationSelector.SetWidth(width)
	app.conversationSelector.SetHeight(height)
	return app.conversationSelector.View().Content
}

func (app *ChatApplication) renderA2ATaskManagement() string {
	if app.taskManager == nil {
		return "Loading tasks…"
	}

	width, height := app.stateManager.GetDimensions()
	app.taskManager.SetWidth(width)
	app.taskManager.SetHeight(height)
	return app.taskManager.View().Content
}

func (app *ChatApplication) renderGithubActionSetup() string {
	width, height := app.stateManager.GetDimensions()
	app.initGithubActionView.SetWidth(width)
	app.initGithubActionView.SetHeight(height)
	return app.initGithubActionView.View().Content
}

// handleHelpViewTrigger fills the help overlay with the current commands and
// keybindings, then transitions to the scrollable help view.
func (app *ChatApplication) handleHelpViewTrigger() []tea.Cmd {
	var cmds []tea.Cmd

	app.helpView.Reset()

	width, height := app.stateManager.GetDimensions()
	app.helpView.SetWidth(width)
	app.helpView.SetHeight(height)
	app.helpView.SetContent(app.buildHelpCommands(), app.collectKeyShortcuts())

	if err := app.stateManager.TransitionToView(domain.ViewStateHelp); err != nil {
		cmds = append(cmds, func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Failed to show help: %v", err),
				Sticky: false,
			}
		})
		return cmds
	}

	cmds = append(cmds, func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    "",
			Spinner:    false,
			StatusType: domain.StatusDefault,
		}
	})

	return cmds
}

// buildHelpCommands collects every registered slash command (sorted by name)
// for the help overlay's commands table.
func (app *ChatApplication) buildHelpCommands() []components.HelpCommand {
	if app.shortcutRegistry == nil {
		return nil
	}

	all := app.shortcutRegistry.GetAll()
	commands := make([]components.HelpCommand, 0, len(all))
	for _, s := range all {
		commands = append(commands, components.HelpCommand{
			Name:        s.GetName(),
			Description: s.GetDescription(),
		})
	}
	return commands
}

func (app *ChatApplication) handleHelpView(msg tea.Msg) []tea.Cmd {
	var cmds []tea.Cmd

	model, cmd := app.helpView.Update(msg)
	app.helpView = model.(*components.HelpViewImpl)
	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	if app.helpView.IsCancelled() {
		return app.handleHelpViewClosed(cmds)
	}

	return cmds
}

func (app *ChatApplication) handleHelpViewClosed(cmds []tea.Cmd) []tea.Cmd {
	app.helpView.Reset()

	if err := app.stateManager.TransitionToView(domain.ViewStateChat); err != nil {
		return []tea.Cmd{tea.Quit}
	}

	app.focusedComponent = app.inputView

	cmds = append(cmds, func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    "",
			Spinner:    false,
			StatusType: domain.StatusDefault,
		}
	})

	return cmds
}

func (app *ChatApplication) renderHelp() string {
	width, height := app.stateManager.GetDimensions()
	app.helpView.SetWidth(width)
	app.helpView.SetHeight(height)
	return app.helpView.View().Content
}

// handleDiffViewerView drives the VS Code-style changes panel. It is lazily
// constructed on first entry and re-initialized when reopened, mirroring the
// A2A task management view.
func (app *ChatApplication) handleDiffViewerView(msg tea.Msg) []tea.Cmd {
	var cmds []tea.Cmd

	if app.diffViewer == nil {
		styleProvider := styles.NewProvider(app.themeService)
		cwd, err := os.Getwd()
		if err != nil {
			cwd = "."
		}
		app.diffViewer = components.NewDiffViewer(gitdiff.NewGitSource(cwd), styleProvider, app.themeService, app.config.Chat.Keybindings)
		if cmd := app.diffViewer.Init(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	if app.diffViewer.IsDone() || app.diffViewer.IsCancelled() {
		app.diffViewer.Reset()
		if cmd := app.diffViewer.Init(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	model, cmd := app.diffViewer.Update(msg)
	app.diffViewer = model.(*components.DiffViewerImpl)
	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	return app.handleDiffViewerClose(cmds)
}

func (app *ChatApplication) handleDiffViewerClose(cmds []tea.Cmd) []tea.Cmd {
	if !app.diffViewer.IsCancelled() {
		return cmds
	}

	if err := app.stateManager.TransitionToView(domain.ViewStateChat); err != nil {
		return []tea.Cmd{tea.Quit}
	}

	if iv, ok := app.inputView.(*components.InputView); ok {
		iv.SetDisabled(false)
		iv.ClearCustomHint()
	}
	app.focusedComponent = app.inputView

	cmds = append(cmds, func() tea.Msg {
		return domain.SetStatusEvent{Message: "", Spinner: false, StatusType: domain.StatusDefault}
	})
	return cmds
}

func (app *ChatApplication) renderDiffViewer() string {
	if app.diffViewer == nil {
		return "Loading changes…"
	}

	width, height := app.stateManager.GetDimensions()
	app.diffViewer.SetWidth(width)
	app.diffViewer.SetHeight(height)
	return app.diffViewer.Render(app.renderDiffViewerInput())
}

// renderDiffViewerInput renders the chat input (disabled, with a hint) sized to
// the diff pane width, so it sits beneath the diff to the right of the sidebar.
func (app *ChatApplication) renderDiffViewerInput() string {
	iv, ok := app.inputView.(*components.InputView)
	if !ok {
		return ""
	}
	iv.SetCustomHint(app.diffViewer.HintText())
	iv.SetWidth(app.diffViewer.PaneWidth())
	return app.inputView.Render()
}

// handleExplorerView drives the VS Code-style file explorer panel. It is lazily
// constructed on first entry and re-initialized when reopened, mirroring the
// diff viewer.
func (app *ChatApplication) handleExplorerView(msg tea.Msg) []tea.Cmd {
	var cmds []tea.Cmd

	if app.fileExplorer == nil {
		styleProvider := styles.NewProvider(app.themeService)
		cwd, err := os.Getwd()
		if err != nil {
			cwd = "."
		}
		app.fileExplorer = components.NewFileExplorer(cwd, styleProvider, app.themeService, app.config.Chat.Keybindings)
		if cmd := app.fileExplorer.Init(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	if app.fileExplorer.IsDone() || app.fileExplorer.IsCancelled() {
		app.fileExplorer.Reset()
		if cmd := app.fileExplorer.Init(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	model, cmd := app.fileExplorer.Update(msg)
	app.fileExplorer = model.(*components.FileExplorerImpl)
	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	return app.handleExplorerClose(cmds)
}

func (app *ChatApplication) handleExplorerClose(cmds []tea.Cmd) []tea.Cmd {
	if app.fileExplorer.IsDone() {
		return app.handleExplorerSubmit(cmds)
	}
	if !app.fileExplorer.IsCancelled() {
		return cmds
	}

	if err := app.stateManager.TransitionToView(domain.ViewStateChat); err != nil {
		return []tea.Cmd{tea.Quit}
	}

	if iv, ok := app.inputView.(*components.InputView); ok {
		iv.SetDisabled(false)
		iv.ClearCustomHint()
	}
	app.focusedComponent = app.inputView
	app.attachmentsFocused = false

	cmds = append(cmds, func() tea.Msg {
		return domain.SetStatusEvent{Message: "", Spinner: false, StatusType: domain.StatusDefault}
	})
	return cmds
}

// handleExplorerSubmit runs when the explorer closes normally (IsDone). It
// carries any captured selections into the pending attachments shown as a tree
// below the chat input (their content is sent with the next message), then
// returns to the chat view.
func (app *ChatApplication) handleExplorerSubmit(cmds []tea.Cmd) []tea.Cmd {
	sels := app.fileExplorer.Selections()
	app.pendingSnippets = append(app.pendingSnippets, sels...)

	if err := app.stateManager.TransitionToView(domain.ViewStateChat); err != nil {
		return []tea.Cmd{tea.Quit}
	}
	if iv, ok := app.inputView.(*components.InputView); ok {
		iv.SetDisabled(false)
		iv.ClearCustomHint()
	}
	app.focusedComponent = app.inputView
	app.attachmentsFocused = false

	status := ""
	if len(sels) > 0 {
		status = fmt.Sprintf("%d snippet(s) attached - sent with your next message", len(sels))
	}
	cmds = append(cmds, func() tea.Msg {
		return domain.SetStatusEvent{Message: status, Spinner: false, StatusType: domain.StatusDefault}
	})
	return cmds
}

func (app *ChatApplication) renderExplorer() string {
	if app.fileExplorer == nil {
		return "Loading explorer…"
	}

	width, height := app.stateManager.GetDimensions()
	app.fileExplorer.SetWidth(width)
	app.fileExplorer.SetHeight(height)
	return app.fileExplorer.Render(app.renderExplorerInput())
}

// renderExplorerInput renders the chat input (disabled, with a hint) sized to the
// preview pane width, so it sits beneath the pane to the right of the sidebar.
func (app *ChatApplication) renderExplorerInput() string {
	iv, ok := app.inputView.(*components.InputView)
	if !ok {
		return ""
	}
	iv.SetCustomHint(app.fileExplorer.HintText())
	iv.SetWidth(app.fileExplorer.PaneWidth())
	return app.inputView.Render()
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

	app.syncSnippetAttachmentsView()

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
		app.approvalBoxView,
		app.questionFormView,
		app.snippetAttachmentsView,
	)

	return chatInterface
}

// syncSnippetAttachmentsView pushes the current pending snippets and focus state
// into the attachments view before each render.
func (app *ChatApplication) syncSnippetAttachmentsView() {
	if app.snippetAttachmentsView == nil {
		return
	}
	app.snippetAttachmentsView.SetData(app.pendingSnippets)
	app.snippetAttachmentsView.SetFocusHint(app.focusAttachmentsKeyLabel())
	if app.attachmentsFocused {
		app.snippetAttachmentsView.Focus()
	} else {
		app.snippetAttachmentsView.Blur()
	}
}

// focusAttachmentsKeyLabel returns the primary key bound to the focus-attachments
// action, for display in the attachments tree header ("" when unbound).
func (app *ChatApplication) focusAttachmentsKeyLabel() string {
	if ks := app.chatKeys[actChatFocusAttachments]; len(ks) > 0 {
		return ks[0]
	}
	return ""
}

func (app *ChatApplication) renderModelSelection() string {
	width, height := app.stateManager.GetDimensions()
	app.modelSelector.SetWidth(width)
	app.modelSelector.SetHeight(height)
	return app.modelSelector.View().Content
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

func (app *ChatApplication) handleFileSelectionKeys(keyMsg tea.KeyPressMsg) tea.Cmd {
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
		logger.Error("failed to transition to chat view after file selection", "error", err)
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
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return false
	}
	if keyMsg.String() != app.lastHandledKey {
		return false
	}

	app.lastHandledKey = ""
	if app.stateManager.GetApprovalUIState() != nil || app.stateManager.GetPlanApprovalUIState() != nil {
		return false
	}

	return app.keyBindingManager.ShouldSkipInputUpdate(keyMsg)
}

// updateUIComponentsForUIMessages updates UI components for UI events and framework messages
func (app *ChatApplication) updateUIComponentsForUIMessages(msg tea.Msg) []tea.Cmd {
	switch msg.(type) {
	case tea.WindowSizeMsg, tea.MouseMsg, tea.KeyPressMsg, tea.FocusMsg, tea.BlurMsg:
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

	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		if app.keyBindingManager.ShouldSkipInputUpdate(keyMsg) {
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
			switch {
			case acMsg.CursorPos > 0:
				app.inputView.SetCursor(acMsg.CursorPos)
			default:
				if idx := strings.Index(acMsg.Completion, `=""`); idx != -1 {
					app.inputView.SetCursor(idx + 2)
				} else {
					app.inputView.SetCursor(len(acMsg.Completion))
				}
			}

			usageHint := app.autocomplete.GetUsageHint()
			app.inputView.SetUsageHint(usageHint)
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
	return app.config
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
	editing := app.stateManager.IsEditingMessage()

	hasSnippets := len(app.pendingSnippets) > 0 && !editing
	if input == "" && len(images) == 0 && !hasSnippets {
		return nil
	}

	if input != "" {
		if err := app.inputView.AddToHistory(input); err != nil {
			logger.Error("failed to add input to history", "error", err)
		}
	}

	app.inputView.ClearInput()

	app.conversationView.ResetUserScroll()

	for _, img := range images {
		if img.SourcePath != "" {
			if err := os.Remove(img.SourcePath); err != nil {
				logger.Warn("failed to clean up temporary image file", "path", img.SourcePath, "error", err)
			}
		}
	}

	if editing {
		editState := app.stateManager.GetMessageEditState()

		app.stateManager.ClearMessageEditState()
		if iv, ok := app.inputView.(*components.InputView); ok {
			iv.ClearCustomHint()
		}

		return func() tea.Msg {
			return domain.MessageEditSubmitEvent{
				RequestID:     "message-edit-submit",
				Timestamp:     time.Now(),
				OriginalIndex: editState.OriginalMessageIndex,
				EditedContent: input,
				Images:        images,
			}
		}
	}

	content := input
	if augmented, appended := app.augmentWithSnippets(input); appended {
		content = augmented
		app.pendingSnippets = nil
		app.attachmentsFocused = false
	}

	return func() tea.Msg {
		return domain.UserInputEvent{
			Content: content,
			Images:  images,
		}
	}
}

// augmentWithSnippets appends the pending snippet attachments (selected lines
// only, via FormatAnnotations) to the outgoing message content. It is skipped
// for slash/bash commands, which must not carry a trailing code blob - their
// attachments are preserved for the next regular message. Returns the content
// to send and whether snippets were appended.
func (app *ChatApplication) augmentWithSnippets(input string) (string, bool) {
	if len(app.pendingSnippets) == 0 || isCommandInput(input) {
		return input, false
	}
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	block := components.FormatAnnotations(cwd, app.pendingSnippets)
	if block == "" {
		return input, false
	}
	if input == "" {
		return block, true
	}
	return input + "\n\n" + block, true
}

// isCommandInput reports whether the message is a slash command or a bash (!)
// invocation routed by the message processor rather than sent to the model.
func isCommandInput(input string) bool {
	return strings.HasPrefix(input, "/") || strings.HasPrefix(input, "!")
}

// ToggleToolResultExpansion toggles tool result expansion
func (app *ChatApplication) ToggleToolResultExpansion() {
	app.toggleToolResultExpansion()
}

// ToggleThinkingExpansion toggles thinking block expansion
func (app *ChatApplication) ToggleThinkingExpansion() {
	app.conversationView.ToggleAllThinkingExpansion()
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

// Message History Navigation Helpers

// handleNavigateBackInTime initiates message history navigation mode
func (app *ChatApplication) handleNavigateBackInTime(event domain.NavigateBackInTimeEvent) []tea.Cmd {
	var cmds []tea.Cmd

	iv, ok := app.inputView.(*components.InputView)
	if !ok {
		return cmds
	}

	if cmd := app.messageHistoryHandler.HandleNavigateBackInTime(event); cmd != nil {
		cmds = append(cmds, cmd)
	}

	iv.SetCustomHint("Loading message history...")

	return cmds
}

// handleEditReady enters edit mode with the selected message content
func (app *ChatApplication) handleEditReady(event domain.MessageHistoryEditReadyEvent) []tea.Cmd {
	var cmds []tea.Cmd

	app.stateManager.SetMessageEditState(&domain.MessageEditState{
		OriginalMessageIndex: event.MessageIndex,
		OriginalContent:      event.Content,
		EditTimestamp:        time.Now(),
	})

	entries := app.conversationRepo.GetMessages()
	deleteIndex := app.adjustRestoreIndexForEdit(entries, event.MessageIndex)

	var err error
	if deleteIndex == 0 {
		err = app.conversationRepo.Clear()
	} else {
		err = app.conversationRepo.DeleteMessagesAfterIndex(deleteIndex - 1)
	}

	if err != nil {
		logger.Error("failed to delete messages during edit", "error", err)
		cmds = append(cmds, func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Failed to delete messages: %v", err),
				Sticky: true,
			}
		})
		return cmds
	}

	cmds = append(cmds, func() tea.Msg {
		return domain.UpdateHistoryEvent{
			History: app.conversationRepo.GetMessages(),
		}
	})

	if iv, ok := app.inputView.(*components.InputView); ok {
		iv.SetText(event.Content)
		iv.SetCursor(len(event.Content))

		timestamp := event.Snapshot.Timestamp.Format("15:04:05")
		hint := fmt.Sprintf("Editing message from %s - Press enter to submit", timestamp)
		iv.SetCustomHint(hint)
	}

	return cmds
}

// adjustRestoreIndexForEdit adjusts the restore index based on message role and tool calls
// This is similar to the logic in message_history_handler.go but adapted for the app layer
func (app *ChatApplication) adjustRestoreIndexForEdit(entries []domain.ConversationEntry, restoreIndex int) int {
	if restoreIndex >= len(entries) {
		return restoreIndex
	}

	msg := entries[restoreIndex]
	if msg.Message.Role == sdk.Assistant && msg.Message.ToolCalls != nil && len(*msg.Message.ToolCalls) > 0 {
		toolResponsesFound := 0
		for i := restoreIndex + 1; i < len(entries); i++ {
			if entries[i].Message.Role == sdk.Tool {
				restoreIndex = i
				toolResponsesFound++
			} else {
				break
			}
		}
	} else {
		for restoreIndex > 0 && entries[restoreIndex].Message.Role == sdk.Tool {
			restoreIndex--
		}
	}

	return restoreIndex
}

// handleMessageHistoryEnter handles the enter key press in message history mode
func (app *ChatApplication) handleMessageHistoryEnter(cv *components.ConversationView, iv *components.InputView, cmds []tea.Cmd) []tea.Cmd {
	selectedIndex := cv.GetSelectedMessageIndex()
	if selectedIndex < 0 {
		return cmds
	}

	selectedSnapshot := cv.GetSelectedMessageSnapshot()
	if selectedSnapshot == nil {
		return cmds
	}

	cv.ExitMessageHistoryMode()

	if selectedSnapshot.Role == sdk.User {
		editEvent := domain.MessageHistoryEditEvent{
			RequestID:       "message-history-edit",
			Timestamp:       time.Now(),
			MessageIndex:    selectedIndex,
			MessageContent:  selectedSnapshot.Content,
			MessageSnapshot: *selectedSnapshot,
		}

		if cmd := app.messageHistoryHandler.HandleEdit(editEvent); cmd != nil {
			cmds = append(cmds, cmd)
		}
	} else {
		restoreEvent := domain.MessageHistoryRestoreEvent{
			RequestID:      "message-history-restore",
			Timestamp:      time.Now(),
			RestoreToIndex: selectedIndex,
		}

		iv.ClearCustomHint()

		if cmd := app.messageHistoryHandler.HandleRestore(restoreEvent); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return cmds
}

// handleMessageHistoryKeys handles key presses during message history navigation
func (app *ChatApplication) handleMessageHistoryKeys(keyMsg tea.KeyPressMsg) []tea.Cmd {
	var cmds []tea.Cmd

	cv, ok := app.conversationView.(*components.ConversationView)
	if !ok {
		logger.Warn("failed to cast conversationView to ConversationView")
		return cmds
	}

	iv, ok := app.inputView.(*components.InputView)
	if !ok {
		logger.Warn("failed to cast inputView to InputView")
		return cmds
	}

	switch keyMsg.String() {
	case "up", "k":
		cv.NavigateHistoryUp()
	case "down", "j":
		cv.NavigateHistoryDown()
	case "enter":
		cmds = app.handleMessageHistoryEnter(cv, iv, cmds)
	case "esc":
		cv.ExitMessageHistoryMode()
		iv.ClearCustomHint()
	}

	return cmds
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

// Version pins and defaults for the generated .github/workflows/infer.yml.
// Bumping any of these is a one-line change picked up by both templates.
const (
	inferActionVersion     = "v0.29.0"
	checkoutActionVersion  = "v7.0.0"
	appTokenActionVersion  = "v3.2.0"
	workflowDefaultModel   = "ollama_cloud/deepseek-v4-flash"
	workflowTimeoutMinutes = 15
)

// workflowHeader is the shared prologue of the generated workflow: header
// comment, triggers (issue/comment mentions + manual workflow_dispatch),
// permissions, and the gated job preamble with concurrency and timeout.
func workflowHeader(extraNote string) string {
	return fmt.Sprintf(`---
# Infer Agent CI
#
# Runs the Infer agent (inference-gateway/infer-action) in three ways:
#
# 1. Issue-driven: mention `+"`@infer`"+` in an issue title, body, or comment and
#    the agent picks up the task, works on it, and opens a draft PR.
# 2. Review-comment-driven: mention `+"`@infer`"+` in a pull request review comment
#    (inline or thread reply) and the agent works on the focused file/diff hunk.
# 3. Manual (workflow_dispatch): run it from the Actions tab with a free-text
#    prompt — useful for ad-hoc tasks like "find bugs and report them".
#    Optionally tick "browser-agent" to spin up the A2A
#    inference-gateway/browser-agent container so the agent can browse the web.
#
# Notes:
# - Jobs are capped at %d minutes (timeout-minutes).
# - Runs are deduplicated per issue, PR, or dispatch run via concurrency.%s
name: Infer

on:
  issues:
    types:
      - opened
      - edited
  issue_comment:
    types:
      - created
  pull_request_review_comment:
    types:
      - created
  workflow_dispatch:
    inputs:
      prompt:
        description: 'Free-text task for the agent (e.g. "find bugs and report them")'
        type: string
        required: true
      browser-agent:
        description: 'Start the A2A browser-agent (inference-gateway/browser-agent) so the agent can browse the web'
        type: boolean
        required: false
        default: false
      debug:
        description: 'Enable debug output and mirror agent logs'
        type: boolean
        required: false
        default: false

permissions:
  issues: write
  contents: write
  pull-requests: write

jobs:
  infer:
    concurrency:
      group: ${{ github.workflow }}-${{ github.event.issue.number || github.event.pull_request.number || github.run_id }}
      cancel-in-progress: true
    if: |
      github.event_name == 'workflow_dispatch' ||
      (
        github.event.sender.type != 'Bot' &&
        !endsWith(github.actor, '[bot]') &&
        (
          (github.event_name == 'issue_comment' && contains(github.event.comment.body, '@infer')) ||
          (github.event_name == 'pull_request_review_comment' && contains(github.event.comment.body, '@infer')) ||
          (github.event_name == 'issues' && (contains(github.event.issue.body, '@infer') || contains(github.event.issue.title, '@infer')))
        )
      )
    runs-on: ubuntu-24.04
    timeout-minutes: %d
    steps:
`, workflowTimeoutMinutes, extraNote, workflowTimeoutMinutes)
}

// workflowAgentInputs is the shared tail of the "Run Infer Agent" step: the
// agent defaults and the provider API key pass-throughs.
const workflowAgentInputs = `          trigger-phrase: '@infer'
          model: ` + workflowDefaultModel + `
          direct-prompt: ${{ inputs.prompt }}
          agents: ${{ inputs.browser-agent && 'browser-agent' || '' }}
          debug: ${{ inputs.debug }}
          mirror-agent-logs: ${{ inputs.debug }}
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
          moonshot-api-key: ${{ secrets.MOONSHOT_API_KEY }}
          minimax-api-key: ${{ secrets.MINIMAX_API_KEY }}
          nvidia-api-key: ${{ secrets.NVIDIA_API_KEY }}
          zai-api-key: ${{ secrets.ZAI_API_KEY }}
`

func (app *ChatApplication) generateStandardWorkflowContent() string {
	return workflowHeader("") + fmt.Sprintf(`      - name: Checkout repository
        uses: actions/checkout@%s

      - name: Run Infer Agent
        uses: inference-gateway/infer-action@%s
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
`, checkoutActionVersion, inferActionVersion) + workflowAgentInputs
}

func (app *ChatApplication) generateGithubActionWorkflowContent() string {
	extraNote := `
# - The GitHub App used for the token needs the "Workflows" (read & write)
#   repository permission so the agent can push changes to .github/workflows.`
	return workflowHeader(extraNote) + fmt.Sprintf(`      - name: Generate GitHub App token
        id: app-token
        uses: actions/create-github-app-token@%s
        with:
          client-id: ${{ secrets.INFER_APP_ID }}
          private-key: ${{ secrets.INFER_APP_PRIVATE_KEY }}
          owner: ${{ github.repository_owner }}
          repositories: |
            ${{ github.event.repository.name }}

      - name: Get GitHub App User ID
        id: get-user-id
        run: echo "user-id=$(gh api "/users/${{ steps.app-token.outputs.app-slug }}[bot]" --jq .id)" >> "$GITHUB_OUTPUT"
        env:
          GH_TOKEN: ${{ steps.app-token.outputs.token }}

      - name: Set up Git
        run: |
          git config --global user.name '${{ steps.app-token.outputs.app-slug }}[bot]'
          git config --global user.email '${{ steps.get-user-id.outputs.user-id }}+${{ steps.app-token.outputs.app-slug }}[bot]@users.noreply.github.com'
          git config --global commit.gpgsign false
          git config --global commit.signoff true

      - name: Checkout repository
        uses: actions/checkout@%s
        with:
          token: ${{ steps.app-token.outputs.token }}

      - name: Run Infer Agent
        uses: inference-gateway/infer-action@%s
        with:
          github-token: ${{ steps.app-token.outputs.token }}
          github-app-slug: ${{ steps.app-token.outputs.app-slug }}
`, appTokenActionVersion, checkoutActionVersion, inferActionVersion) + workflowAgentInputs
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

// buildAgentNameResolver loads ~/.infer/agents.yaml (or the project-level
// equivalent) once and returns a closure that maps an agent URL to its
// configured friendly name. Used by the background-agent indicator to show
// e.g. `Agent(weather-agent=…)` instead of the raw URL. Returns nil on
// load failure so the conversation view falls back to the URL.
func buildAgentNameResolver() func(string) string {
	cfg, err := config.LoadAgents(config.ResolveAgentsPath())
	if err != nil || cfg == nil {
		return nil
	}
	nameByURL := make(map[string]string, len(cfg.Agents))
	for _, a := range cfg.Agents {
		if a.URL != "" && a.Name != "" {
			nameByURL[a.URL] = a.Name
		}
	}
	if len(nameByURL) == 0 {
		return nil
	}
	return func(url string) string {
		return nameByURL[url]
	}
}

// buildAgentModelResolver loads ~/.infer/agents.yaml (or the project-level
// equivalent) once and returns a closure that maps an agent URL to its
// configured model (e.g. "deepseek/deepseek-v4-flash"). Used by the
// background-agent indicator to show `model=<...>` in the live status
// line. Returns nil on load failure or when no agent has a model set,
// so the conversation view omits the model segment cleanly.
func buildAgentModelResolver() func(string) string {
	cfg, err := config.LoadAgents(config.ResolveAgentsPath())
	if err != nil || cfg == nil {
		return nil
	}
	modelByURL := make(map[string]string, len(cfg.Agents))
	for _, a := range cfg.Agents {
		if a.URL != "" && a.Model != "" {
			modelByURL[a.URL] = a.Model
		}
	}
	if len(modelByURL) == 0 {
		return nil
	}
	return func(url string) string {
		return modelByURL[url]
	}
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

// GetCurrentConversationID returns the current conversation ID from the repository.
// Returns an empty string for in-memory (non-persistent) repositories.
func (app *ChatApplication) GetCurrentConversationID() string {
	return app.conversationRepo.GetCurrentConversationID()
}
