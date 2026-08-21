package directexec

import (
	"testing"

	assert "github.com/stretchr/testify/assert"
)

func TestService_ParseToolCall(t *testing.T) {
	svc := &Service{}

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
			name:        "Tool call with numeric arguments",
			input:       `Search(query="test", limit=10)`,
			expectTool:  "Search",
			expectArgs:  map[string]any{"query": "test", "limit": float64(10)},
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
			toolName, args, err := svc.ParseToolCall(tt.input)
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

func TestService_ParseArguments(t *testing.T) {
	svc := &Service{}

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
			args, err := svc.ParseArguments(tt.input)
			if tt.expectError {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.expectArgs, args)
		})
	}
}
