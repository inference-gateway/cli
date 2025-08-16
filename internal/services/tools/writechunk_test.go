package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
)

func TestWriteChunkTool_Definition(t *testing.T) {
	cfg := config.DefaultConfig()
	tool := NewWriteChunkTool(cfg)

	def := tool.Definition()
	if def.Name != "WriteChunk" {
		t.Errorf("Expected tool name 'WriteChunk', got '%s'", def.Name)
	}

	if def.Description == "" {
		t.Error("Tool description should not be empty")
	}

	params, ok := def.Parameters.(map[string]interface{})
	if !ok {
		t.Fatal("Parameters should be a map")
	}

	props, ok := params["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("Properties should be a map")
	}

	required, ok := params["required"].([]string)
	if !ok {
		t.Fatal("Required should be a string slice")
	}

	if len(required) != 2 {
		t.Errorf("Expected 2 required parameters, got %d", len(required))
	}

	expectedFields := []string{"file_path", "content", "chunk_index", "total_chunks", "append_mode", "create_dirs", "overwrite", "format"}
	for _, field := range expectedFields {
		if _, exists := props[field]; !exists {
			t.Errorf("Expected field '%s' in properties", field)
		}
	}
}

func TestWriteChunkTool_IsEnabled(t *testing.T) {
	cfg := config.DefaultConfig()
	tool := NewWriteChunkTool(cfg)

	if !tool.IsEnabled() {
		t.Error("WriteChunk tool should be enabled by default")
	}

	cfg.Tools.Enabled = false
	tool = NewWriteChunkTool(cfg)
	if tool.IsEnabled() {
		t.Error("WriteChunk tool should be disabled when tools are globally disabled")
	}

	cfg.Tools.Enabled = true
	cfg.Tools.Write.Enabled = false
	tool = NewWriteChunkTool(cfg)
	if tool.IsEnabled() {
		t.Error("WriteChunk tool should be disabled when write tool is specifically disabled")
	}
}

func TestWriteChunkTool_Validate_ValidArgs(t *testing.T) {
	cfg := config.DefaultConfig()
	tool := NewWriteChunkTool(cfg)

	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "valid append mode arguments",
			args: map[string]interface{}{
				"file_path":   "test.txt",
				"content":     "hello world",
				"append_mode": true,
			},
		},
		{
			name: "valid indexed mode arguments",
			args: map[string]interface{}{
				"file_path":    "test.txt",
				"content":      "hello world",
				"append_mode":  false,
				"chunk_index":  float64(0),
				"total_chunks": float64(3),
			},
		},
		{
			name: "valid with all optional arguments",
			args: map[string]interface{}{
				"file_path":    "test.txt",
				"content":      "hello world",
				"append_mode":  false,
				"chunk_index":  float64(1),
				"total_chunks": float64(3),
				"create_dirs":  true,
				"overwrite":    false,
				"format":       "json",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(tt.args)
			if err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}
		})
	}
}

func TestWriteChunkTool_Validate_InvalidArgs(t *testing.T) {
	cfg := config.DefaultConfig()
	tool := NewWriteChunkTool(cfg)

	tests := []struct {
		name   string
		args   map[string]interface{}
		errMsg string
	}{
		{
			name:   "missing file_path",
			args:   map[string]interface{}{"content": "hello"},
			errMsg: "file_path parameter is required and must be a string",
		},
		{
			name:   "missing content",
			args:   map[string]interface{}{"file_path": "test.txt"},
			errMsg: "content parameter is required and must be a string",
		},
		{
			name: "empty file_path",
			args: map[string]interface{}{
				"file_path": "",
				"content":   "hello",
			},
			errMsg: "file_path cannot be empty",
		},
		{
			name: "invalid file_path type",
			args: map[string]interface{}{
				"file_path": 123,
				"content":   "hello",
			},
			errMsg: "file_path parameter is required and must be a string",
		},
		{
			name: "invalid content type",
			args: map[string]interface{}{
				"file_path": "test.txt",
				"content":   123,
			},
			errMsg: "content parameter is required and must be a string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(tt.args)
			validateError(t, err, true, tt.errMsg)
		})
	}
}

