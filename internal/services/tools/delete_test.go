package tools

import (
	"context"
	"os"
	"testing"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
)

func TestDeleteTool_Definition(t *testing.T) {
	cfg := config.DefaultConfig()
	tool := NewDeleteTool(cfg)

	def := tool.Definition()
	if def.Name != "Delete" {
		t.Errorf("Expected tool name 'Delete', got '%s'", def.Name)
	}

	if def.Description == "" {
		t.Error("Expected non-empty description")
	}

	if def.Parameters == nil {
		t.Error("Expected non-nil parameters")
	}
}

func TestDeleteTool_IsEnabled(t *testing.T) {
	tests := []struct {
		name            string
		toolsEnabled    bool
		deleteEnabled   bool
		expectedEnabled bool
	}{
		{"tools enabled and delete enabled", true, true, true},
		{"tools disabled and delete enabled", false, true, false},
		{"tools enabled and delete disabled", true, false, false},
		{"tools disabled and delete disabled", false, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.Tools.Enabled = tt.toolsEnabled
			cfg.Tools.Delete.Enabled = tt.deleteEnabled

			tool := NewDeleteTool(cfg)
			if tool.IsEnabled() != tt.expectedEnabled {
				t.Errorf("Expected IsEnabled() to return %v, got %v", tt.expectedEnabled, tool.IsEnabled())
			}
		})
	}
}

func TestDeleteTool_Validate(t *testing.T) {
	cfg := config.DefaultConfig()
	tool := NewDeleteTool(cfg)

	tests := []struct {
		name      string
		args      map[string]interface{}
		expectErr bool
	}{
		{
			name:      "valid args",
			args:      map[string]interface{}{"path": "test.txt"},
			expectErr: false,
		},
		{
			name:      "missing path",
			args:      map[string]interface{}{},
			expectErr: true,
		},
		{
			name:      "empty path",
			args:      map[string]interface{}{"path": ""},
			expectErr: true,
		},
		{
			name:      "invalid recursive type",
			args:      map[string]interface{}{"path": "test.txt", "recursive": "true"},
			expectErr: true,
		},
		{
			name:      "invalid force type",
			args:      map[string]interface{}{"path": "test.txt", "force": "true"},
			expectErr: true,
		},
		{
			name:      "invalid format",
			args:      map[string]interface{}{"path": "test.txt", "format": "xml"},
			expectErr: true,
		},
		{
			name:      "protected path .infer",
			args:      map[string]interface{}{"path": ".infer/config.yaml"},
			expectErr: true,
		},
		{
			name:      "protected path .git",
			args:      map[string]interface{}{"path": ".git/config"},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(tt.args)
			if (err != nil) != tt.expectErr {
				t.Errorf("Expected error: %v, got: %v", tt.expectErr, err)
			}
		})
	}
}

func TestDeleteTool_ValidateDisabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tools.Enabled = false
	tool := NewDeleteTool(cfg)

	args := map[string]interface{}{"path": "test.txt"}
	err := tool.Validate(args)
	if err == nil {
		t.Error("Expected error when tools are disabled")
	}
}

func TestDeleteTool_Execute_SingleFile(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "delete-tool-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Change to temp directory
	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(tempDir)

	// Create a test file
	testFile := "test.txt"
	content := "test content"
	err = os.WriteFile(testFile, []byte(content), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	cfg := config.DefaultConfig()
	tool := NewDeleteTool(cfg)

	args := map[string]interface{}{
		"path": testFile,
	}

	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected success, got: %s", result.Error)
	}

	// Verify file was deleted
	if _, err := os.Stat(testFile); !os.IsNotExist(err) {
		t.Error("Expected file to be deleted")
	}

	// Check result data
	deleteResult, ok := result.Data.(*domain.DeleteToolResult)
	if !ok {
		t.Fatal("Expected DeleteToolResult in result data")
	}

	if deleteResult.TotalFilesDeleted != 1 {
		t.Errorf("Expected 1 file deleted, got %d", deleteResult.TotalFilesDeleted)
	}

	if len(deleteResult.DeletedFiles) != 1 || deleteResult.DeletedFiles[0] != testFile {
		t.Errorf("Expected deleted files to contain %s, got %v", testFile, deleteResult.DeletedFiles)
	}
}

