package handlers

import (
	"context"
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

	// Transition to file selection view
	state.CurrentView = ViewFileSelection
	state.Data["files"] = files

	return nil, nil
}

func (h *FileMessageHandler) handleFileSelected(msg ui.FileSelectedMsg, state *AppState) (tea.Model, tea.Cmd) {
	// Validate the selected file
	if err := h.fileService.ValidateFile(msg.FilePath); err != nil {
		return nil, func() tea.Msg {
			return ui.ShowErrorMsg{
				Error:  fmt.Sprintf("Invalid file selection: %v", err),
				Sticky: false,
			}
		}
	}

	// Return to chat view
	state.CurrentView = ViewChat

	// The file reference will be handled by the input component
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

func (h *ToolMessageHandler) handleToolCall(msg domain.ToolCallEvent, state *AppState) (tea.Model, tea.Cmd) {
	return nil, func() tea.Msg {
		// Parse tool arguments
		args := make(map[string]interface{})
		// In a real implementation, you'd parse the JSON args

		// Execute the tool
		ctx := context.Background()
		_, err := h.toolService.ExecuteTool(ctx, msg.ToolName, args)

		if err != nil {
			return ui.ShowErrorMsg{
				Error:  fmt.Sprintf("Tool execution failed: %v", err),
				Sticky: false,
			}
		}

		return ui.SetStatusMsg{
			Message: fmt.Sprintf("✅ Tool '%s' executed successfully", msg.ToolName),
			Spinner: false,
		}
	}
}

// UIMessageHandler handles general UI messages
type UIMessageHandler struct{}

func NewUIMessageHandler() *UIMessageHandler {
	return &UIMessageHandler{}
}

func (h *UIMessageHandler) GetPriority() int { return 10 } // Low priority

func (h *UIMessageHandler) CanHandle(msg tea.Msg) bool {
	switch msg.(type) {
	case ui.SetStatusMsg, ui.ShowErrorMsg, ui.ClearErrorMsg, ui.ResizeMsg:
		return true
	case tea.WindowSizeMsg:
		return true
	default:
		return false
	}
}

func (h *UIMessageHandler) Handle(msg tea.Msg, state *AppState) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ui.SetStatusMsg:
		state.Status = msg.Message

	case ui.ShowErrorMsg:
		state.Error = msg.Error

	case ui.ClearErrorMsg:
		state.Error = ""

	case ui.ResizeMsg:
		state.Width = msg.Width
		state.Height = msg.Height

	case tea.WindowSizeMsg:
		state.Width = msg.Width
		state.Height = msg.Height
	}

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
		// Check if the input is a command (starts with /)
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

func (h *CommandMessageHandler) handleCommand(input string, state *AppState) (tea.Model, tea.Cmd) {
	commandName, args, err := h.commandRegistry.ParseCommand(input)
	if err != nil {
		return nil, func() tea.Msg {
			return ui.ShowErrorMsg{
				Error:  fmt.Sprintf("Invalid command: %v", err),
				Sticky: false,
			}
		}
	}

	return nil, func() tea.Msg {
		ctx := context.Background()
		result, err := h.commandRegistry.Execute(ctx, commandName, args)

		if err != nil {
			return ui.ShowErrorMsg{
				Error:  fmt.Sprintf("Command execution failed: %v", err),
				Sticky: false,
			}
		}

		if !result.Success {
			return ui.ShowErrorMsg{
				Error:  result.Output,
				Sticky: false,
			}
		}

		// Handle side effects
		switch result.SideEffect {
		case commands.SideEffectExit:
			return tea.Quit()

		case commands.SideEffectSwitchModel:
			if result.Data != nil {
				if modelID, ok := result.Data.(string); ok {
					return ui.ModelSelectedMsg{Model: modelID}
				}
			}
			// Trigger interactive model selection
			state.CurrentView = ViewModelSelection

		case commands.SideEffectClearConversation:
			return ui.UpdateHistoryMsg{History: []domain.ConversationEntry{}}
		}

		return ui.SetStatusMsg{
			Message: result.Output,
			Spinner: false,
		}
	}
}