func TestWriteChunkTool_Validate_IndexedMode(t *testing.T) {
	cfg := config.DefaultConfig()
	tool := NewWriteChunkTool(cfg)

	tests := []struct {
		name   string
		args   map[string]interface{}
		errMsg string
	}{
		{
			name: "indexed mode missing chunk_index",
			args: map[string]interface{}{
				"file_path":    "test.txt",
				"content":      "hello",
				"append_mode":  false,
				"total_chunks": float64(3),
			},
			errMsg: "chunk_index and total_chunks are required when append_mode is false",
		},
		{
			name: "indexed mode missing total_chunks",
			args: map[string]interface{}{
				"file_path":   "test.txt",
				"content":     "hello",
				"append_mode": false,
				"chunk_index": float64(0),
			},
			errMsg: "chunk_index and total_chunks are required when append_mode is false",
		},
		{
			name: "invalid chunk_index type",
			args: map[string]interface{}{
				"file_path":    "test.txt",
				"content":      "hello",
				"append_mode":  false,
				"chunk_index":  "0",
				"total_chunks": float64(3),
			},
			errMsg: "chunk_index must be a number",
		},
		{
			name: "invalid total_chunks type",
			args: map[string]interface{}{
				"file_path":    "test.txt",
				"content":      "hello",
				"append_mode":  false,
				"chunk_index":  float64(0),
				"total_chunks": "3",
			},
			errMsg: "total_chunks must be a number",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(tt.args)
			validateError(t, err, true, tt.errMsg)
		})
	}
}

func TestWriteChunkTool_Validate_ParameterTypes(t *testing.T) {
	cfg := config.DefaultConfig()
	tool := NewWriteChunkTool(cfg)

	tests := []struct {
		name   string
		args   map[string]interface{}
		errMsg string
	}{
		{
			name: "invalid append_mode type",
			args: map[string]interface{}{
				"file_path":   "test.txt",
				"content":     "hello",
				"append_mode": "true",
			},
			errMsg: "append_mode parameter must be a boolean",
		},
		{
			name: "invalid create_dirs type",
			args: map[string]interface{}{
				"file_path":   "test.txt",
				"content":     "hello",
				"create_dirs": "true",
			},
			errMsg: "create_dirs parameter must be a boolean",
		},
		{
			name: "invalid overwrite type",
			args: map[string]interface{}{
				"file_path": "test.txt",
				"content":   "hello",
				"overwrite": "false",
			},
			errMsg: "overwrite parameter must be a boolean",
		},
		{
			name: "invalid format value",
			args: map[string]interface{}{
				"file_path": "test.txt",
				"content":   "hello",
				"format":    "xml",
			},
			errMsg: "format must be 'text' or 'json'",
		},
		{
			name: "invalid format type",
			args: map[string]interface{}{
				"file_path": "test.txt",
				"content":   "hello",
				"format":    123,
			},
			errMsg: "format parameter must be a string",
		},
		{
			name: "excluded path",
			args: map[string]interface{}{
				"file_path": ".infer/test.txt",
				"content":   "hello",
			},
			errMsg: "access to path '.infer/test.txt' is excluded for security",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(tt.args)
			validateError(t, err, true, tt.errMsg)
		})
	}
}

func TestWriteChunkTool_ValidateDisabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tools.Enabled = false
	tool := NewWriteChunkTool(cfg)

	args := map[string]interface{}{
		"file_path": "test.txt",
		"content":   "hello",
	}

	err := tool.Validate(args)
	if err == nil {
		t.Error("Expected error when tools are disabled")
	}
	if err.Error() != "write chunk tool is not enabled" {
		t.Errorf("Expected 'write chunk tool is not enabled', got '%s'", err.Error())
	}
}

func TestWriteChunkTool_Execute_AppendMode(t *testing.T) {
	tempDir := t.TempDir()
	cfg := config.DefaultConfig()
	tool := NewWriteChunkTool(cfg)
	ctx := context.Background()

	t.Run("successful append mode writing", func(t *testing.T) {
		filePath := filepath.Join(tempDir, "append_test.txt")
		chunks := []string{"Hello", " ", "World", "!"}

		for i, chunk := range chunks {
			args := map[string]interface{}{
				"file_path":   filePath,
				"content":     chunk,
				"append_mode": true,
			}
			
			// Only set overwrite to false after the first chunk to prevent overwriting
			if i > 0 {
				args["overwrite"] = false
			}

			result, err := tool.Execute(ctx, args)
			if err != nil {
				t.Fatalf("Execute failed on chunk %d: %v", i, err)
			}

			if !result.Success {
				t.Errorf("Expected success=true on chunk %d, got %v", i, result.Success)
			}

			data, ok := result.Data.(*domain.FileWriteChunkToolResult)
			if !ok {
				t.Fatalf("Expected FileWriteChunkToolResult on chunk %d, got %T", i, result.Data)
			}

			if !data.AppendMode {
				t.Errorf("Expected append_mode=true on chunk %d", i)
			}

			if !data.IsComplete {
				t.Errorf("Expected is_complete=true in append mode on chunk %d", i)
			}

			if i == 0 && !data.Created {
				t.Error("Expected created=true on first chunk")
			}
		}

		// Verify final content
		content, err := os.ReadFile(filePath)
		if err != nil {
			t.Fatalf("Failed to read final file: %v", err)
		}

		expected := "Hello World!"
		if string(content) != expected {
			t.Errorf("Expected final content='%s', got '%s'", expected, string(content))
		}
	})
}

