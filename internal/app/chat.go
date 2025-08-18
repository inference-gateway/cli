package app

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
func NewChatApplication(services *container.ServiceContainer, models []string, defaultModel string) *ChatApplication {
	initialView := domain.ViewStateModelSelection
	if defaultModel != "" {
		initialView = domain.ViewStateChat
	}

	app := &ChatApplication{
		services:         services,
		availableModels:  models,
		stateManager:     services.GetStateManager(),
		debugService:     services.GetDebugService(),
		toolOrchestrator: services.GetToolExecutionOrchestrator(),
	}

	// Initialize the state manager with initial view
	if err := app.stateManager.TransitionToView(initialView); err != nil {
		logger.Error("Failed to transition to initial view", "error", err)
	}

	app.conversationView = ui.CreateConversationView()
	app.inputView = ui.CreateInputView(services.GetModelService(), services.GetCommandRegistry())
	app.statusView = ui.CreateStatusView()
	app.helpBar = ui.CreateHelpBar()
	app.approvalView = ui.CreateApprovalView(services.GetTheme())
	app.fileSelectionView = components.NewFileSelectionView(services.GetTheme())

	// Initialize key binding manager early
	app.keyBindingManager = keybinding.NewKeyBindingManager(app)

	// Initialize help bar with actual commands from registry
	app.updateHelpBarShortcuts()

	app.modelSelector = components.NewModelSelector(models, services.GetModelService(), services.GetTheme())

	if initialView == domain.ViewStateChat {
		app.focusedComponent = app.inputView
	} else {
		app.focusedComponent = nil
	}

	// Create message router and register handlers
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

	if app.hasPendingApproval() {
		if cmd := app.handleApprovalKeys(keyMsg); cmd != nil {
			cmds = append(cmds, cmd)
			return cmds
		}
	}

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
		cmds = append(cmds, func() tea.Msg {
			return shared.SetStatusMsg{
				Message: fmt.Sprintf("Approval view - Key: '%s'", keyMsg.String()),
				Spinner: false,
			}
		})
		if cmd := app.handleApprovalKeys(keyMsg); cmd != nil {
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
		approvalContent := app.renderApproval()
		if app.debugService.IsEnabled() {
			snapshot := app.stateManager.GetStateSnapshot()
			return approvalContent + fmt.Sprintf("\n\n[DEBUG: CurrentView=%v, ToolExecution=%v]",
				snapshot.CurrentView, snapshot.ToolExecution != nil)
		}
		return approvalContent
	default:
		return fmt.Sprintf("Unknown view state: %v", currentView)
	}
}

