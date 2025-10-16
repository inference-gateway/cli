package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	handlers "github.com/inference-gateway/cli/internal/handlers"
	adapters "github.com/inference-gateway/cli/internal/infra/adapters"
	logger "github.com/inference-gateway/cli/internal/logger"
	services "github.com/inference-gateway/cli/internal/services"
	tools "github.com/inference-gateway/cli/internal/services/tools"
	shortcuts "github.com/inference-gateway/cli/internal/shortcuts"
	ui "github.com/inference-gateway/cli/internal/ui"
	components "github.com/inference-gateway/cli/internal/ui/components"
	keybinding "github.com/inference-gateway/cli/internal/ui/keybinding"
	shared "github.com/inference-gateway/cli/internal/ui/shared"
)

// ChatApplication represents the main application model using state management
type ChatApplication struct {
	// Dependencies
	configService    *config.Config
	agentService     domain.AgentService
	conversationRepo domain.ConversationRepository
	modelService     domain.ModelService
	toolService      domain.ToolService
	fileService      domain.FileService
	shortcutRegistry *shortcuts.Registry
	themeService     domain.ThemeService
	toolRegistry     *tools.Registry

	// State management
	stateManager domain.StateManager

	// UI components
	conversationView     ui.ConversationRenderer
	inputView            ui.InputComponent
	statusView           ui.StatusComponent
	helpBar              ui.HelpBarComponent
	modelSelector        *components.ModelSelectorImpl
	themeSelector        *components.ThemeSelectorImpl
	conversationSelector *components.ConversationSelectorImpl
	fileSelectionView    *components.FileSelectionView
	textSelectionView    *components.TextSelectionView
	a2aServersView       *components.A2AServersView
	toolCallRenderer     *components.ToolCallRenderer

	// Presentation layer
	applicationViewRenderer *components.ApplicationViewRenderer
	fileSelectionHandler    *components.FileSelectionHandler

	// Event handling
	chatHandler handlers.EventHandler

	// Current active component for key handling
	focusedComponent ui.InputComponent

	// Key binding system
	keyBindingManager *keybinding.KeyBindingManager

	// Available models
	availableModels []string
}

