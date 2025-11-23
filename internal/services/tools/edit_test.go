package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
)

// MockReadToolTracker for testing read tool usage tracking
type MockReadToolTracker struct {
	readToolUsed bool
}

func (m *MockReadToolTracker) IsReadToolUsed() bool {
	return m.readToolUsed
}

func (m *MockReadToolTracker) SetReadToolUsed() {
	m.readToolUsed = true
}

func TestEditTool_Definition(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Sandbox: config.SandboxConfig{
				Directories: []string{"."},
			},
			Edit: config.EditToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewEditTool(cfg)
	def := tool.Definition()

	if def.Function.Name != "Edit" {
		t.Errorf("Expected tool name 'Edit', got %s", def.Function.Name)
	}

	if *def.Function.Description == "" {
		t.Error("Tool description should not be empty")
	}

	if def.Function.Parameters == nil {
		t.Error("Tool parameters should not be nil")
	}

}

func TestEditTool_IsEnabled(t *testing.T) {
	tests := []struct {
		name          string
		toolsEnabled  bool
		editEnabled   bool
		expectedState bool
	}{
		{
			name:          "enabled when both tools and edit enabled",
			toolsEnabled:  true,
			editEnabled:   true,
			expectedState: true,
		},
		{
			name:          "disabled when tools disabled",
			toolsEnabled:  false,
			editEnabled:   true,
			expectedState: false,
		},
		{
			name:          "disabled when edit disabled",
			toolsEnabled:  true,
			editEnabled:   false,
			expectedState: false,
		},
		{
			name:          "disabled when both disabled",
			toolsEnabled:  false,
			editEnabled:   false,
			expectedState: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Tools: config.ToolsConfig{
					Enabled: tt.toolsEnabled,
					Edit: config.EditToolConfig{
						Enabled: tt.editEnabled,
					},
				},
			}

			tool := NewEditTool(cfg)
			if tool.IsEnabled() != tt.expectedState {
				t.Errorf("Expected IsEnabled() = %v, got %v", tt.expectedState, tool.IsEnabled())
			}
		})
	}
}

func TestEditTool_Validate(t *testing.T) {
	cfg := getTestConfig()
	tests := getValidationTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockTracker := &MockReadToolTracker{readToolUsed: tt.readUsed}
			tool := NewEditToolWithRegistry(cfg, mockTracker)

			err := tool.Validate(tt.args)
			checkValidationResult(t, err, tt.wantError, tt.errorMessage)
		})
	}
}

func getTestConfig() *config.Config {
	return &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Sandbox: config.SandboxConfig{
				Directories: []string{"."},
				ProtectedPaths: []string{
					".infer/",
					".git/",
					"*.env",
					"*.env.database",
				},
			},
			Edit: config.EditToolConfig{
				Enabled: true,
			},
		},
	}
}

func getValidationTests() []struct {
	name         string
	readUsed     bool
	args         map[string]any
	wantError    bool
	errorMessage string
} {
	tests := []struct {
		name         string
		readUsed     bool
		args         map[string]any
		wantError    bool
		errorMessage string
	}{}

	tests = append(tests, getValidSuccessTests()...)
	tests = append(tests, getValidFailureTests()...)
	tests = append(tests, getValidSecurityTests()...)

	return tests
}

func getValidSuccessTests() []struct {
	name         string
	readUsed     bool
	args         map[string]any
	wantError    bool
	errorMessage string
} {
	return []struct {
		name         string
		readUsed     bool
		args         map[string]any
		wantError    bool
		errorMessage string
	}{
		{
			name:     "valid arguments with read tool used",
			readUsed: true,
			args: map[string]any{
				"file_path":  "test.txt",
				"old_string": "old",
				"new_string": "new",
			},
			wantError: false,
		},
		{
			name:     "valid arguments with replace_all",
			readUsed: true,
			args: map[string]any{
				"file_path":   "test.txt",
				"old_string":  "old",
				"new_string":  "new",
				"replace_all": true,
			},
			wantError: false,
		},
	}
}

