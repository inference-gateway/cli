package shortcuts

import (
	"testing"
)

func TestFillTemplate(t *testing.T) {
	tests := []struct {
		name     string
		template string
		data     map[string]string
		expected string
	}{
		{
			name:     "simple placeholder replacement",
			template: "Hello {name}, welcome to {place}!",
			data: map[string]string{
				"name":  "Alice",
				"place": "Wonderland",
			},
			expected: "Hello Alice, welcome to Wonderland!",
		},
		{
			name:     "LLM placeholder replacement",
			template: "## PR Plan\n\n{llm}\n\nReview this plan.",
			data: map[string]string{
				"llm": "**Branch:** feat/test\n**Commit:** feat: Add test",
			},
			expected: "## PR Plan\n\n**Branch:** feat/test\n**Commit:** feat: Add test\n\nReview this plan.",
		},
		{
			name:     "mixed placeholders",
			template: "Branch: {branch}\nDiff: {diff}\nPlan: {llm}",
			data: map[string]string{
				"branch": "main",
				"diff":   "some changes",
				"llm":    "generated plan",
			},
			expected: "Branch: main\nDiff: some changes\nPlan: generated plan",
		},
		{
			name:     "missing placeholder keeps original",
			template: "Hello {name}, from {unknown}",
			data: map[string]string{
				"name": "Bob",
			},
			expected: "Hello Bob, from {unknown}",
		},
		{
			name:     "empty data map",
			template: "No {replacements} here",
			data:     map[string]string{},
			expected: "No {replacements} here",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := fillTemplate(tt.template, tt.data)
			if result != tt.expected {
				t.Errorf("fillTemplate() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestFillTemplateMultiline(t *testing.T) {
	template := `## PR Plan

{llm}

**Next steps:** Review the plan for branch {branch}.`

	data := map[string]string{
		"llm": `**Branch:** feat/new-feature

**Commit:** feat: Add new feature

**PR Title:** Add new feature implementation

**PR Description:**
Implements the requested feature with proper error handling.`,
		"branch": "feat/new-feature",
	}

	result := fillTemplate(template, data)

	expected := `## PR Plan

**Branch:** feat/new-feature

**Commit:** feat: Add new feature

**PR Title:** Add new feature implementation

**PR Description:**
Implements the requested feature with proper error handling.

**Next steps:** Review the plan for branch feat/new-feature.`

	if result != expected {
		t.Errorf("Multiline template filling failed.\nGot:\n%s\n\nExpected:\n%s", result, expected)
	}
}
