package keybinding

import (
	"strings"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/logger"
	"github.com/inference-gateway/cli/internal/ui/shared"
)

// registerDefaultBindings registers all default key bindings
func (r *Registry) registerDefaultBindings() {
	globalActions := r.createGlobalActions()
	chatActions := r.createChatActions()
	scrollActions := r.createScrollActions()

	r.registerActionsToLayers(globalActions, chatActions, scrollActions)
}

// createGlobalActions creates global key actions available in all views
func (r *Registry) createGlobalActions() []*KeyAction {
	return []*KeyAction{
		{
			ID:          "quit",
			Keys:        []string{"ctrl+c"},
			Description: "exit application",
			Category:    "global",
			Handler:     handleQuit,
			Priority:    100,
			Enabled:     true,
			Context: KeyContext{
				Views: []domain.ViewState{},
			},
		},
		{
			ID:          "cancel",
			Keys:        []string{"esc"},
			Description: "cancel current operation",
			Category:    "global",
			Handler:     handleCancel,
			Priority:    100,
			Enabled:     true,
			Context: KeyContext{
				Views: []domain.ViewState{},
			},
		},
	}
}

// createChatActions creates key actions specific to chat view
func (r *Registry) createChatActions() []*KeyAction {
	actions := []*KeyAction{
		{
			ID:          "toggle_tool_expansion",
			Keys:        []string{"ctrl+r"},
			Description: "expand/collapse tool results",
			Category:    "tools",
			Handler:     handleToggleToolExpansion,
			Priority:    150,
			Enabled:     true,
			Context: KeyContext{
				Views: []domain.ViewState{domain.ViewStateChat},
			},
		},
		{
			ID:          "send_message",
			Keys:        []string{"enter"},
			Description: "send message",
			Category:    "chat",
			Handler:     handleSendMessage,
			Priority:    150,
			Enabled:     true,
			Context: KeyContext{
				Views: []domain.ViewState{domain.ViewStateChat},
				Conditions: []ContextCondition{
					{
						Name: "input_has_content",
						Check: func(app KeyHandlerContext) bool {
							input := app.GetInputView().GetInput()
							return len(input) > 0
						},
					},
				},
			},
		},
		{
			ID:          "toggle_help",
			Keys:        []string{"?"},
			Description: "toggle help when input is empty",
			Category:    "help",
			Handler:     handleToggleHelp,
			Priority:    200,
			Enabled:     true,
			Context: KeyContext{
				Views: []domain.ViewState{domain.ViewStateChat},
				Conditions: []ContextCondition{
					{
						Name: "input_is_empty",
						Check: func(app KeyHandlerContext) bool {
							input := strings.TrimSpace(app.GetInputView().GetInput())
							return len(input) == 0
						},
					},
				},
			},
		},
	}

	// Add clipboard actions
	actions = append(actions, r.createClipboardActions()...)
	// Add text editing actions
	actions = append(actions, r.createTextEditingActions()...)
	// Add history actions
	actions = append(actions, r.createHistoryActions()...)

	return actions
}

// createClipboardActions creates clipboard-related key actions
func (r *Registry) createClipboardActions() []*KeyAction {
	return []*KeyAction{
		{
			ID:          "paste_text",
			Keys:        []string{"ctrl+v"},
			Description: "paste text",
			Category:    "clipboard",
			Handler:     handlePaste,
			Priority:    200,
			Enabled:     true,
			Context: KeyContext{
				Views: []domain.ViewState{domain.ViewStateChat},
			},
		},
		{
			ID:          "copy_text",
			Keys:        []string{"ctrl+shift+c"},
			Description: "copy text",
			Category:    "clipboard",
			Handler:     handleCopy,
			Priority:    200,
			Enabled:     true,
			Context: KeyContext{
				Views: []domain.ViewState{domain.ViewStateChat},
			},
		},
	}
}

