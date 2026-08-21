// The Tool contract, tool formatting, and per-tool result types.

package domain

import (
	"context"
	"time"

	sdk "github.com/inference-gateway/sdk"
)

// Tool represents a single tool with its definition, handler, and validator
type Tool interface {
	// Definition returns the tool definition for the LLM
	Definition() sdk.ChatCompletionTool

	// Execute runs the tool with given arguments
	Execute(ctx context.Context, args map[string]any) (*ToolExecutionResult, error)

	// Validate checks if the tool arguments are valid
	Validate(args map[string]any) error

	// IsEnabled returns whether this tool is enabled
	IsEnabled() bool

	// FormatResult formats tool execution results for different contexts
	FormatResult(result *ToolExecutionResult, formatType FormatterType) string

	// FormatPreview returns a short preview of the result for UI display
	FormatPreview(result *ToolExecutionResult) string

	// ShouldCollapseArg determines if an argument should be collapsed in display
	ShouldCollapseArg(key string) bool

	// ShouldAlwaysExpand determines if tool results should always be expanded in UI
	ShouldAlwaysExpand() bool
}

// FormatterType defines the context for formatting tool results
type FormatterType string

const (
	FormatterUI    FormatterType = "ui"    // Compact display for UI
	FormatterLLM   FormatterType = "llm"   // Formatted for LLM consumption
	FormatterShort FormatterType = "short" // Brief summary format
)

// ToolFormatter provides formatting capabilities for tool results
type ToolFormatter interface {
	// FormatToolCall formats a tool call for consistent display
	FormatToolCall(toolName string, args map[string]any) string

	// RenderToolSummary renders the shared "<icon> Name(args) <trailing>" line used by
	// the collapsed status line, live preview, approval summary and queue preview.
	RenderToolSummary(icon, toolName string, args map[string]any, trailing string, terminalWidth int) string

	// FormatToolResultForUI formats tool execution results for UI display
	FormatToolResultForUI(result *ToolExecutionResult, terminalWidth int) string

	// FormatToolResultExpanded formats expanded tool execution results
	FormatToolResultExpanded(result *ToolExecutionResult, terminalWidth int) string

	// FormatToolResultForLLM formats tool execution results for LLM consumption
	FormatToolResultForLLM(result *ToolExecutionResult) string

	// ShouldAlwaysExpandTool checks if a tool result should always be expanded
	ShouldAlwaysExpandTool(toolName string) bool
}

// ToolExecutionResult represents the complete result of a tool execution
type ToolExecutionResult struct {
	ToolName   string            `json:"tool_name"`
	ToolCallID string            `json:"tool_call_id,omitempty"`
	Arguments  map[string]any    `json:"arguments"`
	Success    bool              `json:"success"`
	Duration   time.Duration     `json:"duration"`
	Error      string            `json:"error,omitempty"`
	Data       any               `json:"data,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	Diff       string            `json:"diff,omitempty"`
	Rejected   bool              `json:"rejected,omitempty"`
	Images     []ImageAttachment `json:"images,omitempty"`
}

// BashToolResult represents the result of a bash command execution
type BashToolResult struct {
	Command  string `json:"command"`
	Output   string `json:"output"`
	Error    string `json:"error,omitempty"`
	ExitCode int    `json:"exit_code"`
	Duration string `json:"duration"`
}

// FileReadToolResult represents the result of a file read operation
type FileReadToolResult struct {
	FilePath  string `json:"file_path"`
	Content   string `json:"content"`
	Size      int64  `json:"size"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
	Error     string `json:"error,omitempty"`
}

// FileWriteToolResult represents the result of a file write operation
type FileWriteToolResult struct {
	FilePath     string `json:"file_path"`
	BytesWritten int64  `json:"bytes_written"`
	LinesWritten int    `json:"lines_written"`
	Created      bool   `json:"created"`
	Overwritten  bool   `json:"overwritten"`
	ChunkIndex   int    `json:"chunk_index,omitempty"`
	TotalChunks  int    `json:"total_chunks,omitempty"`
	IsComplete   bool   `json:"is_complete"`
	Error        string `json:"error,omitempty"`
}

