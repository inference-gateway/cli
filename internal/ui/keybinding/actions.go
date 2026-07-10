package keybinding

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	config "github.com/inference-gateway/cli/config"
	clipboard "github.com/inference-gateway/cli/internal/clipboard"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	services "github.com/inference-gateway/cli/internal/services"
	ui "github.com/inference-gateway/cli/internal/ui"
	components "github.com/inference-gateway/cli/internal/ui/components"
	hints "github.com/inference-gateway/cli/internal/ui/hints"
	keys "github.com/inference-gateway/cli/internal/ui/keys"
)

// defaultActions declares every built-in action: its ID, handler, and the
// view/condition context it is active in. Keys, descriptions, and categories
// deliberately do NOT live here — they come from config.GetDefaultKeybindings()
// (merged with keybindings.yaml overrides) when NewRegistry builds each
// action's key.Binding, so there is a single source of truth for defaults.
func defaultActions() []*KeyAction {
	chatView := func(conds ...ContextCondition) KeyContext {
		return KeyContext{
			Views:      []domain.ViewState{domain.ViewStateChat},
			Conditions: conds,
		}
	}
	planApprovalView := KeyContext{Views: []domain.ViewState{domain.ViewStatePlanApproval}}

	inputIsEmpty := ContextCondition{
		Name: "input_is_empty",
		Check: func(app KeyHandlerContext) bool {
			return strings.TrimSpace(app.GetInputView().GetInput()) == ""
		},
	}
	noApprovalPending := ContextCondition{
		Name: "no_approval_pending",
		Check: func(app KeyHandlerContext) bool {
			stateManager := app.GetStateManager()
			return stateManager.GetPlanApprovalUIState() == nil &&
				stateManager.GetApprovalUIState() == nil
		},
	}
	chatIdleOrCompleted := ContextCondition{
		Name: "chat_idle_or_completed",
		Check: func(app KeyHandlerContext) bool {
			stateManager := app.GetStateManager()
			chatSession := stateManager.GetChatSession()
			return stateManager.GetPlanApprovalUIState() == nil &&
				stateManager.GetApprovalUIState() == nil &&
				(chatSession == nil || chatSession.Status == domain.ChatStatusIdle || chatSession.Status == domain.ChatStatusCompleted)
		},
	}

	return []*KeyAction{
		{ID: config.ActionID(config.NamespaceGlobal, "quit"), Handler: handleQuit},
		{ID: config.ActionID(config.NamespaceGlobal, "cancel"), Handler: handleCancel},
		{ID: config.ActionID(config.NamespaceGlobal, "new_session"), Handler: handleNewSession},

		{ID: config.ActionID(config.NamespaceMode, "cycle_agent_mode"), Handler: handleCycleAgentMode, Context: chatView()},
		{ID: config.ActionID(config.NamespaceTools, "toggle_tool_expansion"), Handler: handleToggleToolExpansion, Context: chatView()},
		{ID: config.ActionID(config.NamespaceTools, "background_shell"), Handler: handleBackgroundShell, Context: chatView()},
		{ID: config.ActionID(config.NamespaceDisplay, "toggle_raw_format"), Handler: handleToggleRawFormat, Context: chatView()},
		{ID: config.ActionID(config.NamespaceDisplay, "toggle_todo_box"), Handler: handleToggleTodoBox, Context: chatView()},
		{ID: config.ActionID(config.NamespaceDisplay, "toggle_thinking"), Handler: handleToggleThinkingExpansion, Context: chatView()},
		{ID: config.ActionID(config.NamespaceSelection, "toggle_mouse_mode"), Handler: handleToggleMouseMode, Context: chatView()},
		{ID: config.ActionID(config.NamespaceChat, "tab_key_handler"), Handler: handleTabKey, Context: chatView()},
		{ID: config.ActionID(config.NamespaceChat, "enter_key_handler"), Handler: handleEnterKey, Context: chatView()},
		{ID: config.ActionID(config.NamespaceHelp, "toggle_help"), Handler: handleToggleHelp, Context: chatView(inputIsEmpty)},

		{ID: config.ActionID(config.NamespaceClipboard, "paste_text"), Handler: handlePaste, Context: chatView()},
		{ID: config.ActionID(config.NamespaceClipboard, "copy_text"), Handler: handleCopy, Context: chatView()},

		{ID: config.ActionID(config.NamespaceTextEditing, "insert_newline_alt"), Handler: handleInsertNewline, Context: chatView()},
		{ID: config.ActionID(config.NamespaceTextEditing, "insert_newline_ctrl"), Handler: handleInsertNewline, Context: chatView()},
		{ID: config.ActionID(config.NamespaceTextEditing, "move_cursor_left"), Handler: handleCursorLeftOrPlanNav, Context: chatView()},
		{ID: config.ActionID(config.NamespaceTextEditing, "move_cursor_right"), Handler: handleCursorRightOrPlanNav, Context: chatView()},
		{ID: config.ActionID(config.NamespaceTextEditing, "backspace"), Handler: handleBackspace, Context: chatView()},
		{ID: config.ActionID(config.NamespaceTextEditing, "delete_to_beginning"), Handler: handleDeleteToBeginning, Context: chatView()},
		{ID: config.ActionID(config.NamespaceTextEditing, "delete_word_backward"), Handler: handleDeleteWordBackward, Context: chatView()},
		{ID: config.ActionID(config.NamespaceTextEditing, "delete_word_forward"), Handler: handleDeleteWordForward, Context: chatView()},
		{ID: config.ActionID(config.NamespaceTextEditing, "move_cursor_word_left"), Handler: handleMoveCursorWordLeft, Context: chatView()},
		{ID: config.ActionID(config.NamespaceTextEditing, "move_cursor_word_right"), Handler: handleMoveCursorWordRight, Context: chatView()},
		{ID: config.ActionID(config.NamespaceTextEditing, "move_to_beginning"), Handler: handleMoveToBeginning, Context: chatView()},
		{ID: config.ActionID(config.NamespaceTextEditing, "move_to_end"), Handler: handleMoveToEnd, Context: chatView()},
		{ID: config.ActionID(config.NamespaceTextEditing, "history_up"), Handler: handleHistoryUp, Context: chatView(noApprovalPending)},
		{ID: config.ActionID(config.NamespaceTextEditing, "history_down"), Handler: handleHistoryDown, Context: chatView(noApprovalPending)},

		{ID: config.ActionID(config.NamespaceNavigation, "go_back_in_time"), Handler: handleGoBackInTime, Context: chatView(chatIdleOrCompleted)},
		{ID: config.ActionID(config.NamespaceNavigation, "scroll_to_top"), Handler: handleScrollToTop, Context: chatView()},
		{ID: config.ActionID(config.NamespaceNavigation, "scroll_to_bottom"), Handler: handleScrollToBottom, Context: chatView()},
		{ID: config.ActionID(config.NamespaceNavigation, "scroll_up_half_page"), Handler: handleScrollUpHalfPage, Context: chatView()},
		{ID: config.ActionID(config.NamespaceNavigation, "scroll_down_half_page"), Handler: handleScrollDownHalfPage, Context: chatView()},
		{ID: config.ActionID(config.NamespaceNavigation, "page_up"), Handler: handlePageUp, Context: chatView()},
		{ID: config.ActionID(config.NamespaceNavigation, "page_down"), Handler: handlePageDown, Context: chatView()},

		{ID: config.ActionID(config.NamespacePlanApproval, "plan_approval_left"), Handler: handlePlanApprovalLeft, Context: planApprovalView},
		{ID: config.ActionID(config.NamespacePlanApproval, "plan_approval_right"), Handler: handlePlanApprovalRight, Context: planApprovalView},
		{ID: config.ActionID(config.NamespacePlanApproval, "plan_approval_accept"), Handler: handlePlanApprovalAccept, Context: planApprovalView},
		{ID: config.ActionID(config.NamespacePlanApproval, "plan_approval_reject"), Handler: handlePlanApprovalReject, Context: planApprovalView},
		{ID: config.ActionID(config.NamespacePlanApproval, "plan_approval_accept_standard"), Handler: handlePlanApprovalAcceptStandard, Context: planApprovalView},
	}
}

