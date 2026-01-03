package utils

import (
	"fmt"
	"sync"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
)

// TokenBucketRateLimiter implements token bucket rate limiting for computer use actions
type TokenBucketRateLimiter struct {
	cfg         *config.RateLimitConfig
	actionTimes []time.Time
	mu          sync.Mutex
}

// Ensure TokenBucketRateLimiter implements domain.RateLimiter
var _ domain.RateLimiter = (*TokenBucketRateLimiter)(nil)

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(cfg config.RateLimitConfig) domain.RateLimiter {
	return &TokenBucketRateLimiter{
		cfg:         &cfg,
		actionTimes: make([]time.Time, 0),
	}
}

// CheckAndRecord checks if the action is within rate limits and records it
// Returns an error if the rate limit is exceeded
func (rl *TokenBucketRateLimiter) CheckAndRecord(toolName string) error {
	if !rl.cfg.Enabled {
		return nil
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	windowStart := now.Add(-time.Duration(rl.cfg.WindowSeconds) * time.Second)

	validActions := make([]time.Time, 0)
	for _, t := range rl.actionTimes {
		if t.After(windowStart) {
			validActions = append(validActions, t)
		}
	}
	rl.actionTimes = validActions

	if len(rl.actionTimes) >= rl.cfg.MaxActionsPerMinute {
		return fmt.Errorf("rate limit exceeded: maximum %d actions per %d seconds (current: %d actions in window)",
			rl.cfg.MaxActionsPerMinute, rl.cfg.WindowSeconds, len(rl.actionTimes))
	}

	rl.actionTimes = append(rl.actionTimes, now)
	return nil
}

// GetCurrentCount returns the number of actions in the current window
func (rl *TokenBucketRateLimiter) GetCurrentCount() int {
	if !rl.cfg.Enabled {
		return 0
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	windowStart := now.Add(-time.Duration(rl.cfg.WindowSeconds) * time.Second)

	count := 0
	for _, t := range rl.actionTimes {
		if t.After(windowStart) {
			count++
		}
	}

	return count
}

// Reset clears all recorded actions
func (rl *TokenBucketRateLimiter) Reset() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.actionTimes = make([]time.Time, 0)
}
