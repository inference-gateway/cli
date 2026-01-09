package domain

import (
	"context"
	"time"

	adk "github.com/inference-gateway/adk/types"
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

// ImageAttachment represents an image attachment in a message
type ImageAttachment struct {
	Data        string `json:"data"`
	MimeType    string `json:"mime_type"`
	Filename    string `json:"filename,omitempty"`
	DisplayName string `json:"display_name"`
	SourcePath  string `json:"-"`
}

// Computer use result types

// ScreenRegion represents a rectangular region of the screen
type ScreenRegion struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// Screenshot represents a captured screenshot with metadata
type Screenshot struct {
	ID             string    `json:"id"`
	Timestamp      time.Time `json:"timestamp"`
	Data           string    `json:"data"`            // base64 encoded image
	Width          int       `json:"width"`           // Final image width (after scaling)
	Height         int       `json:"height"`          // Final image height (after scaling)
	Format         string    `json:"format"`          // "png" or "jpeg"
	Method         string    `json:"method"`          // "x11" or "wayland"
	OriginalWidth  int       `json:"original_width"`  // Screen width before scaling
	OriginalHeight int       `json:"original_height"` // Screen height before scaling
}

// ScreenshotProvider defines the interface for getting screenshots from a buffer
type ScreenshotProvider interface {
	GetLatestScreenshot() (*Screenshot, error)
}

// RateLimiter defines the interface for rate limiting computer use actions
type RateLimiter interface {
	// CheckAndRecord checks if the action is within rate limits and records it
	CheckAndRecord(toolName string) error
	// GetCurrentCount returns the number of actions in the current window
	GetCurrentCount() int
	// Reset clears all recorded actions
	Reset()
}

// ScreenshotToolResult represents the result of a screenshot capture
type ScreenshotToolResult struct {
	Display string        `json:"display"`
	Region  *ScreenRegion `json:"region,omitempty"`
	Width   int           `json:"width"`
	Height  int           `json:"height"`
	Format  string        `json:"format"`
	Method  string        `json:"method"`
}

// MouseMoveToolResult represents the result of a mouse move operation
type MouseMoveToolResult struct {
	FromX   int    `json:"from_x"`
	FromY   int    `json:"from_y"`
	ToX     int    `json:"to_x"`
	ToY     int    `json:"to_y"`
	Display string `json:"display"`
	Method  string `json:"method"`
}

// MouseClickToolResult represents the result of a mouse click operation
type MouseClickToolResult struct {
	Button  string `json:"button"`
	Clicks  int    `json:"clicks"`
	X       int    `json:"x"`
	Y       int    `json:"y"`
	Display string `json:"display"`
	Method  string `json:"method"`
}

// KeyboardTypeToolResult represents the result of a keyboard input operation
type KeyboardTypeToolResult struct {
	Text     string `json:"text,omitempty"`
	KeyCombo string `json:"key_combo,omitempty"`
	Display  string `json:"display"`
	Method   string `json:"method"`
}

// ConversationEntry represents a message in the conversation with metadata
type ConversationEntry struct {
	// Core message fields
	Message Message           `json:"message"`
	Model   string            `json:"model,omitempty"`
	Time    time.Time         `json:"time"`
	Hidden  bool              `json:"hidden,omitempty"`
	Images  []ImageAttachment `json:"images,omitempty"`

	// Tool-related fields
	ToolExecution      *ToolExecutionResult               `json:"tool_execution,omitempty"`
	PendingToolCall    *sdk.ChatCompletionMessageToolCall `json:"pending_tool_call,omitempty"`
	ToolApprovalStatus ToolApprovalStatus                 `json:"tool_approval_status,omitempty"`

	// Plan mode fields
	Rejected           bool               `json:"rejected,omitempty"`
	IsPlan             bool               `json:"is_plan,omitempty"`
	PlanApprovalStatus PlanApprovalStatus `json:"plan_approval_status,omitempty"`
}

// PlanApprovalStatus represents the approval status of a plan
type PlanApprovalStatus int

const (
	PlanApprovalPending PlanApprovalStatus = iota
	PlanApprovalAccepted
	PlanApprovalRejected
)

// ToolApprovalStatus represents the approval status of a tool
type ToolApprovalStatus int

const (
	ToolApprovalPending ToolApprovalStatus = iota
	ToolApprovalApproved
	ToolApprovalRejected
)

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
	LastInputTokens   int `json:"last_input_tokens"`
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
	AddTokenUsage(model string, inputTokens, outputTokens, totalTokens int) error
	GetSessionTokens() SessionTokenStats
	GetSessionCostStats() SessionCostStats
	FormatToolResultForLLM(result *ToolExecutionResult) string
	FormatToolResultForUI(result *ToolExecutionResult, terminalWidth int) string
	FormatToolResultExpanded(result *ToolExecutionResult, terminalWidth int) string
	RemovePendingToolCallByID(toolCallID string)
	StartNewConversation(title string) error
	DeleteMessagesAfterIndex(index int) error
	GetCurrentConversationTitle() string
}