func getValidFailureTests() []struct {
	name         string
	readUsed     bool
	args         map[string]any
	wantError    bool
	errorMessage string
} {
	return []struct {
		name         string
		readUsed     bool
		args         map[string]any
		wantError    bool
		errorMessage string
	}{
		{
			name:     "read tool not used",
			readUsed: false,
			args: map[string]any{
				"file_path":  "test.txt",
				"old_string": "old",
				"new_string": "new",
			},
			wantError:    true,
			errorMessage: "edit tool requires that the Read tool has been used at least once in the conversation before editing files",
		},
		{
			name:     "missing file_path",
			readUsed: true,
			args: map[string]any{
				"old_string": "old",
				"new_string": "new",
			},
			wantError:    true,
			errorMessage: "file_path parameter is required and must be a string",
		},
		{
			name:     "empty file_path",
			readUsed: true,
			args: map[string]any{
				"file_path":  "",
				"old_string": "old",
				"new_string": "new",
			},
			wantError:    true,
			errorMessage: "file_path cannot be empty",
		},
		{
			name:     "file_path wrong type",
			readUsed: true,
			args: map[string]any{
				"file_path":  123,
				"old_string": "old",
				"new_string": "new",
			},
			wantError:    true,
			errorMessage: "file_path parameter is required and must be a string",
		},
		{
			name:     "missing old_string",
			readUsed: true,
			args: map[string]any{
				"file_path":  "test.txt",
				"new_string": "new",
			},
			wantError:    true,
			errorMessage: "old_string parameter is required and must be a string",
		},
		{
			name:     "empty old_string",
			readUsed: true,
			args: map[string]any{
				"file_path":  "test.txt",
				"old_string": "",
				"new_string": "new",
			},
			wantError:    true,
			errorMessage: "old_string cannot be empty",
		},
		{
			name:     "old_string wrong type",
			readUsed: true,
			args: map[string]any{
				"file_path":  "test.txt",
				"old_string": 123,
				"new_string": "new",
			},
			wantError:    true,
			errorMessage: "old_string parameter is required and must be a string",
		},
		{
			name:     "missing new_string",
			readUsed: true,
			args: map[string]any{
				"file_path":  "test.txt",
				"old_string": "old",
			},
			wantError:    true,
			errorMessage: "new_string parameter is required and must be a string",
		},
		{
			name:     "new_string wrong type",
			readUsed: true,
			args: map[string]any{
				"file_path":  "test.txt",
				"old_string": "old",
				"new_string": 123,
			},
			wantError:    true,
			errorMessage: "new_string parameter is required and must be a string",
		},
		{
			name:     "old_string equals new_string",
			readUsed: true,
			args: map[string]any{
				"file_path":  "test.txt",
				"old_string": "same",
				"new_string": "same",
			},
			wantError:    true,
			errorMessage: "new_string must be different from old_string",
		},
		{
			name:     "replace_all wrong type",
			readUsed: true,
			args: map[string]any{
				"file_path":   "test.txt",
				"old_string":  "old",
				"new_string":  "new",
				"replace_all": "true",
			},
			wantError:    true,
			errorMessage: "replace_all parameter must be a boolean",
		},
	}
}

func getValidSecurityTests() []struct {
	name         string
	readUsed     bool
	args         map[string]any
	wantError    bool
	errorMessage string
} {
	return []struct {
		name         string
		readUsed     bool
		args         map[string]any
		wantError    bool
		errorMessage string
	}{
		{
			name:     "excluded path",
			readUsed: true,
			args: map[string]any{
				"file_path":  config.DefaultConfigPath,
				"old_string": "old",
				"new_string": "new",
			},
			wantError:    true,
			errorMessage: fmt.Sprintf("access to path '%s' is excluded for security", config.DefaultConfigPath),
		},
		{
			name:     "excluded pattern",
			readUsed: true,
			args: map[string]any{
				"file_path":  ".env.database",
				"old_string": "old",
				"new_string": "new",
			},
			wantError:    true,
			errorMessage: "access to path '.env.database' is excluded for security",
		},
	}
}

func checkValidationResult(t *testing.T, err error, wantError bool, errorMessage string) {
	if wantError {
		if err == nil {
			t.Errorf("Expected error but got none")
			return
		}
		if errorMessage != "" && err.Error() != errorMessage {
			t.Errorf("Expected error message '%s', got '%s'", errorMessage, err.Error())
		}
		return
	}
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestEditTool_Execute_ReadToolNotUsed(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Sandbox: config.SandboxConfig{
				Directories: []string{"."},
			},
			Edit: config.EditToolConfig{
				Enabled: true,
			},
		},
	}

	mockTracker := &MockReadToolTracker{readToolUsed: false}
	tool := NewEditToolWithRegistry(cfg, mockTracker)

	args := map[string]any{
		"file_path":  "test.txt",
		"old_string": "old",
		"new_string": "new",
	}

	result, err := tool.Execute(context.Background(), args)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if result == nil {
		t.Fatal("Expected result, got nil")
	}

	if result.Success {
		t.Error("Expected execution to fail when Read tool not used")
	}

	expectedError := "Edit tool requires that the Read tool has been used at least once in the conversation before editing files"
	if result.Error != expectedError {
		t.Errorf("Expected error '%s', got '%s'", expectedError, result.Error)
	}
}

