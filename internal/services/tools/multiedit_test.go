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

	if result.Success {
		t.Error("Execute should fail when old_string contains line numbers that don't exist in the file")
	}

	if !strings.Contains(result.Error, "not found") {
		t.Errorf("Error should mention 'not found', got: %s", result.Error)
	}

	newContent, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatal(err)
	}

	if string(newContent) != originalContent {
		t.Error("File content should be unchanged when edit fails")
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

type multiEditValidateTestCase struct {
	name      string
	args      map[string]any
	wantError bool
	errMsg    string
}

func getMultiEditValidateTestCases() []multiEditValidateTestCase {
	return []multiEditValidateTestCase{
		{name: "valid single edit", args: map[string]any{"file_path": "/tmp/test.txt", "edits": []any{map[string]any{"old_string": "old", "new_string": "new"}}}, wantError: false},
		{name: "valid multiple edits", args: map[string]any{"file_path": "/tmp/test.txt", "edits": []any{map[string]any{"old_string": "old1", "new_string": "new1"}, map[string]any{"old_string": "old2", "new_string": "new2"}}}, wantError: false},
		{name: "valid with replace_all", args: map[string]any{"file_path": "/tmp/test.txt", "edits": []any{map[string]any{"old_string": "old", "new_string": "new", "replace_all": true}}}, wantError: false},
		{name: "missing file_path", args: map[string]any{"edits": []any{map[string]any{"old_string": "old", "new_string": "new"}}}, wantError: true, errMsg: "file_path parameter is required"},
		{name: "empty file_path", args: map[string]any{"file_path": "", "edits": []any{map[string]any{"old_string": "old", "new_string": "new"}}}, wantError: true, errMsg: "file_path cannot be empty"},
		{name: "file_path wrong type", args: map[string]any{"file_path": 123, "edits": []any{map[string]any{"old_string": "old", "new_string": "new"}}}, wantError: true, errMsg: "file_path parameter is required and must be a string"},
		{name: "missing edits", args: map[string]any{"file_path": "/tmp/test.txt"}, wantError: true, errMsg: "edits parameter is required"},
		{name: "edits wrong type", args: map[string]any{"file_path": "/tmp/test.txt", "edits": "not an array"}, wantError: true, errMsg: "edits parameter must be an array"},
		{name: "empty edits array", args: map[string]any{"file_path": "/tmp/test.txt", "edits": []any{}}, wantError: true, errMsg: "edits array must contain at least one edit operation"},
		{name: "edit missing old_string", args: map[string]any{"file_path": "/tmp/test.txt", "edits": []any{map[string]any{"new_string": "new"}}}, wantError: true, errMsg: "old_string parameter is required"},
		{name: "edit missing new_string", args: map[string]any{"file_path": "/tmp/test.txt", "edits": []any{map[string]any{"old_string": "old"}}}, wantError: true, errMsg: "new_string parameter is required"},
		{name: "edit old_string wrong type", args: map[string]any{"file_path": "/tmp/test.txt", "edits": []any{map[string]any{"old_string": 123, "new_string": "new"}}}, wantError: true, errMsg: "old_string parameter is required and must be a string"},
		{name: "edit new_string wrong type", args: map[string]any{"file_path": "/tmp/test.txt", "edits": []any{map[string]any{"old_string": "old", "new_string": 123}}}, wantError: true, errMsg: "new_string parameter is required and must be a string"},
		{name: "edit old and new string same", args: map[string]any{"file_path": "/tmp/test.txt", "edits": []any{map[string]any{"old_string": "same", "new_string": "same"}}}, wantError: true, errMsg: "new_string must be different from old_string"},
		{name: "edit not an object", args: map[string]any{"file_path": "/tmp/test.txt", "edits": []any{"not an object"}}, wantError: true, errMsg: "edit at index 0 must be an object"},
		{name: "second edit invalid", args: map[string]any{"file_path": "/tmp/test.txt", "edits": []any{map[string]any{"old_string": "old1", "new_string": "new1"}, map[string]any{"old_string": "same", "new_string": "same"}}}, wantError: true, errMsg: "new_string must be different from old_string at edit index 1"},
	}
}

func runMultiEditValidateTest(t *testing.T, tt multiEditValidateTestCase, tool *MultiEditTool) {
	t.Helper()
	err := tool.Validate(tt.args)
	if tt.wantError {
		if err == nil {
			t.Error("Expected error but got none")
			return
		}
		if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
			t.Errorf("Expected error containing '%s', got '%s'", tt.errMsg, err.Error())
		}
	} else if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestMultiEditTool_Validate_TableDriven(t *testing.T) {
	wd, _ := os.Getwd()
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Sandbox: config.SandboxConfig{Directories: []string{wd, "/tmp"}},
			Edit:    config.EditToolConfig{Enabled: true},
		},
	}
	tool := NewMultiEditTool(cfg)

	for _, tt := range getMultiEditValidateTestCases() {
		t.Run(tt.name, func(t *testing.T) {
			runMultiEditValidateTest(t, tt, tool)
		})
	}
}

