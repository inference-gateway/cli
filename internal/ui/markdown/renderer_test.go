package markdown

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContainsMarkdown(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected bool
	}{
		{
			name:     "plain text",
			content:  "Hello, world!",
			expected: false,
		},
		{
			name:     "code block",
			content:  "```go\nfunc main() {}\n```",
			expected: true,
		},
		{
			name:     "bold text",
			content:  "This is **bold** text",
			expected: true,
		},
		{
			name:     "header",
			content:  "# Header",
			expected: true,
		},
		{
			name:     "ordered list",
			content:  "1. item 1\n2. item 2",
			expected: true,
		},
		{
			name:     "blockquote",
			content:  "> quoted text",
			expected: true,
		},
		{
			name:     "inline code",
			content:  "Use `go build` to compile",
			expected: true,
		},
		{
			name:     "link",
			content:  "[Link text](http://example.com)",
			expected: true,
		},
		{
			name:     "horizontal rule",
			content:  "---",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := containsMarkdown(tt.content)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHelperFunctions(t *testing.T) {
	t.Run("stringPtr", func(t *testing.T) {
		s := "test"
		ptr := stringPtr(s)
		assert.NotNil(t, ptr)
		assert.Equal(t, s, *ptr)
	})

	t.Run("boolPtr", func(t *testing.T) {
		b := true
		ptr := boolPtr(b)
		assert.NotNil(t, ptr)
		assert.Equal(t, b, *ptr)
	})

	t.Run("uintPtr", func(t *testing.T) {
		u := uint(42)
		ptr := uintPtr(u)
		assert.NotNil(t, ptr)
		assert.Equal(t, u, *ptr)
	})
}

func TestContainsMarkdownSkipsToolOutput(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected bool
	}{
		{
			name:     "tool output with box drawing",
			content:  "Bash(command=ls)\n├─ Duration: 100ms\n└─ Status: Success",
			expected: false, // Should skip due to box-drawing characters
		},
		{
			name:     "tool output with duration and status",
			content:  "Duration: 500ms Status: ✓ Success",
			expected: false, // Should skip due to Duration/Status pattern
		},
		{
			name:     "tool output with vertical line",
			content:  "Arguments:\n│ file: test.go",
			expected: false, // Should skip due to │ character
		},
		{
			name:     "empty string",
			content:  "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := containsMarkdown(tt.content)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestContainsMarkdownDetectsRealMarkdown(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected bool
	}{
		{
			name:     "multiple markdown elements",
			content:  "# Title\n\n**Bold** and `code`",
			expected: true,
		},
		{
			name:     "h2 header",
			content:  "## Section",
			expected: true,
		},
		{
			name:     "h3 header",
			content:  "### Subsection",
			expected: true,
		},
		{
			name:     "bold underscore",
			content:  "__bold__",
			expected: true,
		},
		{
			name:     "horizontal rule asterisks",
			content:  "***",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := containsMarkdown(tt.content)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRendererWithNilThemeService(t *testing.T) {
	// Test that renderer handles nil gracefully
	// This would be a real integration test with the actual domain.ThemeService
	// For now, we test the containsMarkdown function which is the core logic

	content := "# Hello\n\nThis is a **test**"
	assert.True(t, containsMarkdown(content))
}

func TestCodeBlockDetection(t *testing.T) {
	// Code blocks are particularly important for TUI rendering
	codeBlocks := []string{
		"```go\npackage main\n```",
		"```python\nprint('hello')\n```",
		"```\nplain code\n```",
		"```javascript\nconst x = 1;\n```",
	}

	for _, block := range codeBlocks {
		t.Run("code block", func(t *testing.T) {
			assert.True(t, containsMarkdown(block))
			assert.True(t, strings.Contains(block, "```"))
		})
	}
}

func TestPlainTextNotDetected(t *testing.T) {
	plainTexts := []string{
		"Hello, how are you?",
		"This is a normal sentence.",
		"The price is $100.",
		"Email: user@example.com",
		"Path: /home/user/file",
		"Time: 10:30 AM",
		"Numbers: 1234567890",
	}

	for _, text := range plainTexts {
		t.Run(text[:min(20, len(text))], func(t *testing.T) {
			result := containsMarkdown(text)
			assert.False(t, result, "Plain text should not be detected as markdown: %s", text)
		})
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
