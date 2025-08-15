package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/inference-gateway/cli/internal/commands"
	"github.com/inference-gateway/cli/internal/domain"
)

// DefaultTheme implements Theme interface with default colors
type DefaultTheme struct{}

func NewDefaultTheme() *DefaultTheme {
	return &DefaultTheme{}
}

func (t *DefaultTheme) GetUserColor() string      { return "\033[36m" } // Cyan
func (t *DefaultTheme) GetAssistantColor() string { return "\033[32m" } // Green
func (t *DefaultTheme) GetErrorColor() string     { return "\033[31m" } // Red
func (t *DefaultTheme) GetStatusColor() string    { return "\033[34m" } // Blue
func (t *DefaultTheme) GetAccentColor() string    { return "\033[35m" } // Magenta
func (t *DefaultTheme) GetDimColor() string       { return "\033[90m" } // Gray
func (t *DefaultTheme) GetBorderColor() string    { return "\033[37m" } // White

// DefaultLayout implements Layout interface with default spacing
type DefaultLayout struct{}

func NewDefaultLayout() *DefaultLayout {
	return &DefaultLayout{}
}

func (l *DefaultLayout) CalculateConversationHeight(totalHeight int) int {
	inputHeight := l.CalculateInputHeight(totalHeight)
	statusHeight := l.CalculateStatusHeight(totalHeight)

	extraLines := 5
	if totalHeight < 12 {
		extraLines = 3
	}

	conversationHeight := totalHeight - inputHeight - statusHeight - extraLines

	minConversationHeight := 3
	if conversationHeight < minConversationHeight {
		conversationHeight = minConversationHeight
	}

	return conversationHeight
}

func (l *DefaultLayout) CalculateInputHeight(totalHeight int) int {
	if totalHeight < 8 {
		return 2
	}
	if totalHeight < 12 {
		return 3
	}
	return 4
}

func (l *DefaultLayout) CalculateStatusHeight(totalHeight int) int {
	if totalHeight < 8 {
		return 0
	}
	if totalHeight < 12 {
		return 1
	}
	return 2
}

func (l *DefaultLayout) GetMargins() (top, right, bottom, left int) {
	return 1, 2, 1, 2
}

// ComponentFactory creates UI components with injected dependencies
type ComponentFactory struct {
	theme           Theme
	layout          Layout
	modelService    domain.ModelService
	commandRegistry *commands.Registry
}

func NewComponentFactory(theme Theme, layout Layout, modelService domain.ModelService) *ComponentFactory {
	return &ComponentFactory{
		theme:           theme,
		layout:          layout,
		modelService:    modelService,
		commandRegistry: nil,
	}
}

// SetCommandRegistry updates the command registry for the factory
func (f *ComponentFactory) SetCommandRegistry(registry *commands.Registry) {
	f.commandRegistry = registry
}

func (f *ComponentFactory) CreateConversationView() ConversationRenderer {
	vp := viewport.New(80, 20)
	vp.SetContent("")
	return &ConversationViewImpl{
		theme:              f.theme,
		conversation:       []domain.ConversationEntry{},
		viewport:           vp,
		width:              80,
		height:             20,
		expandedToolResult: -1,
		isToolExpanded:     false,
	}
}

func (f *ComponentFactory) CreateInputView() InputComponent {
	return &InputViewImpl{
		text:         "",
		cursor:       0,
		placeholder:  "Type your message... (Press Ctrl+D to send)",
		width:        80,
		height:       5,  // Initialize with default height
		theme:        f.theme,
		modelService: f.modelService,
		autocomplete: NewAutocomplete(f.theme, f.commandRegistry),
		history:      make([]string, 0, 5),
		historyIndex: -1,
		currentInput: "",
	}
}

func (f *ComponentFactory) CreateStatusView() StatusComponent {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	return &StatusViewImpl{
		message:   "",
		isError:   false,
		isSpinner: false,
		spinner:   s,
		theme:     f.theme,
	}
}

// ConversationViewImpl implements ConversationRenderer
type ConversationViewImpl struct {
	theme              Theme
	conversation       []domain.ConversationEntry
	viewport           viewport.Model
	width              int
	height             int
	expandedToolResult int
	isToolExpanded     bool
}