type multiEditExecuteTestCase struct {
	name                   string
	initialContent         string
	edits                  []any
	expectedContent        string
	expectedSuccess        bool
	expectedSuccessfulEdit int
	expectedModified       bool
	errorContains          string
}

func getMultiEditExecuteTestCases() []multiEditExecuteTestCase {
	return []multiEditExecuteTestCase{
		{name: "single edit success", initialContent: "hello world", edits: []any{map[string]any{"old_string": "hello", "new_string": "hi"}}, expectedContent: "hi world", expectedSuccess: true, expectedSuccessfulEdit: 1, expectedModified: true},
		{name: "multiple sequential edits", initialContent: "aaa bbb ccc", edits: []any{map[string]any{"old_string": "aaa", "new_string": "AAA"}, map[string]any{"old_string": "bbb", "new_string": "BBB"}, map[string]any{"old_string": "ccc", "new_string": "CCC"}}, expectedContent: "AAA BBB CCC", expectedSuccess: true, expectedSuccessfulEdit: 3, expectedModified: true},
		{name: "replace all in one edit", initialContent: "foo foo foo", edits: []any{map[string]any{"old_string": "foo", "new_string": "bar", "replace_all": true}}, expectedContent: "bar bar bar", expectedSuccess: true, expectedSuccessfulEdit: 1, expectedModified: true},
		{name: "chained edits", initialContent: "step1", edits: []any{map[string]any{"old_string": "step1", "new_string": "step2"}, map[string]any{"old_string": "step2", "new_string": "step3"}}, expectedContent: "step3", expectedSuccess: true, expectedSuccessfulEdit: 2, expectedModified: true},
		{name: "unicode content", initialContent: "Hello 世界", edits: []any{map[string]any{"old_string": "世界", "new_string": "World 🌍"}}, expectedContent: "Hello World 🌍", expectedSuccess: true, expectedSuccessfulEdit: 1, expectedModified: true},
		{name: "multi-line content", initialContent: "line1\nline2\nline3", edits: []any{map[string]any{"old_string": "line1\nline2", "new_string": "newline1\nnewline2"}}, expectedContent: "newline1\nnewline2\nline3", expectedSuccess: true, expectedSuccessfulEdit: 1, expectedModified: true},
		{name: "delete content", initialContent: "keep remove keep", edits: []any{map[string]any{"old_string": " remove", "new_string": ""}}, expectedContent: "keep keep", expectedSuccess: true, expectedSuccessfulEdit: 1, expectedModified: true},
		{name: "fail old_string not found", initialContent: "hello world", edits: []any{map[string]any{"old_string": "nonexistent", "new_string": "replacement"}}, expectedContent: "hello world", expectedSuccess: false, errorContains: "old_string not found"},
		{name: "fail non-unique", initialContent: "dup dup dup", edits: []any{map[string]any{"old_string": "dup", "new_string": "unique"}}, expectedContent: "dup dup dup", expectedSuccess: false, errorContains: "is not unique"},
		{name: "fail second edit atomic", initialContent: "first second", edits: []any{map[string]any{"old_string": "first", "new_string": "FIRST"}, map[string]any{"old_string": "nonexistent", "new_string": "replacement"}}, expectedContent: "first second", expectedSuccess: false, errorContains: "old_string not found"},
		{name: "preserve whitespace", initialContent: "  indented\n\tTabbed", edits: []any{map[string]any{"old_string": "  indented", "new_string": "    more indented"}}, expectedContent: "    more indented\n\tTabbed", expectedSuccess: true, expectedSuccessfulEdit: 1, expectedModified: true},
	}
}

