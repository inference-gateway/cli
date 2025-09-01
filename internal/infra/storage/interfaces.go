package storage

import (
	"context"
	"time"

	"github.com/inference-gateway/cli/internal/domain"
)

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
type ConversationMetadata struct {
	ID                  string                     `json:"id"`
	Title               string                     `json:"title"`
	CreatedAt           time.Time                  `json:"created_at"`
	UpdatedAt           time.Time                  `json:"updated_at"`
	MessageCount        int                        `json:"message_count"`
	TokenStats          domain.SessionTokenStats   `json:"token_stats"`
	Model               string                     `json:"model,omitempty"`
	Tags                []string                   `json:"tags,omitempty"`
	Summary             string                     `json:"summary,omitempty"`
	OptimizedMessages   []domain.ConversationEntry `json:"optimized_messages,omitempty"`
	TitleGenerated      bool                       `json:"title_generated,omitempty"`
	TitleInvalidated    bool                       `json:"title_invalidated,omitempty"`
	TitleGenerationTime *time.Time                 `json:"title_generation_time,omitempty"`
}

// ConversationSummary contains summary information about a conversation
type ConversationSummary struct {
	ID                  string                   `json:"id"`
	Title               string                   `json:"title"`
	CreatedAt           time.Time                `json:"created_at"`
	UpdatedAt           time.Time                `json:"updated_at"`
	MessageCount        int                      `json:"message_count"`
	TokenStats          domain.SessionTokenStats `json:"token_stats"`
	Model               string                   `json:"model,omitempty"`
	Tags                []string                 `json:"tags,omitempty"`
	Summary             string                   `json:"summary,omitempty"`
	TitleGenerated      bool                     `json:"title_generated,omitempty"`
	TitleInvalidated    bool                     `json:"title_invalidated,omitempty"`
	TitleGenerationTime *time.Time               `json:"title_generation_time,omitempty"`
}

// StorageConfig contains configuration for storage backends
type StorageConfig struct {
	// Type specifies the storage backend type (sqlite, postgres, redis)
	Type string `json:"type" yaml:"type"`

	// SQLite specific configuration
	SQLite SQLiteConfig `json:"sqlite,omitempty" yaml:"sqlite,omitempty"`

	// Postgres specific configuration
	Postgres PostgresConfig `json:"postgres,omitempty" yaml:"postgres,omitempty"`

	// Redis specific configuration
	Redis RedisConfig `json:"redis,omitempty" yaml:"redis,omitempty"`
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
