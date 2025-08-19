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

	tool := NewMultiEditTool(cfg)
	def := tool.Definition()

	if def.Name != "MultiEdit" {
		t.Errorf("Expected tool name 'MultiEdit', got %s", def.Name)
	}

	if def.Description == "" {
		t.Error("Tool description should not be empty")
	}

	if def.Parameters == nil {
		t.Error("Tool parameters should not be nil")
	}
}

func TestMultiEditTool_Execute_RequiresReadTool(t *testing.T) {
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
	tool := NewMultiEditToolWithRegistry(cfg, mockTracker)

	args := map[string]interface{}{
		"file_path": "/tmp/test.txt",
		"edits": []interface{}{
			map[string]interface{}{
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

	args := map[string]interface{}{
		"file_path": testFile,
		"edits": []interface{}{
			map[string]interface{}{
				"old_string": "Hello",
				"new_string": "Hi",
			},
			map[string]interface{}{
				"old_string":  "world",
				"new_string":  "universe",
				"replace_all": true,
			},
			map[string]interface{}{
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

	// Verify file content
	newContent, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatal(err)
	}

	expectedContent := "Hi universe!\nThis is a example file.\nGoodbye universe!"
	if string(newContent) != expectedContent {
		t.Errorf("Expected content:\n%s\nGot:\n%s", expectedContent, string(newContent))
	}

	// Verify edit results
	if len(multiEditResult.Edits) != 3 {
		t.Errorf("Expected 3 edit results, got %d", len(multiEditResult.Edits))
	}

	// First edit: Hello -> Hi
	if multiEditResult.Edits[0].ReplacedCount != 1 {
		t.Errorf("First edit should replace 1 occurrence, got %d", multiEditResult.Edits[0].ReplacedCount)
	}

	// Second edit: world -> universe (replace_all)
	if multiEditResult.Edits[1].ReplacedCount != 2 {
		t.Errorf("Second edit should replace 2 occurrences, got %d", multiEditResult.Edits[1].ReplacedCount)
	}

	// Third edit: test file -> example file
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

	// Test that edits apply sequentially - first change "name" to "userName", then "userName" to "username"
	args := map[string]interface{}{
		"file_path": testFile,
		"edits": []interface{}{
			map[string]interface{}{
				"old_string": "name",
				"new_string": "userName",
			},
			map[string]interface{}{
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

	// Verify file content
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

	// First edit will succeed, second edit will fail (nonexistent string)
	args := map[string]interface{}{
		"file_path": testFile,
		"edits": []interface{}{
			map[string]interface{}{
				"old_string": "Hello",
				"new_string": "Hi",
			},
			map[string]interface{}{
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

	// Verify file was not modified (atomic behavior)
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

	args := map[string]interface{}{
		"file_path": testFile,
		"edits": []interface{}{
			map[string]interface{}{
				"old_string":  "test",
				"new_string":  "example",
				"replace_all": false, // This should fail because "test" appears multiple times
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

	// Verify file was not modified
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

	args := map[string]interface{}{
		"file_path": testFile,
		"edits": []interface{}{
			map[string]interface{}{
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

	// Create new file with empty old_string and then edit it
	args := map[string]interface{}{
		"file_path": testFile,
		"edits": []interface{}{
			map[string]interface{}{
				"old_string": "",
				"new_string": "Hello world!\nThis is new content.",
			},
			map[string]interface{}{
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
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Sandbox: config.SandboxConfig{
				Directories: []string{".", "/tmp"},
			},
			Edit: config.EditToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewMultiEditTool(cfg)

	testCases := []struct {
		name string
		args map[string]interface{}
		want string
	}{
		{
			name: "missing file_path",
			args: map[string]interface{}{
				"edits": []interface{}{},
			},
			want: "file_path parameter is required",
		},
		{
			name: "missing edits",
			args: map[string]interface{}{
				"file_path": "/tmp/test.txt",
			},
			want: "edits parameter is required",
		},
		{
			name: "empty edits array",
			args: map[string]interface{}{
				"file_path": "/tmp/test.txt",
				"edits":     []interface{}{},
			},
			want: "edits array must contain at least one edit operation",
		},
		{
			name: "invalid edit missing old_string",
			args: map[string]interface{}{
				"file_path": "/tmp/test.txt",
				"edits": []interface{}{
					map[string]interface{}{
						"new_string": "hello",
					},
				},
			},
			want: "old_string parameter is required",
		},
		{
			name: "invalid edit missing new_string",
			args: map[string]interface{}{
				"file_path": "/tmp/test.txt",
				"edits": []interface{}{
					map[string]interface{}{
						"old_string": "hello",
					},
				},
			},
			want: "new_string parameter is required",
		},
		{
			name: "invalid edit same old and new string",
			args: map[string]interface{}{
				"file_path": "/tmp/test.txt",
				"edits": []interface{}{
					map[string]interface{}{
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

	args := map[string]interface{}{
		"file_path": "/tmp/test.txt",
		"edits": []interface{}{
			map[string]interface{}{
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
