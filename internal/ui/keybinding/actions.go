package keybinding

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	clipboard "github.com/inference-gateway/cli/internal/clipboard"
	domain "github.com/inference-gateway/cli/internal/domain"
	ui "github.com/inference-gateway/cli/internal/ui"
	components "github.com/inference-gateway/cli/internal/ui/components"
	keys "github.com/inference-gateway/cli/internal/ui/keys"
)

// registerDefaultBindings registers all default key bindings
func (r *Registry) registerDefaultBindings() {
	globalActions := r.createGlobalActions()
	chatActions := r.createChatActions()
	scrollActions := r.createScrollActions()
	approvalActions := r.createApprovalActions()

	r.registerActionsToLayers(globalActions, chatActions, scrollActions, approvalActions)
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
			ID:          "cycle_agent_mode",
			Keys:        []string{"shift+tab"},
			Description: "cycle agent mode (Standard/Plan/Auto-Accept)",
			Category:    "mode",
			Handler:     handleCycleAgentMode,
			Priority:    150,
			Enabled:     true,
			Context: KeyContext{
				Views: []domain.ViewState{domain.ViewStateChat},
			},
		},
		{
			ID:          "toggle_tool_expansion",
			Keys:        []string{"ctrl+o"},
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
			ID:          "toggle_raw_format",
			Keys:        []string{"ctrl+r"},
			Description: "toggle raw/rendered markdown",
			Category:    "display",
			Handler:     handleToggleRawFormat,
			Priority:    150,
			Enabled:     true,
			Context: KeyContext{
				Views: []domain.ViewState{domain.ViewStateChat},
			},
		},
		{
			ID:          "toggle_mouse_mode",
			Keys:        []string{"ctrl+s"},
			Description: "toggle mouse scrolling/text selection",
			Category:    "selection",
			Handler:     handleToggleMouseMode,
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
		{
			ID:          "toggle_todo_box",
			Keys:        []string{"ctrl+t"},
			Description: "toggle todo list",
			Category:    "display",
			Handler:     handleToggleTodoBox,
			Priority:    150,
			Enabled:     true,
			Context: KeyContext{
				Views: []domain.ViewState{domain.ViewStateChat},
			},
		},
	}

	actions = append(actions, r.createClipboardActions()...)
	actions = append(actions, r.createTextEditingActions()...)
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
			Handler:     handleCursorLeftOrPlanNav,
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
			Handler:     handleCursorRightOrPlanNav,
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

// createApprovalActions creates key actions specific to approval view
func (r *Registry) createApprovalActions() []*KeyAction {
	return []*KeyAction{
		{
			ID:          "plan_approval_left",
			Keys:        []string{"left", "h"},
			Description: "move selection left",
			Category:    "plan_approval",
			Handler:     handlePlanApprovalLeft,
			Priority:    150,
			Enabled:     true,
			Context: KeyContext{
				Views: []domain.ViewState{domain.ViewStatePlanApproval},
			},
		},
		{
			ID:          "plan_approval_right",
			Keys:        []string{"right", "l"},
			Description: "move selection right",
			Category:    "plan_approval",
			Handler:     handlePlanApprovalRight,
			Priority:    150,
			Enabled:     true,
			Context: KeyContext{
				Views: []domain.ViewState{domain.ViewStatePlanApproval},
			},
		},
		{
			ID:          "plan_approval_accept",
			Keys:        []string{"enter", "y"},
			Description: "accept plan",
			Category:    "plan_approval",
			Handler:     handlePlanApprovalAccept,
			Priority:    150,
			Enabled:     true,
			Context: KeyContext{
				Views: []domain.ViewState{domain.ViewStatePlanApproval},
			},
		},
		{
			ID:          "plan_approval_reject",
			Keys:        []string{"n"},
			Description: "reject plan",
			Category:    "plan_approval",
			Handler:     handlePlanApprovalReject,
			Priority:    150,
			Enabled:     true,
			Context: KeyContext{
				Views: []domain.ViewState{domain.ViewStatePlanApproval},
			},
		},
		{
			ID:          "plan_approval_accept_and_auto_approve",
			Keys:        []string{"a"},
			Description: "accept plan and enable auto-approve mode",
			Category:    "plan_approval",
			Handler:     handlePlanApprovalAcceptAndAutoApprove,
			Priority:    150,
			Enabled:     true,
			Context: KeyContext{
				Views: []domain.ViewState{domain.ViewStatePlanApproval},
			},
		},
	}
}

