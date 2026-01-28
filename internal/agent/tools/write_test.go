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

func TestWriteTool_Definition(t *testing.T) {
	cfg := config.DefaultConfig()
	tool := NewWriteTool(cfg)

	def := tool.Definition()
	if def.Function.Name != "Write" {
		t.Errorf("Expected tool name 'Write', got '%s'", def.Function.Name)
	}

	if *def.Function.Description == "" {
		t.Error("Tool description should not be empty")
	}

	if def.Function.Parameters == nil {
		t.Error("Tool parameters should not be nil")
	}
}

func TestWriteTool_IsEnabled(t *testing.T) {
	cfg := config.DefaultConfig()
	tool := NewWriteTool(cfg)

	if !tool.IsEnabled() {
		t.Error("Write tool should be enabled by default")
	}

	cfg.Tools.Enabled = false
	tool = NewWriteTool(cfg)
	if tool.IsEnabled() {
		t.Error("Write tool should be disabled when tools are globally disabled")
	}

	cfg.Tools.Enabled = true
	cfg.Tools.Write.Enabled = false
	tool = NewWriteTool(cfg)
	if tool.IsEnabled() {
		t.Error("Write tool should be disabled when write tool is specifically disabled")
	}
}

func TestWriteTool_Validate(t *testing.T) {
	cfg := config.DefaultConfig()
	tool := NewWriteTool(cfg)

	tests := []struct {
		name    string
		args    map[string]any
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid basic arguments",
			args: map[string]any{
				"file_path": "test.txt",
				"content":   "hello world",
			},
			wantErr: false,
		},
		{
			name: "valid with all optional arguments",
			args: map[string]any{
				"file_path":   "test.txt",
				"content":     "hello world",
				"create_dirs": true,
				"overwrite":   false,
				"format":      "json",
			},
			wantErr: false,
		},
		{
			name:    "missing file_path",
			args:    map[string]any{"content": "hello"},
			wantErr: true,
			errMsg:  "missing required parameter: file_path",
		},
		{
			name:    "missing content",
			args:    map[string]any{"file_path": "test.txt"},
			wantErr: true,
			errMsg:  "missing required parameter: content",
		},
		{
			name: "empty file_path",
			args: map[string]any{
				"file_path": "",
				"content":   "hello",
			},
			wantErr: true,
			errMsg:  "parameter file_path cannot be empty",
		},
		{
			name: "invalid file_path type",
			args: map[string]any{
				"file_path": 123,
				"content":   "hello",
			},
			wantErr: true,
			errMsg:  "parameter file_path must be a string, got int",
		},
		{
			name: "invalid content type",
			args: map[string]any{
				"file_path": "test.txt",
				"content":   123,
			},
			wantErr: true,
			errMsg:  "parameter content must be a string, got int",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(tt.args)
			validateError(t, err, tt.wantErr, tt.errMsg)
		})
	}
}

// validateError is a helper function to validate error expectations
func validateError(t *testing.T, err error, wantErr bool, errMsg string) {
	if wantErr {
		if err == nil {
			t.Errorf("Expected error but got none")
			return
		}
		if errMsg != "" && err.Error() != errMsg {
			t.Errorf("Expected error '%s', got '%s'", errMsg, err.Error())
		}
		return
	}
	if err != nil {
		t.Errorf("Expected no error but got: %v", err)
	}
}

func TestWriteTool_ValidateDisabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tools.Enabled = false
	tool := NewWriteTool(cfg)

	args := map[string]any{
		"file_path": "test.txt",
		"content":   "hello",
	}

	err := tool.Validate(args)
	if err == nil {
		t.Error("Expected error when tools are disabled")
	}
	if err.Error() != "write tool is not enabled" {
		t.Errorf("Expected 'write tool is not enabled', got '%s'", err.Error())
	}
}