// createTextEditingActions creates text editing key actions
func (r *Registry) createTextEditingActions() []*KeyAction {
	return []*KeyAction{
		{
			ID:          "move_cursor_left",
			Keys:        []string{"left"},
			Description: "move cursor left",
			Category:    "text_editing",
			Handler:     handleCursorLeft,
			Priority:    200,
			Enabled:     true,
			Context: KeyContext{
				Views: []domain.ViewState{domain.ViewStateChat},
			},
		},
		{
			ID:          "move_cursor_right",
			Keys:        []string{"right"},
			Description: "move cursor right",
			Category:    "text_editing",
			Handler:     handleCursorRight,
			Priority:    200,
			Enabled:     true,
			Context: KeyContext{
				Views: []domain.ViewState{domain.ViewStateChat},
			},
		},
		{
			ID:          "backspace",
			Keys:        []string{"backspace"},
			Description: "delete character",
			Category:    "text_editing",
			Handler:     handleBackspace,
			Priority:    200,
			Enabled:     true,
			Context: KeyContext{
				Views: []domain.ViewState{domain.ViewStateChat},
			},
		},
		{
			ID:          "delete_to_beginning",
			Keys:        []string{"ctrl+u"},
			Description: "delete to beginning of line",
			Category:    "text_editing",
			Handler:     handleDeleteToBeginning,
			Priority:    200,
			Enabled:     true,
			Context: KeyContext{
				Views: []domain.ViewState{domain.ViewStateChat},
			},
		},
		{
			ID:          "delete_word_backward",
			Keys:        []string{"ctrl+w"},
			Description: "delete word backward",
			Category:    "text_editing",
			Handler:     handleDeleteWordBackward,
			Priority:    200,
			Enabled:     true,
			Context: KeyContext{
				Views: []domain.ViewState{domain.ViewStateChat},
			},
		},
		{
			ID:          "move_to_beginning",
			Keys:        []string{"ctrl+a"},
			Description: "move cursor to beginning",
			Category:    "text_editing",
			Handler:     handleMoveToBeginning,
			Priority:    200,
			Enabled:     true,
			Context: KeyContext{
				Views: []domain.ViewState{domain.ViewStateChat},
			},
		},
		{
			ID:          "move_to_end",
			Keys:        []string{"ctrl+e"},
			Description: "move cursor to end",
			Category:    "text_editing",
			Handler:     handleMoveToEnd,
			Priority:    200,
			Enabled:     true,
			Context: KeyContext{
				Views: []domain.ViewState{domain.ViewStateChat},
			},
		},
	}
}

// createHistoryActions creates history navigation key actions
func (r *Registry) createHistoryActions() []*KeyAction {
	return []*KeyAction{
		{
			ID:          "history_up",
			Keys:        []string{"up"},
			Description: "navigate to previous message in history",
			Category:    "text_editing",
			Handler:     handleHistoryUp,
			Priority:    200,
			Enabled:     true,
			Context: KeyContext{
				Views: []domain.ViewState{domain.ViewStateChat},
			},
		},
		{
			ID:          "history_down",
			Keys:        []string{"down"},
			Description: "navigate to next message in history",
			Category:    "text_editing",
			Handler:     handleHistoryDown,
			Priority:    200,
			Enabled:     true,
			Context: KeyContext{
				Views: []domain.ViewState{domain.ViewStateChat},
			},
		},
	}
}

// createScrollActions creates scroll-related key actions
func (r *Registry) createScrollActions() []*KeyAction {
	return []*KeyAction{
		{
			ID:          "scroll_to_top",
			Keys:        []string{"home"},
			Description: "scroll to top",
			Category:    "navigation",
			Handler:     handleScrollToTop,
			Priority:    120,
			Enabled:     true,
			Context: KeyContext{
				Views: []domain.ViewState{domain.ViewStateChat},
			},
		},
		{
			ID:          "scroll_to_bottom",
			Keys:        []string{"end"},
			Description: "scroll to bottom",
			Category:    "navigation",
			Handler:     handleScrollToBottom,
			Priority:    120,
			Enabled:     true,
			Context: KeyContext{
				Views: []domain.ViewState{domain.ViewStateChat},
			},
		},
		{
			ID:          "scroll_up_half_page",
			Keys:        []string{"shift+up"},
			Description: "scroll up half page",
			Category:    "navigation",
			Handler:     handleScrollUpHalfPage,
			Priority:    120,
			Enabled:     true,
			Context: KeyContext{
				Views: []domain.ViewState{domain.ViewStateChat},
			},
		},
		{
			ID:          "scroll_down_half_page",
			Keys:        []string{"shift+down"},
			Description: "scroll down half page",
			Category:    "navigation",
			Handler:     handleScrollDownHalfPage,
			Priority:    120,
			Enabled:     true,
			Context: KeyContext{
				Views: []domain.ViewState{domain.ViewStateChat},
			},
		},
		{
			ID:          "page_up",
			Keys:        []string{"pgup", "page_up"},
			Description: "page up",
			Category:    "navigation",
			Handler:     handlePageUp,
			Priority:    120,
			Enabled:     true,
			Context: KeyContext{
				Views: []domain.ViewState{domain.ViewStateChat},
			},
		},
		{
			ID:          "page_down",
			Keys:        []string{"pgdn", "page_down"},
			Description: "page down",
			Category:    "navigation",
			Handler:     handlePageDown,
			Priority:    120,
			Enabled:     true,
			Context: KeyContext{
				Views: []domain.ViewState{domain.ViewStateChat},
			},
		},
	}
}