func (cv *ConversationViewImpl) SetConversation(conversation []domain.ConversationEntry) {
	cv.conversation = conversation
	cv.updateViewportContent()
}

func (cv *ConversationViewImpl) SetScrollOffset(offset int) {
	// Viewport handles its own scrolling
}

func (cv *ConversationViewImpl) GetScrollOffset() int {
	return cv.viewport.YOffset
}

func (cv *ConversationViewImpl) CanScrollUp() bool {
	return !cv.viewport.AtTop()
}

func (cv *ConversationViewImpl) CanScrollDown() bool {
	return !cv.viewport.AtBottom()
}

func (cv *ConversationViewImpl) ToggleToolResultExpansion(index int) {
	if index >= 0 && index < len(cv.conversation) {
		if cv.expandedToolResult == index {
			cv.isToolExpanded = !cv.isToolExpanded
		} else {
			cv.expandedToolResult = index
			cv.isToolExpanded = true
		}
	}
}

func (cv *ConversationViewImpl) IsToolResultExpanded(index int) bool {
	if index >= 0 && index < len(cv.conversation) {
		return cv.expandedToolResult == index && cv.isToolExpanded
	}
	return false
}

func (cv *ConversationViewImpl) SetWidth(width int) {
	cv.width = width
	cv.viewport.Width = width
}

func (cv *ConversationViewImpl) SetHeight(height int) {
	cv.height = height
	cv.viewport.Height = height
}

func (cv *ConversationViewImpl) Render() string {
	if len(cv.conversation) == 0 {
		cv.viewport.SetContent(cv.renderWelcome())
	} else {
		cv.updateViewportContent()
	}
	return cv.viewport.View()
}

func (cv *ConversationViewImpl) updateViewportContent() {
	var b strings.Builder

	for i, entry := range cv.conversation {
		b.WriteString(cv.renderEntryWithIndex(entry, i))
		b.WriteString("\n")
	}

	wasAtBottom := cv.viewport.AtBottom()

	cv.viewport.SetContent(b.String())

	if wasAtBottom {
		cv.viewport.GotoBottom()
	}
}

func (cv *ConversationViewImpl) renderWelcome() string {
	return fmt.Sprintf("%s🤖 Chat session ready! Type your message below.%s\n",
		cv.theme.GetStatusColor(), "\033[0m")
}


func (cv *ConversationViewImpl) renderEntryWithIndex(entry domain.ConversationEntry, index int) string {
	var color, role string

	switch string(entry.Message.Role) {
	case "user":
		color = cv.theme.GetUserColor()
		role = "👤 You"
	case "assistant":
		color = cv.theme.GetAssistantColor()
		if entry.Model != "" {
			role = fmt.Sprintf("🤖 %s", entry.Model)
		} else {
			role = "🤖 Assistant"
		}
	case "system":
		color = cv.theme.GetDimColor()
		role = "⚙️ System"
	case "tool":
		color = cv.theme.GetAccentColor()
		role = "🔧 Tool"
		return cv.renderToolEntry(entry, index, color, role)
	default:
		color = cv.theme.GetDimColor()
		role = string(entry.Message.Role)
	}

	content := entry.Message.Content
	resetColor := "\033[0m"
	message := fmt.Sprintf("%s%s:%s %s", color, role, resetColor, content)

	return message + "\n"
}

func (cv *ConversationViewImpl) renderToolEntry(entry domain.ConversationEntry, index int, color, role string) string {
	resetColor := "\033[0m"

	var isExpanded bool
	if index >= 0 {
		isExpanded = cv.IsToolResultExpanded(index)
	}

	content := cv.formatEntryContent(entry, isExpanded)

	message := fmt.Sprintf("%s%s:%s %s", color, role, resetColor, content)
	return message + "\n"
}

func (cv *ConversationViewImpl) formatEntryContent(entry domain.ConversationEntry, isExpanded bool) string {
	if isExpanded {
		return cv.formatExpandedContent(entry)
	}
	return cv.formatCompactContent(entry)
}

