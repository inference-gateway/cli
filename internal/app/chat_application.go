package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/inference-gateway/cli/internal/container"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/handlers"
	"github.com/inference-gateway/cli/internal/logger"
	"github.com/inference-gateway/cli/internal/ui"
	sdk "github.com/inference-gateway/sdk"
)

// ChatApplication represents the main application model following SOLID principles
type ChatApplication struct {
	// Dependencies (injected)
	services *container.ServiceContainer

	// Application state
	state *handlers.AppState

	// UI components (injected)
	conversationView ui.ConversationRenderer
	inputView        ui.InputComponent
	statusView       ui.StatusComponent
	helpBar          ui.HelpBarComponent
	modelSelector    *ui.ModelSelectorImpl

	// Message routing
	messageRouter *handlers.MessageRouter

	// Current active component for key handling
	focusedComponent ui.InputComponent

	// Available models
	availableModels []string
}

// NewChatApplication creates a new chat application with all dependencies injected
func NewChatApplication(services *container.ServiceContainer, models []string, defaultModel string) *ChatApplication {
	initialView := handlers.ViewModelSelection
	if defaultModel != "" {
		initialView = handlers.ViewChat
	}

	app := &ChatApplication{
		services:        services,
		availableModels: models,
		state: &handlers.AppState{
			CurrentView: initialView,
			Width:       80,
			Height:      24,
			Data:        make(map[string]interface{}),
		},
	}

	app.conversationView = ui.CreateConversationView()
	app.inputView = ui.CreateInputView(services.GetModelService(), services.GetCommandRegistry())
	app.statusView = ui.CreateStatusView()
	app.helpBar = ui.CreateHelpBar()

	// Initialize help bar with actual commands from registry
	app.updateHelpBarShortcuts()

	app.modelSelector = ui.NewModelSelector(models, services.GetModelService(), services.GetTheme())

	if initialView == handlers.ViewChat {
		app.focusedComponent = app.inputView
	} else {
		app.focusedComponent = nil
	}

	app.messageRouter = services.GetMessageRouter()

	return app
}

// updateHelpBarShortcuts updates the help bar with essential keyboard shortcuts
func (app *ChatApplication) updateHelpBarShortcuts() {
	var shortcuts []ui.KeyShortcut

	shortcuts = append(shortcuts, ui.KeyShortcut{Key: "!", Description: "for bash mode"})
	shortcuts = append(shortcuts, ui.KeyShortcut{Key: "/", Description: "for commands"})
	shortcuts = append(shortcuts, ui.KeyShortcut{Key: "@", Description: "for file paths"})
	shortcuts = append(shortcuts, ui.KeyShortcut{Key: "#", Description: "to memorize(not implemented)"})

	shortcuts = append(shortcuts, ui.KeyShortcut{Key: "ctrl+d", Description: "to send message"})
	shortcuts = append(shortcuts, ui.KeyShortcut{Key: "ctrl+c", Description: "to exit"})
	shortcuts = append(shortcuts, ui.KeyShortcut{Key: "ctrl+shift+c", Description: "to copy"})
	shortcuts = append(shortcuts, ui.KeyShortcut{Key: "ctrl+v", Description: "to paste"})
	shortcuts = append(shortcuts, ui.KeyShortcut{Key: "shift+↑↓", Description: "scroll chat"})
	shortcuts = append(shortcuts, ui.KeyShortcut{Key: "home/end", Description: "scroll top/bottom"})
	shortcuts = append(shortcuts, ui.KeyShortcut{Key: "ctrl+r", Description: "expand/collapse"})
	shortcuts = append(shortcuts, ui.KeyShortcut{Key: "esc", Description: "interrupt/cancel"})

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

// Update handles all application messages using the message router pattern
func (app *ChatApplication) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if cmd := app.debugKeyBinding(keyMsg, "main"); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	cmds = append(cmds, app.handleCommonMessages(msg)...)
	cmds = append(cmds, app.handleViewSpecificMessages(msg)...)
	cmds = append(cmds, app.updateUIComponents(msg)...)

	return app, tea.Batch(cmds...)
}

