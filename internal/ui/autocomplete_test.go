package ui

import (
	"context"
	"testing"

	domain "github.com/inference-gateway/cli/internal/domain"
	shortcuts "github.com/inference-gateway/cli/internal/shortcuts"
	sdk "github.com/inference-gateway/sdk"
	assert "github.com/stretchr/testify/assert"
	mock "github.com/stretchr/testify/mock"
)

// MockShortcutRegistry implements the ShortcutRegistry interface for testing
type MockShortcutRegistry struct {
	mock.Mock
}

func (m *MockShortcutRegistry) GetAll() []shortcuts.Shortcut {
	args := m.Called()
	return args.Get(0).([]shortcuts.Shortcut)
}

// MockShortcut implements the Shortcut interface for testing
type MockShortcut struct {
	name        string
	description string
}

func (m MockShortcut) GetName() string {
	return m.name
}

func (m MockShortcut) GetDescription() string {
	return m.description
}

func (m MockShortcut) GetUsage() string {
	return ""
}

func (m MockShortcut) Execute(ctx context.Context, args []string) (shortcuts.ShortcutResult, error) {
	return shortcuts.ShortcutResult{}, nil
}

func (m MockShortcut) CanExecute(args []string) bool {
	return true
}

// MockTheme implements the Theme interface for testing
type MockTheme struct{}

func (m MockTheme) GetUserColor() string       { return "#00FF00" }
func (m MockTheme) GetAssistantColor() string  { return "#0000FF" }
func (m MockTheme) GetErrorColor() string      { return "#FF0000" }
func (m MockTheme) GetSuccessColor() string    { return "#00FF00" }
func (m MockTheme) GetStatusColor() string     { return "#FFFF00" }
func (m MockTheme) GetAccentColor() string     { return "#FF00FF" }
func (m MockTheme) GetDimColor() string        { return "#808080" }
func (m MockTheme) GetBorderColor() string     { return "#FFFFFF" }
func (m MockTheme) GetDiffAddColor() string    { return "#00FF00" }
func (m MockTheme) GetDiffRemoveColor() string { return "#FF0000" }

// MockToolService implements the ToolService interface for testing
type MockToolService struct {
	availableTools []string
	tools          []sdk.ChatCompletionTool
}

func (m *MockToolService) ListAvailableTools() []string {
	return m.availableTools
}

func (m *MockToolService) ListTools() []sdk.ChatCompletionTool {
	return m.tools
}

func (m *MockToolService) ListAvailableToolsReturns(tools []string) {
	m.availableTools = tools
}

func (m *MockToolService) ListToolsReturns(tools []sdk.ChatCompletionTool) {
	m.tools = tools
}

// Add other required methods to satisfy the interface
func (m *MockToolService) GetTool(name string) (domain.Tool, error) { return nil, nil }
func (m *MockToolService) ExecuteTool(ctx context.Context, name string, input map[string]any) (*domain.ToolExecutionResult, error) {
	return nil, nil
}
func (m *MockToolService) IsToolEnabled(name string) bool                 { return true }
func (m *MockToolService) SetToolEnabled(name string, enabled bool) error { return nil }
func (m *MockToolService) ValidateToolsConfig() error                     { return nil }

func TestAutocomplete_CommandMode(t *testing.T) {
	mockRegistry := &MockShortcutRegistry{}
	mockRegistry.On("GetAll").Return([]shortcuts.Shortcut{
		MockShortcut{name: "help", description: "Show help"},
		MockShortcut{name: "clear", description: "Clear screen"},
		MockShortcut{name: "exit", description: "Exit application"},
	})

	theme := MockTheme{}
	autocomplete := NewAutocomplete(theme, mockRegistry)

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
			expectedCount:   1, // help
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
			if tt.expectedVisible {
				assert.Equal(t, tt.expectedCount, len(autocomplete.filtered))
			}
		})
	}

	mockRegistry.AssertExpectations(t)
}

func TestAutocomplete_ToolsMode(t *testing.T) {
	mockRegistry := &MockShortcutRegistry{}
	mockToolService := &MockToolService{}
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

	theme := MockTheme{}
	autocomplete := NewAutocomplete(theme, mockRegistry)
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
			expectedCount:   1, // Read
			expectedTools:   []string{"!!Read(file_path=\"\")"},
		},
		{
			name:            "Case insensitive tool match",
			input:           "!!web",
			cursorPos:       5,
			expectedVisible: true,
			expectedCount:   1, // WebSearch
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
			if tt.expectedVisible {
				assert.Equal(t, tt.expectedCount, len(autocomplete.filtered))

				// Check that the expected tools are present
				for i, expectedTool := range tt.expectedTools {
					if i < len(autocomplete.filtered) {
						assert.Equal(t, expectedTool, autocomplete.filtered[i].Shortcut)
					}
				}
			}
		})
	}

}

func TestAutocomplete_KeyHandling(t *testing.T) {
	mockRegistry := &MockShortcutRegistry{}
	mockRegistry.On("GetAll").Return([]shortcuts.Shortcut{
		MockShortcut{name: "help", description: "Show help"},
		MockShortcut{name: "clear", description: "Clear screen"},
	})

	theme := MockTheme{}
	autocomplete := NewAutocomplete(theme, mockRegistry)

	autocomplete.Update("/", 1)
	assert.True(t, autocomplete.IsVisible())
	assert.Equal(t, 0, autocomplete.selected)

	selectedCmd := autocomplete.GetSelectedShortcut()
	assert.Equal(t, "/help", selectedCmd)

	autocomplete.Hide()
	assert.False(t, autocomplete.IsVisible())

	mockRegistry.AssertExpectations(t)
}

func TestAutocomplete_FilterSuggestions(t *testing.T) {
	autocomplete := &AutocompleteImpl{}

	autocomplete.suggestions = []ShortcutOption{
		{Shortcut: "/help", Description: "Show help"},
		{Shortcut: "/clear", Description: "Clear screen"},
	}
	autocomplete.query = "he"
	autocomplete.filterSuggestions()

	assert.Equal(t, 1, len(autocomplete.filtered))
	assert.Equal(t, "/help", autocomplete.filtered[0].Shortcut)

	autocomplete.suggestions = []ShortcutOption{
		{Shortcut: "!!Read(", Description: "Execute Read tool directly"},
		{Shortcut: "!!WebSearch(", Description: "Execute WebSearch tool directly"},
	}
	autocomplete.query = "web"
	autocomplete.filterSuggestions()

	assert.Equal(t, 1, len(autocomplete.filtered))
	assert.Equal(t, "!!WebSearch(", autocomplete.filtered[0].Shortcut)
}
