package storage

import (
	"context"
	"fmt"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
)

// SessionGroupEntry tracks the active session for a given group key plus a
// rollover history so old conversations can still be looked up via
// `infer conversations list`.
type SessionGroupEntry struct {
	CurrentSessionID string    `json:"current_session_id"`
	History          []string  `json:"history,omitempty"`
	LastRollover     time.Time `json:"last_rollover,omitempty"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// SessionGroupStorage defines the interface for persisting the session-group
// index that maps a stable channel/sender key (e.g. "channel-telegram-12345")
// to the current conversation session UUID for that key.
type SessionGroupStorage interface {
	// GetSessionGroup returns the entry for groupKey. The bool is false when
	// no entry exists; the error is non-nil only on storage failure.
	GetSessionGroup(ctx context.Context, groupKey string) (SessionGroupEntry, bool, error)

	// PutSessionGroup creates or replaces the entry for groupKey atomically.
	PutSessionGroup(ctx context.Context, groupKey string, entry SessionGroupEntry) error

	// ListSessionGroups returns all entries keyed by their group key. Used by
	// administrative tooling and tests.
	ListSessionGroups(ctx context.Context) (map[string]SessionGroupEntry, error)
}

// ConversationStorage defines the interface for persistent conversation storage
type ConversationStorage interface {
	// SaveConversation saves a conversation with a unique ID
	SaveConversation(ctx context.Context, conversationID string, entries []domain.ConversationEntry, metadata ConversationMetadata) error

	// LoadConversation loads a conversation by its ID
	LoadConversation(ctx context.Context, conversationID string) ([]domain.ConversationEntry, ConversationMetadata, error)

	// ListConversations returns a list of conversation summaries
	ListConversations(ctx context.Context, limit, offset int) ([]ConversationSummary, error)

	// DeleteConversation removes a conversation by its ID
	DeleteConversation(ctx context.Context, conversationID string) error

	// UpdateConversationMetadata updates metadata for a conversation
	UpdateConversationMetadata(ctx context.Context, conversationID string, metadata ConversationMetadata) error

	// ListConversationsNeedingTitles returns conversations that need title generation
	ListConversationsNeedingTitles(ctx context.Context, limit int) ([]ConversationSummary, error)

	// Close closes the storage connection
	Close() error

	// Health checks if the storage is healthy and reachable
	Health(ctx context.Context) error
}

// ConversationMetadata contains metadata about a conversation
type ConversationMetadata = domain.ConversationMetadata

// ConversationSummary contains summary information about a conversation
type ConversationSummary = domain.ConversationSummary

// ScheduledJobChangeEvent is emitted by ScheduledJobStorage.Watch when a job
// is created, updated, or deleted.
type ScheduledJobChangeEvent struct {
	// ID is the job identifier that changed.
	ID string
	// Type is "create", "update", or "delete".
	Type string
}

// ScheduledJobStorage defines the interface for persisting scheduled jobs.
// Implementations must be safe for concurrent access.
type ScheduledJobStorage interface {
	// SaveJob creates or updates a scheduled job.
	SaveJob(ctx context.Context, job *domain.ScheduledJob) error

	// LoadJob returns a job by ID. Returns ErrJobNotFound when the job does not exist.
	LoadJob(ctx context.Context, id string) (*domain.ScheduledJob, error)

	// ListJobs returns all jobs sorted by CreatedAt ascending.
	ListJobs(ctx context.Context) ([]*domain.ScheduledJob, error)

	// DeleteJob removes a job by ID. Returns ErrJobNotFound when the job does not exist.
	DeleteJob(ctx context.Context, id string) error

	// Watch returns a channel that emits change events for all jobs. The
	// implementation must close the channel when ctx is cancelled.
	Watch(ctx context.Context) <-chan ScheduledJobChangeEvent
}

// ErrJobNotFound is returned by ScheduledJobStorage when a job ID is not found.
var ErrJobNotFound = fmt.Errorf("scheduled job not found")

// PlanRecord is a stored plan-mode plan.
type PlanRecord struct {
	ID        string    `json:"id" yaml:"id"`
	Title     string    `json:"title" yaml:"title"`
	Slug      string    `json:"slug" yaml:"slug"`
	Body      string    `json:"body" yaml:"body"`
	CreatedAt time.Time `json:"created_at" yaml:"created_at"`
}

// PlanStorage defines the interface for persisting plan-mode plans.
type PlanStorage interface {
	// SavePlan creates a plan record. The ID must be set by the caller.
	SavePlan(ctx context.Context, plan *PlanRecord) error

	// LoadPlan returns a plan by ID. Returns an error when the plan does not exist.
	LoadPlan(ctx context.Context, id string) (*PlanRecord, error)

	// ListPlans returns all plans sorted by CreatedAt descending.
	ListPlans(ctx context.Context) ([]*PlanRecord, error)

	// DeletePlan removes a plan by ID. Returns an error when the plan does not exist.
	DeletePlan(ctx context.Context, id string) error
}

// ShellHistoryStorage defines the interface for persisting shell command history.
type ShellHistoryStorage interface {
	// AppendHistory appends a command to the history log.
	AppendHistory(ctx context.Context, command string) error

	// LoadHistory returns the most recent commands up to limit.
	LoadHistory(ctx context.Context, limit int) ([]string, error)
}

// Stores is the aggregate returned by NewStorage, holding all storage backends.
type Stores struct {
	Conversations ConversationStorage
	SessionGroups SessionGroupStorage
	ScheduledJobs ScheduledJobStorage
	Plans         PlanStorage
	ShellHistory  ShellHistoryStorage
}

// StorageConfig contains configuration for storage backends
type StorageConfig struct {
	// Type specifies the storage backend type (sqlite, postgres, redis, jsonl)
	Type string `json:"type" yaml:"type"`

	// SQLite specific configuration
	SQLite SQLiteConfig `json:"sqlite,omitempty" yaml:"sqlite,omitempty"`

	// Postgres specific configuration
	Postgres PostgresConfig `json:"postgres,omitempty" yaml:"postgres,omitempty"`

	// Redis specific configuration
	Redis RedisConfig `json:"redis,omitempty" yaml:"redis,omitempty"`

	// JSONL specific configuration
	Jsonl JsonlStorageConfig `json:"jsonl,omitempty" yaml:"jsonl,omitempty"`

	// D1 specific configuration
	D1 D1Config `json:"d1,omitempty" yaml:"d1,omitempty"`
}

// SQLiteConfig contains SQLite-specific configuration
type SQLiteConfig struct {
	Path string `json:"path" yaml:"path"`
}

// PostgresConfig contains Postgres-specific configuration
type PostgresConfig struct {
	Host     string `json:"host" yaml:"host"`
	Port     int    `json:"port" yaml:"port"`
	Database string `json:"database" yaml:"database"`
	Username string `json:"username" yaml:"username"`
	Password string `json:"password" yaml:"password"`
	SSLMode  string `json:"ssl_mode" yaml:"ssl_mode"`
}

// RedisConfig contains Redis-specific configuration
type RedisConfig struct {
	Host     string `json:"host" yaml:"host"`
	Port     int    `json:"port" yaml:"port"`
	Database int    `json:"database" yaml:"database"`
	Password string `json:"password,omitempty" yaml:"password,omitempty"`
	Username string `json:"username,omitempty" yaml:"username,omitempty"`
	TTL      int    `json:"ttl,omitempty" yaml:"ttl,omitempty"` // TTL in seconds, 0 means no expiration
}

// JsonlStorageConfig contains JSONL-specific configuration
type JsonlStorageConfig struct {
	Path string `json:"path" yaml:"path"`
}

// D1Config contains Cloudflare D1-specific configuration. D1 is SQLite exposed
// over an HTTP query API, so the driver writes the same schema as SQLite but
// over the network instead of a local file handle.
type D1Config struct {
	AccountID  string `json:"account_id" yaml:"account_id"`
	DatabaseID string `json:"database_id" yaml:"database_id"`
	APIToken   string `json:"api_token" yaml:"api_token"`
	BaseURL    string `json:"base_url,omitempty" yaml:"base_url,omitempty"`
}
