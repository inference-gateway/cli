package agent

import (
	"testing"
	"time"

	sdk "github.com/inference-gateway/sdk"
)

func TestRateLimitMessage(t *testing.T) {
	now := time.Date(2026, 10, 1, 22, 0, 0, 0, time.UTC)
	err := &sdk.RateLimitError{Message: "you have reached your session usage limit", RetryAfter: 2*time.Hour + 30*time.Minute + 20*time.Second}

	got := rateLimitMessage("ollamacloud", err, now).Error()
	want := "ollamacloud usage limit reached: you have reached your session usage limit. You can continue at 00:30 (in 2h 30m)"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
