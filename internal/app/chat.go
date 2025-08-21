package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/inference-gateway/cli/internal/container"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/handlers"
	"github.com/inference-gateway/cli/internal/logger"
	"github.com/inference-gateway/cli/internal/services"
	"github.com/inference-gateway/cli/internal/ui"
	"github.com/inference-gateway/cli/internal/ui/components"
	"github.com/inference-gateway/cli/internal/ui/keybinding"
	"github.com/inference-gateway/cli/internal/ui/shared"
)

// ChatApplication represents the main application model using state management
type ChatApplication struct {
	// Dependencies
	services *container.ServiceContainer

	// State management
	stateManager     *services.StateManager
	debugService     *services.DebugService
	toolOrchestrator *services.ToolExecutionOrchestrator

	// UI components
	conversationView  ui.ConversationRenderer
	inputView         ui.InputComponent
	statusView        ui.StatusComponent
	helpBar           ui.HelpBarComponent
	approvalView      ui.ApprovalComponent
	modelSelector     *components.ModelSelectorImpl
	fileSelectionView *components.FileSelectionView

	// Presentation layer
	applicationViewRenderer *components.ApplicationViewRenderer
	fileSelectionHandler    *components.FileSelectionHandler

	// Message routing
	messageRouter *handlers.MessageRouter

	// Current active component for key handling
	focusedComponent ui.InputComponent

	// Key binding system
	keyBindingManager *keybinding.KeyBindingManager

	// Available models
	availableModels []string
}

// NewChatApplication creates a new chat application with all dependencies injected
func NewChatApplication(serviceContainer *container.ServiceContainer, models []string, defaultModel string) *ChatApplication {
	initialView := domain.ViewStateModelSelection
	if defaultModel != "" {
		initialView = domain.ViewStateChat
	}

	app := &ChatApplication{
		services:         serviceContainer,
		availableModels:  models,
		stateManager:     serviceContainer.GetStateManager(),
		debugService:     serviceContainer.GetDebugService(),
		toolOrchestrator: serviceContainer.GetToolExecutionOrchestrator(),
	}

	if err := app.stateManager.TransitionToView(initialView); err != nil {
		logger.Error("Failed to transition to initial view", "error", err)
	}

	app.conversationView = ui.CreateConversationView()
	toolFormatterService := services.NewToolFormatterService(serviceContainer.GetToolRegistry())
	if cv, ok := app.conversationView.(*components.ConversationView); ok {
		cv.SetToolFormatter(toolFormatterService)
	}
	app.inputView = ui.CreateInputViewWithToolService(serviceContainer.GetModelService(), serviceContainer.GetCommandRegistry(), serviceContainer.GetToolService())
	app.statusView = ui.CreateStatusView()
	app.helpBar = ui.CreateHelpBar()
	app.approvalView = ui.CreateApprovalView(serviceContainer.GetTheme())
	app.fileSelectionView = components.NewFileSelectionView(serviceContainer.GetTheme())

	app.applicationViewRenderer = components.NewApplicationViewRenderer(serviceContainer.GetTheme())
	app.fileSelectionHandler = components.NewFileSelectionHandler(serviceContainer.GetTheme())

	app.keyBindingManager = keybinding.NewKeyBindingManager(app)
	app.updateHelpBarShortcuts()

	app.modelSelector = components.NewModelSelector(models, serviceContainer.GetModelService(), serviceContainer.GetTheme())

	if initialView == domain.ViewStateChat {
		app.focusedComponent = app.inputView
	} else {
		app.focusedComponent = nil
	}

	app.messageRouter = handlers.NewMessageRouter()
	app.registerHandlers()

	return app
}

// registerHandlers registers all message handlers
func (app *ChatApplication) registerHandlers() {
	chatHandler := handlers.NewChatHandler(
		app.services.GetChatService(),
		app.services.GetConversationRepository(),
		app.services.GetModelService(),
		app.services.GetConfig(),
		app.services.GetToolService(),
		app.services.GetFileService(),
		app.services.GetCommandRegistry(),
		app.toolOrchestrator,
		app.debugService,
	)
	app.messageRouter.AddHandler(chatHandler)
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
	shortcuts = append(shortcuts, ui.KeyShortcut{Key: "/", Description: "for commands"})
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

	return tea.Batch(cmds...)
}

// Update handles all application messages using the state management system
func (app *ChatApplication) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	if windowMsg, ok := msg.(tea.WindowSizeMsg); ok {
		app.stateManager.SetDimensions(windowMsg.Width, windowMsg.Height)
	}

	if _, cmd := app.messageRouter.Route(msg, app.stateManager, app.debugService); cmd != nil {
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
	case domain.ViewStateToolApproval:
		return app.handleApprovalView(msg)
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

	return app.handleChatViewKeypress(keyMsg)
}

