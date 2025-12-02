package components

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	domain "github.com/inference-gateway/cli/internal/domain"
	history "github.com/inference-gateway/cli/internal/ui/history"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"
	uimocks "github.com/inference-gateway/cli/tests/mocks/ui"
	require "github.com/stretchr/testify/require"
)

// createMockModelService creates a fake model service with default test values
func createMockModelService() *domainmocks.FakeModelService {
	fake := &domainmocks.FakeModelService{}
	fake.ListModelsReturns([]string{"test-model"}, nil)
	fake.GetCurrentModelReturns("test-model")
	fake.IsModelAvailableReturns(true)
	fake.ValidateModelReturns(nil)
	fake.IsVisionModelReturns(false)
	return fake
}

// createInputViewWithTheme creates an InputView with isolated memory-only history for testing
func createInputViewWithTheme(modelService domain.ModelService) *InputView {
	iv := &InputView{
		text:             "",
		cursor:           0,
		placeholder:      "Type your message...",
		width:            80,
		height:           5,
		modelService:     modelService,
		historyManager:   history.NewMemoryOnlyHistoryManager(5),
		themeService:     nil,
		imageAttachments: []domain.ImageAttachment{},
	}

	fakeTheme := &uimocks.FakeTheme{}
	fakeTheme.GetDimColorReturns("#888888")

	fakeThemeService := &domainmocks.FakeThemeService{}
	fakeThemeService.GetCurrentThemeReturns(fakeTheme)

	iv.themeService = fakeThemeService
	iv.styleProvider = styles.NewProvider(fakeThemeService)

	return iv
}

func TestNewInputView(t *testing.T) {
	mockModelService := createMockModelService()
	iv := NewInputView(mockModelService)

	if iv.text != "" {
		t.Errorf("Expected empty text, got '%s'", iv.text)
	}

	if iv.cursor != 0 {
		t.Errorf("Expected cursor at 0, got %d", iv.cursor)
	}

	if iv.width != 80 {
		t.Errorf("Expected default width 80, got %d", iv.width)
	}

	if iv.height != 5 {
		t.Errorf("Expected default height 5, got %d", iv.height)
	}

	if iv.modelService != mockModelService {
		t.Error("Expected model service to be set")
	}

	if iv.historyManager == nil {
		t.Error("Expected history manager to be initialized")
	}
}

func TestInputView_GetInput(t *testing.T) {
	mockModelService := createMockModelService()
	iv := NewInputView(mockModelService)

	testText := "Hello, world!"
	iv.text = testText

	if iv.GetInput() != testText {
		t.Errorf("Expected GetInput to return '%s', got '%s'", testText, iv.GetInput())
	}
}

func TestInputView_ClearInput(t *testing.T) {
	mockModelService := createMockModelService()
	iv := NewInputView(mockModelService)

	iv.text = "Some text"
	iv.cursor = 5

	iv.ClearInput()

	if iv.text != "" {
		t.Errorf("Expected empty text after clear, got '%s'", iv.text)
	}

	if iv.cursor != 0 {
		t.Errorf("Expected cursor at 0 after clear, got %d", iv.cursor)
	}
}

func TestInputView_SetPlaceholder(t *testing.T) {
	mockModelService := createMockModelService()
	iv := NewInputView(mockModelService)

	testPlaceholder := "Enter your message..."
	iv.SetPlaceholder(testPlaceholder)

	if iv.placeholder != testPlaceholder {
		t.Errorf("Expected placeholder '%s', got '%s'", testPlaceholder, iv.placeholder)
	}
}

func TestInputView_GetCursor(t *testing.T) {
	mockModelService := createMockModelService()
	iv := NewInputView(mockModelService)

	iv.cursor = 42

	if iv.GetCursor() != 42 {
		t.Errorf("Expected cursor position 42, got %d", iv.GetCursor())
	}
}

func TestInputView_SetCursor(t *testing.T) {
	mockModelService := createMockModelService()
	iv := NewInputView(mockModelService)

	iv.SetCursor(15)
	if iv.cursor != 0 {
		t.Errorf("Expected cursor to remain at 0 for invalid position, got %d", iv.cursor)
	}

	iv.SetText("Hello World")
	iv.SetCursor(5)
	if iv.cursor != 5 {
		t.Errorf("Expected cursor position 5, got %d", iv.cursor)
	}
}

func TestInputView_SetText(t *testing.T) {
	mockModelService := createMockModelService()
	iv := NewInputView(mockModelService)

	testText := "New text content"
	iv.SetText(testText)

	if iv.text != testText {
		t.Errorf("Expected text '%s', got '%s'", testText, iv.text)
	}

	if iv.cursor != 0 {
		t.Errorf("Expected cursor to remain at 0, got %d", iv.cursor)
	}
}

func TestInputView_SetWidth(t *testing.T) {
	mockModelService := createMockModelService()
	iv := NewInputView(mockModelService)

	iv.SetWidth(120)

	if iv.width != 120 {
		t.Errorf("Expected width 120, got %d", iv.width)
	}
}

