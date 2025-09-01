package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
)

func TestMultiEditTool_Definition(t *testing.T) {
	wd, _ := os.Getwd()
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Sandbox: config.SandboxConfig{
				Directories: []string{wd},
			},
			Edit: config.EditToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewMultiEditTool(cfg)
	def := tool.Definition()

	if def.Function.Name != "MultiEdit" {
		t.Errorf("Expected tool name 'MultiEdit', got %s", def.Function.Name)
	}

	if *def.Function.Description == "" {
		t.Error("Tool description should not be empty")
	}

	if def.Function.Parameters == nil {
		t.Error("Tool parameters should not be nil")
	}
}

func TestMultiEditTool_Execute_RequiresReadTool(t *testing.T) {
	wd, _ := os.Getwd()
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Sandbox: config.SandboxConfig{
				Directories: []string{wd},
			},
			Edit: config.EditToolConfig{
				Enabled: true,
			},
		},
	}

	mockTracker := &MockReadToolTracker{readToolUsed: false}
	tool := NewMultiEditToolWithRegistry(cfg, mockTracker)

	args := map[string]any{
		"file_path": "/tmp/test.txt",
		"edits": []any{
			map[string]any{
				"old_string": "hello",
				"new_string": "hi",
			},
		},
	}

	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute should not return error: %v", err)
	}

	if result.Success {
		t.Error("Execute should fail when Read tool hasn't been used")
	}

	if !strings.Contains(result.Error, "Read tool has been used at least once") {
		t.Errorf("Error should mention Read tool requirement, got: %s", result.Error)
	}
}

func TestMultiEditTool_Execute_SuccessfulMultipleEdits(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "multiedit_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	testFile := filepath.Join(tmpDir, "test.txt")
	originalContent := "Hello world!\nThis is a test file.\nGoodbye world!"
	err = os.WriteFile(testFile, []byte(originalContent), 0644)
	if err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Sandbox: config.SandboxConfig{
				Directories: []string{tmpDir},
			},
			Edit: config.EditToolConfig{
				Enabled: true,
			},
		},
	}

	mockTracker := &MockReadToolTracker{readToolUsed: true}
	tool := NewMultiEditToolWithRegistry(cfg, mockTracker)

	args := map[string]any{
		"file_path": testFile,
		"edits": []any{
			map[string]any{
				"old_string": "Hello",
				"new_string": "Hi",
			},
			map[string]any{
				"old_string":  "world",
				"new_string":  "universe",
				"replace_all": true,
			},
			map[string]any{
				"old_string": "test file",
				"new_string": "example file",
			},
		},
	}

	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute should not return error: %v", err)
	}

	if !result.Success {
		t.Errorf("Execute should succeed, got error: %s", result.Error)
	}

	multiEditResult, ok := result.Data.(*domain.MultiEditToolResult)
	if !ok {
		t.Fatal("Expected MultiEditToolResult in result data")
	}

	if multiEditResult.TotalEdits != 3 {
		t.Errorf("Expected 3 total edits, got %d", multiEditResult.TotalEdits)
	}

	if multiEditResult.SuccessfulEdits != 3 {
		t.Errorf("Expected 3 successful edits, got %d", multiEditResult.SuccessfulEdits)
	}

	if !multiEditResult.FileModified {
		t.Error("File should be marked as modified")
	}

	newContent, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatal(err)
	}

	expectedContent := "Hi universe!\nThis is a example file.\nGoodbye universe!"
	if string(newContent) != expectedContent {
		t.Errorf("Expected content:\n%s\nGot:\n%s", expectedContent, string(newContent))
	}

	if len(multiEditResult.Edits) != 3 {
		t.Errorf("Expected 3 edit results, got %d", len(multiEditResult.Edits))
	}

	if multiEditResult.Edits[0].ReplacedCount != 1 {
		t.Errorf("First edit should replace 1 occurrence, got %d", multiEditResult.Edits[0].ReplacedCount)
	}

	if multiEditResult.Edits[1].ReplacedCount != 2 {
		t.Errorf("Second edit should replace 2 occurrences, got %d", multiEditResult.Edits[1].ReplacedCount)
	}

	if multiEditResult.Edits[2].ReplacedCount != 1 {
		t.Errorf("Third edit should replace 1 occurrence, got %d", multiEditResult.Edits[2].ReplacedCount)
	}
}