// ConversationOptimizerService optimizes conversation history to reduce token usage
type ConversationOptimizerService interface {
	OptimizeMessages(messages []sdk.Message, model string, force bool) []sdk.Message
}

// ModelService handles model selection and information
type ModelService interface {
	ListModels(ctx context.Context) ([]string, error)
	SelectModel(modelID string) error
	GetCurrentModel() string
	IsModelAvailable(modelID string) bool
	ValidateModel(modelID string) error
	IsVisionModel(modelID string) bool
}

// ChatEvent represents events during chat operations
type ChatEvent interface {
	GetRequestID() string
	GetTimestamp() time.Time
}

// EventBridge multicasts chat events to multiple subscribers (e.g., terminal UI and floating window)
type EventBridge interface {
	// Tap intercepts an event stream and multicasts it to all subscribers
	// Returns a new channel that mirrors the input channel
	Tap(input <-chan ChatEvent) <-chan ChatEvent

	// Publish broadcasts an event to all subscribers
	Publish(event ChatEvent)
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

// MessageQueue handles centralized message queuing for all components
type MessageQueue interface {
	// Enqueue adds a message to the queue
	Enqueue(message Message, requestID string)

	// Dequeue removes and returns the next message from the queue
	// Returns nil if the queue is empty
	Dequeue() *QueuedMessage

	// Peek returns the next message without removing it
	// Returns nil if the queue is empty
	Peek() *QueuedMessage

	// Size returns the number of messages in the queue
	Size() int

	// IsEmpty returns true if the queue has no messages
	IsEmpty() bool

	// Clear removes all messages from the queue
	Clear()

	// GetAll returns all messages in the queue without removing them
	GetAll() []QueuedMessage
}

// StateManager interface defines state management operations
type StateManager interface {
	// View state management
	GetCurrentView() ViewState
	GetPreviousView() ViewState
	TransitionToView(newView ViewState) error

	// Agent mode management
	GetAgentMode() AgentMode
	SetAgentMode(mode AgentMode)
	CycleAgentMode() AgentMode

	// Chat session management
	SetChatPending()
	StartChatSession(requestID, model string, eventChan <-chan ChatEvent) error
	UpdateChatStatus(status ChatStatus) error
	EndChatSession()
	GetChatSession() *ChatSession
	IsAgentBusy() bool

	// Event multicast for floating window
	SetEventBridge(bridge EventBridge)
	BroadcastEvent(event ChatEvent)

	// Tool execution management
	StartToolExecution(toolCalls []sdk.ChatCompletionMessageToolCall) error
	CompleteCurrentTool(result *ToolExecutionResult) error
	FailCurrentTool(result *ToolExecutionResult) error
	EndToolExecution()
	GetToolExecution() *ToolExecutionSession

	// Dimensions management
	SetDimensions(width, height int)
	GetDimensions() (int, int)

	// File selection management
	SetupFileSelection(files []string)
	GetFileSelectionState() *FileSelectionState
	UpdateFileSearchQuery(query string)
	SetFileSelectedIndex(index int)
	ClearFileSelectionState()

	// Approval management
	SetupApprovalUIState(toolCall *sdk.ChatCompletionMessageToolCall, responseChan chan ApprovalAction)
	GetApprovalUIState() *ApprovalUIState
	SetApprovalSelectedIndex(index int)
	ClearApprovalUIState()

	// Plan approval management
	SetupPlanApprovalUIState(planContent string, responseChan chan PlanApprovalAction)
	GetPlanApprovalUIState() *PlanApprovalUIState
	SetPlanApprovalSelectedIndex(index int)
	ClearPlanApprovalUIState()

	// Todo management
	SetTodos(todos []TodoItem)
	GetTodos() []TodoItem

	// Agent readiness management
	InitializeAgentReadiness(totalAgents int)
	UpdateAgentStatus(name string, state AgentState, message string, url string, image string)
	SetAgentError(name string, err error)
	GetAgentReadiness() *AgentReadinessState
	AreAllAgentsReady() bool
	ClearAgentReadiness()
	RemoveAgent(name string)

	// Message edit state management
	SetMessageEditState(state *MessageEditState)
	GetMessageEditState() *MessageEditState
	ClearMessageEditState()
	IsEditingMessage() bool

	// Focus management (macOS computer-use tools)
	SetLastFocusedApp(appID string)
	GetLastFocusedApp() string
	ClearLastFocusedApp()
	SetLastClickCoordinates(x, y int)
	GetLastClickCoordinates() (x, y int)
	ClearLastClickCoordinates()

	// Computer Use Pause State
	SetComputerUsePaused(paused bool, requestID string)
	IsComputerUsePaused() bool
	GetPausedRequestID() string
	ClearComputerUsePauseState()
}

// FileService handles file operations
type FileService interface {
	ListProjectFiles() ([]string, error)
	ReadFile(path string) (string, error)
	ReadFileLines(path string, startLine, endLine int) (string, error)
	ValidateFile(path string) error
	GetFileInfo(path string) (FileInfo, error)
}

// ImageService handles image operations including loading and encoding
type ImageService interface {
	// ReadImageFromFile reads an image from a file path and returns it as a base64 attachment
	ReadImageFromFile(filePath string) (*ImageAttachment, error)
	// ReadImageFromBinary reads an image from binary data and returns it as a base64 attachment
	ReadImageFromBinary(imageData []byte, filename string) (*ImageAttachment, error)
	// ReadImageFromURL fetches an image from a URL and returns it as a base64 attachment
	ReadImageFromURL(imageURL string) (*ImageAttachment, error)
	// CreateDataURL creates a data URL from an image attachment
	CreateDataURL(attachment *ImageAttachment) string
	// IsImageFile checks if a file is a supported image format
	IsImageFile(filePath string) bool
	// IsImageURL checks if a string is a valid image URL
	IsImageURL(urlStr string) bool
}

// FileInfo contains file metadata
type FileInfo struct {
	Path  string
	Size  int64
	IsDir bool
}

// MarkdownRenderer handles conversion of markdown text to styled terminal output
type MarkdownRenderer interface {
	// Render converts markdown text to styled terminal output
	Render(content string) string
	// SetWidth updates the renderer width for responsive rendering
	SetWidth(width int)
}

// TaskPollingState represents the state of background polling for a task
type TaskPollingState struct {
	TaskID          string
	ContextID       string
	AgentURL        string
	TaskDescription string
	IsPolling       bool
	StartedAt       time.Time
	LastPollAt      time.Time
	NextPollTime    time.Time
	CurrentInterval time.Duration
	LastKnownState  string
	CancelFunc      context.CancelFunc
	ResultChan      chan *ToolExecutionResult
	ErrorChan       chan error
	StatusChan      chan *A2ATaskStatusUpdate
}

// A2ATaskStatusUpdate represents a status update for an ongoing A2A task
type A2ATaskStatusUpdate struct {
	TaskID    string
	AgentURL  string
	State     string
	Message   string
	Timestamp time.Time
}

// TaskInfo wraps ADK Task with UI-specific metadata for completed/terminal tasks
// Used for A2A task retention and display
type TaskInfo struct {
	// ADK Task contains: ID, ContextID, Status (with State), History, Artifacts, Metadata
	Task adk.Task

	// UI-specific fields
	AgentURL    string
	StartedAt   time.Time
	CompletedAt time.Time
}

// TaskRetentionService manages in-memory retention of completed/terminal A2A tasks
// Only enabled when A2A is enabled - decouples task retention from StateManager
type TaskRetentionService interface {
	// AddTask adds a terminal task (completed, failed, canceled, etc.) to retention
	AddTask(task TaskInfo)

	// GetTasks returns all retained tasks
	GetTasks() []TaskInfo

	// Clear removes all retained tasks
	Clear()

	// SetMaxRetention updates the maximum retention count
	SetMaxRetention(maxRetention int)

	// GetMaxRetention returns the current maximum retention count
	GetMaxRetention() int
}

// BackgroundTaskService handles background A2A task operations
// Only enabled when A2A is enabled - provides task cancellation and retrieval
type BackgroundTaskService interface {
	// GetBackgroundTasks returns all current background polling tasks
	GetBackgroundTasks() []TaskPollingState

	// CancelBackgroundTask cancels a background task by task ID
	CancelBackgroundTask(taskID string) error
}

// TaskTracker handles task ID and context ID tracking within chat sessions
// Following A2A spec: supports multi-tenant with multiple contexts per agent
type TaskTracker interface {
	// Context management (contexts are server-generated and tracked here)
	// Multiple contexts per agent enable multi-tenant/multi-session support
	RegisterContext(agentURL, contextID string)
	GetContextsForAgent(agentURL string) []string
	GetAgentForContext(contextID string) string
	GetLatestContextForAgent(agentURL string) string
	HasContext(contextID string) bool
	RemoveContext(contextID string)

	// Task management (tasks are server-generated and scoped to contexts per A2A spec)
	AddTask(contextID, taskID string)
	GetTasksForContext(contextID string) []string
	GetLatestTaskForContext(contextID string) string
	GetContextForTask(taskID string) string
	RemoveTask(taskID string)
	HasTask(taskID string) bool

	// Agent management
	GetAllAgents() []string
	GetAllContexts() []string
	ClearAllAgents()

	// Polling state management (one polling state per task)
	StartPolling(taskID string, state *TaskPollingState)
	StopPolling(taskID string)
	GetPollingState(taskID string) *TaskPollingState
	IsPolling(taskID string) bool
	GetPollingTasksForContext(contextID string) []string
	GetAllPollingTasks() []string
}

// FetchResult represents the result of a fetch operation
type FetchResult struct {
	Content     string            `json:"content"`
	URL         string            `json:"url"`
	Status      int               `json:"status"`
	Size        int64             `json:"size"`
	ContentType string            `json:"content_type"`
	Cached      bool              `json:"cached"`
	SavedPath   string            `json:"saved_path,omitempty"`
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

// GatewayManager manages the lifecycle of the gateway (container or binary)
type GatewayManager interface {
	// Start starts the gateway container or binary if configured to run locally
	Start(ctx context.Context) error

	// Stop stops the gateway container or binary
	Stop(ctx context.Context) error

	// IsRunning returns whether the gateway is running
	IsRunning() bool

	// GetGatewayURL returns the actual gateway URL with the assigned port
	GetGatewayURL() string

	// EnsureStarted starts the gateway if configured and not already running
	// This is a convenience method that checks config and running state before starting
	EnsureStarted() error
}

// BashDetachChannelHolder manages the bash detach channel for background shell operations
type BashDetachChannelHolder interface {
	SetBashDetachChan(chan<- struct{})
	GetBashDetachChan() chan<- struct{}
	ClearBashDetachChan()
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
	GetSuccessColor() string
	GetStatusColor() string
	GetAccentColor() string
	GetDimColor() string
	GetBorderColor() string
	GetDiffAddColor() string
	GetDiffRemoveColor() string
}

// MCPDiscoveredTool represents a tool discovered from an MCP server
type MCPDiscoveredTool struct {
	ServerName  string
	Name        string
	Description string
	InputSchema any
}

// MCPServerEntry represents an MCP server configuration entry
type MCPServerEntry struct {
	Name         string
	URL          string
	Enabled      bool
	Timeout      int
	Description  string
	IncludeTools []string
	ExcludeTools []string
}

// MCPClient handles communication with MCP servers
type MCPClient interface {
	// DiscoverTools discovers all tools from enabled MCP servers
	DiscoverTools(ctx context.Context) (map[string][]MCPDiscoveredTool, error)

	// CallTool executes a tool on an MCP server
	CallTool(ctx context.Context, serverName, toolName string, args map[string]any) (any, error)

	// PingServer sends a ping request to check if a specific server is alive
	PingServer(ctx context.Context, serverName string) error

	// Close cleans up MCP client resources
	Close() error
}

// MCPManager manages the lifecycle, health monitoring, and container orchestration of MCP servers
type MCPManager interface {
	// Returns a list of clients
	GetClients() []MCPClient

	// GetTotalServers returns the total number of configured MCP servers
	GetTotalServers() int

	// StartMonitoring begins background health monitoring and returns a channel for status updates
	StartMonitoring(ctx context.Context) <-chan MCPServerStatusUpdateEvent

	// UpdateToolCount updates the tool count for a specific server
	UpdateToolCount(serverName string, count int)

	// ClearToolCount removes the tool count for a specific server
	ClearToolCount(serverName string)

	// Container lifecycle management
	// StartServers starts all MCP servers that have run=true (non-fatal)
	StartServers(ctx context.Context) error

	// StopServers stops all running MCP server containers
	StopServers(ctx context.Context) error

	// Close stops monitoring, stops containers, and cleans up resources
	Close() error
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
	Rejected  bool              `json:"rejected,omitempty"`
	Images    []ImageAttachment `json:"images,omitempty"`
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
	OriginalLines   int    `json:"original_lines"`
	NewLines        int    `json:"new_lines"`
	LinesDifference int    `json:"lines_difference"`
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

// MCPToolResult represents the result of an MCP tool execution
type MCPToolResult struct {
	ServerName string `json:"server_name"`
	ToolName   string `json:"tool_name"`
	Content    string `json:"content"`
	Error      string `json:"error,omitempty"`
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