func (app *ChatApplication) renderChatInterface() string {
	width, height := app.stateManager.GetDimensions()

	headerHeight := 3
	helpBarHeight := 0

	app.helpBar.SetWidth(width)
	if app.helpBar.IsEnabled() {
		helpBarHeight = 6
	}

	adjustedHeight := height - headerHeight - helpBarHeight
	conversationHeight := ui.CalculateConversationHeight(adjustedHeight)
	inputHeight := ui.CalculateInputHeight(adjustedHeight)
	statusHeight := ui.CalculateStatusHeight(adjustedHeight)

	if conversationHeight < 3 {
		conversationHeight = 3
	}

	app.conversationView.SetWidth(width)
	app.conversationView.SetHeight(conversationHeight)
	app.inputView.SetWidth(width)
	app.inputView.SetHeight(inputHeight)
	app.statusView.SetWidth(width)

	headerStyle := lipgloss.NewStyle().
		Width(width).
		Align(lipgloss.Center).
		Foreground(shared.HeaderColor.GetLipglossColor()).
		Bold(true).
		Padding(0, 1)

	header := headerStyle.Render("🚀 Inference Gateway CLI")
	headerBorder := shared.CreateSeparator(width, "═")

	conversationStyle := lipgloss.NewStyle().
		Width(width).
		Height(conversationHeight)

	inputStyle := lipgloss.NewStyle().
		Width(width)

	conversationArea := conversationStyle.Render(app.conversationView.Render())
	separator := shared.CreateSeparator(width, "─")
	inputArea := inputStyle.Render(app.inputView.Render())

	components := []string{header, headerBorder, conversationArea, separator}

	if statusHeight > 0 {
		statusContent := app.statusView.Render()
		if statusContent != "" {
			statusStyle := lipgloss.NewStyle().Width(width)
			components = append(components, statusStyle.Render(statusContent))
		}
	}

	if app.hasPendingApproval() {
		toolExecution := app.stateManager.GetToolExecution()
		selectedIndex := int(domain.ApprovalApprove)
		if approvalState := app.stateManager.GetApprovalUIState(); approvalState != nil {
			selectedIndex = approvalState.SelectedIndex
		}

		app.approvalView.SetWidth(width)
		approvalContent := app.approvalView.Render(toolExecution, selectedIndex)
		if approvalContent != "" {
			approvalStyle := lipgloss.NewStyle().Width(width)
			components = append(components, approvalStyle.Render(approvalContent))
		}
	} else {
		components = append(components, inputArea)
	}

	app.helpBar.SetWidth(width)
	helpBarContent := app.helpBar.Render()
	if helpBarContent != "" {
		separator := shared.CreateSeparator(width, "─")
		components = append(components, separator)

		helpBarStyle := lipgloss.NewStyle().
			Width(width).
			Padding(1, 1)
		components = append(components, helpBarStyle.Render(helpBarContent))
	}

	return lipgloss.JoinVertical(lipgloss.Left, components...)
}

// hasPendingApproval checks if there's a pending tool call that requires approval
func (app *ChatApplication) hasPendingApproval() bool {
	toolExecution := app.stateManager.GetToolExecution()
	return toolExecution != nil &&
		toolExecution.Status == domain.ToolExecutionStatusWaitingApproval &&
		app.stateManager.GetCurrentView() == domain.ViewStateChat
}

func (app *ChatApplication) renderModelSelection() string {
	width, height := app.stateManager.GetDimensions()
	app.modelSelector.SetWidth(width)
	app.modelSelector.SetHeight(height)
	return app.modelSelector.View()
}

func (app *ChatApplication) renderFileSelection() string {
	allFiles, searchQuery, selectedIndex := app.getFileSelectionState()

	width, _ := app.stateManager.GetDimensions()
	app.fileSelectionView.SetWidth(width)

	if allFiles != nil {
		files := app.filterFiles(allFiles, searchQuery)
		if selectedIndex >= len(files) {
			selectedIndex = 0
			app.stateManager.SetFileSelectedIndex(0)
		}
	}

	return app.fileSelectionView.RenderView(allFiles, searchQuery, selectedIndex)
}

func (app *ChatApplication) renderApproval() string {
	toolExecution := app.stateManager.GetToolExecution()
	if toolExecution == nil || !toolExecution.RequiresApproval {
		return ui.FormatWarning("No pending tool call found")
	}

	selectedIndex := int(domain.ApprovalApprove)

	var b strings.Builder
	theme := app.services.GetTheme()
	width, _ := app.stateManager.GetDimensions()

	b.WriteString("🔧 Tool Execution Approval Required\n")
	b.WriteString(strings.Repeat("═", width))
	b.WriteString("\n\n")

	currentTool := toolExecution.CurrentTool
	if currentTool == nil {
		return ui.FormatWarning("No current tool call found")
	}

	b.WriteString(fmt.Sprintf("Tool: %s\n", currentTool.Name))

	switch currentTool.Name {
	case "Edit":
		b.WriteString(app.renderEditToolArguments(currentTool.Arguments, theme))
	case "MultiEdit":
		b.WriteString(app.renderMultiEditToolArguments(currentTool.Arguments, theme))
	default:
		b.WriteString("Arguments:\n")
		if currentTool.Arguments != nil {
			for key, value := range currentTool.Arguments {
				b.WriteString(fmt.Sprintf("  • %s: %v\n", key, value))
			}
		}
	}

	b.WriteString("\n")
	b.WriteString("⚠️  This tool will execute on your system. Please review carefully.\n\n")

	options := []string{
		"✅ Approve and execute",
		"❌ Deny and cancel",
	}

	b.WriteString("Please select an action:\n\n")

	for i, option := range options {
		if i == selectedIndex {
			b.WriteString(fmt.Sprintf("%s▶ %s%s\n", theme.GetAccentColor(), option, "\033[0m"))
		} else {
			b.WriteString(fmt.Sprintf("%s  %s%s\n", theme.GetDimColor(), option, "\033[0m"))
		}
	}

	b.WriteString("\n")

	helpText := "Use ↑↓ arrow keys to navigate, SPACE to select, ESC to cancel"
	b.WriteString(theme.GetDimColor() + helpText + "\033[0m")

	return b.String()
}

