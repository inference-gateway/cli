package autocomplete_test

import (
	"testing"

	assert "github.com/stretchr/testify/assert"

	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"
	uimocks "github.com/inference-gateway/cli/tests/mocks/ui"

	tea "charm.land/bubbletea/v2"

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

	// loadTools now sources the !! suggestions from ListToolsForMode (the same
	// mode-aware gating the agent uses for the LLM).
	mockToolService.ListToolsForModeReturns([]sdk.ChatCompletionTool{
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
		{
			Type:     sdk.Function,
			Function: sdk.FunctionObject{Name: "WebSearch"},
		},
		{
			Type:     sdk.Function,
			Function: sdk.FunctionObject{Name: "Tree"},
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

func TestAutocomplete_ToolsRespectAgentMode(t *testing.T) {
	mockRegistry := &uimocks.FakeShortcutRegistry{}
	mockRegistry.GetAllReturns([]shortcuts.Shortcut{})

	mockToolService := &domainmocks.FakeToolService{}
	mockToolService.ListToolsForModeStub = func(mode domain.AgentMode) []sdk.ChatCompletionTool {
		if mode == domain.AgentModePlan {
			return []sdk.ChatCompletionTool{
				{Type: sdk.Function, Function: sdk.FunctionObject{Name: "AskUserQuestion"}},
			}
		}
		return []sdk.ChatCompletionTool{
			{Type: sdk.Function, Function: sdk.FunctionObject{Name: "Bash"}},
		}
	}

	sm := &domainmocks.FakeStateManager{}

	theme := &uimocks.FakeTheme{}
	theme.GetDimColorReturns("#808080")
	theme.GetAccentColorReturns("#FF00FF")

	ac := autocomplete.NewAutocomplete(theme, mockRegistry)
	ac.SetToolService(mockToolService)
	ac.SetStateManager(sm)

	// Standard mode: AskUserQuestion is plan-only, so it must not autocomplete.
	sm.GetAgentModeReturns(domain.AgentModeStandard)
	ac.Update("!!AskUser", 9)
	if ac.IsVisible() {
		t.Error("AskUserQuestion should not autocomplete in standard mode")
	}

	// Plan mode: it appears.
	sm.GetAgentModeReturns(domain.AgentModePlan)
	ac.Update("!!AskUser", 9)
	if !ac.IsVisible() {
		t.Error("AskUserQuestion should autocomplete in plan mode")
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
		handled, completion := ac.HandleKey(tea.KeyPressMsg{Code: tea.KeyTab})
		assert.True(t, handled)
		assert.Equal(t, "look at #573 ", completion)
		assert.Equal(t, 13, ac.GetCompletionCursorPos())
	})

	t.Run("selection preserves trailing suffix without doubling spaces", func(t *testing.T) {
		ac := autocomplete.NewAutocomplete(theme, mockRegistry)
		ac.SetGitHubIssueService(mockGH)
		input := "look at #5 then something"
		ac.Update(input, 10)
		assert.True(t, ac.IsVisible())
		handled, completion := ac.HandleKey(tea.KeyPressMsg{Code: tea.KeyTab})
		assert.True(t, handled)
		assert.Equal(t, "look at #573 then something", completion)
	})
}

func TestAutocomplete_SkillsMidText(t *testing.T) {
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
		assert.Equal(t, "/maintainer", ac.GetSelectedShortcut(),
			"mid-text dropdown must only contain skills")
	})

	t.Run("mid-text hides when no skills match the query", func(t *testing.T) {
		ac := autocomplete.NewAutocomplete(theme, mockRegistry)
		ac.SetSkillsService(skillsSvc)
		input := "use /clear"
		ac.Update(input, len(input))
		assert.False(t, ac.IsVisible(),
			"mid-text query that only matches shortcuts must not show dropdown")
	})

	t.Run("start-of-input / still shows shortcuts via existing path", func(t *testing.T) {
		ac := autocomplete.NewAutocomplete(theme, mockRegistry)
		ac.SetSkillsService(skillsSvc)
		ac.Update("/", 1)
		assert.True(t, ac.IsVisible())
	})

	t.Run("selection splices /skill without executing", func(t *testing.T) {
		ac := autocomplete.NewAutocomplete(theme, mockRegistry)
		ac.SetSkillsService(skillsSvc)
		input := "use /mai"
		ac.Update(input, len(input))
		assert.True(t, ac.IsVisible())
		handled, completion := ac.HandleKey(tea.KeyPressMsg{Code: tea.KeyTab})
		assert.True(t, handled)
		assert.Equal(t, "use /maintainer ", completion)
	})
}

// TestAutocomplete_ToolsAllOptionalSchema covers the regression in issue #690:
// a tool whose useful arguments are not top-level "required" (a one-of /
// all-optional schema like the Agent tool) must still surface its arguments in
// the !! skeleton and the dropdown description, rather than showing a bare
// "Tool()" with a generic "Execute <tool> tool directly" line.
func TestAutocomplete_ToolsAllOptionalSchema(t *testing.T) {
	mockRegistry := &uimocks.FakeShortcutRegistry{}
	mockRegistry.GetAllReturns([]shortcuts.Shortcut{})

	mockToolService := &domainmocks.FakeToolService{}

	agentDesc := "Spawn local subagents in parallel"

	agentParams := sdk.FunctionParameters(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"tasks": map[string]any{
				"type":        "array",
				"description": "Subagent tasks to run in parallel",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"description": map[string]any{"type": "string"},
					},
					"required": []string{"description"},
				},
			},
			"description": map[string]any{
				"type":        "string",
				"description": "Shorthand for a single subagent task",
			},
			"system_prompt": map[string]any{
				"type":        "string",
				"description": "Optional system prompt for the single-task form",
			},
			"type": map[string]any{
				"type":        "string",
				"enum":        []string{"ReadOnly", "ReadWrite"},
				"description": "Capability for the single-task form",
			},
		},
	})

	mockToolService.ListToolsForModeReturns([]sdk.ChatCompletionTool{
		{
			Type: sdk.Function,
			Function: sdk.FunctionObject{
				Name:        "Agent",
				Description: &agentDesc,
				Parameters:  &agentParams,
			},
		},
	})

	theme := &uimocks.FakeTheme{}
	theme.GetDimColorReturns("#808080")
	theme.GetAccentColorReturns("#FF00FF")

	ac := autocomplete.NewAutocomplete(theme, mockRegistry)
	ac.SetToolService(mockToolService)
	ac.SetWidth(240)

	t.Run("skeleton includes all top-level properties with type-appropriate defaults", func(t *testing.T) {
		ac.Update("!!Age", 5)
		assert.True(t, ac.IsVisible(), "!!Age should match the Agent tool")
		assert.Equal(t, `!!Agent(description="", system_prompt="", tasks=[], type="")`,
			ac.GetSelectedShortcut(),
			"all-optional schema must fill the skeleton with its top-level properties, not empty parens")
	})

	t.Run("dropdown description lists the parameters with type and required/optional", func(t *testing.T) {
		ac.Update("!!Age", 5)
		assert.True(t, ac.IsVisible())
		rendered := ac.Render()
		assert.Contains(t, rendered, "args:",
			"description should be enriched with a parameter list")
		assert.Contains(t, rendered, "description (string, optional)")
		assert.Contains(t, rendered, "system_prompt (string, optional)")
		assert.Contains(t, rendered, "tasks (array, optional)")
		assert.Contains(t, rendered, "type (string, optional)")
	})

	t.Run("empty !! prefix still shows the enriched Agent suggestion", func(t *testing.T) {
		ac.Update("!!", 2)
		assert.True(t, ac.IsVisible())
		assert.Equal(t, `!!Agent(description="", system_prompt="", tasks=[], type="")`,
			ac.GetSelectedShortcut())
	})
}

