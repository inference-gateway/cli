package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	lipgloss "github.com/charmbracelet/lipgloss"
	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	filewriter "github.com/inference-gateway/cli/internal/domain/filewriter"
	filewriterservice "github.com/inference-gateway/cli/internal/services/filewriter"
	sdk "github.com/inference-gateway/sdk"
)

// ChunkedWriteSession tracks the state of a chunked write operation
type ChunkedWriteSession struct {
	FilePath    string
	TempPath    string
	ChunksCount int
	TotalBytes  int64
	StartedAt   time.Time
	LastChunkAt time.Time
	IsComplete  bool
}

// ChunkedWriteManager manages active chunked write sessions
type ChunkedWriteManager struct {
	sessions map[string]*ChunkedWriteSession
	mu       sync.RWMutex
}

// Global chunked write manager instance
var chunkedWriteManager = &ChunkedWriteManager{
	sessions: make(map[string]*ChunkedWriteSession),
}

// GetSession returns a session by file path
func (m *ChunkedWriteManager) GetSession(filePath string) (*ChunkedWriteSession, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	session, exists := m.sessions[filePath]
	return session, exists
}

// CreateSession creates a new chunked write session
func (m *ChunkedWriteManager) CreateSession(filePath, tempPath string) *ChunkedWriteSession {
	m.mu.Lock()
	defer m.mu.Unlock()

	session := &ChunkedWriteSession{
		FilePath:    filePath,
		TempPath:    tempPath,
		ChunksCount: 0,
		TotalBytes:  0,
		StartedAt:   time.Now(),
		LastChunkAt: time.Now(),
		IsComplete:  false,
	}
	m.sessions[filePath] = session
	return session
}

// UpdateSession updates a session after appending a chunk
func (m *ChunkedWriteManager) UpdateSession(filePath string, bytesWritten int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if session, exists := m.sessions[filePath]; exists {
		session.ChunksCount++
		session.TotalBytes += bytesWritten
		session.LastChunkAt = time.Now()
	}
}

// CompleteSession marks a session as complete and removes it
func (m *ChunkedWriteManager) CompleteSession(filePath string) *ChunkedWriteSession {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, exists := m.sessions[filePath]
	if exists {
		session.IsComplete = true
		delete(m.sessions, filePath)
	}
	return session
}

// RemoveSession removes a session without completing it
func (m *ChunkedWriteManager) RemoveSession(filePath string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, filePath)
}

// Styles for chunked write tools
var (
	chunkSuccessStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("10")).
				Bold(true)

	chunkInfoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("12"))
)

// =============================================================================
// WriteStart Tool
// =============================================================================

// WriteStartTool implements a tool to start a chunked write operation
type WriteStartTool struct {
	config        *config.Config
	enabled       bool
	pathValidator filewriter.PathValidator
}

// NewWriteStartTool creates a new WriteStart tool
func NewWriteStartTool(cfg *config.Config) *WriteStartTool {
	return &WriteStartTool{
		config:        cfg,
		enabled:       cfg.Tools.Enabled && cfg.Tools.Write.Enabled,
		pathValidator: filewriterservice.NewPathValidator(cfg),
	}
}

// Definition returns the tool definition for the LLM
func (t *WriteStartTool) Definition() sdk.ChatCompletionTool {
	description := `Starts a chunked write operation for creating large files.

Use this tool when you need to write files that are too large to fit in a single tool call due to output token limits.

WORKFLOW:
1. Call WriteStart with the file path and first chunk of content
2. Call WriteAppend for each additional chunk of content
3. Call WriteComplete to finalize and save the file

IMPORTANT:
- Each chunk should be a reasonable size (under 50 lines recommended)
- Call WriteComplete when done to finalize the file
- If you don't call WriteComplete, the partial file will be discarded
- The file is written atomically - either all chunks succeed or none do`

	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "WriteStart",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"file_path": map[string]any{
						"type":        "string",
						"description": "The absolute path to the file to write (must be absolute, not relative)",
					},
					"content": map[string]any{
						"type":        "string",
						"description": "The first chunk of content to write to the file",
					},
				},
				"required": []string{"file_path", "content"},
			},
		},
	}
}