// renderEditToolArguments renders Edit tool arguments with a colored diff preview
func (app *ChatApplication) renderEditToolArguments(args map[string]any, theme ui.Theme) string {
	var b strings.Builder

	filePath, _ := args["file_path"].(string)
	oldString, _ := args["old_string"].(string)
	newString, _ := args["new_string"].(string)
	replaceAll, _ := args["replace_all"].(bool)

	b.WriteString("Arguments:\n")
	b.WriteString(fmt.Sprintf("  • file_path: %s\n", filePath))
	b.WriteString(fmt.Sprintf("  • replace_all: %v\n", replaceAll))
	b.WriteString("\n")

	b.WriteString("← Test edit for diff verification →\n")
	b.WriteString(app.renderColoredDiff(oldString, newString, theme))

	return b.String()
}

// renderColoredDiff creates a colored diff view using the same logic as generateDiff
func (app *ChatApplication) renderColoredDiff(oldContent, newContent string, theme ui.Theme) string {
	if oldContent == newContent {
		return "No changes to display.\n"
	}

	oldLines := strings.Split(oldContent, "\n")
	newLines := strings.Split(newContent, "\n")

	var diff strings.Builder
	maxLines := len(oldLines)
	if len(newLines) > maxLines {
		maxLines = len(newLines)
	}

	firstChanged := -1
	lastChanged := -1
	for i := 0; i < maxLines; i++ {
		oldLine := ""
		newLine := ""
		if i < len(oldLines) {
			oldLine = oldLines[i]
		}
		if i < len(newLines) {
			newLine = newLines[i]
		}

		if oldLine != newLine {
			if firstChanged == -1 {
				firstChanged = i
			}
			lastChanged = i
		}
	}

	if firstChanged == -1 {
		return "No changes to display.\n"
	}

	contextBefore := 3
	contextAfter := 3
	startLine := firstChanged - contextBefore
	if startLine < 0 {
		startLine = 0
	}
	endLine := lastChanged + contextAfter
	if endLine >= maxLines {
		endLine = maxLines - 1
	}

	for i := startLine; i <= endLine; i++ {
		lineNum := i + 1
		app.appendDiffLine(&diff, i, lineNum, oldLines, newLines, theme)
	}

	return diff.String()
}

func (app *ChatApplication) appendDiffLine(diff *strings.Builder, i, lineNum int, oldLines, newLines []string, theme ui.Theme) {
	oldExists := i < len(oldLines)
	newExists := i < len(newLines)

	if oldExists && newExists {
		app.appendBothLinesDiff(diff, lineNum, oldLines[i], newLines[i], theme)
		return
	}

	if oldExists {
		fmt.Fprintf(diff, "%s-%3d %s\033[0m\n", theme.GetDiffRemoveColor(), lineNum, oldLines[i])
		return
	}

	if newExists {
		fmt.Fprintf(diff, "%s+%3d %s\033[0m\n", theme.GetDiffAddColor(), lineNum, newLines[i])
	}
}

