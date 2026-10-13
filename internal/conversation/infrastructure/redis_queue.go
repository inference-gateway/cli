package infrastructure

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	redis "github.com/go-redis/redis/v8"

	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
)

// RedisQueueKeyPrefix namespaces the queue lists. External producers feed a
// running infer by pushing a JSON queuedRecord to infer:queue:<name> (RPUSH).
const RedisQueueKeyPrefix = "infer:queue:"

// redisDrainScript reads the whole list and empties it in one server-side step,
// so two draining processes never receive the same record.
const redisDrainScript = `
if redis.call('LLEN', KEYS[1]) == 0 then
	return {}
end
local entries = redis.call('LRANGE', KEYS[1], 0, -1)
redis.call('DEL', KEYS[1])
return entries
`

// NewRedisMessageQueue dials the storage.redis connection and returns the
// Redis-backed queue adapter. Startup fails fast with an error naming the
// backend when Redis is unreachable.
func NewRedisMessageQueue(conn storage.RedisConfig, name func() string, interval time.Duration, wake func()) (convdomain.MessageQueue, error) {
	client, err := storage.OpenRedis(conn)
	if err != nil {
		return nil, fmt.Errorf("queue backend redis: %w", err)
	}
	return newDurableMessageQueue("redis", &redisQueueBackend{client: client}, name, interval, wake), nil
}

// redisQueueBackend implements queueBackend over one Redis list per queue.
type redisQueueBackend struct {
	client *redis.Client
}

func (b *redisQueueBackend) key(name string) string {
	return RedisQueueKeyPrefix + name
}

func (b *redisQueueBackend) enqueue(ctx context.Context, name string, record queuedRecord) error {
	payload, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshaling queued record: %w", err)
	}
	if err := b.client.RPush(ctx, b.key(name), payload).Err(); err != nil {
		return fmt.Errorf("pushing Redis list %s: %w", b.key(name), err)
	}
	return nil
}

func (b *redisQueueBackend) drain(ctx context.Context, name string) ([]queuedRecord, error) {
	response, err := b.client.Eval(ctx, redisDrainScript, []string{b.key(name)}).Result()
	if err != nil {
		return nil, fmt.Errorf("draining Redis list %s: %w", b.key(name), err)
	}
	return redisRecords(response), nil
}

func (b *redisQueueBackend) dequeue(ctx context.Context, name string) (*queuedRecord, error) {
	payload, err := b.client.LPop(ctx, b.key(name)).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("popping Redis list %s: %w", b.key(name), err)
	}

	record, decodeErr := decodeRecord([]byte(payload))
	if decodeErr != nil {
		return nil, decodeErr
	}
	return &record, nil
}

func (b *redisQueueBackend) clear(ctx context.Context, name string) error {
	if err := b.client.Del(ctx, b.key(name)).Err(); err != nil {
		return fmt.Errorf("clearing Redis list %s: %w", b.key(name), err)
	}
	return nil
}

// list reads every record of the queue without removing them (poll reads).
func (b *redisQueueBackend) list(ctx context.Context, name string) ([]queuedRecord, error) {
	payloads, err := b.client.LRange(ctx, b.key(name), 0, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("reading Redis list %s: %w", b.key(name), err)
	}
	return decodeRecords(payloads), nil
}

// redisRecords converts a Lua script response into queued records.
func redisRecords(response any) []queuedRecord {
	items, ok := response.([]any)
	if !ok {
		return nil
	}
	payloads := make([]string, 0, len(items))
	for _, item := range items {
		if payload, isString := item.(string); isString {
			payloads = append(payloads, payload)
		}
	}
	return decodeRecords(payloads)
}

// decodeRecords parses list payloads, skipping entries that do not decode.
func decodeRecords(payloads []string) []queuedRecord {
	records := make([]queuedRecord, 0, len(payloads))
	for _, payload := range payloads {
		if record, err := decodeRecord([]byte(payload)); err == nil {
			records = append(records, record)
		}
	}
	return records
}
