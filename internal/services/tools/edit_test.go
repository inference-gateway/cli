package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
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