func TestMultiEditTool_Execute_SequentialEditsChangeContent(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "multiedit_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	testFile := filepath.Join(tmpDir, "test.txt")
	originalContent := "var name = value;"
	err = os.WriteFile(testFile, []byte(originalContent), 0644)
	if err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Sandbox: config.SandboxConfig{
				Directories: []string{tmpDir},
			},
			Edit: config.EditToolConfig{
				Enabled: true,
			},
		},
	}

	mockTracker := &MockReadToolTracker{readToolUsed: true}
	tool := NewMultiEditToolWithRegistry(cfg, mockTracker)

	args := map[string]any{
		"file_path": testFile,
		"edits": []any{
			map[string]any{
				"old_string": "name",
				"new_string": "userName",
			},
			map[string]any{
				"old_string": "userName",
				"new_string": "username",
			},
		},
	}

	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute should not return error: %v", err)
	}

	if !result.Success {
		t.Errorf("Execute should succeed, got error: %s", result.Error)
	}

	newContent, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatal(err)
	}

	expectedContent := "var username = value;"
	if string(newContent) != expectedContent {
		t.Errorf("Expected content:\n%s\nGot:\n%s", expectedContent, string(newContent))
	}
}

func TestMultiEditTool_Execute_AtomicFailure(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "multiedit_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	testFile := filepath.Join(tmpDir, "test.txt")
	originalContent := "Hello world!\nThis is a test."
	err = os.WriteFile(testFile, []byte(originalContent), 0644)
	if err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Sandbox: config.SandboxConfig{
				Directories: []string{tmpDir},
			},
			Edit: config.EditToolConfig{
				Enabled: true,
			},
		},
	}

	mockTracker := &MockReadToolTracker{readToolUsed: true}
	tool := NewMultiEditToolWithRegistry(cfg, mockTracker)

	args := map[string]any{
		"file_path": testFile,
		"edits": []any{
			map[string]any{
				"old_string": "Hello",
				"new_string": "Hi",
			},
			map[string]any{
				"old_string": "nonexistent",
				"new_string": "something",
			},
		},
	}

	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute should not return error: %v", err)
	}

	if result.Success {
		t.Error("Execute should fail when any edit fails")
	}

	if !strings.Contains(result.Error, "not found in file") {
		t.Errorf("Error should mention string not found, got: %s", result.Error)
	}

	currentContent, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatal(err)
	}

	if string(currentContent) != originalContent {
		t.Errorf("File should not be modified when any edit fails.\nOriginal: %s\nCurrent: %s", originalContent, string(currentContent))
	}
}

func TestMultiEditTool_Execute_NonUniqueString(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "multiedit_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	testFile := filepath.Join(tmpDir, "test.txt")
	originalContent := "test test test"
	err = os.WriteFile(testFile, []byte(originalContent), 0644)
	if err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Sandbox: config.SandboxConfig{
				Directories: []string{tmpDir},
			},
			Edit: config.EditToolConfig{
				Enabled: true,
			},
		},
	}

	mockTracker := &MockReadToolTracker{readToolUsed: true}
	tool := NewMultiEditToolWithRegistry(cfg, mockTracker)

	args := map[string]any{
		"file_path": testFile,
		"edits": []any{
			map[string]any{
				"old_string":  "test",
				"new_string":  "example",
				"replace_all": false,
			},
		},
	}

	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute should not return error: %v", err)
	}

	if result.Success {
		t.Error("Execute should fail when old_string is not unique and replace_all is false")
	}

	if !strings.Contains(result.Error, "is not unique") {
		t.Errorf("Error should mention string not unique, got: %s", result.Error)
	}

	currentContent, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatal(err)
	}

	if string(currentContent) != originalContent {
		t.Errorf("File should not be modified when edit fails.\nOriginal: %s\nCurrent: %s", originalContent, string(currentContent))
	}
}