func (app *ChatApplication) appendBothLinesDiff(diff *strings.Builder, lineNum int, oldLine, newLine string, theme ui.Theme) {
	if oldLine != newLine {
		fmt.Fprintf(diff, "%s-%3d %s\033[0m\n", theme.GetDiffRemoveColor(), lineNum, oldLine)
		fmt.Fprintf(diff, "%s+%3d %s\033[0m\n", theme.GetDiffAddColor(), lineNum, newLine)
	} else {
		fmt.Fprintf(diff, " %3d %s\n", lineNum, oldLine)
	}
}

// renderMultiEditToolArguments renders MultiEdit tool arguments with a colored diff preview
func (app *ChatApplication) renderMultiEditToolArguments(args map[string]any, theme ui.Theme) string {
	var b strings.Builder

	filePath, _ := args["file_path"].(string)
	editsInterface := args["edits"]

	b.WriteString("Arguments:\n")
	b.WriteString(fmt.Sprintf("  • file_path: %s\n", filePath))

	editsArray, ok := editsInterface.([]any)
	if !ok {
		b.WriteString("  • edits: [invalid format]\n")
		return b.String()
	}

	b.WriteString(fmt.Sprintf("  • edits: %d operations\n", len(editsArray)))
	b.WriteString("\n")

	b.WriteString("Edit Operations:\n")
	for i, editInterface := range editsArray {
		editMap, ok := editInterface.(map[string]any)
		if !ok {
			continue
		}

		oldString, _ := editMap["old_string"].(string)
		newString, _ := editMap["new_string"].(string)
		replaceAll, _ := editMap["replace_all"].(bool)

		b.WriteString(fmt.Sprintf("  %d. ", i+1))
		if replaceAll {
			b.WriteString("[replace_all] ")
		}

		oldPreview := strings.ReplaceAll(oldString, "\n", "\\n")
		newPreview := strings.ReplaceAll(newString, "\n", "\\n")
		if len(oldPreview) > 50 {
			oldPreview = oldPreview[:47] + "..."
		}
		if len(newPreview) > 50 {
			newPreview = newPreview[:47] + "..."
		}

		b.WriteString(fmt.Sprintf("%s\"%s\"%s → %s\"%s\"%s\n",
			theme.GetDiffRemoveColor(), oldPreview, "\033[0m",
			theme.GetDiffAddColor(), newPreview, "\033[0m"))
	}

	b.WriteString("\n← Simulated diff preview →\n")

	simulatedDiff := app.simulateMultiEditDiff(filePath, editsArray, theme)
	b.WriteString(simulatedDiff)

	return b.String()
}

// simulateMultiEditDiff simulates the multi-edit operation and generates a diff
func (app *ChatApplication) simulateMultiEditDiff(filePath string, editsArray []any, theme ui.Theme) string {
	originalContent := ""
	if content, err := os.ReadFile(filePath); err == nil {
		originalContent = string(content)
	}

	currentContent := originalContent

	for _, editInterface := range editsArray {
		editMap, ok := editInterface.(map[string]any)
		if !ok {
			continue
		}

		oldString, ok1 := editMap["old_string"].(string)
		newString, ok2 := editMap["new_string"].(string)
		replaceAll, _ := editMap["replace_all"].(bool)

		if !ok1 || !ok2 {
			continue
		}

		if !strings.Contains(currentContent, oldString) {
			return "⚠️  Edit simulation failed: old_string not found after previous edits\n"
		}

		if replaceAll {
			currentContent = strings.ReplaceAll(currentContent, oldString, newString)
		} else {
			count := strings.Count(currentContent, oldString)
			if count > 1 {
				return fmt.Sprintf("⚠️  Edit simulation failed: old_string not unique (%d occurrences)\n", count)
			}
			currentContent = strings.Replace(currentContent, oldString, newString, 1)
		}
	}

	if originalContent == currentContent {
		return "No changes to display.\n"
	}

	return app.renderColoredDiff(originalContent, currentContent, theme)
}