// NewChatApplication creates a new chat application
func NewChatApplication(
	models []string,
	defaultModel string,
	agentService domain.AgentService,
	conversationRepo domain.ConversationRepository,
	modelService domain.ModelService,
	configService *config.Config,
	toolService domain.ToolService,
	fileService domain.FileService,
	shortcutRegistry *shortcuts.Registry,
	stateManager domain.StateManager,
	themeService domain.ThemeService,
	toolRegistry *tools.Registry,
	configPath string,
) *ChatApplication {
	initialView := domain.ViewStateModelSelection
	if defaultModel != "" {
		initialView = domain.ViewStateChat
	}

	app := &ChatApplication{
		agentService:     agentService,
		conversationRepo: conversationRepo,
		modelService:     modelService,
		configService:    configService,
		toolService:      toolService,
		fileService:      fileService,
		shortcutRegistry: shortcutRegistry,
		themeService:     themeService,
		toolRegistry:     toolRegistry,
		availableModels:  models,
		stateManager:     stateManager,
	}

	if err := app.stateManager.TransitionToView(initialView); err != nil {
		logger.Error("Failed to transition to initial view", "error", err)
	}

	app.toolCallRenderer = components.NewToolCallRenderer()
	app.conversationView = ui.CreateConversationView(app.themeService)
	toolFormatterService := services.NewToolFormatterService(app.toolRegistry)
	if cv, ok := app.conversationView.(*components.ConversationView); ok {
		cv.SetToolFormatter(toolFormatterService)
		cv.SetConfigPath(configPath)
		cv.SetToolCallRenderer(app.toolCallRenderer)
	}

	configDir := ".infer"
	if configPath != "" {
		configDir = filepath.Dir(configPath)
	}

	app.inputView = ui.CreateInputViewWithToolServiceAndConfigDir(app.modelService, app.shortcutRegistry, app.toolService, configDir)
	if iv, ok := app.inputView.(*components.InputView); ok {
		iv.SetThemeService(app.themeService)
	}
	app.statusView = ui.CreateStatusView(app.themeService)
	app.helpBar = ui.CreateHelpBar(app.themeService)

	app.fileSelectionView = components.NewFileSelectionView(app.themeService)
	app.textSelectionView = components.NewTextSelectionView()

	app.applicationViewRenderer = components.NewApplicationViewRenderer(app.themeService)
	app.fileSelectionHandler = components.NewFileSelectionHandler(app.themeService)

	app.keyBindingManager = keybinding.NewKeyBindingManager(app)
	app.updateHelpBarShortcuts()

	app.modelSelector = components.NewModelSelector(models, app.modelService, app.themeService)
	app.themeSelector = components.NewThemeSelector(app.themeService)

	if persistentRepo, ok := app.conversationRepo.(*services.PersistentConversationRepository); ok {
		adapter := adapters.NewPersistentConversationAdapter(persistentRepo)
		app.conversationSelector = components.NewConversationSelector(adapter, app.themeService)
	} else {
		app.conversationSelector = nil
	}

	if initialView == domain.ViewStateChat {
		app.focusedComponent = app.inputView
	} else {
		app.focusedComponent = nil
	}

	app.chatHandler = handlers.NewChatHandler(
		app.agentService,
		app.conversationRepo,
		app.modelService,
		app.configService,
		app.toolService,
		app.fileService,
		app.shortcutRegistry,
		app.stateManager,
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

	shortcuts = append(shortcuts, ui.KeyShortcut{Key: "ctrl+v", Description: "paste text"})
	shortcuts = append(shortcuts, ui.KeyShortcut{Key: "ctrl+shift+c", Description: "copy text"})
	shortcuts = append(shortcuts, ui.KeyShortcut{Key: "alt+enter/ctrl+j", Description: "new line"})

	app.helpBar.SetShortcuts(shortcuts)
}

// updateHelpBarShortcutsForTextSelection updates help bar with vim navigation instructions
func (app *ChatApplication) updateHelpBarShortcutsForTextSelection() {
	var shortcuts []ui.KeyShortcut

	shortcuts = append(shortcuts, ui.KeyShortcut{Key: "h/j/k/l", Description: "navigate"})
	shortcuts = append(shortcuts, ui.KeyShortcut{Key: "w/b", Description: "word jump"})
	shortcuts = append(shortcuts, ui.KeyShortcut{Key: "0/$", Description: "line start/end"})
	shortcuts = append(shortcuts, ui.KeyShortcut{Key: "g/G", Description: "document start/end"})
	shortcuts = append(shortcuts, ui.KeyShortcut{Key: "v", Description: "visual mode"})
	shortcuts = append(shortcuts, ui.KeyShortcut{Key: "V", Description: "visual line"})
	shortcuts = append(shortcuts, ui.KeyShortcut{Key: "y", Description: "copy"})
	shortcuts = append(shortcuts, ui.KeyShortcut{Key: "ctrl+c", Description: "copy & exit"})
	shortcuts = append(shortcuts, ui.KeyShortcut{Key: "esc/q", Description: "exit"})

	app.helpBar.SetEnabled(true)
	app.helpBar.SetShortcuts(shortcuts)
}

// Init initializes the application
func (app *ChatApplication) Init() tea.Cmd {
	var cmds []tea.Cmd

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

	return tea.Batch(cmds...)
}

// Update handles all application messages using the state management system
func (app *ChatApplication) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	if cmd := app.chatHandler.Handle(msg); cmd != nil {
		cmds = append(cmds, cmd)
	}

	cmds = append(cmds, app.handleViewSpecificMessages(msg)...)

	cmds = append(cmds, app.updateUIComponentsForUIMessages(msg)...)

	return app, tea.Batch(cmds...)
}

func (app *ChatApplication) handleViewSpecificMessages(msg tea.Msg) []tea.Cmd {
	currentView := app.stateManager.GetCurrentView()

	switch currentView {
	case domain.ViewStateModelSelection:
		return app.handleModelSelectionView(msg)
	case domain.ViewStateChat:
		return app.handleChatView(msg)
	case domain.ViewStateFileSelection:
		return app.handleFileSelectionView(msg)
	case domain.ViewStateTextSelection:
		return app.handleTextSelectionView(msg)
	case domain.ViewStateConversationSelection:
		return app.handleConversationSelectionView(msg)
	case domain.ViewStateThemeSelection:
		return app.handleThemeSelectionView(msg)
	case domain.ViewStateA2AServers:
		return app.handleA2AServersView(msg)
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

	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return cmds
	}

	return app.handleChatViewKeyPress(keyMsg)
}