func TestEditTool_Execute_Success(t *testing.T) {
	// Create temporary test file
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.txt")
	originalContent := "Hello world\nThis is a test\nHello again"

	err := os.WriteFile(testFile, []byte(originalContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Sandbox: config.SandboxConfig{
				Directories: []string{tempDir},
			},
			Edit: config.EditToolConfig{
				Enabled: true,
			},
		},
	}

	tests := []struct {
		name             string
		args             map[string]any
		expectedContent  string
		expectedReplaced int
		expectedModified bool
	}{
		{
			name: "single replacement",
			args: map[string]any{
				"file_path":  testFile,
				"old_string": "Hello world",
				"new_string": "Hi universe",
			},
			expectedContent:  "Hi universe\nThis is a test\nHello again",
			expectedReplaced: 1,
			expectedModified: true,
		},
		{
			name: "replace all",
			args: map[string]any{
				"file_path":   testFile,
				"old_string":  "Hello",
				"new_string":  "Hi",
				"replace_all": true,
			},
			expectedContent:  "Hi world\nThis is a test\nHi again",
			expectedReplaced: 2,
			expectedModified: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset file content
			err := os.WriteFile(testFile, []byte(originalContent), 0644)
			if err != nil {
				t.Fatalf("Failed to reset test file: %v", err)
			}

			mockTracker := &MockReadToolTracker{readToolUsed: true}
			tool := NewEditToolWithRegistry(cfg, mockTracker)

			result, err := tool.Execute(context.Background(), tt.args)

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if result == nil {
				t.Fatal("Expected result, got nil")
			}

			if !result.Success {
				t.Errorf("Expected successful execution, got error: %s", result.Error)
				return
			}

			// Check result data
			editResult, ok := result.Data.(*domain.EditToolResult)
			if !ok {
				t.Fatal("Expected EditToolResult in result data")
			}

			if editResult.ReplacedCount != tt.expectedReplaced {
				t.Errorf("Expected %d replacements, got %d", tt.expectedReplaced, editResult.ReplacedCount)
			}

			if editResult.FileModified != tt.expectedModified {
				t.Errorf("Expected FileModified = %v, got %v", tt.expectedModified, editResult.FileModified)
			}

			// Check file content
			if tt.expectedModified {
				content, err := os.ReadFile(testFile)
				if err != nil {
					t.Fatalf("Failed to read modified file: %v", err)
				}

				if string(content) != tt.expectedContent {
					t.Errorf("Expected file content '%s', got '%s'", tt.expectedContent, string(content))
				}
			}
		})
	}
}

func TestEditTool_Execute_Errors(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.txt")
	originalContent := "Hello world\nHello world\nThis is a test"

	err := os.WriteFile(testFile, []byte(originalContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Sandbox: config.SandboxConfig{
				Directories: []string{tempDir},
			},
			Edit: config.EditToolConfig{
				Enabled: true,
			},
		},
	}

	mockTracker := &MockReadToolTracker{readToolUsed: true}
	tool := NewEditToolWithRegistry(cfg, mockTracker)

	tests := []struct {
		name                   string
		args                   map[string]any
		expectedErrorSubstring string
	}{
		{
			name: "file does not exist",
			args: map[string]any{
				"file_path":  filepath.Join(tempDir, "nonexistent.txt"),
				"old_string": "old",
				"new_string": "new",
			},
			expectedErrorSubstring: "does not exist. Edit tool only works with existing files",
		},
		{
			name: "directory instead of file",
			args: map[string]any{
				"file_path":  tempDir,
				"old_string": "old",
				"new_string": "new",
			},
			expectedErrorSubstring: "is a directory, not a file",
		},
		{
			name: "old_string not found",
			args: map[string]any{
				"file_path":  testFile,
				"old_string": "nonexistent",
				"new_string": "new",
			},
			expectedErrorSubstring: "old_string not found in file",
		},
		{
			name: "old_string not unique without replace_all",
			args: map[string]any{
				"file_path":  testFile,
				"old_string": "Hello world",
				"new_string": "Hi universe",
			},
			expectedErrorSubstring: "old_string 'Hello world' is not unique in file",
		},
		{
			name: "old_string equals new_string",
			args: map[string]any{
				"file_path":  testFile,
				"old_string": "Hello world",
				"new_string": "Hello world",
			},
			expectedErrorSubstring: "new_string must be different from old_string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tool.Execute(context.Background(), tt.args)

			checkExecuteErrorResult(t, err, result, tt.expectedErrorSubstring)
		})
	}
}

