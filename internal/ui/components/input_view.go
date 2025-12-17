package components

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	formatting "github.com/inference-gateway/cli/internal/formatting"
	history "github.com/inference-gateway/cli/internal/ui/history"
	keys "github.com/inference-gateway/cli/internal/ui/keys"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

// InputView handles user input with history
type InputView struct {
	text                 string
	cursor               int
	placeholder          string
	width                int
	height               int
	modelService         domain.ModelService
	imageService         domain.ImageService
	stateManager         domain.StateManager
	configService        *config.Config
	conversationRepo     domain.ConversationRepository
	historyManager       *history.HistoryManager
	disabled             bool
	savedText            string
	savedCursor          int
	themeService         domain.ThemeService
	styleProvider        *styles.Provider
	imageAttachments     []domain.ImageAttachment
	historySuggestion    string
	historySuggestions   []string
	historySelectedIndex int
	focused              bool
	usageHint            string
	customHint           string
}

func NewInputView(modelService domain.ModelService) *InputView {
	return NewInputViewWithConfigDir(modelService, "")
}

func NewInputViewWithConfigDir(modelService domain.ModelService, configDir string) *InputView {
	if configDir == "" {
		configDir = ".infer"
	}

	historyManager, err := history.NewHistoryManagerWithDir(5, configDir)
	if err != nil {
		historyManager = history.NewMemoryOnlyHistoryManager(5)
	}

	return &InputView{
		text:             "",
		cursor:           0,
		placeholder:      "Type your message... (Press Enter to send, alt+enter or ctrl+j for newline, ? for help)",
		width:            80,
		height:           5,
		modelService:     modelService,
		historyManager:   historyManager,
		themeService:     nil,
		imageAttachments: []domain.ImageAttachment{},
		focused:          true,
	}
}

// SetThemeService sets the theme service for this input view
func (iv *InputView) SetThemeService(themeService domain.ThemeService) {
	iv.themeService = themeService
	iv.styleProvider = styles.NewProvider(themeService)
}

// SetStateManager sets the state manager for this input view
func (iv *InputView) SetStateManager(stateManager domain.StateManager) {
	iv.stateManager = stateManager
}

// SetConfigService sets the config service for this input view
func (iv *InputView) SetConfigService(configService *config.Config) {
	iv.configService = configService
}

// SetImageService sets the image service for this input view
func (iv *InputView) SetImageService(imageService domain.ImageService) {
	iv.imageService = imageService
}

// SetConversationRepo sets the conversation repository for context usage display
func (iv *InputView) SetConversationRepo(repo domain.ConversationRepository) {
	iv.conversationRepo = repo
}

func (iv *InputView) GetInput() string {
	return iv.text
}

func (iv *InputView) ClearInput() {
	iv.text = ""
	iv.cursor = 0
	iv.imageAttachments = []domain.ImageAttachment{}
	iv.historyManager.ResetNavigation()
}

func (iv *InputView) SetPlaceholder(text string) {
	iv.placeholder = text
}

func (iv *InputView) GetCursor() int {
	return iv.cursor
}

func (iv *InputView) SetCursor(position int) {
	if position >= 0 && position <= len(iv.text) {
		iv.cursor = position
	}
}

func (iv *InputView) SetText(text string) {
	iv.text = text
}

func (iv *InputView) SetWidth(width int) {
	iv.width = width
}

func (iv *InputView) SetHeight(height int) {
	iv.height = height
}

func (iv *InputView) Render() string {
	if !iv.disabled {
		iv.updateHistorySuggestions()
	}

	isToolsMode := strings.HasPrefix(iv.text, "!!")
	isBashMode := strings.HasPrefix(iv.text, "!") && !isToolsMode
	displayText := iv.renderDisplayText()

	inputContent := fmt.Sprintf("> %s", displayText)

	focused := isBashMode || isToolsMode
	borderedInput := iv.styleProvider.RenderInputField(inputContent, iv.width-4, focused)

	return borderedInput
}

