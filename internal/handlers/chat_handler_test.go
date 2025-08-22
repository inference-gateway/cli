package handlers

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestChatHandler_extractMarkdownSummary_BasicCases(t *testing.T) {
	handler := &ChatHandler{}

	tests := []struct {
		name            string
		content         string
		expectedSummary string
		expectedFound   bool
	}{
		{
			name: "Basic summary section",
			content: `# Document Title

## Summary
This is a summary of the document.
It has multiple lines.

## Details
More content here.`,
			expectedSummary: `## Summary
This is a summary of the document.
It has multiple lines.
`,
			expectedFound: true,
		},
		{
			name: "Summary with next section",
			content: `## Summary
Brief overview of the project.
Key features included.

## Installation
Follow these steps...`,
			expectedSummary: `## Summary
Brief overview of the project.
Key features included.
`,
			expectedFound: true,
		},
		{
			name: "Summary with document separator",
			content: `## Summary
Project overview here.
Some bullet points.

---

More content after separator.`,
			expectedSummary: `## Summary
Project overview here.
Some bullet points.
`,
			expectedFound: true,
		},
		{
			name: "Summary at end of document",
			content: `# Main Title

## Summary
This is the final summary.
End of document.`,
			expectedSummary: `## Summary
This is the final summary.
End of document.
`,
			expectedFound: true,
		},
		{
			name: "No summary section",
			content: `# Document

## Introduction
Some content.

## Details
More content.`,
			expectedSummary: "",
			expectedFound:   false,
		},
		{
			name: "Empty summary section",
			content: `## Summary

## Next Section
Content here.`,
			expectedSummary: `## Summary
`,
			expectedFound: true,
		},
		{
			name: "Summary with only heading",
			content: `## Summary
## Next Section`,
			expectedSummary: "",
			expectedFound:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary, found := handler.extractMarkdownSummary(tt.content)

			assert.Equal(t, tt.expectedFound, found, "Found flag should match expected")
			if tt.expectedFound {
				assert.Equal(t, tt.expectedSummary, summary, "Summary content should match expected")
			} else {
				assert.Empty(t, summary, "Summary should be empty when not found")
			}
		})
	}
}

func TestChatHandler_extractMarkdownSummary_ComplexCases(t *testing.T) {
	handler := &ChatHandler{}

	tests := []struct {
		name            string
		content         string
		expectedSummary string
		expectedFound   bool
	}{
		{
			name: "Multiple summary sections (first one wins)",
			content: `## Summary
First summary content.

## Details
Some details.

## Summary
Second summary content.`,
			expectedSummary: `## Summary
First summary content.
`,
			expectedFound: true,
		},
		{
			name: "Summary with subsections",
			content: `## Summary
Main summary content.

### Key Points
- Point 1
- Point 2

## Next Section
Other content.`,
			expectedSummary: `## Summary
Main summary content.

### Key Points
- Point 1
- Point 2
`,
			expectedFound: true,
		},
		{
			name: "Summary with extra whitespace",
			content: `   ## Summary
Content with spaces.
More content.

  ## Next Section
Other stuff.`,
			expectedSummary: `   ## Summary
Content with spaces.
More content.
`,
			expectedFound: true,
		},
		{
			name: "Case sensitivity test",
			content: `## summary
Lowercase summary.

## Details
Content.`,
			expectedSummary: "",
			expectedFound:   false,
		},
		{
			name: "Summary with code blocks",
			content: `## Summary
This project includes:

` + "```go" + `
func main() {
    fmt.Println("Hello")
}
` + "```" + `

## Usage
Instructions here.`,
			expectedSummary: `## Summary
This project includes:

` + "```go" + `
func main() {
    fmt.Println("Hello")
}
` + "```" + `
`,
			expectedFound: true,
		},
		{
			name: "Summary with horizontal rule at end",
			content: `## Summary
Project summary here.
---`,
			expectedSummary: `## Summary
Project summary here.
`,
			expectedFound: true,
		},
		{
			name:            "Empty content",
			content:         "",
			expectedSummary: "",
			expectedFound:   false,
		},
		{
			name: "Only newlines",
			content: `


`,
			expectedSummary: "",
			expectedFound:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary, found := handler.extractMarkdownSummary(tt.content)

			assert.Equal(t, tt.expectedFound, found, "Found flag should match expected")
			if tt.expectedFound {
				assert.Equal(t, tt.expectedSummary, summary, "Summary content should match expected")
			} else {
				assert.Empty(t, summary, "Summary should be empty when not found")
			}
		})
	}
}

func TestChatHandler_extractMarkdownSummary_ExportFormat(t *testing.T) {
	handler := &ChatHandler{}

	tests := []struct {
		name            string
		content         string
		expectedSummary string
		expectedFound   bool
	}{
		{
			name: "Export file format with summary until separator",
			content: `# Chat Conversation Export

**Generated:** August 19, 2025 at 3:29 PM
**Total Messages:** 8

---

## Summary

**Conversation Summary:**

**Main Topics:**
- Introduction and availability for software engineering assistance

---

## Full Conversation

Message content here...`,
			expectedSummary: `## Summary

**Conversation Summary:**

**Main Topics:**
- Introduction and availability for software engineering assistance
`,
			expectedFound: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary, found := handler.extractMarkdownSummary(tt.content)

			assert.Equal(t, tt.expectedFound, found, "Found flag should match expected")
			if tt.expectedFound {
				assert.Equal(t, tt.expectedSummary, summary, "Summary content should match expected")
			} else {
				assert.Empty(t, summary, "Summary should be empty when not found")
			}
		})
	}
}