func TestMultiEditTool_Execute_ReplaceAll(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "multiedit_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	testFile := filepath.Join(tmpDir, "test.txt")
	originalContent := "test test test"
	err = os.WriteFile(testFile, []byte(originalContent), 0644)
	if err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Sandbox: config.SandboxConfig{
				Directories: []string{tmpDir},
			},
			Edit: config.EditToolConfig{
				Enabled: true,
			},
		},
	}

	mockTracker := &MockReadToolTracker{readToolUsed: true}
	tool := NewMultiEditToolWithRegistry(cfg, mockTracker)

	args := map[string]any{
		"file_path": testFile,
		"edits": []any{
			map[string]any{
				"old_string":  "test",
				"new_string":  "example",
				"replace_all": true,
			},
		},
	}

	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute should not return error: %v", err)
	}

	if !result.Success {
		t.Errorf("Execute should succeed with replace_all, got error: %s", result.Error)
	}

	multiEditResult, ok := result.Data.(*domain.MultiEditToolResult)
	if !ok {
		t.Fatal("Expected MultiEditToolResult in result data")
	}

	if multiEditResult.Edits[0].ReplacedCount != 3 {
		t.Errorf("Should replace 3 occurrences, got %d", multiEditResult.Edits[0].ReplacedCount)
	}

	// Verify file content
	newContent, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatal(err)
	}

	expectedContent := "example example example"
	if string(newContent) != expectedContent {
		t.Errorf("Expected content:\n%s\nGot:\n%s", expectedContent, string(newContent))
	}
}

func TestMultiEditTool_Execute_NewFileCreation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "multiedit_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	testFile := filepath.Join(tmpDir, "new_file.txt")

	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Sandbox: config.SandboxConfig{
				Directories: []string{tmpDir},
			},
			Edit: config.EditToolConfig{
				Enabled: true,
			},
		},
	}

	mockTracker := &MockReadToolTracker{readToolUsed: true}
	tool := NewMultiEditToolWithRegistry(cfg, mockTracker)

	args := map[string]any{
		"file_path": testFile,
		"edits": []any{
			map[string]any{
				"old_string": "",
				"new_string": "Hello world!\nThis is new content.",
			},
			map[string]any{
				"old_string": "world",
				"new_string": "universe",
			},
		},
	}

	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute should not return error: %v", err)
	}

	if !result.Success {
		t.Errorf("Execute should succeed for new file creation, got error: %s", result.Error)
	}

	// Verify file was created and has correct content
	newContent, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatal("New file should be created")
	}

	expectedContent := "Hello universe!\nThis is new content."
	if string(newContent) != expectedContent {
		t.Errorf("Expected content:\n%s\nGot:\n%s", expectedContent, string(newContent))
	}
}

func TestMultiEditTool_Validate_InvalidArgs(t *testing.T) {
	wd, _ := os.Getwd()
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Sandbox: config.SandboxConfig{
				Directories: []string{wd, "/tmp"},
			},
			Edit: config.EditToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewMultiEditTool(cfg)

	testCases := []struct {
		name string
		args map[string]any
		want string
	}{
		{
			name: "missing file_path",
			args: map[string]any{
				"edits": []any{},
			},
			want: "file_path parameter is required",
		},
		{
			name: "missing edits",
			args: map[string]any{
				"file_path": "/tmp/test.txt",
			},
			want: "edits parameter is required",
		},
		{
			name: "empty edits array",
			args: map[string]any{
				"file_path": "/tmp/test.txt",
				"edits":     []any{},
			},
			want: "edits array must contain at least one edit operation",
		},
		{
			name: "invalid edit missing old_string",
			args: map[string]any{
				"file_path": "/tmp/test.txt",
				"edits": []any{
					map[string]any{
						"new_string": "hello",
					},
				},
			},
			want: "old_string parameter is required",
		},
		{
			name: "invalid edit missing new_string",
			args: map[string]any{
				"file_path": "/tmp/test.txt",
				"edits": []any{
					map[string]any{
						"old_string": "hello",
					},
				},
			},
			want: "new_string parameter is required",
		},
		{
			name: "invalid edit same old and new string",
			args: map[string]any{
				"file_path": "/tmp/test.txt",
				"edits": []any{
					map[string]any{
						"old_string": "hello",
						"new_string": "hello",
					},
				},
			},
			want: "new_string must be different from old_string",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tool.Validate(tc.args)
			if err == nil {
				t.Error("Validate should return error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Expected error to contain %q, got: %v", tc.want, err)
			}
		})
	}
}