func (iv *InputView) renderDisplayText() string {
	if iv.text == "" {
		return iv.renderPlaceholder()
	}
	return iv.renderTextWithCursor()
}

// getDisplayTextAndCursorOffset returns the display text with mode prefixes and cursor offset adjustment
// For bash mode (!), we show "! " prefix; for tools mode (!!), we show "!! " prefix
func (iv *InputView) getDisplayTextAndCursorOffset() (displayText string, cursorOffset int) {
	isToolsMode := strings.HasPrefix(iv.text, "!!")
	isBashMode := strings.HasPrefix(iv.text, "!") && !isToolsMode

	if isToolsMode {
		return "!! " + iv.text[2:], 3
	} else if isBashMode {
		return "! " + iv.text[1:], 2
	}
	return iv.text, 0
}

func (iv *InputView) renderPlaceholder() string {
	if iv.disabled {
		return iv.renderDisabledPlaceholder()
	}

	if !iv.focused {
		return iv.styleProvider.RenderInputPlaceholder(iv.placeholder)
	}

	return iv.renderFocusedPlaceholder()
}

// renderDisabledPlaceholder returns placeholder text when input is disabled
func (iv *InputView) renderDisabledPlaceholder() string {
	if iv.customHint != "" {
		return iv.styleProvider.RenderDimText("⏸  " + iv.customHint)
	}

	if iv.stateManager == nil {
		return iv.styleProvider.RenderDimText("⏸  Input disabled")
	}

	if iv.stateManager.GetPlanApprovalUIState() != nil {
		return iv.styleProvider.RenderDimText("⏸  Plan approval required - use ←/→ or h/l to navigate, Enter/y to accept, n to reject, a for auto-approve")
	}

	if iv.stateManager.GetApprovalUIState() != nil {
		return iv.styleProvider.RenderDimText("⏸  Tool approval required - use ←/→ to navigate, Enter to confirm")
	}

	return iv.styleProvider.RenderDimText("⏸  Input disabled")
}

// renderFocusedPlaceholder returns placeholder text when input is focused
func (iv *InputView) renderFocusedPlaceholder() string {
	if len(iv.placeholder) == 0 {
		return iv.createCursorChar(" ")
	}

	firstChar := string(iv.placeholder[0])
	rest := ""
	if len(iv.placeholder) > 1 {
		rest = iv.placeholder[1:]
	}
	cursorChar := iv.createCursorChar(firstChar)
	restPlaceholder := iv.styleProvider.RenderInputPlaceholder(rest)
	return cursorChar + restPlaceholder
}

// calculateAdjustedCursor calculates the cursor position for display text
// accounting for the space added after mode prefixes (! or !!)
func (iv *InputView) calculateAdjustedCursor(cursorOffset int, displayTextLen int) int {
	adjustedCursor := iv.cursor

	if cursorOffset > 0 {
		adjustedCursor = iv.calculateModeCursorOffset()
	}

	if adjustedCursor > displayTextLen {
		adjustedCursor = displayTextLen
	}

	return adjustedCursor
}

// calculateModeCursorOffset returns the adjusted cursor position for bash/tools mode
func (iv *InputView) calculateModeCursorOffset() int {
	isToolsMode := strings.HasPrefix(iv.text, "!!")

	if isToolsMode && iv.cursor >= 2 {
		return iv.cursor + 1
	}
	if !isToolsMode && iv.cursor >= 1 {
		return iv.cursor + 1
	}

	return iv.cursor
}

func (iv *InputView) renderTextWithCursor() string {
	displayText, cursorOffset := iv.getDisplayTextAndCursorOffset()
	adjustedCursor := iv.calculateAdjustedCursor(cursorOffset, len(displayText))

	before := displayText[:adjustedCursor]
	after := displayText[adjustedCursor:]
	availableWidth := iv.width - 8

	var result string
	if availableWidth > 0 {
		result = iv.renderWrappedText(before, after, availableWidth)
	} else {
		result = iv.renderUnwrappedText(before, after)
	}

	return iv.applyModePrefixStyling(result)
}