// Execute starts a chunked write operation
func (t *WriteStartTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	start := time.Now()

	if !t.enabled {
		return &domain.ToolExecutionResult{
			ToolName:  "WriteStart",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "chunked write tools are not enabled",
		}, nil
	}

	filePath, ok := args["file_path"].(string)
	if !ok || filePath == "" {
		return &domain.ToolExecutionResult{
			ToolName:  "WriteStart",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "file_path is required",
		}, nil
	}

	content, ok := args["content"].(string)
	if !ok {
		return &domain.ToolExecutionResult{
			ToolName:  "WriteStart",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "content is required",
		}, nil
	}

	// Validate path is within sandbox
	if err := t.pathValidator.Validate(filePath); err != nil {
		return &domain.ToolExecutionResult{
			ToolName:  "WriteStart",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     fmt.Sprintf("path validation failed: %v", err),
		}, nil
	}

	// Check if there's already an active session for this file
	if _, exists := chunkedWriteManager.GetSession(filePath); exists {
		return &domain.ToolExecutionResult{
			ToolName:  "WriteStart",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     fmt.Sprintf("chunked write session already active for %s - call WriteComplete first or start a new file", filePath),
		}, nil
	}

	// Create temporary file for chunked writing
	tempDir := filepath.Join(filepath.Dir(filePath), ".infer_chunks")
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return &domain.ToolExecutionResult{
			ToolName:  "WriteStart",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     fmt.Sprintf("failed to create temp directory: %v", err),
		}, nil
	}

	tempPath := filepath.Join(tempDir, fmt.Sprintf("%s.chunk", filepath.Base(filePath)))

	// Write the first chunk
	if err := os.WriteFile(tempPath, []byte(content), 0644); err != nil {
		return &domain.ToolExecutionResult{
			ToolName:  "WriteStart",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     fmt.Sprintf("failed to write initial chunk: %v", err),
		}, nil
	}

	// Create session
	session := chunkedWriteManager.CreateSession(filePath, tempPath)
	session.ChunksCount = 1
	session.TotalBytes = int64(len(content))

	return &domain.ToolExecutionResult{
		ToolName:  "WriteStart",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data: &domain.FileWriteToolResult{
			FilePath:     filePath,
			BytesWritten: int64(len(content)),
			LinesWritten: strings.Count(content, "\n") + 1,
			Created:      true,
			ChunkIndex:   1,
			IsComplete:   false,
		},
	}, nil
}

// Validate checks if the arguments are valid
func (t *WriteStartTool) Validate(args map[string]any) error {
	if !t.enabled {
		return fmt.Errorf("chunked write tools are not enabled")
	}

	filePath, ok := args["file_path"].(string)
	if !ok || filePath == "" {
		return fmt.Errorf("file_path is required")
	}

	if _, ok := args["content"].(string); !ok {
		return fmt.Errorf("content is required")
	}

	return t.pathValidator.Validate(filePath)
}

// IsEnabled returns whether the tool is enabled
func (t *WriteStartTool) IsEnabled() bool {
	return t.enabled
}

// FormatResult formats tool execution results
func (t *WriteStartTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
	if result == nil || !result.Success {
		return fmt.Sprintf("WriteStart failed: %s", result.Error)
	}

	if writeResult, ok := result.Data.(*domain.FileWriteToolResult); ok {
		return fmt.Sprintf("Started chunked write to %s (chunk 1: %d bytes). Use WriteAppend to add more content, then WriteComplete to finalize.",
			writeResult.FilePath, writeResult.BytesWritten)
	}
	return "WriteStart completed"
}

// FormatPreview returns a short preview
func (t *WriteStartTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return "WriteStart failed"
	}
	if writeResult, ok := result.Data.(*domain.FileWriteToolResult); ok {
		return chunkSuccessStyle.Render(fmt.Sprintf("Started %s", filepath.Base(writeResult.FilePath)))
	}
	return "WriteStart completed"
}