func (app *ChatApplication) handleChatViewKeyPress(keyMsg tea.KeyMsg) []tea.Cmd {
	var cmds []tea.Cmd

	if cmd := app.keyBindingManager.ProcessKey(keyMsg); cmd != nil {
		cmds = append(cmds, cmd)
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

func (app *ChatApplication) handleTextSelectionView(msg tea.Msg) []tea.Cmd {
	var cmds []tea.Cmd

	if _, ok := msg.(domain.ExitSelectionModeEvent); ok {
		return app.handleExitSelectionMode(cmds)
	}

	if _, ok := msg.(domain.InitializeTextSelectionEvent); ok {
		if conversationView, ok := app.conversationView.(*components.ConversationView); ok {
			lines := conversationView.GetPlainTextLines()
			app.textSelectionView.SetLines(lines)
		}
		return cmds
	}

	if windowMsg, ok := msg.(tea.WindowSizeMsg); ok {
		app.textSelectionView.SetWidth(windowMsg.Width)
		app.textSelectionView.SetHeight(windowMsg.Height - 5)
	}

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if cmd := app.textSelectionView.HandleKey(keyMsg); cmd != nil {
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
	case domain.ViewStateTextSelection:
		return app.renderTextSelection()
	case domain.ViewStateConversationSelection:
		return app.renderConversationSelection()
	case domain.ViewStateThemeSelection:
		return app.renderThemeSelection()
	case domain.ViewStateA2AServers:
		return app.renderA2AServers()
	default:
		return fmt.Sprintf("Unknown view state: %v", currentView)
	}
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

	// Auto-reset if selector is in any completed state (selected or cancelled) for reuse
	if app.conversationSelector.IsSelected() || app.conversationSelector.IsCancelled() {
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

func (app *ChatApplication) handleA2AServersView(msg tea.Msg) []tea.Cmd {
	var cmds []tea.Cmd

	if app.a2aServersView != nil {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "esc":
				app.a2aServersView = nil
				_ = app.stateManager.TransitionToView(domain.ViewStateChat)
				cmds = append(cmds, func() tea.Msg {
					return domain.SetStatusEvent{
						Message:    "Returned to chat",
						Spinner:    false,
						StatusType: domain.StatusDefault,
					}
				})
			}
		}

		if app.a2aServersView != nil {
			model, cmd := app.a2aServersView.Update(msg)
			app.a2aServersView = model.(*components.A2AServersView)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		return cmds
	}

	var a2aAgentService domain.A2AAgentService
	if a2aShortcut, exists := app.shortcutRegistry.Get("a2a"); exists {
		if a2a, ok := a2aShortcut.(*shortcuts.A2AShortcut); ok {
			a2aAgentService = a2a.GetA2AAgentService()
		}
	}
	app.a2aServersView = components.NewA2AServersView(app.configService, a2aAgentService, app.themeService)

	ctx := context.Background()
	if cmd := app.a2aServersView.LoadServers(ctx); cmd != nil {
		cmds = append(cmds, cmd)
	}

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
	}

	app.modelSelector = components.NewModelSelector(app.availableModels, app.modelService, app.themeService)
}

func (app *ChatApplication) renderThemeSelection() string {
	width, height := app.stateManager.GetDimensions()
	app.themeSelector.SetWidth(width)
	app.themeSelector.SetHeight(height)
	return app.themeSelector.View()
}

func (app *ChatApplication) renderA2AServers() string {
	if app.a2aServersView == nil {
		var a2aAgentService domain.A2AAgentService
		if a2aShortcut, exists := app.shortcutRegistry.Get("a2a"); exists {
			if a2a, ok := a2aShortcut.(*shortcuts.A2AShortcut); ok {
				a2aAgentService = a2a.GetA2AAgentService()
			}
		}
		app.a2aServersView = components.NewA2AServersView(app.configService, a2aAgentService, app.themeService)
	}

	width, height := app.stateManager.GetDimensions()
	app.a2aServersView.SetWidth(width)
	app.a2aServersView.SetHeight(height)
	return app.a2aServersView.View()
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

func (app *ChatApplication) renderChatInterface() string {
	app.inputView.SetTextSelectionMode(false)

	app.updateHelpBarShortcuts()

	width, height := app.stateManager.GetDimensions()
	data := components.ChatInterfaceData{
		Width:         width,
		Height:        height,
		ToolExecution: app.stateManager.GetToolExecution(),
		CurrentView:   app.stateManager.GetCurrentView(),
	}

	chatInterface := app.applicationViewRenderer.RenderChatInterface(
		data,
		app.conversationView,
		app.inputView,
		app.statusView,
		app.helpBar,
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
		return shared.FormatWarning("No files available for selection")
	}

	data := components.FileSelectionData{
		Width:         width,
		Files:         fileState.Files,
		SearchQuery:   fileState.SearchQuery,
		SelectedIndex: fileState.SelectedIndex,
	}

	return app.fileSelectionHandler.RenderFileSelection(data)
}

func (app *ChatApplication) renderTextSelection() string {
	app.inputView.SetTextSelectionMode(true)

	app.updateHelpBarShortcutsForTextSelection()

	width, height := app.stateManager.GetDimensions()

	helpBarHeight := 0
	if app.helpBar.IsEnabled() {
		helpBarHeight = 6
	}
	adjustedHeight := height - 3 - helpBarHeight
	conversationHeight := ui.CalculateConversationHeight(adjustedHeight)
	statusHeight := ui.CalculateStatusHeight(adjustedHeight)

	app.textSelectionView.SetWidth(width)
	app.textSelectionView.SetHeight(conversationHeight)
	app.statusView.SetWidth(width)

	textSelectionContent := app.textSelectionView.Render()
	inputContent := app.inputView.Render()

	components := []string{textSelectionContent}

	if statusHeight > 0 {
		statusContent := app.statusView.Render()
		if statusContent != "" {
			components = append(components, statusContent)
		}
	}

	components = append(components, inputContent)

	return strings.Join(components, "\n")
}

func (app *ChatApplication) handleExitSelectionMode(cmds []tea.Cmd) []tea.Cmd {
	app.inputView.SetTextSelectionMode(false)
	app.updateHelpBarShortcuts()

	cmds = append(cmds, func() tea.Msg {
		return domain.HideHelpBarEvent{}
	})

	if app.statusView.HasSavedState() {
		if cmd := app.statusView.RestoreSavedState(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	err := app.stateManager.TransitionToView(domain.ViewStateChat)
	if err != nil {
		logger.Error("Failed to transition back to chat view", "error", err)
	}

	return cmds
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
	_ = app.stateManager.TransitionToView(domain.ViewStateChat)
	app.stateManager.ClearFileSelectionState()
}

func (app *ChatApplication) updateInputWithSelectedFile(selectedFile string) {
	currentInput := app.inputView.GetInput()
	cursor := app.inputView.GetCursor()

	newInput, newCursor := app.fileSelectionHandler.UpdateInputWithSelectedFile(currentInput, cursor, selectedFile)

	app.inputView.SetText(newInput)
	app.inputView.SetCursor(newCursor)
}

func (app *ChatApplication) updateUIComponents(msg tea.Msg) []tea.Cmd {
	var cmds []tea.Cmd

	if windowMsg, ok := msg.(tea.WindowSizeMsg); ok {
		app.stateManager.SetDimensions(windowMsg.Width, windowMsg.Height)
	}

	if setupMsg, ok := msg.(domain.SetupFileSelectionEvent); ok {
		app.stateManager.SetupFileSelection(setupMsg.Files)
		return cmds
	}

	if model, cmd := app.conversationView.(tea.Model).Update(msg); cmd != nil {
		cmds = append(cmds, cmd)
		if convModel, ok := model.(ui.ConversationRenderer); ok {
			app.conversationView = convModel
		}
	}

	if model, cmd := app.statusView.(tea.Model).Update(msg); cmd != nil {
		cmds = append(cmds, cmd)
		if statusModel, ok := model.(ui.StatusComponent); ok {
			app.statusView = statusModel
		}
	}

	if model, cmd := app.inputView.(tea.Model).Update(msg); cmd != nil {
		cmds = append(cmds, cmd)
		if inputModel, ok := model.(ui.InputComponent); ok {
			app.inputView = inputModel
		}
	}

	if model, cmd := app.helpBar.(tea.Model).Update(msg); cmd != nil {
		cmds = append(cmds, cmd)
		if helpBarModel, ok := model.(ui.HelpBarComponent); ok {
			app.helpBar = helpBarModel
		}
	}

	if app.conversationSelector != nil {
		switch msg.(type) {
		case domain.ConversationsLoadedEvent:
			model, cmd := app.conversationSelector.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			if convSelectorModel, ok := model.(*components.ConversationSelectorImpl); ok {
				app.conversationSelector = convSelectorModel
			}
		}
	}

	return cmds
}

// updateUIComponentsForUIMessages updates UI components for UI events and framework messages
func (app *ChatApplication) updateUIComponentsForUIMessages(msg tea.Msg) []tea.Cmd {
	switch msg.(type) {
	case tea.WindowSizeMsg, tea.MouseMsg, tea.KeyMsg:
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
	conversationHeight := ui.CalculateConversationHeight(height)
	return max(1, conversationHeight-2)
}

// toggleToolResultExpansion toggles expansion of all tool results
func (app *ChatApplication) toggleToolResultExpansion() {
	app.conversationView.ToggleAllToolResultsExpansion()
}

// GetServices returns the service container
func (app *ChatApplication) GetConversationRepository() domain.ConversationRepository {
	return app.conversationRepo
}

// GetAgentService returns the agent service
func (app *ChatApplication) GetAgentService() domain.AgentService {
	return app.agentService
}

// GetConfig returns the configuration for keybinding context
func (app *ChatApplication) GetConfig() *config.Config {
	return app.configService
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
	if input == "" {
		return nil
	}

	_ = app.inputView.AddToHistory(input)

	app.inputView.ClearInput()

	return func() tea.Msg {
		return domain.UserInputEvent{
			Content: input,
		}
	}
}

// ToggleToolResultExpansion toggles tool result expansion
func (app *ChatApplication) ToggleToolResultExpansion() {
	app.toggleToolResultExpansion()
}