// Handler implementations
func handleQuit(app KeyHandlerContext, keyMsg tea.KeyPressMsg) tea.Cmd {
	return tea.Quit
}

func handleCancel(app KeyHandlerContext, keyMsg tea.KeyPressMsg) tea.Cmd {
	stateManager := app.GetStateManager()

	if stateManager.IsEditingMessage() {
		stateManager.ClearMessageEditState()

		input := app.GetInputView()
		if input != nil {
			input.ClearInput()
			if iv, ok := input.(*components.InputView); ok {
				iv.ClearCustomHint()
			}
		}

		return nil
	}

	autocomplete := app.GetAutocomplete()
	if autocomplete != nil && autocomplete.IsVisible() {
		return func() tea.Msg {
			return domain.AutocompleteHideEvent{}
		}
	}

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

	// Dismiss any pending AskUserQuestion form so it doesn't linger after the
	// turn is cancelled (closing the channel unblocks the tool's Execute).
	stateManager.ClearUserQuestionUIState()
	stateManager.EndChatSession()
	stateManager.EndToolExecution()
	_ = stateManager.TransitionToView(domain.ViewStateChat)

	return func() tea.Msg {
		return domain.SetStatusEvent{
			Message: "User interrupted",
			Spinner: false,
		}
	}
}

