package autocomplete_test

import (
	"testing"

	shortcuts "github.com/inference-gateway/cli/internal/shortcuts"
	autocomplete "github.com/inference-gateway/cli/internal/ui/autocomplete"
	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"
	shortcutsmocks "github.com/inference-gateway/cli/tests/mocks/shortcuts"
	uimocks "github.com/inference-gateway/cli/tests/mocks/ui"
	sdk "github.com/inference-gateway/sdk"
	assert "github.com/stretchr/testify/assert"
)

func TestAutocomplete_CommandMode(t *testing.T) {
	fakeShortcut1 := &shortcutsmocks.FakeShortcut{}
	fakeShortcut1.GetNameReturns("help")
	fakeShortcut1.GetDescriptionReturns("Show help")

	fakeShortcut2 := &shortcutsmocks.FakeShortcut{}
	fakeShortcut2.GetNameReturns("clear")
	fakeShortcut2.GetDescriptionReturns("Clear screen")

	fakeShortcut3 := &shortcutsmocks.FakeShortcut{}
	fakeShortcut3.GetNameReturns("exit")
	fakeShortcut3.GetDescriptionReturns("Exit application")

	mockRegistry := &uimocks.FakeShortcutRegistry{}
	mockRegistry.GetAllReturns([]shortcuts.Shortcut{fakeShortcut1, fakeShortcut2, fakeShortcut3})

	theme := &uimocks.FakeTheme{}
	theme.GetDimColorReturns("#808080")
	theme.GetAccentColorReturns("#FF00FF")

	autocomplete := autocomplete.NewAutocomplete(theme, mockRegistry)

	tests := []struct {
		name            string
		input           string
		cursorPos       int
		expectedVisible bool
		expectedCount   int
	}{
		{
			name:            "Empty command prefix shows all commands",
			input:           "/",
			cursorPos:       1,
			expectedVisible: true,
			expectedCount:   3,
		},
		{
			name:            "Partial command match",
			input:           "/he",
			cursorPos:       3,
			expectedVisible: true,
			expectedCount:   1,
		},
		{
			name:            "No command match",
			input:           "/xyz",
			cursorPos:       4,
			expectedVisible: false,
			expectedCount:   0,
		},
		{
			name:            "Not a command",
			input:           "regular text",
			cursorPos:       12,
			expectedVisible: false,
			expectedCount:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			autocomplete.Update(tt.input, tt.cursorPos)

			assert.Equal(t, tt.expectedVisible, autocomplete.IsVisible())
		})
	}
}

func TestAutocomplete_ToolsMode(t *testing.T) {
	mockRegistry := &uimocks.FakeShortcutRegistry{}
	mockRegistry.GetAllReturns([]shortcuts.Shortcut{})

	mockToolService := &domainmocks.FakeToolService{}
	mockToolService.ListAvailableToolsReturns([]string{
		"Read", "Write", "Bash", "WebSearch", "Tree",
	})

	readDesc := "Read files"
	writeDesc := "Write files"
	bashDesc := "Execute bash commands"

	readParams := sdk.FunctionParameters(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "The path to the file to read",
			},
		},
		"required": []string{"file_path"},
	})

	writeParams := sdk.FunctionParameters(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "The path to the file to write",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "The content to write",
			},
		},
		"required": []string{"file_path", "content"},
	})

	bashParams := sdk.FunctionParameters(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "The command to execute",
			},
		},
		"required": []string{"command"},
	})

	mockToolService.ListToolsReturns([]sdk.ChatCompletionTool{
		{
			Type: sdk.Function,
			Function: sdk.FunctionObject{
				Name:        "Read",
				Description: &readDesc,
				Parameters:  &readParams,
			},
		},
		{
			Type: sdk.Function,
			Function: sdk.FunctionObject{
				Name:        "Write",
				Description: &writeDesc,
				Parameters:  &writeParams,
			},
		},
		{
			Type: sdk.Function,
			Function: sdk.FunctionObject{
				Name:        "Bash",
				Description: &bashDesc,
				Parameters:  &bashParams,
			},
		},
	})

	theme := &uimocks.FakeTheme{}
	theme.GetDimColorReturns("#808080")
	theme.GetAccentColorReturns("#FF00FF")

	autocomplete := autocomplete.NewAutocomplete(theme, mockRegistry)
	autocomplete.SetToolService(mockToolService)

	tests := []struct {
		name            string
		input           string
		cursorPos       int
		expectedVisible bool
		expectedCount   int
		expectedTools   []string
	}{
		{
			name:            "Empty tools prefix shows all tools",
			input:           "!!",
			cursorPos:       2,
			expectedVisible: true,
			expectedCount:   5,
			expectedTools:   []string{"!!Read(file_path=\"\")", "!!Write(file_path=\"\", content=\"\")", "!!Bash(command=\"\")", "!!WebSearch(", "!!Tree("},
		},
		{
			name:            "Partial tool match",
			input:           "!!Re",
			cursorPos:       4,
			expectedVisible: true,
			expectedCount:   1,
			expectedTools:   []string{"!!Read(file_path=\"\")"},
		},
		{
			name:            "Case insensitive tool match",
			input:           "!!web",
			cursorPos:       5,
			expectedVisible: true,
			expectedCount:   1,
			expectedTools:   []string{"!!WebSearch("},
		},
		{
			name:            "No tool match",
			input:           "!!xyz",
			cursorPos:       5,
			expectedVisible: false,
			expectedCount:   0,
		},
		{
			name:            "Not a tool command",
			input:           "regular text",
			cursorPos:       12,
			expectedVisible: false,
			expectedCount:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			autocomplete.Update(tt.input, tt.cursorPos)

			assert.Equal(t, tt.expectedVisible, autocomplete.IsVisible())
		})
	}
}

