package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/inference-gateway/cli/internal/domain"
)

// RedisStorage implements ConversationStorage using Redis
type RedisStorage struct {
	client *redis.Client
	ttl    time.Duration
}

// NewRedisStorage creates a new Redis storage instance
func NewRedisStorage(config RedisConfig) (*RedisStorage, error) {
	options := &redis.Options{
		Addr:     fmt.Sprintf("%s:%d", config.Host, config.Port),
		DB:       config.Database,
		Password: config.Password,
		Username: config.Username,
	}

	client := redis.NewClient(options)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	var ttl time.Duration
	if config.TTL > 0 {
		ttl = time.Duration(config.TTL) * time.Second
	}

	return &RedisStorage{
		client: client,
		ttl:    ttl,
	}, nil
}

// conversationKey generates the Redis key for a conversation's metadata
func (s *RedisStorage) conversationKey(conversationID string) string {
	return fmt.Sprintf("conversation:%s", conversationID)
}

// conversationEntriesKey generates the Redis key for a conversation's entries
func (s *RedisStorage) conversationEntriesKey(conversationID string) string {
	return fmt.Sprintf("conversation:%s:entries", conversationID)
}

// conversationIndexKey generates the Redis key for the conversation index (sorted set)
func (s *RedisStorage) conversationIndexKey() string {
	return "conversations:index"
}

// SaveConversation saves a conversation with its entries
func (s *RedisStorage) SaveConversation(ctx context.Context, conversationID string, entries []domain.ConversationEntry, metadata ConversationMetadata) error {
	pipe := s.client.Pipeline()

	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	convKey := s.conversationKey(conversationID)
	if s.ttl > 0 {
		pipe.Set(ctx, convKey, metadataJSON, s.ttl)
	} else {
		pipe.Set(ctx, convKey, metadataJSON, 0)
	}

	entriesJSON, err := json.Marshal(entries)
	if err != nil {
		return fmt.Errorf("failed to marshal entries: %w", err)
	}

	entriesKey := s.conversationEntriesKey(conversationID)
	if s.ttl > 0 {
		pipe.Set(ctx, entriesKey, entriesJSON, s.ttl)
	} else {
		pipe.Set(ctx, entriesKey, entriesJSON, 0)
	}

	indexKey := s.conversationIndexKey()
	score := float64(metadata.UpdatedAt.Unix())
	pipe.ZAdd(ctx, indexKey, &redis.Z{
		Score:  score,
		Member: conversationID,
	})

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to save conversation: %w", err)
	}

	return nil
}

// LoadConversation loads a conversation by its ID
func (s *RedisStorage) LoadConversation(ctx context.Context, conversationID string) ([]domain.ConversationEntry, ConversationMetadata, error) {
	var metadata ConversationMetadata
	var entries []domain.ConversationEntry

	pipe := s.client.Pipeline()
	metadataCmd := pipe.Get(ctx, s.conversationKey(conversationID))
	entriesCmd := pipe.Get(ctx, s.conversationEntriesKey(conversationID))

	_, err := pipe.Exec(ctx)
	if err != nil {
		if err == redis.Nil {
			return nil, metadata, fmt.Errorf("conversation not found: %s", conversationID)
		}
		return nil, metadata, fmt.Errorf("failed to load conversation: %w", err)
	}

	metadataJSON, err := metadataCmd.Result()
	if err != nil {
		if err == redis.Nil {
			return nil, metadata, fmt.Errorf("conversation not found: %s", conversationID)
		}
		return nil, metadata, fmt.Errorf("failed to get metadata: %w", err)
	}

	if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {
		return nil, metadata, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	entriesJSON, err := entriesCmd.Result()
	if err != nil {
		if err == redis.Nil {
			return nil, metadata, fmt.Errorf("conversation entries not found: %s", conversationID)
		}
		return nil, metadata, fmt.Errorf("failed to get entries: %w", err)
	}

	if err := json.Unmarshal([]byte(entriesJSON), &entries); err != nil {
		return nil, metadata, fmt.Errorf("failed to unmarshal entries: %w", err)
	}

	return entries, metadata, nil
}

// ListConversations returns a list of conversation summaries
func (s *RedisStorage) ListConversations(ctx context.Context, limit, offset int) ([]ConversationSummary, error) {
	indexKey := s.conversationIndexKey()

	conversationIDs, err := s.client.ZRevRange(ctx, indexKey, int64(offset), int64(offset+limit-1)).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get conversation index: %w", err)
	}

	if len(conversationIDs) == 0 {
		return []ConversationSummary{}, nil
	}

	pipe := s.client.Pipeline()
	var metadataCmds []*redis.StringCmd

	for _, conversationID := range conversationIDs {
		cmd := pipe.Get(ctx, s.conversationKey(conversationID))
		metadataCmds = append(metadataCmds, cmd)
	}

	_, err = pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("failed to load conversation metadata: %w", err)
	}

	var summaries []ConversationSummary
	for i, cmd := range metadataCmds {
		metadataJSON, err := cmd.Result()
		if err != nil {
			if err == redis.Nil {
				continue
			}
			return nil, fmt.Errorf("failed to get metadata for conversation %s: %w", conversationIDs[i], err)
		}

		var metadata ConversationMetadata
		if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata for conversation %s: %w", conversationIDs[i], err)
		}

		summary := ConversationSummary(metadata)

		summaries = append(summaries, summary)
	}

	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].UpdatedAt.After(summaries[j].UpdatedAt)
	})

	return summaries, nil
}