func handleNewSession(app KeyHandlerContext, keyMsg tea.KeyPressMsg) tea.Cmd {
	stateManager := app.GetStateManager()

	if chatSession := stateManager.GetChatSession(); chatSession != nil {
		agentService := app.GetAgentService()
		if agentService != nil {
			_ = agentService.CancelRequest(chatSession.RequestID)
		}
	}

	stateManager.ClearUserQuestionUIState()
	stateManager.EndChatSession()
	stateManager.EndToolExecution()
	_ = stateManager.TransitionToView(domain.ViewStateChat)

	conversationRepo := app.GetConversationRepository()
	if conversationRepo != nil {
		_ = conversationRepo.Clear()
	}

	input := app.GetInputView()
	if input != nil {
		input.ClearInput()
		if iv, ok := input.(*components.InputView); ok {
			iv.ClearCustomHint()
		}
	}

	return tea.Batch(
		func() tea.Msg {
			return domain.UpdateHistoryEvent{
				History: []domain.ConversationEntry{},
			}
		},
		func() tea.Msg {
			return domain.ClearInputEvent{}
		},
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message: "New session started",
				Spinner: false,
			}
		},
	)
}

func handleToggleToolExpansion(app KeyHandlerContext, keyMsg tea.KeyPressMsg) tea.Cmd {
	app.ToggleToolResultExpansion()
	return nil
}

func handleToggleThinkingExpansion(app KeyHandlerContext, keyMsg tea.KeyPressMsg) tea.Cmd {
	app.ToggleThinkingExpansion()
	return nil
}

func handleBackgroundShell(app KeyHandlerContext, keyMsg tea.KeyPressMsg) tea.Cmd {
	return func() tea.Msg {
		return domain.BackgroundShellRequestEvent{}
	}
}

func handleToggleRawFormat(app KeyHandlerContext, keyMsg tea.KeyPressMsg) tea.Cmd {
	app.ToggleRawFormat()
	return func() tea.Msg {
		return domain.SetStatusEvent{
			Message: "Toggled raw/rendered format",
			Spinner: false,
		}
	}
}

func handleEnterKey(app KeyHandlerContext, keyMsg tea.KeyPressMsg) tea.Cmd {
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
				cursorPos := autocomplete.GetCompletionCursorPos()
				return func() tea.Msg {
					return domain.AutocompleteCompleteEvent{
						Completion: completion,
						CursorPos:  cursorPos,
					}
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

// handleTabKey handles Tab key press: routes to autocomplete when visible,
// otherwise delegates to input view for history suggestion cycling.
func handleTabKey(app KeyHandlerContext, keyMsg tea.KeyPressMsg) tea.Cmd {
	autocomplete := app.GetAutocomplete()
	if autocomplete != nil && autocomplete.IsVisible() {
		if handled, completion := autocomplete.HandleKey(keyMsg); handled {
			if completion != "" {
				cursorPos := autocomplete.GetCompletionCursorPos()
				return func() tea.Msg {
					return domain.AutocompleteCompleteEvent{
						Completion: completion,
						CursorPos:  cursorPos,
					}
				}
			}
			return nil
		}
	}

	inputView := app.GetInputView()
	if inputView != nil {
		if iv, ok := inputView.(*components.InputView); ok {
			iv.TryHandleHistorySuggestionTab()
		}
	}
	return nil
}

// handleImagePaste processes clipboard image data and adds it as an attachment
func handleImagePaste(app KeyHandlerContext, imageService domain.ImageService, inputView ui.InputComponent, imageData []byte) bool {
	timestamp := time.Now().Format("20060102-150405")
	tmpDir := filepath.Join(app.GetConfigDir(), "tmp")

	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		logger.Warn("failed to create tmp directory", "path", tmpDir, "error", err)
		return false
	}

	tmpPath := filepath.Join(tmpDir, fmt.Sprintf("clipboard-image-%s.png", timestamp))

	if err := os.WriteFile(tmpPath, imageData, 0644); err != nil {
		logger.Warn("failed to save clipboard image", "path", tmpPath, "error", err)
		return false
	}

	cfg := app.GetConfig()
	finalPath := applyImageOptimization(tmpPath, imageData, cfg)

	imageAttachment, err := imageService.ReadImageFromFile(finalPath)
	if err != nil {
		logger.Warn("failed to read saved clipboard image", "path", finalPath, "error", err)
		return false
	}

	imageAttachment.SourcePath = finalPath

	inputView.AddImageAttachment(*imageAttachment)
	return true
}

// applyImageOptimization applies image optimization if enabled in config
// Returns the final file path (which may have a different extension)
func applyImageOptimization(tmpPath string, originalData []byte, cfg *config.Config) string {
	if cfg == nil || !cfg.Image.ClipboardOptimize.Enabled {
		logger.Info("clipboard image saved (you can inspect this file)", "path", tmpPath)
		return tmpPath
	}

	result, err := optimizeClipboardImage(tmpPath, cfg.Image.ClipboardOptimize)
	if err != nil {
		logger.Warn("failed to optimize clipboard image, using original", "error", err)
		return tmpPath
	}

	finalPath := tmpPath
	if result.Extension != "png" {
		finalPath = strings.TrimSuffix(tmpPath, filepath.Ext(tmpPath)) + "." + result.Extension
	}

	if err := os.WriteFile(finalPath, result.Data, 0644); err != nil {
		logger.Warn("failed to save optimized image", "path", finalPath, "error", err)
		return tmpPath
	}

	if finalPath != tmpPath {
		_ = os.Remove(tmpPath)
	}

	logger.Info("clipboard image optimized and saved",
		"path", finalPath, "original_bytes", len(originalData), "optimized_bytes", len(result.Data))

	return finalPath
}

// optimizeClipboardImage optimizes an image according to configuration
func optimizeClipboardImage(imagePath string, cfg config.ClipboardImageOptimizeConfig) (*services.OptimizeResult, error) {
	optimizer := services.NewImageOptimizer(cfg)
	return optimizer.OptimizeImage(imagePath)
}

// clipboardFlashDuration keeps a clipboard-action confirmation on screen long
// enough to read but short enough to read as a flash (well under 3s). When a
// spinner is active the loading indicator is restored within this window.
const clipboardFlashDuration = 1500 * time.Millisecond

// flashStatus shows a short, auto-dismissing status message. When a spinner is
// active it saves and restores the status state so the loading indicator is not
// interrupted (same approach as handleCycleAgentMode); otherwise it clears the
// status line afterwards. Mirrors the double-esc sequence-hint behaviour.
func flashStatus(app KeyHandlerContext, message string) tea.Cmd {
	statusView := app.GetStatusView()
	if statusView != nil && statusView.IsShowingSpinner() {
		return tea.Batch(
			func() tea.Msg { return domain.SaveStatusStateEvent{} },
			func() tea.Msg { return domain.SetStatusEvent{Message: message, Spinner: false} },
			func() tea.Msg {
				time.Sleep(clipboardFlashDuration)
				return domain.RestoreStatusStateEvent{}
			},
		)
	}

	return tea.Batch(
		func() tea.Msg { return domain.SetStatusEvent{Message: message, Spinner: false} },
		func() tea.Msg {
			time.Sleep(clipboardFlashDuration)
			return domain.SetStatusEvent{Message: "", Spinner: false}
		},
	)
}

func handlePaste(app KeyHandlerContext, keyMsg tea.KeyPressMsg) tea.Cmd {
	inputView := app.GetInputView()
	if inputView == nil {
		return nil
	}

	imageService := app.GetImageService()

	imageData := clipboard.Read(clipboard.FmtImage)
	if len(imageData) > 0 {
		if handleImagePaste(app, imageService, inputView, imageData) {
			return flashStatus(app, "Image pasted from clipboard")
		}
		return nil
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
			return flashStatus(app, "Image pasted from clipboard")
		}
	}

	currentText := inputView.GetInput()
	cursor := inputView.GetCursor()

	newText := currentText[:cursor] + cleanText + currentText[cursor:]
	newCursor := cursor + len(cleanText)

	inputView.SetText(newText)
	inputView.SetCursor(newCursor)

	return flashStatus(app, "Text pasted from clipboard")
}