// registerActionsToLayers registers actions to their appropriate layers
func (r *Registry) registerActionsToLayers(globalActions, chatActions, scrollActions, approvalActions []*KeyAction) {
	allActions := append(globalActions, chatActions...)
	allActions = append(allActions, scrollActions...)
	allActions = append(allActions, approvalActions...)

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

	for _, action := range approvalActions {
		_ = r.addActionToLayer("approval_view", action)
	}
}

// Handler implementations
func handleQuit(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	return tea.Quit
}

func handleCancel(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	autocomplete := app.GetAutocomplete()
	if autocomplete != nil && autocomplete.IsVisible() {
		return func() tea.Msg {
			return domain.AutocompleteHideEvent{}
		}
	}

	stateManager := app.GetStateManager()

	planApprovalState := stateManager.GetPlanApprovalUIState()
	if planApprovalState != nil {
		return func() tea.Msg {
			return domain.PlanApprovalResponseEvent{
				Action: domain.PlanApprovalReject,
			}
		}
	}

	approvalState := stateManager.GetApprovalUIState()
	if approvalState != nil && approvalState.PendingToolCall != nil {
		return func() tea.Msg {
			return domain.ToolApprovalResponseEvent{
				Action:   domain.ApprovalReject,
				ToolCall: *approvalState.PendingToolCall,
			}
		}
	}

	if chatSession := stateManager.GetChatSession(); chatSession != nil {
		agentService := app.GetAgentService()
		if agentService != nil {
			_ = agentService.CancelRequest(chatSession.RequestID)
		}
	}

	stateManager.EndChatSession()
	stateManager.EndToolExecution()
	_ = stateManager.TransitionToView(domain.ViewStateChat)

	return func() tea.Msg {
		return domain.SetStatusEvent{
			Message: "Operation cancelled",
			Spinner: false,
		}
	}
}

func handleToggleToolExpansion(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	app.ToggleToolResultExpansion()
	return nil
}

func handleToggleRawFormat(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	app.ToggleRawFormat()
	return func() tea.Msg {
		return domain.SetStatusEvent{
			Message: "Toggled raw/rendered format",
			Spinner: false,
		}
	}
}