func TestMultiEditTool_Execute_ToolDisabled(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: false,
		},
	}

	tool := NewMultiEditTool(cfg)

	args := map[string]any{
		"file_path": "/tmp/test.txt",
		"edits": []any{
			map[string]any{
				"old_string": "hello",
				"new_string": "hi",
			},
		},
	}

	_, err := tool.Execute(context.Background(), args)
	if err == nil {
		t.Error("Execute should return error when tool is disabled")
	}

	if !strings.Contains(err.Error(), "not enabled") {
		t.Errorf("Error should mention tool not enabled, got: %v", err)
	}
}

func TestMultiEditTool_IsEnabled(t *testing.T) {
	wd, _ := os.Getwd()
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Sandbox: config.SandboxConfig{
				Directories: []string{wd},
			},
			Edit: config.EditToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewMultiEditTool(cfg)
	if !tool.IsEnabled() {
		t.Error("Tool should be enabled")
	}

	cfg.Tools.Enabled = false
	tool = NewMultiEditTool(cfg)
	if tool.IsEnabled() {
		t.Error("Tool should be disabled when tools are globally disabled")
	}

	cfg.Tools.Enabled = true
	cfg.Tools.Edit.Enabled = false
	tool = NewMultiEditTool(cfg)
	if tool.IsEnabled() {
		t.Error("Tool should be disabled when edit tool is disabled")
	}
}

func TestMultiEditTool_Execute_WithLineNumbers(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "multiedit_linenum_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	testFile := filepath.Join(tmpDir, "test.go")
	originalContent := `package main

import "fmt"

func oldFunction() {
	fmt.Println("This is the old function")
}

func anotherFunction() {
	fmt.Println("Another function here")
	oldFunction()
}

var oldVariable = "old value"

func main() {
	fmt.Println("Starting program")
	oldFunction()
	fmt.Println(oldVariable)
}`

	err = os.WriteFile(testFile, []byte(originalContent), 0644)
	if err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Sandbox: config.SandboxConfig{
				Directories: []string{tmpDir},
			},
			Edit: config.EditToolConfig{
				Enabled: true,
			},
		},
	}

	mockTracker := &MockReadToolTracker{readToolUsed: true}
	tool := NewMultiEditToolWithRegistry(cfg, mockTracker)

	args := map[string]any{
		"file_path": testFile,
		"edits": []any{
			map[string]any{
				"old_string": "     5\tfunc oldFunction() {",
				"new_string": "func newFunction() {",
			},
		},
	}

	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute should not return error: %v", err)
	}

	if !result.Success {
		t.Errorf("Execute should succeed with line number cleaning, got error: %s", result.Error)
	}

	newContent, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatal(err)
	}

	expectedContent := `package main

import "fmt"

func newFunction() {
	fmt.Println("This is the old function")
}

func anotherFunction() {
	fmt.Println("Another function here")
	oldFunction()
}

var oldVariable = "old value"

func main() {
	fmt.Println("Starting program")
	oldFunction()
	fmt.Println(oldVariable)
}`

	if string(newContent) != expectedContent {
		t.Errorf("Expected content:\n%s\nGot:\n%s", expectedContent, string(newContent))
	}
}