func (app *ChatApplication) handleFileSelectionKeys(keyMsg tea.KeyMsg) tea.Cmd {
	allFiles, searchQuery, selectedIndex := app.getFileSelectionState()
	if allFiles == nil {
		return nil
	}

	files := app.filterFiles(allFiles, searchQuery)

	switch keyMsg.String() {
	case "up":
		return app.handleFileNavigation(files, selectedIndex, -1)
	case "down":
		return app.handleFileNavigation(files, selectedIndex, 1)
	case "enter", "return":
		return app.handleFileSelection(files, selectedIndex)
	case "backspace":
		return app.handleFileSearchBackspace(searchQuery)
	case "esc":
		return app.handleFileSelectionCancel()
	default:
		return app.handleFileSearchInput(keyMsg, searchQuery)
	}
}

func (app *ChatApplication) getFileSelectionState() ([]string, string, int) {
	fileState := app.stateManager.GetFileSelectionState()
	if fileState == nil || len(fileState.Files) == 0 {
		return nil, "", 0
	}

	return fileState.Files, fileState.SearchQuery, fileState.SelectedIndex
}

func (app *ChatApplication) filterFiles(allFiles []string, searchQuery string) []string {
	if searchQuery == "" {
		return allFiles
	}

	var files []string
	for _, file := range allFiles {
		if strings.Contains(strings.ToLower(file), strings.ToLower(searchQuery)) {
			files = append(files, file)
		}
	}
	return files
}

func (app *ChatApplication) handleFileNavigation(files []string, selectedIndex, direction int) tea.Cmd {
	if len(files) == 0 {
		return nil
	}

	newIndex := selectedIndex + direction
	if newIndex >= 0 && newIndex < len(files) {
		app.stateManager.SetFileSelectedIndex(newIndex)
	}
	return nil
}

func (app *ChatApplication) handleFileSelection(files []string, selectedIndex int) tea.Cmd {
	if len(files) == 0 || selectedIndex < 0 || selectedIndex >= len(files) {
		return nil
	}

	selectedFile := files[selectedIndex]
	app.clearFileSelectionState()
	app.updateInputWithSelectedFile(selectedFile)

	return func() tea.Msg {
		return shared.SetStatusMsg{
			Message: fmt.Sprintf("📁 File selected: %s", selectedFile),
			Spinner: false,
		}
	}
}

func (app *ChatApplication) clearFileSelectionState() {
	_ = app.stateManager.TransitionToView(domain.ViewStateChat)
	app.stateManager.ClearFileSelectionState()
}

func (app *ChatApplication) updateInputWithSelectedFile(selectedFile string) {
	currentInput := app.inputView.GetInput()
	cursor := app.inputView.GetCursor()

	atIndex := app.findAtSymbolIndex(currentInput, cursor)
	newInput, newCursor := app.buildInputWithFile(currentInput, cursor, atIndex, selectedFile)

	app.inputView.SetText(newInput)
	app.inputView.SetCursor(newCursor)
}

func (app *ChatApplication) findAtSymbolIndex(input string, cursor int) int {
	for i := cursor - 1; i >= 0; i-- {
		if input[i] == '@' {
			return i
		}
	}
	return -1
}

func (app *ChatApplication) buildInputWithFile(input string, cursor, atIndex int, selectedFile string) (string, int) {
	replacement := "@" + selectedFile + " "

	if atIndex >= 0 {
		before := input[:atIndex]
		after := input[cursor:]
		return before + replacement + after, atIndex + len(replacement)
	}

	newInput := input + replacement
	return newInput, len(newInput)
}

func (app *ChatApplication) handleFileSearchBackspace(searchQuery string) tea.Cmd {
	if len(searchQuery) > 0 {
		app.stateManager.UpdateFileSearchQuery(searchQuery[:len(searchQuery)-1])
	}
	return nil
}

