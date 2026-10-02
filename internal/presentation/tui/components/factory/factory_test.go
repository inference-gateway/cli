package factory

import (
	"testing"

	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"
	tuimocks "github.com/inference-gateway/cli/tests/mocks/tui"
)

func TestCreateConversationView(t *testing.T) {
	fakeThemeService := &tuimocks.FakeThemeService{}
	fakeTheme := &tuimocks.FakeTheme{}
	fakeTheme.GetDimColorReturns("#888888")
	fakeThemeService.GetCurrentThemeReturns(fakeTheme)

	cv := CreateConversationView(fakeThemeService)

	if cv == nil {
		t.Fatal("Expected CreateConversationView to return non-nil component")
	}
}

func TestCreateInputView(t *testing.T) {
	mockModelService := &convmocks.FakeModelService{}
	mockModelService.ListModelsReturns([]string{"test-model"}, nil)
	mockModelService.GetCurrentModelReturns("test-model")
	mockModelService.IsModelAvailableReturns(true)
	mockModelService.ValidateModelReturns(nil)
	iv := CreateInputView(mockModelService)

	if iv == nil {
		t.Fatal("Expected CreateInputView to return non-nil component")
	}
}

func TestCreateStatusView(t *testing.T) {
	fakeThemeService := &tuimocks.FakeThemeService{}
	fakeTheme := &tuimocks.FakeTheme{}
	fakeTheme.GetDimColorReturns("#888888")
	fakeThemeService.GetCurrentThemeReturns(fakeTheme)

	sv := CreateStatusView(fakeThemeService)

	if sv == nil {
		t.Fatal("Expected CreateStatusView to return non-nil component")
	}
}

func TestCreateHelpBar(t *testing.T) {
	fakeThemeService := &tuimocks.FakeThemeService{}
	fakeTheme := &tuimocks.FakeTheme{}
	fakeTheme.GetDimColorReturns("#888888")
	fakeThemeService.GetCurrentThemeReturns(fakeTheme)

	hb := CreateHelpBar(fakeThemeService)

	if hb == nil {
		t.Fatal("Expected CreateHelpBar to return non-nil component")
	}
}