func TestMultiEditTool_Execute_UserReportedCase(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "multiedit_user_case_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	testFile := filepath.Join(tmpDir, "test.go")
	originalContent := `package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
}`

	err = os.WriteFile(testFile, []byte(originalContent), 0644)
	if err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Sandbox: config.SandboxConfig{
				Directories: []string{tmpDir},
			},
			Edit: config.EditToolConfig{
				Enabled: true,
			},
		},
	}

	mockTracker := &MockReadToolTracker{readToolUsed: true}
	tool := NewMultiEditToolWithRegistry(cfg, mockTracker)

	args := map[string]any{
		"file_path": testFile,
		"edits": []any{
			map[string]any{
				"old_string": "package main\n\nimport \"fmt\"\n\nfunc main() {",
				"new_string": "package main\n\nimport \"fmt\"\n\n// This is a test comment\nfunc main() {",
			},
			map[string]any{
				"old_string": "\tfmt.Println(\"Hello, World!\")",
				"new_string": "\tfmt.Println(\"Hello, MultiEdit!\")",
			},
		},
	}

	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute should not return error: %v", err)
	}

	if !result.Success {
		t.Errorf("Execute should succeed with the improved MultiEdit tool, got error: %s", result.Error)
	}

	currentContent, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatal(err)
	}

	expectedContent := `package main

import "fmt"

// This is a test comment
func main() {
	fmt.Println("Hello, MultiEdit!")
}`

	if string(currentContent) != expectedContent {
		t.Errorf("Expected content:\n%s\nGot:\n%s", expectedContent, string(currentContent))
	}
}

func TestMultiEditTool_Execute_UserCaseCorrected(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "multiedit_user_case_corrected_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	testFile := filepath.Join(tmpDir, "test.go")
	originalContent := `package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
}`

	err = os.WriteFile(testFile, []byte(originalContent), 0644)
	if err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Sandbox: config.SandboxConfig{
				Directories: []string{tmpDir},
			},
			Edit: config.EditToolConfig{
				Enabled: true,
			},
		},
	}

	mockTracker := &MockReadToolTracker{readToolUsed: true}
	tool := NewMultiEditToolWithRegistry(cfg, mockTracker)

	args := map[string]any{
		"file_path": testFile,
		"edits": []any{
			map[string]any{
				"old_string": "import \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"Hello, World!\")\n}",
				"new_string": "import \"fmt\"\n\n// This is a test comment\nfunc main() {\n\tfmt.Println(\"Hello, MultiEdit!\")\n}",
			},
		},
	}

	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute should not return error: %v", err)
	}

	if !result.Success {
		t.Errorf("Execute should succeed with corrected approach, got error: %s", result.Error)
	}

	newContent, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatal(err)
	}

	expectedContent := `package main

import "fmt"

// This is a test comment
func main() {
	fmt.Println("Hello, MultiEdit!")
}`

	if string(newContent) != expectedContent {
		t.Errorf("Expected content:\n%s\nGot:\n%s", expectedContent, string(newContent))
	}
}

func TestMultiEditTool_FormatForUI(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
		},
	}

	tool := NewMultiEditTool(cfg)

	t.Run("Successful multi-edit with collapsed view", func(t *testing.T) {
		result := &domain.ToolExecutionResult{
			ToolName: "MultiEdit",
			Success:  true,
			Arguments: map[string]any{
				"file_path": "/path/to/test.go",
				"edits": []any{
					map[string]any{
						"old_string": "old1",
						"new_string": "new1",
					},
					map[string]any{
						"old_string": "old2",
						"new_string": "new2",
					},
					map[string]any{
						"old_string": "old3",
						"new_string": "new3",
					},
				},
			},
			Data: &domain.MultiEditToolResult{
				FilePath:        "/path/to/test.go",
				TotalEdits:      3,
				SuccessfulEdits: 3,
				FileModified:    true,
				OriginalSize:    100,
				NewSize:         105,
				BytesDifference: 5,
			},
		}

		output := tool.FormatForUI(result)

		if !strings.Contains(output, "MultiEdit(file=test.go, 3 edits)") {
			t.Errorf("Expected collapsed view to show 'MultiEdit(file=test.go, 3 edits)', got:\n%s", output)
		}

		if !strings.Contains(output, "✓") {
			t.Errorf("Expected success icon in output, got:\n%s", output)
		}

		if !strings.Contains(output, "Applied 3/3 edits") {
			t.Errorf("Expected 'Applied 3/3 edits' in preview, got:\n%s", output)
		}
	})

	t.Run("Failed multi-edit", func(t *testing.T) {
		result := &domain.ToolExecutionResult{
			ToolName: "MultiEdit",
			Success:  false,
			Error:    "old_string not found",
			Arguments: map[string]any{
				"file_path": "/path/to/test.go",
				"edits": []any{
					map[string]any{
						"old_string": "old1",
						"new_string": "new1",
					},
				},
			},
		}

		output := tool.FormatForUI(result)

		if !strings.Contains(output, "MultiEdit(file=test.go, 1 edits)") {
			t.Errorf("Expected collapsed view to show 'MultiEdit(file=test.go, 1 edits)', got:\n%s", output)
		}

		if !strings.Contains(output, "✗") {
			t.Errorf("Expected failure icon in output, got:\n%s", output)
		}
	})

	t.Run("Empty edits array", func(t *testing.T) {
		result := &domain.ToolExecutionResult{
			ToolName: "MultiEdit",
			Success:  false,
			Arguments: map[string]any{
				"file_path": "/path/to/test.go",
				"edits":     []any{},
			},
		}

		output := tool.FormatForUI(result)

		if !strings.Contains(output, "MultiEdit(") {
			t.Errorf("Expected MultiEdit in output, got:\n%s", output)
		}
	})

	t.Run("Nil result", func(t *testing.T) {
		output := tool.FormatForUI(nil)

		if output != "Tool execution result unavailable" {
			t.Errorf("Expected 'Tool execution result unavailable', got: %s", output)
		}
	})
}

