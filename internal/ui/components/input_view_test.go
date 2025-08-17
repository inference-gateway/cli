package components

import (
	"context"
	"testing"

	"github.com/charmbracelet/bubbletea"
	"github.com/inference-gateway/cli/internal/domain"
)

// mockModelService is a simple mock for testing
type mockModelService struct{}

var _ domain.ModelService = (*mockModelService)(nil)

func (m *mockModelService) ListModels(ctx context.Context) ([]string, error) {
	return []string{"test-model"}, nil
}

func (m *mockModelService) SelectModel(modelID string) error {
	return nil
}

func (m *mockModelService) GetCurrentModel() string {
	return "test-model"
}

func (m *mockModelService) IsModelAvailable(modelID string) bool {
	return true
}

func (m *mockModelService) ValidateModel(modelID string) error {
	return nil
}

func TestNewInputView(t *testing.T) {
	mockModelService := &mockModelService{}
	iv := NewInputView(mockModelService)

	if iv == nil {
		t.Fatal("Expected InputView to be created, got nil")
	}

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
	mockModelService := &mockModelService{}
	iv := NewInputView(mockModelService)

	testText := "Hello, world!"
	iv.text = testText

	if iv.GetInput() != testText {
		t.Errorf("Expected GetInput to return '%s', got '%s'", testText, iv.GetInput())
	}
}

func TestInputView_ClearInput(t *testing.T) {
	mockModelService := &mockModelService{}
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
	mockModelService := &mockModelService{}
	iv := NewInputView(mockModelService)

	testPlaceholder := "Enter your message..."
	iv.SetPlaceholder(testPlaceholder)

	if iv.placeholder != testPlaceholder {
		t.Errorf("Expected placeholder '%s', got '%s'", testPlaceholder, iv.placeholder)
	}
}

func TestInputView_GetCursor(t *testing.T) {
	mockModelService := &mockModelService{}
	iv := NewInputView(mockModelService)

	iv.cursor = 42

	if iv.GetCursor() != 42 {
		t.Errorf("Expected cursor position 42, got %d", iv.GetCursor())
	}
}

func TestInputView_SetCursor(t *testing.T) {
	mockModelService := &mockModelService{}
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
	mockModelService := &mockModelService{}
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
	mockModelService := &mockModelService{}
	iv := NewInputView(mockModelService)

	iv.SetWidth(120)

	if iv.width != 120 {
		t.Errorf("Expected width 120, got %d", iv.width)
	}
}

func TestInputView_SetHeight(t *testing.T) {
	mockModelService := &mockModelService{}
	iv := NewInputView(mockModelService)

	iv.SetHeight(8)

	if iv.height != 8 {
		t.Errorf("Expected height 8, got %d", iv.height)
	}
}

func TestInputView_Render(t *testing.T) {
	mockModelService := &mockModelService{}
	iv := NewInputView(mockModelService)

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
	mockModelService := &mockModelService{}
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

func TestInputView_HandleKey_CharacterInput(t *testing.T) {
	mockModelService := &mockModelService{}
	iv := NewInputView(mockModelService)

	charKey := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'H'}}

	model, _ := iv.HandleKey(charKey)

	if model == nil {
		t.Error("Expected HandleKey to return non-nil model")
	}

	if iv.text != "H" {
		t.Errorf("Expected text 'H', got '%s'", iv.text)
	}

	if iv.cursor != 1 {
		t.Errorf("Expected cursor position 1, got %d", iv.cursor)
	}
}

func TestInputView_HandleKey_Backspace(t *testing.T) {
	mockModelService := &mockModelService{}
	iv := NewInputView(mockModelService)

	iv.SetText("Hello")
	iv.SetCursor(len("Hello"))

	backspaceKey := tea.KeyMsg{Type: tea.KeyBackspace}
	model, _ := iv.HandleKey(backspaceKey)

	if model == nil {
		t.Error("Expected HandleKey to return non-nil model")
	}

	if iv.text != "Hell" {
		t.Errorf("Expected text 'Hell' after backspace, got '%s'", iv.text)
	}

	if iv.cursor != 4 {
		t.Errorf("Expected cursor position 4 after backspace, got %d", iv.cursor)
	}
}

func TestInputView_HandleKey_ArrowKeys(t *testing.T) {
	mockModelService := &mockModelService{}
	iv := NewInputView(mockModelService)

	iv.SetText("Hello")
	iv.SetCursor(len("Hello"))

	leftKey := tea.KeyMsg{Type: tea.KeyLeft}
	model, _ := iv.HandleKey(leftKey)

	if model == nil {
		t.Error("Expected HandleKey to return non-nil model")
	}

	if iv.cursor != 4 {
		t.Errorf("Expected cursor position 4 after left arrow, got %d", iv.cursor)
	}

	rightKey := tea.KeyMsg{Type: tea.KeyRight}
	iv.HandleKey(rightKey)

	if iv.cursor != 5 {
		t.Errorf("Expected cursor position 5 after right arrow, got %d", iv.cursor)
	}
}

func TestInputView_History(t *testing.T) {
	mockModelService := &mockModelService{}
	iv := NewInputView(mockModelService)

	// Test that history manager is available
	if iv.historyManager == nil {
		t.Error("Expected history manager to be initialized")
	}
}