func TestWriteTool_Execute(t *testing.T) {
	tempDir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Tools.Sandbox.Directories = []string{tempDir}
	tool := NewWriteTool(cfg)
	ctx := context.Background()

	t.Run("successful write to new file", func(t *testing.T) {
		testWriteNewFile(t, tempDir, tool, ctx)
	})

	t.Run("successful write with directory creation", func(t *testing.T) {
		testWriteWithDirCreation(t, tempDir, tool, ctx)
	})

	t.Run("successful overwrite existing file", func(t *testing.T) {
		testWriteOverwriteExisting(t, tempDir, tool, ctx)
	})

	t.Run("fail when overwrite is false and file exists", func(t *testing.T) {
		testWriteFailNoOverwrite(t, tempDir, tool, ctx)
	})

	t.Run("fail with invalid arguments", func(t *testing.T) {
		testWriteFailInvalidArgs(t, tool, ctx)
	})

	t.Run("fail when tools are disabled", func(t *testing.T) {
		testWriteFailDisabled(t, ctx)
	})
}

func testWriteNewFile(t *testing.T, tempDir string, tool *WriteTool, ctx context.Context) {
	filePath := filepath.Join(tempDir, "test1.txt")
	content := "Hello, World!"

	args := map[string]any{
		"file_path": filePath,
		"content":   content,
	}

	result, err := tool.Execute(ctx, args)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected success=true, got %v", result.Success)
	}

	if result.Error != "" {
		t.Errorf("Expected no error, got: %s", result.Error)
	}

	data, ok := result.Data.(*domain.FileWriteToolResult)
	if !ok {
		t.Fatalf("Expected FileWriteToolResult, got %T", result.Data)
	}

	if data.FilePath != filePath {
		t.Errorf("Expected file_path='%s', got '%s'", filePath, data.FilePath)
	}

	if data.BytesWritten != int64(len(content)) {
		t.Errorf("Expected bytes_written=%d, got %d", len(content), data.BytesWritten)
	}

	if !data.Created {
		t.Error("Expected created=true")
	}

	if data.Overwritten {
		t.Error("Expected overwritten=false")
	}

	writtenContent, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("Failed to read written file: %v", err)
	}

	if string(writtenContent) != content {
		t.Errorf("Expected content='%s', got '%s'", content, string(writtenContent))
	}
}

func testWriteWithDirCreation(t *testing.T, tempDir string, tool *WriteTool, ctx context.Context) {
	filePath := filepath.Join(tempDir, "subdir", "test2.txt")
	content := "Hello, Directory!"

	args := map[string]any{
		"file_path":   filePath,
		"content":     content,
		"create_dirs": true,
	}

	result, err := tool.Execute(ctx, args)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected success=true, got %v", result.Success)
	}

	// Directory creation is automatic and not tracked in this implementation

	writtenContent, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("Failed to read written file: %v", err)
	}

	if string(writtenContent) != content {
		t.Errorf("Expected content='%s', got '%s'", content, string(writtenContent))
	}
}

func testWriteOverwriteExisting(t *testing.T, tempDir string, tool *WriteTool, ctx context.Context) {
	filePath := filepath.Join(tempDir, "test3.txt")
	originalContent := "Original content"
	newContent := "New content"

	if err := os.WriteFile(filePath, []byte(originalContent), 0644); err != nil {
		t.Fatalf("Failed to create initial file: %v", err)
	}

	args := map[string]any{
		"file_path": filePath,
		"content":   newContent,
		"overwrite": true,
	}

	result, err := tool.Execute(ctx, args)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected success=true, got %v", result.Success)
	}

	data, ok := result.Data.(*domain.FileWriteToolResult)
	if !ok {
		t.Fatalf("Expected FileWriteToolResult, got %T", result.Data)
	}

	if data.Created {
		t.Error("Expected created=false")
	}

	if !data.Overwritten {
		t.Error("Expected overwritten=true")
	}

	writtenContent, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("Failed to read written file: %v", err)
	}

	if string(writtenContent) != newContent {
		t.Errorf("Expected content='%s', got '%s'", newContent, string(writtenContent))
	}
}