// ShouldCollapseArg determines if an argument should be collapsed
func (t *WriteStartTool) ShouldCollapseArg(key string) bool {
	return key == "content"
}

// ShouldAlwaysExpand returns whether results should always be expanded
func (t *WriteStartTool) ShouldAlwaysExpand() bool {
	return false
}

// =============================================================================
// WriteAppend Tool
// =============================================================================

// WriteAppendTool implements a tool to append content to a chunked write operation
type WriteAppendTool struct {
	config  *config.Config
	enabled bool
}

// NewWriteAppendTool creates a new WriteAppend tool
func NewWriteAppendTool(cfg *config.Config) *WriteAppendTool {
	return &WriteAppendTool{
		config:  cfg,
		enabled: cfg.Tools.Enabled && cfg.Tools.Write.Enabled,
	}
}

// Definition returns the tool definition for the LLM
func (t *WriteAppendTool) Definition() sdk.ChatCompletionTool {
	description := `Appends content to an active chunked write operation.

Use this tool after WriteStart to add additional chunks of content to a file.

IMPORTANT:
- Must have called WriteStart first for this file path
- Each chunk should be a reasonable size (under 50 lines recommended)
- Call WriteComplete when done to finalize the file`

	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "WriteAppend",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"file_path": map[string]any{
						"type":        "string",
						"description": "The absolute path to the file (must match the path used in WriteStart)",
					},
					"content": map[string]any{
						"type":        "string",
						"description": "The next chunk of content to append to the file",
					},
				},
				"required": []string{"file_path", "content"},
			},
		},
	}
}

// Execute appends content to a chunked write operation
func (t *WriteAppendTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	start := time.Now()

	if !t.enabled {
		return &domain.ToolExecutionResult{
			ToolName:  "WriteAppend",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "chunked write tools are not enabled",
		}, nil
	}

	filePath, ok := args["file_path"].(string)
	if !ok || filePath == "" {
		return &domain.ToolExecutionResult{
			ToolName:  "WriteAppend",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "file_path is required",
		}, nil
	}

	content, ok := args["content"].(string)
	if !ok {
		return &domain.ToolExecutionResult{
			ToolName:  "WriteAppend",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "content is required",
		}, nil
	}

	// Get active session
	session, exists := chunkedWriteManager.GetSession(filePath)
	if !exists {
		return &domain.ToolExecutionResult{
			ToolName:  "WriteAppend",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     fmt.Sprintf("no active chunked write session for %s - call WriteStart first", filePath),
		}, nil
	}

	// Append to temp file
	f, err := os.OpenFile(session.TempPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return &domain.ToolExecutionResult{
			ToolName:  "WriteAppend",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     fmt.Sprintf("failed to open temp file: %v", err),
		}, nil
	}
	defer func() { _ = f.Close() }()

	bytesWritten, err := f.WriteString(content)
	if err != nil {
		return &domain.ToolExecutionResult{
			ToolName:  "WriteAppend",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     fmt.Sprintf("failed to append chunk: %v", err),
		}, nil
	}

	// Calculate chunk index before updating session
	chunkIndex := session.ChunksCount + 1

	// Update session
	chunkedWriteManager.UpdateSession(filePath, int64(bytesWritten))

	return &domain.ToolExecutionResult{
		ToolName:  "WriteAppend",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data: &domain.FileWriteToolResult{
			FilePath:     filePath,
			BytesWritten: int64(bytesWritten),
			LinesWritten: strings.Count(content, "\n"),
			Appended:     true,
			ChunkIndex:   chunkIndex,
			IsComplete:   false,
		},
	}, nil
}

// Validate checks if the arguments are valid
func (t *WriteAppendTool) Validate(args map[string]any) error {
	if !t.enabled {
		return fmt.Errorf("chunked write tools are not enabled")
	}

	filePath, ok := args["file_path"].(string)
	if !ok || filePath == "" {
		return fmt.Errorf("file_path is required")
	}

	if _, ok := args["content"].(string); !ok {
		return fmt.Errorf("content is required")
	}

	// Check if session exists
	if _, exists := chunkedWriteManager.GetSession(filePath); !exists {
		return fmt.Errorf("no active chunked write session for %s - call WriteStart first", filePath)
	}

	return nil
}