func checkExecuteErrorResult(t *testing.T, err error, result any, expectedErrorSubstring string) {
	if err != nil {
		if expectedErrorSubstring != "" && !strings.Contains(err.Error(), expectedErrorSubstring) {
			t.Errorf("Expected error message to contain '%s', got '%s'", expectedErrorSubstring, err.Error())
		}
		return
	}

	toolResult, ok := result.(*domain.ToolExecutionResult)
	if !ok || toolResult == nil {
		t.Error("Expected error but got successful result")
		return
	}

	if !toolResult.Success {
		if expectedErrorSubstring != "" && !strings.Contains(toolResult.Error, expectedErrorSubstring) {
			t.Errorf("Expected error message to contain '%s', got '%s'", expectedErrorSubstring, toolResult.Error)
		}
		return
	}

	t.Error("Expected error but got successful result")
}

func TestEditTool_Execute_DisabledTool(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: false,
		},
	}

	tool := NewEditTool(cfg)

	args := map[string]any{
		"file_path":  "test.txt",
		"old_string": "old",
		"new_string": "new",
	}

	_, err := tool.Execute(context.Background(), args)

	if err == nil {
		t.Error("Expected error for disabled tool")
	}

	expectedError := "edit tool is not enabled"
	if err.Error() != expectedError {
		t.Errorf("Expected error '%s', got '%s'", expectedError, err.Error())
	}
}

func TestEditTool_ShouldCollapseArg(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Edit: config.EditToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewEditTool(cfg)

	tests := []struct {
		arg      string
		expected bool
	}{
		{"old_string", true},
		{"new_string", true},
		{"file_path", false},
		{"replace_all", false},
		{"other_param", false},
	}

	for _, tt := range tests {
		t.Run(tt.arg, func(t *testing.T) {
			result := tool.ShouldCollapseArg(tt.arg)
			if result != tt.expected {
				t.Errorf("ShouldCollapseArg(%s) = %v, expected %v", tt.arg, result, tt.expected)
			}
		})
	}
}

type editExecuteTestCase struct {
	name             string
	initialContent   string
	args             map[string]any
	expectedContent  string
	expectedSuccess  bool
	expectedReplaced int
	expectedModified bool
	errorContains    string
}

func getEditExecuteBasicTestCases() []editExecuteTestCase {
	return []editExecuteTestCase{
		{
			name:           "single word replacement",
			initialContent: "hello world",
			args: map[string]any{
				"file_path":  "",
				"old_string": "hello",
				"new_string": "hi",
			},
			expectedContent:  "hi world",
			expectedSuccess:  true,
			expectedReplaced: 1,
			expectedModified: true,
		},
		{
			name:           "multi-line replacement",
			initialContent: "line1\nline2\nline3",
			args: map[string]any{
				"file_path":  "",
				"old_string": "line2",
				"new_string": "replaced_line",
			},
			expectedContent:  "line1\nreplaced_line\nline3",
			expectedSuccess:  true,
			expectedReplaced: 1,
			expectedModified: true,
		},
		{
			name:           "replace with multi-line content",
			initialContent: "start\nmiddle\nend",
			args: map[string]any{
				"file_path":  "",
				"old_string": "middle",
				"new_string": "line1\nline2\nline3",
			},
			expectedContent:  "start\nline1\nline2\nline3\nend",
			expectedSuccess:  true,
			expectedReplaced: 1,
			expectedModified: true,
		},
		{
			name:           "replace all occurrences",
			initialContent: "test test test",
			args: map[string]any{
				"file_path":   "",
				"old_string":  "test",
				"new_string":  "example",
				"replace_all": true,
			},
			expectedContent:  "example example example",
			expectedSuccess:  true,
			expectedReplaced: 3,
			expectedModified: true,
		},
		{
			name:           "delete content (replace with empty)",
			initialContent: "keep this remove_this keep that",
			args: map[string]any{
				"file_path":  "",
				"old_string": " remove_this",
				"new_string": "",
			},
			expectedContent:  "keep this keep that",
			expectedSuccess:  true,
			expectedReplaced: 1,
			expectedModified: true,
		},
		{
			name:           "whitespace-only changes",
			initialContent: "no  spaces",
			args: map[string]any{
				"file_path":  "",
				"old_string": "  ",
				"new_string": " ",
			},
			expectedContent:  "no spaces",
			expectedSuccess:  true,
			expectedReplaced: 1,
			expectedModified: true,
		},
		{
			name:           "unicode content replacement",
			initialContent: "Hello 世界! emoji: 🎉",
			args: map[string]any{
				"file_path":  "",
				"old_string": "世界",
				"new_string": "World",
			},
			expectedContent:  "Hello World! emoji: 🎉",
			expectedSuccess:  true,
			expectedReplaced: 1,
			expectedModified: true,
		},
		{
			name:           "preserve indentation",
			initialContent: "func test() {\n\treturn nil\n}",
			args: map[string]any{
				"file_path":  "",
				"old_string": "\treturn nil",
				"new_string": "\treturn true",
			},
			expectedContent:  "func test() {\n\treturn true\n}",
			expectedSuccess:  true,
			expectedReplaced: 1,
			expectedModified: true,
		},
	}
}

