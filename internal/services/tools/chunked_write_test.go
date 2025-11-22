package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
)

func TestWriteStartTool_Definition(t *testing.T) {
	cfg := config.DefaultConfig()
	tool := NewWriteStartTool(cfg)

	def := tool.Definition()
	if def.Function.Name != "WriteStart" {
		t.Errorf("Expected tool name 'WriteStart', got '%s'", def.Function.Name)
	}

	if *def.Function.Description == "" {
		t.Error("Tool description should not be empty")
	}

	if def.Function.Parameters == nil {
		t.Error("Tool parameters should not be nil")
	}
}

func TestWriteAppendTool_Definition(t *testing.T) {
	cfg := config.DefaultConfig()
	tool := NewWriteAppendTool(cfg)

	def := tool.Definition()
	if def.Function.Name != "WriteAppend" {
		t.Errorf("Expected tool name 'WriteAppend', got '%s'", def.Function.Name)
	}

	if *def.Function.Description == "" {
		t.Error("Tool description should not be empty")
	}
}

func TestWriteCompleteTool_Definition(t *testing.T) {
	cfg := config.DefaultConfig()
	tool := NewWriteCompleteTool(cfg)

	def := tool.Definition()
	if def.Function.Name != "WriteComplete" {
		t.Errorf("Expected tool name 'WriteComplete', got '%s'", def.Function.Name)
	}

	if *def.Function.Description == "" {
		t.Error("Tool description should not be empty")
	}
}

func TestChunkedWriteTools_IsEnabled(t *testing.T) {
	cfg := config.DefaultConfig()

	startTool := NewWriteStartTool(cfg)
	appendTool := NewWriteAppendTool(cfg)
	completeTool := NewWriteCompleteTool(cfg)

	if !startTool.IsEnabled() {
		t.Error("WriteStart tool should be enabled by default")
	}
	if !appendTool.IsEnabled() {
		t.Error("WriteAppend tool should be enabled by default")
	}
	if !completeTool.IsEnabled() {
		t.Error("WriteComplete tool should be enabled by default")
	}

	// Test disabled when tools are globally disabled
	cfg.Tools.Enabled = false
	startTool = NewWriteStartTool(cfg)
	appendTool = NewWriteAppendTool(cfg)
	completeTool = NewWriteCompleteTool(cfg)

	if startTool.IsEnabled() {
		t.Error("WriteStart tool should be disabled when tools are globally disabled")
	}
	if appendTool.IsEnabled() {
		t.Error("WriteAppend tool should be disabled when tools are globally disabled")
	}
	if completeTool.IsEnabled() {
		t.Error("WriteComplete tool should be disabled when tools are globally disabled")
	}
}