func TestWriteChunkTool_Execute_IndexedMode(t *testing.T) {
	tempDir := t.TempDir()
	cfg := config.DefaultConfig()
	tool := NewWriteChunkTool(cfg)
	ctx := context.Background()

	t.Run("successful indexed mode writing", func(t *testing.T) {
		filePath := filepath.Join(tempDir, "indexed_test.txt")
		chunks := []string{"First", "Second", "Third"}
		totalChunks := len(chunks)

		// Write chunks out of order to test indexing
		order := []int{1, 0, 2}

		for i, idx := range order {
			args := map[string]interface{}{
				"file_path":    filePath,
				"content":     chunks[idx],
				"append_mode":  false,
				"chunk_index":  float64(idx),
				"total_chunks": float64(totalChunks),
			}

			result, err := tool.Execute(ctx, args)
			if err != nil {
				t.Fatalf("Execute failed on chunk %d: %v", idx, err)
			}

			if !result.Success {
				t.Errorf("Expected success=true on chunk %d, got %v", idx, result.Success)
			}

			data, ok := result.Data.(*domain.FileWriteChunkToolResult)
			if !ok {
				t.Fatalf("Expected FileWriteChunkToolResult on chunk %d, got %T", idx, result.Data)
			}

			if data.AppendMode {
				t.Errorf("Expected append_mode=false on chunk %d", idx)
			}

			if data.ChunkIndex != idx {
				t.Errorf("Expected chunk_index=%d on chunk %d, got %d", idx, idx, data.ChunkIndex)
			}

			if data.TotalChunks != totalChunks {
				t.Errorf("Expected total_chunks=%d on chunk %d, got %d", totalChunks, idx, data.TotalChunks)
			}

			// Only the last chunk should be complete
			isLastChunk := i == len(order)-1
			if data.IsComplete != isLastChunk {
				t.Errorf("Expected is_complete=%v on chunk %d, got %v", isLastChunk, idx, data.IsComplete)
			}

			// Only after the last chunk should the final file exist
			if isLastChunk {
				if _, err := os.Stat(filePath); err != nil {
					t.Errorf("Final file should exist after last chunk: %v", err)
				}
			}
		}

		// Verify final content (should be in order regardless of write order)
		content, err := os.ReadFile(filePath)
		if err != nil {
			t.Fatalf("Failed to read final file: %v", err)
		}

		expected := "FirstSecondThird"
		if string(content) != expected {
			t.Errorf("Expected final content='%s', got '%s'", expected, string(content))
		}
	})
}

func TestWriteChunkTool_Execute_WithDirectoryCreation(t *testing.T) {
	tempDir := t.TempDir()
	cfg := config.DefaultConfig()
	tool := NewWriteChunkTool(cfg)
	ctx := context.Background()

	filePath := filepath.Join(tempDir, "newdir", "test.txt")
	args := map[string]interface{}{
		"file_path":   filePath,
		"content":     "Hello",
		"create_dirs": true,
	}

	result, err := tool.Execute(ctx, args)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected success=true, got %v", result.Success)
	}

	data, ok := result.Data.(*domain.FileWriteChunkToolResult)
	if !ok {
		t.Fatalf("Expected FileWriteChunkToolResult, got %T", result.Data)
	}

	if !data.DirsCreated {
		t.Error("Expected dirs_created=true")
	}

	// Verify file was created
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("Failed to read created file: %v", err)
	}

	if string(content) != "Hello" {
		t.Errorf("Expected content='Hello', got '%s'", string(content))
	}
}

