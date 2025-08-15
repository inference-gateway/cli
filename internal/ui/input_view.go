package ui

import (
	"fmt"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/inference-gateway/cli/internal/domain"
)

// InputView handles user input with history and autocomplete
type InputView struct {
	text         string
	cursor       int
	placeholder  string
	width        int
	height       int
	modelService domain.ModelService
	Autocomplete *AutocompleteImpl // Exported for components.go access
	history      []string
	historyIndex int
	currentInput string
}

func NewInputView(modelService domain.ModelService) *InputView {
	return &InputView{
		text:         "",
		cursor:       0,
		placeholder:  "Type your message... (Press Ctrl+D to send, ? for help)",
		width:        80,
		height:       5,
		modelService: modelService,
		Autocomplete: nil, // Will be set later if needed
		history:      make([]string, 0, 5),
		historyIndex: -1,
		currentInput: "",
	}
}

func (iv *InputView) GetInput() string {
	return iv.text
}

func (iv *InputView) ClearInput() {
	iv.text = ""
	iv.cursor = 0
	iv.historyIndex = -1
	iv.currentInput = ""
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
			Foreground(lipgloss.Color("240")).
			Render(iv.placeholder)
	} else {
		before := iv.text[:iv.cursor]
		after := iv.text[iv.cursor:]

		availableWidth := iv.width - 8
		if availableWidth > 0 {
			wrappedBefore := WrapText(before, availableWidth)
			wrappedAfter := WrapText(after, availableWidth)
			displayText = fmt.Sprintf("%s│%s", wrappedBefore, wrappedAfter)
		} else {
			displayText = fmt.Sprintf("%s│%s", before, after)
		}
	}

	inputContent := fmt.Sprintf("> %s", displayText)

	borderColor := "240" // Gray
	if isBashMode {
		borderColor = "34" // Green
	}

	inputStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(borderColor)).
		Padding(0, 1).
		Width(iv.width - 4)

	borderedInput := inputStyle.Render(inputContent)
	components := []string{borderedInput}

	// Bash mode indicator
	if isBashMode && iv.height >= 2 {
		bashIndicator := lipgloss.NewStyle().
			Foreground(lipgloss.Color("34")).
			Bold(true).
			Width(iv.width).
			Render("BASH MODE - Command will be executed directly")
		components = append(components, bashIndicator)
	}

	// Autocomplete
	if iv.Autocomplete != nil && iv.Autocomplete.IsVisible() && iv.height >= 3 {
		autocompleteContent := iv.Autocomplete.Render()
		if autocompleteContent != "" {
			components = append(components, autocompleteContent)
		}
	}

	// Model display
	if iv.modelService != nil {
		currentModel := iv.modelService.GetCurrentModel()
		if currentModel != "" && iv.height >= 2 && !isBashMode {
			modelStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("240")).
				Width(iv.width)
			modelDisplay := modelStyle.Render(fmt.Sprintf("Model: %s", currentModel))
			components = append(components, modelDisplay)
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, components...)
}

// addToHistory adds a message to the input history, keeping only the last 5
func (iv *InputView) addToHistory(message string) {
	if message == "" {
		return
	}

	if len(iv.history) > 0 && iv.history[len(iv.history)-1] == message {
		return
	}

	iv.history = append(iv.history, message)
	if len(iv.history) > 5 {
		iv.history = iv.history[1:]
	}
}

// navigateHistoryUp moves up in history (to older messages)
func (iv *InputView) navigateHistoryUp() {
	if len(iv.history) == 0 {
		return
	}

	if iv.historyIndex == -1 {
		iv.currentInput = iv.text
		iv.historyIndex = len(iv.history) - 1
	} else if iv.historyIndex > 0 {
		iv.historyIndex--
	} else {
		return
	}

	iv.text = iv.history[iv.historyIndex]
	iv.cursor = len(iv.text)
}