// EditToolResult represents the result of an edit operation
type EditToolResult struct {
	FilePath             string `json:"file_path"`
	OldString            string `json:"old_string"`
	NewString            string `json:"new_string"`
	ReplacedCount        int    `json:"replaced_count"`
	ReplaceAll           bool   `json:"replace_all"`
	FileModified         bool   `json:"file_modified"`
	OriginalSize         int64  `json:"original_size"`
	NewSize              int64  `json:"new_size"`
	BytesDifference      int64  `json:"bytes_difference"`
	OriginalLines        int    `json:"original_lines"`
	NewLines             int    `json:"new_lines"`
	LinesDifference      int    `json:"lines_difference"`
	Diff                 string `json:"diff,omitempty"`
	WhitespaceNormalized bool   `json:"whitespace_normalized,omitempty"`
	StartLine            int    `json:"start_line,omitempty"`
}

// TreeToolResult represents the result of a tree operation
type TreeToolResult struct {
	Path            string `json:"path"`
	Output          string `json:"output"`
	TotalFiles      int    `json:"total_files"`
	TotalDirs       int    `json:"total_dirs"`
	MaxDepth        int    `json:"max_depth"`
	MaxFiles        int    `json:"max_files"`
	ShowHidden      bool   `json:"show_hidden"`
	Format          string `json:"format"`
	UsingNativeTree bool   `json:"using_native_tree"`
	Truncated       bool   `json:"truncated"`
}

// DeleteToolResult represents the result of a delete operation
type DeleteToolResult struct {
	Path              string   `json:"path"`
	DeletedFiles      []string `json:"deleted_files"`
	DeletedDirs       []string `json:"deleted_dirs"`
	TotalFilesDeleted int      `json:"total_files_deleted"`
	TotalDirsDeleted  int      `json:"total_dirs_deleted"`
	WildcardExpanded  bool     `json:"wildcard_expanded"`
	Errors            []string `json:"errors,omitempty"`
}

// MultiEditToolResult represents the result of a MultiEdit operation
type MultiEditToolResult struct {
	FilePath        string                `json:"file_path"`
	Edits           []EditOperationResult `json:"edits"`
	TotalEdits      int                   `json:"total_edits"`
	SuccessfulEdits int                   `json:"successful_edits"`
	FileModified    bool                  `json:"file_modified"`
	OriginalSize    int64                 `json:"original_size"`
	NewSize         int64                 `json:"new_size"`
	BytesDifference int64                 `json:"bytes_difference"`
	NormalizedEdits int                   `json:"normalized_edits,omitempty"`
}

// EditOperationResult represents the result of a single edit operation within MultiEdit
type EditOperationResult struct {
	OldString     string `json:"old_string"`
	NewString     string `json:"new_string"`
	ReplaceAll    bool   `json:"replace_all"`
	ReplacedCount int    `json:"replaced_count"`
	Success       bool   `json:"success"`
	Error         string `json:"error,omitempty"`
	// WhitespaceNormalized is true when this edit matched via the indentation-tolerant fallback.
	WhitespaceNormalized bool `json:"whitespace_normalized,omitempty"`
}

// TodoItem represents a single todo item
type TodoItem struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Status  string `json:"status"`
}

// TodoWriteToolResult represents the result of a TodoWrite operation
type TodoWriteToolResult struct {
	Todos          []TodoItem `json:"todos"`
	TotalTasks     int        `json:"total_tasks"`
	CompletedTasks int        `json:"completed_tasks"`
	InProgressTask string     `json:"in_progress_task,omitempty"`
	ValidationOK   bool       `json:"validation_ok"`
}

// MCPToolResult represents the result of an MCP tool execution
type MCPToolResult struct {
	ServerName string `json:"server_name"`
	ToolName   string `json:"tool_name"`
	Content    string `json:"content"`
	Error      string `json:"error,omitempty"`
}

// UI Event Types for application state management