func TestDeleteTool_Execute_Directory(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "delete-tool-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Change to temp directory
	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(tempDir)

	// Create a test directory
	testDir := "testdir"
	err = os.Mkdir(testDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create test dir: %v", err)
	}

	cfg := config.DefaultConfig()
	tool := NewDeleteTool(cfg)

	// Test without recursive flag (should fail)
	args := map[string]interface{}{
		"path": testDir,
	}

	result, err := tool.Execute(context.Background(), args)
	if err == nil {
		t.Error("Expected error when deleting directory without recursive flag")
	}

	// Test with recursive flag
	args["recursive"] = true
	result, err = tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected success, got: %s", result.Error)
	}

	// Verify directory was deleted
	if _, err := os.Stat(testDir); !os.IsNotExist(err) {
		t.Error("Expected directory to be deleted")
	}

	// Check result data
	deleteResult, ok := result.Data.(*domain.DeleteToolResult)
	if !ok {
		t.Fatal("Expected DeleteToolResult in result data")
	}

	if deleteResult.TotalDirsDeleted != 1 {
		t.Errorf("Expected 1 directory deleted, got %d", deleteResult.TotalDirsDeleted)
	}
}

func TestDeleteTool_Execute_Wildcard(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "delete-tool-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Change to temp directory
	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(tempDir)

	// Create test files
	testFiles := []string{"test1.txt", "test2.txt", "other.log"}
	for _, file := range testFiles {
		err = os.WriteFile(file, []byte("content"), 0644)
		if err != nil {
			t.Fatalf("Failed to create test file %s: %v", file, err)
		}
	}

	cfg := config.DefaultConfig()
	cfg.Tools.Delete.AllowWildcards = true
	tool := NewDeleteTool(cfg)

	args := map[string]interface{}{
		"path": "*.txt",
	}

	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected success, got: %s", result.Error)
	}

	// Verify only .txt files were deleted
	if _, err := os.Stat("test1.txt"); !os.IsNotExist(err) {
		t.Error("Expected test1.txt to be deleted")
	}
	if _, err := os.Stat("test2.txt"); !os.IsNotExist(err) {
		t.Error("Expected test2.txt to be deleted")
	}
	if _, err := os.Stat("other.log"); os.IsNotExist(err) {
		t.Error("Expected other.log to remain")
	}

	// Check result data
	deleteResult, ok := result.Data.(*domain.DeleteToolResult)
	if !ok {
		t.Fatal("Expected DeleteToolResult in result data")
	}

	if deleteResult.TotalFilesDeleted != 2 {
		t.Errorf("Expected 2 files deleted, got %d", deleteResult.TotalFilesDeleted)
	}

	if !deleteResult.WildcardExpanded {
		t.Error("Expected WildcardExpanded to be true")
	}
}

func TestDeleteTool_Execute_WildcardDisabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tools.Delete.AllowWildcards = false
	tool := NewDeleteTool(cfg)

	args := map[string]interface{}{
		"path": "*.txt",
	}

	_, err := tool.Execute(context.Background(), args)
	if err == nil {
		t.Error("Expected error when wildcards are disabled")
	}
}

func TestDeleteTool_Execute_NonExistentFile(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "delete-tool-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Change to temp directory
	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(tempDir)

	cfg := config.DefaultConfig()
	tool := NewDeleteTool(cfg)

	// Test without force flag (should fail)
	args := map[string]interface{}{
		"path": "nonexistent.txt",
	}

	result, err := tool.Execute(context.Background(), args)
	if err == nil {
		t.Error("Expected error when deleting non-existent file without force flag")
	}

	// Test with force flag (should succeed)
	args["force"] = true
	result, err = tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected success with force flag, got: %s", result.Error)
	}
}

func TestDeleteTool_SecurityRestrictions(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "delete-tool-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Change to temp directory
	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(tempDir)

	cfg := config.DefaultConfig()
	cfg.Tools.Delete.RestrictToWorkDir = true
	tool := NewDeleteTool(cfg)

	// Test path outside working directory
	args := map[string]interface{}{
		"path": "../outside.txt",
	}

	err = tool.Validate(args)
	if err == nil {
		t.Error("Expected validation error for path outside working directory")
	}

	// Test absolute path outside working directory
	args = map[string]interface{}{
		"path": "/etc/passwd",
	}

	err = tool.Validate(args)
	if err == nil {
		t.Error("Expected validation error for absolute path outside working directory")
	}
}

func TestDeleteTool_ProtectedPaths(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tools.Delete.ProtectedPaths = []string{"protected/", "*.secret"}
	tool := NewDeleteTool(cfg)

	tests := []struct {
		name      string
		path      string
		expectErr bool
	}{
		{"protected directory", "protected/file.txt", true},
		{"protected pattern", "config.secret", true},
		{"allowed file", "normal.txt", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := map[string]interface{}{"path": tt.path}
			err := tool.Validate(args)
			if (err != nil) != tt.expectErr {
				t.Errorf("Expected error: %v, got: %v", tt.expectErr, err)
			}
		})
	}
}
