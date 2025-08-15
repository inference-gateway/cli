package domain

import (
	"context"
	"time"

	sdk "github.com/inference-gateway/sdk"
)

// ConversationEntry represents a message in the conversation with metadata
type ConversationEntry struct {
	Message       sdk.Message          `json:"message"`
	Model         string               `json:"model,omitempty"`
	Time          time.Time            `json:"time"`
	ToolExecution *ToolExecutionResult `json:"tool_execution,omitempty"` // For tool result entries
}

// ExportFormat defines the format for exporting conversations
type ExportFormat string

const (
	ExportMarkdown ExportFormat = "markdown"
	ExportJSON     ExportFormat = "json"
	ExportText     ExportFormat = "text"
)

// ApprovalAction defines the possible approval actions for tool calls
type ApprovalAction int

const (
	ApprovalApprove ApprovalAction = iota // Approve and execute
	ApprovalReject                        // Deny and cancel
)

// ConversationRepository handles conversation storage and retrieval
type ConversationRepository interface {
	AddMessage(msg ConversationEntry) error
	GetMessages() []ConversationEntry
	Clear() error
	Export(format ExportFormat) ([]byte, error)
	GetMessageCount() int
	UpdateLastMessage(content string) error
	UpdateLastMessageToolCalls(toolCalls *[]sdk.ChatCompletionMessageToolCall) error
}

// ModelService handles model selection and information
type ModelService interface {
	ListModels(ctx context.Context) ([]string, error)
	SelectModel(modelID string) error
	GetCurrentModel() string
	IsModelAvailable(modelID string) bool
	ValidateModel(modelID string) error
}

// ChatEventType defines types of chat events
type ChatEventType int

const (
	EventChatStart ChatEventType = iota
	EventChatChunk
	EventChatComplete
	EventChatError
	EventToolCall
	EventCancelled
)

// ChatEvent represents events during chat operations
type ChatEvent interface {
	GetType() ChatEventType
	GetRequestID() string
	GetTimestamp() time.Time
}

// ChatMetrics holds performance and usage metrics
type ChatMetrics struct {
	Duration time.Duration
	Usage    *sdk.CompletionUsage
}

// ChatService handles chat completion operations
type ChatService interface {
	SendMessage(ctx context.Context, model string, messages []sdk.Message) (<-chan ChatEvent, error)
	CancelRequest(requestID string) error
	GetMetrics(requestID string) *ChatMetrics
}

// ToolDefinition describes an available tool
type ToolDefinition struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters"`
}

// ToolService handles tool execution
type ToolService interface {
	ListTools() []ToolDefinition
	ExecuteTool(ctx context.Context, name string, args map[string]interface{}) (*ToolExecutionResult, error)
	IsToolEnabled(name string) bool
	ValidateTool(name string, args map[string]interface{}) error
}

// FileService handles file operations
type FileService interface {
	ListProjectFiles() ([]string, error)
	ReadFile(path string) (string, error)
	ReadFileLines(path string, startLine, endLine int) (string, error)
	ValidateFile(path string) error
	GetFileInfo(path string) (FileInfo, error)
}

// FileInfo contains file metadata
type FileInfo struct {
	Path  string
	Size  int64
	IsDir bool
}

// FetchResult represents the result of a fetch operation
type FetchResult struct {
	Content     string            `json:"content"`
	URL         string            `json:"url"`
	Status      int               `json:"status"`
	Size        int64             `json:"size"`
	ContentType string            `json:"content_type"`
	Cached      bool              `json:"cached"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// FetchService handles content fetching operations
type FetchService interface {
	ValidateURL(url string) error
	FetchContent(ctx context.Context, target string) (*FetchResult, error)
	ClearCache()
	GetCacheStats() map[string]interface{}
}

// WebSearchResult represents a single search result
type WebSearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// WebSearchResponse represents the complete search response
type WebSearchResponse struct {
	Query   string            `json:"query"`
	Engine  string            `json:"engine"`
	Results []WebSearchResult `json:"results"`
	Total   int               `json:"total"`
	Time    time.Duration     `json:"time"`
	Error   string            `json:"error,omitempty"`
}

// WebSearchService handles web search operations
type WebSearchService interface {
	SearchGoogle(ctx context.Context, query string, maxResults int) (*WebSearchResponse, error)
	SearchDuckDuckGo(ctx context.Context, query string, maxResults int) (*WebSearchResponse, error)
	IsEnabled() bool
	SetEnabled(enabled bool)
}

// Tool represents a single tool with its definition, handler, and validator
type Tool interface {
	// Definition returns the tool definition for the LLM
	Definition() ToolDefinition

	// Execute runs the tool with given arguments
	Execute(ctx context.Context, args map[string]interface{}) (*ToolExecutionResult, error)

	// Validate checks if the tool arguments are valid
	Validate(args map[string]interface{}) error

	// IsEnabled returns whether this tool is enabled
	IsEnabled() bool
}

// ToolFactory creates tool instances
type ToolFactory interface {
	// CreateTool creates a tool instance by name
	CreateTool(name string) (Tool, error)

	// ListAvailableTools returns names of all available tools
	ListAvailableTools() []string
}

// ToolExecutionResult represents the complete result of a tool execution
type ToolExecutionResult struct {
	ToolName  string                 `json:"tool_name"`
	Arguments map[string]interface{} `json:"arguments"`
	Success   bool                   `json:"success"`
	Duration  time.Duration          `json:"duration"`
	Error     string                 `json:"error,omitempty"`
	Data      interface{}            `json:"data,omitempty"`
	Metadata  map[string]string      `json:"metadata,omitempty"`
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
	FilePath    string `json:"file_path"`
	BytesWriten int64  `json:"bytes_written"`
	Created     bool   `json:"created"`
	Overwritten bool   `json:"overwritten"`
	DirsCreated bool   `json:"dirs_created"`
	Error       string `json:"error,omitempty"`
}

// TreeToolResult represents the result of a tree operation
type TreeToolResult struct {
	Path            string   `json:"path"`
	Output          string   `json:"output"`
	TotalFiles      int      `json:"total_files"`
	TotalDirs       int      `json:"total_dirs"`
	MaxDepth        int      `json:"max_depth"`
	MaxFiles        int      `json:"max_files"`
	ExcludePatterns []string `json:"exclude_patterns"`
	ShowHidden      bool     `json:"show_hidden"`
	Format          string   `json:"format"`
	UsingNativeTree bool     `json:"using_native_tree"`
	Truncated       bool     `json:"truncated"`
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
