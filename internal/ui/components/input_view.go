package components

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	models "github.com/inference-gateway/cli/internal/models"
	history "github.com/inference-gateway/cli/internal/ui/history"
	keys "github.com/inference-gateway/cli/internal/ui/keys"
	shared "github.com/inference-gateway/cli/internal/ui/shared"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

// InputView handles user input with history and autocomplete
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
	Autocomplete         shared.AutocompleteInterface
	historyManager       *history.HistoryManager
	isTextSelectionMode  bool
	themeService         domain.ThemeService
	styleProvider        *styles.Provider
	imageAttachments     []domain.ImageAttachment
	historySuggestion    string
	historySuggestions   []string
	historySelectedIndex int
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
		text:                "",
		cursor:              0,
		placeholder:         "Type your message... (Press Enter to send, Alt+Enter or Ctrl+J for newline, ? for help)",
		width:               80,
		height:              5,
		modelService:        modelService,
		Autocomplete:        nil,
		historyManager:      historyManager,
		isTextSelectionMode: false,
		themeService:        nil,
		imageAttachments:    []domain.ImageAttachment{},
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
	if iv.Autocomplete != nil {
		iv.Autocomplete.Hide()
	}
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
	if iv.Autocomplete != nil {
		iv.Autocomplete.SetWidth(width)
	}
}

func (iv *InputView) SetHeight(height int) {
	iv.height = height
}

func (iv *InputView) Render() string {
	iv.updateHistorySuggestions()

	isToolsMode := strings.HasPrefix(iv.text, "!!")
	isBashMode := strings.HasPrefix(iv.text, "!") && !isToolsMode
	displayText := iv.renderDisplayText()

	inputContent := fmt.Sprintf("> %s", displayText)

	focused := isBashMode || isToolsMode
	borderedInput := iv.styleProvider.RenderInputField(inputContent, iv.width-4, focused)

	components := []string{borderedInput}

	components = iv.addModeIndicatorBelowInput(components, isBashMode, isToolsMode)
	components = iv.addAutocomplete(components)
	components = iv.addModelDisplayWithMode(components, isBashMode, isToolsMode)

	return iv.styleProvider.JoinVertical(components...)
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
	return iv.styleProvider.RenderInputPlaceholder(iv.placeholder)
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
	wrappedAfter := shared.WrapText(after, availableWidth)
	return iv.buildTextWithCursor(wrappedBefore, wrappedAfter)
}

func (iv *InputView) renderUnwrappedText(before, after string) string {
	return iv.buildTextWithCursor(before, after)
}

func (iv *InputView) buildTextWithCursor(before, after string) string {
	if len(after) == 0 {
		cursorChar := iv.createCursorChar(" ")
		result := fmt.Sprintf("%s%s", before, cursorChar)

		if iv.cursor == len(iv.text) && iv.historySuggestion != "" {
			ghostText := iv.styleProvider.RenderDimText(iv.historySuggestion)
			result = fmt.Sprintf("%s%s", before, ghostText)
		}

		return result
	}

	cursorChar := iv.createCursorChar(string(after[0]))
	restAfter := ""
	if len(after) > 1 {
		restAfter = after[1:]
	}
	return fmt.Sprintf("%s%s%s", before, cursorChar, restAfter)
}

func (iv *InputView) createCursorChar(char string) string {
	return iv.styleProvider.RenderTextSelectionCursor(char)
}

// getAgentModeIndicator returns a compact mode indicator for display on the right side
func (iv *InputView) getAgentModeIndicator() string {
	if iv.stateManager == nil {
		return ""
	}

	agentMode := iv.stateManager.GetAgentMode()
	if agentMode == domain.AgentModeStandard {
		return ""
	}

	var modeText string
	switch agentMode {
	case domain.AgentModePlan:
		modeText = "▶ PLAN"
	case domain.AgentModeAutoAccept:
		modeText = "▸ AUTO"
	}

	return iv.styleProvider.RenderStyledText(
		modeText,
		styles.StyleOptions{
			Foreground: iv.styleProvider.GetThemeColor("accent"),
			Bold:       true,
		},
	)
}