// IsEnabled returns whether the tool is enabled
func (t *WriteAppendTool) IsEnabled() bool {
	return t.enabled
}

// FormatResult formats tool execution results
func (t *WriteAppendTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
	if result == nil || !result.Success {
		return fmt.Sprintf("WriteAppend failed: %s", result.Error)
	}

	if writeResult, ok := result.Data.(*domain.FileWriteToolResult); ok {
		return fmt.Sprintf("Appended chunk %d to %s (%d bytes). Continue with WriteAppend or finalize with WriteComplete.",
			writeResult.ChunkIndex, writeResult.FilePath, writeResult.BytesWritten)
	}
	return "WriteAppend completed"
}

// FormatPreview returns a short preview
func (t *WriteAppendTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return "WriteAppend failed"
	}
	if writeResult, ok := result.Data.(*domain.FileWriteToolResult); ok {
		return chunkInfoStyle.Render(fmt.Sprintf("Appended chunk %d", writeResult.ChunkIndex))
	}
	return "WriteAppend completed"
}

// ShouldCollapseArg determines if an argument should be collapsed
func (t *WriteAppendTool) ShouldCollapseArg(key string) bool {
	return key == "content"
}

// ShouldAlwaysExpand returns whether results should always be expanded
func (t *WriteAppendTool) ShouldAlwaysExpand() bool {
	return false
}

// =============================================================================
// WriteComplete Tool
// =============================================================================

// WriteCompleteTool implements a tool to finalize a chunked write operation
type WriteCompleteTool struct {
	config        *config.Config
	enabled       bool
	pathValidator filewriter.PathValidator
}

// NewWriteCompleteTool creates a new WriteComplete tool
func NewWriteCompleteTool(cfg *config.Config) *WriteCompleteTool {
	return &WriteCompleteTool{
		config:        cfg,
		enabled:       cfg.Tools.Enabled && cfg.Tools.Write.Enabled,
		pathValidator: filewriterservice.NewPathValidator(cfg),
	}
}

// Definition returns the tool definition for the LLM
func (t *WriteCompleteTool) Definition() sdk.ChatCompletionTool {
	description := `Finalizes a chunked write operation and saves the file.

Use this tool after WriteStart and any WriteAppend calls to finalize and save the file.

This will:
1. Move the accumulated content from temporary storage to the final file path
2. Create any necessary parent directories
3. Clean up temporary files

IMPORTANT:
- Must have called WriteStart first for this file path
- The file will only exist after WriteComplete is called
- If WriteComplete is not called, all chunks will be discarded`

	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "WriteComplete",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"file_path": map[string]any{
						"type":        "string",
						"description": "The absolute path to the file (must match the path used in WriteStart)",
					},
				},
				"required": []string{"file_path"},
			},
		},
	}
}

