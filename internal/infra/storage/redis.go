package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"time"

	redis "github.com/go-redis/redis/v8"

	domain "github.com/inference-gateway/cli/internal/domain"
)

// RedisStorage implements ConversationStorage using Redis
type RedisStorage struct {
	client *redis.Client
	ttl    time.Duration
}

// verifyRedisAvailable checks if Redis is available
func verifyRedisAvailable(config RedisConfig) error {
	options := &redis.Options{
		Addr:     fmt.Sprintf("%s:%d", config.Host, config.Port),
		DB:       config.Database,
		Password: config.Password,
		Username: config.Username,
	}

	client := redis.NewClient(options)
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis connection failed: %w\n\n"+
			"Redis server not available. Verify:\n"+
			"  - Redis server is running\n"+
			"  - Connection details are correct\n"+
			"  - Network connectivity to %s:%d", err, config.Host, config.Port)
	}

	return nil
}

// NewRedisStorage creates a new Redis storage instance
func NewRedisStorage(config RedisConfig) (*RedisStorage, error) {
	if err := verifyRedisAvailable(config); err != nil {
		return nil, err
	}

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

		summary := ConversationSummary{
			ID:           metadata.ID,
			Title:        metadata.Title,
			CreatedAt:    metadata.CreatedAt,
			UpdatedAt:    metadata.UpdatedAt,
			MessageCount: metadata.MessageCount,
			TokenStats:   metadata.TokenStats,
			Model:        metadata.Model,
			Tags:         metadata.Tags,

			TitleGenerated:      metadata.TitleGenerated,
			TitleInvalidated:    metadata.TitleInvalidated,
			TitleGenerationTime: metadata.TitleGenerationTime,
		}

		summaries = append(summaries, summary)
	}

	slices.SortFunc(summaries, func(a, b ConversationSummary) int {
		return b.UpdatedAt.Compare(a.UpdatedAt)
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
			summary := ConversationSummary{
				ID:           metadata.ID,
				Title:        metadata.Title,
				CreatedAt:    metadata.CreatedAt,
				UpdatedAt:    metadata.UpdatedAt,
				MessageCount: metadata.MessageCount,
				TokenStats:   metadata.TokenStats,
				Model:        metadata.Model,
				Tags:         metadata.Tags,

				TitleGenerated:      metadata.TitleGenerated,
				TitleInvalidated:    metadata.TitleInvalidated,
				TitleGenerationTime: metadata.TitleGenerationTime,
			}
			summaries = append(summaries, summary)

			if len(summaries) >= limit {
				break
			}
		}
	}

	slices.SortFunc(summaries, func(a, b ConversationSummary) int {
		return b.UpdatedAt.Compare(a.UpdatedAt)
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

// sessionGroupsKey is the Redis hash where session-group entries live, keyed
// by group key with the JSON-serialized SessionGroupEntry as the value.
const sessionGroupsKey = "session_groups"

// GetSessionGroup returns the entry for groupKey or (_, false, nil) if missing.
func (s *RedisStorage) GetSessionGroup(ctx context.Context, groupKey string) (SessionGroupEntry, bool, error) {
	raw, err := s.client.HGet(ctx, sessionGroupsKey, groupKey).Result()
	if err != nil {
		if err == redis.Nil {
			return SessionGroupEntry{}, false, nil
		}
		return SessionGroupEntry{}, false, fmt.Errorf("redis HGET %s/%s: %w", sessionGroupsKey, groupKey, err)
	}

	var entry SessionGroupEntry
	if err := json.Unmarshal([]byte(raw), &entry); err != nil {
		return SessionGroupEntry{}, false, fmt.Errorf("decode session group %s: %w", groupKey, err)
	}
	return entry, true, nil
}

// PutSessionGroup creates or replaces the entry for groupKey via an atomic
// HSET. If a TTL is configured for this Redis backend, the TTL is refreshed on
// the parent hash so the index doesn't outlive the conversation data.
func (s *RedisStorage) PutSessionGroup(ctx context.Context, groupKey string, entry SessionGroupEntry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode session group %s: %w", groupKey, err)
	}

	if err := s.client.HSet(ctx, sessionGroupsKey, groupKey, data).Err(); err != nil {
		return fmt.Errorf("redis HSET %s/%s: %w", sessionGroupsKey, groupKey, err)
	}

	if s.ttl > 0 {
		if err := s.client.Expire(ctx, sessionGroupsKey, s.ttl).Err(); err != nil {
			return fmt.Errorf("redis EXPIRE %s: %w", sessionGroupsKey, err)
		}
	}
	return nil
}

// ListSessionGroups returns all entries from the session-groups hash.
func (s *RedisStorage) ListSessionGroups(ctx context.Context) (map[string]SessionGroupEntry, error) {
	raw, err := s.client.HGetAll(ctx, sessionGroupsKey).Result()
	if err != nil {
		return nil, fmt.Errorf("redis HGETALL %s: %w", sessionGroupsKey, err)
	}

	out := make(map[string]SessionGroupEntry, len(raw))
	for k, v := range raw {
		var entry SessionGroupEntry
		if err := json.Unmarshal([]byte(v), &entry); err != nil {
			return nil, fmt.Errorf("decode session group %s: %w", k, err)
		}
		out[k] = entry
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// ScheduledJobStorage (RedisStorage)
// ---------------------------------------------------------------------------

const (
	redisScheduledJobsKey = "scheduled_jobs"
	redisPlansKey         = "plans"
	redisShellHistoryKey  = "shell_history"
)

// scheduledJobKey returns the Redis key for a scheduled job.
func (s *RedisStorage) scheduledJobKey(id string) string {
	return fmt.Sprintf("%s:%s", redisScheduledJobsKey, id)
}

// SaveJob creates or updates a scheduled job.
func (s *RedisStorage) SaveJob(ctx context.Context, job *domain.ScheduledJob) error {
	data, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("marshal job %s: %w", job.ID, err)
	}
	key := s.scheduledJobKey(job.ID)
	if s.ttl > 0 {
		return s.client.Set(ctx, key, data, s.ttl).Err()
	}
	return s.client.Set(ctx, key, data, 0).Err()
}

// LoadJob returns a job by ID.
func (s *RedisStorage) LoadJob(ctx context.Context, id string) (*domain.ScheduledJob, error) {
	data, err := s.client.Get(ctx, s.scheduledJobKey(id)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, ErrJobNotFound
		}
		return nil, fmt.Errorf("get job %s: %w", id, err)
	}
	var job domain.ScheduledJob
	if err := json.Unmarshal(data, &job); err != nil {
		return nil, fmt.Errorf("unmarshal job %s: %w", id, err)
	}
	return &job, nil
}

// ListJobs returns all jobs sorted by CreatedAt ascending.
func (s *RedisStorage) ListJobs(ctx context.Context) ([]*domain.ScheduledJob, error) {
	keys, err := s.client.Keys(ctx, redisScheduledJobsKey+":*").Result()
	if err != nil {
		return nil, fmt.Errorf("list job keys: %w", err)
	}
	if len(keys) == 0 {
		return nil, nil
	}
	pipe := s.client.Pipeline()
	var cmds []*redis.StringCmd
	for _, k := range keys {
		cmds = append(cmds, pipe.Get(ctx, k))
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, fmt.Errorf("load jobs: %w", err)
	}
	var jobs []*domain.ScheduledJob
	for _, cmd := range cmds {
		data, err := cmd.Bytes()
		if err != nil {
			continue
		}
		var job domain.ScheduledJob
		if err := json.Unmarshal(data, &job); err != nil {
			continue
		}
		jobs = append(jobs, &job)
	}
	slices.SortFunc(jobs, func(a, b *domain.ScheduledJob) int {
		return a.CreatedAt.Compare(b.CreatedAt)
	})
	return jobs, nil
}

// DeleteJob removes a job by ID.
func (s *RedisStorage) DeleteJob(ctx context.Context, id string) error {
	result, err := s.client.Del(ctx, s.scheduledJobKey(id)).Result()
	if err != nil {
		return fmt.Errorf("delete job %s: %w", id, err)
	}
	if result == 0 {
		return ErrJobNotFound
	}
	return nil
}

// Watch returns a channel that polls every 2s for changes.
func (s *RedisStorage) Watch(ctx context.Context) <-chan ScheduledJobChangeEvent {
	ch := make(chan ScheduledJobChangeEvent)
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		known := make(map[string]bool)
		for {
			select {
			case <-ctx.Done():
				close(ch)
				return
			case <-ticker.C:
				keys, err := s.client.Keys(ctx, redisScheduledJobsKey+":*").Result()
				if err != nil {
					continue
				}
				seen := make(map[string]bool)
				for _, k := range keys {
					id := k[len(redisScheduledJobsKey)+1:]
					seen[id] = true
					if !known[id] {
						ch <- ScheduledJobChangeEvent{ID: id, Type: "create"}
					}
				}
				for id := range known {
					if !seen[id] {
						ch <- ScheduledJobChangeEvent{ID: id, Type: "delete"}
					}
				}
				known = seen
			}
		}
	}()
	return ch
}

