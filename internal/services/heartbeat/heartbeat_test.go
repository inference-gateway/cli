package heartbeat

import (
	"context"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewService_ValidatesInterval(t *testing.T) {
	if _, err := NewService(Options{}); err == nil {
		t.Fatal("expected error when interval is zero")
	}
	if _, err := NewService(Options{Config: Config{Interval: -time.Second}}); err == nil {
		t.Fatal("expected error when interval is negative")
	}
	if _, err := NewService(Options{Config: Config{Interval: time.Second}}); err != nil {
		t.Fatalf("unexpected error for valid interval: %v", err)
	}
}

func TestService_FiresOnInterval(t *testing.T) {
	fired := &atomic.Int32{}
	svc, err := NewService(Options{
		Config: Config{
			Interval:     50 * time.Millisecond,
			InitialDelay: 0,
			Prompt:       "test prompt",
		},
		ExecCommand: func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
			fired.Add(1)
			return exec.CommandContext(ctx, "echo", "hello")
		},
		BinaryPath: "/usr/bin/true",
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if fired.Load() >= 3 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	if err := svc.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if got := fired.Load(); got < 3 {
		t.Errorf("expected at least 3 fires, got %d", got)
	}
}

func TestService_RespectsInitialDelay(t *testing.T) {
	fired := &atomic.Int32{}
	svc, err := NewService(Options{
		Config: Config{
			Interval:     20 * time.Millisecond,
			InitialDelay: 200 * time.Millisecond,
			Prompt:       "test",
		},
		ExecCommand: func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
			fired.Add(1)
			return exec.CommandContext(ctx, "echo", "hello")
		},
		BinaryPath: "/usr/bin/true",
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// At 100ms, the initial delay (200ms) should not yet have elapsed.
	time.Sleep(100 * time.Millisecond)
	if got := fired.Load(); got != 0 {
		t.Errorf("expected 0 fires before initial_delay elapses, got %d", got)
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	if err := svc.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestService_SkipsOverlappingTicks(t *testing.T) {
	fired := &atomic.Int32{}
	// Each fire blocks for 200ms (sleep), so with interval=50ms several
	// ticks would overlap if we did not guard against it. Expect roughly
	// one fire per 200ms window.
	svc, err := NewService(Options{
		Config: Config{
			Interval:     50 * time.Millisecond,
			InitialDelay: 0,
			Prompt:       "test",
		},
		ExecCommand: func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
			fired.Add(1)
			return exec.CommandContext(ctx, "sleep", "0.2")
		},
		BinaryPath: "/usr/bin/true",
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Run for 500ms — without guard we'd see ~10 fires; with the guard
	// we expect at most ~3 (one every 200ms).
	time.Sleep(500 * time.Millisecond)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	if err := svc.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	got := fired.Load()
	if got > 5 {
		t.Errorf("overlap guard failed: %d fires in 500ms with 200ms-per-fire work", got)
	}
	if got < 1 {
		t.Errorf("expected at least one fire, got %d", got)
	}
}

func TestService_StopIsIdempotent(t *testing.T) {
	svc, err := NewService(Options{
		Config: Config{Interval: time.Second, Prompt: "x"},
		ExecCommand: func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "echo")
		},
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	ctx := context.Background()
	if err := svc.Stop(ctx); err != nil {
		t.Errorf("Stop before Start should be a no-op, got: %v", err)
	}

	startCtx, startCancel := context.WithCancel(context.Background())
	defer startCancel()
	if err := svc.Start(startCtx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	if err := svc.Stop(stopCtx); err != nil {
		t.Errorf("Stop: %v", err)
	}
}
