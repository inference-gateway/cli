package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/inference-gateway/cli/internal/container"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/handlers"
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
	modelSelector    *ui.ModelSelectorImpl

	// Message routing
	messageRouter *handlers.MessageRouter

	// Current active component for key handling
	focusedComponent ui.InputHandler

	// Available models
	availableModels []string
}

// NewChatApplication creates a new chat application with all dependencies injected
func NewChatApplication(services *container.ServiceContainer, models []string) *ChatApplication {
	app := &ChatApplication{
		services:        services,
		availableModels: models,
		state: &handlers.AppState{
			CurrentView: handlers.ViewModelSelection,
			Width:       80,
			Height:      24,
			Data:        make(map[string]interface{}),
		},
	}

	factory := services.GetComponentFactory()
	app.conversationView = factory.CreateConversationView()
	app.inputView = factory.CreateInputView()
	app.statusView = factory.CreateStatusView()

	app.modelSelector = ui.NewModelSelector(models, services.GetModelService(), services.GetTheme())

	app.focusedComponent = nil

	app.messageRouter = services.GetMessageRouter()

	return app
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

	switch msg := msg.(type) {
	case tea.KeyMsg:
		cmds = append(cmds, func() tea.Msg {
			return ui.SetStatusMsg{
				Message: fmt.Sprintf("UPDATE: Key='%s', View=%d", msg.String(), app.state.CurrentView),
				Spinner: false,
			}
		})
	case ui.SetStatusMsg:

	default:
		cmds = append(cmds, func() tea.Msg {
			return ui.SetStatusMsg{
				Message: fmt.Sprintf("UPDATE: Msg=%T, View=%d", msg, app.state.CurrentView),
				Spinner: false,
			}
		})
	}

	if windowMsg, ok := msg.(tea.WindowSizeMsg); ok {
		app.handleResize(windowMsg)
	}

	if app.state.CurrentView != handlers.ViewApproval {
		if _, cmd := app.messageRouter.Route(msg, app.state); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	switch app.state.CurrentView {
	case handlers.ViewModelSelection:
		if model, cmd := app.modelSelector.Update(msg); cmd != nil {
			cmds = append(cmds, cmd)
			app.modelSelector = model.(*ui.ModelSelectorImpl)

			if app.modelSelector.IsSelected() {
				app.state.CurrentView = handlers.ViewChat
				app.focusedComponent = app.inputView
			} else if app.modelSelector.IsCancelled() {
				return app, tea.Quit
			}
		}

	case handlers.ViewChat:
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			if cmd := app.handleGlobalKeys(keyMsg); cmd != nil {
				cmds = append(cmds, cmd)
			}

			if app.focusedComponent != nil && app.focusedComponent.CanHandle(keyMsg) {
				if model, cmd := app.focusedComponent.HandleKey(keyMsg); cmd != nil {
					cmds = append(cmds, cmd)
					app.updateFocusedComponent(model)
				}
			}
		}

	case handlers.ViewApproval:
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
	}

	cmds = append(cmds, app.updateUIComponents(msg)...)

	return app, tea.Batch(cmds...)
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
	case handlers.ViewHelp:
		return app.renderHelp()
	default:
		return fmt.Sprintf("Unknown view state: %v", app.state.CurrentView)
	}
}

func (app *ChatApplication) renderChatInterface() string {
	var b strings.Builder

	layout := app.services.GetLayout()
	conversationHeight := layout.CalculateConversationHeight(app.state.Height)

	app.conversationView.SetWidth(app.state.Width)
	app.conversationView.SetHeight(conversationHeight)
	app.inputView.SetWidth(app.state.Width)
	app.statusView.SetWidth(app.state.Width)

	b.WriteString(app.conversationView.Render())
	b.WriteString("\n")

	b.WriteString(strings.Repeat("─", app.state.Width))
	b.WriteString("\n")

	statusContent := app.statusView.Render()
	if statusContent != "" {
		b.WriteString(statusContent)
		b.WriteString("\n")
	}

	b.WriteString(app.inputView.Render())
	b.WriteString("\n")

	b.WriteString(app.renderHelpText())

	return b.String()
}

func (app *ChatApplication) renderModelSelection() string {
	app.modelSelector.SetWidth(app.state.Width)
	app.modelSelector.SetHeight(app.state.Height)
	return app.modelSelector.View()
}

func (app *ChatApplication) renderFileSelection() string {
	return "📁 Please select a file...\n(File selection UI would be implemented here)"
}