func (app *ChatApplication) handleFileSelectionCancel() tea.Cmd {
	app.clearFileSelectionState()
	return func() tea.Msg {
		return shared.SetStatusMsg{
			Message: "File selection cancelled",
			Spinner: false,
		}
	}
}

func (app *ChatApplication) handleFileSearchInput(keyMsg tea.KeyMsg, searchQuery string) tea.Cmd {
	if len(keyMsg.String()) == 1 && keyMsg.String()[0] >= 32 && keyMsg.String()[0] <= 126 {
		char := keyMsg.String()
		app.stateManager.UpdateFileSearchQuery(searchQuery + char)
	}
	return nil
}

func (app *ChatApplication) handleApprovalKeys(keyMsg tea.KeyMsg) tea.Cmd {
	selectedIndex := int(domain.ApprovalApprove)
	if approvalState := app.stateManager.GetApprovalUIState(); approvalState != nil {
		selectedIndex = approvalState.SelectedIndex
	}

	switch keyMsg.String() {
	case "up", "left":
		if selectedIndex > int(domain.ApprovalApprove) {
			selectedIndex--
		}
		app.stateManager.SetApprovalSelectedIndex(selectedIndex)
		return nil

	case "down", "right":
		if selectedIndex < int(domain.ApprovalReject) {
			selectedIndex++
		}
		app.stateManager.SetApprovalSelectedIndex(selectedIndex)
		return nil

	case "enter", "return", "ctrl+m", " ":
		switch domain.ApprovalAction(selectedIndex) {
		case domain.ApprovalApprove:
			return app.approveToolCall()
		case domain.ApprovalReject:
			return app.denyToolCall()
		}
		return nil

	case "esc":
		// Cancel tool execution (already in chat view with inline approval)
		app.stateManager.EndToolExecution()
		app.stateManager.ClearApprovalUIState()
		return func() tea.Msg {
			return shared.SetStatusMsg{
				Message: "Tool execution cancelled",
				Spinner: false,
			}
		}
	}

	return nil
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

	return cmds
}

// updateUIComponentsForUIMessages only updates UI components for UI-specific messages
// Business logic messages are handled by the router system
func (app *ChatApplication) updateUIComponentsForUIMessages(msg tea.Msg) []tea.Cmd {
	var cmds []tea.Cmd

	// Only process certain message types to avoid double processing
	switch msg.(type) {
	case tea.WindowSizeMsg, tea.MouseMsg:
		// UI events should always be handled by components
		return app.updateUIComponents(msg)
	case tea.KeyMsg:
		// Key events need to be handled by UI components for input/navigation
		return app.updateUIComponents(msg)
	case shared.UpdateHistoryMsg, shared.SetStatusMsg, shared.UpdateStatusMsg,
		shared.ShowErrorMsg, shared.ClearErrorMsg, shared.ClearInputMsg, shared.SetInputMsg,
		shared.ToggleHelpBarMsg, shared.HideHelpBarMsg, shared.DebugKeyMsg, shared.SetupFileSelectionMsg,
		shared.ScrollRequestMsg:
		// These are UI update messages sent by handlers - components need to process them
		return app.updateUIComponents(msg)
	case shared.UserInputMsg:
		// UserInputMsg should NOT be sent to UI components - it's handled by the router
		// This prevents double processing
		return cmds
	default:
		// Check if this might be a spinner tick message or other UI-related message
		msgType := fmt.Sprintf("%T", msg)
		if strings.Contains(msgType, "spinner.TickMsg") || strings.Contains(msgType, "Tick") {
			// Spinner tick messages should go to UI components
			return app.updateUIComponents(msg)
		}
		// For other business logic messages, don't send to UI components
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

// HandleFileNavigation handles file navigation
func (app *ChatApplication) HandleFileNavigation(direction int) tea.Cmd {
	// This would implement file navigation logic
	return nil
}

// HandleFileSelection handles file selection
func (app *ChatApplication) HandleFileSelection() tea.Cmd {
	// This would implement file selection logic
	return nil
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
