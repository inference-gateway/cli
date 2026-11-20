package container

import (
	"fmt"
	"strings"
	"testing"
	"time"

	config "github.com/inference-gateway/cli/config"
)

// SDK-internal HTTP retries must reach the RetryNotifier hook so remote
// channels (Telegram) see progress during backoff.
func TestCreateRetryConfigNotifiesRetries(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Client.Retry.Enabled = true
	c := &ServiceContainer{config: cfg}

	var got string
	RetryNotifier = func(m string) { got = m }
	t.Cleanup(func() { RetryNotifier = nil })

	rc := c.createRetryConfig()
	rc.OnRetry(2, fmt.Errorf("HTTP 502"), 10*time.Second)

	for _, want := range []string{"HTTP 502", "attempt 2", "10s"} {
		if !strings.Contains(got, want) {
			t.Errorf("notification %q missing %q", got, want)
		}
	}
}

// SDK-internal HTTP retries drive the chat TUI's reconnecting indicator, so a
// down gateway is visible during the client's own backoff.
func TestCreateRetryConfigSetsRetryStatus(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Client.Retry.Enabled = true
	cfg.Client.Retry.MaxAttempts = 5
	c := &ServiceContainer{config: cfg}
	c.initializeStateManager()
	c.stateManager.SetChatPending()

	c.createRetryConfig().OnRetry(2, fmt.Errorf("HTTP 502"), 10*time.Second)

	got := c.stateManager.GetRetryStatus()
	if got == nil || got.Attempt != 2 || got.MaxAttempts != 4 {
		t.Fatalf("retry status = %+v, want attempt 2 of 4", got)
	}
}

func TestUseMainAgentPrompt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())

	fresh := config.DefaultConfig()
	useMainAgentPrompt(fresh)
	if !strings.HasPrefix(fresh.Prompts.Agent.SystemPrompt, "Autonomous software engineering agent.") {
		t.Fatalf("an empty system_prompt must take the main agent's body, got %q", fresh.Prompts.Agent.SystemPrompt)
	}

	overridden := config.DefaultConfig()
	overridden.Prompts.Agent.SystemPrompt = "From prompts.yaml."
	useMainAgentPrompt(overridden)
	if overridden.Prompts.Agent.SystemPrompt != "From prompts.yaml." {
		t.Fatalf("a set system_prompt must override the main agent, got %q", overridden.Prompts.Agent.SystemPrompt)
	}
}