// registerActionsToLayers registers actions to their appropriate layers
func (r *Registry) registerActionsToLayers(globalActions, chatActions, scrollActions []*KeyAction) {
	allActions := append(globalActions, chatActions...)
	allActions = append(allActions, scrollActions...)

	for _, action := range allActions {
		if err := r.Register(action); err != nil {
			continue
		}
	}

	for _, action := range globalActions {
		_ = r.addActionToLayer("global", action)
	}

	for _, action := range chatActions {
		_ = r.addActionToLayer("chat_view", action)
	}

	for _, action := range scrollActions {
		_ = r.addActionToLayer("chat_view", action)
	}
}

// Handler implementations
func handleQuit(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	return tea.Quit
}

func handleCancel(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	if chatSession := app.GetStateManager().GetChatSession(); chatSession != nil {
		app.GetStateManager().EndChatSession()
		return func() tea.Msg {
			return shared.SetStatusMsg{
				Message: "Response cancelled",
				Spinner: false,
			}
		}
	}

	app.GetStateManager().EndChatSession()
	app.GetStateManager().EndToolExecution()
	_ = app.GetStateManager().TransitionToView(domain.ViewStateChat)

	return func() tea.Msg {
		return shared.SetStatusMsg{
			Message: "Operation cancelled",
			Spinner: false,
		}
	}
}

func handleToggleToolExpansion(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	app.ToggleToolResultExpansion()
	return nil
}

func handleSendMessage(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	return app.SendMessage()
}

func handlePaste(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	clipboardText, err := clipboard.ReadAll()
	if err != nil {
		return nil
	}

	if clipboardText == "" {
		return nil
	}

	cleanText := strings.ReplaceAll(clipboardText, "\n", " ")
	cleanText = strings.ReplaceAll(cleanText, "\r", " ")
	cleanText = strings.ReplaceAll(cleanText, "\t", " ")

	if cleanText != "" {
		inputView := app.GetInputView()
		if inputView != nil {
			currentText := inputView.GetInput()
			cursor := inputView.GetCursor()

			newText := currentText[:cursor] + cleanText + currentText[cursor:]
			newCursor := cursor + len(cleanText)

			inputView.SetText(newText)
			inputView.SetCursor(newCursor)
		}
	}
	return nil
}

func handleCopy(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	inputView := app.GetInputView()
	if inputView != nil {
		text := inputView.GetInput()
		if text != "" {
			_ = clipboard.WriteAll(text)
		}
	}
	return nil
}

func handleScrollToTop(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	return func() tea.Msg {
		return shared.ScrollRequestMsg{
			ComponentID: "conversation",
			Direction:   shared.ScrollToTop,
			Amount:      0,
		}
	}
}

func handleScrollToBottom(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	return func() tea.Msg {
		return shared.ScrollRequestMsg{
			ComponentID: "conversation",
			Direction:   shared.ScrollToBottom,
			Amount:      0,
		}
	}
}

func handleScrollUpHalfPage(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	return func() tea.Msg {
		return shared.ScrollRequestMsg{
			ComponentID: "conversation",
			Direction:   shared.ScrollUp,
			Amount:      10,
		}
	}
}

func handleScrollDownHalfPage(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	return func() tea.Msg {
		return shared.ScrollRequestMsg{
			ComponentID: "conversation",
			Direction:   shared.ScrollDown,
			Amount:      10,
		}
	}
}

func handlePageUp(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	return func() tea.Msg {
		return shared.ScrollRequestMsg{
			ComponentID: "conversation",
			Direction:   shared.ScrollUp,
			Amount:      20,
		}
	}
}

func handlePageDown(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	return func() tea.Msg {
		return shared.ScrollRequestMsg{
			ComponentID: "conversation",
			Direction:   shared.ScrollDown,
			Amount:      20,
		}
	}
}

// Text editing handlers
func handleCursorLeft(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	inputView := app.GetInputView()
	if inputView != nil {
		cursor := inputView.GetCursor()
		if cursor > 0 {
			inputView.SetCursor(cursor - 1)
		}
	}
	return nil
}

func handleCursorRight(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	inputView := app.GetInputView()
	if inputView != nil {
		cursor := inputView.GetCursor()
		text := inputView.GetInput()
		if cursor < len(text) {
			inputView.SetCursor(cursor + 1)
		}
	}
	return nil
}

func handleBackspace(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	inputView := app.GetInputView()
	if inputView != nil {
		cursor := inputView.GetCursor()
		text := inputView.GetInput()
		if cursor > 0 {
			newText := text[:cursor-1] + text[cursor:]
			inputView.SetText(newText)
			inputView.SetCursor(cursor - 1)
		}
	}
	return nil
}

func handleHistoryUp(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	inputView := app.GetInputView()
	if inputView != nil {
		inputView.NavigateHistoryUp()
	}
	return nil
}

func handleHistoryDown(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	inputView := app.GetInputView()
	if inputView != nil {
		inputView.NavigateHistoryDown()
	}
	return nil
}