func (iv *InputView) renderWrappedText(before, after string, availableWidth int) string {
	wrappedBefore := iv.preserveTrailingSpaces(before, availableWidth)
	wrappedAfter := formatting.WrapText(after, availableWidth)
	return iv.buildTextWithCursor(wrappedBefore, wrappedAfter)
}

func (iv *InputView) renderUnwrappedText(before, after string) string {
	return iv.buildTextWithCursor(before, after)
}

func (iv *InputView) buildTextWithCursor(before, after string) string {
	if !iv.focused {
		return iv.buildUnfocusedText(before, after)
	}

	if len(after) == 0 {
		return iv.buildEndOfTextWithCursor(before)
	}

	cursorChar := iv.createCursorChar(string(after[0]))
	restAfter := ""
	if len(after) > 1 {
		restAfter = after[1:]
	}
	return fmt.Sprintf("%s%s%s", before, cursorChar, restAfter)
}

// buildUnfocusedText renders text when input is not focused
func (iv *InputView) buildUnfocusedText(before, after string) string {
	if len(after) == 0 {
		if iv.cursor == len(iv.text) && iv.usageHint != "" {
			ghostText := iv.styleProvider.RenderDimText(iv.usageHint)
			return fmt.Sprintf("%s%s", before, ghostText)
		}
		if iv.cursor == len(iv.text) && iv.historySuggestion != "" {
			ghostText := iv.styleProvider.RenderDimText(iv.historySuggestion)
			return fmt.Sprintf("%s%s", before, ghostText)
		}
		return before
	}
	return fmt.Sprintf("%s%s", before, after)
}

// buildEndOfTextWithCursor renders end of text with cursor and ghost text
func (iv *InputView) buildEndOfTextWithCursor(before string) string {
	if iv.cursor == len(iv.text) && iv.usageHint != "" && len(iv.usageHint) > 0 {
		ghostText := iv.styleProvider.RenderDimText(iv.usageHint)
		cursorChar := iv.createCursorChar(" ")
		return fmt.Sprintf("%s%s%s", before, cursorChar, ghostText)
	}

	if iv.cursor == len(iv.text) && iv.historySuggestion != "" && len(iv.historySuggestion) > 0 {
		firstGhostChar := string(iv.historySuggestion[0])
		cursorChar := iv.createCursorChar(firstGhostChar)
		restGhost := ""
		if len(iv.historySuggestion) > 1 {
			restGhost = iv.styleProvider.RenderDimText(iv.historySuggestion[1:])
		}
		return fmt.Sprintf("%s%s%s", before, cursorChar, restGhost)
	}

	cursorChar := iv.createCursorChar(" ")
	return fmt.Sprintf("%s%s", before, cursorChar)
}

func (iv *InputView) createCursorChar(char string) string {
	return iv.styleProvider.RenderCursor(char)
}

// NavigateHistoryUp moves up in history (to older messages) - public method for interface
func (iv *InputView) NavigateHistoryUp() {
	iv.navigateHistoryUp()
}

// NavigateHistoryDown moves down in history (to newer messages) - public method for interface
func (iv *InputView) NavigateHistoryDown() {
	iv.navigateHistoryDown()
}

// navigateHistoryUp moves up in history (to older messages)
func (iv *InputView) navigateHistoryUp() {
	newText := iv.historyManager.NavigateUp(iv.text)
	iv.text = newText
	iv.cursor = len(iv.text)
}

// navigateHistoryDown moves down in history (to newer messages)
func (iv *InputView) navigateHistoryDown() {
	newText := iv.historyManager.NavigateDown(iv.text)
	iv.text = newText
	iv.cursor = len(iv.text)
}

// AddToHistory adds the current input to the history
func (iv *InputView) AddToHistory(text string) error {
	if text == "" {
		return nil
	}
	return iv.historyManager.AddToHistory(text)
}

// SetUsageHint sets the usage hint for ghost text display
func (iv *InputView) SetUsageHint(hint string) {
	iv.usageHint = hint
}

// GetUsageHint returns the current usage hint
func (iv *InputView) GetUsageHint() string {
	return iv.usageHint
}