// navigateHistoryDown moves down in history (to newer messages)
func (iv *InputView) navigateHistoryDown() {
	if iv.historyIndex == -1 {
		return
	}

	if iv.historyIndex < len(iv.history)-1 {
		iv.historyIndex++
		iv.text = iv.history[iv.historyIndex]
		iv.cursor = len(iv.text)
	} else {
		iv.historyIndex = -1
		iv.text = iv.currentInput
		iv.cursor = len(iv.text)
	}
}

// Bubble Tea interface
func (iv *InputView) Init() tea.Cmd { return nil }

func (iv *InputView) View() string { return iv.Render() }

func (iv *InputView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if windowMsg, ok := msg.(tea.WindowSizeMsg); ok {
		iv.SetWidth(windowMsg.Width)
	}

	switch msg := msg.(type) {
	case ClearInputMsg:
		iv.ClearInput()
		return iv, nil
	case SetInputMsg:
		iv.SetText(msg.Text)
		iv.SetCursor(len(msg.Text))
		return iv, nil
	}
	return iv, nil
}

func (iv *InputView) HandleKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	keyStr := key.String()

	// History navigation (when autocomplete not visible)
	if iv.Autocomplete == nil || !iv.Autocomplete.IsVisible() {
		switch keyStr {
		case "up":
			iv.navigateHistoryUp()
			if iv.Autocomplete != nil {
				if iv.Autocomplete != nil {
					iv.Autocomplete.Update(iv.text, iv.cursor)
				}
			}
			return iv, nil
		case "down":
			iv.navigateHistoryDown()
			if iv.Autocomplete != nil {
				if iv.Autocomplete != nil {
					iv.Autocomplete.Update(iv.text, iv.cursor)
				}
			}
			return iv, nil
		}
	}

	// Autocomplete handling
	if iv.Autocomplete != nil {
		if handled, completion := iv.Autocomplete.HandleKey(key); handled {
			return iv.handleAutocomplete(completion)
		}
	}

	// Reset history navigation on non-navigation keys
	if keyStr != "up" && keyStr != "down" && keyStr != "left" && keyStr != "right" &&
		keyStr != "ctrl+a" && keyStr != "ctrl+e" && keyStr != "home" && keyStr != "end" {
		iv.historyIndex = -1
		iv.currentInput = ""
	}

	cmd := iv.handleSpecificKeys(key)
	if iv.Autocomplete != nil {
		if iv.Autocomplete != nil {
			iv.Autocomplete.Update(iv.text, iv.cursor)
		}
	}
	return iv, cmd
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

func (iv *InputView) handleSpecificKeys(key tea.KeyMsg) tea.Cmd {
	keyStr := key.String()
	switch keyStr {
	case "left":
		if iv.cursor > 0 {
			iv.cursor--
		}
	case "right":
		if iv.cursor < len(iv.text) {
			iv.cursor++
		}
	case "backspace":
		if key.Alt {
			iv.deleteWordBackward()
		} else {
			if iv.cursor > 0 {
				iv.text = iv.text[:iv.cursor-1] + iv.text[iv.cursor:]
				iv.cursor--
			}
		}
		return nil
	case "ctrl+u":
		iv.deleteToBeginning()
		return nil
	case "ctrl+w":
		iv.deleteWordBackward()
		return nil
	case "ctrl+d":
		return iv.handleSubmit()
	case "ctrl+shift+c":
		iv.handleCopy()
	case "ctrl+v", "alt+v":
		iv.handlePaste()
		return nil
	case "ctrl+x":
		iv.handleCut()
		return nil
	case "ctrl+a":
		iv.cursor = 0
	case "ctrl+e":
		iv.cursor = len(iv.text)
	case "?":
		if len(strings.TrimSpace(iv.text)) == 0 {
			return func() tea.Msg {
				return ToggleHelpBarMsg{}
			}
		}
		return iv.handleCharacterInput(key)
	default:
		return iv.handleCharacterInput(key)
	}
	return nil
}

