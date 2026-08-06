package container

import (
	"fmt"
	"strings"
	"testing"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
)

// SDK-internal HTTP retries must reach the RetryNotifier hook so remote
// channels (Telegram) see progress during backoff.
func TestCreateRetryConfigNotifiesRetries(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Client.Retry.Enabled = true
	c := &ServiceContainer{config: cfg}

	var got string
	domain.RetryNotifier = func(m string) { got = m }
	t.Cleanup(func() { domain.RetryNotifier = nil })

	rc := c.createRetryConfig()
	rc.OnRetry(2, fmt.Errorf("HTTP 502"), 10*time.Second)

	for _, want := range []string{"HTTP 502", "attempt 2", "10s"} {
		if !strings.Contains(got, want) {
			t.Errorf("notification %q missing %q", got, want)
		}
	}
}