func (cv *ConversationViewImpl) formatExpandedContent(entry domain.ConversationEntry) string {
	if entry.ToolExecution != nil {
		return FormatToolResultExpanded(entry.ToolExecution) + "\n\n💡 Press Ctrl+R to collapse"
	}
	return entry.Message.Content + "\n\n💡 Press Ctrl+R to collapse"
}

func (cv *ConversationViewImpl) formatCompactContent(entry domain.ConversationEntry) string {
	if entry.ToolExecution != nil {
		return FormatToolResultForUI(entry.ToolExecution) + "\n💡 Press Ctrl+R to expand details"
	}
	return cv.formatToolContentCompact(entry.Message.Content) + "\n💡 Press Ctrl+R to expand details"
}

func (cv *ConversationViewImpl) formatToolContentCompact(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) <= 3 {
		return content
	}

	return strings.Join(lines[:3], "\n") + "\n... (truncated)"
}

func (cv *ConversationViewImpl) GetID() string { return "conversation" }

func (cv *ConversationViewImpl) Init() tea.Cmd { return nil }

func (cv *ConversationViewImpl) View() string { return cv.Render() }

func (cv *ConversationViewImpl) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	if mouseMsg, ok := msg.(tea.MouseMsg); ok {
		if mouseMsg.Action == tea.MouseActionPress {
			switch mouseMsg.Button {
			case tea.MouseButtonWheelDown:
				cv.viewport.ScrollDown(1)
				return cv, nil
			case tea.MouseButtonWheelUp:
				cv.viewport.ScrollUp(1)
				return cv, nil
			}
		}
	}

	switch msg.(type) {
	case tea.KeyMsg:

	default:
		cv.viewport, cmd = cv.viewport.Update(msg)
	}

	switch msg := msg.(type) {
	case UpdateHistoryMsg:
		cv.SetConversation(msg.History)
		return cv, cmd
	case ScrollRequestMsg:
		if msg.ComponentID == cv.GetID() {
			return cv.handleScrollRequest(msg)
		}
	}
	return cv, cmd
}


func (cv *ConversationViewImpl) handleScrollRequest(msg ScrollRequestMsg) (tea.Model, tea.Cmd) {
	switch msg.Direction {
	case ScrollUp:
		for i := 0; i < msg.Amount; i++ {
			cv.viewport.ScrollUp(1)
		}
	case ScrollDown:
		for i := 0; i < msg.Amount; i++ {
			cv.viewport.ScrollDown(1)
		}
	case ScrollToTop:
		cv.viewport.GotoTop()
	case ScrollToBottom:
		cv.viewport.GotoBottom()
	}
	return cv, nil
}

// InputViewImpl implements InputComponent
type InputViewImpl struct {
	text          string
	cursor        int
	placeholder   string
	width         int
	height        int
	theme         Theme
	modelService  domain.ModelService
	autocomplete  *AutocompleteImpl
	history       []string
	historyIndex  int
	currentInput  string
}

func (iv *InputViewImpl) GetInput() string {
	return iv.text
}

func (iv *InputViewImpl) ClearInput() {
	iv.text = ""
	iv.cursor = 0
	iv.historyIndex = -1
	iv.currentInput = ""
	iv.autocomplete.Hide()
}

func (iv *InputViewImpl) SetPlaceholder(text string) {
	iv.placeholder = text
}

func (iv *InputViewImpl) GetCursor() int {
	return iv.cursor
}

func (iv *InputViewImpl) SetCursor(position int) {
	if position >= 0 && position <= len(iv.text) {
		iv.cursor = position
	}
}

func (iv *InputViewImpl) SetText(text string) {
	iv.text = text
}