// addModeIndicatorBelowInput adds mode indicators below the input field (for bash/tools modes)
func (iv *InputView) addModeIndicatorBelowInput(components []string, isBashMode bool, isToolsMode bool) []string {
	if iv.height >= 2 {
		if iv.isTextSelectionMode {
			indicator := iv.styleProvider.RenderStyledText(
				"TEXT SELECTION MODE - Use vim keys to navigate and select text (Escape to exit)",
				styles.StyleOptions{
					Foreground: iv.styleProvider.GetThemeColor("accent"),
					Bold:       true,
					Width:      iv.width,
				},
			)
			components = append(components, indicator)
		} else if isBashMode {
			indicator := iv.styleProvider.RenderStyledText(
				"  BASH MODE - Command will be executed directly",
				styles.StyleOptions{
					Foreground: iv.styleProvider.GetThemeColor("status"),
					Bold:       true,
					Width:      iv.width,
				},
			)
			components = append(components, indicator)
		} else if isToolsMode {
			indicator := iv.styleProvider.RenderStyledText(
				"  TOOLS MODE - !!ToolName(arg=\"value\") - Tab for autocomplete",
				styles.StyleOptions{
					Foreground: iv.styleProvider.GetThemeColor("accent"),
					Bold:       true,
					Width:      iv.width,
				},
			)
			components = append(components, indicator)
		}
	}
	return components
}

func (iv *InputView) addAutocomplete(components []string) []string {
	if iv.Autocomplete != nil && iv.Autocomplete.IsVisible() && iv.height >= 3 {
		autocompleteContent := iv.Autocomplete.Render()
		if autocompleteContent != "" {
			components = append(components, autocompleteContent)
		}
	}
	return components
}

func (iv *InputView) addModelDisplayWithMode(components []string, isBashMode bool, isToolsMode bool) []string {
	if !iv.shouldShowModelDisplay(isBashMode, isToolsMode) {
		return components
	}

	currentModel := iv.modelService.GetCurrentModel()
	displayText := iv.buildModelDisplayText(currentModel)
	modeIndicator := iv.getAgentModeIndicator()

	if modeIndicator != "" {
		return iv.addModelWithModeIndicator(components, displayText, modeIndicator)
	}

	return iv.addModelOnly(components, displayText)
}

func (iv *InputView) shouldShowModelDisplay(isBashMode bool, isToolsMode bool) bool {
	if iv.modelService == nil {
		return false
	}

	currentModel := iv.modelService.GetCurrentModel()
	return currentModel != "" && iv.height >= 2 && !isBashMode && !isToolsMode
}

func (iv *InputView) buildModelDisplayText(currentModel string) string {
	parts := []string{fmt.Sprintf("Model: %s", currentModel)}

	if iv.themeService != nil {
		currentTheme := iv.themeService.GetCurrentThemeName()
		parts = append(parts, fmt.Sprintf("Theme: %s", currentTheme))
	}

	if iv.configService != nil {
		maxTokens := iv.configService.Agent.MaxTokens
		if maxTokens > 0 {
			parts = append(parts, fmt.Sprintf("Max Output: %d", maxTokens))
		}
	}

	if iv.stateManager != nil {
		if readiness := iv.stateManager.GetAgentReadiness(); readiness != nil && readiness.TotalAgents > 0 {
			parts = append(parts, fmt.Sprintf("Agents: %d/%d", readiness.ReadyAgents, readiness.TotalAgents))
		}
	}

	if contextIndicator := iv.getContextUsageIndicator(currentModel); contextIndicator != "" {
		parts = append(parts, contextIndicator)
	}

	return "  " + strings.Join(parts, " • ")
}

// getContextUsageIndicator returns a context usage indicator string
func (iv *InputView) getContextUsageIndicator(model string) string {
	if iv.conversationRepo == nil {
		return ""
	}

	stats := iv.conversationRepo.GetSessionTokens()
	currentContextSize := stats.LastInputTokens
	if currentContextSize == 0 {
		return ""
	}

	contextWindow := iv.estimateContextWindow(model)
	if contextWindow == 0 {
		return ""
	}

	usagePercent := float64(currentContextSize) * 100 / float64(contextWindow)

	displayPercent := usagePercent
	if displayPercent > 100 {
		displayPercent = 100
	}

	if usagePercent >= 90 {
		return fmt.Sprintf("Context: %.0f%% FULL", displayPercent)
	} else if usagePercent >= 75 {
		return fmt.Sprintf("Context: %.0f%% HIGH", displayPercent)
	} else if usagePercent >= 50 {
		return fmt.Sprintf("Context: %.0f%%", displayPercent)
	}

	return ""
}

// estimateContextWindow returns an estimated context window size based on model name
func (iv *InputView) estimateContextWindow(model string) int {
	return models.EstimateContextWindow(model)
}

func (iv *InputView) addModelWithModeIndicator(components []string, displayText string, modeIndicator string) []string {
	modelText := iv.styleProvider.RenderStyledText(displayText, styles.StyleOptions{
		Foreground: iv.styleProvider.GetThemeColor("dim"),
	})

	combinedLine := iv.buildCombinedLine(modelText, modeIndicator)
	return append(components, combinedLine)
}

