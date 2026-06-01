package autocomplete_test

import (
	"testing"

	assert "github.com/stretchr/testify/assert"

	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"
	uimocks "github.com/inference-gateway/cli/tests/mocks/ui"

	tea "github.com/charmbracelet/bubbletea"

	sdk "github.com/inference-gateway/sdk"

	domain "github.com/inference-gateway/cli/internal/domain"
	shortcuts "github.com/inference-gateway/cli/internal/shortcuts"
	autocomplete "github.com/inference-gateway/cli/internal/ui/autocomplete"
	shortcutsmocks "github.com/inference-gateway/cli/tests/mocks/shortcuts"
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
		"deepseek-v4-pro",
		"deepseek-v4-flash",
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
			expectedCount:   2,
		},
		{
			name:            "Partial model match - claude",
			input:           "/model claude",
			cursorPos:       13,
			expectedVisible: true,
			expectedCount:   2,
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

func TestAutocomplete_IssueMode(t *testing.T) {
	mockRegistry := &uimocks.FakeShortcutRegistry{}
	mockRegistry.GetAllReturns([]shortcuts.Shortcut{})

	mockGH := &domainmocks.FakeGitHubIssueService{}
	mockGH.IsAvailableReturns(true)
	mockGH.ListIssuesReturns([]domain.GitHubIssue{
		{Number: 1, Title: "Add login flow"},
		{Number: 12, Title: "Fix auth bug"},
		{Number: 120, Title: "Add docs"},
		{Number: 573, Title: "Github issue autocomplete"},
	}, nil)

	theme := &uimocks.FakeTheme{}
	theme.GetDimColorReturns("#808080")
	theme.GetAccentColorReturns("#FF00FF")

	tests := []struct {
		name            string
		input           string
		cursorPos       int
		expectedVisible bool
		expectedCount   int
	}{
		{
			name: "empty query shows all issues", input: "#", cursorPos: 1,
			expectedVisible: true, expectedCount: 4,
		},
		{
			name: "numeric prefix filters by number", input: "#12", cursorPos: 3,
			expectedVisible: true, expectedCount: 2, // #12 and #120
		},
		{
			name: "non-numeric query filters by title substring", input: "#auth", cursorPos: 5,
			expectedVisible: true, expectedCount: 1,
		},
		{
			name: "no match hides dropdown", input: "#zzz", cursorPos: 4,
			expectedVisible: false, expectedCount: 0,
		},
		{
			name: "non-# prefix is not issues mode", input: "look at this", cursorPos: 12,
			expectedVisible: false, expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ac := autocomplete.NewAutocomplete(theme, mockRegistry)
			ac.SetGitHubIssueService(mockGH)
			ac.Update(tt.input, tt.cursorPos)
			assert.Equal(t, tt.expectedVisible, ac.IsVisible(), "visibility mismatch")
		})
	}
}

func TestAutocomplete_IssueMode_SelectionInsertsHashNumber(t *testing.T) {
	mockRegistry := &uimocks.FakeShortcutRegistry{}
	mockRegistry.GetAllReturns([]shortcuts.Shortcut{})

	mockGH := &domainmocks.FakeGitHubIssueService{}
	mockGH.IsAvailableReturns(true)
	mockGH.ListIssuesReturns([]domain.GitHubIssue{
		{Number: 573, Title: "Github issue autocomplete"},
	}, nil)

	theme := &uimocks.FakeTheme{}
	theme.GetDimColorReturns("#808080")
	theme.GetAccentColorReturns("#FF00FF")

	ac := autocomplete.NewAutocomplete(theme, mockRegistry)
	ac.SetGitHubIssueService(mockGH)
	ac.Update("#", 1)
	assert.True(t, ac.IsVisible())
	assert.Equal(t, "#573", ac.GetSelectedShortcut())
}

func TestAutocomplete_IssueMode_NoServiceShortCircuits(t *testing.T) {
	mockRegistry := &uimocks.FakeShortcutRegistry{}
	mockRegistry.GetAllReturns([]shortcuts.Shortcut{})

	theme := &uimocks.FakeTheme{}
	theme.GetDimColorReturns("#808080")
	theme.GetAccentColorReturns("#FF00FF")

	ac := autocomplete.NewAutocomplete(theme, mockRegistry)
	// no SetGitHubIssueService - simulates "gh not installed / no repo".
	ac.Update("#", 1)
	assert.False(t, ac.IsVisible())
}

func TestAutocomplete_IssueMode_UnavailableServiceHidesDropdown(t *testing.T) {
	mockRegistry := &uimocks.FakeShortcutRegistry{}
	mockRegistry.GetAllReturns([]shortcuts.Shortcut{})

	mockGH := &domainmocks.FakeGitHubIssueService{}
	mockGH.IsAvailableReturns(false)

	theme := &uimocks.FakeTheme{}
	theme.GetDimColorReturns("#808080")
	theme.GetAccentColorReturns("#FF00FF")

	ac := autocomplete.NewAutocomplete(theme, mockRegistry)
	ac.SetGitHubIssueService(mockGH)
	ac.Update("#", 1)
	assert.False(t, ac.IsVisible())
}

