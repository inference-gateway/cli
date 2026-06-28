package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	filewriter "github.com/inference-gateway/cli/internal/domain/filewriter"
	filewriterservice "github.com/inference-gateway/cli/internal/services/filewriter"
	sdk "github.com/inference-gateway/sdk"
)

const (
	ToolNameMemory = "Memory"

	OperationRead    = "read"
	OperationAppend  = "append"
	OperationReplace = "replace"
	OperationRemove  = "remove"
)

// MemoryTool implements persistent agent memory with read/append/replace/remove operations.
type MemoryTool struct {
	config    *config.Config
	enabled   bool
	formatter domain.CustomFormatter
	writer    filewriter.FileWriter
}

// NewMemoryTool creates a new memory tool with clean architecture.
func NewMemoryTool(cfg *config.Config) *MemoryTool {
	pathValidator := filewriterservice.NewPathValidator(cfg)
	backupManager := filewriterservice.NewBackupManager(".")
	fileWriter := filewriterservice.NewSafeFileWriter(pathValidator, backupManager)

	return &MemoryTool{
		config:  cfg,
		enabled: cfg.Memory.Enabled,
		formatter: domain.NewCustomFormatter(ToolNameMemory, func(key string) bool {
			return key == "content"
		}),
		writer: fileWriter,
	}
}

// Definition returns the tool definition for the LLM.
func (t *MemoryTool) Definition() sdk.ChatCompletionTool {
	description := t.config.Prompts.Tools.Memory.Description
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        ToolNameMemory,
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"operation": map[string]any{
						"type": "string",
						"enum": []string{OperationRead, OperationAppend, OperationReplace, OperationRemove},
						"description": "The memory operation to perform. " +
							"read: return the full contents of the memory file(s). " +
							"append: add new content to the memory file. " +
							"replace: overwrite the memory file with new content. " +
							"remove: delete the memory file entirely.",
					},
					"content": map[string]any{
						"type":        "string",
						"description": "The content to append or replace. Required for append and replace operations.",
					},
				},
				"required": []string{"operation"},
			},
		},
	}
}

// Execute runs the memory tool with given arguments.
func (t *MemoryTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	start := time.Now()

	if !t.enabled {
		return &domain.ToolExecutionResult{
			ToolName:  ToolNameMemory,
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "memory tool is not enabled",
		}, nil
	}

	operation, ok := args["operation"].(string)
	if !ok {
		return &domain.ToolExecutionResult{
			ToolName:  ToolNameMemory,
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "operation parameter is required and must be a string",
		}, nil
	}

	content, _ := args["content"].(string)

	switch operation {
	case OperationRead:
		return t.execRead(ctx, args, start)
	case OperationAppend:
		return t.execAppend(ctx, args, content, start)
	case OperationReplace:
		return t.execReplace(ctx, args, content, start)
	case OperationRemove:
		return t.execRemove(ctx, args, start)
	default:
		return &domain.ToolExecutionResult{
			ToolName:  ToolNameMemory,
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     fmt.Sprintf("unknown operation: %s", operation),
		}, nil
	}
}

// execRead reads the memory file(s) and returns their content.
func (t *MemoryTool) execRead(ctx context.Context, args map[string]any, start time.Time) (*domain.ToolExecutionResult, error) {
	primaryPath, err := t.config.ResolveMemoryPath()
	if err != nil {
		return &domain.ToolExecutionResult{
			ToolName:  ToolNameMemory,
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     fmt.Sprintf("failed to resolve memory path: %v", err),
		}, nil
	}

	var parts []string

	// Read primary memory file
	primaryContent, err := os.ReadFile(primaryPath)
	if err == nil {
		parts = append(parts, fmt.Sprintf("=== %s ===\n%s", primaryPath, string(primaryContent)))
	} else if !os.IsNotExist(err) {
		return &domain.ToolExecutionResult{
			ToolName:  ToolNameMemory,
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     fmt.Sprintf("failed to read memory file %s: %v", primaryPath, err),
		}, nil
	}

	// Read user memory file if configured
	if t.config.Memory.UserPath != "" {
		userContent, err := os.ReadFile(t.config.Memory.UserPath)
		if err == nil {
			parts = append(parts, fmt.Sprintf("=== %s ===\n%s", t.config.Memory.UserPath, string(userContent)))
		} else if !os.IsNotExist(err) {
			return &domain.ToolExecutionResult{
				ToolName:  ToolNameMemory,
				Arguments: args,
				Success:   false,
				Duration:  time.Since(start),
				Error:     fmt.Sprintf("failed to read user memory file %s: %v", t.config.Memory.UserPath, err),
			}, nil
		}
	}

	combined := strings.Join(parts, "\n\n")

	return &domain.ToolExecutionResult{
		ToolName:  ToolNameMemory,
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data: &MemoryToolResult{
			Operation: OperationRead,
			Content:   combined,
			Size:      int64(len(combined)),
		},
	}, nil
}

