package app

import (
	"context"
	"fmt"
	utils "github.com/inference-gateway/cli/internal/platform/utils"
	statemanager "github.com/inference-gateway/cli/internal/presentation/tui/statemanager"
	toolformatter "github.com/inference-gateway/cli/internal/presentation/tui/toolformatter"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"

	key "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	tools "github.com/inference-gateway/cli/internal/agent/tools"
	conversation "github.com/inference-gateway/cli/internal/conversation"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	constants "github.com/inference-gateway/cli/internal/platform/constants"
	formatting "github.com/inference-gateway/cli/internal/platform/formatting"
	gitdiff "github.com/inference-gateway/cli/internal/platform/gitdiff"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
	shortcuts "github.com/inference-gateway/cli/internal/presentation/shortcuts"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	autocomplete "github.com/inference-gateway/cli/internal/presentation/tui/autocomplete"
	components "github.com/inference-gateway/cli/internal/presentation/tui/components"
	factory "github.com/inference-gateway/cli/internal/presentation/tui/components/factory"
	handlers "github.com/inference-gateway/cli/internal/presentation/tui/handlers"
	keybinding "github.com/inference-gateway/cli/internal/presentation/tui/keybinding"
	styles "github.com/inference-gateway/cli/internal/presentation/tui/styles"
)

// actChatFocusAttachments is the chat-namespace action that moves key focus to
// the snippet attachments tree below the input.
var actChatFocusAttachments = config.ActionID(config.NamespaceChat, "focus_attachments")

// ChatApplication represents the main application model using state management
type ChatApplication struct {
	// Dependencies
	config                 *config.Config
	agentService           agentdomain.AgentService
	conversationRepo       convdomain.ConversationRepository
	conversationOptimizer  convdomain.ConversationOptimizer
	sessionRolloverManager *conversation.SessionRolloverManager
	modelService           convdomain.ModelService
	toolService            agentdomain.ToolService
	fileService            agentdomain.FileService
	imageService           agentdomain.ImageService
	skillsService          agentdomain.SkillsService
	githubIssueService     agentdomain.GitHubIssueService
	githubSetupService     agentdomain.GitHubSetupService
	pricingService         convdomain.PricingService
	shortcutRegistry       *shortcuts.Registry
	themeService           tui.ThemeService
	toolRegistry           *tools.Registry
	mcpManager             agentdomain.MCPManager
	taskRetentionService   scheddomain.TaskRetentionService
	backgroundTaskService  scheddomain.BackgroundTaskService
	backgroundTaskRegistry scheddomain.BackgroundTaskRegistry

	// Chat orchestration services
	a2aTaskCoordinator       tui.A2ATaskCoordinator
	approvalCoordinator      tui.ApprovalCoordinator
	chatCompletionRunner     tui.ChatCompletionRunner
	directExecutionService   tui.DirectExecutionService
	toolExecutionCoordinator tui.ToolExecutionCoordinator

	// State management
	stateManager *statemanager.StateManager
	messageQueue convdomain.MessageQueue
	mouseEnabled bool

	// UI components
	conversationView     tui.ConversationRenderer
	inputView            tui.InputComponent
	autocomplete         tui.AutocompleteComponent
	inputStatusBar       tui.InputStatusBarComponent
	statusView           tui.StatusComponent
	modeIndicator        *components.ModeIndicator
	helpBar              tui.HelpBarComponent
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
	chatHandler           tui.ChatHandler
	messageHistoryHandler *handlers.MessageHistoryHandler

	// Current active component for key handling
	focusedComponent tui.InputComponent

	// Pending snippet attachments captured in the file explorer, shown as a tree
	// below the input and sent (then cleared) with the next chat message.
	pendingSnippets    []components.SnippetSelection
	attachmentsFocused bool

	// Keyboard focus on the status-indicator row below the input, entered
	// with arrow-down when input-history navigation is idle.
	statusBarFocused bool

	// Key binding system
	keyBindingManager *keybinding.KeyBindingManager

	// Config-backed binding that moves key focus to the snippet attachments
	// tree; the fixed guard bindings live in the package-level guardKeys.
	focusAttachments key.Binding

	// Track last key handled by keybinding action to prevent double-handling
	lastHandledKey string
	lastView       tui.ViewState

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
	versionInfo tui.VersionInfo,
	agentManager agentdomain.AgentManager,
	agentService agentdomain.AgentService,
	backgroundTaskService scheddomain.BackgroundTaskService,
	backgroundTaskRegistry scheddomain.BackgroundTaskRegistry,
	conversationOptimizer convdomain.ConversationOptimizer,
	conversationRepo convdomain.ConversationRepository,
	fileService agentdomain.FileService,
	imageService agentdomain.ImageService,
	skillsService agentdomain.SkillsService,
	githubIssueService agentdomain.GitHubIssueService,
	githubSetupService agentdomain.GitHubSetupService,
	mcpManager agentdomain.MCPManager,
	messageQueue convdomain.MessageQueue,
	modelService convdomain.ModelService,
	pricingService convdomain.PricingService,
	sessionRolloverManager *conversation.SessionRolloverManager,
	stateManager *statemanager.StateManager,
	taskRetentionService scheddomain.TaskRetentionService,
	themeService tui.ThemeService,
	toolService agentdomain.ToolService,
	shortcutRegistry *shortcuts.Registry,
	toolRegistry *tools.Registry,
	a2aTaskCoordinator tui.A2ATaskCoordinator,
	approvalCoordinator tui.ApprovalCoordinator,
	chatCompletionRunner tui.ChatCompletionRunner,
	directExecutionService tui.DirectExecutionService,
	toolExecutionCoordinator tui.ToolExecutionCoordinator,
	shellHistoryStore storage.ShellHistoryStorage,
) *ChatApplication {
	initialView := tui.ViewStateModelSelection
	if defaultModel != "" {
		initialView = tui.ViewStateChat
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
		githubSetupService:       githubSetupService,
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
	app.toolCallRenderer.SetStateManager(app.stateManager)
	app.conversationView = factory.CreateConversationView(app.themeService)
	toolFormatterService := toolformatter.NewToolFormatterService(app.toolRegistry, styleProvider)
	app.toolCallRenderer.SetToolFormatter(toolFormatterService)

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

	historyName := os.Getenv(scheddomain.EnvSubagentHistoryName)
	app.inputView = factory.CreateInputViewWithName(app.modelService, configDir, historyName, shellHistoryStore)
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
		isb.SetEffortSource(app.agentService)
		isb.SetThemeService(app.themeService)
		isb.SetStateManager(app.stateManager)
		isb.SetConfig(app.config)
		isb.SetConversationRepo(app.conversationRepo)
		isb.SetToolService(app.toolService)
		isb.SetTokenEstimator(conversation.NewTokenizerService(conversation.DefaultTokenizerConfig()))
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
	app.queueBoxView.SetToolFormatter(toolFormatterService)
	app.todoBoxView = components.NewTodoBoxView(styleProvider)
	app.snippetAttachmentsView = components.NewSnippetAttachmentsView(styleProvider)
	app.focusAttachments = focusAttachmentsBinding(app.config.Chat.Keybindings)
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
		sv.SetStateManager(app.stateManager)
	}

	app.toolCallRenderer.SetKeyHintFormatter(keyHintFormatter)
	app.approvalBoxView.SetKeyHintFormatter(keyHintFormatter)
	app.modelSelector = components.NewModelSelector(models, app.modelService, app.pricingService, app.config, styleProvider)
	app.themeSelector = components.NewThemeSelector(app.themeService, styleProvider)
	app.toolsView = components.NewToolsView(app.toolService, app.stateManager, styleProvider)
	app.a2aAgentsView = components.NewA2AAgentsView(app.stateManager, styleProvider)
	app.initGithubActionView = components.NewInitGithubActionView(styleProvider)

	app.initGithubActionView.SetSecretsExistChecker(func(appID string) bool {
		repo, err := app.githubSetupService.GetCurrentRepo()
		if err != nil {
			return false
		}

		isOrg, err := app.githubSetupService.IsOrgRepo(repo)
		if err != nil || !isOrg {
			return false
		}

		orgName := strings.Split(repo, "/")[0]
		secretsExist, err := app.githubSetupService.CheckOrgSecretsExist(orgName)
		return err == nil && secretsExist
	})

	if persistentRepo, ok := app.conversationRepo.(*conversation.PersistentConversationRepository); ok {
		app.conversationSelector = components.NewConversationSelector(persistentRepo, styleProvider)
	} else {
		app.conversationSelector = nil
	}

	app.taskManager = nil

	if initialView == tui.ViewStateChat {
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
		app.conversationRepo,
	)

	return app
}