// addToHistory adds a message to the input history, keeping only the last 5
func (iv *InputViewImpl) addToHistory(message string) {
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
func (iv *InputViewImpl) navigateHistoryUp() {
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
func (iv *InputViewImpl) navigateHistoryDown() {
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

func (iv *InputViewImpl) SetWidth(width int) {
	iv.width = width
	if iv.autocomplete != nil {
		iv.autocomplete.SetWidth(width)
	}
}

func (iv *InputViewImpl) SetHeight(height int) {
	iv.height = height
}

func (iv *InputViewImpl) Render() string {
	var displayText string

	if iv.text == "" {
		displayText = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Render(iv.placeholder)
	} else {
		before := iv.text[:iv.cursor]
		after := iv.text[iv.cursor:]
		displayText = fmt.Sprintf("%s│%s", before, after)
	}

	inputContent := fmt.Sprintf("> %s", displayText)

	inputStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 1).
		Width(iv.width - 4)

	borderedInput := inputStyle.Render(inputContent)

	components := []string{borderedInput}

	if iv.autocomplete.IsVisible() && iv.height >= 3 {
		autocompleteContent := iv.autocomplete.Render()
		if autocompleteContent != "" {
			components = append(components, autocompleteContent)
		}
	}

	currentModel := iv.modelService.GetCurrentModel()
	if currentModel != "" && iv.height >= 2 {
		modelStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Width(iv.width)
		modelDisplay := modelStyle.Render(fmt.Sprintf("Model: %s", currentModel))
		components = append(components, modelDisplay)
	}

	return lipgloss.JoinVertical(lipgloss.Left, components...)
}

func (iv *InputViewImpl) GetID() string { return "input" }

func (iv *InputViewImpl) Init() tea.Cmd { return nil }

func (iv *InputViewImpl) View() string { return iv.Render() }

func (iv *InputViewImpl) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

func (iv *InputViewImpl) HandleKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	keyStr := key.String()

	if !iv.autocomplete.IsVisible() {
		switch keyStr {
		case "up":
			iv.navigateHistoryUp()
			iv.autocomplete.Update(iv.text, iv.cursor)
			return iv, nil
		case "down":
			iv.navigateHistoryDown()
			iv.autocomplete.Update(iv.text, iv.cursor)
			return iv, nil
		}
	}

	if handled, completion := iv.autocomplete.HandleKey(key); handled {
		return iv.handleAutocomplete(completion)
	}

	if keyStr != "up" && keyStr != "down" && keyStr != "left" && keyStr != "right" &&
	   keyStr != "ctrl+a" && keyStr != "ctrl+e" && keyStr != "home" && keyStr != "end" {
		iv.historyIndex = -1
		iv.currentInput = ""
	}

	cmd := iv.handleSpecificKeys(key)
	iv.autocomplete.Update(iv.text, iv.cursor)
	return iv, cmd
}

func (iv *InputViewImpl) handleAutocomplete(completion string) (tea.Model, tea.Cmd) {
	if completion != "" {
		iv.text = completion
		iv.cursor = len(completion)
		iv.autocomplete.Hide()
	}
	iv.autocomplete.Update(iv.text, iv.cursor)
	return iv, nil
}

func (iv *InputViewImpl) handleSpecificKeys(key tea.KeyMsg) tea.Cmd {
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
	case "ctrl+u":
		iv.deleteToBeginning()
	case "ctrl+w":
		iv.deleteWordBackward()
	case "ctrl+d":
		return iv.handleSubmit()
	case "ctrl+shift+c":
		iv.handleCopy()
	case "ctrl+v", "alt+v":
		iv.handlePaste()
	case "ctrl+x":
		iv.handleCut()
	case "ctrl+a":
		iv.cursor = 0
	case "ctrl+e":
		iv.cursor = len(iv.text)
	default:
		return iv.handleCharacterInput(key)
	}
	return nil
}

func (iv *InputViewImpl) handleSubmit() tea.Cmd {
	if iv.text != "" {
		input := iv.text
		iv.addToHistory(input) // Add to history before clearing
		iv.ClearInput()
		iv.autocomplete.Hide()
		return func() tea.Msg {
			return UserInputMsg{Content: input}
		}
	}
	return nil
}

func (iv *InputViewImpl) handleCopy() {
	if iv.text != "" {
		_ = clipboard.WriteAll(iv.text)
	}
}

