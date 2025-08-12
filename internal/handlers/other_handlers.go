package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbletea"
	"github.com/inference-gateway/cli/internal/commands"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/ui"
)

// FileMessageHandler handles file-related messages
type FileMessageHandler struct {
	fileService domain.FileService
}

func NewFileMessageHandler(fileService domain.FileService) *FileMessageHandler {
	return &FileMessageHandler{fileService: fileService}
}

func (h *FileMessageHandler) GetPriority() int { return 50 }

func (h *FileMessageHandler) CanHandle(msg tea.Msg) bool {
	switch msg.(type) {
	case ui.FileSelectionRequestMsg, ui.FileSelectedMsg:
		return true
	default:
		return false
	}
}

func (h *FileMessageHandler) Handle(msg tea.Msg, state *AppState) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ui.FileSelectionRequestMsg:
		return h.handleFileSelectionRequest(state)

	case ui.FileSelectedMsg:
		return h.handleFileSelected(msg, state)
	}

	return nil, nil
}

func (h *FileMessageHandler) handleFileSelectionRequest(state *AppState) (tea.Model, tea.Cmd) {
	files, err := h.fileService.ListProjectFiles()
	if err != nil {
		return nil, func() tea.Msg {
			return ui.ShowErrorMsg{
				Error:  fmt.Sprintf("Failed to list files: %v", err),
				Sticky: false,
			}
		}
	}

	if len(files) == 0 {
		return nil, func() tea.Msg {
			return ui.ShowErrorMsg{
				Error:  "No files found in current directory",
				Sticky: false,
			}
		}
	}

	state.CurrentView = ViewFileSelection
	state.Data["files"] = files

	return nil, nil
}

func (h *FileMessageHandler) handleFileSelected(msg ui.FileSelectedMsg, state *AppState) (tea.Model, tea.Cmd) {
	if err := h.fileService.ValidateFile(msg.FilePath); err != nil {
		return nil, func() tea.Msg {
			return ui.ShowErrorMsg{
				Error:  fmt.Sprintf("Invalid file selection: %v", err),
				Sticky: false,
			}
		}
	}

	state.CurrentView = ViewChat

	return nil, nil
}

// CommandSelectionHandler handles command selection UI
type CommandSelectionHandler struct {
	commandRegistry *commands.Registry
}

func NewCommandSelectionHandler(registry *commands.Registry) *CommandSelectionHandler {
	return &CommandSelectionHandler{
		commandRegistry: registry,
	}
}

func (h *CommandSelectionHandler) GetPriority() int { return 55 }

func (h *CommandSelectionHandler) CanHandle(msg tea.Msg) bool {
	switch msg.(type) {
	case ui.CommandSelectionRequestMsg, ui.CommandSelectedMsg:
		return true
	default:
		return false
	}
}

func (h *CommandSelectionHandler) Handle(msg tea.Msg, state *AppState) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ui.CommandSelectionRequestMsg:
		return h.handleCommandSelectionRequest(state)

	case ui.CommandSelectedMsg:
		return h.handleCommandSelected(msg, state)
	}

	return nil, nil
}

func (h *CommandSelectionHandler) handleCommandSelectionRequest(state *AppState) (tea.Model, tea.Cmd) {
	state.CurrentView = ViewCommandSelection
	return nil, nil
}

func (h *CommandSelectionHandler) handleCommandSelected(msg ui.CommandSelectedMsg, state *AppState) (tea.Model, tea.Cmd) {
	state.CurrentView = ViewChat

	commandName := strings.TrimPrefix(msg.Command, "/")

	cmd, exists := h.commandRegistry.Get(commandName)
	if !exists || cmd == nil {
		return nil, func() tea.Msg {
			return ui.ShowErrorMsg{
				Error:  fmt.Sprintf("Command not found: %s", commandName),
				Sticky: false,
			}
		}
	}

	ctx := context.Background()
	result, err := cmd.Execute(ctx, []string{})

	if err != nil {
		return nil, func() tea.Msg {
			return ui.ShowErrorMsg{
				Error:  fmt.Sprintf("Command execution failed: %v", err),
				Sticky: false,
			}
		}
	}

	switch result.SideEffect {
	case commands.SideEffectExit:
		return nil, tea.Quit
	case commands.SideEffectClearConversation:
		return nil, tea.Batch(
			func() tea.Msg {
				return ui.ClearInputMsg{}
			},
			func() tea.Msg {
				return ui.UpdateHistoryMsg{
					History: []domain.ConversationEntry{},
				}
			},
			func() tea.Msg {
				return ui.SetStatusMsg{
					Message: result.Output,
					Spinner: false,
				}
			},
		)
	default:
		return nil, tea.Batch(
			func() tea.Msg {
				return ui.ClearInputMsg{}
			},
			func() tea.Msg {
				return ui.SetStatusMsg{
					Message: result.Output,
					Spinner: false,
				}
			},
		)
	}
}