func handleDeleteToBeginning(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	inputView := app.GetInputView()
	if inputView != nil {
		cursor := inputView.GetCursor()
		if cursor > 0 {
			text := inputView.GetInput()
			newText := text[cursor:]
			inputView.SetText(newText)
			inputView.SetCursor(0)
		}
	}
	return nil
}

func handleDeleteWordBackward(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	inputView := app.GetInputView()
	if inputView != nil {
		cursor := inputView.GetCursor()
		text := inputView.GetInput()
		if cursor > 0 {
			start := cursor

			for start > 0 && (text[start-1] == ' ' || text[start-1] == '\t') {
				start--
			}

			for start > 0 && text[start-1] != ' ' && text[start-1] != '\t' {
				start--
			}

			newText := text[:start] + text[cursor:]
			inputView.SetText(newText)
			inputView.SetCursor(start)
		}
	}
	return nil
}

func handleMoveToBeginning(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	inputView := app.GetInputView()
	if inputView != nil {
		inputView.SetCursor(0)
	}
	return nil
}

func handleMoveToEnd(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	inputView := app.GetInputView()
	if inputView != nil {
		text := inputView.GetInput()
		inputView.SetCursor(len(text))
	}
	return nil
}

func handleToggleHelp(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	return func() tea.Msg {
		return shared.ToggleHelpBarMsg{}
	}
}

// KeyBindingManager manages the key binding system for ChatApplication
type KeyBindingManager struct {
	registry KeyRegistry
	app      KeyHandlerContext
}

// NewKeyBindingManager creates a new key binding manager
func NewKeyBindingManager(app KeyHandlerContext) *KeyBindingManager {
	return &KeyBindingManager{
		registry: NewRegistry(),
		app:      app,
	}
}

// ProcessKey processes a key press through the key binding system
func (m *KeyBindingManager) ProcessKey(keyMsg tea.KeyMsg) tea.Cmd {
	key := keyMsg.String()

	action := m.registry.Resolve(key, m.app)
	var handlerName string
	var cmd tea.Cmd

	if action != nil {
		handlerName = action.ID
		cmd = action.Handler(m.app, keyMsg)
	} else {
		handlerName = "character_input_fallback"
		cmd = handleCharacterInput(m.app, keyMsg)
	}

	if debugCmd := m.debugKeyBinding(keyMsg, handlerName); debugCmd != nil {
		return tea.Batch(cmd, debugCmd)
	}

	return cmd
}

// GetHelpShortcuts returns help shortcuts for the current context
func (m *KeyBindingManager) GetHelpShortcuts() []HelpShortcut {
	return m.registry.GetHelpShortcuts(m.app)
}

// RegisterCustomAction registers a new custom key action
func (m *KeyBindingManager) RegisterCustomAction(action *KeyAction) error {
	return m.registry.Register(action)
}

// GetRegistry returns the underlying registry (for advanced usage)
func (m *KeyBindingManager) GetRegistry() KeyRegistry {
	return m.registry
}

// debugKeyBinding logs key binding events when debug mode is enabled
func (m *KeyBindingManager) debugKeyBinding(keyMsg tea.KeyMsg, handlerName string) tea.Cmd {
	config := m.app.GetServices().GetConfig()
	if config != nil && config.Output.Debug {
		logger.Debug("Key binding debug",
			"key", keyMsg.String(),
			"handler", handlerName,
			"type", keyMsg.Type,
			"alt", keyMsg.Alt,
			"runes", string(keyMsg.Runes))

		return func() tea.Msg {
			return shared.DebugKeyMsg{
				Key:     keyMsg.String(),
				Handler: handlerName,
			}
		}
	}
	return nil
}

func handleCharacterInput(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	keyStr := keyMsg.String()

	if len(keyStr) == 1 && keyStr[0] >= ' ' && keyStr[0] <= '~' {
		inputView := app.GetInputView()
		if inputView != nil {
			cursor := inputView.GetCursor()
			text := inputView.GetInput()
			newText := text[:cursor] + keyStr + text[cursor:]
			inputView.SetText(newText)
			inputView.SetCursor(cursor + 1)

			if keyStr == "@" {
				return tea.Batch(
					func() tea.Msg {
						return shared.ScrollRequestMsg{
							ComponentID: "conversation",
							Direction:   shared.ScrollToBottom,
							Amount:      0,
						}
					},
					func() tea.Msg {
						return shared.FileSelectionRequestMsg{}
					},
				)
			}

			return tea.Batch(
				func() tea.Msg {
					return shared.ScrollRequestMsg{
						ComponentID: "conversation",
						Direction:   shared.ScrollToBottom,
						Amount:      0,
					}
				},
				func() tea.Msg {
					return shared.HideHelpBarMsg{}
				},
			)
		}
	}
	return nil
}
