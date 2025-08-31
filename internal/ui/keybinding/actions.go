package keybinding

import (
	"fmt"
	"strings"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/ui"
	"github.com/inference-gateway/cli/internal/ui/components"
	"github.com/inference-gateway/cli/internal/ui/keys"
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
			ID:          "enter_selection_mode",
			Keys:        []string{"ctrl+s"},
			Description: "enter text selection mode",
			Category:    "selection",
			Handler:     handleEnterSelectionMode,
			Priority:    150,
			Enabled:     true,
			Context: KeyContext{
				Views: []domain.ViewState{domain.ViewStateChat},
			},
		},
		{
			ID:          "enter_key_handler",
			Keys:        []string{"enter"},
			Description: "send message or insert newline",
			Category:    "chat",
			Handler:     handleEnterKey,
			Priority:    150,
			Enabled:     true,
			Context: KeyContext{
				Views: []domain.ViewState{domain.ViewStateChat},
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
			ID:          "insert_newline_alt",
			Keys:        []string{"alt+enter"},
			Description: "insert newline",
			Category:    "text_editing",
			Handler:     handleInsertNewline,
			Priority:    200,
			Enabled:     true,
			Context: KeyContext{
				Views: []domain.ViewState{domain.ViewStateChat},
			},
		},
		{
			ID:          "insert_newline_ctrl",
			Keys:        []string{"ctrl+j"},
			Description: "insert newline",
			Category:    "text_editing",
			Handler:     handleInsertNewline,
			Priority:    200,
			Enabled:     true,
			Context: KeyContext{
				Views: []domain.ViewState{domain.ViewStateChat},
			},
		},
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
	inputView := app.GetInputView()
	if inputView != nil && inputView.IsAutocompleteVisible() {
		return nil
	}

	app.GetStateManager().EndChatSession()
	app.GetStateManager().EndToolExecution()
	_ = app.GetStateManager().TransitionToView(domain.ViewStateChat)

	return func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    "Operation cancelled",
			Spinner:    false,
			TokenUsage: getCurrentTokenUsage(app),
		}
	}
}

func handleToggleToolExpansion(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	app.ToggleToolResultExpansion()
	return nil
}

func handleEnterKey(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	inputView := app.GetInputView()
	if inputView == nil {
		return nil
	}

	if inputView.IsAutocompleteVisible() {
		if handled, completion := inputView.TryHandleAutocomplete(keyMsg); handled {
			if completion != "" {
				inputView.SetText(completion)
				inputView.SetCursor(len(completion))
			}
			return nil
		}
	}

	input := inputView.GetInput()
	cursor := inputView.GetCursor()

	if len(input) == 0 {
		return nil
	}

	if cursor == len(input) && cursor > 0 && input[cursor-1] == '\\' {
		if cursor > 1 && input[cursor-2] == '\\' {
			return app.SendMessage()
		}
		return handleInsertNewline(app, keyMsg)
	}

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

	cleanText := strings.ReplaceAll(clipboardText, "\r\n", "\n")
	cleanText = strings.ReplaceAll(cleanText, "\r", "\n")

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
		return domain.ScrollRequestEvent{
			ComponentID: "conversation",
			Direction:   domain.ScrollToTop,
			Amount:      0,
		}
	}
}

func handleScrollToBottom(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	return func() tea.Msg {
		return domain.ScrollRequestEvent{
			ComponentID: "conversation",
			Direction:   domain.ScrollToBottom,
			Amount:      0,
		}
	}
}