func TestWriteChunkTool_Execute_OverwriteExisting(t *testing.T) {
	tempDir := t.TempDir()
	cfg := config.DefaultConfig()
	tool := NewWriteChunkTool(cfg)
	ctx := context.Background()

	filePath := filepath.Join(tempDir, "overwrite_test.txt")

	// Create initial file
	if err := os.WriteFile(filePath, []byte("original"), 0644); err != nil {
		t.Fatalf("Failed to create initial file: %v", err)
	}

	args := map[string]interface{}{
		"file_path": filePath,
		"content":   "new content",
		"overwrite": true,
	}

	result, err := tool.Execute(ctx, args)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected success=true, got %v", result.Success)
	}

	data, ok := result.Data.(*domain.FileWriteChunkToolResult)
	if !ok {
		t.Fatalf("Expected FileWriteChunkToolResult, got %T", result.Data)
	}

	if !data.Overwritten {
		t.Error("Expected overwritten=true")
	}

	if data.Created {
		t.Error("Expected created=false")
	}

	// Verify content was overwritten
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("Failed to read overwritten file: %v", err)
	}

	if string(content) != "new content" {
		t.Errorf("Expected content='new content', got '%s'", string(content))
	}
}

func TestWriteChunkTool_Execute_FailInvalidArgs(t *testing.T) {
	cfg := config.DefaultConfig()
	tool := NewWriteChunkTool(cfg)
	ctx := context.Background()

	tests := []struct {
		name     string
		args     map[string]interface{}
		errorMsg string
	}{
		{
			name: "invalid file_path type",
			args: map[string]interface{}{
				"file_path": 123,
				"content":   "hello",
			},
			errorMsg: "file_path parameter is required and must be a string",
		},
		{
			name: "invalid content type",
			args: map[string]interface{}{
				"file_path": "test.txt",
				"content":   123,
			},
			errorMsg: "content parameter is required and must be a string",
		},
		{
			name: "indexed mode with invalid chunk_index",
			args: map[string]interface{}{
				"file_path":    "test.txt",
				"content":      "hello",
				"append_mode":  false,
				"chunk_index":  float64(5),
				"total_chunks": float64(3),
			},
			errorMsg: "chunk_index (5) must be between 0 and 2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tool.Execute(ctx, tt.args)
			if err != nil {
				t.Fatalf("Execute should not return error for validation failures: %v", err)
			}

			if result.Success {
				t.Error("Expected success=false")
			}

			if result.Error != tt.errorMsg {
				t.Errorf("Expected error '%s', got '%s'", tt.errorMsg, result.Error)
			}
		})
	}
}

func TestWriteChunkTool_Execute_FailDisabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tools.Enabled = false
	tool := NewWriteChunkTool(cfg)
	ctx := context.Background()

	args := map[string]interface{}{
		"file_path": "test.txt",
		"content":   "hello",
	}

	_, err := tool.Execute(ctx, args)
	if err == nil {
		t.Error("Expected error when tools are disabled")
	}

	if err.Error() != "write chunk tool is not enabled" {
		t.Errorf("Expected 'write chunk tool is not enabled', got '%s'", err.Error())
	}
}

func TestWriteChunkTool_PathSecurity(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tools.ExcludePaths = []string{".infer/", "*.secret", "/etc/*"}
	tool := NewWriteChunkTool(cfg)

	tests := []struct {
		name     string
		path     string
		allowed  bool
		errorMsg string
	}{
		{
			name:    "allowed path",
			path:    "test.txt",
			allowed: true,
		},
		{
			name:    "allowed subdirectory",
			path:    "subdir/test.txt",
			allowed: true,
		},
		{
			name:     "excluded .infer directory",
			path:     ".infer/config.yaml",
			allowed:  false,
			errorMsg: "access to path '.infer/config.yaml' is excluded for security",
		},
		{
			name:     "excluded pattern *.secret",
			path:     "database.secret",
			allowed:  false,
			errorMsg: "access to path 'database.secret' is excluded for security",
		},
		{
			name:     "excluded pattern /etc/*",
			path:     "/etc/passwd",
			allowed:  false,
			errorMsg: "access to path '/etc/passwd' is excluded for security",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := map[string]interface{}{
				"file_path": tt.path,
				"content":   "test content",
			}

			err := tool.Validate(args)
			validatePathSecurity(t, err, tt.allowed, tt.errorMsg)
		})
	}
}