func handleCopy(app KeyHandlerContext, keyMsg tea.KeyPressMsg) tea.Cmd {
	inputView := app.GetInputView()
	if inputView == nil {
		return nil
	}

	text := inputView.GetInput()
	if text == "" {
		return nil
	}

	clipboard.Write(clipboard.FmtText, []byte(text))
	return flashStatus(app, "Copied to clipboard")
}

func handleGoBackInTime(app KeyHandlerContext, keyMsg tea.KeyPressMsg) tea.Cmd {
	return func() tea.Msg {
		return domain.NavigateBackInTimeEvent{
			RequestID: "navigate-back-in-time",
			Timestamp: time.Now(),
		}
	}
}

func handleScrollToTop(app KeyHandlerContext, keyMsg tea.KeyPressMsg) tea.Cmd {
	return func() tea.Msg {
		return domain.ScrollRequestEvent{
			ComponentID: "conversation",
			Direction:   domain.ScrollToTop,
			Amount:      0,
		}
	}
}

func handleScrollToBottom(app KeyHandlerContext, keyMsg tea.KeyPressMsg) tea.Cmd {
	return func() tea.Msg {
		return domain.ScrollRequestEvent{
			ComponentID: "conversation",
			Direction:   domain.ScrollToBottom,
			Amount:      0,
		}
	}
}

func handleScrollUpHalfPage(app KeyHandlerContext, keyMsg tea.KeyPressMsg) tea.Cmd {
	return func() tea.Msg {
		return domain.ScrollRequestEvent{
			ComponentID: "conversation",
			Direction:   domain.ScrollUp,
			Amount:      10,
		}
	}
}

func handleScrollDownHalfPage(app KeyHandlerContext, keyMsg tea.KeyPressMsg) tea.Cmd {
	return func() tea.Msg {
		return domain.ScrollRequestEvent{
			ComponentID: "conversation",
			Direction:   domain.ScrollDown,
			Amount:      10,
		}
	}
}