func getEditExecuteAdvancedTestCases() []editExecuteTestCase {
	return []editExecuteTestCase{
		{
			name:           "fail on non-unique string without replace_all",
			initialContent: "duplicate duplicate",
			args: map[string]any{
				"file_path":  "",
				"old_string": "duplicate",
				"new_string": "unique",
			},
			expectedContent: "duplicate duplicate",
			expectedSuccess: false,
			errorContains:   "is not unique in file",
		},
		{
			name:           "fail when old_string not found",
			initialContent: "hello world",
			args: map[string]any{
				"file_path":  "",
				"old_string": "nonexistent",
				"new_string": "replacement",
			},
			expectedContent: "hello world",
			expectedSuccess: false,
			errorContains:   "old_string not found in file",
		},
		{
			name:           "special regex characters in content",
			initialContent: "path/to/file.go (line 10)",
			args: map[string]any{
				"file_path":  "",
				"old_string": "(line 10)",
				"new_string": "(line 20)",
			},
			expectedContent:  "path/to/file.go (line 20)",
			expectedSuccess:  true,
			expectedReplaced: 1,
			expectedModified: true,
		},
		{
			name:           "replace at start of file",
			initialContent: "START middle end",
			args: map[string]any{
				"file_path":  "",
				"old_string": "START",
				"new_string": "BEGIN",
			},
			expectedContent:  "BEGIN middle end",
			expectedSuccess:  true,
			expectedReplaced: 1,
			expectedModified: true,
		},
		{
			name:           "replace at end of file",
			initialContent: "start middle END",
			args: map[string]any{
				"file_path":  "",
				"old_string": "END",
				"new_string": "FINISH",
			},
			expectedContent:  "start middle FINISH",
			expectedSuccess:  true,
			expectedReplaced: 1,
			expectedModified: true,
		},
		{
			name:           "newline handling - add newlines",
			initialContent: "single line",
			args: map[string]any{
				"file_path":  "",
				"old_string": "single line",
				"new_string": "first line\nsecond line\nthird line",
			},
			expectedContent:  "first line\nsecond line\nthird line",
			expectedSuccess:  true,
			expectedReplaced: 1,
			expectedModified: true,
		},
		{
			name:           "newline handling - remove newlines",
			initialContent: "line1\nline2\nline3",
			args: map[string]any{
				"file_path":  "",
				"old_string": "line1\nline2\nline3",
				"new_string": "single line",
			},
			expectedContent:  "single line",
			expectedSuccess:  true,
			expectedReplaced: 1,
			expectedModified: true,
		},
	}
}

func getEditExecuteTestCases() []editExecuteTestCase {
	cases := getEditExecuteBasicTestCases()
	return append(cases, getEditExecuteAdvancedTestCases()...)
}