// SetCustomHint sets a custom hint
// Note: The input is disabled separately by handleViewSpecificMessages based on navigation mode
func (iv *InputView) SetCustomHint(hint string) {
	iv.customHint = hint
}

// ClearCustomHint clears the custom hint
// Note: The input is re-enabled separately by handleViewSpecificMessages when exiting navigation mode
func (iv *InputView) ClearCustomHint() {
	iv.customHint = ""
}

// Bubble Tea interface
func (iv *InputView) Init() tea.Cmd { return nil }

func (iv *InputView) View() string { return iv.Render() }

func (iv *InputView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		iv.SetWidth(msg.Width)
		return iv, nil
	case tea.FocusMsg:
		iv.focused = true
		return iv, func() tea.Msg { return nil }
	case tea.BlurMsg:
		iv.focused = false
		return iv, func() tea.Msg { return nil }
	case domain.ClearInputEvent:
		iv.ClearInput()
		return iv, nil
	case domain.SetInputEvent:
		iv.SetText(msg.Text)
		iv.SetCursor(len(msg.Text))
		return iv, nil
	}
	return iv, nil
}

func (iv *InputView) HandleKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	keyStr := key.String()

	if keyStr == "tab" {
		if iv.HasHistorySuggestion() && iv.cursor == len(iv.text) {
			iv.cycleHistorySuggestion()
			return iv, nil
		}
	}

	switch keyStr {
	case "up":
		iv.navigateHistoryUp()
		return iv, nil
	case "down":
		iv.navigateHistoryDown()
		return iv, nil
	}

	if keyStr != "up" && keyStr != "down" && keyStr != "left" && keyStr != "right" &&
		keyStr != "ctrl+a" && keyStr != "ctrl+e" && keyStr != "home" && keyStr != "end" {
		iv.historyManager.ResetNavigation()
	}

	return iv, nil
}

func (iv *InputView) CanHandle(key tea.KeyMsg) bool {
	return keys.CanInputHandle(key)
}

func (iv *InputView) preserveTrailingSpaces(text string, availableWidth int) string {
	wrappedText := formatting.WrapText(text, availableWidth)

	trailingSpaces := 0
	for i := len(text) - 1; i >= 0 && text[i] == ' '; i-- {
		trailingSpaces++
	}

	wrappedTrailingSpaces := 0
	for i := len(wrappedText) - 1; i >= 0 && wrappedText[i] == ' '; i-- {
		wrappedTrailingSpaces++
	}

	if trailingSpaces > wrappedTrailingSpaces {
		spacesToAdd := trailingSpaces - wrappedTrailingSpaces
		wrappedText += strings.Repeat(" ", spacesToAdd)
	}

	return wrappedText
}

// SetDisabled sets whether the input is disabled (prevents typing)
// When disabling, saves the current text and clears the input
// When re-enabling, restores the saved text
func (iv *InputView) SetDisabled(disabled bool) {
	if disabled && !iv.disabled {
		iv.savedText = iv.text
		iv.savedCursor = iv.cursor
		iv.text = ""
		iv.cursor = 0
	} else if !disabled && iv.disabled {
		iv.text = iv.savedText
		iv.cursor = iv.savedCursor
		iv.savedText = ""
		iv.savedCursor = 0
	}
	iv.disabled = disabled
}

// IsDisabled returns whether the input is disabled
func (iv *InputView) IsDisabled() bool {
	return iv.disabled
}

// AddImageAttachment adds an image attachment to the pending list
func (iv *InputView) AddImageAttachment(image domain.ImageAttachment) {
	image.DisplayName = fmt.Sprintf("Image #%d", len(iv.imageAttachments)+1)
	iv.imageAttachments = append(iv.imageAttachments, image)

	imageToken := fmt.Sprintf("[%s]", image.DisplayName)
	iv.text = iv.text[:iv.cursor] + imageToken + iv.text[iv.cursor:]
	iv.cursor += len(imageToken)
}