// execAppend reads existing content and appends new content.
func (t *MemoryTool) execAppend(ctx context.Context, args map[string]any, content string, start time.Time) (*domain.ToolExecutionResult, error) {
	if content == "" {
		return &domain.ToolExecutionResult{
			ToolName:  ToolNameMemory,
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "content is required for append operation",
		}, nil
	}

	path, err := t.config.ResolveMemoryPath()
	if err != nil {
		return &domain.ToolExecutionResult{
			ToolName:  ToolNameMemory,
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     fmt.Sprintf("failed to resolve memory path: %v", err),
		}, nil
	}

	// Read existing content
	existingContent := ""
	existing, err := os.ReadFile(path)
	if err == nil {
		existingContent = string(existing)
	} else if !os.IsNotExist(err) {
		return &domain.ToolExecutionResult{
			ToolName:  ToolNameMemory,
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     fmt.Sprintf("failed to read existing memory file: %v", err),
		}, nil
	}

	// Append new content
	var newContent string
	if existingContent != "" {
		newContent = existingContent + "\n" + content
	} else {
		newContent = content
	}

	writeResult, err := t.writer.Write(ctx, filewriter.WriteRequest{
		Path:      path,
		Content:   newContent,
		Overwrite: true,
		Backup:    true,
	})
	if err != nil {
		return &domain.ToolExecutionResult{
			ToolName:  ToolNameMemory,
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     fmt.Sprintf("failed to write memory file: %v", err),
		}, nil
	}

	return &domain.ToolExecutionResult{
		ToolName:  ToolNameMemory,
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data: &MemoryToolResult{
			Operation:  OperationAppend,
			Content:    content,
			Size:       writeResult.BytesWritten,
			FilePath:   writeResult.Path,
			BackupPath: writeResult.BackupPath,
		},
	}, nil
}

// execReplace writes new content via SafeFileWriter with backup.
func (t *MemoryTool) execReplace(ctx context.Context, args map[string]any, content string, start time.Time) (*domain.ToolExecutionResult, error) {
	if content == "" {
		return &domain.ToolExecutionResult{
			ToolName:  ToolNameMemory,
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "content is required for replace operation",
		}, nil
	}

	path, err := t.config.ResolveMemoryPath()
	if err != nil {
		return &domain.ToolExecutionResult{
			ToolName:  ToolNameMemory,
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     fmt.Sprintf("failed to resolve memory path: %v", err),
		}, nil
	}

	writeResult, err := t.writer.Write(ctx, filewriter.WriteRequest{
		Path:      path,
		Content:   content,
		Overwrite: true,
		Backup:    true,
	})
	if err != nil {
		return &domain.ToolExecutionResult{
			ToolName:  ToolNameMemory,
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     fmt.Sprintf("failed to write memory file: %v", err),
		}, nil
	}

	return &domain.ToolExecutionResult{
		ToolName:  ToolNameMemory,
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data: &MemoryToolResult{
			Operation:  OperationReplace,
			Content:    content,
			Size:       writeResult.BytesWritten,
			FilePath:   writeResult.Path,
			BackupPath: writeResult.BackupPath,
		},
	}, nil
}

// execRemove deletes the memory file via os.Remove.
func (t *MemoryTool) execRemove(ctx context.Context, args map[string]any, start time.Time) (*domain.ToolExecutionResult, error) {
	path, err := t.config.ResolveMemoryPath()
	if err != nil {
		return &domain.ToolExecutionResult{
			ToolName:  ToolNameMemory,
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     fmt.Sprintf("failed to resolve memory path: %v", err),
		}, nil
	}

	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return &domain.ToolExecutionResult{
				ToolName:  ToolNameMemory,
				Arguments: args,
				Success:   true,
				Duration:  time.Since(start),
				Data: &MemoryToolResult{
					Operation: OperationRemove,
					Content:   "Memory file did not exist; nothing to remove.",
				},
			}, nil
		}
		return &domain.ToolExecutionResult{
			ToolName:  ToolNameMemory,
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     fmt.Sprintf("failed to remove memory file: %v", err),
		}, nil
	}

	return &domain.ToolExecutionResult{
		ToolName:  ToolNameMemory,
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data: &MemoryToolResult{
			Operation: OperationRemove,
			Content:   "Memory file removed successfully.",
			FilePath:  path,
		},
	}, nil
}

// IsEnabled returns whether the memory tool is enabled.
func (t *MemoryTool) IsEnabled() bool {
	return t.enabled
}

