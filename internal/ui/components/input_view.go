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
)

// InputView handles user input with history and autocomplete
type InputView struct {
	text           string
	cursor         int
	placeholder    string
	width          int
	height         int
	modelService   domain.ModelService
	Autocomplete   shared.AutocompleteInterface
	historyManager *history.HistoryManager
}

func NewInputView(modelService domain.ModelService) *InputView {
	historyManager, err := history.NewHistoryManager(5)
	if err != nil {
		historyManager = history.NewMemoryOnlyHistoryManager(5)
	}

	return &InputView{
		text:           "",
		cursor:         0,
		placeholder:    "Type your message... (Press Enter to send, Alt+Enter for newline)",
		width:          80,
		height:         5,
		modelService:   modelService,
		Autocomplete:   nil,
		historyManager: historyManager,
	}
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
	isBashMode := strings.HasPrefix(iv.text, "!")
	displayText := iv.renderDisplayText()

	inputContent := fmt.Sprintf("> %s", displayText)
	borderColor := iv.getBorderColor(isBashMode)

	inputStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(borderColor)).
		Padding(0, 1).
		Width(iv.width - 4)

	borderedInput := inputStyle.Render(inputContent)
	components := []string{borderedInput}

	components = iv.addBashIndicator(components, isBashMode)
	components = iv.addAutocomplete(components)
	components = iv.addModelDisplay(components, isBashMode)

	return lipgloss.JoinVertical(lipgloss.Left, components...)
}

func (iv *InputView) renderDisplayText() string {
	if iv.text == "" {
		return iv.renderPlaceholder()
	}
	return iv.renderTextWithCursor()
}

func (iv *InputView) renderPlaceholder() string {
	return lipgloss.NewStyle().
		Foreground(shared.DimColor.GetLipglossColor()).
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
		Background(lipgloss.Color("#FFFFFF")).
		Foreground(lipgloss.Color("#000000")).
		Render(char)
}

func (iv *InputView) getBorderColor(isBashMode bool) string {
	if isBashMode {
		return shared.StatusColor.Lipgloss
	}
	return shared.DimColor.Lipgloss
}

func (iv *InputView) addBashIndicator(components []string, isBashMode bool) []string {
	if isBashMode && iv.height >= 2 {
		bashIndicator := lipgloss.NewStyle().
			Foreground(shared.StatusColor.GetLipglossColor()).
			Bold(true).
			Width(iv.width).
			Render("BASH MODE - Command will be executed directly")
		components = append(components, bashIndicator)
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

func (iv *InputView) addModelDisplay(components []string, isBashMode bool) []string {
	if iv.modelService != nil {
		currentModel := iv.modelService.GetCurrentModel()
		if currentModel != "" && iv.height >= 2 && !isBashMode {
			modelStyle := lipgloss.NewStyle().
				Foreground(shared.DimColor.GetLipglossColor()).
				Width(iv.width)
			modelDisplay := modelStyle.Render(fmt.Sprintf("  Model: %s", currentModel))
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

// Bubble Tea interface
func (iv *InputView) Init() tea.Cmd { return nil }

func (iv *InputView) View() string { return iv.Render() }

func (iv *InputView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if windowMsg, ok := msg.(tea.WindowSizeMsg); ok {
		iv.SetWidth(windowMsg.Width)
	}

	switch msg := msg.(type) {
	case shared.ClearInputMsg:
		iv.ClearInput()
		return iv, nil
	case shared.SetInputMsg:
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

	if keyStr == "alt+enter" {
		iv.text = iv.text[:iv.cursor] + "\n" + iv.text[iv.cursor:]
		iv.cursor++
		return iv, nil
	}

	if iv.Autocomplete != nil && iv.Autocomplete.IsVisible() {
		if handled, completion := iv.Autocomplete.HandleKey(key); handled {
			return iv.handleAutocomplete(completion)
		}

		if keyStr == "up" || keyStr == "down" {
			return iv, nil
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
		iv.cursor = len(completion)
		if iv.Autocomplete != nil {
			iv.Autocomplete.Hide()
		}
		return iv, nil
	}
	if iv.Autocomplete != nil {
		iv.Autocomplete.Update(iv.text, iv.cursor)
	}
	return iv, nil
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

// handlePaste handles clipboard paste operations
func (iv *InputView) handlePaste() (tea.Model, tea.Cmd) {
	clipboardText, err := clipboard.ReadAll()
	if err != nil {
		return iv, nil
	}

	if clipboardText == "" {
		return iv, nil
	}

	cleanText := strings.ReplaceAll(clipboardText, "\n", " ")
	cleanText = strings.ReplaceAll(cleanText, "\r", " ")
	cleanText = strings.ReplaceAll(cleanText, "\t", " ")

	if cleanText != "" {
		newText := iv.text[:iv.cursor] + cleanText + iv.text[iv.cursor:]
		newCursor := iv.cursor + len(cleanText)

		iv.text = newText
		iv.cursor = newCursor
	}

	return iv, nil
}
