package tools

import (
	"fmt"
	"os"
	"sync"
	"time"

	config "github.com/inference-gateway/cli/config"
)

// DisplayServer represents the type of display server
type DisplayServer int

const (
	DisplayServerX11 DisplayServer = iota
	DisplayServerWayland
	DisplayServerUnknown
)

// DetectDisplayServer detects which display server is running
func DetectDisplayServer() DisplayServer {
	// Check for Wayland first (more modern)
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		return DisplayServerWayland
	}

	// Check for X11
	if os.Getenv("DISPLAY") != "" {
		return DisplayServerX11
	}

	return DisplayServerUnknown
}

// GetDisplayName returns a string name for the display server
func (ds DisplayServer) String() string {
	switch ds {
	case DisplayServerX11:
		return "x11"
	case DisplayServerWayland:
		return "wayland"
	default:
		return "unknown"
	}
}

// RateLimiter implements token bucket rate limiting for computer use actions
type RateLimiter struct {
	cfg         *config.RateLimitConfig
	actionTimes []time.Time
	mu          sync.Mutex
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(cfg config.RateLimitConfig) *RateLimiter {
	return &RateLimiter{
		cfg:         &cfg,
		actionTimes: make([]time.Time, 0),
	}
}

// CheckAndRecord checks if the action is within rate limits and records it
// Returns an error if the rate limit is exceeded
func (rl *RateLimiter) CheckAndRecord(toolName string) error {
	if !rl.cfg.Enabled {
		return nil // Rate limiting disabled
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	windowStart := now.Add(-time.Duration(rl.cfg.WindowSeconds) * time.Second)

	// Remove actions outside the time window
	validActions := make([]time.Time, 0)
	for _, t := range rl.actionTimes {
		if t.After(windowStart) {
			validActions = append(validActions, t)
		}
	}
	rl.actionTimes = validActions

	// Check if at limit
	if len(rl.actionTimes) >= rl.cfg.MaxActionsPerMinute {
		return fmt.Errorf("rate limit exceeded: maximum %d actions per %d seconds (current: %d actions in window)",
			rl.cfg.MaxActionsPerMinute, rl.cfg.WindowSeconds, len(rl.actionTimes))
	}

	// Record the new action
	rl.actionTimes = append(rl.actionTimes, now)
	return nil
}

// GetCurrentCount returns the number of actions in the current window
func (rl *RateLimiter) GetCurrentCount() int {
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
func (rl *RateLimiter) Reset() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.actionTimes = make([]time.Time, 0)
}