// Test edge cases and boundary conditions
func TestChatHandler_extractMarkdownSummary_EdgeCases(t *testing.T) {
	handler := &ChatHandler{}

	t.Run("Very large summary", func(t *testing.T) {
		largeContent := "## Summary\n"
		for i := range 1000 {
			largeContent += "This is line " + string(rune(i)) + " of the summary.\n"
		}
		largeContent += "\n## Next Section\nOther content."

		summary, found := handler.extractMarkdownSummary(largeContent)

		assert.True(t, found)
		assert.Contains(t, summary, "## Summary")
		assert.NotContains(t, summary, "## Next Section")
	})

	t.Run("Summary with special characters", func(t *testing.T) {
		content := `## Summary
Special chars: !@#$%^&*()
Unicode: 你好世界 🚀
Emojis work too! ✨

## Details
More content.`

		summary, found := handler.extractMarkdownSummary(content)

		assert.True(t, found)
		assert.Contains(t, summary, "Special chars: !@#$%^&*()")
		assert.Contains(t, summary, "Unicode: 你好世界 🚀")
		assert.Contains(t, summary, "Emojis work too! ✨")
		assert.NotContains(t, summary, "## Details")
	})

	t.Run("Mixed line endings", func(t *testing.T) {
		content := "## Summary\r\nWindows line ending content.\nUnix line ending.\r\n\r\n## Next Section\r\nMore content."

		summary, found := handler.extractMarkdownSummary(content)

		assert.True(t, found)
		assert.Contains(t, summary, "Windows line ending content.")
		assert.Contains(t, summary, "Unix line ending.")
	})
}

func TestChatHandler_parseToolCall(t *testing.T) {
	handler := &ChatHandler{}

	tests := []struct {
		name        string
		input       string
		expectTool  string
		expectArgs  map[string]any
		expectError bool
	}{
		{
			name:        "Simple tool call with single argument",
			input:       `Read(file_path="test.txt")`,
			expectTool:  "Read",
			expectArgs:  map[string]any{"file_path": "test.txt"},
			expectError: false,
		},
		{
			name:        "Tool call with multiple arguments",
			input:       `Write(file_path="output.txt", content="Hello World")`,
			expectTool:  "Write",
			expectArgs:  map[string]any{"file_path": "output.txt", "content": "Hello World"},
			expectError: false,
		},
		{
			name:        "Tool call with no arguments",
			input:       `Tree()`,
			expectTool:  "Tree",
			expectArgs:  map[string]any{},
			expectError: false,
		},
		{
			name:        "Tool call with single quoted arguments",
			input:       `Bash(command='ls -la')`,
			expectTool:  "Bash",
			expectArgs:  map[string]any{"command": "ls -la"},
			expectError: false,
		},
		{
			name:        "Tool call with mixed quotes",
			input:       `WebSearch(query="golang testing", max_results=10)`,
			expectTool:  "WebSearch",
			expectArgs:  map[string]any{"query": "golang testing", "max_results": float64(10)},
			expectError: false,
		},
		{
			name:        "Tool call with complex paths",
			input:       `Read(file_path="/home/user/Documents/file with spaces.txt")`,
			expectTool:  "Read",
			expectArgs:  map[string]any{"file_path": "/home/user/Documents/file with spaces.txt"},
			expectError: false,
		},
		{
			name:        "Missing opening parenthesis",
			input:       `ReadFile`,
			expectTool:  "",
			expectArgs:  nil,
			expectError: true,
		},
		{
			name:        "Missing closing parenthesis",
			input:       `Read(file_path="test.txt"`,
			expectTool:  "",
			expectArgs:  nil,
			expectError: true,
		},
		{
			name:        "Empty tool name",
			input:       `(file_path="test.txt")`,
			expectTool:  "",
			expectArgs:  nil,
			expectError: true,
		},
		{
			name:        "Tool call with spaces around tool name",
			input:       ` Write (file_path="test.txt")`,
			expectTool:  "Write",
			expectArgs:  map[string]any{"file_path": "test.txt"},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toolName, args, err := handler.parseToolCall(tt.input)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.expectTool, toolName)
			assert.Equal(t, tt.expectArgs, args)
		})
	}
}

func TestChatHandler_parseArguments(t *testing.T) {
	handler := &ChatHandler{}

	tests := []struct {
		name        string
		input       string
		expectArgs  map[string]any
		expectError bool
	}{
		{
			name:        "Single quoted argument",
			input:       `file_path="test.txt"`,
			expectArgs:  map[string]any{"file_path": "test.txt"},
			expectError: false,
		},
		{
			name:        "Multiple arguments",
			input:       `file_path="test.txt", content="Hello World"`,
			expectArgs:  map[string]any{"file_path": "test.txt", "content": "Hello World"},
			expectError: false,
		},
		{
			name:        "Single quoted arguments",
			input:       `command='ls -la'`,
			expectArgs:  map[string]any{"command": "ls -la"},
			expectError: false,
		},
		{
			name:        "Unquoted argument",
			input:       `count=10`,
			expectArgs:  map[string]any{"count": float64(10)},
			expectError: false,
		},
		{
			name:        "Quoted number argument",
			input:       `limit="51"`,
			expectArgs:  map[string]any{"limit": float64(51)},
			expectError: false,
		},
		{
			name:        "Empty string",
			input:       ``,
			expectArgs:  map[string]any{},
			expectError: false,
		},
		{
			name:        "Arguments with spaces",
			input:       `path="/home/user/file with spaces.txt", mode="read"`,
			expectArgs:  map[string]any{"path": "/home/user/file with spaces.txt", "mode": "read"},
			expectError: false,
		},
		{
			name:        "Arguments with special characters",
			input:       `pattern="[a-zA-Z0-9]+"`,
			expectArgs:  map[string]any{"pattern": "[a-zA-Z0-9]+"},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, err := handler.parseArguments(tt.input)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.expectArgs, args)
		})
	}
}