func TestInputView_SetHeight(t *testing.T) {
	mockModelService := createMockModelService()
	iv := NewInputView(mockModelService)

	iv.SetHeight(8)

	if iv.height != 8 {
		t.Errorf("Expected height 8, got %d", iv.height)
	}
}

func TestInputView_Render(t *testing.T) {
	mockModelService := createMockModelService()
	iv := createInputViewWithTheme(mockModelService)

	output := iv.Render()
	if output == "" {
		t.Error("Expected non-empty render output")
	}

	iv.SetText("Hello")
	output = iv.Render()
	if output == "" {
		t.Error("Expected non-empty render output with text")
	}
}

func TestInputView_CanHandle(t *testing.T) {
	mockModelService := createMockModelService()
	iv := NewInputView(mockModelService)

	charKey := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}
	if !iv.CanHandle(charKey) {
		t.Error("Expected CanHandle to return true for character input")
	}

	backspaceKey := tea.KeyMsg{Type: tea.KeyBackspace}
	if !iv.CanHandle(backspaceKey) {
		t.Error("Expected CanHandle to return true for backspace")
	}

	leftKey := tea.KeyMsg{Type: tea.KeyLeft}
	if !iv.CanHandle(leftKey) {
		t.Error("Expected CanHandle to return true for left arrow")
	}

	rightKey := tea.KeyMsg{Type: tea.KeyRight}
	if !iv.CanHandle(rightKey) {
		t.Error("Expected CanHandle to return true for right arrow")
	}
}

func TestInputView_History(t *testing.T) {
	mockModelService := createMockModelService()
	iv := NewInputView(mockModelService)

	if iv.historyManager == nil {
		t.Error("Expected history manager to be initialized")
	}
}

func TestInputView_BashModeBorderColor(t *testing.T) {
	mockModelService := createMockModelService()
	iv := createInputViewWithTheme(mockModelService)

	iv.SetText("normal text")
	normalOutput := iv.Render()
	if normalOutput == "" {
		t.Error("Expected non-empty render output for normal text")
	}

	iv.SetText("!")
	bashOutput := iv.Render()
	if bashOutput == "" {
		t.Error("Expected non-empty render output for bash mode")
	}

	iv.SetText("!!")
	toolsOutput := iv.Render()
	if toolsOutput == "" {
		t.Error("Expected non-empty render output for tools mode")
	}
}

func TestInputView_HistorySuggestions_SingleMatch(t *testing.T) {
	mockModelService := createMockModelService()
	iv := createInputViewWithTheme(mockModelService)

	require.NoError(t, iv.historyManager.AddToHistory("create a pull request"))
	require.NoError(t, iv.historyManager.AddToHistory("list files"))

	iv.SetText("cre")
	iv.cursor = len(iv.text)

	iv.Render()

	if !iv.HasHistorySuggestion() {
		t.Error("Expected history suggestion to be available")
	}

	if iv.historySuggestion != "ate a pull request" {
		t.Errorf("Expected suggestion 'ate a pull request', got '%s'", iv.historySuggestion)
	}

	if len(iv.historySuggestions) != 1 {
		t.Errorf("Expected 1 matching suggestion, got %d", len(iv.historySuggestions))
	}
}

func TestInputView_HistorySuggestions_MultipleMatches(t *testing.T) {
	mockModelService := createMockModelService()
	iv := createInputViewWithTheme(mockModelService)

	require.NoError(t, iv.historyManager.AddToHistory("create a pull request"))
	require.NoError(t, iv.historyManager.AddToHistory("create a new branch"))
	require.NoError(t, iv.historyManager.AddToHistory("create tests"))

	iv.SetText("create")
	iv.cursor = len(iv.text)

	iv.Render()

	if len(iv.historySuggestions) != 3 {
		t.Errorf("Expected 3 matching suggestions, got %d", len(iv.historySuggestions))
	}

	if iv.historySuggestion != " a pull request" {
		t.Errorf("Expected suggestion ' a pull request', got '%s'", iv.historySuggestion)
	}
}

func TestInputView_HistorySuggestions_CycleThrough(t *testing.T) {
	mockModelService := createMockModelService()
	iv := createInputViewWithTheme(mockModelService)

	require.NoError(t, iv.historyManager.AddToHistory("create a pull request"))
	require.NoError(t, iv.historyManager.AddToHistory("create a new branch"))

	iv.SetText("create")
	iv.cursor = len(iv.text)
	iv.Render()

	firstSuggestion := iv.historySuggestion

	iv.cycleHistorySuggestion()
	secondSuggestion := iv.historySuggestion

	if firstSuggestion == secondSuggestion {
		t.Error("Expected different suggestion after cycling")
	}

	iv.cycleHistorySuggestion()
	if iv.historySuggestion != firstSuggestion {
		t.Errorf("Expected to cycle back to first suggestion '%s', got '%s'", firstSuggestion, iv.historySuggestion)
	}
}