// ListConversationsNeedingTitles returns conversations that need title generation
func (s *RedisStorage) ListConversationsNeedingTitles(ctx context.Context, limit int) ([]ConversationSummary, error) {
	indexKey := s.conversationIndexKey()

	conversationIDs, err := s.client.ZRevRange(ctx, indexKey, 0, int64(limit*2-1)).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get conversation index: %w", err)
	}

	if len(conversationIDs) == 0 {
		return []ConversationSummary{}, nil
	}

	pipe := s.client.Pipeline()
	var metadataCmds []*redis.StringCmd

	for _, conversationID := range conversationIDs {
		cmd := pipe.Get(ctx, s.conversationKey(conversationID))
		metadataCmds = append(metadataCmds, cmd)
	}

	_, err = pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("failed to load conversation metadata: %w", err)
	}

	var summaries []ConversationSummary
	for i, cmd := range metadataCmds {
		metadataJSON, err := cmd.Result()
		if err != nil {
			if err == redis.Nil {
				continue
			}
			return nil, fmt.Errorf("failed to get metadata for conversation %s: %w", conversationIDs[i], err)
		}

		var metadata ConversationMetadata
		if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata for conversation %s: %w", conversationIDs[i], err)
		}

		if (!metadata.TitleGenerated || metadata.TitleInvalidated) && metadata.MessageCount > 0 {
			summary := ConversationSummary(metadata)
			summaries = append(summaries, summary)

			if len(summaries) >= limit {
				break
			}
		}
	}

	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].UpdatedAt.After(summaries[j].UpdatedAt)
	})

	return summaries, nil
}

// DeleteConversation removes a conversation by its ID
func (s *RedisStorage) DeleteConversation(ctx context.Context, conversationID string) error {
	pipe := s.client.Pipeline()

	pipe.Del(ctx, s.conversationKey(conversationID))

	pipe.Del(ctx, s.conversationEntriesKey(conversationID))

	pipe.ZRem(ctx, s.conversationIndexKey(), conversationID)

	results, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete conversation: %w", err)
	}

	deletedCount := results[0].(*redis.IntCmd).Val() + results[1].(*redis.IntCmd).Val()
	if deletedCount == 0 {
		return fmt.Errorf("conversation not found: %s", conversationID)
	}

	return nil
}

// UpdateConversationMetadata updates metadata for a conversation
func (s *RedisStorage) UpdateConversationMetadata(ctx context.Context, conversationID string, metadata ConversationMetadata) error {
	exists, err := s.client.Exists(ctx, s.conversationKey(conversationID)).Result()
	if err != nil {
		return fmt.Errorf("failed to check conversation existence: %w", err)
	}

	if exists == 0 {
		return fmt.Errorf("conversation not found: %s", conversationID)
	}

	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	pipe := s.client.Pipeline()

	convKey := s.conversationKey(conversationID)
	if s.ttl > 0 {
		pipe.Set(ctx, convKey, metadataJSON, s.ttl)
	} else {
		pipe.Set(ctx, convKey, metadataJSON, 0)
	}

	indexKey := s.conversationIndexKey()
	score := float64(metadata.UpdatedAt.Unix())
	pipe.ZAdd(ctx, indexKey, &redis.Z{
		Score:  score,
		Member: conversationID,
	})

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to update conversation metadata: %w", err)
	}

	return nil
}

// Close closes the Redis connection
func (s *RedisStorage) Close() error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

// Health checks if Redis is reachable and functional
func (s *RedisStorage) Health(ctx context.Context) error {
	if s.client == nil {
		return fmt.Errorf("redis client is nil")
	}

	if err := s.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis ping failed: %w", err)
	}

	testKey := "health_check_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	testValue := "ok"

	if err := s.client.Set(ctx, testKey, testValue, time.Second*10).Err(); err != nil {
		return fmt.Errorf("redis set test failed: %w", err)
	}

	result, err := s.client.Get(ctx, testKey).Result()
	if err != nil {
		return fmt.Errorf("redis get test failed: %w", err)
	}

	if result != testValue {
		return fmt.Errorf("redis test value mismatch: expected %s, got %s", testValue, result)
	}

	s.client.Del(ctx, testKey)

	return nil
}
