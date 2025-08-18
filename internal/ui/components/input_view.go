package components

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/ui/history"
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
	var displayText string
	isBashMode := strings.HasPrefix(iv.text, "!")

	if iv.text == "" {
		displayText = lipgloss.NewStyle().
			Foreground(shared.DimColor.GetLipglossColor()).
			Render(iv.placeholder)
	} else {
		before := iv.text[:iv.cursor]
		after := iv.text[iv.cursor:]

		availableWidth := iv.width - 8
		if availableWidth > 0 {
			wrappedBefore := iv.preserveTrailingSpaces(before, availableWidth)
			wrappedAfter := shared.WrapText(after, availableWidth)
			displayText = fmt.Sprintf("%s│%s", wrappedBefore, wrappedAfter)
		} else {
			displayText = fmt.Sprintf("%s│%s", before, after)
		}
	}

	inputContent := fmt.Sprintf("> %s", displayText)

	borderColor := shared.DimColor.Lipgloss
	if isBashMode {
		borderColor = shared.StatusColor.Lipgloss
	}

	inputStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(borderColor)).
		Padding(0, 1).
		Width(iv.width - 4)

	borderedInput := inputStyle.Render(inputContent)
	components := []string{borderedInput}

	if isBashMode && iv.height >= 2 {
		bashIndicator := lipgloss.NewStyle().
			Foreground(shared.StatusColor.GetLipglossColor()).
			Bold(true).
			Width(iv.width).
			Render("BASH MODE - Command will be executed directly")
		components = append(components, bashIndicator)
	}

	if iv.Autocomplete != nil && iv.Autocomplete.IsVisible() && iv.height >= 3 {
		autocompleteContent := iv.Autocomplete.Render()
		if autocompleteContent != "" {
			components = append(components, autocompleteContent)
		}
	}

	if iv.modelService != nil {
		currentModel := iv.modelService.GetCurrentModel()
		if currentModel != "" && iv.height >= 2 && !isBashMode {
			modelStyle := lipgloss.NewStyle().
				Foreground(shared.DimColor.GetLipglossColor()).
				Width(iv.width)
			modelDisplay := modelStyle.Render(fmt.Sprintf("Model: %s", currentModel))
			components = append(components, modelDisplay)
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, components...)
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

	if keyStr == "alt+enter" {
		iv.text = iv.text[:iv.cursor] + "\n" + iv.text[iv.cursor:]
		iv.cursor++
		return iv, nil
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

	if iv.Autocomplete != nil {
		if handled, completion := iv.Autocomplete.HandleKey(key); handled {
			return iv.handleAutocomplete(completion)
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
	keyStr := key.String()

	// Input text keys - all printable characters
	if len(keyStr) == 1 && keyStr[0] >= ' ' && keyStr[0] <= '~' {
		return true
	}

	// Multi-character input keys
	if keyStr == "space" || keyStr == "tab" {
		return true
	}

	// Control keys that input should handle
	switch keyStr {
	case "enter", "alt+enter":
		return true
	case "backspace", "delete":
		return true
	case "up", "down": // History navigation when autocomplete is not active
		return true
	case "left", "right": // Cursor movement
		return true
	case "ctrl+a", "ctrl+e", "home", "end": // Text navigation
		return true
	case "ctrl+u", "ctrl+k": // Text deletion
		return true
	case "ctrl+w": // Word deletion
		return true
	case "ctrl+z", "ctrl+y": // Undo/redo
		return true
	case "ctrl+l": // Clear
		return true
	}

	// Don't handle scroll keys - these should go to conversation view
	if keyStr == "shift+up" || keyStr == "shift+down" ||
		keyStr == "pgup" || keyStr == "page_up" ||
		keyStr == "pgdn" || keyStr == "page_down" {
		return false
	}

	return false
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