// GetImageAttachments returns the list of pending image attachments
func (iv *InputView) GetImageAttachments() []domain.ImageAttachment {
	return iv.imageAttachments
}

// ClearImageAttachments clears all pending image attachments
func (iv *InputView) ClearImageAttachments() {
	iv.imageAttachments = []domain.ImageAttachment{}
}

// GetHistoryManager returns the history manager for external use
func (iv *InputView) GetHistoryManager() *history.HistoryManager {
	return iv.historyManager
}

// updateHistorySuggestions filters history based on current input and updates suggestions
func (iv *InputView) updateHistorySuggestions() {
	if iv.text == "" || iv.cursor != len(iv.text) {
		iv.historySuggestion = ""
		iv.historySuggestions = nil
		iv.historySelectedIndex = 0
		return
	}

	if iv.historyManager.IsNavigating() {
		return
	}

	count := iv.historyManager.GetHistoryCount()
	matches := make([]string, 0)

	iv.historyManager.ResetNavigation()
	tempHistory := make([]string, 0, count)

	for i := 0; i < count; i++ {
		entry := iv.historyManager.NavigateUp("")
		if entry != "" {
			tempHistory = append([]string{entry}, tempHistory...)
		}
	}
	iv.historyManager.ResetNavigation()

	query := strings.ToLower(iv.text)
	for _, entry := range tempHistory {
		if entry != iv.text && strings.HasPrefix(strings.ToLower(entry), query) {
			matches = append(matches, entry)
		}
	}

	iv.historySuggestions = matches
	if len(matches) > 0 {
		if iv.historySelectedIndex >= len(matches) {
			iv.historySelectedIndex = 0
		}
		iv.historySuggestion = matches[iv.historySelectedIndex][len(iv.text):]
	} else {
		iv.historySuggestion = ""
		iv.historySelectedIndex = 0
	}
}

// cycleHistorySuggestion moves to the next suggestion in the list
func (iv *InputView) cycleHistorySuggestion() {
	if len(iv.historySuggestions) == 0 {
		return
	}

	iv.historySelectedIndex = (iv.historySelectedIndex + 1) % len(iv.historySuggestions)
	iv.historySuggestion = iv.historySuggestions[iv.historySelectedIndex][len(iv.text):]
}

// AcceptHistorySuggestion applies the current suggestion to the input
func (iv *InputView) AcceptHistorySuggestion() bool {
	if iv.historySuggestion == "" {
		return false
	}

	iv.text += iv.historySuggestion
	iv.cursor = len(iv.text)
	iv.historySuggestion = ""
	iv.historySuggestions = nil
	iv.historySelectedIndex = 0
	return true
}

// TryHandleHistorySuggestionTab handles Tab key for history suggestions
// Returns true if handled (either cycled or accepted), false if no suggestion available
func (iv *InputView) TryHandleHistorySuggestionTab() bool {
	if len(iv.historySuggestions) == 0 {
		return false
	}

	if iv.historySuggestion != "" {
		iv.cycleHistorySuggestion()
		return true
	}

	return false
}

// HasHistorySuggestion returns true if there's a history suggestion available
func (iv *InputView) HasHistorySuggestion() bool {
	return iv.historySuggestion != ""
}

// applyModePrefixStyling applies accent color styling to mode prefixes (! or !!)
func (iv *InputView) applyModePrefixStyling(text string) string {
	isToolsMode := strings.HasPrefix(iv.text, "!!")
	isBashMode := strings.HasPrefix(iv.text, "!") && !isToolsMode

	if !isBashMode && !isToolsMode {
		return text
	}

	accentColor := iv.styleProvider.GetThemeColor("accent")

	if isToolsMode && strings.HasPrefix(text, "!! ") {
		styledPrefix := iv.styleProvider.RenderWithColor("!!", accentColor)
		return styledPrefix + text[2:]
	} else if isBashMode && strings.HasPrefix(text, "! ") {
		styledPrefix := iv.styleProvider.RenderWithColor("!", accentColor)
		return styledPrefix + text[1:]
	}

	return text
}