// ---------------------------------------------------------------------------
// PlanStorage (RedisStorage)
// ---------------------------------------------------------------------------

// planKey returns the Redis key for a plan.
func (s *RedisStorage) planKey(id string) string {
	return fmt.Sprintf("%s:%s", redisPlansKey, id)
}

// SavePlan creates a plan record.
func (s *RedisStorage) SavePlan(ctx context.Context, plan *PlanRecord) error {
	data, err := json.Marshal(plan)
	if err != nil {
		return fmt.Errorf("marshal plan %s: %w", plan.ID, err)
	}
	key := s.planKey(plan.ID)
	if s.ttl > 0 {
		return s.client.Set(ctx, key, data, s.ttl).Err()
	}
	return s.client.Set(ctx, key, data, 0).Err()
}

// LoadPlan returns a plan by ID.
func (s *RedisStorage) LoadPlan(ctx context.Context, id string) (*PlanRecord, error) {
	data, err := s.client.Get(ctx, s.planKey(id)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("plan not found: %s", id)
		}
		return nil, fmt.Errorf("get plan %s: %w", id, err)
	}
	var plan PlanRecord
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, fmt.Errorf("unmarshal plan %s: %w", id, err)
	}
	return &plan, nil
}

// ListPlans returns all plans sorted by CreatedAt descending.
func (s *RedisStorage) ListPlans(ctx context.Context) ([]*PlanRecord, error) {
	keys, err := s.client.Keys(ctx, redisPlansKey+":*").Result()
	if err != nil {
		return nil, fmt.Errorf("list plan keys: %w", err)
	}
	if len(keys) == 0 {
		return nil, nil
	}
	pipe := s.client.Pipeline()
	var cmds []*redis.StringCmd
	for _, k := range keys {
		cmds = append(cmds, pipe.Get(ctx, k))
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, fmt.Errorf("load plans: %w", err)
	}
	var plans []*PlanRecord
	for _, cmd := range cmds {
		data, err := cmd.Bytes()
		if err != nil {
			continue
		}
		var plan PlanRecord
		if err := json.Unmarshal(data, &plan); err != nil {
			continue
		}
		plans = append(plans, &plan)
	}
	slices.SortFunc(plans, func(a, b *PlanRecord) int {
		return b.CreatedAt.Compare(a.CreatedAt)
	})
	return plans, nil
}