func TestAutocomplete_KeyHandling(t *testing.T) {
	fakeShortcut1 := &shortcutsmocks.FakeShortcut{}
	fakeShortcut1.GetNameReturns("help")
	fakeShortcut1.GetDescriptionReturns("Show help")

	fakeShortcut2 := &shortcutsmocks.FakeShortcut{}
	fakeShortcut2.GetNameReturns("clear")
	fakeShortcut2.GetDescriptionReturns("Clear screen")

	mockRegistry := &uimocks.FakeShortcutRegistry{}
	mockRegistry.GetAllReturns([]shortcuts.Shortcut{fakeShortcut1, fakeShortcut2})

	theme := &uimocks.FakeTheme{}
	theme.GetDimColorReturns("#808080")
	theme.GetAccentColorReturns("#FF00FF")

	autocomplete := autocomplete.NewAutocomplete(theme, mockRegistry)

	autocomplete.Update("/", 1)
	assert.True(t, autocomplete.IsVisible())

	selectedCmd := autocomplete.GetSelectedShortcut()
	assert.Equal(t, "/help", selectedCmd)

	autocomplete.Hide()
	assert.False(t, autocomplete.IsVisible())
}

func TestAutocomplete_ModelsMode(t *testing.T) {
	mockRegistry := &uimocks.FakeShortcutRegistry{}
	mockRegistry.GetAllReturns([]shortcuts.Shortcut{})

	mockModelService := &domainmocks.FakeModelService{}
	mockModelService.ListModelsReturns([]string{
		"deepseek-chat",
		"deepseek-reasoner",
		"claude-opus-4",
		"claude-sonnet-4",
		"gpt-4o",
	}, nil)

	theme := &uimocks.FakeTheme{}
	theme.GetDimColorReturns("#808080")
	theme.GetAccentColorReturns("#FF00FF")

	ac := autocomplete.NewAutocomplete(theme, mockRegistry)
	ac.SetModelService(mockModelService)

	tests := []struct {
		name            string
		input           string
		cursorPos       int
		expectedVisible bool
		expectedCount   int
	}{
		{
			name:            "Empty model prefix shows all models",
			input:           "/model ",
			cursorPos:       7,
			expectedVisible: true,
			expectedCount:   5,
		},
		{
			name:            "Partial model match - deepseek",
			input:           "/model deep",
			cursorPos:       11,
			expectedVisible: true,
			expectedCount:   2, // deepseek-chat and deepseek-reasoner
		},
		{
			name:            "Partial model match - claude",
			input:           "/model claude",
			cursorPos:       13,
			expectedVisible: true,
			expectedCount:   2, // claude-opus-4 and claude-sonnet-4
		},
		{
			name:            "No model match",
			input:           "/model xyz",
			cursorPos:       10,
			expectedVisible: false,
			expectedCount:   0,
		},
		{
			name:            "Not a model command - just /model",
			input:           "/model",
			cursorPos:       6,
			expectedVisible: false,
			expectedCount:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ac.Update(tt.input, tt.cursorPos)

			assert.Equal(t, tt.expectedVisible, ac.IsVisible(), "visibility mismatch")
		})
	}
}