func TestInputView_HistorySuggestions_AcceptSuggestion(t *testing.T) {
	mockModelService := createMockModelService()
	iv := createInputViewWithTheme(mockModelService)

	require.NoError(t, iv.historyManager.AddToHistory("create a pull request"))

	iv.SetText("cre")
	iv.cursor = len(iv.text)
	iv.Render()

	accepted := iv.AcceptHistorySuggestion()

	if !accepted {
		t.Error("Expected AcceptHistorySuggestion to return true")
	}

	if iv.text != "create a pull request" {
		t.Errorf("Expected text to be 'create a pull request', got '%s'", iv.text)
	}

	if iv.cursor != len(iv.text) {
		t.Errorf("Expected cursor to be at end (%d), got %d", len(iv.text), iv.cursor)
	}

	if iv.HasHistorySuggestion() {
		t.Error("Expected suggestion to be cleared after acceptance")
	}
}

func TestInputView_HistorySuggestions_NoMatchWhenEmpty(t *testing.T) {
	mockModelService := createMockModelService()
	iv := createInputViewWithTheme(mockModelService)

	require.NoError(t, iv.historyManager.AddToHistory("create a pull request"))

	iv.SetText("")
	iv.cursor = 0
	iv.Render()

	if iv.HasHistorySuggestion() {
		t.Error("Expected no suggestion for empty text")
	}

	if len(iv.historySuggestions) != 0 {
		t.Errorf("Expected 0 suggestions, got %d", len(iv.historySuggestions))
	}
}

func TestInputView_HistorySuggestions_NoMatchWhenCursorNotAtEnd(t *testing.T) {
	mockModelService := createMockModelService()
	iv := createInputViewWithTheme(mockModelService)

	require.NoError(t, iv.historyManager.AddToHistory("create a pull request"))

	iv.SetText("create")
	iv.cursor = 3
	iv.Render()

	if iv.HasHistorySuggestion() {
		t.Error("Expected no suggestion when cursor is not at end")
	}
}

func TestInputView_HistorySuggestions_NoMatchWhenNoPrefix(t *testing.T) {
	mockModelService := createMockModelService()
	iv := createInputViewWithTheme(mockModelService)

	require.NoError(t, iv.historyManager.AddToHistory("create a pull request"))

	iv.SetText("xyz")
	iv.cursor = len(iv.text)
	iv.Render()

	if iv.HasHistorySuggestion() {
		t.Error("Expected no suggestion for non-matching prefix")
	}

	if len(iv.historySuggestions) != 0 {
		t.Errorf("Expected 0 suggestions, got %d", len(iv.historySuggestions))
	}
}

func TestInputView_HistorySuggestions_CaseInsensitive(t *testing.T) {
	mockModelService := createMockModelService()
	iv := createInputViewWithTheme(mockModelService)

	require.NoError(t, iv.historyManager.AddToHistory("Create a pull request"))

	iv.SetText("cre")
	iv.cursor = len(iv.text)
	iv.Render()

	if !iv.HasHistorySuggestion() {
		t.Error("Expected case-insensitive matching to work")
	}

	if len(iv.historySuggestions) != 1 {
		t.Errorf("Expected 1 suggestion with case-insensitive match, got %d", len(iv.historySuggestions))
	}
}

func TestInputView_HistorySuggestions_ExcludesExactMatch(t *testing.T) {
	mockModelService := createMockModelService()
	iv := createInputViewWithTheme(mockModelService)

	require.NoError(t, iv.historyManager.AddToHistory("create"))

	iv.SetText("create")
	iv.cursor = len(iv.text)
	iv.Render()

	if iv.HasHistorySuggestion() {
		t.Error("Expected no suggestion for exact match")
	}
}

func TestInputView_HistorySuggestions_AcceptWithNoSuggestion(t *testing.T) {
	mockModelService := createMockModelService()
	iv := createInputViewWithTheme(mockModelService)

	accepted := iv.AcceptHistorySuggestion()

	if accepted {
		t.Error("Expected AcceptHistorySuggestion to return false when no suggestion")
	}
}

func TestInputView_HistorySuggestions_TabHandling(t *testing.T) {
	mockModelService := createMockModelService()
	iv := createInputViewWithTheme(mockModelService)

	require.NoError(t, iv.historyManager.AddToHistory("create a pull request"))
	require.NoError(t, iv.historyManager.AddToHistory("create a new branch"))

	iv.SetText("create")
	iv.cursor = len(iv.text)
	iv.Render()

	firstSuggestion := iv.historySuggestion

	tabKey := tea.KeyMsg{Type: tea.KeyTab}
	_, _ = iv.HandleKey(tabKey)

	if iv.historySuggestion == firstSuggestion {
		t.Error("Expected Tab to cycle to different suggestion")
	}
}