func runEditExecuteTest(t *testing.T, tt editExecuteTestCase, tempDir string, cfg *config.Config) {
	t.Helper()
	testFile := filepath.Join(tempDir, fmt.Sprintf("test_%s.txt", strings.ReplaceAll(tt.name, " ", "_")))
	err := os.WriteFile(testFile, []byte(tt.initialContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	tt.args["file_path"] = testFile

	mockTracker := &MockReadToolTracker{readToolUsed: true}
	tool := NewEditToolWithRegistry(cfg, mockTracker)

	result, err := tool.Execute(context.Background(), tt.args)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}

	if result.Success != tt.expectedSuccess {
		t.Errorf("Expected success=%v, got %v. Error: %s", tt.expectedSuccess, result.Success, result.Error)
	}

	if tt.expectedSuccess {
		editResult, ok := result.Data.(*domain.EditToolResult)
		if !ok {
			t.Fatal("Expected EditToolResult in result data")
		}
		if editResult.ReplacedCount != tt.expectedReplaced {
			t.Errorf("Expected %d replacements, got %d", tt.expectedReplaced, editResult.ReplacedCount)
		}
		if editResult.FileModified != tt.expectedModified {
			t.Errorf("Expected FileModified=%v, got %v", tt.expectedModified, editResult.FileModified)
		}
	}

	if tt.errorContains != "" && !strings.Contains(result.Error, tt.errorContains) {
		t.Errorf("Expected error containing '%s', got '%s'", tt.errorContains, result.Error)
	}

	content, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read test file: %v", err)
	}
	if string(content) != tt.expectedContent {
		t.Errorf("Expected file content:\n%q\nGot:\n%q", tt.expectedContent, string(content))
	}
}

func TestEditTool_Execute_TableDriven(t *testing.T) {
	tempDir := t.TempDir()
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Sandbox: config.SandboxConfig{
				Directories: []string{tempDir},
			},
			Edit: config.EditToolConfig{
				Enabled: true,
			},
		},
	}

	for _, tt := range getEditExecuteTestCases() {
		t.Run(tt.name, func(t *testing.T) {
			runEditExecuteTest(t, tt, tempDir, cfg)
		})
	}
}