func (iv *InputView) buildCombinedLine(modelText string, modeIndicator string) string {
	inputRightEdge := iv.width - 4
	modelWidth := iv.styleProvider.GetWidth(modelText)
	modeWidth := iv.styleProvider.GetWidth(modeIndicator)
	availableWidth := inputRightEdge - modelWidth - modeWidth

	if availableWidth > 0 {
		return modelText + strings.Repeat(" ", availableWidth) + modeIndicator
	}
	return modelText + " " + modeIndicator
}

func (iv *InputView) addModelOnly(components []string, displayText string) []string {
	modelDisplay := iv.styleProvider.RenderStyledText(displayText, styles.StyleOptions{
		Foreground: iv.styleProvider.GetThemeColor("dim"),
		Width:      iv.width,
	})
	return append(components, modelDisplay)
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

// Bubble Tea interface
func (iv *InputView) Init() tea.Cmd { return nil }

func (iv *InputView) View() string { return iv.Render() }

func (iv *InputView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if windowMsg, ok := msg.(tea.WindowSizeMsg); ok {
		iv.SetWidth(windowMsg.Width)
	}

	switch msg := msg.(type) {
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

	if keyStr == "tab" && (iv.Autocomplete == nil || !iv.Autocomplete.IsVisible()) {
		if iv.HasHistorySuggestion() && iv.cursor == len(iv.text) {
			iv.cycleHistorySuggestion()
			return iv, nil
		}
	}

	if iv.Autocomplete != nil && iv.Autocomplete.IsVisible() {
		if handled, completion := iv.Autocomplete.HandleKey(key); handled {
			return iv.handleAutocomplete(completion)
		}
	}

	if iv.Autocomplete == nil || !iv.Autocomplete.IsVisible() {
		switch keyStr {
		case "up":
			iv.navigateHistoryUp()
			if iv.Autocomplete != nil {
				iv.Autocomplete.Update(iv.text, iv.cursor)
			}
			return iv, nil
		case "down":
			iv.navigateHistoryDown()
			if iv.Autocomplete != nil {
				iv.Autocomplete.Update(iv.text, iv.cursor)
			}
			return iv, nil
		}
	}

	if keyStr != "up" && keyStr != "down" && keyStr != "left" && keyStr != "right" &&
		keyStr != "ctrl+a" && keyStr != "ctrl+e" && keyStr != "home" && keyStr != "end" {
		iv.historyManager.ResetNavigation()
	}

	if iv.Autocomplete != nil {
		iv.Autocomplete.Update(iv.text, iv.cursor)
	}
	return iv, nil
}

func (iv *InputView) handleAutocomplete(completion string) (tea.Model, tea.Cmd) {
	if completion != "" {
		iv.text = completion
		iv.setCursorPosition(completion)
		if iv.Autocomplete != nil {
			iv.Autocomplete.Hide()
		}
		return iv, nil
	}
	return iv, nil
}

// setCursorPosition sets the appropriate cursor position based on completion content
func (iv *InputView) setCursorPosition(completion string) {
	if strings.Contains(completion, `=""`) {
		if idx := strings.Index(completion, `=""`); idx != -1 {
			iv.cursor = idx + 2
		} else {
			iv.cursor = len(completion)
		}
	} else {
		iv.cursor = len(completion)
	}
}

func (iv *InputView) CanHandle(key tea.KeyMsg) bool {
	return keys.CanInputHandle(key)
}

func (iv *InputView) preserveTrailingSpaces(text string, availableWidth int) string {
	wrappedText := shared.WrapText(text, availableWidth)

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

// IsAutocompleteVisible returns whether autocomplete is currently visible
func (iv *InputView) IsAutocompleteVisible() bool {
	return iv.Autocomplete != nil && iv.Autocomplete.IsVisible()
}

// TryHandleAutocomplete attempts to handle autocomplete key input
func (iv *InputView) TryHandleAutocomplete(key tea.KeyMsg) (handled bool, completion string) {
	if iv.Autocomplete != nil && iv.Autocomplete.IsVisible() {
		return iv.Autocomplete.HandleKey(key)
	}
	return false, ""
}

func (iv *InputView) SetTextSelectionMode(enabled bool) {
	iv.isTextSelectionMode = enabled
}

func (iv *InputView) IsTextSelectionMode() bool {
	return iv.isTextSelectionMode
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

	if iv.Autocomplete != nil && iv.Autocomplete.IsVisible() {
		iv.historySuggestion = ""
		iv.historySuggestions = nil
		iv.historySelectedIndex = 0
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
