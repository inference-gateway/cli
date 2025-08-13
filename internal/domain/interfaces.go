package domain

import (
	"context"
	"time"

	sdk "github.com/inference-gateway/sdk"
)

// ConversationEntry represents a message in the conversation with metadata
type ConversationEntry struct {
	Message sdk.Message `json:"message"`
	Model   string      `json:"model,omitempty"`
	Time    time.Time   `json:"time"`
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
	ApprovalView                          // View full response
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
	ExecuteTool(ctx context.Context, name string, args map[string]interface{}) (string, error)
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
