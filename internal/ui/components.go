package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
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
	return totalHeight - inputHeight - statusHeight
}

func (l *DefaultLayout) CalculateInputHeight(totalHeight int) int {
	return 5 // Input area takes 5 lines (border + input + model name + border)
}

func (l *DefaultLayout) CalculateStatusHeight(totalHeight int) int {
	return 3 // Status area takes 3 lines
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
	return &ConversationViewImpl{
		theme:        f.theme,
		conversation: []domain.ConversationEntry{},
		scrollOffset: 0,
		width:        80,
		height:       20,
	}
}

func (f *ComponentFactory) CreateInputView() InputComponent {
	return &InputViewImpl{
		text:         "",
		cursor:       0,
		placeholder:  "Type your message... (Press Ctrl+D to send)",
		width:        80,
		theme:        f.theme,
		modelService: f.modelService,
		autocomplete: NewAutocomplete(f.theme, f.commandRegistry),
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
	theme        Theme
	conversation []domain.ConversationEntry
	scrollOffset int
	width        int
	height       int
}

func (cv *ConversationViewImpl) SetConversation(conversation []domain.ConversationEntry) {
	cv.conversation = conversation
}

func (cv *ConversationViewImpl) SetScrollOffset(offset int) {
	cv.scrollOffset = offset
}

func (cv *ConversationViewImpl) GetScrollOffset() int {
	return cv.scrollOffset
}

func (cv *ConversationViewImpl) CanScrollUp() bool {
	return cv.scrollOffset > 0
}

func (cv *ConversationViewImpl) CanScrollDown() bool {
	return len(cv.conversation) > cv.height && cv.scrollOffset < len(cv.conversation)-cv.height
}

func (cv *ConversationViewImpl) SetWidth(width int) {
	cv.width = width
}

func (cv *ConversationViewImpl) SetHeight(height int) {
	cv.height = height
}

func (cv *ConversationViewImpl) Render() string {
	if len(cv.conversation) == 0 {
		return cv.renderWelcome()
	}

	var b strings.Builder
	visibleEntries := cv.getVisibleEntries()

	for _, entry := range visibleEntries {
		b.WriteString(cv.renderEntry(entry))
		b.WriteString("\n")
	}

	return b.String()
}

func (cv *ConversationViewImpl) renderWelcome() string {
	return fmt.Sprintf("%s🤖 Chat session ready! Type your message below.%s\n",
		cv.theme.GetStatusColor(), "\033[0m")
}

func (cv *ConversationViewImpl) getVisibleEntries() []domain.ConversationEntry {
	if len(cv.conversation) <= cv.height {
		return cv.conversation
	}

	start := cv.scrollOffset
	end := start + cv.height
	if end > len(cv.conversation) {
		end = len(cv.conversation)
	}

	return cv.conversation[start:end]
}

func (cv *ConversationViewImpl) renderEntry(entry domain.ConversationEntry) string {
	var color, role string

	switch entry.Message.Role {
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
	default:
		color = cv.theme.GetDimColor()
		role = string(entry.Message.Role)
	}

	content := entry.Message.Content
	resetColor := "\033[0m"

	return fmt.Sprintf("%s%s:%s %s", color, role, resetColor, content)
}

func (cv *ConversationViewImpl) GetID() string { return "conversation" }

func (cv *ConversationViewImpl) Init() tea.Cmd { return nil }

func (cv *ConversationViewImpl) View() string { return cv.Render() }

func (cv *ConversationViewImpl) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case UpdateHistoryMsg:
		cv.SetConversation(msg.History)
		return cv, nil
	}
	return cv, nil
}

// InputViewImpl implements InputComponent
type InputViewImpl struct {
	text         string
	cursor       int
	placeholder  string
	width        int
	theme        Theme
	modelService domain.ModelService
	autocomplete *AutocompleteImpl
}

func (iv *InputViewImpl) GetInput() string {
	return iv.text
}

func (iv *InputViewImpl) ClearInput() {
	iv.text = ""
	iv.cursor = 0
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

func (iv *InputViewImpl) SetWidth(width int) {
	iv.width = width
	if iv.autocomplete != nil {
		iv.autocomplete.SetWidth(width)
	}
}

func (iv *InputViewImpl) SetHeight(height int) {
	// Input view height is fixed
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

	var result strings.Builder
	result.WriteString(borderedInput)
	result.WriteString("\n")

	// Add autocomplete suggestions if visible
	if iv.autocomplete.IsVisible() {
		autocompleteContent := iv.autocomplete.Render()
		if autocompleteContent != "" {
			result.WriteString(autocompleteContent)
			result.WriteString("\n")
		}
	}

	currentModel := iv.modelService.GetCurrentModel()
	if currentModel != "" {
		modelStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))
		modelDisplay := modelStyle.Render(fmt.Sprintf("Model: %s", currentModel))
		result.WriteString(modelDisplay)
	}

	return result.String()
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
	// First, check if autocomplete should handle the key
	if handled, completion := iv.autocomplete.HandleKey(key); handled {
		if completion != "" {
			// Replace the current input with the selected completion
			iv.text = completion
			iv.cursor = len(completion)
			iv.autocomplete.Hide()
		}
		// Update autocomplete state after any changes
		iv.autocomplete.Update(iv.text, iv.cursor)
		return iv, nil
	}

	switch key.String() {
	case "left":
		if iv.cursor > 0 {
			iv.cursor--
		}
	case "right":
		if iv.cursor < len(iv.text) {
			iv.cursor++
		}
	case "backspace":
		if iv.cursor > 0 {
			iv.text = iv.text[:iv.cursor-1] + iv.text[iv.cursor:]
			iv.cursor--
		}
	case "ctrl+d":
		if iv.text != "" {
			input := iv.text
			iv.ClearInput()
			iv.autocomplete.Hide()
			return iv, func() tea.Msg {
				return UserInputMsg{Content: input}
			}
		}
	default:
		if len(key.String()) == 1 && key.String()[0] >= 32 {
			char := key.String()
			iv.text = iv.text[:iv.cursor] + char + iv.text[iv.cursor:]
			iv.cursor++

			if char == "@" {
				return iv, func() tea.Msg {
					return FileSelectionRequestMsg{}
				}
			}
		}
	}

	// Update autocomplete after any text changes
	iv.autocomplete.Update(iv.text, iv.cursor)

	return iv, nil
}

func (iv *InputViewImpl) CanHandle(key tea.KeyMsg) bool {
	return true
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
	if sv.message == "" && sv.baseMessage == "" {
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

		// Calculate elapsed time
		elapsed := time.Since(sv.startTime)
		seconds := int(elapsed.Seconds())
		displayMessage = fmt.Sprintf("%s (%ds)", sv.baseMessage, seconds)
	} else {
		prefix = "ℹ️"
		color = sv.theme.GetStatusColor()
		displayMessage = sv.message

		// Show token usage if available
		if sv.tokenUsage != "" {
			displayMessage = fmt.Sprintf("%s (%s)", displayMessage, sv.tokenUsage)
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
	}

	return sv, cmd
}
