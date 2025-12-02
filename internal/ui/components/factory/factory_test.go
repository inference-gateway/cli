package factory

import (
	"testing"

	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"
	uimocks "github.com/inference-gateway/cli/tests/mocks/ui"
)

func TestCreateConversationView(t *testing.T) {
	fakeThemeService := &domainmocks.FakeThemeService{}
	fakeTheme := &uimocks.FakeTheme{}
	fakeTheme.GetDimColorReturns("#888888")
	fakeThemeService.GetCurrentThemeReturns(fakeTheme)

	cv := CreateConversationView(fakeThemeService)

	if cv == nil {
		t.Fatal("Expected CreateConversationView to return non-nil component")
	}
}

func TestCreateInputView(t *testing.T) {
	mockModelService := &domainmocks.FakeModelService{}
	mockModelService.ListModelsReturns([]string{"test-model"}, nil)
	mockModelService.GetCurrentModelReturns("test-model")
	mockModelService.IsModelAvailableReturns(true)
	mockModelService.ValidateModelReturns(nil)
	mockModelService.IsVisionModelReturns(false)

	iv := CreateInputView(mockModelService)

	if iv == nil {
		t.Fatal("Expected CreateInputView to return non-nil component")
	}
}

func TestCreateStatusView(t *testing.T) {
	fakeThemeService := &domainmocks.FakeThemeService{}
	fakeTheme := &uimocks.FakeTheme{}
	fakeTheme.GetDimColorReturns("#888888")
	fakeThemeService.GetCurrentThemeReturns(fakeTheme)

	sv := CreateStatusView(fakeThemeService)

	if sv == nil {
		t.Fatal("Expected CreateStatusView to return non-nil component")
	}
}

func TestCreateHelpBar(t *testing.T) {
	fakeThemeService := &domainmocks.FakeThemeService{}
	fakeTheme := &uimocks.FakeTheme{}
	fakeTheme.GetDimColorReturns("#888888")
	fakeThemeService.GetCurrentThemeReturns(fakeTheme)

	hb := CreateHelpBar(fakeThemeService)

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
