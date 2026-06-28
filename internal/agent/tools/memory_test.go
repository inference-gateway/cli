package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

func TestMemoryTool_Definition(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Memory.Enabled = true
	cfg.Prompts = *config.DefaultPromptsConfig()
	tool := NewMemoryTool(cfg)

	def := tool.Definition()
	if def.Function.Name != "Memory" {
		t.Errorf("Expected tool name 'Memory', got '%s'", def.Function.Name)
	}

	if *def.Function.Description == "" {
		t.Error("Tool description should not be empty")
	}

	if def.Function.Parameters == nil {
		t.Error("Tool parameters should not be nil")
	}
}

func TestMemoryTool_IsEnabled(t *testing.T) {
	cfg := config.DefaultConfig()
	tool := NewMemoryTool(cfg)

	if tool.IsEnabled() {
		t.Error("Memory tool should be disabled by default")
	}

	cfg.Memory.Enabled = true
	tool = NewMemoryTool(cfg)
	if !tool.IsEnabled() {
		t.Error("Memory tool should be enabled when memory.enabled is true")
	}
}

func TestMemoryTool_Validate(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Memory.Enabled = true
	tool := NewMemoryTool(cfg)

	tests := []struct {
		name    string
		args    map[string]any
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid read operation",
			args: map[string]any{
				"operation": "read",
			},
			wantErr: false,
		},
		{
			name: "valid append operation",
			args: map[string]any{
				"operation": "append",
				"content":   "new content",
			},
			wantErr: false,
		},
		{
			name: "valid replace operation",
			args: map[string]any{
				"operation": "replace",
				"content":   "new content",
			},
			wantErr: false,
		},
		{
			name: "valid remove operation",
			args: map[string]any{
				"operation": "remove",
			},
			wantErr: false,
		},
		{
			name: "missing operation",
			args: map[string]any{
				"content": "test",
			},
			wantErr: true,
			errMsg:  "operation",
		},
		{
			name: "invalid operation",
			args: map[string]any{
				"operation": "invalid",
			},
			wantErr: true,
			errMsg:  "operation",
		},
		{
			name: "append without content",
			args: map[string]any{
				"operation": "append",
			},
			wantErr: true,
			errMsg:  "content",
		},
		{
			name: "replace without content",
			args: map[string]any{
				"operation": "replace",
			},
			wantErr: true,
			errMsg:  "content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected error but got nil")
				} else if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("Expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestMemoryTool_Execute_Read(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Memory.Enabled = true
	cfg.Prompts = *config.DefaultPromptsConfig()

	dir := t.TempDir()
	memoryPath := filepath.Join(dir, "memory.md")
	cfg.Memory.Path = memoryPath

	content := "# Test Memory\n\nSome test content.\n"
	if err := os.WriteFile(memoryPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	tool := NewMemoryTool(cfg)
	result, err := tool.Execute(context.Background(), map[string]any{
		"operation": "read",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !result.Success {
		t.Fatalf("Expected success, got error: %s", result.Error)
	}
}

func TestMemoryTool_Execute_Append(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Memory.Enabled = true
	cfg.Prompts = *config.DefaultPromptsConfig()

	dir := t.TempDir()
	memoryPath := filepath.Join(dir, "memory.md")
	cfg.Memory.Path = memoryPath

	initialContent := "# Test Memory\n"
	if err := os.WriteFile(memoryPath, []byte(initialContent), 0644); err != nil {
		t.Fatal(err)
	}

	tool := NewMemoryTool(cfg)
	result, err := tool.Execute(context.Background(), map[string]any{
		"operation": "append",
		"content":   "Appended content.\n",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !result.Success {
		t.Fatalf("Expected success, got error: %s", result.Error)
	}

	data, err := os.ReadFile(memoryPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "# Test Memory\n\nAppended content.\n" {
		t.Errorf("Expected %q, got %q", initialContent+"Appended content.\n", string(data))
	}
}

func TestMemoryTool_Execute_Replace(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Memory.Enabled = true
	cfg.Prompts = *config.DefaultPromptsConfig()

	dir := t.TempDir()
	memoryPath := filepath.Join(dir, "memory.md")
	cfg.Memory.Path = memoryPath

	initialContent := "# Old Content\n\nOld data.\n"
	if err := os.WriteFile(memoryPath, []byte(initialContent), 0644); err != nil {
		t.Fatal(err)
	}

	tool := NewMemoryTool(cfg)
	newContent := "# New Content\n\nReplaced data.\n"
	result, err := tool.Execute(context.Background(), map[string]any{
		"operation": "replace",
		"content":   newContent,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !result.Success {
		t.Fatalf("Expected success, got error: %s", result.Error)
	}

	data, err := os.ReadFile(memoryPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != newContent {
		t.Errorf("Expected %q, got %q", newContent, string(data))
	}
}

func TestMemoryTool_Execute_Remove(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Memory.Enabled = true
	cfg.Prompts = *config.DefaultPromptsConfig()

	dir := t.TempDir()
	memoryPath := filepath.Join(dir, "memory.md")
	cfg.Memory.Path = memoryPath

	if err := os.WriteFile(memoryPath, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	tool := NewMemoryTool(cfg)
	result, err := tool.Execute(context.Background(), map[string]any{
		"operation": "remove",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !result.Success {
		t.Fatalf("Expected success, got error: %s", result.Error)
	}

	if _, err := os.Stat(memoryPath); !os.IsNotExist(err) {
		t.Error("Expected file to be removed")
	}
}

func TestMemoryTool_Execute_Read_NonExistent(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Memory.Enabled = true
	cfg.Prompts = *config.DefaultPromptsConfig()

	dir := t.TempDir()
	cfg.Memory.Path = filepath.Join(dir, "nonexistent.md")

	tool := NewMemoryTool(cfg)
	result, err := tool.Execute(context.Background(), map[string]any{
		"operation": "read",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !result.Success {
		t.Fatalf("Expected success for non-existent file read, got error: %s", result.Error)
	}
}
