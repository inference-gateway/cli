package handlers

import (
	"fmt"

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
type UIMessageHandler struct {
	commandRegistry *commands.Registry
}

func NewUIMessageHandler(commandRegistry *commands.Registry) *UIMessageHandler {
	return &UIMessageHandler{
		commandRegistry: commandRegistry,
	}
}

func (h *UIMessageHandler) GetPriority() int { return 10 }

func (h *UIMessageHandler) CanHandle(msg tea.Msg) bool {
	switch msg.(type) {
	case ui.SetStatusMsg, ui.ShowErrorMsg, ui.ClearErrorMsg, SwitchModelMsg:
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

	case SwitchModelMsg:
		state.CurrentView = ViewModelSelection
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