// TODO - fix this
func handleScrollUpHalfPage(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd { // nolint:unused
	return func() tea.Msg {
		componentID := "conversation"

		return domain.ScrollRequestEvent{
			ComponentID: componentID,
			Direction:   domain.ScrollUp,
			Amount:      10,
		}
	}
}

// TODO - fix this
func handleScrollDownHalfPage(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd { // nolint:unused
	return func() tea.Msg {
		componentID := "conversation"

		return domain.ScrollRequestEvent{
			ComponentID: componentID,
			Direction:   domain.ScrollDown,
			Amount:      10,
		}
	}
}

func handlePageUp(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	return func() tea.Msg {
		return domain.ScrollRequestEvent{
			ComponentID: "conversation",
			Direction:   domain.ScrollUp,
			Amount:      20,
		}
	}
}

func handlePageDown(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	return func() tea.Msg {
		return domain.ScrollRequestEvent{
			ComponentID: "conversation",
			Direction:   domain.ScrollDown,
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

			if iv, ok := inputView.(*components.InputView); ok && iv.Autocomplete != nil {
				iv.Autocomplete.Update(newText, cursor-1)
			}
		}
	}
	return nil
}

func handleHistoryUp(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	inputView := app.GetInputView()
	if inputView != nil {
		if inputView.IsAutocompleteVisible() {
			_, cmd := inputView.HandleKey(keyMsg)
			return cmd
		}
		inputView.NavigateHistoryUp()
	}
	return nil
}

func handleHistoryDown(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	inputView := app.GetInputView()
	if inputView != nil {
		if inputView.IsAutocompleteVisible() {
			_, cmd := inputView.HandleKey(keyMsg)
			return cmd
		}
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

func handleInsertNewline(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	inputView := app.GetInputView()
	if inputView != nil {
		cursor := inputView.GetCursor()
		text := inputView.GetInput()
		newText := text[:cursor] + "\n" + text[cursor:]
		inputView.SetText(newText)
		inputView.SetCursor(cursor + 1)
	}
	return nil
}

func handleToggleHelp(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	return func() tea.Msg {
		return domain.ToggleHelpBarEvent{}
	}
}

func handleEnterSelectionMode(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	stateManager := app.GetStateManager()

	statusView := app.GetStatusView()
	statusView.SaveCurrentState()

	err := stateManager.TransitionToView(domain.ViewStateTextSelection)
	if err != nil {
		return func() tea.Msg {
			return domain.ShowErrorEvent{
				Error: "Failed to enter selection mode: " + err.Error(),
			}
		}
	}

	return tea.Batch(
		func() tea.Msg {
			return domain.InitializeTextSelectionEvent{}
		},
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message: "Entered text selection mode - use vim keys to navigate",
				Spinner: false,
			}
		},
	)
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

// ProcessKey handles key input and executes the appropriate action
func (m *KeyBindingManager) ProcessKey(keyMsg tea.KeyMsg) tea.Cmd {
	keyStr := keyMsg.String()
	var cmds []tea.Cmd

	config := m.app.GetConfig()
	if config != nil && config.Logging.Debug {
		debugInfo := keyStr
		if len(keyStr) == 1 {
			debugInfo = fmt.Sprintf("%s (char: 0x%02X)", keyStr, keyStr[0])
		}
		if debugCmd := m.debugKeyBinding(keyMsg, debugInfo); debugCmd != nil {
			cmds = append(cmds, debugCmd)
		}
	}

	action := m.registry.Resolve(keyStr, m.app)
	if action != nil {
		actionCmd := action.Handler(m.app, keyMsg)
		if len(cmds) > 0 {
			cmds = append(cmds, actionCmd)
			return tea.Batch(cmds...)
		}
		return actionCmd
	}

	charCmd := handleCharacterInput(m.app, keyMsg)
	if len(cmds) > 0 {
		cmds = append(cmds, charCmd)
		return tea.Batch(cmds...)
	}
	return charCmd
}

// GetHelpShortcuts returns help shortcuts for the current context
func (m *KeyBindingManager) GetHelpShortcuts() []HelpShortcut {
	return m.registry.GetHelpShortcuts(m.app)
}

// RegisterCustomAction registers a new custom key action
func (m *KeyBindingManager) RegisterCustomAction(action *KeyAction) error {
	return m.registry.Register(action)
}

// getCurrentTokenUsage returns current session token usage string
func getCurrentTokenUsage(app KeyHandlerContext) string {
	conversationRepo := app.GetConversationRepository()
	if conversationRepo == nil {
		return ""
	}

	return shared.FormatCurrentTokenUsage(conversationRepo)
}

// GetRegistry returns the underlying registry (for advanced usage)
func (m *KeyBindingManager) GetRegistry() KeyRegistry {
	return m.registry
}

// debugKeyBinding logs key binding events when debug mode is enabled
func (m *KeyBindingManager) debugKeyBinding(keyMsg tea.KeyMsg, info string) tea.Cmd {
	config := m.app.GetConfig()
	if config != nil && config.Logging.Debug {
		return func() tea.Msg {
			return domain.DebugKeyEvent{
				Key:     keyMsg.String(),
				Handler: info,
			}
		}
	}
	return nil
}

func handleCharacterInput(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	keyStr := keyMsg.String()

	if strings.Contains(keyStr, "???") ||
		keyStr == "ctrl+?" || keyStr == "ctrl+shift+/" || keyStr == "ctrl+_" {
		return nil
	}

	if len(keyStr) > 1 && !keys.IsKnownKey(keyStr) {
		return handlePasteEvent(app, keyStr)
	}

	inputView := app.GetInputView()
	if inputView != nil {
		if inputView.CanHandle(keyMsg) {
			_, cmd := inputView.HandleKey(keyMsg)
			if cmd != nil {
				return cmd
			}
		}
	}

	if keys.IsPrintableCharacter(keyStr) {
		return handlePrintableCharacter(keyStr, inputView)
	}
	return nil
}

// handlePrintableCharacter processes printable character input
func handlePrintableCharacter(keyStr string, inputView ui.InputComponent) tea.Cmd {
	if inputView == nil {
		return nil
	}

	cursor := inputView.GetCursor()
	text := inputView.GetInput()
	newText := text[:cursor] + keyStr + text[cursor:]
	newCursor := cursor + 1
	inputView.SetText(newText)
	inputView.SetCursor(newCursor)

	if autocomplete := inputView.(*components.InputView).Autocomplete; autocomplete != nil {
		autocomplete.Update(newText, newCursor)
	}

	if keyStr == "@" {
		return tea.Batch(
			func() tea.Msg {
				return domain.ScrollRequestEvent{
					ComponentID: "conversation",
					Direction:   domain.ScrollToBottom,
					Amount:      0,
				}
			},
			func() tea.Msg {
				return domain.FileSelectionRequestEvent{}
			},
		)
	}

	return tea.Batch(
		func() tea.Msg {
			return domain.ScrollRequestEvent{
				ComponentID: "conversation",
				Direction:   domain.ScrollToBottom,
				Amount:      0,
			}
		},
		func() tea.Msg {
			return domain.HideHelpBarEvent{}
		},
	)
}

// handlePasteEvent handles when the terminal sends clipboard content directly
func handlePasteEvent(app KeyHandlerContext, pastedText string) tea.Cmd {
	inputView := app.GetInputView()
	if inputView == nil {
		return nil
	}

	cleanText := strings.ReplaceAll(pastedText, "\r\n", "\n")
	cleanText = strings.ReplaceAll(cleanText, "\r", "\n")
	cleanText = strings.Trim(cleanText, "[]")

	if cleanText != "" {
		cursor := inputView.GetCursor()
		text := inputView.GetInput()
		newText := text[:cursor] + cleanText + text[cursor:]
		newCursor := cursor + len(cleanText)

		inputView.SetText(newText)
		inputView.SetCursor(newCursor)
	}

	return nil
}