// updateHelpBarShortcuts updates the help bar with essential keyboard shortcuts
func (app *ChatApplication) updateHelpBarShortcuts() {
	app.helpBar.SetShortcuts(app.collectKeyBindings())
}

// collectKeyBindings gathers the input-prefix hints and the active keybinding
// shortcuts into a single list, shared by the help bar and the /help overlay.
func (app *ChatApplication) collectKeyBindings() []key.Binding {
	bindings := []key.Binding{
		key.NewBinding(key.WithKeys("!"), key.WithHelp("!", "for bash mode")),
		key.NewBinding(key.WithKeys("!!"), key.WithHelp("!!", "for tools mode")),
		key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "for shortcuts")),
		key.NewBinding(key.WithKeys("@"), key.WithHelp("@", "for file paths")),
		key.NewBinding(key.WithKeys("#"), key.WithHelp("#", "for github issues")),
	}

	if app.keyBindingManager != nil {
		for _, kbShortcut := range app.keyBindingManager.GetHelpShortcuts() {
			bindings = append(bindings, key.NewBinding(
				key.WithKeys(kbShortcut.Key),
				key.WithHelp(kbShortcut.Key, kbShortcut.Description),
			))
		}
	}

	return bindings
}

// Init initializes the application
func (app *ChatApplication) Init() tea.Cmd {
	var cmds []tea.Cmd

	cmds = append(cmds, tea.ClearScreen)

	if app.config.GetTheme() == "" {
		cmds = append(cmds, tea.RequestBackgroundColor)
	}

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
		app.inputStatusBar.UpdateMCPStatus(&agentdomain.MCPServerStatus{
			TotalServers:     app.mcpManager.GetTotalServers(),
			ConnectedServers: 0,
			TotalTools:       0,
		})

		app.mcpManager.StartMonitoring(context.Background())
	}

	if msgs := app.conversationRepo.GetMessages(); len(msgs) > 0 {
		cmds = append(cmds, func() tea.Msg {
			return tui.UpdateHistoryEvent{History: msgs}
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

	if viewBefore == tui.ViewStateModelSelection && app.lastView != tui.ViewStateModelSelection {
		app.modelSelector.Reset()
	}

	if viewBefore == tui.ViewStateA2AAgents && app.lastView != tui.ViewStateA2AAgents {
		app.a2aAgentsView.Reset()
	}

	var cmds []tea.Cmd

	if cmd := app.handleAppEvents(msg); cmd != nil {
		cmds = append(cmds, cmd)
	}

	if isDomainEvent(msg) {
		if cmd := app.chatHandler.Handle(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	cmds = append(cmds, app.forwardToOverlayForms(msg)...)

	cmds = append(cmds, app.handleViewSpecificMessages(msg)...)

	cmds = append(cmds, app.updateUIComponentsForUIMessages(msg, viewBefore)...)

	if event, ok := msg.(agentdomain.MCPServerStatusUpdateEvent); ok {
		if cmd := app.handleMCPStatusUpdate(event); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	if viewBefore != tui.ViewStateChat &&
		app.stateManager.GetCurrentView() == tui.ViewStateChat &&
		!app.messageQueue.IsEmpty() {
		cmds = append(cmds, func() tea.Msg { return agentdomain.DrainQueueEvent{} })
	}

	app.lastView = viewBefore

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

// forwardToOverlayForms starts the AskUserQuestion / tool-approval huh forms
// when their events arrive and routes non-key messages to them while they are
// up - huh's internal group/field navigation rides on those messages, so
// without this the forms stall. Key presses reach them via
// handleChatViewKeyPress instead.
func (app *ChatApplication) forwardToOverlayForms(msg tea.Msg) []tea.Cmd {
	switch msg.(type) {
	case agentdomain.UserQuestionRequestedEvent:
		return []tea.Cmd{app.questionFormView.Begin()}
	case agentdomain.ToolApprovalRequestedEvent:
		return []tea.Cmd{app.approvalBoxView.Begin()}
	case tea.KeyPressMsg:
		return nil
	}

	var cmds []tea.Cmd
	if app.stateManager.GetUserQuestionUIState() != nil {
		cmds = append(cmds, app.questionFormView.Forward(msg))
	}
	if app.stateManager.GetApprovalUIState() != nil {
		cmds = append(cmds, app.approvalBoxView.Forward(msg))
	}
	return cmds
}

// isDomainEvent checks if an event should be handled by ChatHandler (positive filtering).
// This replaces the negative filtering pattern (isUIOnlyEvent) with an explicit declaration
// of what ChatHandler SHOULD handle, not what it shouldn't.
func isDomainEvent(msg tea.Msg) bool {
	switch msg.(type) {
	// User input and interaction
	case agentdomain.UserInputEvent,
		tui.FileSelectionRequestEvent,
		tui.ConversationSelectedEvent:
		return true

	// Chat lifecycle
	case agentdomain.ChatStartEvent,
		agentdomain.ChatChunkEvent,
		agentdomain.ChatCompleteEvent,
		agentdomain.ChatErrorEvent,
		agentdomain.OptimizationStatusEvent,
		tui.RolloverCompletedEvent:
		return true

	// Tool execution
	case agentdomain.ToolCallUpdateEvent,
		agentdomain.ToolCallReadyEvent,
		tui.ToolExecutionStartedEvent,
		agentdomain.ToolExecutionProgressEvent,
		agentdomain.ToolExecutionCompletedEvent:
		return true

	// Tool and plan approval
	case agentdomain.ToolApprovalRequestedEvent,
		agentdomain.ToolApprovalResponseEvent,
		agentdomain.PlanApprovalRequestedEvent,
		tui.PlanApprovalResponseEvent,
		agentdomain.UserQuestionRequestedEvent:
		return true

	// Bash command execution
	case agentdomain.BashOutputChunkEvent,
		tui.BashCommandCompletedEvent,
		agentdomain.BackgroundShellRequestEvent:
		return true

	// A2A (Agent-to-Agent) task management
	case agentdomain.A2AToolCallExecutedEvent,
		agentdomain.A2ATaskSubmittedEvent,
		agentdomain.A2ATaskStatusUpdateEvent,
		agentdomain.A2ATaskCompletedEvent,
		agentdomain.A2ATaskFailedEvent,
		agentdomain.A2ATaskInputRequiredEvent:
		return true

	case agentdomain.SubagentSubmittedEvent,
		agentdomain.SubagentCompletedEvent,
		agentdomain.SubagentFailedEvent:
		return true

	case agentdomain.MessageQueuedEvent,
		agentdomain.ToolCancelledEvent,
		agentdomain.TodoUpdateChatEvent,
		tui.AgentStatusUpdateEvent,
		agentdomain.DrainQueueEvent,
		tui.DrainQueueRetryEvent,
		agentdomain.NavigateBackInTimeEvent,
		agentdomain.MessageHistoryRestoreEvent,
		agentdomain.ComputerUsePausedEvent,
		agentdomain.ComputerUseResumedEvent:
		return true
	}

	return false
}

// handleAppEvents handles application-level events (not component-specific)
func (app *ChatApplication) handleAppEvents(msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case tui.TriggerGithubActionSetupEvent:
		return tea.Batch(app.handleGithubActionSetupTrigger()...)

	case githubSetupCheckedMsg:
		return tea.Batch(app.handleGithubSetupChecked(m)...)

	case tui.TriggerHelpViewEvent:
		return tea.Batch(app.handleHelpViewTrigger()...)

	case agentdomain.MessageHistoryRestoreEvent:
		return app.messageHistoryHandler.HandleRestore(m)

	case tea.BackgroundColorMsg:
		app.handleBackgroundColorDetected(m)

	}

	return nil
}

// handleMCPStatusUpdate processes MCP server connection status changes
func (app *ChatApplication) handleMCPStatusUpdate(event agentdomain.MCPServerStatusUpdateEvent) tea.Cmd {
	app.inputStatusBar.UpdateMCPStatus(&agentdomain.MCPServerStatus{
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
			return agentdomain.RefreshAutocompleteEvent{}
		}
	}

	return nil
}

func (app *ChatApplication) handleViewSpecificMessages(msg tea.Msg) []tea.Cmd {
	currentView := app.stateManager.GetCurrentView()
	inputBlocked := app.isInputBlocked(currentView)

	if inputView, ok := app.inputView.(*components.InputView); ok {
		inputView.SetDisabled(inputBlocked)
	}

	if app.statusBarFocused && (inputBlocked || currentView != tui.ViewStateChat) {
		app.blurStatusBar()
	}

	cmds := app.dispatchViewMessage(currentView, msg)

	if newView := app.stateManager.GetCurrentView(); newView != currentView {
		if inputView, ok := app.inputView.(*components.InputView); ok {
			inputView.SetDisabled(app.isInputBlocked(newView))
		}
	}

	return cmds
}

func (app *ChatApplication) dispatchViewMessage(currentView tui.ViewState, msg tea.Msg) []tea.Cmd {
	switch currentView {
	case tui.ViewStateModelSelection:
		return app.handleModelSelectionView(msg)
	case tui.ViewStateChat:
		return app.handleChatView(msg)
	case tui.ViewStateFileSelection:
		return app.handleFileSelectionView(msg)
	case tui.ViewStateConversationSelection:
		return app.handleConversationSelectionView(msg)
	case tui.ViewStateThemeSelection:
		return app.handleThemeSelectionView(msg)
	case tui.ViewStateA2ATaskManagement:
		return app.handleA2ATaskManagementView(msg)
	case tui.ViewStateGithubActionSetup:
		return app.handleInitGithubActionView(msg)
	case tui.ViewStateDiffViewer:
		return app.handleDiffViewerView(msg)
	case tui.ViewStateExplorer:
		return app.handleExplorerView(msg)
	case tui.ViewStateHelp:
		return app.handleHelpView(msg)
	case tui.ViewStateToolsList:
		return app.handleToolsListView(msg)
	case tui.ViewStateA2AAgents:
		return app.handleA2AAgentsView(msg)
	default:
		return nil
	}
}

// ponytail: extracted to reduce cyclomatic complexity of handleViewSpecificMessages
func (app *ChatApplication) isInputBlocked(currentView tui.ViewState) bool {
	inHistoryMode := false
	if cv, ok := app.conversationView.(*components.ConversationView); ok {
		inHistoryMode = cv.IsInMessageHistoryMode()
	}

	return currentView != tui.ViewStateChat ||
		app.stateManager.GetApprovalUIState() != nil ||
		app.stateManager.GetPlanApprovalUIState() != nil ||
		app.stateManager.GetUserQuestionUIState() != nil ||
		app.stateManager.GetRetryStatus() != nil ||
		inHistoryMode ||
		currentView == tui.ViewStateDiffViewer ||
		currentView == tui.ViewStateExplorer ||
		currentView == tui.ViewStateHelp
}

func (app *ChatApplication) handleModelSelectionView(msg tea.Msg) []tea.Cmd {
	var cmds []tea.Cmd

	model, cmd := app.modelSelector.Update(msg)
	app.modelSelector = model.(*components.ModelSelectorImpl)
	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	return app.handleModelSelection(cmds)
}

func (app *ChatApplication) handleModelSelection(cmds []tea.Cmd) []tea.Cmd {
	if app.modelSelector.IsSelected() {
		if err := app.stateManager.TransitionToView(tui.ViewStateChat); err != nil {
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

	if approvalEvent, ok := msg.(tui.PlanApprovalResponseEvent); ok {
		approvalState := app.stateManager.GetPlanApprovalUIState()
		if approvalState != nil && approvalState.ResponseChan != nil {
			approvalState.ResponseChan <- approvalEvent.Action
			app.stateManager.ClearPlanApprovalUIState()
		}
		return cmds
	}

	if navEvent, ok := msg.(agentdomain.NavigateBackInTimeEvent); ok {
		return app.handleNavigateBackInTime(navEvent)
	}

	if readyEvent, ok := msg.(tui.MessageHistoryReadyEvent); ok {
		if cv, ok := app.conversationView.(*components.ConversationView); ok {
			cv.EnterMessageHistoryMode(readyEvent.Messages)

			if iv, ok := app.inputView.(*components.InputView); ok {
				iv.SetCustomHint("Input paused - use ↑/↓ to navigate, enter to restore, esc to cancel")
			}
		}
		return cmds
	}

	if editReadyEvent, ok := msg.(tui.MessageHistoryEditReadyEvent); ok {
		return app.handleEditReady(editReadyEvent)
	}

	if editSubmitEvent, ok := msg.(agentdomain.MessageEditSubmitEvent); ok {
		if cmd := app.messageHistoryHandler.HandleEditSubmit(editSubmitEvent); cmd != nil {
			cmds = append(cmds, cmd)
		}
		return cmds
	}

	if _, ok := msg.(tui.FocusStatusBarEvent); ok {
		if app.inputStatusBar.Focus() {
			app.statusBarFocused = true
		}
		return cmds
	}

	if pasteMsg, ok := msg.(tea.PasteMsg); ok {
		if cmd := keybinding.HandlePasteEvent(app, pasteMsg.Content); cmd != nil {
			return []tea.Cmd{cmd}
		}
		return nil
	}

	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return cmds
	}

	return app.handleChatViewKeyPress(keyMsg)
}

func (app *ChatApplication) handleChatViewKeyPress(keyMsg tea.KeyPressMsg) []tea.Cmd {
	var cmds []tea.Cmd

	if app.stateManager.GetUserQuestionUIState() != nil && !key.Matches(keyMsg, guardKeys.interrupt) {
		if cmd := app.questionFormView.Forward(keyMsg); cmd != nil {
			return []tea.Cmd{cmd}
		}
		return nil
	}

	if app.stateManager.GetApprovalUIState() != nil {
		switch keyMsg.Code {
		case tea.KeyLeft, tea.KeyRight, tea.KeyEnter:
			if cmd := app.approvalBoxView.Forward(keyMsg); cmd != nil {
				return []tea.Cmd{cmd}
			}
			return nil
		case tea.KeyUp, tea.KeyDown:
			if app.scrollApprovalDiff(keyMsg.Code) {
				return nil
			}
		}
	}

	if cv, ok := app.conversationView.(*components.ConversationView); ok && cv.IsInMessageHistoryMode() {
		return app.handleMessageHistoryKeys(keyMsg)
	}

	if app.attachmentsFocused && !key.Matches(keyMsg, guardKeys.interrupt) {
		app.lastHandledKey = keyMsg.String()
		return app.handleAttachmentsKeys(keyMsg)
	}
	if app.statusBarFocused && !key.Matches(keyMsg, guardKeys.interrupt) {
		if cmds, handled := app.handleStatusBarKeys(keyMsg); handled {
			app.lastHandledKey = keyMsg.String()
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
	return key.Matches(keyMsg, app.focusAttachments)
}

// handleAttachmentsKeys interprets keys while the snippet attachments tree holds
// focus: navigate, remove one, clear all, or leave. All keys are consumed.
func (app *ChatApplication) handleAttachmentsKeys(keyMsg tea.KeyPressMsg) []tea.Cmd {
	if app.matchesFocusAttachments(keyMsg) {
		app.attachmentsFocused = false
		return nil
	}
	gk := guardKeys
	switch {
	case key.Matches(keyMsg, gk.navUp):
		app.snippetAttachmentsView.MoveCursor(-1)
	case key.Matches(keyMsg, gk.navDown):
		app.snippetAttachmentsView.MoveCursor(1)
	case key.Matches(keyMsg, gk.attachRemove):
		app.removeFocusedSnippet()
	case key.Matches(keyMsg, gk.attachClear):
		app.pendingSnippets = nil
		app.attachmentsFocused = false
	case key.Matches(keyMsg, gk.attachExit):
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
	gk := guardKeys
	switch {
	case key.Matches(keyMsg, gk.statusPrev):
		app.inputStatusBar.SelectPrev()
		return nil, true
	case key.Matches(keyMsg, gk.statusNext):
		app.inputStatusBar.SelectNext()
		return nil, true
	case key.Matches(keyMsg, gk.statusHold):
		return nil, true
	case key.Matches(keyMsg, gk.statusBlur):
		app.blurStatusBar()
		return nil, true
	case key.Matches(keyMsg, gk.confirm):
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
	case tui.StatusIndicatorActionModelSelection:
		_ = app.stateManager.TransitionToView(tui.ViewStateModelSelection)
		return []tea.Cmd{func() tea.Msg {
			return tui.SetStatusEvent{
				Message:    "Select a model from the dropdown",
				Spinner:    false,
				StatusType: tui.StatusDefault,
			}
		}}
	case tui.StatusIndicatorActionThemeSelection:
		_ = app.stateManager.TransitionToView(tui.ViewStateThemeSelection)
		return []tea.Cmd{func() tea.Msg {
			return tui.SetStatusEvent{
				Message:    "",
				Spinner:    false,
				StatusType: tui.StatusDefault,
			}
		}}
	case tui.StatusIndicatorActionToolsList:
		_ = app.stateManager.TransitionToView(tui.ViewStateToolsList)
		return []tea.Cmd{func() tea.Msg {
			return tui.SetStatusEvent{
				Message:    "",
				Spinner:    false,
				StatusType: tui.StatusDefault,
			}
		}}
	case tui.StatusIndicatorActionA2AAgents:
		_ = app.stateManager.TransitionToView(tui.ViewStateA2AAgents)
		return []tea.Cmd{func() tea.Msg {
			return tui.SetStatusEvent{
				Message:    "",
				Spinner:    false,
				StatusType: tui.StatusDefault,
			}
		}}
	case tui.StatusIndicatorActionTaskManagement:
		if err := app.stateManager.TransitionToView(tui.ViewStateA2ATaskManagement); err != nil {
			return []tea.Cmd{func() tea.Msg {
				return tui.ShowErrorEvent{
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
			return tui.SetStatusEvent{
				Message:    "Task management interface",
				Spinner:    hasBackgroundTasks,
				StatusType: tui.StatusDefault,
			}
		}}
	default:
		return nil
	}
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
	case tui.ViewStateModelSelection:
		return app.renderModelSelection()
	case tui.ViewStateChat:
		return app.renderChatInterface()
	case tui.ViewStateFileSelection:
		return app.renderFileSelection()
	case tui.ViewStateConversationSelection:
		return app.renderConversationSelection()
	case tui.ViewStateThemeSelection:
		return app.renderThemeSelection()
	case tui.ViewStateA2ATaskManagement:
		return app.renderA2ATaskManagement()
	case tui.ViewStateGithubActionSetup:
		return app.renderGithubActionSetup()
	case tui.ViewStateDiffViewer:
		return app.renderDiffViewer()
	case tui.ViewStateExplorer:
		return app.renderExplorer()
	case tui.ViewStateHelp:
		return app.renderHelp()
	case tui.ViewStateToolsList:
		return app.renderToolsList()
	case tui.ViewStateA2AAgents:
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
			return tui.ShowErrorEvent{
				Error:  fmt.Sprintf("Init Github Action setup failed: %v", err),
				Sticky: false,
			}
		})
	} else {
		cmds = append(cmds, func() tea.Msg {
			return tui.SetStatusEvent{
				Message:    "Setting up Init Github Action...",
				Spinner:    true,
				StatusType: tui.StatusDefault,
			}
		})

		cmds = append(cmds, app.performGithubActionSetup(appID, privateKeyPath))
	}

	app.initGithubActionView.Reset()
	if cmd := app.initGithubActionView.Init(); cmd != nil {
		cmds = append(cmds, cmd)
	}

	if err := app.stateManager.TransitionToView(tui.ViewStateChat); err != nil {
		cmds = append(cmds, func() tea.Msg {
			return tui.ShowErrorEvent{
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
		return tui.SetStatusEvent{
			Message:    "Init Github Action setup cancelled",
			Spinner:    false,
			StatusType: tui.StatusDefault,
		}
	})

	app.initGithubActionView.Reset()
	if cmd := app.initGithubActionView.Init(); cmd != nil {
		cmds = append(cmds, cmd)
	}

	if err := app.stateManager.TransitionToView(tui.ViewStateChat); err != nil {
		cmds = append(cmds, func() tea.Msg {
			return tui.ShowErrorEvent{
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
			return tui.SetStatusEvent{
				Message:    "Checking repository...",
				Spinner:    true,
				StatusType: tui.StatusDefault,
			}
		},
		app.checkGithubSetupPreconditions(),
	}
}

// checkGithubSetupPreconditions runs the gh CLI checks off the Update loop;
// each check shells out and can take seconds.
func (app *ChatApplication) checkGithubSetupPreconditions() tea.Cmd {
	return func() tea.Msg {
		repo, err := app.githubSetupService.GetCurrentRepo()
		if err != nil {
			return githubSetupCheckedMsg{err: fmt.Errorf("failed to get repository info: %w", err)}
		}

		isOrg, err := app.githubSetupService.IsOrgRepo(repo)
		if err != nil {
			return githubSetupCheckedMsg{err: fmt.Errorf("failed to check repository type: %w", err)}
		}

		msg := githubSetupCheckedMsg{repo: repo, isOrg: isOrg}
		if isOrg {
			owner := strings.Split(repo, "/")[0]
			if msg.secretsExist, err = app.githubSetupService.CheckOrgSecretsExist(owner); err != nil {
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
			return tui.ShowErrorEvent{
				Error:  fmt.Sprintf("GitHub Action setup failed: %v", msg.err),
				Sticky: true,
			}
		})
		return cmds
	}

	if msg.isOrg && msg.secretsExist {
		cmds = append(cmds, func() tea.Msg {
			return tui.SetStatusEvent{
				Message:    "Org secrets found, creating workflow...",
				Spinner:    true,
				StatusType: tui.StatusDefault,
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

	if err := app.stateManager.TransitionToView(tui.ViewStateGithubActionSetup); err != nil {
		cmds = append(cmds, func() tea.Msg {
			return tui.ShowErrorEvent{
				Error:  fmt.Sprintf("Failed to show Init Github Action setup: %v", err),
				Sticky: false,
			}
		})
		return cmds
	}

	cmds = append(cmds, func() tea.Msg {
		return tui.SetStatusEvent{
			Message:    "Setting up Init GitHub Action...",
			Spinner:    false,
			StatusType: tui.StatusDefault,
		}
	})

	return cmds
}

func (app *ChatApplication) performGithubActionSetup(appID, privateKeyPath string) tea.Cmd {
	return func() tea.Msg {
		repo, err := app.githubSetupService.GetCurrentRepo()
		if err != nil {
			return tui.ShowErrorEvent{
				Error:  fmt.Sprintf("Failed to get repository info: %v", err),
				Sticky: true,
			}
		}

		isOrg, err := app.githubSetupService.IsOrgRepo(repo)
		if err != nil {
			return tui.ShowErrorEvent{
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
	workflowContent := app.githubSetupService.GenerateStandardWorkflowContent()
	workflowPath := ".github/workflows/infer.yml"

	if err := app.githubSetupService.WriteWorkflowFile(workflowPath, workflowContent); err != nil {
		return tui.ShowErrorEvent{
			Error:  fmt.Sprintf("Failed to write workflow file: %v", err),
			Sticky: true,
		}
	}

	prURL, err := app.githubSetupService.PreparePRCreation(repo, workflowPath)
	if err != nil {
		return tui.ShowErrorEvent{
			Error:  fmt.Sprintf("Failed to prepare PR: %v. You can manually commit and push the changes.", err),
			Sticky: true,
		}
	}

	return app.createSuccessMessage(repo, prURL, "✅ GitHub workflow configured with github-actions[bot]!")
}

func (app *ChatApplication) setupOrgWorkflow(repo, appID, privateKeyPath string) tea.Msg {
	orgName := strings.Split(repo, "/")[0]

	secretsExist, err := app.githubSetupService.CheckOrgSecretsExist(orgName)
	if err != nil {
		return tui.ShowErrorEvent{
			Error:  fmt.Sprintf("Failed to check org secrets: %v", err),
			Sticky: true,
		}
	}

	if !secretsExist && privateKeyPath != "" {
		if err := app.setupOrgSecrets(orgName, appID, privateKeyPath); err != nil {
			return err
		}
	}

	workflowContent := app.githubSetupService.GenerateGithubActionWorkflowContent()
	workflowPath := ".github/workflows/infer.yml"

	if err := app.githubSetupService.WriteWorkflowFile(workflowPath, workflowContent); err != nil {
		return tui.ShowErrorEvent{
			Error:  fmt.Sprintf("Failed to write workflow file: %v", err),
			Sticky: true,
		}
	}

	prURL, err := app.githubSetupService.PreparePRCreation(repo, workflowPath)
	if err != nil {
		return tui.ShowErrorEvent{
			Error:  fmt.Sprintf("Failed to prepare PR: %v. You can manually commit and push the changes.", err),
			Sticky: true,
		}
	}

	return app.createSuccessMessage(repo, prURL, "✅ GitHub App configured with org-level secrets!")
}

func (app *ChatApplication) setupOrgSecrets(orgName, appID, privateKeyPath string) tea.Msg {
	privateKey, err := app.fileService.ReadFile(privateKeyPath)
	if err != nil {
		return tui.ShowErrorEvent{
			Error:  fmt.Sprintf("Failed to read private key: %v", err),
			Sticky: true,
		}
	}

	if err := app.githubSetupService.SetOrgSecret(orgName, "INFER_APP_ID", appID); err != nil {
		return tui.ShowErrorEvent{
			Error:  fmt.Sprintf("Failed to set org secret INFER_APP_ID: %v", err),
			Sticky: true,
		}
	}

	if err := app.githubSetupService.SetOrgSecret(orgName, "INFER_APP_PRIVATE_KEY", privateKey); err != nil {
		return tui.ShowErrorEvent{
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
	entry := convdomain.ConversationEntry{
		Message: message,
		Time:    time.Now(),
	}
	if err := app.conversationRepo.AddMessage(entry); err != nil {
		logger.Error("failed to add pull request creation message to conversation", "error", err)
	}

	return tea.Batch(
		func() tea.Msg {
			return tui.UpdateHistoryEvent{
				History: app.conversationRepo.GetMessages(),
			}
		},
		func() tea.Msg {
			return tui.SetStatusEvent{
				Message:    "",
				Spinner:    false,
				StatusType: tui.StatusDefault,
			}
		},
	)()
}

func (app *ChatApplication) handleConversationSelectionView(msg tea.Msg) []tea.Cmd {
	var cmds []tea.Cmd

	if app.conversationSelector == nil {
		cmds = append(cmds, func() tea.Msg {
			return tui.ShowErrorEvent{
				Error:  "Conversation selection requires persistent storage (SQLite). Current storage type not supported.",
				Sticky: true,
			}
		})
		return cmds
	}

	isDone := app.conversationSelector.IsSelected() || app.conversationSelector.IsCancelled()
	needsInit := app.conversationSelector.NeedsInitialization()
	fromDifferentView := app.stateManager.GetPreviousView() != tui.ViewStateConversationSelection

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
		cmds = append(cmds, tea.Sequence(clearStatusCmd(), func() tea.Msg {
			return tui.ConversationSelectedEvent{ConversationID: selectedConv.ID}
		}))
	} else {
		cmds = append(cmds, clearStatusCmd())
	}

	if err := app.stateManager.TransitionToView(tui.ViewStateChat); err != nil {
		return []tea.Cmd{tea.Quit}
	}

	app.focusedComponent = app.inputView
	return cmds
}

func (app *ChatApplication) handleConversationCancelled(cmds []tea.Cmd) []tea.Cmd {
	cmds = append(cmds, clearStatusCmd())

	if err := app.stateManager.TransitionToView(tui.ViewStateChat); err != nil {
		return []tea.Cmd{tea.Quit}
	}

	app.focusedComponent = app.inputView
	return cmds
}

func clearStatusCmd() tea.Cmd {
	return func() tea.Msg {
		return tui.SetStatusEvent{
			Message:    "",
			Spinner:    false,
			StatusType: tui.StatusDefault,
		}
	}
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
	if err := app.stateManager.TransitionToView(tui.ViewStateChat); err != nil {
		return []tea.Cmd{tea.Quit}
	}

	app.focusedComponent = app.inputView

	cmds = append(cmds, func() tea.Msg {
		return tui.SetStatusEvent{
			Message:    "",
			Spinner:    false,
			StatusType: tui.StatusDefault,
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
			return tui.ThemeSelectedEvent{Theme: selectedTheme}
		})
	}

	return app.handleThemeCancelled(cmds)
}

func (app *ChatApplication) handleThemeCancelled(cmds []tea.Cmd) []tea.Cmd {
	if err := app.stateManager.TransitionToView(tui.ViewStateChat); err != nil {
		cmds = append(cmds, func() tea.Msg {
			return tui.ShowErrorEvent{
				Error:  fmt.Sprintf("Failed to return to chat: %v", err),
				Sticky: false,
			}
		})
	}

	app.focusedComponent = app.inputView

	cmds = append(cmds, func() tea.Msg {
		return tui.UpdateHistoryEvent{
			History: app.conversationRepo.GetMessages(),
		}
	})

	return cmds
}

// handleBackgroundColorDetected switches to the light theme when the terminal
// reports a light background. Only requested (see Init) and applied when no
// theme is configured, so an explicit config or /theme choice always wins; the
// dark default (tokyo-night) already fits dark terminals, so dark is a no-op.
func (app *ChatApplication) handleBackgroundColorDetected(msg tea.BackgroundColorMsg) {
	if msg.IsDark() || app.config.GetTheme() != "" {
		return
	}
	if err := app.themeService.SetTheme("github-light"); err != nil {
		return
	}
	app.updateAllComponentsWithNewTheme()
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
		if err := app.stateManager.TransitionToView(tui.ViewStateChat); err != nil {
			cmds = append(cmds, func() tea.Msg {
				return tui.ShowErrorEvent{
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
		if err := app.stateManager.TransitionToView(tui.ViewStateChat); err != nil {
			cmds = append(cmds, func() tea.Msg {
				return tui.ShowErrorEvent{
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

	bindings := app.collectKeyBindings()
	shortcuts := make([]tui.KeyShortcut, len(bindings))
	for i, b := range bindings {
		h := b.Help()
		shortcuts[i] = tui.KeyShortcut{Key: h.Key, Description: h.Desc}
	}
	app.helpView.SetContent(app.buildHelpCommands(), shortcuts)

	if err := app.stateManager.TransitionToView(tui.ViewStateHelp); err != nil {
		cmds = append(cmds, func() tea.Msg {
			return tui.ShowErrorEvent{
				Error:  fmt.Sprintf("Failed to show help: %v", err),
				Sticky: false,
			}
		})
		return cmds
	}

	cmds = append(cmds, func() tea.Msg {
		return tui.SetStatusEvent{
			Message:    "",
			Spinner:    false,
			StatusType: tui.StatusDefault,
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

	if err := app.stateManager.TransitionToView(tui.ViewStateChat); err != nil {
		return []tea.Cmd{tea.Quit}
	}

	app.focusedComponent = app.inputView

	cmds = append(cmds, func() tea.Msg {
		return tui.SetStatusEvent{
			Message:    "",
			Spinner:    false,
			StatusType: tui.StatusDefault,
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

	if err := app.stateManager.TransitionToView(tui.ViewStateChat); err != nil {
		return []tea.Cmd{tea.Quit}
	}

	if iv, ok := app.inputView.(*components.InputView); ok {
		iv.SetDisabled(false)
		iv.ClearCustomHint()
	}
	app.focusedComponent = app.inputView

	cmds = append(cmds, func() tea.Msg {
		return tui.SetStatusEvent{Message: "", Spinner: false, StatusType: tui.StatusDefault}
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
	return app.diffViewer.Render(app.diffViewer.FooterBar(app.diffViewer.PaneWidth()))
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

	if err := app.stateManager.TransitionToView(tui.ViewStateChat); err != nil {
		return []tea.Cmd{tea.Quit}
	}

	if iv, ok := app.inputView.(*components.InputView); ok {
		iv.SetDisabled(false)
		iv.ClearCustomHint()
	}
	app.focusedComponent = app.inputView
	app.attachmentsFocused = false

	cmds = append(cmds, func() tea.Msg {
		return tui.SetStatusEvent{Message: "", Spinner: false, StatusType: tui.StatusDefault}
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

	if err := app.stateManager.TransitionToView(tui.ViewStateChat); err != nil {
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
		return tui.SetStatusEvent{Message: status, Spinner: false, StatusType: tui.StatusDefault}
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
	if ks := app.focusAttachments.Keys(); len(ks) > 0 {
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
	if err := app.stateManager.TransitionToView(tui.ViewStateChat); err != nil {
		logger.Error("failed to transition to chat view after file selection", "error", err)
	}
	app.stateManager.ClearFileSelectionState()
}

// updateInputWithSelectedFile inserts "@<path> " for every selected file,
// including images: expandFileReferences resolves the token at send time,
// attaching the image AND replacing the token with "[Image: <path>]" so the
// model always learns the file path even without vision support. Pre-attaching
// images here (the old behavior) dropped the token, leaving non-vision models
// with no reference to the selected file at all.
func (app *ChatApplication) updateInputWithSelectedFile(selectedFile string) {
	if iv, ok := app.inputView.(*components.InputView); ok {
		iv.SetDisabled(false)
	}

	currentInput := app.inputView.GetInput()
	cursor := app.inputView.GetCursor()

	newInput, newCursor := app.fileSelectionHandler.UpdateInputWithSelectedFile(currentInput, cursor, selectedFile)

	app.inputView.SetText(newInput)
	app.inputView.SetCursor(newCursor)
}

func (app *ChatApplication) updateUIComponents(msg tea.Msg, activeView tui.ViewState) []tea.Cmd {
	var cmds []tea.Cmd

	if handled := app.handleWindowAndSetupEvents(msg, &cmds); handled {
		return cmds
	}

	if handled := app.handleDuplicateKeyEvents(msg, &cmds); handled {
		return cmds
	}

	app.updateMainUIComponents(msg, activeView, &cmds)

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

	if setupMsg, ok := msg.(tui.SetupFileSelectionEvent); ok {
		app.stateManager.SetupFileSelection(setupMsg.Files)
		return true
	}

	return false
}

// handleDuplicateKeyEvents handles duplicate key events to prevent double processing.
// lastHandledKey marks a key a chat-view handler fully consumed this cycle; the mark
// is trusted as-is because it was evaluated in-context when set - re-resolving here
// would consult the current view, which the handler may have already transitioned.
func (app *ChatApplication) handleDuplicateKeyEvents(msg tea.Msg, _ *[]tea.Cmd) bool {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok || keyMsg.String() != app.lastHandledKey {
		return false
	}

	app.lastHandledKey = ""
	return app.stateManager.GetApprovalUIState() == nil && app.stateManager.GetPlanApprovalUIState() == nil
}

// updateUIComponentsForUIMessages updates UI components for UI events and framework messages
func (app *ChatApplication) updateUIComponentsForUIMessages(msg tea.Msg, activeView tui.ViewState) []tea.Cmd {
	switch msg.(type) {
	case tea.WindowSizeMsg, tea.MouseMsg, tea.KeyPressMsg, tea.FocusMsg, tea.BlurMsg:
		return app.updateUIComponents(msg, activeView)
	}

	if shouldRouteToUIComponents(msg) {
		return app.updateUIComponents(msg, activeView)
	}

	return nil
}

// uiEventsPkgPath is resolved from a real event type so a future package move
// updates it via the compiler instead of silently breaking a string match.
var uiEventsPkgPath = reflect.TypeFor[tui.UpdateHistoryEvent]().PkgPath()

// shouldRouteToUIComponents reports whether a non-framework message is a UI or
// domain event the components should see: anything from internal/ui, anything
// from an internal */domain package, or a framework tick (spinner animation).
func shouldRouteToUIComponents(msg tea.Msg) bool {
	t := reflect.TypeOf(msg)
	if t == nil {
		return false
	}
	pkg := t.PkgPath()
	return pkg == uiEventsPkgPath ||
		(strings.HasPrefix(pkg, "github.com/inference-gateway/cli/internal/") && strings.HasSuffix(pkg, "/domain")) ||
		strings.Contains(t.String(), "Tick")
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
func (app *ChatApplication) updateMainUIComponents(msg tea.Msg, activeView tui.ViewState, cmds *[]tea.Cmd) {
	if model, cmd := app.conversationView.(tea.Model).Update(msg); cmd != nil {
		*cmds = append(*cmds, cmd)
		if convModel, ok := model.(tui.ConversationRenderer); ok {
			app.conversationView = convModel
		}
	}

	if model, cmd := app.statusView.(tea.Model).Update(msg); cmd != nil {
		*cmds = append(*cmds, cmd)
		if statusModel, ok := model.(tui.StatusComponent); ok {
			app.statusView = statusModel
		}
	}

	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		if app.shouldSkipInputKeyUpdate(keyMsg, activeView) {
			return
		}
	}

	if model, cmd := app.inputView.(tea.Model).Update(msg); cmd != nil {
		*cmds = append(*cmds, cmd)
		if inputModel, ok := model.(tui.InputComponent); ok {
			app.inputView = inputModel
		}
	}

	if model, cmd := app.helpBar.(tea.Model).Update(msg); cmd != nil {
		*cmds = append(*cmds, cmd)
		if helpBarModel, ok := model.(tui.HelpBarComponent); ok {
			app.helpBar = helpBarModel
		}
	}

	if model, cmd := app.inputStatusBar.(tea.Model).Update(msg); cmd != nil {
		*cmds = append(*cmds, cmd)
		if statusBarModel, ok := model.(tui.InputStatusBarComponent); ok {
			app.inputStatusBar = statusBarModel
		}
	}

}

func (app *ChatApplication) shouldSkipInputKeyUpdate(keyMsg tea.KeyPressMsg, activeView tui.ViewState) bool {
	if activeView != tui.ViewStateChat {
		return true
	}
	if app.inputView != nil && app.inputView.IsDisabled() {
		return true
	}
	if app.keyBindingManager == nil {
		return false
	}
	return app.keyBindingManager.ShouldSkipInputUpdate(keyMsg)
}

// updateOptionalComponents updates optional components (conversation selector, task manager)
func (app *ChatApplication) updateOptionalComponents(msg tea.Msg, cmds *[]tea.Cmd) {
	if app.conversationSelector != nil {
		switch msg.(type) {
		case tui.ConversationsLoadedEvent:
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
		case tui.TasksLoadedEvent, tui.TaskCancelledEvent:
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
	case tui.TodoUpdateEvent:
		if app.todoBoxView != nil {
			app.todoBoxView.SetTodos(todoMsg.Todos)
			app.stateManager.SetTodos(todoMsg.Todos)
			*cmds = append(*cmds, components.ScheduleAutoCollapse())
		}
	case tui.ToggleTodoBoxEvent:
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
	case tui.AutocompleteUpdateEvent:
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

	case tui.AutocompleteHideEvent:
		app.autocomplete.Hide()

	case tui.AutocompleteCompleteEvent:
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

	case agentdomain.RefreshAutocompleteEvent:
		text := app.inputView.GetInput()
		cursor := app.inputView.GetCursor()
		app.autocomplete.Update(text, cursor)
		app.inputView.SetUsageHint("")

	case tui.ClearInputEvent:
		app.autocomplete.Hide()
		app.inputView.SetUsageHint("")
	}
}

// GetServices returns the service container
func (app *ChatApplication) GetConversationRepository() convdomain.ConversationRepository {
	return app.conversationRepo
}

// GetAgentService returns the agent service
func (app *ChatApplication) GetAgentService() agentdomain.AgentService {
	return app.agentService
}

// GetImageService returns the image service
func (app *ChatApplication) GetImageService() agentdomain.ImageService {
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

// GetStateManager returns the current state manager as the narrow slice key
// handlers consume.
func (app *ChatApplication) GetStateManager() keybinding.StateManager {
	return app.stateManager
}

// Additional methods needed by keybinding system

// GetConversationView returns the conversation view
func (app *ChatApplication) GetConversationView() tui.ConversationRenderer {
	return app.conversationView
}

// GetInputView returns the input view
func (app *ChatApplication) GetInputView() tui.InputComponent {
	return app.inputView
}

// GetAutocomplete returns the autocomplete component
func (app *ChatApplication) GetAutocomplete() tui.AutocompleteComponent {
	return app.autocomplete
}

// GetStatusView returns the status view
func (app *ChatApplication) GetStatusView() tui.StatusComponent {
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

	// Keep just-sent image files on disk - the message references their path
	// so the model can inspect them via ImageDecode. Stale ones are pruned by
	// retention instead of deleted on send.
	for _, img := range images {
		if img.SourcePath != "" {
			utils.PruneClipboardImages(filepath.Dir(img.SourcePath))
			break
		}
	}

	if editing {
		editState := app.stateManager.GetMessageEditState()

		app.stateManager.ClearMessageEditState()
		if iv, ok := app.inputView.(*components.InputView); ok {
			iv.ClearCustomHint()
		}

		return func() tea.Msg {
			return agentdomain.MessageEditSubmitEvent{
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
		return agentdomain.UserInputEvent{
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

// scrollApprovalDiff scrolls the expanded approval diff by one line for up/down and
// reports whether it handled the key. It only acts while the diff is expanded so
// up/down keep their normal behaviour otherwise.
func (app *ChatApplication) scrollApprovalDiff(code rune) bool {
	if !app.approvalBoxView.IsExpanded() {
		return false
	}
	if code == tea.KeyUp {
		app.approvalBoxView.ScrollDiff(-1)
	} else {
		app.approvalBoxView.ScrollDiff(1)
	}
	return true
}

// ToggleToolResultExpansion toggles tool result expansion. While an approval is
// pending, ctrl+o instead expands the pending diff preview so it can be reviewed
// in full before approving — consistent with expanding tool results inline.
func (app *ChatApplication) ToggleToolResultExpansion() {
	if app.approvalBoxView != nil && app.approvalBoxView.IsActive() {
		app.approvalBoxView.ToggleExpanded()
		return
	}
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
func (app *ChatApplication) handleNavigateBackInTime(event agentdomain.NavigateBackInTimeEvent) []tea.Cmd {
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
func (app *ChatApplication) handleEditReady(event tui.MessageHistoryEditReadyEvent) []tea.Cmd {
	var cmds []tea.Cmd

	app.stateManager.SetMessageEditState(&tui.MessageEditState{
		OriginalMessageIndex: event.MessageIndex,
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
			return tui.ShowErrorEvent{
				Error:  fmt.Sprintf("Failed to delete messages: %v", err),
				Sticky: true,
			}
		})
		return cmds
	}

	cmds = append(cmds, func() tea.Msg {
		return tui.UpdateHistoryEvent{
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
func (app *ChatApplication) adjustRestoreIndexForEdit(entries []convdomain.ConversationEntry, restoreIndex int) int {
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
		editEvent := tui.MessageHistoryEditEvent{
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
		restoreEvent := agentdomain.MessageHistoryRestoreEvent{
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

	gk := guardKeys
	switch {
	case key.Matches(keyMsg, gk.navUp):
		cv.NavigateHistoryUp()
	case key.Matches(keyMsg, gk.navDown):
		cv.NavigateHistoryDown()
	case key.Matches(keyMsg, gk.confirm):
		cmds = app.handleMessageHistoryEnter(cv, iv, cmds)
	case key.Matches(keyMsg, gk.cancel):
		cv.ExitMessageHistoryMode()
		iv.ClearCustomHint()
	}

	return cmds
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