func runMultiEditExecuteTest(t *testing.T, tt multiEditExecuteTestCase, tmpDir string, cfg *config.Config) {
	t.Helper()
	testFile := filepath.Join(tmpDir, strings.ReplaceAll(tt.name, " ", "_")+".txt")
	if err := os.WriteFile(testFile, []byte(tt.initialContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	mockTracker := &MockReadToolTracker{readToolUsed: true}
	tool := NewMultiEditToolWithRegistry(cfg, mockTracker)
	args := map[string]any{"file_path": testFile, "edits": tt.edits}

	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}

	if result.Success != tt.expectedSuccess {
		t.Errorf("Expected success=%v, got %v. Error: %s", tt.expectedSuccess, result.Success, result.Error)
	}

	if tt.expectedSuccess {
		multiEditResult, ok := result.Data.(*domain.MultiEditToolResult)
		if !ok {
			t.Fatal("Expected MultiEditToolResult in result data")
		}
		if multiEditResult.SuccessfulEdits != tt.expectedSuccessfulEdit {
			t.Errorf("Expected %d successful edits, got %d", tt.expectedSuccessfulEdit, multiEditResult.SuccessfulEdits)
		}
		if multiEditResult.FileModified != tt.expectedModified {
			t.Errorf("Expected FileModified=%v, got %v", tt.expectedModified, multiEditResult.FileModified)
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

func TestMultiEditTool_Execute_TableDriven(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Sandbox: config.SandboxConfig{Directories: []string{tmpDir}},
			Edit:    config.EditToolConfig{Enabled: true},
		},
	}

	for _, tt := range getMultiEditExecuteTestCases() {
		t.Run(tt.name, func(t *testing.T) {
			runMultiEditExecuteTest(t, tt, tmpDir, cfg)
		})
	}
}

func TestMultiEditTool_FormatResult_TableDriven(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Edit: config.EditToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewMultiEditTool(cfg)

	tests := []struct {
		name       string
		result     *domain.ToolExecutionResult
		formatType domain.FormatterType
		contains   []string
	}{
		{
			name: "FormatPreview - successful multiple edits",
			result: &domain.ToolExecutionResult{
				ToolName: "MultiEdit",
				Success:  true,
				Arguments: map[string]any{
					"file_path": "/path/to/file.go",
					"edits": []any{
						map[string]any{"old_string": "a", "new_string": "b"},
						map[string]any{"old_string": "c", "new_string": "d"},
					},
				},
				Data: &domain.MultiEditToolResult{
					FilePath:        "/path/to/file.go",
					TotalEdits:      2,
					SuccessfulEdits: 2,
					FileModified:    true,
					BytesDifference: 10,
				},
			},
			formatType: domain.FormatterShort,
			contains:   []string{"Applied", "2/2", "edits"},
		},
		{
			name: "FormatPreview - no changes",
			result: &domain.ToolExecutionResult{
				ToolName: "MultiEdit",
				Success:  true,
				Arguments: map[string]any{
					"file_path": "/path/to/file.go",
					"edits":     []any{},
				},
				Data: &domain.MultiEditToolResult{
					FilePath:     "/path/to/file.go",
					FileModified: false,
				},
			},
			formatType: domain.FormatterShort,
			contains:   []string{"No changes"},
		},
		{
			name: "FormatUI - collapsed view with edit count",
			result: &domain.ToolExecutionResult{
				ToolName: "MultiEdit",
				Success:  true,
				Arguments: map[string]any{
					"file_path": "/path/to/file.go",
					"edits": []any{
						map[string]any{"old_string": "a", "new_string": "b"},
						map[string]any{"old_string": "c", "new_string": "d"},
						map[string]any{"old_string": "e", "new_string": "f"},
					},
				},
				Data: &domain.MultiEditToolResult{
					FilePath:        "/path/to/file.go",
					TotalEdits:      3,
					SuccessfulEdits: 3,
					FileModified:    true,
				},
			},
			formatType: domain.FormatterUI,
			contains:   []string{"MultiEdit(", "3 edits", "file.go"},
		},
		{
			name: "FormatUI - failed edit",
			result: &domain.ToolExecutionResult{
				ToolName: "MultiEdit",
				Success:  false,
				Error:    "old_string not found",
				Arguments: map[string]any{
					"file_path": "/path/to/file.go",
					"edits": []any{
						map[string]any{"old_string": "missing", "new_string": "new"},
					},
				},
			},
			formatType: domain.FormatterUI,
			contains:   []string{"MultiEdit(", "✗"},
		},
		{
			name:       "FormatResult - nil result",
			result:     nil,
			formatType: domain.FormatterUI,
			contains:   []string{"unavailable"},
		},
		{
			name: "FormatLLM - successful with details",
			result: &domain.ToolExecutionResult{
				ToolName: "MultiEdit",
				Success:  true,
				Arguments: map[string]any{
					"file_path": "/path/to/file.go",
					"edits": []any{
						map[string]any{"old_string": "old", "new_string": "new"},
					},
				},
				Data: &domain.MultiEditToolResult{
					FilePath:        "/path/to/file.go",
					TotalEdits:      1,
					SuccessfulEdits: 1,
					FileModified:    true,
					BytesDifference: 5,
				},
			},
			formatType: domain.FormatterLLM,
			contains:   []string{"MultiEdit"},
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

func testMultiEditEdgeCaseLargeEdits(t *testing.T, tmpDir string, tool *MultiEditTool) {
	t.Helper()
	testFile := filepath.Join(tmpDir, "many_edits.txt")
	if err := os.WriteFile(testFile, []byte("[a1] [a2] [a3] [a4] [a5] [a6] [a7] [a8] [a9] [a10]"), 0644); err != nil {
		t.Fatal(err)
	}
	edits := make([]any, 10)
	for i := range 10 {
		edits[i] = map[string]any{"old_string": fmt.Sprintf("[a%d]", i+1), "new_string": fmt.Sprintf("[b%d]", i+1)}
	}
	result, err := tool.Execute(context.Background(), map[string]any{"file_path": testFile, "edits": edits})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !result.Success {
		t.Errorf("Expected success, got error: %s", result.Error)
	}
	newContent, _ := os.ReadFile(testFile)
	if string(newContent) != "[b1] [b2] [b3] [b4] [b5] [b6] [b7] [b8] [b9] [b10]" {
		t.Errorf("Unexpected content: %q", string(newContent))
	}
}

func testMultiEditEdgeCaseCancelEdits(t *testing.T, tmpDir string, tool *MultiEditTool) {
	t.Helper()
	testFile := filepath.Join(tmpDir, "cancel_edits.txt")
	if err := os.WriteFile(testFile, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	args := map[string]any{"file_path": testFile, "edits": []any{map[string]any{"old_string": "original", "new_string": "modified"}, map[string]any{"old_string": "modified", "new_string": "original"}}}
	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !result.Success {
		t.Errorf("Expected success, got error: %s", result.Error)
	}
	newContent, _ := os.ReadFile(testFile)
	if string(newContent) != "original" {
		t.Errorf("Expected 'original', got %q", string(newContent))
	}
}

func testMultiEditEdgeCaseBinary(t *testing.T, tmpDir string, tool *MultiEditTool) {
	t.Helper()
	testFile := filepath.Join(tmpDir, "binary.txt")
	if err := os.WriteFile(testFile, []byte("start\x00middle\x00end"), 0644); err != nil {
		t.Fatal(err)
	}
	result, err := tool.Execute(context.Background(), map[string]any{"file_path": testFile, "edits": []any{map[string]any{"old_string": "middle", "new_string": "replaced"}}})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !result.Success {
		t.Errorf("Expected success, got error: %s", result.Error)
	}
}

func testMultiEditEdgeCaseCRLF(t *testing.T, tmpDir string, tool *MultiEditTool) {
	t.Helper()
	testFile := filepath.Join(tmpDir, "crlf.txt")
	if err := os.WriteFile(testFile, []byte("line1\r\nline2\r\nline3"), 0644); err != nil {
		t.Fatal(err)
	}
	result, err := tool.Execute(context.Background(), map[string]any{"file_path": testFile, "edits": []any{map[string]any{"old_string": "line2", "new_string": "replaced"}}})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !result.Success {
		t.Errorf("Expected success, got error: %s", result.Error)
	}
	newContent, _ := os.ReadFile(testFile)
	if !strings.Contains(string(newContent), "replaced") {
		t.Errorf("Expected content to contain 'replaced', got %q", string(newContent))
	}
}

func testMultiEditEdgeCaseDirectory(t *testing.T, tmpDir string, tool *MultiEditTool) {
	t.Helper()
	result, err := tool.Execute(context.Background(), map[string]any{"file_path": tmpDir, "edits": []any{map[string]any{"old_string": "old", "new_string": "new"}}})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.Success {
		t.Error("Expected failure when path is a directory")
	}
	if !strings.Contains(result.Error, "is a directory") {
		t.Errorf("Expected error about directory, got: %s", result.Error)
	}
}

func TestMultiEditTool_AdditionalEdgeCases(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Sandbox: config.SandboxConfig{Directories: []string{tmpDir}},
			Edit:    config.EditToolConfig{Enabled: true},
		},
	}
	mockTracker := &MockReadToolTracker{readToolUsed: true}
	tool := NewMultiEditToolWithRegistry(cfg, mockTracker)

	t.Run("large number of edits", func(t *testing.T) { testMultiEditEdgeCaseLargeEdits(t, tmpDir, tool) })
	t.Run("cancel edits", func(t *testing.T) { testMultiEditEdgeCaseCancelEdits(t, tmpDir, tool) })
	t.Run("binary content", func(t *testing.T) { testMultiEditEdgeCaseBinary(t, tmpDir, tool) })
	t.Run("CRLF", func(t *testing.T) { testMultiEditEdgeCaseCRLF(t, tmpDir, tool) })
	t.Run("directory path", func(t *testing.T) { testMultiEditEdgeCaseDirectory(t, tmpDir, tool) })
}

func TestMultiEditTool_ShouldCollapseArg(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Edit: config.EditToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewMultiEditTool(cfg)

	tests := []struct {
		arg      string
		expected bool
	}{
		{"edits", true},
		{"file_path", false},
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

func TestMultiEditTool_GetDiffInfo(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "multiedit_diffinfo_test")
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
			Edit: config.EditToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewMultiEditTool(cfg)

	t.Run("valid edits simulation", func(t *testing.T) {
		testFile := filepath.Join(tmpDir, "test.txt")
		err := os.WriteFile(testFile, []byte("hello world"), 0644)
		if err != nil {
			t.Fatal(err)
		}

		args := map[string]any{
			"file_path": testFile,
			"edits": []any{
				map[string]any{
					"old_string": "hello",
					"new_string": "hi",
				},
			},
		}

		diffInfo := tool.GetDiffInfo(args)

		if diffInfo.FilePath != testFile {
			t.Errorf("Expected FilePath=%q, got %q", testFile, diffInfo.FilePath)
		}

		if diffInfo.OldContent != "hello world" {
			t.Errorf("Expected OldContent='hello world', got %q", diffInfo.OldContent)
		}

		if diffInfo.NewContent != "hi world" {
			t.Errorf("Expected NewContent='hi world', got %q", diffInfo.NewContent)
		}
	})

	t.Run("invalid edits format", func(t *testing.T) {
		args := map[string]any{
			"file_path": "/path/to/file.go",
			"edits":     "not an array",
		}

		diffInfo := tool.GetDiffInfo(args)

		if !strings.Contains(diffInfo.NewContent, "Invalid") {
			t.Errorf("Expected error message about invalid format, got: %s", diffInfo.NewContent)
		}
	})

	t.Run("edit would fail - old_string not found", func(t *testing.T) {
		testFile := filepath.Join(tmpDir, "notfound.txt")
		err := os.WriteFile(testFile, []byte("hello world"), 0644)
		if err != nil {
			t.Fatal(err)
		}

		args := map[string]any{
			"file_path": testFile,
			"edits": []any{
				map[string]any{
					"old_string": "nonexistent",
					"new_string": "new",
				},
			},
		}

		diffInfo := tool.GetDiffInfo(args)

		if !strings.Contains(diffInfo.NewContent, "failed") || !strings.Contains(diffInfo.NewContent, "not found") {
			t.Errorf("Expected failure message, got: %s", diffInfo.NewContent)
		}
	})
}