// Execute finalizes a chunked write operation
func (t *WriteCompleteTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	start := time.Now()

	if !t.enabled {
		return &domain.ToolExecutionResult{
			ToolName:  "WriteComplete",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "chunked write tools are not enabled",
		}, nil
	}

	filePath, ok := args["file_path"].(string)
	if !ok || filePath == "" {
		return &domain.ToolExecutionResult{
			ToolName:  "WriteComplete",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "file_path is required",
		}, nil
	}

	// Get active session
	session, exists := chunkedWriteManager.GetSession(filePath)
	if !exists {
		return &domain.ToolExecutionResult{
			ToolName:  "WriteComplete",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     fmt.Sprintf("no active chunked write session for %s - call WriteStart first", filePath),
		}, nil
	}

	// Ensure parent directory exists
	parentDir := filepath.Dir(filePath)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		return &domain.ToolExecutionResult{
			ToolName:  "WriteComplete",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     fmt.Sprintf("failed to create parent directory: %v", err),
		}, nil
	}

	// Check if target file exists (for determining Created vs Updated)
	_, existsErr := os.Stat(filePath)
	created := os.IsNotExist(existsErr)

	// Move temp file to final location
	if err := os.Rename(session.TempPath, filePath); err != nil {
		// If rename fails (e.g., cross-device), fall back to copy
		content, readErr := os.ReadFile(session.TempPath)
		if readErr != nil {
			return &domain.ToolExecutionResult{
				ToolName:  "WriteComplete",
				Arguments: args,
				Success:   false,
				Duration:  time.Since(start),
				Error:     fmt.Sprintf("failed to read temp file: %v", readErr),
			}, nil
		}

		if writeErr := os.WriteFile(filePath, content, 0644); writeErr != nil {
			return &domain.ToolExecutionResult{
				ToolName:  "WriteComplete",
				Arguments: args,
				Success:   false,
				Duration:  time.Since(start),
				Error:     fmt.Sprintf("failed to write final file: %v", writeErr),
			}, nil
		}

		// Clean up temp file (ignore error - best effort cleanup)
		_ = os.Remove(session.TempPath)
	}

	// Clean up temp directory if empty (ignore error - best effort cleanup)
	tempDir := filepath.Dir(session.TempPath)
	_ = os.Remove(tempDir)

	// Get final file info
	fileInfo, _ := os.Stat(filePath)
	var totalBytes int64
	var lineCount int
	if fileInfo != nil {
		totalBytes = fileInfo.Size()
		if content, err := os.ReadFile(filePath); err == nil {
			lineCount = strings.Count(string(content), "\n") + 1
		}
	}

	// Complete the session
	completedSession := chunkedWriteManager.CompleteSession(filePath)

	return &domain.ToolExecutionResult{
		ToolName:  "WriteComplete",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data: &domain.FileWriteToolResult{
			FilePath:     filePath,
			BytesWritten: totalBytes,
			LinesWritten: lineCount,
			Created:      created,
			Overwritten:  !created,
			TotalChunks:  completedSession.ChunksCount,
			IsComplete:   true,
		},
	}, nil
}

// Validate checks if the arguments are valid
func (t *WriteCompleteTool) Validate(args map[string]any) error {
	if !t.enabled {
		return fmt.Errorf("chunked write tools are not enabled")
	}

	filePath, ok := args["file_path"].(string)
	if !ok || filePath == "" {
		return fmt.Errorf("file_path is required")
	}

	// Check if session exists
	if _, exists := chunkedWriteManager.GetSession(filePath); !exists {
		return fmt.Errorf("no active chunked write session for %s - call WriteStart first", filePath)
	}

	return nil
}

// IsEnabled returns whether the tool is enabled
func (t *WriteCompleteTool) IsEnabled() bool {
	return t.enabled
}

// FormatResult formats tool execution results
func (t *WriteCompleteTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
	if result == nil || !result.Success {
		return fmt.Sprintf("WriteComplete failed: %s", result.Error)
	}

	if writeResult, ok := result.Data.(*domain.FileWriteToolResult); ok {
		action := "Updated"
		if writeResult.Created {
			action = "Created"
		}
		return fmt.Sprintf("%s %s successfully (%d bytes, %d lines, %d chunks)",
			action, writeResult.FilePath, writeResult.BytesWritten, writeResult.LinesWritten, writeResult.TotalChunks)
	}
	return "WriteComplete completed"
}

// FormatPreview returns a short preview
func (t *WriteCompleteTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return "WriteComplete failed"
	}
	if writeResult, ok := result.Data.(*domain.FileWriteToolResult); ok {
		action := "Updated"
		if writeResult.Created {
			action = "Created"
		}
		return chunkSuccessStyle.Render(fmt.Sprintf("%s %s", action, filepath.Base(writeResult.FilePath)))
	}
	return "WriteComplete completed"
}

// ShouldCollapseArg determines if an argument should be collapsed
func (t *WriteCompleteTool) ShouldCollapseArg(key string) bool {
	return false
}

// ShouldAlwaysExpand returns whether results should always be expanded
func (t *WriteCompleteTool) ShouldAlwaysExpand() bool {
	return false
}