// ToolMessageHandler handles tool-related messages
type ToolMessageHandler struct {
	toolService domain.ToolService
}

func NewToolMessageHandler(toolService domain.ToolService) *ToolMessageHandler {
	return &ToolMessageHandler{toolService: toolService}
}

func (h *ToolMessageHandler) GetPriority() int { return 75 }

func (h *ToolMessageHandler) CanHandle(msg tea.Msg) bool {
	switch msg.(type) {
	case domain.ToolCallEvent:
		return true
	default:
		return false
	}
}

func (h *ToolMessageHandler) Handle(msg tea.Msg, state *AppState) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case domain.ToolCallEvent:
		return h.handleToolCall(msg, state)
	}

	return nil, nil
}

func (h *ToolMessageHandler) handleToolCall(event domain.ToolCallEvent, state *AppState) (tea.Model, tea.Cmd) {
	// For now, just show a status message
	return nil, func() tea.Msg {
		return ui.SetStatusMsg{
			Message: fmt.Sprintf("Tool call received: %s", event.ToolName),
			Spinner: false,
		}
	}
}

// UIMessageHandler handles UI-related messages
type UIMessageHandler struct{}

func NewUIMessageHandler() *UIMessageHandler {
	return &UIMessageHandler{}
}

func (h *UIMessageHandler) GetPriority() int { return 10 }

func (h *UIMessageHandler) CanHandle(msg tea.Msg) bool {
	switch msg.(type) {
	case ui.SetStatusMsg, ui.ShowErrorMsg, ui.ClearErrorMsg:
		return true
	default:
		return false
	}
}

func (h *UIMessageHandler) Handle(msg tea.Msg, state *AppState) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ui.SetStatusMsg:
		state.Status = msg.Message
		return nil, nil

	case ui.ShowErrorMsg:
		state.Error = msg.Error
		return nil, nil

	case ui.ClearErrorMsg:
		state.Error = ""
		return nil, nil
	}

	return nil, nil
}

// HelpMessageHandler handles help-related messages
type HelpMessageHandler struct{}

func NewHelpMessageHandler() *HelpMessageHandler {
	return &HelpMessageHandler{}
}

func (h *HelpMessageHandler) GetPriority() int { return 70 }

func (h *HelpMessageHandler) CanHandle(msg tea.Msg) bool {
	// Can handle help request messages when they're implemented
	return false
}

func (h *HelpMessageHandler) Handle(msg tea.Msg, state *AppState) (tea.Model, tea.Cmd) {
	// Implementation for help handling
	return nil, nil
}

// CommandMessageHandler handles command execution
type CommandMessageHandler struct {
	commandRegistry *commands.Registry
}

func NewCommandMessageHandler(registry *commands.Registry) *CommandMessageHandler {
	return &CommandMessageHandler{commandRegistry: registry}
}

func (h *CommandMessageHandler) GetPriority() int { return 90 }

func (h *CommandMessageHandler) CanHandle(msg tea.Msg) bool {
	switch msg := msg.(type) {
	case ui.UserInputMsg:
		return len(msg.Content) > 0 && msg.Content[0] == '/'
	default:
		return false
	}
}

func (h *CommandMessageHandler) Handle(msg tea.Msg, state *AppState) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ui.UserInputMsg:
		return h.handleCommand(msg.Content, state)
	}

	return nil, nil
}

func (h *CommandMessageHandler) handleCommand(command string, state *AppState) (tea.Model, tea.Cmd) {
	cmd, exists := h.commandRegistry.Get(command)
	if !exists {
		return nil, func() tea.Msg {
			return ui.ShowErrorMsg{
				Error:  fmt.Sprintf("Unknown command: %s", command),
				Sticky: false,
			}
		}
	}

	// Execute command asynchronously
	return nil, func() tea.Msg {
		ctx := context.Background()
		result, err := cmd.Execute(ctx, []string{})

		if err != nil {
			return ui.ShowErrorMsg{
				Error:  fmt.Sprintf("Command failed: %v", err),
				Sticky: false,
			}
		}

		return ui.SetStatusMsg{
			Message: result.Output,
			Spinner: false,
		}
	}
}