func handlePageUp(app KeyHandlerContext, keyMsg tea.KeyPressMsg) tea.Cmd {
	return func() tea.Msg {
		return domain.ScrollRequestEvent{
			ComponentID: "conversation",
			Direction:   domain.ScrollUp,
			Amount:      20,
		}
	}
}

func handlePageDown(app KeyHandlerContext, keyMsg tea.KeyPressMsg) tea.Cmd {
	return func() tea.Msg {
		return domain.ScrollRequestEvent{
			ComponentID: "conversation",
			Direction:   domain.ScrollDown,
			Amount:      20,
		}
	}
}

// Text editing handlers
func handleCursorLeftOrPlanNav(app KeyHandlerContext, keyMsg tea.KeyPressMsg) tea.Cmd {
	stateManager := app.GetStateManager()

	approvalState := stateManager.GetApprovalUIState()
	if approvalState != nil {
		newIndex := approvalState.SelectedIndex - 1
		if newIndex < 0 {
			newIndex = int(domain.ApprovalAutoAccept)
		}
		stateManager.SetApprovalSelectedIndex(newIndex)
		return func() tea.Msg {
			return domain.ApprovalSelectionChangedEvent{NewIndex: newIndex}
		}
	}

	planApprovalState := stateManager.GetPlanApprovalUIState()
	if planApprovalState != nil {
		newIndex := planApprovalState.SelectedIndex - 1
		if newIndex < 0 {
			newIndex = int(domain.PlanApprovalAcceptStandard)
		}
		stateManager.SetPlanApprovalSelectedIndex(newIndex)
		return func() tea.Msg {
			return domain.PlanApprovalSelectionChangedEvent{NewIndex: newIndex}
		}
	}

	return handleInputChangedAfterTextarea(app, false)
}

func handleCursorRightOrPlanNav(app KeyHandlerContext, keyMsg tea.KeyPressMsg) tea.Cmd {
	stateManager := app.GetStateManager()

	approvalState := stateManager.GetApprovalUIState()
	if approvalState != nil {
		newIndex := approvalState.SelectedIndex + 1
		if newIndex > int(domain.ApprovalAutoAccept) {
			newIndex = 0
		}

		stateManager.SetApprovalSelectedIndex(newIndex)
		return func() tea.Msg {
			return domain.ApprovalSelectionChangedEvent{NewIndex: newIndex}
		}
	}

	planApprovalState := stateManager.GetPlanApprovalUIState()
	if planApprovalState != nil {
		newIndex := planApprovalState.SelectedIndex + 1
		if newIndex > int(domain.PlanApprovalAcceptStandard) {
			newIndex = 0
		}
		stateManager.SetPlanApprovalSelectedIndex(newIndex)
		return func() tea.Msg {
			return domain.PlanApprovalSelectionChangedEvent{NewIndex: newIndex}
		}
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

			autocomplete := app.GetAutocomplete()
			if autocomplete != nil && autocomplete.IsVisible() {
				autocomplete.Hide()
			}

			return nil
		}
	}

	return handleInputChangedAfterTextarea(app, false)
}

func handleBackspace(app KeyHandlerContext, keyMsg tea.KeyPressMsg) tea.Cmd {
	return handleInputChangedAfterTextarea(app, false)
}