func TestEditTool_CleanString_TableDriven(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Sandbox: config.SandboxConfig{
				Directories: []string{"."},
			},
			Edit: config.EditToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewEditTool(cfg)

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "tab-separated line number single line",
			input:    "  10\tfunc main() {",
			expected: "func main() {",
		},
		{
			name:     "arrow-separated line number single line",
			input:    "  10→func main() {",
			expected: "func main() {",
		},
		{
			name:     "multi-line with tab prefixes",
			input:    "   5\tpackage main\n   6\t\n   7\timport \"fmt\"",
			expected: "package main\n\nimport \"fmt\"",
		},
		{
			name:     "multi-line with arrow prefixes",
			input:    "   5→package main\n   6→\n   7→import \"fmt\"",
			expected: "package main\n\nimport \"fmt\"",
		},
		{
			name:     "no line number prefix - unchanged",
			input:    "regular content without prefix",
			expected: "regular content without prefix",
		},
		{
			name:     "line starting with number but no separator",
			input:    "10 items in stock",
			expected: "10 items in stock",
		},
		{
			name:     "mixed content - some with prefixes",
			input:    "  1\tline one\nregular line\n  3\tline three",
			expected: "line one\nregular line\nline three",
		},
		{
			name:     "high line numbers",
			input:    "  999\tsome code\n 1000\tmore code",
			expected: "some code\nmore code",
		},
		{
			name:     "single digit line number",
			input:    "1\tcode",
			expected: "code",
		},
		{
			name:     "empty line with number prefix",
			input:    "  5\t",
			expected: "",
		},
		{
			name:     "content with tabs inside",
			input:    "  1\tvar x\t= 10",
			expected: "var x\t= 10",
		},
		{
			name:     "preserve leading spaces in content",
			input:    "  5\t    indented content",
			expected: "    indented content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tool.cleanString(tt.input)
			if result != tt.expected {
				t.Errorf("cleanString(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestEditTool_FormatResult_TableDriven(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Edit: config.EditToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewEditTool(cfg)

	tests := []struct {
		name       string
		result     *domain.ToolExecutionResult
		formatType domain.FormatterType
		contains   []string
	}{
		{
			name: "FormatPreview - successful single edit",
			result: &domain.ToolExecutionResult{
				ToolName: "Edit",
				Success:  true,
				Arguments: map[string]any{
					"file_path":  "/path/to/file.go",
					"old_string": "old",
					"new_string": "new",
				},
				Data: &domain.EditToolResult{
					FilePath:        "/path/to/file.go",
					ReplacedCount:   1,
					FileModified:    true,
					BytesDifference: 5,
					LinesDifference: 0,
				},
			},
			formatType: domain.FormatterShort,
			contains:   []string{"Updated", "file.go"},
		},
		{
			name: "FormatPreview - replace all",
			result: &domain.ToolExecutionResult{
				ToolName: "Edit",
				Success:  true,
				Arguments: map[string]any{
					"file_path":   "/path/to/file.go",
					"old_string":  "old",
					"new_string":  "new",
					"replace_all": true,
				},
				Data: &domain.EditToolResult{
					FilePath:        "/path/to/file.go",
					ReplacedCount:   5,
					ReplaceAll:      true,
					FileModified:    true,
					BytesDifference: 10,
					LinesDifference: 0,
				},
			},
			formatType: domain.FormatterShort,
			contains:   []string{"Replaced", "5", "occurrences"},
		},
		{
			name: "FormatPreview - no changes",
			result: &domain.ToolExecutionResult{
				ToolName: "Edit",
				Success:  true,
				Arguments: map[string]any{
					"file_path":  "/path/to/file.go",
					"old_string": "old",
					"new_string": "old",
				},
				Data: &domain.EditToolResult{
					FilePath:     "/path/to/file.go",
					FileModified: false,
				},
			},
			formatType: domain.FormatterShort,
			contains:   []string{"No changes"},
		},
		{
			name: "FormatUI - successful edit",
			result: &domain.ToolExecutionResult{
				ToolName: "Edit",
				Success:  true,
				Arguments: map[string]any{
					"file_path":  "/path/to/file.go",
					"old_string": "old",
					"new_string": "new",
				},
				Data: &domain.EditToolResult{
					FilePath:        "/path/to/file.go",
					ReplacedCount:   1,
					FileModified:    true,
					BytesDifference: 3,
					LinesDifference: 0,
				},
			},
			formatType: domain.FormatterUI,
			contains:   []string{"Edit(", "file_path"},
		},
		{
			name: "FormatUI - failed edit",
			result: &domain.ToolExecutionResult{
				ToolName: "Edit",
				Success:  false,
				Error:    "old_string not found",
				Arguments: map[string]any{
					"file_path":  "/path/to/file.go",
					"old_string": "missing",
					"new_string": "new",
				},
			},
			formatType: domain.FormatterUI,
			contains:   []string{"Edit("},
		},
		{
			name:       "FormatResult - nil result",
			result:     nil,
			formatType: domain.FormatterUI,
			contains:   []string{"unavailable"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := tool.FormatResult(tt.result, tt.formatType)
			for _, expected := range tt.contains {
				if !strings.Contains(output, expected) {
					t.Errorf("FormatResult output should contain '%s', got:\n%s", expected, output)
				}
			}
		})
	}
}

func testEditEdgeCaseEmptyFile(t *testing.T, tempDir string, tool *EditTool) {
	t.Helper()
	testFile := filepath.Join(tempDir, "empty.txt")
	if err := os.WriteFile(testFile, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}
	args := map[string]any{"file_path": testFile, "old_string": "nonexistent", "new_string": "new content"}
	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.Success {
		t.Error("Expected failure when searching in empty file")
	}
}

func testEditEdgeCaseLongReplacement(t *testing.T, tempDir string, tool *EditTool) {
	t.Helper()
	testFile := filepath.Join(tempDir, "long.txt")
	if err := os.WriteFile(testFile, []byte("MARKER"), 0644); err != nil {
		t.Fatal(err)
	}
	longContent := strings.Repeat("a", 10000)
	args := map[string]any{"file_path": testFile, "old_string": "MARKER", "new_string": longContent}
	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !result.Success {
		t.Errorf("Expected success, got error: %s", result.Error)
	}
	content, _ := os.ReadFile(testFile)
	if len(content) != 10000 {
		t.Errorf("Expected content length 10000, got %d", len(content))
	}
}

func testEditEdgeCaseBinaryContent(t *testing.T, tempDir string, tool *EditTool) {
	t.Helper()
	testFile := filepath.Join(tempDir, "binary.txt")
	if err := os.WriteFile(testFile, []byte("start\x00middle\x00end"), 0644); err != nil {
		t.Fatal(err)
	}
	args := map[string]any{"file_path": testFile, "old_string": "middle", "new_string": "replaced"}
	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !result.Success {
		t.Errorf("Expected success, got error: %s", result.Error)
	}
}

func testEditEdgeCaseDuplicates(t *testing.T, tempDir string, tool *EditTool) {
	t.Helper()
	testFile := filepath.Join(tempDir, "introduce_dup.txt")
	if err := os.WriteFile(testFile, []byte("unique value"), 0644); err != nil {
		t.Fatal(err)
	}
	args := map[string]any{"file_path": testFile, "old_string": "unique", "new_string": "value"}
	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !result.Success {
		t.Errorf("Expected success, got error: %s", result.Error)
	}
	content, _ := os.ReadFile(testFile)
	if string(content) != "value value" {
		t.Errorf("Expected 'value value', got '%s'", string(content))
	}
}

func testEditEdgeCaseCRLF(t *testing.T, tempDir string, tool *EditTool) {
	t.Helper()
	testFile := filepath.Join(tempDir, "crlf.txt")
	if err := os.WriteFile(testFile, []byte("line1\r\nline2\r\nline3"), 0644); err != nil {
		t.Fatal(err)
	}
	args := map[string]any{"file_path": testFile, "old_string": "line2", "new_string": "replaced"}
	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !result.Success {
		t.Errorf("Expected success, got error: %s", result.Error)
	}
}

func testEditEdgeCaseOverlapping(t *testing.T, tempDir string, tool *EditTool) {
	t.Helper()
	testFile := filepath.Join(tempDir, "overlap.txt")
	if err := os.WriteFile(testFile, []byte("abc-abc-abc"), 0644); err != nil {
		t.Fatal(err)
	}
	args := map[string]any{"file_path": testFile, "old_string": "abc", "new_string": "XYZ", "replace_all": true}
	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !result.Success {
		t.Errorf("Expected success, got error: %s", result.Error)
	}
	content, _ := os.ReadFile(testFile)
	if string(content) != "XYZ-XYZ-XYZ" {
		t.Errorf("Expected 'XYZ-XYZ-XYZ', got '%s'", string(content))
	}
}

func TestEditTool_Execute_EdgeCases(t *testing.T) {
	tempDir := t.TempDir()
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Sandbox: config.SandboxConfig{Directories: []string{tempDir}},
			Edit:    config.EditToolConfig{Enabled: true},
		},
	}
	mockTracker := &MockReadToolTracker{readToolUsed: true}
	tool := NewEditToolWithRegistry(cfg, mockTracker)

	t.Run("empty file editing", func(t *testing.T) { testEditEdgeCaseEmptyFile(t, tempDir, tool) })
	t.Run("very long replacement", func(t *testing.T) { testEditEdgeCaseLongReplacement(t, tempDir, tool) })
	t.Run("binary-like content", func(t *testing.T) { testEditEdgeCaseBinaryContent(t, tempDir, tool) })
	t.Run("introduces duplicates", func(t *testing.T) { testEditEdgeCaseDuplicates(t, tempDir, tool) })
	t.Run("CRLF handling", func(t *testing.T) { testEditEdgeCaseCRLF(t, tempDir, tool) })
	t.Run("overlapping pattern", func(t *testing.T) { testEditEdgeCaseOverlapping(t, tempDir, tool) })
}