func testWriteFailNoOverwrite(t *testing.T, tempDir string, tool *WriteTool, ctx context.Context) {
	filePath := filepath.Join(tempDir, "test4.txt")
	originalContent := "Original content"
	newContent := "New content"

	if err := os.WriteFile(filePath, []byte(originalContent), 0644); err != nil {
		t.Fatalf("Failed to create initial file: %v", err)
	}

	args := map[string]any{
		"file_path": filePath,
		"content":   newContent,
	}

	result, err := tool.Execute(ctx, args)
	if err != nil {
		t.Fatalf("Execute should not return error: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected successful overwrite, got error: %s", result.Error)
	}

	writtenContent, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("Failed to read written file: %v", err)
	}

	if string(writtenContent) != newContent {
		t.Errorf("Expected file content '%s', got '%s'", newContent, string(writtenContent))
	}
}

func testWriteFailInvalidArgs(t *testing.T, tool *WriteTool, ctx context.Context) {
	args := map[string]any{
		"file_path": 123,
		"content":   "hello",
	}

	result, err := tool.Execute(ctx, args)
	if err != nil {
		t.Fatalf("Execute should not return error for validation failures: %v", err)
	}

	if result.Success {
		t.Error("Expected success=false")
	}

	if result.Error != "parameter extraction failed: parameter file_path must be a string, got int" {
		t.Errorf("Expected validation error, got: %s", result.Error)
	}
}

func testWriteFailDisabled(t *testing.T, ctx context.Context) {
	cfg := config.DefaultConfig()
	cfg.Tools.Enabled = false
	disabledTool := NewWriteTool(cfg)

	args := map[string]any{
		"file_path": "test.txt",
		"content":   "hello",
	}

	result, err := disabledTool.Execute(ctx, args)
	if err != nil {
		t.Fatalf("Execute should not return error: %v", err)
	}

	if result.Success {
		t.Error("Expected success=false when tools are disabled")
	}

	if result.Error != "write tool is not enabled" {
		t.Errorf("Expected 'write tool is not enabled', got: %s", result.Error)
	}
}

func TestWriteTool_PathSecurity(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "write-tool-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	cfg := config.DefaultConfig()
	cfg.Tools.Sandbox.Directories = []string{tempDir}
	tool := NewWriteTool(cfg)

	tests := []struct {
		name     string
		path     string
		allowed  bool
		errorMsg string
	}{
		{
			name:    "allowed path within sandbox",
			path:    filepath.Join(tempDir, "test.txt"),
			allowed: true,
		},
		{
			name:    "allowed subdirectory within sandbox",
			path:    filepath.Join(tempDir, "subdir/test.txt"),
			allowed: true,
		},
		{
			name:     "path outside sandbox",
			path:     "/etc/passwd",
			allowed:  false,
			errorMsg: "is outside configured sandbox directories",
		},
		{
			name:     "relative path outside sandbox",
			path:     "../outside.txt",
			allowed:  false,
			errorMsg: "path traversal attempts are not allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := map[string]any{
				"file_path": tt.path,
				"content":   "test content",
			}

			result, err := tool.Execute(context.Background(), args)
			if err != nil {
				t.Fatalf("Execute should not return error: %v", err)
			}

			validatePathResult(t, result, tt.allowed, tt.errorMsg)
		})
	}
}

func validatePathResult(t *testing.T, result *domain.ToolExecutionResult, allowed bool, errorMsg string) {
	if allowed {
		if !result.Success {
			t.Errorf("Path should be allowed but got error: %s", result.Error)
		}
		return
	}

	if result.Success {
		t.Error("Path should be blocked")
	}
	if errorMsg != "" && !strings.Contains(result.Error, errorMsg) {
		t.Errorf("Expected error to contain '%s', got '%s'", errorMsg, result.Error)
	}
}