// Validate checks if the memory tool arguments are valid.
func (t *MemoryTool) Validate(args map[string]any) error {
	if !t.config.Memory.Enabled {
		return fmt.Errorf("memory tool is not enabled")
	}

	operation, ok := args["operation"].(string)
	if !ok {
		return fmt.Errorf("operation parameter is required and must be a string")
	}

	switch operation {
	case OperationRead, OperationAppend, OperationReplace, OperationRemove:
		// valid
	default:
		return fmt.Errorf("invalid operation: %s (must be one of: read, append, replace, remove)", operation)
	}

	if operation == OperationAppend || operation == OperationReplace {
		content, hasContent := args["content"]
		if !hasContent {
			return fmt.Errorf("content is required for %s operation", operation)
		}
		if _, ok := content.(string); !ok {
			return fmt.Errorf("content must be a string")
		}
	}

	return nil
}

// FormatResult formats tool execution results for different contexts.
func (t *MemoryTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
	switch formatType {
	case domain.FormatterUI:
		return t.FormatForUI(result)
	case domain.FormatterLLM:
		return t.FormatForLLM(result)
	case domain.FormatterShort:
		return t.FormatPreview(result)
	default:
		return t.FormatForUI(result)
	}
}

// FormatPreview returns a short preview of the result for UI display.
func (t *MemoryTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Memory operation result unavailable"
	}

	if !result.Success {
		return "Memory operation failed"
	}

	if result.Data == nil {
		return "Memory operation completed successfully"
	}

	memResult, ok := result.Data.(*MemoryToolResult)
	if !ok {
		return "Memory operation completed"
	}

	switch memResult.Operation {
	case OperationRead:
		if memResult.Content == "" {
			return "Memory file is empty"
		}
		lineCount := strings.Count(memResult.Content, "\n") + 1
		return fmt.Sprintf("Read %d lines from memory", lineCount)
	case OperationAppend:
		return fmt.Sprintf("Appended %d bytes to memory", memResult.Size)
	case OperationReplace:
		return fmt.Sprintf("Replaced memory with %d bytes", memResult.Size)
	case OperationRemove:
		return "Memory file removed"
	default:
		return "Memory operation completed"
	}
}

// FormatForUI formats the result for UI display.
func (t *MemoryTool) FormatForUI(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	statusIcon := t.formatter.FormatStatusIcon(result.Success)

	var output strings.Builder
	toolCall := t.formatter.FormatToolCall(result.Arguments, false)
	fmt.Fprintf(&output, "%s\n", toolCall)

	if !result.Success {
		fmt.Fprintf(&output, "└─ %s Memory failed: %s", statusIcon, result.Error)
		return output.String()
	}

	preview := t.FormatPreview(result)
	fmt.Fprintf(&output, "└─ %s %s", statusIcon, preview)
	return output.String()
}

// FormatForLLM formats the result for LLM consumption with expanded tree structure.
func (t *MemoryTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Memory operation result unavailable"
	}

	var output strings.Builder

	output.WriteString(t.formatter.FormatExpandedHeader(result))
	output.WriteString(t.formatMemoryResultData(result))

	hasDataSection := result.Data != nil
	output.WriteString(t.formatter.FormatExpandedFooter(result, hasDataSection))

	return output.String()
}

// formatMemoryResultData formats the memory result data section.
func (t *MemoryTool) formatMemoryResultData(result *domain.ToolExecutionResult) string {
	if result.Data == nil {
		return ""
	}

	memResult, ok := result.Data.(*MemoryToolResult)
	if !ok {
		return ""
	}

	connector := "└─"
	if len(result.Metadata) > 0 {
		connector = "├─"
	}

	var output strings.Builder
	fmt.Fprintf(&output, "%s Result:\n", connector)
	fmt.Fprintf(&output, "   Operation: %s\n", memResult.Operation)
	fmt.Fprintf(&output, "   Size: %d bytes\n", memResult.Size)

	if memResult.FilePath != "" {
		fmt.Fprintf(&output, "   File: %s\n", memResult.FilePath)
	}
	if memResult.BackupPath != "" {
		fmt.Fprintf(&output, "   Backup: %s\n", memResult.BackupPath)
	}
	if memResult.Content != "" && memResult.Operation == OperationRead {
		fmt.Fprintf(&output, "   Content:\n%s\n", memResult.Content)
	}

	return output.String()
}

// ShouldCollapseArg determines if an argument should be collapsed in display.
func (t *MemoryTool) ShouldCollapseArg(key string) bool {
	return t.formatter.ShouldCollapseArg(key)
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI.
func (t *MemoryTool) ShouldAlwaysExpand() bool {
	return false
}

// MemoryToolResult represents the result of a memory tool operation.
type MemoryToolResult struct {
	Operation  string `json:"operation"`
	Content    string `json:"content,omitempty"`
	Size       int64  `json:"size,omitempty"`
	FilePath   string `json:"file_path,omitempty"`
	BackupPath string `json:"backup_path,omitempty"`
}