func TestChunkedWriteWorkflow(t *testing.T) {
	tempDir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Tools.Sandbox.Directories = []string{tempDir}

	startTool := NewWriteStartTool(cfg)
	appendTool := NewWriteAppendTool(cfg)
	completeTool := NewWriteCompleteTool(cfg)
	ctx := context.Background()

	filePath := filepath.Join(tempDir, "chunked_test.txt")

	// Step 1: WriteStart
	t.Run("WriteStart creates new session", func(t *testing.T) {
		args := map[string]any{
			"file_path": filePath,
			"content":   "Line 1\n",
		}

		result, err := startTool.Execute(ctx, args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		if !result.Success {
			t.Errorf("Expected success=true, got error: %s", result.Error)
		}

		data, ok := result.Data.(*domain.FileWriteToolResult)
		if !ok {
			t.Fatalf("Expected FileWriteToolResult, got %T", result.Data)
		}

		if data.ChunkIndex != 1 {
			t.Errorf("Expected chunk_index=1, got %d", data.ChunkIndex)
		}

		if data.IsComplete {
			t.Error("Expected is_complete=false")
		}
	})

	// Step 2: WriteAppend
	t.Run("WriteAppend adds to session", func(t *testing.T) {
		args := map[string]any{
			"file_path": filePath,
			"content":   "Line 2\n",
		}

		result, err := appendTool.Execute(ctx, args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		if !result.Success {
			t.Errorf("Expected success=true, got error: %s", result.Error)
		}

		data, ok := result.Data.(*domain.FileWriteToolResult)
		if !ok {
			t.Fatalf("Expected FileWriteToolResult, got %T", result.Data)
		}

		if data.ChunkIndex != 2 {
			t.Errorf("Expected chunk_index=2, got %d", data.ChunkIndex)
		}

		if !data.Appended {
			t.Error("Expected appended=true")
		}
	})

	// Step 3: WriteAppend again
	t.Run("WriteAppend adds more content", func(t *testing.T) {
		args := map[string]any{
			"file_path": filePath,
			"content":   "Line 3\n",
		}

		result, err := appendTool.Execute(ctx, args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		if !result.Success {
			t.Errorf("Expected success=true, got error: %s", result.Error)
		}

		data, ok := result.Data.(*domain.FileWriteToolResult)
		if !ok {
			t.Fatalf("Expected FileWriteToolResult, got %T", result.Data)
		}

		if data.ChunkIndex != 3 {
			t.Errorf("Expected chunk_index=3, got %d", data.ChunkIndex)
		}
	})

	// Step 4: WriteComplete
	t.Run("WriteComplete finalizes file", func(t *testing.T) {
		args := map[string]any{
			"file_path": filePath,
		}

		result, err := completeTool.Execute(ctx, args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		if !result.Success {
			t.Errorf("Expected success=true, got error: %s", result.Error)
		}

		data, ok := result.Data.(*domain.FileWriteToolResult)
		if !ok {
			t.Fatalf("Expected FileWriteToolResult, got %T", result.Data)
		}

		if !data.IsComplete {
			t.Error("Expected is_complete=true")
		}

		if data.TotalChunks != 3 {
			t.Errorf("Expected total_chunks=3, got %d", data.TotalChunks)
		}

		if !data.Created {
			t.Error("Expected created=true for new file")
		}

		// Verify file content
		content, err := os.ReadFile(filePath)
		if err != nil {
			t.Fatalf("Failed to read file: %v", err)
		}

		expected := "Line 1\nLine 2\nLine 3\n"
		if string(content) != expected {
			t.Errorf("Expected content='%s', got '%s'", expected, string(content))
		}
	})
}

func TestWriteStart_Validation(t *testing.T) {
	cfg := config.DefaultConfig()
	tool := NewWriteStartTool(cfg)

	tests := []struct {
		name    string
		args    map[string]any
		wantErr bool
	}{
		{
			name: "valid arguments",
			args: map[string]any{
				"file_path": "/tmp/test.txt",
				"content":   "hello world",
			},
			wantErr: false,
		},
		{
			name: "missing file_path",
			args: map[string]any{
				"content": "hello",
			},
			wantErr: true,
		},
		{
			name: "missing content",
			args: map[string]any{
				"file_path": "/tmp/test.txt",
			},
			wantErr: true,
		},
		{
			name: "empty file_path",
			args: map[string]any{
				"file_path": "",
				"content":   "hello",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestWriteAppend_NoSession(t *testing.T) {
	cfg := config.DefaultConfig()
	tool := NewWriteAppendTool(cfg)
	ctx := context.Background()

	args := map[string]any{
		"file_path": "/tmp/nonexistent_session.txt",
		"content":   "hello",
	}

	result, err := tool.Execute(ctx, args)
	if err != nil {
		t.Fatalf("Execute should not return error: %v", err)
	}

	if result.Success {
		t.Error("Expected success=false when no session exists")
	}

	if result.Error == "" {
		t.Error("Expected error message about missing session")
	}
}

func TestWriteComplete_NoSession(t *testing.T) {
	cfg := config.DefaultConfig()
	tool := NewWriteCompleteTool(cfg)
	ctx := context.Background()

	args := map[string]any{
		"file_path": "/tmp/nonexistent_session.txt",
	}

	result, err := tool.Execute(ctx, args)
	if err != nil {
		t.Fatalf("Execute should not return error: %v", err)
	}

	if result.Success {
		t.Error("Expected success=false when no session exists")
	}

	if result.Error == "" {
		t.Error("Expected error message about missing session")
	}
}

func TestWriteStart_DuplicateSession(t *testing.T) {
	tempDir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Tools.Sandbox.Directories = []string{tempDir}

	startTool := NewWriteStartTool(cfg)
	ctx := context.Background()

	filePath := filepath.Join(tempDir, "duplicate_test.txt")

	// First WriteStart should succeed
	args := map[string]any{
		"file_path": filePath,
		"content":   "First chunk",
	}

	result, err := startTool.Execute(ctx, args)
	if err != nil {
		t.Fatalf("First Execute failed: %v", err)
	}
	if !result.Success {
		t.Fatalf("First Execute should succeed: %s", result.Error)
	}

	// Second WriteStart for same file should fail
	result, err = startTool.Execute(ctx, args)
	if err != nil {
		t.Fatalf("Second Execute should not return error: %v", err)
	}
	if result.Success {
		t.Error("Second WriteStart for same file should fail")
	}
	if result.Error == "" {
		t.Error("Expected error about existing session")
	}

	// Clean up session
	chunkedWriteManager.RemoveSession(filePath)
}

func TestChunkedWriteManager(t *testing.T) {
	manager := &ChunkedWriteManager{
		sessions: make(map[string]*ChunkedWriteSession),
	}

	t.Run("CreateSession", func(t *testing.T) {
		session := manager.CreateSession("/test/file.txt", "/tmp/file.chunk")
		if session == nil {
			t.Fatal("CreateSession should return a session")
		}
		if session.FilePath != "/test/file.txt" {
			t.Errorf("Expected FilePath='/test/file.txt', got '%s'", session.FilePath)
		}
		if session.TempPath != "/tmp/file.chunk" {
			t.Errorf("Expected TempPath='/tmp/file.chunk', got '%s'", session.TempPath)
		}
	})

	t.Run("GetSession", func(t *testing.T) {
		session, exists := manager.GetSession("/test/file.txt")
		if !exists {
			t.Error("Session should exist")
		}
		if session == nil {
			t.Error("Session should not be nil")
		}

		_, exists = manager.GetSession("/nonexistent.txt")
		if exists {
			t.Error("Nonexistent session should not be found")
		}
	})

	t.Run("UpdateSession", func(t *testing.T) {
		manager.UpdateSession("/test/file.txt", 100)
		session, _ := manager.GetSession("/test/file.txt")
		if session.ChunksCount != 1 {
			t.Errorf("Expected ChunksCount=1, got %d", session.ChunksCount)
		}
		if session.TotalBytes != 100 {
			t.Errorf("Expected TotalBytes=100, got %d", session.TotalBytes)
		}
	})

	t.Run("CompleteSession", func(t *testing.T) {
		session := manager.CompleteSession("/test/file.txt")
		if session == nil {
			t.Fatal("CompleteSession should return the session")
		}
		if !session.IsComplete {
			t.Error("Session should be marked complete")
		}

		_, exists := manager.GetSession("/test/file.txt")
		if exists {
			t.Error("Session should be removed after completion")
		}
	})

	t.Run("RemoveSession", func(t *testing.T) {
		manager.CreateSession("/another/file.txt", "/tmp/another.chunk")
		manager.RemoveSession("/another/file.txt")

		_, exists := manager.GetSession("/another/file.txt")
		if exists {
			t.Error("Session should be removed")
		}
	})
}

func TestChunkedWriteTools_FormatResult(t *testing.T) {
	cfg := config.DefaultConfig()

	t.Run("WriteStart FormatResult", func(t *testing.T) {
		tool := NewWriteStartTool(cfg)
		result := &domain.ToolExecutionResult{
			Success: true,
			Data: &domain.FileWriteToolResult{
				FilePath:     "/test/file.txt",
				BytesWritten: 100,
				ChunkIndex:   1,
			},
		}

		formatted := tool.FormatResult(result, domain.FormatterLLM)
		if formatted == "" {
			t.Error("FormatResult should not return empty string")
		}
	})

	t.Run("WriteAppend FormatResult", func(t *testing.T) {
		tool := NewWriteAppendTool(cfg)
		result := &domain.ToolExecutionResult{
			Success: true,
			Data: &domain.FileWriteToolResult{
				FilePath:     "/test/file.txt",
				BytesWritten: 100,
				ChunkIndex:   2,
			},
		}

		formatted := tool.FormatResult(result, domain.FormatterLLM)
		if formatted == "" {
			t.Error("FormatResult should not return empty string")
		}
	})

	t.Run("WriteComplete FormatResult", func(t *testing.T) {
		tool := NewWriteCompleteTool(cfg)
		result := &domain.ToolExecutionResult{
			Success: true,
			Data: &domain.FileWriteToolResult{
				FilePath:     "/test/file.txt",
				BytesWritten: 300,
				TotalChunks:  3,
				Created:      true,
			},
		}

		formatted := tool.FormatResult(result, domain.FormatterLLM)
		if formatted == "" {
			t.Error("FormatResult should not return empty string")
		}
	})
}

func TestChunkedWriteTools_ShouldCollapseArg(t *testing.T) {
	cfg := config.DefaultConfig()

	startTool := NewWriteStartTool(cfg)
	appendTool := NewWriteAppendTool(cfg)
	completeTool := NewWriteCompleteTool(cfg)

	// WriteStart and WriteAppend should collapse "content"
	if !startTool.ShouldCollapseArg("content") {
		t.Error("WriteStart should collapse 'content' argument")
	}
	if !appendTool.ShouldCollapseArg("content") {
		t.Error("WriteAppend should collapse 'content' argument")
	}

	// WriteComplete has no content argument, so nothing to collapse
	if completeTool.ShouldCollapseArg("file_path") {
		t.Error("WriteComplete should not collapse 'file_path' argument")
	}
}