func (app *ChatApplication) handleCommonMessages(msg tea.Msg) []tea.Cmd {
	var cmds []tea.Cmd

	if windowMsg, ok := msg.(tea.WindowSizeMsg); ok {
		app.handleResize(windowMsg)
	}

	if autoApproveMsg, ok := msg.(handlers.ToolAutoApproveMsg); ok {
		app.state.Data["pendingToolCall"] = autoApproveMsg.ToolCall
		app.state.Data["toolCallResponse"] = autoApproveMsg.Response
		cmds = append(cmds, app.approveToolCall())
		return cmds
	}

	if app.state.CurrentView != handlers.ViewApproval {
		if _, cmd := app.messageRouter.Route(msg, app.state); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return cmds
}

func (app *ChatApplication) handleViewSpecificMessages(msg tea.Msg) []tea.Cmd {
	switch app.state.CurrentView {
	case handlers.ViewModelSelection:
		return app.handleModelSelectionView(msg)
	case handlers.ViewChat:
		return app.handleChatView(msg)
	case handlers.ViewFileSelection:
		return app.handleFileSelectionView(msg)
	case handlers.ViewApproval:
		return app.handleApprovalView(msg)
	default:
		return nil
	}
}

func (app *ChatApplication) handleModelSelectionView(msg tea.Msg) []tea.Cmd {
	var cmds []tea.Cmd

	if model, cmd := app.modelSelector.Update(msg); cmd != nil {
		cmds = append(cmds, cmd)
		app.modelSelector = model.(*ui.ModelSelectorImpl)

		if app.modelSelector.IsSelected() {
			app.state.CurrentView = handlers.ViewChat
			app.focusedComponent = app.inputView
		} else if app.modelSelector.IsCancelled() {
			cmds = append(cmds, tea.Quit)
		}
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
	key := keyMsg.String()

	if key == "ctrl+r" {
		app.toggleToolResultExpansion()
		return cmds
	}

	if key == "ctrl+v" || key == "alt+v" || key == "ctrl+x" || key == "ctrl+shift+c" {
		if cmd := app.handleFocusedComponentKeys(keyMsg); cmd != nil {
			cmds = append(cmds, cmd)
		}
		return cmds
	}

	if cmd := app.handleScrollKeys(keyMsg); cmd != nil {
		cmds = append(cmds, cmd)
		return cmds
	}

	if cmd := app.handleGlobalKeys(keyMsg); cmd != nil {
		cmds = append(cmds, cmd)
	}

	if cmd := app.handleFocusedComponentKeys(keyMsg); cmd != nil {
		cmds = append(cmds, cmd)
	}

	return cmds
}

func (app *ChatApplication) handleFocusedComponentKeys(keyMsg tea.KeyMsg) tea.Cmd {
	if app.focusedComponent == nil || !app.focusedComponent.CanHandle(keyMsg) {
		return nil
	}

	model, cmd := app.focusedComponent.HandleKey(keyMsg)
	if cmd != nil {
		app.updateFocusedComponent(model)
	}

	return cmd
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
			return ui.SetStatusMsg{
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

// View renders the current application view
func (app *ChatApplication) View() string {
	switch app.state.CurrentView {
	case handlers.ViewModelSelection:
		return app.renderModelSelection()
	case handlers.ViewChat:
		return app.renderChatInterface()
	case handlers.ViewFileSelection:
		return app.renderFileSelection()
	case handlers.ViewApproval:
		approvalContent := app.renderApproval()
		return approvalContent + fmt.Sprintf("\n\n[DEBUG: CurrentView=%v, PendingToolCall=%v]",
			app.state.CurrentView, app.state.Data["pendingToolCall"] != nil)
	default:
		return fmt.Sprintf("Unknown view state: %v", app.state.CurrentView)
	}
}

func (app *ChatApplication) renderChatInterface() string {

	headerHeight := 3
	helpBarHeight := 0

	app.helpBar.SetWidth(app.state.Width)
	if app.helpBar.IsEnabled() {
		helpBarHeight = 6
	}

	adjustedHeight := app.state.Height - headerHeight - helpBarHeight
	conversationHeight := ui.CalculateConversationHeight(adjustedHeight)
	inputHeight := ui.CalculateInputHeight(adjustedHeight)
	statusHeight := ui.CalculateStatusHeight(adjustedHeight)

	if conversationHeight < 3 {
		conversationHeight = 3
	}

	app.conversationView.SetWidth(app.state.Width)
	app.conversationView.SetHeight(conversationHeight)
	app.inputView.SetWidth(app.state.Width)
	app.inputView.SetHeight(inputHeight)
	app.statusView.SetWidth(app.state.Width)

	headerStyle := lipgloss.NewStyle().
		Width(app.state.Width).
		Align(lipgloss.Center).
		Foreground(lipgloss.Color("39")).
		Bold(true).
		Padding(0, 1)

	titleBorderStyle := lipgloss.NewStyle().
		Width(app.state.Width).
		Foreground(lipgloss.Color("240"))

	header := headerStyle.Render("🚀 Inference Gateway CLI")
	headerBorder := titleBorderStyle.Render(strings.Repeat("═", app.state.Width))

	conversationStyle := lipgloss.NewStyle().
		Width(app.state.Width).
		Height(conversationHeight)

	separatorStyle := lipgloss.NewStyle().
		Width(app.state.Width).
		Foreground(lipgloss.Color("240"))

	inputStyle := lipgloss.NewStyle().
		Width(app.state.Width)

	conversationArea := conversationStyle.Render(app.conversationView.Render())
	separator := separatorStyle.Render(strings.Repeat("─", app.state.Width))
	inputArea := inputStyle.Render(app.inputView.Render())

	components := []string{header, headerBorder, conversationArea, separator}

	if statusHeight > 0 {
		statusContent := app.statusView.Render()
		if statusContent != "" {
			statusStyle := lipgloss.NewStyle().Width(app.state.Width)
			components = append(components, statusStyle.Render(statusContent))
		}
	}

	components = append(components, inputArea)

	app.helpBar.SetWidth(app.state.Width)
	helpBarContent := app.helpBar.Render()
	if helpBarContent != "" {
		separatorStyle := lipgloss.NewStyle().
			Width(app.state.Width).
			Foreground(lipgloss.Color("240"))
		separator := separatorStyle.Render(strings.Repeat("─", app.state.Width))
		components = append(components, separator)

		helpBarStyle := lipgloss.NewStyle().
			Width(app.state.Width).
			Padding(1, 1)
		components = append(components, helpBarStyle.Render(helpBarContent))
	}

	return lipgloss.JoinVertical(lipgloss.Left, components...)
}

func (app *ChatApplication) renderModelSelection() string {
	app.modelSelector.SetWidth(app.state.Width)
	app.modelSelector.SetHeight(app.state.Height)
	return app.modelSelector.View()
}

func (app *ChatApplication) renderFileSelection() string {
	allFiles, searchQuery, selectedIndex := app.getFileSelectionState()
	if allFiles == nil {
		return "📁 No files found in current directory\n\nPress ESC to return to chat"
	}

	files := app.filterFiles(allFiles, searchQuery)
	selectedIndex = app.validateSelectedIndex(files, selectedIndex)

	var b strings.Builder
	app.renderFileSelectionHeader(&b, files, allFiles, searchQuery)
	app.renderFileSearchField(&b, searchQuery)

	if len(files) == 0 {
		return app.renderNoFilesFound(&b, searchQuery)
	}

	app.renderFileList(&b, files, selectedIndex)
	app.renderFileSelectionFooter(&b, files)

	return b.String()
}

func (app *ChatApplication) validateSelectedIndex(files []string, selectedIndex int) int {
	if selectedIndex >= len(files) {
		selectedIndex = 0
		app.state.Data["fileSelectedIndex"] = 0
	}
	return selectedIndex
}

func (app *ChatApplication) renderFileSelectionHeader(b *strings.Builder, files, allFiles []string, searchQuery string) {
	if searchQuery != "" {
		fmt.Fprintf(b, "📁 File Search - %d matches (of %d total files):\n", len(files), len(allFiles))
	} else {
		fmt.Fprintf(b, "📁 Select a file to include in your message (%d files found):\n", len(files))
	}
	b.WriteString(strings.Repeat("═", app.state.Width))
	b.WriteString("\n\n")
}

func (app *ChatApplication) renderFileSearchField(b *strings.Builder, searchQuery string) {
	theme := app.services.GetTheme()

	b.WriteString("🔍 Search: ")
	if searchQuery != "" {
		fmt.Fprintf(b, "%s%s%s│", theme.GetUserColor(), searchQuery, "\033[0m")
	} else {
		fmt.Fprintf(b, "%stype to filter files...%s│", theme.GetDimColor(), "\033[0m")
	}
	b.WriteString("\n\n")
}

func (app *ChatApplication) renderNoFilesFound(b *strings.Builder, searchQuery string) string {
	theme := app.services.GetTheme()

	fmt.Fprintf(b, "%sNo files match '%s'%s\n\n", theme.GetErrorColor(), searchQuery, "\033[0m")
	helpText := "Type to search, BACKSPACE to clear search, ESC to cancel"
	b.WriteString(theme.GetDimColor() + helpText + "\033[0m")
	return b.String()
}

func (app *ChatApplication) renderFileList(b *strings.Builder, files []string, selectedIndex int) {
	theme := app.services.GetTheme()
	maxVisible := 12
	startIndex, endIndex := app.calculateVisibleRange(len(files), selectedIndex, maxVisible)

	for i := startIndex; i < endIndex; i++ {
		file := files[i]
		if i == selectedIndex {
			fmt.Fprintf(b, "%s▶ %s%s\n", theme.GetAccentColor(), file, "\033[0m")
		} else {
			fmt.Fprintf(b, "%s  %s%s\n", theme.GetDimColor(), file, "\033[0m")
		}
	}
}

func (app *ChatApplication) calculateVisibleRange(totalFiles, selectedIndex, maxVisible int) (int, int) {
	startIndex := 0
	if selectedIndex >= maxVisible {
		startIndex = selectedIndex - maxVisible + 1
	}
	endIndex := startIndex + maxVisible
	if endIndex > totalFiles {
		endIndex = totalFiles
	}
	return startIndex, endIndex
}

func (app *ChatApplication) renderFileSelectionFooter(b *strings.Builder, files []string) {
	theme := app.services.GetTheme()
	maxVisible := 12

	b.WriteString("\n")

	if len(files) > maxVisible {
		selectedIndex := 0
		if idx, ok := app.state.Data["fileSelectedIndex"].(int); ok {
			selectedIndex = idx
		}
		startIndex, endIndex := app.calculateVisibleRange(len(files), selectedIndex, maxVisible)
		fmt.Fprintf(b, "%sShowing %d-%d of %d matches%s\n",
			theme.GetDimColor(), startIndex+1, endIndex, len(files), "\033[0m")
		b.WriteString("\n")
	}

	helpText := "Type to search, ↑↓ to navigate, ENTER to select, BACKSPACE to clear, ESC to cancel"
	b.WriteString(theme.GetDimColor() + helpText + "\033[0m")
}

func (app *ChatApplication) renderApproval() string {
	pendingToolCall, ok := app.state.Data["pendingToolCall"].(handlers.ToolCallRequest)
	if !ok {
		return ui.FormatWarning("No pending tool call found")
	}

	selectedIndex := int(domain.ApprovalApprove)
	if idx, ok := app.state.Data["approvalSelectedIndex"].(int); ok {
		selectedIndex = idx
	}

	var b strings.Builder
	theme := app.services.GetTheme()

	b.WriteString("🔧 Tool Execution Approval Required\n")
	b.WriteString(strings.Repeat("═", app.state.Width))
	b.WriteString("\n\n")

	b.WriteString(fmt.Sprintf("Tool: %s\n", pendingToolCall.String()))
	b.WriteString("Arguments:\n")

	for key, value := range pendingToolCall.Arguments {
		b.WriteString(fmt.Sprintf("  • %s: %v\n", key, value))
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

func (app *ChatApplication) handleResize(msg tea.WindowSizeMsg) {
	app.state.Width = msg.Width
	app.state.Height = msg.Height
}

func (app *ChatApplication) handleGlobalKeys(keyMsg tea.KeyMsg) tea.Cmd {
	switch keyMsg.String() {
	case "ctrl+c":
		return tea.Quit

	case "tab":
		return nil

	case "esc":
		if requestID, ok := app.state.Data["currentRequestID"].(string); ok {
			chatService := app.services.GetChatService()
			if err := chatService.CancelRequest(requestID); err == nil {
				delete(app.state.Data, "currentRequestID")
				delete(app.state.Data, "eventChannel")

				return func() tea.Msg {
					return ui.SetStatusMsg{
						Message: "Response cancelled",
						Spinner: false,
					}
				}
			}
		}
		return nil

	}

	return nil
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
	allFiles, ok := app.state.Data["files"].([]string)
	if !ok || len(allFiles) == 0 {
		return nil, "", 0
	}

	searchQuery := ""
	if query, ok := app.state.Data["fileSearchQuery"].(string); ok {
		searchQuery = query
	}

	selectedIndex := 0
	if idx, ok := app.state.Data["fileSelectedIndex"].(int); ok {
		selectedIndex = idx
	}

	return allFiles, searchQuery, selectedIndex
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
		app.state.Data["fileSelectedIndex"] = newIndex
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
		return ui.SetStatusMsg{
			Message: fmt.Sprintf("📁 File selected: %s", selectedFile),
			Spinner: false,
		}
	}
}

func (app *ChatApplication) clearFileSelectionState() {
	app.state.CurrentView = handlers.ViewChat
	delete(app.state.Data, "files")
	delete(app.state.Data, "fileSelectedIndex")
	delete(app.state.Data, "fileSearchQuery")
}

func (app *ChatApplication) updateInputWithSelectedFile(selectedFile string) {
	currentInput := app.inputView.GetInput()
	cursor := app.inputView.GetCursor()

	atIndex := app.findAtSymbolIndex(currentInput, cursor)
	newInput, newCursor := app.buildInputWithFile(currentInput, cursor, atIndex, selectedFile)

	if inputImpl, ok := app.inputView.(*ui.InputView); ok {
		inputImpl.SetText(newInput)
		inputImpl.SetCursor(newCursor)
	}
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
		app.state.Data["fileSearchQuery"] = searchQuery[:len(searchQuery)-1]
		app.state.Data["fileSelectedIndex"] = 0
	}
	return nil
}

func (app *ChatApplication) handleFileSelectionCancel() tea.Cmd {
	app.clearFileSelectionState()
	return func() tea.Msg {
		return ui.SetStatusMsg{
			Message: "File selection cancelled",
			Spinner: false,
		}
	}
}

func (app *ChatApplication) handleFileSearchInput(keyMsg tea.KeyMsg, searchQuery string) tea.Cmd {
	if len(keyMsg.String()) == 1 && keyMsg.String()[0] >= 32 && keyMsg.String()[0] <= 126 {
		char := keyMsg.String()
		app.state.Data["fileSearchQuery"] = searchQuery + char
		app.state.Data["fileSelectedIndex"] = 0
	}
	return nil
}

func (app *ChatApplication) handleApprovalKeys(keyMsg tea.KeyMsg) tea.Cmd {
	selectedIndex := int(domain.ApprovalApprove)
	if idx, ok := app.state.Data["approvalSelectedIndex"].(int); ok {
		selectedIndex = idx
	}

	switch keyMsg.String() {
	case "up":
		if selectedIndex > int(domain.ApprovalApprove) {
			selectedIndex--
		}
		app.state.Data["approvalSelectedIndex"] = selectedIndex
		return nil

	case "down":
		if selectedIndex < int(domain.ApprovalReject) {
			selectedIndex++
		}
		app.state.Data["approvalSelectedIndex"] = selectedIndex
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
		toolCall, ok := app.state.Data["pendingToolCall"].(handlers.ToolCallRequest)
		if !ok {
			app.state.CurrentView = handlers.ViewChat
			delete(app.state.Data, "pendingToolCall")
			delete(app.state.Data, "toolCallResponse")
			delete(app.state.Data, "approvalSelectedIndex")
			return func() tea.Msg {
				return ui.SetStatusMsg{
					Message: "Tool call cancelled - no pending call found",
					Spinner: false,
				}
			}
		}

		app.state.CurrentView = handlers.ViewChat
		delete(app.state.Data, "pendingToolCall")
		delete(app.state.Data, "toolCallResponse")
		delete(app.state.Data, "approvalSelectedIndex")

		return func() tea.Msg {
			cancelledResult := &domain.ToolExecutionResult{
				ToolName:  toolCall.Name,
				Arguments: toolCall.Arguments,
				Success:   false,
				Duration:  0,
				Error:     "Cancelled by user",
			}

			toolResultEntry := domain.ConversationEntry{
				Message: sdk.Message{
					Role:       sdk.Tool,
					Content:    "Tool execution cancelled by user.",
					ToolCallId: &toolCall.ID,
				},
				Time:          time.Now(),
				ToolExecution: cancelledResult,
			}

			conversationRepo := app.services.GetConversationRepository()
			if err := conversationRepo.AddMessage(toolResultEntry); err != nil {
				return ui.ShowErrorMsg{
					Error:  fmt.Sprintf("Failed to save tool result: %v", err),
					Sticky: false,
				}
			}

			return tea.Batch(
				func() tea.Msg {
					return ui.SetStatusMsg{
						Message: "Tool call cancelled",
						Spinner: false,
					}
				},
				func() tea.Msg {
					return ui.UpdateHistoryMsg{
						History: conversationRepo.GetMessages(),
					}
				},
				func() tea.Msg {
					return handlers.ProcessNextToolCallMsg{}
				},
			)()
		}

	default:
		return func() tea.Msg {
			return ui.SetStatusMsg{
				Message: fmt.Sprintf("Key pressed: '%s' (selection: %d)", keyMsg.String(), selectedIndex),
				Spinner: false,
			}
		}
	}
}

func (app *ChatApplication) approveToolCall() tea.Cmd {
	toolCall, ok := app.state.Data["pendingToolCall"].(handlers.ToolCallRequest)
	if !ok {
		return func() tea.Msg {
			return ui.ShowErrorMsg{
				Error:  "No pending tool call found",
				Sticky: false,
			}
		}
	}

	delete(app.state.Data, "pendingToolCall")
	delete(app.state.Data, "toolCallResponse")
	delete(app.state.Data, "approvalSelectedIndex")
	app.state.CurrentView = handlers.ViewChat

	return func() tea.Msg {
		toolService := app.services.GetToolService()
		result, err := toolService.ExecuteTool(context.Background(), toolCall.Name, toolCall.Arguments)

		var toolContent string
		var toolResult *domain.ToolExecutionResult
		if err != nil {
			failedResult := &domain.ToolExecutionResult{
				ToolName:  toolCall.Name,
				Arguments: toolCall.Arguments,
				Success:   false,
				Duration:  0,
				Error:     err.Error(),
			}
			toolResult = failedResult
			toolContent = ui.FormatToolResultForLLM(failedResult)
		} else {
			toolResult = result
			toolContent = ui.FormatToolResultForLLM(result)
		}

		toolResultEntry := domain.ConversationEntry{
			Message: sdk.Message{
				Role:       sdk.Tool,
				Content:    toolContent,
				ToolCallId: &toolCall.ID,
			},
			Time:          time.Now(),
			ToolExecution: toolResult,
		}

		conversationRepo := app.services.GetConversationRepository()
		if saveErr := conversationRepo.AddMessage(toolResultEntry); saveErr != nil {
			return ui.ShowErrorMsg{
				Error:  fmt.Sprintf("Failed to save tool result: %v", saveErr),
				Sticky: false,
			}
		}

		var statusMessage string
		if err != nil {
			statusMessage = fmt.Sprintf("Tool failed: %s - sending error to model...", toolCall.String())
		} else {
			statusMessage = ui.FormatSuccess(fmt.Sprintf("Tool executed: %s - sending to model...", toolCall.String()))
		}

		return tea.Batch(
			func() tea.Msg {
				return ui.SetStatusMsg{
					Message: statusMessage,
					Spinner: true,
				}
			},
			func() tea.Msg {
				return ui.UpdateHistoryMsg{
					History: conversationRepo.GetMessages(),
				}
			},
			func() tea.Msg {
				return handlers.ProcessNextToolCallMsg{}
			},
		)()
	}
}

func (app *ChatApplication) denyToolCall() tea.Cmd {
	toolCall, ok := app.state.Data["pendingToolCall"].(handlers.ToolCallRequest)
	if !ok {
		delete(app.state.Data, "pendingToolCall")
		delete(app.state.Data, "toolCallResponse")
		delete(app.state.Data, "approvalSelectedIndex")
		app.state.CurrentView = handlers.ViewChat
		return func() tea.Msg {
			return ui.SetStatusMsg{
				Message: "Tool call denied - no pending call found",
				Spinner: false,
			}
		}
	}

	delete(app.state.Data, "pendingToolCall")
	delete(app.state.Data, "toolCallResponse")
	delete(app.state.Data, "approvalSelectedIndex")

	app.state.CurrentView = handlers.ViewChat

	return func() tea.Msg {
		deniedResult := &domain.ToolExecutionResult{
			ToolName:  toolCall.Name,
			Arguments: toolCall.Arguments,
			Success:   false,
			Duration:  0,
			Error:     "Denied by user",
		}

		toolResultEntry := domain.ConversationEntry{
			Message: sdk.Message{
				Role:       sdk.Tool,
				Content:    "Tool execution denied by user.",
				ToolCallId: &toolCall.ID,
			},
			Time:          time.Now(),
			ToolExecution: deniedResult,
		}

		conversationRepo := app.services.GetConversationRepository()
		if err := conversationRepo.AddMessage(toolResultEntry); err != nil {
			return ui.ShowErrorMsg{
				Error:  fmt.Sprintf("Failed to save tool result: %v", err),
				Sticky: false,
			}
		}

		return tea.Batch(
			func() tea.Msg {
				return ui.SetStatusMsg{
					Message: "Tool call denied",
					Spinner: false,
				}
			},
			func() tea.Msg {
				return ui.UpdateHistoryMsg{
					History: conversationRepo.GetMessages(),
				}
			},
			func() tea.Msg {
				return handlers.ProcessNextToolCallMsg{}
			},
		)()
	}
}

func (app *ChatApplication) updateFocusedComponent(model tea.Model) {
	switch app.focusedComponent.(type) {
	case *ui.InputView:
		if inputModel, ok := model.(*ui.InputView); ok {
			app.inputView = inputModel
			app.focusedComponent = inputModel
		}
	}
}

func (app *ChatApplication) updateUIComponents(msg tea.Msg) []tea.Cmd {
	var cmds []tea.Cmd

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

func (app *ChatApplication) handleScrollKeys(keyMsg tea.KeyMsg) tea.Cmd {
	keyStr := keyMsg.String()

	if strings.Contains(keyStr, "ctrl+") || strings.Contains(keyStr, "cmd+") || strings.Contains(keyStr, "alt+") {
		return nil
	}

	switch keyStr {
	case "home":
		return func() tea.Msg {
			return ui.ScrollRequestMsg{
				ComponentID: "conversation",
				Direction:   ui.ScrollToTop,
				Amount:      0,
			}
		}
	case "end":
		return func() tea.Msg {
			return ui.ScrollRequestMsg{
				ComponentID: "conversation",
				Direction:   ui.ScrollToBottom,
				Amount:      0,
			}
		}
	case "shift+up":
		return func() tea.Msg {
			return ui.ScrollRequestMsg{
				ComponentID: "conversation",
				Direction:   ui.ScrollUp,
				Amount:      app.getPageSize() / 2,
			}
		}
	case "shift+down":
		return func() tea.Msg {
			return ui.ScrollRequestMsg{
				ComponentID: "conversation",
				Direction:   ui.ScrollDown,
				Amount:      app.getPageSize() / 2,
			}
		}
	}
	return nil
}

func (app *ChatApplication) getPageSize() int {
	conversationHeight := ui.CalculateConversationHeight(app.state.Height)
	return max(1, conversationHeight-2)
}

// toggleToolResultExpansion toggles expansion of the most recent tool result
func (app *ChatApplication) toggleToolResultExpansion() {
	conversationRepo := app.services.GetConversationRepository()
	messages := conversationRepo.GetMessages()

	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Message.Role == "tool" {
			app.conversationView.ToggleToolResultExpansion(i)
			break
		}
	}
}

// GetServices returns the service container (for testing or extensions)
func (app *ChatApplication) GetServices() *container.ServiceContainer {
	return app.services
}

// GetState returns the current application state (for testing or extensions)
func (app *ChatApplication) GetState() *handlers.AppState {
	return app.state
}

// debugKeyBinding logs key binding events when debug mode is enabled
func (app *ChatApplication) debugKeyBinding(keyMsg tea.KeyMsg, handler string) tea.Cmd {
	config := app.services.GetConfig()
	if config != nil && config.Output.Debug {
		logger.Debug("Key binding debug",
			"key", keyMsg.String(),
			"handler", handler,
			"type", keyMsg.Type,
			"alt", keyMsg.Alt,
			"runes", string(keyMsg.Runes))

		return func() tea.Msg {
			return ui.DebugKeyMsg{
				Key:     keyMsg.String(),
				Handler: handler,
			}
		}
	}
	return nil
}
