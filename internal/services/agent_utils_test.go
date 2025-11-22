package services

import (
	"testing"
)

func TestIsCompleteJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "valid simple object",
			input:    `{"key": "value"}`,
			expected: true,
		},
		{
			name:     "valid object with nested content",
			input:    `{"file_path": "/test/file.txt", "content": "hello world"}`,
			expected: true,
		},
		{
			name:     "valid array",
			input:    `["item1", "item2"]`,
			expected: true,
		},
		{
			name:     "valid object with whitespace",
			input:    `  {"key": "value"}  `,
			expected: true,
		},
		{
			name:     "valid number",
			input:    `42`,
			expected: true,
		},
		{
			name:     "valid string",
			input:    `"hello"`,
			expected: true,
		},
		{
			name:     "valid boolean",
			input:    `true`,
			expected: true,
		},
		{
			name:     "valid null",
			input:    `null`,
			expected: true,
		},
		{
			name:     "incomplete object - missing closing brace",
			input:    `{"key": "value"`,
			expected: false,
		},
		{
			name:     "incomplete object - truncated string",
			input:    `{"file_path": "/test/file.txt", "content": "hello wo`,
			expected: false,
		},
		{
			name:     "incomplete array - missing closing bracket",
			input:    `["item1", "item2"`,
			expected: false,
		},
		{
			name:     "incomplete nested object",
			input:    `{"outer": {"inner": "value"`,
			expected: false,
		},
		{
			name:     "empty string",
			input:    ``,
			expected: false,
		},
		{
			name:     "whitespace only",
			input:    `   `,
			expected: false,
		},
		{
			name:     "incomplete - just opening brace",
			input:    `{`,
			expected: false,
		},
		{
			name:     "incomplete - truncated key",
			input:    `{"file_pa`,
			expected: false,
		},
		{
			name:     "incomplete - missing value",
			input:    `{"key":`,
			expected: false,
		},
		{
			name:     "malformed JSON",
			input:    `{key: "value"}`,
			expected: false,
		},
		{
			name:     "valid complex object with multiline content",
			input:    `{"file_path": "/test.txt", "content": "line1\nline2\nline3"}`,
			expected: true,
		},
		{
			name:     "incomplete with escaped quotes",
			input:    `{"content": "hello \"world`,
			expected: false,
		},
		{
			name:     "valid with escaped quotes",
			input:    `{"content": "hello \"world\""}`,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isCompleteJSON(tt.input)
			if result != tt.expected {
				t.Errorf("isCompleteJSON(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{
			name:     "string shorter than maxLen",
			input:    "hello",
			maxLen:   10,
			expected: "hello",
		},
		{
			name:     "string equal to maxLen",
			input:    "hello",
			maxLen:   5,
			expected: "hello",
		},
		{
			name:     "string longer than maxLen",
			input:    "hello world",
			maxLen:   8,
			expected: "hello...",
		},
		{
			name:     "very long string",
			input:    "this is a very long string that should be truncated",
			maxLen:   20,
			expected: "this is a very lo...",
		},
		{
			name:     "maxLen of 3",
			input:    "hello",
			maxLen:   3,
			expected: "hel",
		},
		{
			name:     "maxLen of 4",
			input:    "hello",
			maxLen:   4,
			expected: "h...",
		},
		{
			name:     "maxLen of 2",
			input:    "hello",
			maxLen:   2,
			expected: "he",
		},
		{
			name:     "maxLen of 1",
			input:    "hello",
			maxLen:   1,
			expected: "h",
		},
		{
			name:     "empty string",
			input:    "",
			maxLen:   10,
			expected: "",
		},
		{
			name:     "maxLen of 0",
			input:    "hello",
			maxLen:   0,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateString(tt.input, tt.maxLen)
			if result != tt.expected {
				t.Errorf("truncateString(%q, %d) = %q, want %q", tt.input, tt.maxLen, result, tt.expected)
			}
		})
	}
}

// TestIsCompleteJSON_LargeFileSimulation tests the scenario where an LLM
// hits output token limits while generating large file content
func TestIsCompleteJSON_LargeFileSimulation(t *testing.T) {
	// Simulate a truncated Write tool call that would occur when
	// DeepSeek or another LLM hits output token limits
	incompleteWriteCall := `{"file_path": "/home/user/project/src/components/MyComponent.tsx", "content": "import React from 'react';\n\nexport const MyComponent: React.FC = () => {\n  const [state, setState] = React.useState<string>('');\n\n  return (\n    <div className=\"container\">\n      <h1>My Component</h1>\n      <p>This is a test component that demonstrates`

	if isCompleteJSON(incompleteWriteCall) {
		t.Error("Expected incomplete JSON to return false - this simulates the DeepSeek token limit issue")
	}

	// A complete version should pass
	completeWriteCall := `{"file_path": "/home/user/project/src/components/MyComponent.tsx", "content": "import React from 'react';\n\nexport const MyComponent: React.FC = () => {\n  return <div>Hello</div>;\n};\n"}`

	if !isCompleteJSON(completeWriteCall) {
		t.Error("Expected complete JSON to return true")
	}
}
