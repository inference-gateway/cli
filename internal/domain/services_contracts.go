// Cross-cutting service contracts consumed by multiple packages.

package domain

import (
	"context"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	"time"

	adk "github.com/inference-gateway/adk/types"
)

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
	ReadImageFromFile(filePath string) (*agentdomain.ImageAttachment, error)
	// ReadImageFromBinary reads an image from binary data and returns it as a base64 attachment
	ReadImageFromBinary(imageData []byte, filename string) (*agentdomain.ImageAttachment, error)
	// ReadImageFromURL fetches an image from a URL and returns it as a base64 attachment
	ReadImageFromURL(imageURL string) (*agentdomain.ImageAttachment, error)
	// CreateDataURL creates a data URL from an image attachment
	CreateDataURL(attachment *agentdomain.ImageAttachment) string
	// IsImageFile checks if a file is a supported image format
	IsImageFile(filePath string) bool
	// IsImageURL checks if a string is a valid image URL
	IsImageURL(urlStr string) bool
	// IsImageModel reports whether the model generates images rather than text
	IsImageModel(model string) bool
	// GenerateImage generates an image from prompt using model ("provider/name")
	// and returns the path of the saved file. A blank quality or size leaves the
	// provider's own default
	GenerateImage(ctx context.Context, model, prompt, quality, size string) (string, error)
	// EditImage edits the image at imagePath using prompt and model
	// ("provider/name") and returns the path of the saved file. A blank quality
	// or size leaves the provider's own default. A non-empty maskPath points to
	// a PNG whose transparent (alpha=0) areas mark the editable region; all
	// other pixels are preserved exactly.
	EditImage(ctx context.Context, model, prompt, imagePath, quality, size, maskPath string) (string, error)
	// CreateImageVariation creates a variation of the image at imagePath using
	// model ("provider/name") and returns the path of the saved file. A blank
	// size leaves the provider's own default
	CreateImageVariation(ctx context.Context, model, imagePath, size string) (string, error)
}

// FileInfo contains file metadata
type FileInfo struct {
	Path  string
	Size  int64
	IsDir bool
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
	GetBackgroundTasks() []agentdomain.TaskPollingState

	// CancelBackgroundTask cancels a background task by task ID
	CancelBackgroundTask(taskID string) error
}

// A2AClearer is the one-method projection of the A2A tracker used by
// conversation clear/switch to discard the A2A context/task graph. The concrete
// *utils.A2ATaskTrackerImpl (and the BackgroundTaskRegistry that embeds it)
// satisfies it; consumers that only clear depend on this instead of the whole
// tracker.
type A2AClearer interface {
	ClearAllAgents()
}