func TestMultiEditTool_EdgeCases(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "multiedit_edge_cases_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Sandbox: config.SandboxConfig{
				Directories: []string{tmpDir},
			},
			Edit: config.EditToolConfig{
				Enabled: true,
			},
		},
	}

	mockTracker := &MockReadToolTracker{readToolUsed: true}
	tool := NewMultiEditToolWithRegistry(cfg, mockTracker)

	t.Run("Sequential edits with dependencies fail gracefully", func(t *testing.T) {
		testFile := filepath.Join(tmpDir, "deps.go")
		content := "func oldName() {}\nvar x = oldName"
		err := os.WriteFile(testFile, []byte(content), 0644)
		if err != nil {
			t.Fatal(err)
		}

		args := map[string]any{
			"file_path": testFile,
			"edits": []any{
				map[string]any{
					"old_string":  "oldName",
					"new_string":  "newName",
					"replace_all": true,
				},
				map[string]any{
					"old_string": "oldName",
					"new_string": "anotherName",
				},
			},
		}

		result, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute should not return error: %v", err)
		}

		if result.Success {
			t.Error("Execute should fail when later edits reference already-changed content")
		}

		if !strings.Contains(result.Error, "old_string not found") {
			t.Errorf("Error should mention old_string not found, got: %s", result.Error)
		}
	})

	t.Run("Multiple line edit with proper indentation", func(t *testing.T) {
		testFile := filepath.Join(tmpDir, "multiline.go")
		content := `func example() {
	if true {
		doSomething()
	}
}`
		err := os.WriteFile(testFile, []byte(content), 0644)
		if err != nil {
			t.Fatal(err)
		}

		args := map[string]any{
			"file_path": testFile,
			"edits": []any{
				map[string]any{
					"old_string": "if true {\n\t\tdoSomething()\n\t}",
					"new_string": "if false {\n\t\tdoNothing()\n\t}",
				},
			},
		}

		result, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute should not return error: %v", err)
		}

		if !result.Success {
			t.Errorf("Execute should succeed with proper multiline edit, got error: %s", result.Error)
		}
	})

	t.Run("Empty old_string for file creation", func(t *testing.T) {
		testFile := filepath.Join(tmpDir, "new.go")

		args := map[string]any{
			"file_path": testFile,
			"edits": []any{
				map[string]any{
					"old_string": "",
					"new_string": "package main\n\nfunc main() {}",
				},
			},
		}

		result, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute should not return error: %v", err)
		}

		if !result.Success {
			t.Errorf("Execute should succeed for new file creation, got error: %s", result.Error)
		}

		content, err := os.ReadFile(testFile)
		if err != nil {
			t.Fatal("New file should be created")
		}

		expected := "package main\n\nfunc main() {}"
		if string(content) != expected {
			t.Errorf("Expected: %s, Got: %s", expected, string(content))
		}
	})
}
