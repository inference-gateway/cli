package ui

import (
	"context"
	"testing"

	domain "github.com/inference-gateway/cli/internal/domain"
	shortcuts "github.com/inference-gateway/cli/internal/shortcuts"
)

// mockTheme implements domain.Theme for testing
type mockTheme struct{}

func (m *mockTheme) GetUserColor() string       { return "#00FF00" }
func (m *mockTheme) GetAssistantColor() string  { return "#0000FF" }
func (m *mockTheme) GetErrorColor() string      { return "#FF0000" }
func (m *mockTheme) GetSuccessColor() string    { return "#00FF00" }
func (m *mockTheme) GetStatusColor() string     { return "#FFFF00" }
func (m *mockTheme) GetAccentColor() string     { return "#FF00FF" }
func (m *mockTheme) GetDimColor() string        { return "#808080" }
func (m *mockTheme) GetBorderColor() string     { return "#FFFFFF" }
func (m *mockTheme) GetDiffAddColor() string    { return "#00FF00" }
func (m *mockTheme) GetDiffRemoveColor() string { return "#FF0000" }

// mockThemeService implements domain.ThemeService for testing
type mockThemeService struct{}

var _ domain.ThemeService = (*mockThemeService)(nil)

func (m *mockThemeService) ListThemes() []string {
	return []string{"default"}
}

func (m *mockThemeService) GetCurrentTheme() domain.Theme {
	return &mockTheme{}
}

func (m *mockThemeService) GetCurrentThemeName() string {
	return "default"
}

func (m *mockThemeService) SetTheme(themeName string) error {
	return nil
}

func TestCreateConversationView(t *testing.T) {
	cv := CreateConversationView(&mockThemeService{})

	if cv == nil {
		t.Fatal("Expected CreateConversationView to return non-nil component")
	}
}

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

func TestCreateInputView(t *testing.T) {
	mockModelService := &mockModelService{}

	iv := CreateInputView(mockModelService, nil)

	if iv == nil {
		t.Fatal("Expected CreateInputView to return non-nil component")
	}

	shortcutRegistry := &shortcuts.Registry{}
	iv2 := CreateInputView(mockModelService, shortcutRegistry)

	if iv2 == nil {
		t.Fatal("Expected CreateInputView to return non-nil component with command registry")
	}
}

func TestCreateStatusView(t *testing.T) {
	sv := CreateStatusView(&mockThemeService{})

	if sv == nil {
		t.Fatal("Expected CreateStatusView to return non-nil component")
	}
}

func TestCreateHelpBar(t *testing.T) {
	hb := CreateHelpBar(&mockThemeService{})

	if hb == nil {
		t.Fatal("Expected CreateHelpBar to return non-nil component")
	}
}

func TestCalculateConversationHeight(t *testing.T) {
	testCases := []struct {
		name            string
		totalHeight     int
		expectedMinimum int
	}{
		{"Very small terminal", 6, 3},
		{"Small terminal", 10, 3},
		{"Medium terminal", 20, 3},
		{"Large terminal", 40, 3},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			height := CalculateConversationHeight(tc.totalHeight)

			if height < tc.expectedMinimum {
				t.Errorf("Expected conversation height >= %d, got %d", tc.expectedMinimum, height)
			}

			if height > tc.totalHeight {
				t.Errorf("Expected conversation height <= total height %d, got %d", tc.totalHeight, height)
			}
		})
	}
}

func TestCalculateInputHeight(t *testing.T) {
	testCases := []struct {
		totalHeight    int
		expectedHeight int
	}{
		{6, 2},
		{8, 3},
		{12, 4},
		{20, 4},
		{50, 4},
	}

	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			height := CalculateInputHeight(tc.totalHeight)

			if height != tc.expectedHeight {
				t.Errorf("For total height %d, expected input height %d, got %d",
					tc.totalHeight, tc.expectedHeight, height)
			}
		})
	}
}

func TestCalculateStatusHeight(t *testing.T) {
	testCases := []struct {
		totalHeight    int
		expectedHeight int
	}{
		{6, 0},
		{8, 1},
		{12, 2},
		{20, 2},
		{50, 2},
	}

	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			height := CalculateStatusHeight(tc.totalHeight)

			if height != tc.expectedHeight {
				t.Errorf("For total height %d, expected status height %d, got %d",
					tc.totalHeight, tc.expectedHeight, height)
			}
		})
	}
}

func TestGetMargins(t *testing.T) {
	top, right, bottom, left := GetMargins()

	expectedTop := 1
	expectedRight := 2
	expectedBottom := 1
	expectedLeft := 2

	if top != expectedTop {
		t.Errorf("Expected top margin %d, got %d", expectedTop, top)
	}

	if right != expectedRight {
		t.Errorf("Expected right margin %d, got %d", expectedRight, right)
	}

	if bottom != expectedBottom {
		t.Errorf("Expected bottom margin %d, got %d", expectedBottom, bottom)
	}

	if left != expectedLeft {
		t.Errorf("Expected left margin %d, got %d", expectedLeft, left)
	}
}

func TestLayoutCalculations_Consistency(t *testing.T) {
	totalHeight := 24

	conversationHeight := CalculateConversationHeight(totalHeight)
	inputHeight := CalculateInputHeight(totalHeight)
	statusHeight := CalculateStatusHeight(totalHeight)

	top, _, bottom, _ := GetMargins()
	usedHeight := conversationHeight + inputHeight + statusHeight + top + bottom + 5

	if usedHeight > totalHeight+5 {
		t.Errorf("Layout calculations exceed total height: conversation=%d, input=%d, status=%d, margins=%d, extra=5, total=%d",
			conversationHeight, inputHeight, statusHeight, top+bottom, totalHeight)
	}
}

func TestLayoutCalculations_MinimumHeights(t *testing.T) {
	totalHeight := 5

	conversationHeight := CalculateConversationHeight(totalHeight)
	inputHeight := CalculateInputHeight(totalHeight)
	statusHeight := CalculateStatusHeight(totalHeight)

	if conversationHeight < 3 {
		t.Errorf("Conversation height too small: %d", conversationHeight)
	}

	if inputHeight < 2 {
		t.Errorf("Input height too small: %d", inputHeight)
	}

	if statusHeight < 0 {
		t.Errorf("Status height negative: %d", statusHeight)
	}
}
