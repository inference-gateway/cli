package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
)

// TestBashTool_DetachOnSignal is the regression guard for "ctrl+b didn't move the
// command to the background". When the detach channel fires, the Bash tool must
// call DetachToBackground and return promptly (not run the command to completion).
func TestBashTool_DetachOnSignal(t *testing.T) {
	cfg := config.DefaultConfig()
	fake := &domainmocks.FakeBackgroundShellService{}
	fake.DetachToBackgroundReturns("shell-abc123", nil)
	tool := NewBashTool(cfg, fake)

	detachChan := make(chan struct{}, 1)
	ctx := domain.WithToolApproved(context.Background())
	ctx = domain.WithBashOutputCallback(ctx, func(string) {})
	ctx = domain.WithBashDetachChannel(ctx, detachChan)

	resCh := make(chan *domain.ToolExecutionResult, 1)
	startedAt := time.Now()
	go func() {
		r, _ := tool.Execute(ctx, map[string]any{"command": "sleep 30"})
		resCh <- r
	}()

	time.Sleep(150 * time.Millisecond)
	detachChan <- struct{}{}

	select {
	case r := <-resCh:
		if elapsed := time.Since(startedAt); elapsed > 5*time.Second {
			t.Fatalf("Execute ran the full command (%s); detach signal was ignored", elapsed)
		}
		if got := fake.DetachToBackgroundCallCount(); got != 1 {
			t.Fatalf("DetachToBackground called %d times, want 1", got)
		}
		if !r.Success {
			t.Fatalf("detach result not success: %+v", r)
		}
		data, ok := r.Data.(*domain.BashToolResult)
		if !ok {
			t.Fatalf("result Data is %T, want *domain.BashToolResult", r.Data)
		}
		if !strings.Contains(data.Output, "detached to background") {
			t.Fatalf("output missing detach confirmation: %q", data.Output)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Execute did not return after the detach signal")
	}
}