func (app *ChatApplication) handleChatViewKeypress(keyMsg tea.KeyMsg) []tea.Cmd {
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

func (app *ChatApplication) handleApprovalView(msg tea.Msg) []tea.Cmd {
	var cmds []tea.Cmd

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		// Approval keys are now handled by the keybinding system
		if cmd := app.keyBindingManager.ProcessKey(keyMsg); cmd != nil {
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
	case domain.ViewStateToolApproval:
		return app.renderChatInterface()
	default:
		return fmt.Sprintf("Unknown view state: %v", currentView)
	}
}

func (app *ChatApplication) renderChatInterface() string {
	width, height := app.stateManager.GetDimensions()

	data := components.ChatInterfaceData{
		Width:           width,
		Height:          height,
		ToolExecution:   app.stateManager.GetToolExecution(),
		ApprovalUIState: app.stateManager.GetApprovalUIState(),
		CurrentView:     app.stateManager.GetCurrentView(),
	}

	return app.applicationViewRenderer.RenderChatInterface(
		data,
		app.conversationView,
		app.inputView,
		app.statusView,
		app.helpBar,
		app.approvalView,
	)
}

// hasPendingApproval checks if there's a pending tool call that requires approval
func (app *ChatApplication) hasPendingApproval() bool {
	toolExecution := app.stateManager.GetToolExecution()
	currentView := app.stateManager.GetCurrentView()
	return toolExecution != nil &&
		toolExecution.Status == domain.ToolExecutionStatusWaitingApproval &&
		(currentView == domain.ViewStateChat || currentView == domain.ViewStateToolApproval)
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

	// Update state based on handler response
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

func (app *ChatApplication) approveToolCall() tea.Cmd {
	toolExecution := app.stateManager.GetToolExecution()
	if toolExecution == nil || toolExecution.CurrentTool == nil {
		return func() tea.Msg {
			return shared.ShowErrorMsg{
				Error:  "No pending tool call found",
				Sticky: false,
			}
		}
	}

	app.stateManager.ClearApprovalUIState()
	_ = app.stateManager.TransitionToView(domain.ViewStateChat)

	return app.toolOrchestrator.HandleApprovalResponse(true, toolExecution.CompletedTools)
}

func (app *ChatApplication) denyToolCall() tea.Cmd {
	toolExecution := app.stateManager.GetToolExecution()
	if toolExecution == nil || toolExecution.CurrentTool == nil {
		return func() tea.Msg {
			return shared.ShowErrorMsg{
				Error:  "No pending tool call found",
				Sticky: false,
			}
		}
	}

	app.stateManager.ClearApprovalUIState()
	_ = app.stateManager.TransitionToView(domain.ViewStateChat)

	return app.toolOrchestrator.HandleApprovalResponse(false, toolExecution.CompletedTools)
}

func (app *ChatApplication) updateUIComponents(msg tea.Msg) []tea.Cmd {
	var cmds []tea.Cmd
	if setupMsg, ok := msg.(shared.SetupFileSelectionMsg); ok {
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

	if model, cmd := app.approvalView.(tea.Model).Update(msg); cmd != nil {
		cmds = append(cmds, cmd)
		if approvalModel, ok := model.(ui.ApprovalComponent); ok {
			app.approvalView = approvalModel
		}
	}

	return cmds
}

// updateUIComponentsForUIMessages only updates UI components for UI-specific messages
// Business logic messages are handled by the router system
func (app *ChatApplication) updateUIComponentsForUIMessages(msg tea.Msg) []tea.Cmd {
	var cmds []tea.Cmd

	switch msg.(type) {
	case tea.WindowSizeMsg, tea.MouseMsg:
		return app.updateUIComponents(msg)
	case tea.KeyMsg:
		return app.updateUIComponents(msg)
	case shared.UpdateHistoryMsg, shared.SetStatusMsg, shared.UpdateStatusMsg,
		shared.ShowErrorMsg, shared.ClearErrorMsg, shared.ClearInputMsg, shared.SetInputMsg,
		shared.ToggleHelpBarMsg, shared.HideHelpBarMsg, shared.DebugKeyMsg, shared.SetupFileSelectionMsg,
		shared.ScrollRequestMsg:
		return app.updateUIComponents(msg)
	case shared.UserInputMsg:
		return cmds
	default:
		msgType := fmt.Sprintf("%T", msg)
		if strings.Contains(msgType, "spinner.TickMsg") || strings.Contains(msgType, "Tick") {
			return app.updateUIComponents(msg)
		}
		return cmds
	}
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

// GetServices returns the service container (for testing or extensions)
func (app *ChatApplication) GetServices() *container.ServiceContainer {
	return app.services
}

// GetStateManager returns the current state manager (for testing or extensions)
func (app *ChatApplication) GetStateManager() *services.StateManager {
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

// HasPendingApproval checks if there's a pending approval
func (app *ChatApplication) HasPendingApproval() bool {
	return app.hasPendingApproval()
}

// GetPageSize returns the current page size for scrolling
func (app *ChatApplication) GetPageSize() int {
	return app.getPageSize()
}

// ApproveToolCall approves the current tool call
func (app *ChatApplication) ApproveToolCall() tea.Cmd {
	return app.approveToolCall()
}

// DenyToolCall denies the current tool call
func (app *ChatApplication) DenyToolCall() tea.Cmd {
	return app.denyToolCall()
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
		return shared.UserInputMsg{
			Content: input,
		}
	}
}

// ToggleToolResultExpansion toggles tool result expansion
func (app *ChatApplication) ToggleToolResultExpansion() {
	app.toggleToolResultExpansion()
}