func (iv *InputView) handleSubmit() tea.Cmd {
	if iv.text != "" {
		input := iv.text
		iv.addToHistory(input)
		iv.ClearInput()
		if iv.Autocomplete != nil {
			iv.Autocomplete.Hide()
		}
		return func() tea.Msg {
			return UserInputMsg{Content: input}
		}
	}
	return nil
}

func (iv *InputView) handleCopy() {
	if iv.text != "" {
		_ = clipboard.WriteAll(iv.text)
	}
}

func (iv *InputView) handlePaste() {
	clipboardText, err := clipboard.ReadAll()
	if err != nil {
		return
	}

	if clipboardText == "" {
		return
	}

	cleanText := strings.ReplaceAll(clipboardText, "\n", " ")
	cleanText = strings.ReplaceAll(cleanText, "\r", " ")
	cleanText = strings.ReplaceAll(cleanText, "\t", " ")

	if cleanText != "" {
		iv.text = iv.text[:iv.cursor] + cleanText + iv.text[iv.cursor:]
		iv.cursor += len(cleanText)
	}
}

func (iv *InputView) handleCut() {
	if iv.text != "" {
		_ = clipboard.WriteAll(iv.text)
		iv.text = ""
		iv.cursor = 0
	}
}

func (iv *InputView) handleCharacterInput(key tea.KeyMsg) tea.Cmd {
	keyStr := key.String()

	if len(keyStr) > 1 && key.Type == tea.KeyRunes {
		cleanText := strings.ReplaceAll(keyStr, "\n", " ")
		cleanText = strings.ReplaceAll(cleanText, "\r", " ")
		cleanText = strings.ReplaceAll(cleanText, "\t", " ")

		if strings.HasPrefix(cleanText, "[") && strings.HasSuffix(cleanText, "]") {
			cleanText = cleanText[1 : len(cleanText)-1]
		}

		if cleanText != "" {
			iv.text = iv.text[:iv.cursor] + cleanText + iv.text[iv.cursor:]
			iv.cursor += len(cleanText)

			return func() tea.Msg {
				return ScrollRequestMsg{
					ComponentID: "conversation",
					Direction:   ScrollToBottom,
					Amount:      0,
				}
			}
		}
		return nil
	}

	if len(keyStr) == 1 && keyStr[0] >= 32 {
		char := keyStr
		iv.text = iv.text[:iv.cursor] + char + iv.text[iv.cursor:]
		iv.cursor++

		if char == "@" {
			return tea.Batch(
				func() tea.Msg {
					return ScrollRequestMsg{
						ComponentID: "conversation",
						Direction:   ScrollToBottom,
						Amount:      0,
					}
				},
				func() tea.Msg {
					return FileSelectionRequestMsg{}
				},
			)
		}

		return tea.Batch(
			func() tea.Msg {
				return ScrollRequestMsg{
					ComponentID: "conversation",
					Direction:   ScrollToBottom,
					Amount:      0,
				}
			},
			func() tea.Msg {
				return HideHelpBarMsg{}
			},
		)
	}
	return nil
}

func (iv *InputView) CanHandle(key tea.KeyMsg) bool {
	return true
}

// deleteWordBackward deletes the word before the cursor
func (iv *InputView) deleteWordBackward() {
	if iv.cursor > 0 {
		start := iv.cursor

		for start > 0 && (iv.text[start-1] == ' ' || iv.text[start-1] == '\t') {
			start--
		}

		for start > 0 && iv.text[start-1] != ' ' && iv.text[start-1] != '\t' {
			start--
		}

		iv.text = iv.text[:start] + iv.text[iv.cursor:]
		iv.cursor = start
	}
}

// deleteToBeginning deletes from the cursor to the beginning of the line
func (iv *InputView) deleteToBeginning() {
	if iv.cursor > 0 {
		iv.text = iv.text[iv.cursor:]
		iv.cursor = 0
	}
}
