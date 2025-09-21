package domain

import (
	"context"
	"time"

	sdk "github.com/inference-gateway/sdk"
)

// Assistant message
type Message = sdk.Message

// Common role constants
const (
	RoleUser      = sdk.User
	RoleAssistant = sdk.Assistant
	RoleTool      = sdk.Tool
	RoleSystem    = sdk.System
)

// ConversationEntry represents a message in the conversation with metadata
type ConversationEntry struct {
	Message       Message              `json:"message"`
	Model         string               `json:"model,omitempty"`
	Time          time.Time            `json:"time"`
	ToolExecution *ToolExecutionResult `json:"tool_execution,omitempty"`
	Hidden        bool                 `json:"hidden,omitempty"`
}

// ExportFormat defines the format for exporting conversations
type ExportFormat string

const (
	ExportMarkdown ExportFormat = "markdown"
	ExportJSON     ExportFormat = "json"
	ExportText     ExportFormat = "text"
)

// SessionTokenStats tracks accumulated token usage across a session
type SessionTokenStats struct {
	TotalInputTokens  int `json:"total_input_tokens"`
	TotalOutputTokens int `json:"total_output_tokens"`
	TotalTokens       int `json:"total_tokens"`
	RequestCount      int `json:"request_count"`
}

// ConversationRepository handles conversation storage and retrieval
type ConversationRepository interface {
	AddMessage(msg ConversationEntry) error
	GetMessages() []ConversationEntry
	Clear() error
	ClearExceptFirstUserMessage() error
	Export(format ExportFormat) ([]byte, error)
	GetMessageCount() int
	UpdateLastMessage(content string) error
	UpdateLastMessageToolCalls(toolCalls *[]sdk.ChatCompletionMessageToolCall) error
	AddTokenUsage(inputTokens, outputTokens, totalTokens int) error
	GetSessionTokens() SessionTokenStats
	FormatToolResultForLLM(result *ToolExecutionResult) string
	FormatToolResultForUI(result *ToolExecutionResult, terminalWidth int) string
	FormatToolResultExpanded(result *ToolExecutionResult, terminalWidth int) string
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
	EventToolCallPreview
	EventToolCallUpdate
	EventToolCallReady
	EventCancelled
	EventOptimizationStatus
	EventA2AToolCallExecuted
	EventA2ATaskSubmitted
	EventA2ATaskStatusUpdate
	EventA2ATaskCompleted
	EventA2ATaskInputRequired
	EventParallelToolsStart
	EventToolExecutionProgress
	EventParallelToolsComplete
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

// ChatSyncResponse represents a synchronous chat completion response
type ChatSyncResponse struct {
	RequestID string                              `json:"request_id"`
	Content   string                              `json:"content"`
	ToolCalls []sdk.ChatCompletionMessageToolCall `json:"tool_calls,omitempty"`
	Usage     *sdk.CompletionUsage                `json:"usage,omitempty"`
	Duration  time.Duration                       `json:"duration"`
}

// ChatService handles chat completion operations
type ChatService interface {
	CancelRequest(requestID string) error
	GetMetrics(requestID string) *ChatMetrics
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

// TaskTracker handles task ID and context ID tracking within chat sessions
type TaskTracker interface {
	GetFirstTaskID() string
	SetFirstTaskID(taskID string)
	ClearTaskID()
	GetContextID() string
	SetContextID(contextID string)
	ClearContextID()
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
	Warning     string            `json:"warning,omitempty"`
}

// WebFetchService handles content fetching operations
type WebFetchService interface {
	ValidateURL(url string) error
	FetchContent(ctx context.Context, target string) (*FetchResult, error)
	ClearCache()
	GetCacheStats() map[string]any
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

// ThemeService handles theme management
type ThemeService interface {
	ListThemes() []string
	GetCurrentTheme() Theme
	GetCurrentThemeName() string
	SetTheme(themeName string) error
}

// Theme interface for theming support
type Theme interface {
	GetUserColor() string
	GetAssistantColor() string
	GetErrorColor() string
	GetStatusColor() string
	GetAccentColor() string
	GetDimColor() string
	GetBorderColor() string
	GetDiffAddColor() string
	GetDiffRemoveColor() string
}

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

	// FormatToolResultForUI formats tool execution results for UI display
	FormatToolResultForUI(result *ToolExecutionResult, terminalWidth int) string

	// FormatToolResultExpanded formats expanded tool execution results
	FormatToolResultExpanded(result *ToolExecutionResult, terminalWidth int) string

	// FormatToolResultForLLM formats tool execution results for LLM consumption
	FormatToolResultForLLM(result *ToolExecutionResult) string

	// ShouldAlwaysExpandTool checks if a tool result should always be expanded
	ShouldAlwaysExpandTool(toolName string) bool
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
	ToolName  string            `json:"tool_name"`
	Arguments map[string]any    `json:"arguments"`
	Success   bool              `json:"success"`
	Duration  time.Duration     `json:"duration"`
	Error     string            `json:"error,omitempty"`
	Data      any               `json:"data,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Diff      string            `json:"diff,omitempty"`
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
	DirsCreated  bool   `json:"dirs_created"`
	Appended     bool   `json:"appended"`
	ChunkIndex   int    `json:"chunk_index,omitempty"`
	TotalChunks  int    `json:"total_chunks,omitempty"`
	IsComplete   bool   `json:"is_complete"`
	Error        string `json:"error,omitempty"`
}

// EditToolResult represents the result of an edit operation
type EditToolResult struct {
	FilePath        string `json:"file_path"`
	OldString       string `json:"old_string"`
	NewString       string `json:"new_string"`
	ReplacedCount   int    `json:"replaced_count"`
	ReplaceAll      bool   `json:"replace_all"`
	FileModified    bool   `json:"file_modified"`
	OriginalSize    int64  `json:"original_size"`
	NewSize         int64  `json:"new_size"`
	BytesDifference int64  `json:"bytes_difference"`
	Diff            string `json:"diff,omitempty"`
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
}

// EditOperationResult represents the result of a single edit operation within MultiEdit
type EditOperationResult struct {
	OldString     string `json:"old_string"`
	NewString     string `json:"new_string"`
	ReplaceAll    bool   `json:"replace_all"`
	ReplacedCount int    `json:"replaced_count"`
	Success       bool   `json:"success"`
	Error         string `json:"error,omitempty"`
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

// GitHubUser represents a GitHub user
type GitHubUser struct {
	ID        int    `json:"id"`
	Login     string `json:"login"`
	AvatarURL string `json:"avatar_url"`
	HTMLURL   string `json:"html_url"`
	Type      string `json:"type"`
}

// GitHubLabel represents a GitHub label
type GitHubLabel struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description,omitempty"`
}

// GitHubMilestone represents a GitHub milestone
type GitHubMilestone struct {
	ID          int        `json:"id"`
	Number      int        `json:"number"`
	Title       string     `json:"title"`
	Description string     `json:"description,omitempty"`
	State       string     `json:"state"`
	DueOn       *time.Time `json:"due_on,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// GitHubIssue represents a GitHub issue
type GitHubIssue struct {
	ID          int              `json:"id"`
	Number      int              `json:"number"`
	Title       string           `json:"title"`
	Body        string           `json:"body"`
	State       string           `json:"state"`
	User        GitHubUser       `json:"user"`
	Assignees   []GitHubUser     `json:"assignees,omitempty"`
	Labels      []GitHubLabel    `json:"labels,omitempty"`
	Milestone   *GitHubMilestone `json:"milestone,omitempty"`
	Comments    int              `json:"comments"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
	ClosedAt    *time.Time       `json:"closed_at,omitempty"`
	HTMLURL     string           `json:"html_url"`
	PullRequest *struct {
		URL      string `json:"url"`
		HTMLURL  string `json:"html_url"`
		DiffURL  string `json:"diff_url"`
		PatchURL string `json:"patch_url"`
	} `json:"pull_request,omitempty"`
}

// GitHubPullRequest represents a GitHub pull request
type GitHubPullRequest struct {
	ID           int              `json:"id"`
	Number       int              `json:"number"`
	Title        string           `json:"title"`
	Body         string           `json:"body"`
	State        string           `json:"state"`
	User         GitHubUser       `json:"user"`
	Assignees    []GitHubUser     `json:"assignees,omitempty"`
	Labels       []GitHubLabel    `json:"labels,omitempty"`
	Milestone    *GitHubMilestone `json:"milestone,omitempty"`
	Comments     int              `json:"comments"`
	Commits      int              `json:"commits"`
	Additions    int              `json:"additions"`
	Deletions    int              `json:"deletions"`
	ChangedFiles int              `json:"changed_files"`
	CreatedAt    time.Time        `json:"created_at"`
	UpdatedAt    time.Time        `json:"updated_at"`
	ClosedAt     *time.Time       `json:"closed_at,omitempty"`
	MergedAt     *time.Time       `json:"merged_at,omitempty"`
	Merged       bool             `json:"merged"`
	Mergeable    *bool            `json:"mergeable,omitempty"`
	Head         GitHubBranch     `json:"head"`
	Base         GitHubBranch     `json:"base"`
	HTMLURL      string           `json:"html_url"`
	DiffURL      string           `json:"diff_url"`
	PatchURL     string           `json:"patch_url"`
}

// GitHubBranch represents a branch reference in a pull request
type GitHubBranch struct {
	Label string           `json:"label"`
	Ref   string           `json:"ref"`
	SHA   string           `json:"sha"`
	User  GitHubUser       `json:"user"`
	Repo  GitHubRepository `json:"repo"`
}

// GitHubRepository represents a GitHub repository
type GitHubRepository struct {
	ID       int        `json:"id"`
	Name     string     `json:"name"`
	FullName string     `json:"full_name"`
	Owner    GitHubUser `json:"owner"`
	Private  bool       `json:"private"`
	HTMLURL  string     `json:"html_url"`
	CloneURL string     `json:"clone_url"`
}

// GitHubComment represents a GitHub comment
type GitHubComment struct {
	ID        int        `json:"id"`
	Body      string     `json:"body"`
	User      GitHubUser `json:"user"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	HTMLURL   string     `json:"html_url"`
}

// GitHubError represents a GitHub API error response
type GitHubError struct {
	Message          string `json:"message"`
	DocumentationURL string `json:"documentation_url,omitempty"`
}

// UI Event Types for application state management

// StatusType represents different types of status messages
type StatusType int

const (
	StatusDefault StatusType = iota
	StatusThinking
	StatusGenerating
	StatusWorking
	StatusProcessing
	StatusPreparing
	StatusError
)

// StatusProgress represents progress information for status messages
type StatusProgress struct {
	Current int
	Total   int
}

// UIEvent interface for all UI-related events
type UIEvent interface {
	GetType() UIEventType
}

// UIEventType defines types of UI events
type UIEventType int

const (
	UIEventUpdateHistory UIEventType = iota
	UIEventStreamingContent
	UIEventSetStatus
	UIEventUpdateStatus
	UIEventShowError
	UIEventClearError
	UIEventClearInput
	UIEventSetInput
	UIEventUserInput
	UIEventModelSelected
	UIEventThemeSelected
	UIEventConversationSelected
	UIEventInitializeConversationSelection
	UIEventFileSelected
	UIEventFileSelectionRequest
	UIEventSetupFileSelection
	UIEventScrollRequest
	UIEventFocusRequest
	UIEventResize
	UIEventDebugKey
	UIEventToggleHelpBar
	UIEventHideHelpBar
	UIEventExitSelectionMode
	UIEventInitializeTextSelection
	UIEventConversationsLoaded
	UIEventToolExecutionStarted
	UIEventToolExecutionProgress
	UIEventToolExecutionCompleted
	UIEventParallelToolsStart
	UIEventParallelToolsComplete
	UIEventA2ATaskSubmitted
	UIEventA2ATaskStatusUpdate
	UIEventA2ATaskCompleted
	UIEventA2ATaskInputRequired
)

// ScrollDirection defines scroll direction
type ScrollDirection int

const (
	ScrollUp ScrollDirection = iota
	ScrollDown
	ScrollLeft
	ScrollRight
	ScrollToTop
	ScrollToBottom
)
