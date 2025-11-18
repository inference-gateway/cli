package components

import (
	"fmt"
	"strings"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/ui/history"
	"github.com/inference-gateway/cli/internal/ui/keys"
	"github.com/inference-gateway/cli/internal/ui/shared"
	"github.com/inference-gateway/cli/internal/ui/styles/colors"
)

// InputView handles user input with history and autocomplete
type InputView struct {
	text                string
	cursor              int
	placeholder         string
	width               int
	height              int
	modelService        domain.ModelService
	Autocomplete        shared.AutocompleteInterface
	historyManager      *history.HistoryManager
	isTextSelectionMode bool
	themeService        domain.ThemeService
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
	}
}

// SetThemeService sets the theme service for this input view
func (iv *InputView) SetThemeService(themeService domain.ThemeService) {
	iv.themeService = themeService
}

func (iv *InputView) GetInput() string {
	return iv.text
}

func (iv *InputView) ClearInput() {
	iv.text = ""
	iv.cursor = 0
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
	isToolsMode := strings.HasPrefix(iv.text, "!!")
	isBashMode := strings.HasPrefix(iv.text, "!") && !isToolsMode
	displayText := iv.renderDisplayText()

	inputContent := fmt.Sprintf("> %s", displayText)
	borderColor := iv.getBorderColor(isBashMode, isToolsMode)

	inputStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(borderColor)).
		Padding(0, 1).
		Width(iv.width - 4)

	borderedInput := inputStyle.Render(inputContent)
	components := []string{borderedInput}

	components = iv.addModeIndicator(components, isBashMode, isToolsMode)
	components = iv.addAutocomplete(components)
	components = iv.addModelDisplay(components, isBashMode, isToolsMode)

	return lipgloss.JoinVertical(lipgloss.Left, components...)
}

func (iv *InputView) renderDisplayText() string {
	if iv.text == "" {
		return iv.renderPlaceholder()
	}
	return iv.renderTextWithCursor()
}

func (iv *InputView) renderPlaceholder() string {
	dimColor := iv.getDimColor()
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(dimColor)).
		Render(iv.placeholder)
}

func (iv *InputView) renderTextWithCursor() string {
	before := iv.text[:iv.cursor]
	after := iv.text[iv.cursor:]
	availableWidth := iv.width - 8

	if availableWidth > 0 {
		return iv.renderWrappedText(before, after, availableWidth)
	}
	return iv.renderUnwrappedText(before, after)
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
		return fmt.Sprintf("%s%s", before, cursorChar)
	}

	cursorChar := iv.createCursorChar(string(after[0]))
	restAfter := ""
	if len(after) > 1 {
		restAfter = after[1:]
	}
	return fmt.Sprintf("%s%s%s", before, cursorChar, restAfter)
}

func (iv *InputView) createCursorChar(char string) string {
	return lipgloss.NewStyle().
		Background(lipgloss.Color(colors.LipglossWhiteBg)).
		Foreground(lipgloss.Color(colors.LipglossBlack)).
		Render(char)
}

func (iv *InputView) getBorderColor(isBashMode bool, isToolsMode bool) string {
	if isBashMode {
		return iv.getSuccessColor()
	}
	if isToolsMode {
		return iv.getAccentColor()
	}
	return iv.getDimColor()
}

func (iv *InputView) addModeIndicator(components []string, isBashMode bool, isToolsMode bool) []string {
	if iv.height >= 2 {
		if iv.isTextSelectionMode {
			accentColor := iv.getAccentColor()
			textSelectionIndicator := lipgloss.NewStyle().
				Foreground(lipgloss.Color(accentColor)).
				Bold(true).
				Width(iv.width).
				Render("TEXT SELECTION MODE - Use vim keys to navigate and select text (Escape to exit)")
			components = append(components, textSelectionIndicator)
		} else if isBashMode {
			statusColor := iv.getStatusColor()
			bashIndicator := lipgloss.NewStyle().
				Foreground(lipgloss.Color(statusColor)).
				Bold(true).
				Width(iv.width).
				Render("BASH MODE - Command will be executed directly")
			components = append(components, bashIndicator)
		} else if isToolsMode {
			accentColor := iv.getAccentColor()
			toolsIndicator := lipgloss.NewStyle().
				Foreground(lipgloss.Color(accentColor)).
				Bold(true).
				Width(iv.width).
				Render("TOOLS MODE - !!ToolName(arg=\"value\") - Tab for autocomplete")
			components = append(components, toolsIndicator)
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

func (iv *InputView) addModelDisplay(components []string, isBashMode bool, isToolsMode bool) []string {
	if iv.modelService != nil {
		currentModel := iv.modelService.GetCurrentModel()
		if currentModel != "" && iv.height >= 2 && !isBashMode && !isToolsMode {
			dimColor := iv.getDimColor()
			modelStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color(dimColor)).
				Width(iv.width)

			displayText := fmt.Sprintf("  Model: %s", currentModel)

			if iv.themeService != nil {
				currentTheme := iv.themeService.GetCurrentThemeName()
				displayText = fmt.Sprintf("  Model: %s • Theme: %s", currentModel, currentTheme)
			}

			modelDisplay := modelStyle.Render(displayText)
			components = append(components, modelDisplay)
		}
	}
	return components
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

	if keyStr == "ctrl+v" {
		return iv.handlePaste()
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

// handlePaste handles clipboard paste operations
func (iv *InputView) handlePaste() (tea.Model, tea.Cmd) {
	clipboardText, err := clipboard.ReadAll()
	if err != nil {
		return iv, nil
	}

	if clipboardText == "" {
		return iv, nil
	}

	cleanText := strings.ReplaceAll(clipboardText, "\r\n", "\n")
	cleanText = strings.ReplaceAll(cleanText, "\r", "\n")

	if cleanText != "" {
		newText := iv.text[:iv.cursor] + cleanText + iv.text[iv.cursor:]
		newCursor := iv.cursor + len(cleanText)

		iv.text = newText
		iv.cursor = newCursor
	}

	return iv, nil
}

func (iv *InputView) SetTextSelectionMode(enabled bool) {
	iv.isTextSelectionMode = enabled
}

func (iv *InputView) IsTextSelectionMode() bool {
	return iv.isTextSelectionMode
}

// Helper methods to get theme colors with fallbacks
func (iv *InputView) getDimColor() string {
	if iv.themeService != nil {
		return iv.themeService.GetCurrentTheme().GetDimColor()
	}
	return colors.DimColor.Lipgloss
}

func (iv *InputView) getAccentColor() string {
	if iv.themeService != nil {
		return iv.themeService.GetCurrentTheme().GetAccentColor()
	}
	return colors.AccentColor.Lipgloss
}

func (iv *InputView) getStatusColor() string {
	if iv.themeService != nil {
		return iv.themeService.GetCurrentTheme().GetStatusColor()
	}
	return colors.StatusColor.Lipgloss
}

func (iv *InputView) getSuccessColor() string {
	if iv.themeService != nil {
		return iv.themeService.GetCurrentTheme().GetSuccessColor()
	}
	return colors.SuccessColor.Lipgloss
}