// DeletePlan removes a plan by ID.
func (s *RedisStorage) DeletePlan(ctx context.Context, id string) error {
	result, err := s.client.Del(ctx, s.planKey(id)).Result()
	if err != nil {
		return fmt.Errorf("delete plan %s: %w", id, err)
	}
	if result == 0 {
		return fmt.Errorf("plan not found: %s", id)
	}
	return nil
}

// ---------------------------------------------------------------------------
// ShellHistoryStorage (RedisStorage)
// ---------------------------------------------------------------------------

// AppendHistory appends a command to the shell history list.
func (s *RedisStorage) AppendHistory(ctx context.Context, command string) error {
	if err := s.client.LPush(ctx, redisShellHistoryKey, command).Err(); err != nil {
		return fmt.Errorf("append shell history: %w", err)
	}
	return nil
}

// LoadHistory returns the most recent commands up to limit.
func (s *RedisStorage) LoadHistory(ctx context.Context, limit int) ([]string, error) {
	commands, err := s.client.LRange(ctx, redisShellHistoryKey, 0, int64(limit-1)).Result()
	if err != nil {
		return nil, fmt.Errorf("load shell history: %w", err)
	}
	// Reverse to get chronological order
	for i, j := 0, len(commands)-1; i < j; i, j = i+1, j-1 {
		commands[i], commands[j] = commands[j], commands[i]
	}
	return commands, nil
}