func handleEnterKey(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	stateManager := app.GetStateManager()

	approvalState := stateManager.GetApprovalUIState()
	if approvalState != nil {
		action := domain.ApprovalAction(approvalState.SelectedIndex)
		return func() tea.Msg {
			return domain.ToolApprovalResponseEvent{
				Action:   action,
				ToolCall: *approvalState.PendingToolCall,
			}
		}
	}

	planApprovalState := stateManager.GetPlanApprovalUIState()
	if planApprovalState != nil {
		action := domain.PlanApprovalAction(planApprovalState.SelectedIndex)
		return func() tea.Msg {
			return domain.PlanApprovalResponseEvent{
				Action: action,
			}
		}
	}

	inputView := app.GetInputView()
	if inputView == nil {
		return nil
	}

	autocomplete := app.GetAutocomplete()
	if autocomplete != nil && autocomplete.IsVisible() {
		if handled, completion := autocomplete.HandleKey(keyMsg); handled {
			if completion != "" {
				return func() tea.Msg {
					return domain.AutocompleteCompleteEvent{Completion: completion}
				}
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
	inputView := app.GetInputView()
	if inputView == nil {
		return nil
	}

	imageService := app.GetImageService()

	imageData := clipboard.Read(clipboard.FmtImage)
	if len(imageData) > 0 {
		imageAttachment, err := imageService.ReadImageFromBinary(imageData, "clipboard-screenshot.png")
		if err == nil {
			inputView.AddImageAttachment(*imageAttachment)
			return nil
		}
	}

	clipboardText := string(clipboard.Read(clipboard.FmtText))
	if clipboardText == "" {
		return nil
	}

	cleanText := strings.ReplaceAll(clipboardText, "\r\n", "\n")
	cleanText = strings.ReplaceAll(cleanText, "\r", "\n")
	cleanText = strings.TrimSpace(cleanText)

	if cleanText == "" {
		return nil
	}

	if imageService.IsImageFile(cleanText) {
		imageAttachment, err := imageService.ReadImageFromFile(cleanText)
		if err == nil {
			inputView.AddImageAttachment(*imageAttachment)
			return nil
		}
	}

	currentText := inputView.GetInput()
	cursor := inputView.GetCursor()

	newText := currentText[:cursor] + cleanText + currentText[cursor:]
	newCursor := cursor + len(cleanText)

	inputView.SetText(newText)
	inputView.SetCursor(newCursor)

	return nil
}

func handleCopy(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	inputView := app.GetInputView()
	if inputView != nil {
		text := inputView.GetInput()
		if text != "" {
			clipboard.Write(clipboard.FmtText, []byte(text))
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

func handleScrollUpHalfPage(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	return func() tea.Msg {
		return domain.ScrollRequestEvent{
			ComponentID: "conversation",
			Direction:   domain.ScrollUp,
			Amount:      10,
		}
	}
}

func handleScrollDownHalfPage(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	return func() tea.Msg {
		return domain.ScrollRequestEvent{
			ComponentID: "conversation",
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
func handleCursorLeftOrPlanNav(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	stateManager := app.GetStateManager()

	approvalState := stateManager.GetApprovalUIState()
	if approvalState != nil {
		newIndex := approvalState.SelectedIndex - 1
		if newIndex < 0 {
			newIndex = int(domain.ApprovalAutoAccept)
		}
		stateManager.SetApprovalSelectedIndex(newIndex)
		return func() tea.Msg { return nil }
	}

	planApprovalState := stateManager.GetPlanApprovalUIState()
	if planApprovalState != nil {
		newIndex := planApprovalState.SelectedIndex - 1
		if newIndex < 0 {
			newIndex = int(domain.PlanApprovalAcceptAndAutoApprove)
		}
		stateManager.SetPlanApprovalSelectedIndex(newIndex)
		return func() tea.Msg { return nil }
	}

	inputView := app.GetInputView()
	if inputView != nil {
		cursor := inputView.GetCursor()
		if cursor > 0 {
			inputView.SetCursor(cursor - 1)
		}
	}
	return nil
}

func handleCursorRightOrPlanNav(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	stateManager := app.GetStateManager()

	approvalState := stateManager.GetApprovalUIState()
	if approvalState != nil {
		newIndex := approvalState.SelectedIndex + 1
		if newIndex > int(domain.ApprovalAutoAccept) {
			newIndex = 0
		}
		stateManager.SetApprovalSelectedIndex(newIndex)
		return func() tea.Msg { return nil }
	}

	planApprovalState := stateManager.GetPlanApprovalUIState()
	if planApprovalState != nil {
		newIndex := planApprovalState.SelectedIndex + 1
		if newIndex > int(domain.PlanApprovalAcceptAndAutoApprove) {
			newIndex = 0
		}
		stateManager.SetPlanApprovalSelectedIndex(newIndex)
		return func() tea.Msg { return nil }
	}

	inputView := app.GetInputView()
	if inputView == nil {
		return nil
	}

	cursor := inputView.GetCursor()
	text := inputView.GetInput()

	if cursor == len(text) {
		if iv, ok := inputView.(*components.InputView); ok && iv.HasHistorySuggestion() {
			iv.AcceptHistorySuggestion()
			return nil
		}
	}

	if cursor < len(text) {
		inputView.SetCursor(cursor + 1)
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

			return func() tea.Msg {
				return domain.AutocompleteUpdateEvent{
					Text:      newText,
					CursorPos: cursor - 1,
				}
			}
		}
	}
	return nil
}

func handleHistoryUp(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	inputView := app.GetInputView()
	autocomplete := app.GetAutocomplete()
	if inputView != nil {
		if autocomplete != nil && autocomplete.IsVisible() {
			autocomplete.HandleKey(keyMsg)
			return nil
		}
		inputView.NavigateHistoryUp()
	}
	return nil
}

func handleHistoryDown(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	inputView := app.GetInputView()
	autocomplete := app.GetAutocomplete()
	if inputView != nil {
		if autocomplete != nil && autocomplete.IsVisible() {
			autocomplete.HandleKey(keyMsg)
			return nil
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

func handleToggleTodoBox(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	return func() tea.Msg {
		return domain.ToggleTodoBoxEvent{}
	}
}

func handleCycleAgentMode(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	stateManager := app.GetStateManager()
	newMode := stateManager.CycleAgentMode()

	return tea.Batch(
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message: fmt.Sprintf("Mode changed to: %s", newMode.DisplayName()),
				Spinner: false,
			}
		},
		func() tea.Msg {
			return domain.RefreshAutocompleteEvent{}
		},
	)
}

func handleToggleMouseMode(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	mouseEnabled := app.GetMouseEnabled()
	app.SetMouseEnabled(!mouseEnabled)

	if !mouseEnabled {
		return tea.Batch(
			tea.EnableMouseCellMotion,
			func() tea.Msg {
				return domain.SetStatusEvent{
					Message:    "Mouse scrolling enabled",
					Spinner:    false,
					StatusType: domain.StatusDefault,
				}
			},
		)
	}

	return tea.Batch(
		tea.DisableMouse,
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    "Text selection enabled",
				Spinner:    false,
				StatusType: domain.StatusDefault,
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

// IsKeyHandledByAction returns true if the key would be handled by a keybinding action
func (m *KeyBindingManager) IsKeyHandledByAction(keyMsg tea.KeyMsg) bool {
	keyStr := keyMsg.String()
	action := m.registry.Resolve(keyStr, m.app)
	return action != nil
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

	stateManager := app.GetStateManager()
	currentView := stateManager.GetCurrentView()

	if currentView == domain.ViewStatePlanApproval {
		return nil
	}

	if stateManager.GetApprovalUIState() != nil {
		return nil
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

	autocompleteCmd := func() tea.Msg {
		return domain.AutocompleteUpdateEvent{
			Text:      newText,
			CursorPos: newCursor,
		}
	}

	if keyStr == "@" {
		return tea.Batch(
			autocompleteCmd,
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

	// Default: autocomplete update, scroll to bottom, hide help
	return tea.Batch(
		autocompleteCmd,
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

// Approval handlers
// Plan Approval handlers

func handlePlanApprovalLeft(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	stateManager := app.GetStateManager()
	planApprovalState := stateManager.GetPlanApprovalUIState()
	if planApprovalState == nil {
		return nil
	}

	newIndex := planApprovalState.SelectedIndex - 1
	if newIndex < 0 {
		newIndex = int(domain.PlanApprovalAcceptAndAutoApprove)
	}
	stateManager.SetPlanApprovalSelectedIndex(newIndex)

	return func() tea.Msg { return nil }
}

func handlePlanApprovalRight(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	stateManager := app.GetStateManager()
	planApprovalState := stateManager.GetPlanApprovalUIState()
	if planApprovalState == nil {
		return nil
	}

	newIndex := planApprovalState.SelectedIndex + 1
	if newIndex > int(domain.PlanApprovalAcceptAndAutoApprove) {
		newIndex = 0
	}
	stateManager.SetPlanApprovalSelectedIndex(newIndex)

	return func() tea.Msg { return nil }
}

func handlePlanApprovalAccept(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	return func() tea.Msg {
		stateManager := app.GetStateManager()
		planApprovalState := stateManager.GetPlanApprovalUIState()
		if planApprovalState == nil {
			return nil
		}

		action := domain.PlanApprovalAction(planApprovalState.SelectedIndex)
		if action == domain.PlanApprovalAccept || keyMsg.String() == "y" {
			action = domain.PlanApprovalAccept
		}

		return domain.PlanApprovalResponseEvent{
			Action: action,
		}
	}
}

func handlePlanApprovalReject(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	return func() tea.Msg {
		return domain.PlanApprovalResponseEvent{
			Action: domain.PlanApprovalReject,
		}
	}
}

func handlePlanApprovalAcceptAndAutoApprove(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
	return func() tea.Msg {
		return domain.PlanApprovalResponseEvent{
			Action: domain.PlanApprovalAcceptAndAutoApprove,
		}
	}
}