func TestEditTool_GetDiffInfo(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Edit: config.EditToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewEditTool(cfg)

	tests := []struct {
		name         string
		args         map[string]any
		expectedFile string
		expectedOld  string
		expectedNew  string
	}{
		{
			name: "basic diff info",
			args: map[string]any{
				"file_path":  "/path/to/file.go",
				"old_string": "old content",
				"new_string": "new content",
			},
			expectedFile: "/path/to/file.go",
			expectedOld:  "old content",
			expectedNew:  "new content",
		},
		{
			name: "multi-line diff info",
			args: map[string]any{
				"file_path":  "/path/to/file.go",
				"old_string": "line1\nline2",
				"new_string": "newline1\nnewline2\nnewline3",
			},
			expectedFile: "/path/to/file.go",
			expectedOld:  "line1\nline2",
			expectedNew:  "newline1\nnewline2\nnewline3",
		},
		{
			name: "empty strings",
			args: map[string]any{
				"file_path":  "/path/to/file.go",
				"old_string": "",
				"new_string": "new content",
			},
			expectedFile: "/path/to/file.go",
			expectedOld:  "",
			expectedNew:  "new content",
		},
		{
			name:         "missing arguments",
			args:         map[string]any{},
			expectedFile: "",
			expectedOld:  "",
			expectedNew:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diffInfo := tool.GetDiffInfo(tt.args)

			if diffInfo.FilePath != tt.expectedFile {
				t.Errorf("Expected FilePath=%q, got %q", tt.expectedFile, diffInfo.FilePath)
			}
			if diffInfo.OldContent != tt.expectedOld {
				t.Errorf("Expected OldContent=%q, got %q", tt.expectedOld, diffInfo.OldContent)
			}
			if diffInfo.NewContent != tt.expectedNew {
				t.Errorf("Expected NewContent=%q, got %q", tt.expectedNew, diffInfo.NewContent)
			}
		})
	}
}