func TestAutocomplete_TabCompletesWithoutSubmitting(t *testing.T) {
	noArgShortcut := &shortcutsmocks.FakeShortcut{}
	noArgShortcut.GetNameReturns("clear")
	noArgShortcut.GetDescriptionReturns("Clear screen")

	mockRegistry := &uimocks.FakeShortcutRegistry{}
	mockRegistry.GetAllReturns([]shortcuts.Shortcut{noArgShortcut})

	theme := &uimocks.FakeTheme{}
	theme.GetDimColorReturns("#808080")
	theme.GetAccentColorReturns("#FF00FF")

	t.Run("no-arg shortcut completes with trailing space, no auto-submit", func(t *testing.T) {
		ac := autocomplete.NewAutocomplete(theme, mockRegistry)
		ac.Update("/cle", 4)
		assert.True(t, ac.IsVisible())

		handled, completion := ac.HandleKey(tea.KeyPressMsg{Code: tea.KeyTab})
		assert.True(t, handled)
		assert.Equal(t, "/clear ", completion,
			"Tab must complete the shortcut text, not signal submission")
	})

	t.Run("completed shortcut does not re-open the popup", func(t *testing.T) {
		ac := autocomplete.NewAutocomplete(theme, mockRegistry)
		ac.Update("/cle", 4)
		_, completion := ac.HandleKey(tea.KeyPressMsg{Code: tea.KeyTab})

		ac.Update(completion, len(completion))
		assert.False(t, ac.IsVisible(),
			"a fully completed no-arg shortcut must not re-show the dropdown")
	})
}