func TestAutocomplete_IssueMode_MidText(t *testing.T) {
	mockRegistry := &uimocks.FakeShortcutRegistry{}
	mockRegistry.GetAllReturns([]shortcuts.Shortcut{})

	mockGH := &domainmocks.FakeGitHubIssueService{}
	mockGH.IsAvailableReturns(true)
	mockGH.ListIssuesReturns([]domain.GitHubIssue{
		{Number: 573, Title: "Github issue autocomplete"},
	}, nil)

	theme := &uimocks.FakeTheme{}
	theme.GetDimColorReturns("#808080")
	theme.GetAccentColorReturns("#FF00FF")

	t.Run("triggers at cursor in mid-sentence", func(t *testing.T) {
		ac := autocomplete.NewAutocomplete(theme, mockRegistry)
		ac.SetGitHubIssueService(mockGH)
		input := "can you work on #"
		ac.Update(input, len(input))
		assert.True(t, ac.IsVisible(), "# at end of mid-sentence input should trigger")
	})

	t.Run("does not trigger when # is part of a non-whitespace token", func(t *testing.T) {
		ac := autocomplete.NewAutocomplete(theme, mockRegistry)
		ac.SetGitHubIssueService(mockGH)
		input := "abc#1"
		ac.Update(input, len(input))
		assert.False(t, ac.IsVisible(), "# preceded by non-whitespace must not trigger")
	})

	t.Run("does not trigger when whitespace sits between # and cursor", func(t *testing.T) {
		ac := autocomplete.NewAutocomplete(theme, mockRegistry)
		ac.SetGitHubIssueService(mockGH)
		input := "# something"
		ac.Update(input, len(input))
		assert.False(t, ac.IsVisible(), "whitespace breaks the trigger token")
	})

	t.Run("selection splices into the middle of a sentence", func(t *testing.T) {
		ac := autocomplete.NewAutocomplete(theme, mockRegistry)
		ac.SetGitHubIssueService(mockGH)
		input := "look at #"
		ac.Update(input, len(input))
		assert.True(t, ac.IsVisible())
		// Simulate enter-key selection by invoking handleSelection via HandleKey.
		handled, completion := ac.HandleKey(tea.KeyMsg{Type: tea.KeyEnter})
		assert.True(t, handled)
		assert.Equal(t, "look at #573 ", completion)
		// Caret should be right after "#573 " (position 13).
		assert.Equal(t, 13, ac.GetCompletionCursorPos())
	})

	t.Run("selection preserves trailing suffix without doubling spaces", func(t *testing.T) {
		ac := autocomplete.NewAutocomplete(theme, mockRegistry)
		ac.SetGitHubIssueService(mockGH)
		input := "look at #5 then something"
		// Cursor between "5" and " " - i.e. right after "#5".
		ac.Update(input, 10)
		assert.True(t, ac.IsVisible())
		handled, completion := ac.HandleKey(tea.KeyMsg{Type: tea.KeyEnter})
		assert.True(t, handled)
		// Splice should yield "look at #573 then something" - no double space
		// because the suffix already starts with one.
		assert.Equal(t, "look at #573 then something", completion)
	})
}

func TestAutocomplete_SkillsMidText(t *testing.T) {
	// A registered shortcut and a skill with similar names - mid-text must
	// show ONLY the skill, never the shortcut. Shortcuts are commands meant
	// for start-of-input.
	fakeShortcut := &shortcutsmocks.FakeShortcut{}
	fakeShortcut.GetNameReturns("clear")
	fakeShortcut.GetDescriptionReturns("Clear screen")

	mockRegistry := &uimocks.FakeShortcutRegistry{}
	mockRegistry.GetAllReturns([]shortcuts.Shortcut{fakeShortcut})

	skillsSvc := &domainmocks.FakeSkillsService{}
	skillsSvc.ListReturns([]domain.Skill{
		{Name: "maintainer", Description: "Maintain the org"},
	})

	theme := &uimocks.FakeTheme{}
	theme.GetDimColorReturns("#808080")
	theme.GetAccentColorReturns("#FF00FF")

	t.Run("mid-text shows only skills, not shortcuts", func(t *testing.T) {
		ac := autocomplete.NewAutocomplete(theme, mockRegistry)
		ac.SetSkillsService(skillsSvc)
		input := "use /"
		ac.Update(input, len(input))
		assert.True(t, ac.IsVisible(), "mid-sentence /<query> should trigger")
		// Only the skill should match - "clear" shortcut must be filtered out.
		assert.Equal(t, "/maintainer", ac.GetSelectedShortcut(),
			"mid-text dropdown must only contain skills")
	})

	t.Run("mid-text hides when no skills match the query", func(t *testing.T) {
		ac := autocomplete.NewAutocomplete(theme, mockRegistry)
		ac.SetSkillsService(skillsSvc)
		// "clear" matches the shortcut but NOT the skill, so dropdown must hide.
		input := "use /clear"
		ac.Update(input, len(input))
		assert.False(t, ac.IsVisible(),
			"mid-text query that only matches shortcuts must not show dropdown")
	})

	t.Run("start-of-input / still shows shortcuts via existing path", func(t *testing.T) {
		ac := autocomplete.NewAutocomplete(theme, mockRegistry)
		ac.SetSkillsService(skillsSvc)
		// At position 0 the existing shortcuts mode handles it - the dropdown
		// includes both the shortcut and the skill. TestAutocomplete_CommandMode
		// covers full behaviour; here we just assert it stays visible.
		ac.Update("/", 1)
		assert.True(t, ac.IsVisible())
	})

	t.Run("selection splices /skill without executing", func(t *testing.T) {
		ac := autocomplete.NewAutocomplete(theme, mockRegistry)
		ac.SetSkillsService(skillsSvc)
		input := "use /mai"
		ac.Update(input, len(input))
		assert.True(t, ac.IsVisible())
		handled, completion := ac.HandleKey(tea.KeyMsg{Type: tea.KeyEnter})
		assert.True(t, handled)
		assert.Equal(t, "use /maintainer ", completion)
		assert.False(t, ac.ShouldExecuteImmediately(),
			"mid-text skill completion must NEVER execute on selection")
	})
}