// BackgroundTaskRegistry is the single tracker that owns *all* in-flight
// background work an agent session can produce: A2A tasks (long-running
// work delegated to remote agents) and background bash shells (long-running
// commands the agent has detached from the foreground). Both are
// conceptually the same thing - async producers of results that need to
// land back on the conversation when they finish - so they live behind one
// type here.
//
// The interface unifies what used to be two separate trackers
// (A2ATaskTracker and ShellTracker) via composition: depending on what a
// caller needs, it can use the narrower A2ATaskTracker or ShellTracker
// interface, or this full BackgroundTaskRegistry to access both plus the
// HasPending() aggregator method.
type BackgroundTaskRegistry interface {
	agentdomain.A2ATaskTracker
	ShellTracker
	SubagentTracker

	// HasPending reports whether *any* background work is still in flight,
	// regardless of type. True when there is at least one A2A task being
	// polled, one running background shell, OR one running HEADLESS subagent.
	// It deliberately excludes interactive subagents so a one-shot `infer headless`
	// does not hang at exit waiting on a user-driven tmux pane.
	HasPending() bool

	// Submit hands a background job to the supervisor, which spawns its monitor
	// goroutine and folds its result back onto the conversation when it finishes.
	// This is the single entry point every kind (A2A task, shell, subagent) uses
	// instead of running its own poller.
	Submit(job BackgroundJob)

	// Snapshot returns the supervisor's view of all live and recently-finished
	// jobs for the task view and status line.
	Snapshot() []TrackedJob

	// CountRunningJobs returns how many supervised jobs are running, optionally
	// filtered to one kind (pass "" for all kinds).
	CountRunningJobs(kind JobKind) int

	// IsJobRunning reports whether the supervised job with the given id is still
	// running. It is the per-id liveness query a tool uses (via the narrow
	// JobLivenessReporter projection) to defer to the supervisor - the single
	// source of truth - instead of racing it with a manual read.
	IsJobRunning(id string) bool

	// WindJob sends a graceful wind-down or hard stop to one supervised job.
	WindJob(id string, sig WindSignal) error
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

// GitHubIssue is a minimal projection of a GitHub issue, big enough for both
// the autocomplete dropdown (Number, Title, State) and inline expansion into a
// chat-message block (Body, URL, Comments, UpdatedAt). Comments is nil for the
// list variant and populated for the view variant.
type GitHubIssue struct {
	Number    int
	Title     string
	Body      string
	State     string
	URL       string
	UpdatedAt time.Time
	Author    string
	Comments  []GitHubIssueComment
}

// GitHubIssueComment is a single comment on a GitHub issue, sorted by
// CreatedAt ascending.
type GitHubIssueComment struct {
	Author    string
	Body      string
	CreatedAt time.Time
}

// GitHubIssueService provides cached access to the current repository's GitHub
// issues via the gh CLI. Implementations gracefully degrade (return empty/nil
// with no error) when not in a git repo, when gh is not installed, or when the
// remote/auth is not configured - the chat input's "#" autocomplete and "#N"
// inline expansion simply become no-ops in those environments.
type GitHubIssueService interface {
	// ListIssues returns recent open issues for the current repo, newest first.
	// Results are cached for a short TTL so repeated autocomplete keystrokes
	// don't shell out per character. Returns ([], nil) on environment failures.
	ListIssues(ctx context.Context) ([]GitHubIssue, error)

	// GetIssue fetches an issue with body and the most-recent comments (capped
	// internally). Uncached. Returns (nil, err) on failure so the expansion
	// path can leave the raw token in place.
	GetIssue(ctx context.Context, number int) (*GitHubIssue, error)

	// IsAvailable reports whether the service can serve requests in the
	// current environment. Used by the autocomplete trigger to short-circuit
	// a slow first shell-out when gh / repo / auth are missing.
	IsAvailable() bool
}

// GitHubSetupService handles git/gh/CI operations for the GitHub Action CI setup
// flow triggered from the init-github-action wizard. Every shell invocation carries
// a context so a wedged subprocess cannot hang the UI.
type GitHubSetupService interface {
	GetCurrentRepo() (string, error)
	IsOrgRepo(repo string) (bool, error)
	CheckOrgSecretsExist(orgName string) (bool, error)
	SetOrgSecret(orgName, name, value string) error
	PreparePRCreation(repo, workflowPath string) (string, error)
	WriteWorkflowFile(path, content string) error
	GenerateStandardWorkflowContent() string
	GenerateGithubActionWorkflowContent() string
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

	// GetClient returns the client for a specific server by name, or nil if
	// no client is registered for that name. This is the O(1) lookup variant
	// of GetClients and should be preferred when the server name is known -
	// it avoids re-running DiscoverTools across every client just to find
	// the owning one.
	GetClient(serverName string) MCPClient

	// GetTotalServers returns the total number of configured MCP servers
	GetTotalServers() int

	// StartMonitoring begins background health monitoring, pushing every
	// MCPServerStatusUpdateEvent through the UI notifier injected at
	// construction. Idempotent; the initial status is emitted asynchronously.
	StartMonitoring(ctx context.Context)

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

// MemoryBackend syncs the persistent memory directory with a remote. The local
// backend is a no-op; the git backend pulls on run start and commits + pushes
// when a fact changes. Both directions are best-effort: an error is returned for
// tests/telemetry but callers log and continue - a sync failure never aborts the
// agent run. SyncIn is idempotent and runs at most once per process.
type MemoryBackend interface {
	SyncIn(ctx context.Context) error
	SyncOut(ctx context.Context) error
}
