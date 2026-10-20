package tools

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	schedmocks "github.com/inference-gateway/cli/tests/mocks/scheduler"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// TestBashTool_DetachOnSignal is the regression guard for "ctrl+b didn't move the
// command to the background". When the detach channel fires, the Bash tool must
// call DetachToBackground and return promptly (not run the command to completion).
func TestBashTool_DetachOnSignal(t *testing.T) {
	cfg := config.DefaultConfig()
	fake := &schedmocks.FakeBackgroundShellService{}
	fake.DetachToBackgroundReturns("shell-abc123", nil)
	tool := NewBashTool(cfg, fake)

	detachChan := make(chan struct{}, 1)
	ctx := agentdomain.WithToolApproved(context.Background())
	ctx = agentdomain.WithBashOutputCallback(ctx, func(string) {})
	ctx = agentdomain.WithBashDetachChannel(ctx, detachChan)

	resCh := make(chan *agentdomain.ToolExecutionResult, 1)
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
		data, ok := r.Data.(*agentdomain.BashToolResult)
		if !ok {
			t.Fatalf("result Data is %T, want *agentdomain.BashToolResult", r.Data)
		}
		if !strings.Contains(data.Output, "detached to background") {
			t.Fatalf("output missing detach confirmation: %q", data.Output)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Execute did not return after the detach signal")
	}
}

// TestBashTool_DetachedParam verifies that passing detached=true in the tool args
// immediately detaches the command to the background, without needing a Ctrl+B signal.
func TestBashTool_DetachedParam(t *testing.T) {
	cfg := config.DefaultConfig()
	fake := &schedmocks.FakeBackgroundShellService{}
	fake.DetachToBackgroundReturns("shell-abc123", nil)
	tool := NewBashTool(cfg, fake)

	ctx := agentdomain.WithToolApproved(context.Background())
	ctx = agentdomain.WithBashOutputCallback(ctx, func(string) {})

	resCh := make(chan *agentdomain.ToolExecutionResult, 1)
	startedAt := time.Now()
	go func() {
		r, _ := tool.Execute(ctx, map[string]any{"command": "sleep 30", "detached": true})
		resCh <- r
	}()

	select {
	case r := <-resCh:
		if elapsed := time.Since(startedAt); elapsed > 5*time.Second {
			t.Fatalf("Execute ran the full command (%s); detached=true was ignored", elapsed)
		}
		if got := fake.DetachToBackgroundCallCount(); got != 1 {
			t.Fatalf("DetachToBackground called %d times, want 1", got)
		}
		if !r.Success {
			t.Fatalf("detach result not success: %+v", r)
		}
		data, ok := r.Data.(*agentdomain.BashToolResult)
		if !ok {
			t.Fatalf("result Data is %T, want *agentdomain.BashToolResult", r.Data)
		}
		if !strings.Contains(data.Output, "detached to background") {
			t.Fatalf("output missing detach confirmation: %q", data.Output)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Execute did not return after detached=true")
	}
}

// A detachable command had no timeout at all: the agent loop attaches a detach
// channel to every Bash call, and that path ran a hung command until the
// session's own budget ended it.
func TestBashTool_DetachableCommandTimesOut(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tools.Bash.Timeout = 1
	fake := &schedmocks.FakeBackgroundShellService{}
	tool := NewBashTool(cfg, fake)

	ctx := agentdomain.WithToolApproved(context.Background())
	ctx = agentdomain.WithBashOutputCallback(ctx, func(string) {})
	ctx = agentdomain.WithBashDetachChannel(ctx, make(chan struct{}, 1))
	marker := "infer-bash-detach-timeout-" + t.Name()

	startedAt := time.Now()
	r, err := tool.Execute(ctx, map[string]any{"command": ": " + marker + "; (sleep 30) & echo started; wait"})
	if err != nil {
		t.Fatalf("Execute returned an error: %v", err)
	}
	if elapsed := time.Since(startedAt); elapsed > 5*time.Second {
		t.Fatalf("Execute took %s for a 1 s timeout", elapsed)
	}
	if r.Success {
		t.Fatal("a timed-out command must not be reported as success")
	}
	data, ok := r.Data.(*agentdomain.BashToolResult)
	if !ok {
		t.Fatalf("result Data is %T, want *agentdomain.BashToolResult", r.Data)
	}
	if !strings.Contains(data.Error, "timed out") || !strings.Contains(data.Error, "detached: true") {
		t.Fatalf("error %q does not say the command timed out and how to run it in the background", data.Error)
	}
	if got := fake.DetachToBackgroundCallCount(); got != 0 {
		t.Fatalf("DetachToBackground called %d times, want 0", got)
	}
	if out, _ := exec.Command("pgrep", "-f", marker).Output(); len(out) > 0 {
		t.Fatalf("the command's child survived the timeout: pids %s", strings.TrimSpace(string(out)))
	}
}
