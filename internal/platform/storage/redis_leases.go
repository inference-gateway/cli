package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	redis "github.com/go-redis/redis/v8"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// redisLease is the stored shape of a lease: the lapse is a unix timestamp so
// the claim script compares it without parsing dates.
type redisLease struct {
	Executor   string `json:"executor"`
	Workspace  string `json:"workspace"`
	TransferID string `json:"transfer_id"`
	UntilUnix  int64  `json:"until_unix"`
}

// redisClaimLeaseScript is the compare-and-swap: ARGV[1] is the new lease,
// ARGV[2] its executor, ARGV[3] its transfer id and ARGV[4] now. It returns
// the current holder when that holder keeps the lease, or an empty string.
const redisClaimLeaseScript = `
local current = redis.call('GET', KEYS[1])
if current then
	local held = cjson.decode(current)
	local yields = held.executor == ARGV[2]
		or (ARGV[3] ~= '' and held.transfer_id == ARGV[3])
		or tonumber(held.until_unix) < tonumber(ARGV[4])
	if not yields then
		return current
	end
end
redis.call('SET', KEYS[1], ARGV[1])
return ''
`

// redisRenewLeaseScript moves the lapse of ARGV[1]'s lease to ARGV[2] and
// returns 1, or 0 when another executor holds it.
const redisRenewLeaseScript = `
local current = redis.call('GET', KEYS[1])
if not current then
	return 0
end
local held = cjson.decode(current)
if held.executor ~= ARGV[1] then
	return 0
end
held.until_unix = tonumber(ARGV[2])
redis.call('SET', KEYS[1], cjson.encode(held))
return 1
`

// redisReleaseLeaseScript deletes the lease while ARGV[1] holds it.
const redisReleaseLeaseScript = `
local current = redis.call('GET', KEYS[1])
if current and cjson.decode(current).executor == ARGV[1] then
	redis.call('DEL', KEYS[1])
end
return 1
`

func (s *RedisStorage) leaseKey(conversationID string) string {
	return fmt.Sprintf("lease:%s", conversationID)
}

func (s *RedisStorage) GetLease(ctx context.Context, conversationID string) (agentdomain.Lease, bool, error) {
	raw, err := s.client.Get(ctx, s.leaseKey(conversationID)).Result()
	if errors.Is(err, redis.Nil) {
		return agentdomain.Lease{}, false, nil
	}
	if err != nil {
		return agentdomain.Lease{}, false, fmt.Errorf("reading the session lease: %w", err)
	}
	return decodeRedisLease(conversationID, raw)
}

func (s *RedisStorage) ClaimLease(ctx context.Context, lease agentdomain.Lease) (agentdomain.Lease, bool, error) {
	payload, err := json.Marshal(redisLease{
		Executor: lease.Executor, Workspace: lease.Workspace, TransferID: lease.TransferID, UntilUnix: lease.Until.Unix(),
	})
	if err != nil {
		return agentdomain.Lease{}, false, fmt.Errorf("encoding the session lease: %w", err)
	}
	held, err := s.client.Eval(ctx, redisClaimLeaseScript, []string{s.leaseKey(lease.ConversationID)},
		string(payload), lease.Executor, lease.TransferID, time.Now().Unix()).Text()
	if err != nil {
		return agentdomain.Lease{}, false, fmt.Errorf("claiming the session lease: %w", err)
	}
	if held == "" {
		return lease, true, nil
	}
	current, _, err := decodeRedisLease(lease.ConversationID, held)
	return current, false, err
}

func (s *RedisStorage) RenewLease(ctx context.Context, conversationID, executor string, until time.Time) error {
	renewed, err := s.client.Eval(ctx, redisRenewLeaseScript, []string{s.leaseKey(conversationID)}, executor, until.Unix()).Int()
	if err != nil {
		return fmt.Errorf("renewing the session lease: %w", err)
	}
	if renewed == 0 {
		return agentdomain.ErrLeaseLost
	}
	return nil
}

func (s *RedisStorage) ReleaseLease(ctx context.Context, conversationID, executor string) error {
	if err := s.client.Eval(ctx, redisReleaseLeaseScript, []string{s.leaseKey(conversationID)}, executor).Err(); err != nil {
		return fmt.Errorf("releasing the session lease: %w", err)
	}
	return nil
}

func decodeRedisLease(conversationID, raw string) (agentdomain.Lease, bool, error) {
	var stored redisLease
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return agentdomain.Lease{}, false, fmt.Errorf("decoding the session lease: %w", err)
	}
	return agentdomain.Lease{
		ConversationID: conversationID,
		Executor:       stored.Executor,
		Workspace:      stored.Workspace,
		TransferID:     stored.TransferID,
		Until:          time.Unix(stored.UntilUnix, 0).UTC(),
	}, true, nil
}
