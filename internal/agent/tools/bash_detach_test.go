package tools

import (
	"context"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
)

// fakeShellService is a minimal domain.BackgroundShellService for exercising the
// Bash tool's ctrl+b detach path without the real services package.
type fakeShellService struct {
	detachCalls int32
	shellID     string
	err         error
}

func (f *fakeShellService) DetachToBackground(_ context.Context, _ *exec.Cmd, _ string, _ domain.OutputRingBuffer) (string, error) {
	atomic.AddInt32(&f.detachCalls, 1)
	return f.shellID, f.err
}
func (f *fakeShellService) GetShellOutput(string, int64) (string, int64, domain.ShellState, error) {
	return "", 0, "", nil
}
func (f *fakeShellService) GetShellOutputWithFilter(string, int64, string) (string, int64, domain.ShellState, error) {
	return "", 0, "", nil
}
func (f *fakeShellService) GetShell(string) *domain.BackgroundShell { return nil }
func (f *fakeShellService) GetAllShells() []*domain.BackgroundShell { return nil }
func (f *fakeShellService) CancelShell(string) error                { return nil }
func (f *fakeShellService) RemoveShell(string) error                { return nil }

// TestBashTool_DetachOnSignal is the regression guard for "ctrl+b didn't move the
// command to the background". When the detach channel fires, the Bash tool must
// call DetachToBackground and return promptly (not run the command to completion).
func TestBashTool_DetachOnSignal(t *testing.T) {
	cfg := config.DefaultConfig()
	fake := &fakeShellService{shellID: "shell-abc123"}
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
		if got := atomic.LoadInt32(&fake.detachCalls); got != 1 {
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