func (iv *InputViewImpl) handlePaste() {
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

func (iv *InputViewImpl) handleCut() {
	if iv.text != "" {
		_ = clipboard.WriteAll(iv.text)
		iv.text = ""
		iv.cursor = 0
	}
}

func (iv *InputViewImpl) handleCharacterInput(key tea.KeyMsg) tea.Cmd {
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

func (iv *InputViewImpl) CanHandle(key tea.KeyMsg) bool {
	return true
}

// deleteWordBackward deletes the word before the cursor
func (iv *InputViewImpl) deleteWordBackward() {
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
func (iv *InputViewImpl) deleteToBeginning() {
	if iv.cursor > 0 {
		iv.text = iv.text[iv.cursor:]
		iv.cursor = 0
	}
}

// StatusViewImpl implements StatusComponent
type StatusViewImpl struct {
	message     string
	isError     bool
	isSpinner   bool
	spinner     spinner.Model
	theme       Theme
	startTime   time.Time
	tokenUsage  string
	baseMessage string
	debugInfo   string
}

func (sv *StatusViewImpl) ShowStatus(message string) {
	sv.message = message
	sv.baseMessage = message
	sv.isError = false
	sv.isSpinner = false
	sv.tokenUsage = ""
}

func (sv *StatusViewImpl) ShowError(message string) {
	sv.message = message
	sv.isError = true
	sv.isSpinner = false
}

func (sv *StatusViewImpl) ShowSpinner(message string) {
	sv.baseMessage = message
	sv.message = message
	sv.isError = false
	sv.isSpinner = true
	sv.startTime = time.Now()
	sv.tokenUsage = ""
}

func (sv *StatusViewImpl) ClearStatus() {
	sv.message = ""
	sv.baseMessage = ""
	sv.isError = false
	sv.isSpinner = false
	sv.tokenUsage = ""
	sv.startTime = time.Time{}
	sv.debugInfo = ""
}

func (sv *StatusViewImpl) IsShowingError() bool {
	return sv.isError
}

func (sv *StatusViewImpl) IsShowingSpinner() bool {
	return sv.isSpinner
}

func (sv *StatusViewImpl) SetTokenUsage(usage string) {
	sv.tokenUsage = usage
}

func (sv *StatusViewImpl) SetWidth(width int) {
}

func (sv *StatusViewImpl) SetHeight(height int) {
}

func (sv *StatusViewImpl) Render() string {
	if sv.message == "" && sv.baseMessage == "" && sv.debugInfo == "" {
		return ""
	}

	var prefix, color, displayMessage string
	if sv.isError {
		prefix = "❌"
		color = sv.theme.GetErrorColor()
		displayMessage = sv.message
	} else if sv.isSpinner {
		prefix = sv.spinner.View()
		color = sv.theme.GetStatusColor()

		elapsed := time.Since(sv.startTime)
		seconds := int(elapsed.Seconds())
		displayMessage = fmt.Sprintf("%s (%ds) - Press ESC to interrupt", sv.baseMessage, seconds)
	} else {
		prefix = "ℹ️"
		color = sv.theme.GetStatusColor()
		displayMessage = sv.message

		if sv.tokenUsage != "" {
			displayMessage = fmt.Sprintf("%s (%s)", displayMessage, sv.tokenUsage)
		}
	}

	if sv.debugInfo != "" {
		if displayMessage != "" {
			displayMessage = fmt.Sprintf("%s | %s", displayMessage, sv.debugInfo)
		} else {
			displayMessage = sv.debugInfo
		}
	}

	return fmt.Sprintf("%s%s %s%s", color, prefix, displayMessage, "\033[0m")
}

func (sv *StatusViewImpl) GetID() string { return "status" }

func (sv *StatusViewImpl) Init() tea.Cmd { return sv.spinner.Tick }

func (sv *StatusViewImpl) View() string { return sv.Render() }

func (sv *StatusViewImpl) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	if sv.isSpinner {
		sv.spinner, cmd = sv.spinner.Update(msg)
	}

	switch msg := msg.(type) {
	case SetStatusMsg:
		if msg.Spinner {
			sv.ShowSpinner(msg.Message)
			if cmd == nil {
				cmd = sv.spinner.Tick
			}
		} else {
			sv.ShowStatus(msg.Message)
			if msg.TokenUsage != "" {
				sv.SetTokenUsage(msg.TokenUsage)
			}
		}
	case ShowErrorMsg:
		sv.ShowError(msg.Error)
	case ClearErrorMsg:
		sv.ClearStatus()
	case DebugKeyMsg:
		sv.debugInfo = fmt.Sprintf("DEBUG: %s -> %s", msg.Key, msg.Handler)
	}

	return sv, cmd
}
