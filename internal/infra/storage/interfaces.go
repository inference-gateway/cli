package storage

import (
	"context"
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