func (app *ChatApplication) renderApproval() string {
	pendingToolCall, ok := app.state.Data["pendingToolCall"].(handlers.ToolCallRequest)
	if !ok {
		return "⚠️ No pending tool call found"
	}

	selectedIndex := 0
	if idx, ok := app.state.Data["approvalSelectedIndex"].(int); ok {
		selectedIndex = idx
	}

	var b strings.Builder
	theme := app.services.GetTheme()

	b.WriteString("🔧 Tool Execution Approval Required\n")
	b.WriteString(strings.Repeat("═", app.state.Width))
	b.WriteString("\n\n")

	b.WriteString(fmt.Sprintf("Tool: %s\n", pendingToolCall.Name))
	b.WriteString("Arguments:\n")

	for key, value := range pendingToolCall.Arguments {
		b.WriteString(fmt.Sprintf("  • %s: %v\n", key, value))
	}

	b.WriteString("\n")
	b.WriteString("⚠️  This tool will execute on your system. Please review carefully.\n\n")

	options := []string{
		"✅ Approve and execute",
		"❌ Deny and cancel",
		"👁️  View full response",
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

func (app *ChatApplication) renderHelp() string {
	commands := app.services.GetCommandRegistry().GetAll()
	var b strings.Builder

	b.WriteString("Available commands:\n\n")
	for _, cmd := range commands {
		b.WriteString("  ")
		b.WriteString(cmd.GetUsage())
		b.WriteString(" - ")
		b.WriteString(cmd.GetDescription())
		b.WriteString("\n")
	}

	return b.String()
}

func (app *ChatApplication) renderHelpText() string {
	theme := app.services.GetTheme()
	helpText := "Press Ctrl+D to send message, Ctrl+C to exit • Type @ for files, / for commands"
	return theme.GetDimColor() + helpText + "\033[0m"
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
		if app.statusView.IsShowingSpinner() {
			return func() tea.Msg {
				return ui.ShowErrorMsg{
					Error:  "Operation cancelled by user",
					Sticky: false,
				}
			}
		}
		return nil

	case "f1":
		if app.state.CurrentView == handlers.ViewHelp {
			app.state.CurrentView = handlers.ViewChat
		} else {
			app.state.CurrentView = handlers.ViewHelp
		}
		return nil
	}

	return nil
}

func (app *ChatApplication) handleApprovalKeys(keyMsg tea.KeyMsg) tea.Cmd {
	selectedIndex := 0
	if idx, ok := app.state.Data["approvalSelectedIndex"].(int); ok {
		selectedIndex = idx
	}

	switch keyMsg.String() {
	case "up", "k":
		if selectedIndex > 0 {
			selectedIndex--
		}
		app.state.Data["approvalSelectedIndex"] = selectedIndex
		return nil

	case "down", "j":
		if selectedIndex < 2 { // 3 options: 0, 1, 2
			selectedIndex++
		}
		app.state.Data["approvalSelectedIndex"] = selectedIndex
		return nil

	case "enter", "return", "ctrl+m", " ":
		switch selectedIndex {
		case 0: // Approve and execute
			return app.approveToolCall()
		case 1: // Deny and cancel
			return app.denyToolCall()
		case 2: // View full response
			if response, ok := app.state.Data["toolCallResponse"].(string); ok {
				return func() tea.Msg {
					return ui.ShowErrorMsg{
						Error:  fmt.Sprintf("Full response: %s", response),
						Sticky: true,
					}
				}
			}
			return nil
		}
		return nil

	case "esc":
		app.state.CurrentView = handlers.ViewChat
		delete(app.state.Data, "pendingToolCall")
		delete(app.state.Data, "toolCallResponse")
		delete(app.state.Data, "approvalSelectedIndex")
		return func() tea.Msg {
			return ui.SetStatusMsg{
				Message: "Tool call cancelled",
				Spinner: false,
			}
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

		if err != nil {
			return ui.ShowErrorMsg{
				Error:  fmt.Sprintf("Tool execution failed: %v", err),
				Sticky: true,
			}
		}

		toolResultEntry := domain.ConversationEntry{
			Message: sdk.Message{
				Role:    sdk.Tool,
				Content: result,
				ToolCallId: &toolCall.ID,
			},
			Time: time.Now(),
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
					Message: fmt.Sprintf("✅ Tool executed: %s - sending to model...", toolCall.Name),
					Spinner: true,
				}
			},
			func() tea.Msg {
				return ui.UpdateHistoryMsg{
					History: conversationRepo.GetMessages(),
				}
			},

			app.triggerFollowUpLLMCall(),
		)()
	}
}

// triggerFollowUpLLMCall sends the conversation with tool result back to the LLM for reasoning
func (app *ChatApplication) triggerFollowUpLLMCall() tea.Cmd {
	return func() tea.Msg {
		conversationRepo := app.services.GetConversationRepository()
		modelService := app.services.GetModelService()

		entries := conversationRepo.GetMessages()
		messages := make([]sdk.Message, len(entries))
		for i, entry := range entries {
			messages[i] = entry.Message
		}

		ctx := context.Background()
		currentModel := modelService.GetCurrentModel()
		if currentModel == "" {
			return ui.ShowErrorMsg{
				Error:  "No model selected for follow-up",
				Sticky: false,
			}
		}

		eventChan, err := app.services.GetChatService().SendMessage(ctx, currentModel, messages)
		if err != nil {
			return ui.ShowErrorMsg{
				Error:  fmt.Sprintf("Failed to send follow-up to LLM: %v", err),
				Sticky: true,
			}
		}

		return handlers.ChatStreamStartedMsg{EventChannel: eventChan}
	}
}

func (app *ChatApplication) denyToolCall() tea.Cmd {
	delete(app.state.Data, "pendingToolCall")
	delete(app.state.Data, "toolCallResponse")
	delete(app.state.Data, "approvalSelectedIndex")

	app.state.CurrentView = handlers.ViewChat

	return func() tea.Msg {
		return ui.SetStatusMsg{
			Message: "Tool call denied",
			Spinner: false,
		}
	}
}

func (app *ChatApplication) updateFocusedComponent(model tea.Model) {
	switch app.focusedComponent.(type) {
	case *ui.InputViewImpl:
		if inputModel, ok := model.(*ui.InputViewImpl); ok {
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

	return cmds
}

// GetServices returns the service container (for testing or extensions)
func (app *ChatApplication) GetServices() *container.ServiceContainer {
	return app.services
}

// GetState returns the current application state (for testing or extensions)
func (app *ChatApplication) GetState() *handlers.AppState {
	return app.state
}