func handleHistoryUp(app KeyHandlerContext, keyMsg tea.KeyPressMsg) tea.Cmd {
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

func handleHistoryDown(app KeyHandlerContext, keyMsg tea.KeyPressMsg) tea.Cmd {
	inputView := app.GetInputView()
	autocomplete := app.GetAutocomplete()
	if inputView != nil {
		if autocomplete != nil && autocomplete.IsVisible() {
			autocomplete.HandleKey(keyMsg)
			return nil
		}
		if !inputView.IsNavigatingHistory() {
			return func() tea.Msg { return domain.FocusStatusBarEvent{} }
		}
		inputView.NavigateHistoryDown()
	}
	return nil
}

func handleDeleteToBeginning(app KeyHandlerContext, keyMsg tea.KeyPressMsg) tea.Cmd {
	return handleInputChangedAfterTextarea(app, false)
}

func handleDeleteWordBackward(app KeyHandlerContext, keyMsg tea.KeyPressMsg) tea.Cmd {
	return handleInputChangedAfterTextarea(app, false)
}

func handleDeleteWordForward(app KeyHandlerContext, keyMsg tea.KeyPressMsg) tea.Cmd {
	return handleInputChangedAfterTextarea(app, false)
}

func handleMoveCursorWordLeft(app KeyHandlerContext, keyMsg tea.KeyPressMsg) tea.Cmd {
	return handleInputChangedAfterTextarea(app, false)
}

func handleMoveCursorWordRight(app KeyHandlerContext, keyMsg tea.KeyPressMsg) tea.Cmd {
	return handleInputChangedAfterTextarea(app, false)
}

func handleMoveToBeginning(app KeyHandlerContext, keyMsg tea.KeyPressMsg) tea.Cmd {
	return handleInputChangedAfterTextarea(app, false)
}

func handleMoveToEnd(app KeyHandlerContext, keyMsg tea.KeyPressMsg) tea.Cmd {
	return handleInputChangedAfterTextarea(app, false)
}

func handleInsertNewline(app KeyHandlerContext, keyMsg tea.KeyPressMsg) tea.Cmd {
	return handleInputChangedAfterTextarea(app, false)
}

func handleToggleHelp(app KeyHandlerContext, keyMsg tea.KeyPressMsg) tea.Cmd {
	return func() tea.Msg {
		return domain.ToggleHelpBarEvent{}
	}
}

func handleToggleTodoBox(app KeyHandlerContext, keyMsg tea.KeyPressMsg) tea.Cmd {
	return func() tea.Msg {
		return domain.ToggleTodoBoxEvent{}
	}
}

func handleCycleAgentMode(app KeyHandlerContext, keyMsg tea.KeyPressMsg) tea.Cmd {
	stateManager := app.GetStateManager()
	statusView := app.GetStatusView()
	newMode := stateManager.CycleAgentMode()

	if statusView.IsShowingSpinner() {
		return tea.Batch(
			func() tea.Msg {
				return domain.SaveStatusStateEvent{}
			},
			func() tea.Msg {
				return domain.SetStatusEvent{
					Message: fmt.Sprintf("Mode changed to: %s", newMode.DisplayName()),
					Spinner: false,
				}
			},
			func() tea.Msg {
				time.Sleep(800 * time.Millisecond)
				return domain.RestoreStatusStateEvent{}
			},
			func() tea.Msg {
				return domain.RefreshAutocompleteEvent{}
			},
		)
	}

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

func handleToggleMouseMode(app KeyHandlerContext, keyMsg tea.KeyPressMsg) tea.Cmd {
	mouseEnabled := app.GetMouseEnabled()
	app.SetMouseEnabled(!mouseEnabled)

	if !mouseEnabled {
		return tea.Batch(
			// Bubble Tea v2 controls mouse mode via View.MouseMode rather
			// than a one-shot command; ChatApplication.View() reflects
			// app.GetMouseEnabled() into View.MouseMode. The status event
			// here keeps the user feedback unchanged.
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
	registry            *Registry
	app                 KeyHandlerContext
	keySequenceBuffer   []string
	lastKeyTime         time.Time
	sequenceTimeout     time.Duration
	sequenceConsumedKey string
}

const maxSequenceLength = 2

var textareaInputActionIDs = map[string]struct{}{
	config.ActionID(config.NamespaceTextEditing, "insert_newline_alt"):     {},
	config.ActionID(config.NamespaceTextEditing, "insert_newline_ctrl"):    {},
	config.ActionID(config.NamespaceTextEditing, "move_cursor_left"):       {},
	config.ActionID(config.NamespaceTextEditing, "move_cursor_right"):      {},
	config.ActionID(config.NamespaceTextEditing, "backspace"):              {},
	config.ActionID(config.NamespaceTextEditing, "delete_to_beginning"):    {},
	config.ActionID(config.NamespaceTextEditing, "delete_word_backward"):   {},
	config.ActionID(config.NamespaceTextEditing, "delete_word_forward"):    {},
	config.ActionID(config.NamespaceTextEditing, "move_cursor_word_left"):  {},
	config.ActionID(config.NamespaceTextEditing, "move_cursor_word_right"): {},
	config.ActionID(config.NamespaceTextEditing, "move_to_beginning"):      {},
	config.ActionID(config.NamespaceTextEditing, "move_to_end"):            {},
}

// NewKeyBindingManager creates a new key binding manager
func NewKeyBindingManager(app KeyHandlerContext, cfg *config.Config) *KeyBindingManager {
	return &KeyBindingManager{
		registry:          NewRegistry(cfg),
		app:               app,
		keySequenceBuffer: make([]string, 0, maxSequenceLength),
		sequenceTimeout:   300 * time.Millisecond,
	}
}

// ProcessKey handles key input and executes the appropriate action
func (m *KeyBindingManager) ProcessKey(keyMsg tea.KeyPressMsg) tea.Cmd {
	keyStr := keyMsg.String()
	var cmds []tea.Cmd

	if debugCmd := m.addDebugCmd(keyStr, keyMsg); debugCmd != nil {
		cmds = append(cmds, debugCmd)
	}

	now := time.Now()
	if timeoutCmd := m.handleSequenceTimeout(now, keyMsg); timeoutCmd != nil {
		return timeoutCmd
	}

	m.keySequenceBuffer = append(m.keySequenceBuffer, keyStr)
	m.lastKeyTime = now

	if len(m.keySequenceBuffer) > maxSequenceLength {
		m.keySequenceBuffer = m.keySequenceBuffer[:0]
		m.keySequenceBuffer = append(m.keySequenceBuffer, keyStr)
	}

	sequenceKey := m.joinSequence(m.keySequenceBuffer)

	if cmd := m.handleMultiKeySequence(sequenceKey, keyMsg, cmds); cmd != nil {
		return cmd
	}

	if cmd := m.handleSingleKey(keyStr, keyMsg, cmds); cmd != nil {
		return cmd
	}

	return m.batchCmds(cmds)
}

func (m *KeyBindingManager) addDebugCmd(keyStr string, keyMsg tea.KeyPressMsg) tea.Cmd {
	config := m.app.GetConfig()
	if config == nil || !config.Logging.Debug {
		return nil
	}

	debugInfo := keyStr
	if len(keyStr) == 1 {
		debugInfo = fmt.Sprintf("%s (char: 0x%02X)", keyStr, keyStr[0])
	}
	return m.debugKeyBinding(keyMsg, debugInfo)
}

func (m *KeyBindingManager) handleSequenceTimeout(now time.Time, keyMsg tea.KeyPressMsg) tea.Cmd {
	if m.lastKeyTime.IsZero() || now.Sub(m.lastKeyTime) <= m.sequenceTimeout {
		return nil
	}

	if len(m.keySequenceBuffer) == 1 {
		pendingKey := m.keySequenceBuffer[0]
		m.keySequenceBuffer = m.keySequenceBuffer[:0]

		action := m.registry.ResolveKey(pendingKey, m.app)
		if action != nil {
			pendingCmd := action.Handler(m.app, tea.KeyPressMsg{})
			newKeyCmd := m.ProcessKey(keyMsg)
			return m.batchCmds([]tea.Cmd{pendingCmd, newKeyCmd})
		}
	}
	m.keySequenceBuffer = m.keySequenceBuffer[:0]
	return nil
}

func (m *KeyBindingManager) handleMultiKeySequence(sequenceKey string, keyMsg tea.KeyPressMsg, cmds []tea.Cmd) tea.Cmd {
	if len(m.keySequenceBuffer) <= 1 {
		return nil
	}

	sequenceAction := m.registry.ResolveKey(sequenceKey, m.app)
	if sequenceAction != nil {
		m.keySequenceBuffer = m.keySequenceBuffer[:0]
		m.sequenceConsumedKey = keyMsg.String()
		actionCmd := sequenceAction.Handler(m.app, keyMsg)
		return m.batchCmds(append(cmds, actionCmd))
	}
	return m.batchCmds(cmds)
}

func (m *KeyBindingManager) handleSingleKey(keyStr string, keyMsg tea.KeyPressMsg, cmds []tea.Cmd) tea.Cmd {
	if len(m.keySequenceBuffer) != 1 {
		return nil
	}

	if statusCmds := m.showSequenceHint(keyStr); statusCmds != nil {
		return m.batchCmds(append(cmds, statusCmds...))
	}

	action := m.registry.Resolve(keyMsg, m.app)
	if action != nil {
		m.keySequenceBuffer = m.keySequenceBuffer[:0]
		actionCmd := action.Handler(m.app, keyMsg)
		return m.batchCmds(append(cmds, actionCmd))
	}

	m.keySequenceBuffer = m.keySequenceBuffer[:0]
	charCmd := handleCharacterInput(m.app, keyMsg)
	return m.batchCmds(append(cmds, charCmd))
}

func (m *KeyBindingManager) showSequenceHint(keyStr string) []tea.Cmd {
	sequenceAction := m.registry.GetSequenceActionForPrefix(keyStr, m.app)
	if sequenceAction == nil {
		return nil
	}

	statusCmd := func() tea.Msg {
		return domain.SetStatusEvent{
			Message: sequenceAction.Binding.Help().Desc,
			Spinner: false,
		}
	}

	clearStatusCmd := func() tea.Msg {
		time.Sleep(1 * time.Second)
		return domain.SetStatusEvent{
			Message: "",
			Spinner: false,
		}
	}

	return []tea.Cmd{statusCmd, clearStatusCmd}
}

func (m *KeyBindingManager) batchCmds(cmds []tea.Cmd) tea.Cmd {
	var validCmds []tea.Cmd
	for _, cmd := range cmds {
		if cmd != nil {
			validCmds = append(validCmds, cmd)
		}
	}

	if len(validCmds) == 0 {
		return nil
	}
	if len(validCmds) == 1 {
		return validCmds[0]
	}
	return tea.Batch(validCmds...)
}

// joinSequence joins key sequence buffer into a comma-separated string
func (m *KeyBindingManager) joinSequence(keys []string) string {
	if len(keys) == 0 {
		return ""
	}
	if len(keys) == 1 {
		return keys[0]
	}
	result := keys[0]
	for i := 1; i < len(keys); i++ {
		result += "," + keys[i]
	}
	return result
}

// IsKeyHandledByAction returns true if the key would be handled by a keybinding action
func (m *KeyBindingManager) IsKeyHandledByAction(keyMsg tea.KeyPressMsg) bool {
	return m.registry.Resolve(keyMsg, m.app) != nil
}

// ShouldSkipInputUpdate reports whether a keybinding action fully consumed the
// key. Textarea-owned editing actions still pass through to InputView.Update.
func (m *KeyBindingManager) ShouldSkipInputUpdate(keyMsg tea.KeyPressMsg) bool {
	keyStr := keyMsg.String()
	if m.sequenceConsumedKey == keyStr {
		m.sequenceConsumedKey = ""
		return true
	}
	action := m.registry.Resolve(keyMsg, m.app)
	if action == nil {
		return false
	}
	_, passThrough := textareaInputActionIDs[action.ID]
	return !passThrough
}

// GetHelpShortcuts returns help shortcuts for the current context
func (m *KeyBindingManager) GetHelpShortcuts() []HelpShortcut {
	return m.registry.GetHelpShortcuts(m.app)
}

// GetRegistry returns the underlying registry (for advanced usage)
func (m *KeyBindingManager) GetRegistry() *Registry {
	return m.registry
}

// GetHintFormatter returns a hint formatter for displaying keybinding hints in UI
func (m *KeyBindingManager) GetHintFormatter() *hints.Formatter {
	return NewHintFormatterFromRegistry(m.registry)
}

// debugKeyBinding logs key binding events when debug mode is enabled
func (m *KeyBindingManager) debugKeyBinding(keyMsg tea.KeyPressMsg, info string) tea.Cmd {
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

func handleCharacterInput(app KeyHandlerContext, keyMsg tea.KeyPressMsg) tea.Cmd {
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
	if inputView != nil && inputView.IsDisabled() {
		return nil
	}

	if inputView != nil {
		if inputView.CanHandle(keyMsg) {
			_, cmd := inputView.HandleKey(keyMsg)
			if cmd != nil {
				return cmd
			}
		}
	}

	if literal := keys.PrintableText(keyMsg); literal != "" {
		return handleInputChangedAfterTextarea(app, literal == "@")
	}
	return nil
}

// handleInputChangedAfterTextarea emits side effects for a key that the
// textarea will apply later in this Bubble Tea update cycle.
func handleInputChangedAfterTextarea(app KeyHandlerContext, openFileSelection bool) tea.Cmd {
	if app.GetInputView() == nil {
		return nil
	}

	scrollCmd := func() tea.Msg {
		return domain.ScrollRequestEvent{
			ComponentID: "conversation",
			Direction:   domain.ScrollToBottom,
			Amount:      0,
		}
	}

	if openFileSelection {
		return tea.Batch(
			scrollCmd,
			func() tea.Msg {
				return domain.FileSelectionRequestEvent{}
			},
		)
	}

	return tea.Batch(
		scrollCmd,
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

	if cleanText == "" {
		return nil
	}

	cursor := inputView.GetCursor()
	text := inputView.GetInput()
	newText := text[:cursor] + cleanText + text[cursor:]
	newCursor := cursor + len(cleanText)

	inputView.SetText(newText)
	inputView.SetCursor(newCursor)

	return flashStatus(app, "Text pasted from clipboard")
}

// Approval handlers
// Plan Approval handlers

func handlePlanApprovalLeft(app KeyHandlerContext, keyMsg tea.KeyPressMsg) tea.Cmd {
	stateManager := app.GetStateManager()
	planApprovalState := stateManager.GetPlanApprovalUIState()
	if planApprovalState == nil {
		return nil
	}

	newIndex := planApprovalState.SelectedIndex - 1
	if newIndex < 0 {
		newIndex = int(domain.PlanApprovalAcceptStandard)
	}
	stateManager.SetPlanApprovalSelectedIndex(newIndex)

	return func() tea.Msg {
		return domain.PlanApprovalSelectionChangedEvent{NewIndex: newIndex}
	}
}

func handlePlanApprovalRight(app KeyHandlerContext, keyMsg tea.KeyPressMsg) tea.Cmd {
	stateManager := app.GetStateManager()
	planApprovalState := stateManager.GetPlanApprovalUIState()
	if planApprovalState == nil {
		return nil
	}

	newIndex := planApprovalState.SelectedIndex + 1
	if newIndex > int(domain.PlanApprovalAcceptStandard) {
		newIndex = 0
	}
	stateManager.SetPlanApprovalSelectedIndex(newIndex)

	return func() tea.Msg {
		return domain.PlanApprovalSelectionChangedEvent{NewIndex: newIndex}
	}
}

func handlePlanApprovalAccept(app KeyHandlerContext, keyMsg tea.KeyPressMsg) tea.Cmd {
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

func handlePlanApprovalReject(app KeyHandlerContext, keyMsg tea.KeyPressMsg) tea.Cmd {
	return func() tea.Msg {
		return domain.PlanApprovalResponseEvent{
			Action: domain.PlanApprovalReject,
		}
	}
}

func handlePlanApprovalAcceptStandard(app KeyHandlerContext, keyMsg tea.KeyPressMsg) tea.Cmd {
	return func() tea.Msg {
		return domain.PlanApprovalResponseEvent{
			Action: domain.PlanApprovalAcceptStandard,
		}
	}
}
