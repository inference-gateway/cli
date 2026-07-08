package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"
	fsnotify "github.com/fsnotify/fsnotify"
)

// testWaitConfig returns a minimal config with Wait tool enabled.
func testWaitConfig() *config.Config {
	cfg := config.DefaultConfig()
	cfg.Tools.Wait.Enabled = true
	cfg.Tools.Wait.MaxTimeoutSeconds = 300
	cfg.Tools.Wait.CommandPollIntervalMs = 100
	cfg.Prompts = *config.DefaultPromptsConfig()
	return cfg
}

func TestWaitTool_Validate(t *testing.T) {
	cfg := testWaitConfig()
	tool := NewWaitTool(cfg, nil)

	tests := []struct {
		name    string
		args    map[string]any
		wantErr string
	}{
		{
			name:    "missing condition",
			args:    map[string]any{},
			wantErr: "condition is required",
		},
		{
			name: "invalid condition",
			args: map[string]any{
				"condition":       "invalid",
				"timeout_seconds": float64(30),
			},
			wantErr: "condition must be one of",
		},
		{
			name: "missing timeout",
			args: map[string]any{
				"condition": "shells",
			},
			wantErr: "timeout_seconds is required",
		},
		{
			name: "timeout exceeds max",
			args: map[string]any{
				"condition":       "shells",
				"timeout_seconds": float64(9999),
			},
			wantErr: "exceeds maximum",
		},
		{
			name: "file missing path",
			args: map[string]any{
				"condition":       "file",
				"timeout_seconds": float64(30),
			},
			wantErr: "path is required when condition is 'file'",
		},
		{
			name: "command missing command",
			args: map[string]any{
				"condition":       "command",
				"timeout_seconds": float64(30),
			},
			wantErr: "command is required when condition is 'command'",
		},
		{
			name: "valid shells",
			args: map[string]any{
				"condition":       "shells",
				"timeout_seconds": float64(30),
			},
			wantErr: "",
		},
		{
			name: "valid file",
			args: map[string]any{
				"condition":       "file",
				"timeout_seconds": float64(30),
				"path":            "/tmp/test.txt",
			},
			wantErr: "",
		},
		{
			name: "valid command",
			args: map[string]any{
				"condition":       "command",
				"timeout_seconds": float64(30),
				"command":          "curl -sf localhost:8080/health",
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(tt.args)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("Validate() unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("Validate() expected error containing %q, got nil", tt.wantErr)
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("Validate() error = %q, want containing %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

func TestWaitTool_Definition(t *testing.T) {
	cfg := testWaitConfig()
	tool := NewWaitTool(cfg, nil)
	def := tool.Definition()

	if def.Function.Name != "Wait" {
		t.Errorf("Definition() name = %q, want %q", def.Function.Name, "Wait")
	}
	if def.Function.Description == nil || *def.Function.Description == "" {
		t.Error("Definition() description is empty")
	}
}

func TestWaitTool_IsEnabled(t *testing.T) {
	cfg := testWaitConfig()
	tool := NewWaitTool(cfg, nil)
	if !tool.IsEnabled() {
		t.Error("IsEnabled() = false, want true")
	}

	cfg.Tools.Wait.Enabled = false
	tool2 := NewWaitTool(cfg, nil)
	if tool2.IsEnabled() {
		t.Error("IsEnabled() = true, want false when disabled")
	}
}

func TestWaitTool_FormatPreview(t *testing.T) {
	cfg := testWaitConfig()
	tool := NewWaitTool(cfg, nil)

	tests := []struct {
		name   string
		result *domain.ToolExecutionResult
		want   string
	}{
		{
			name:   "nil result",
			result: nil,
			want:   "Tool execution result unavailable",
		},
		{
			name: "condition met",
			result: &domain.ToolExecutionResult{
				Success:  true,
				Duration: 5 * time.Second,
				Data: map[string]any{
					"condition": "shells",
					"reason":    "condition_met",
				},
			},
			want: "Wait(shells) condition met after 5.0s",
		},
		{
			name: "timeout",
			result: &domain.ToolExecutionResult{
				Success:  false,
				Duration: 30 * time.Second,
				Error:    "timed out",
				Data: map[string]any{
					"condition": "file",
					"reason":    "timeout",
				},
			},
			want: "Wait failed: timed out",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tool.FormatPreview(tt.result)
			if got != tt.want {
				t.Errorf("FormatPreview() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWaitTool_FormatForLLM(t *testing.T) {
	cfg := testWaitConfig()
	tool := NewWaitTool(cfg, nil)

	result := &domain.ToolExecutionResult{
		Success:  true,
		Duration: 3 * time.Second,
		Data: map[string]any{
			"condition":       "shells",
			"reason":          "condition_met",
			"elapsed_seconds": float64(3.0),
			"shells": []any{
				map[string]any{
					"shell_id":  "shell-1",
					"exit_code": float64(0),
					"output":    "done",
				},
			},
		},
	}

	got := tool.FormatForLLM(result)
	if !strings.Contains(got, "Wait condition: shells") {
		t.Errorf("FormatForLLM missing condition, got: %s", got)
	}
	if !strings.Contains(got, "Outcome: condition_met") {
		t.Errorf("FormatForLLM missing outcome, got: %s", got)
	}
	if !strings.Contains(got, "shell-1: exit 0") {
		t.Errorf("FormatForLLM missing shell info, got: %s", got)
	}
}

func TestEventMatches(t *testing.T) {
	tests := []struct {
		name     string
		op       fsnotify.Op
		eventStr string
		want     bool
	}{
		{"create matches create", fsnotify.Create, "create", true},
		{"create does not match modify", fsnotify.Create, "modify", false},
		{"write matches modify", fsnotify.Write, "modify", true},
		{"chmod matches modify", fsnotify.Chmod, "modify", true},
		{"remove matches remove", fsnotify.Remove, "remove", true},
		{"rename matches remove", fsnotify.Rename, "remove", true},
		{"create matches any", fsnotify.Create, "any", true},
		{"write matches any", fsnotify.Write, "any", true},
		{"remove matches any", fsnotify.Remove, "any", true},
		{"unknown event defaults to true", fsnotify.Create, "unknown", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := eventMatches(tt.op, tt.eventStr)
			if got != tt.want {
				t.Errorf("eventMatches(%v, %q) = %v, want %v", tt.op, tt.eventStr, got, tt.want)
			}
		})
	}
}

func TestWaitTool_Execute_UnknownCondition(t *testing.T) {
	cfg := testWaitConfig()
	tool := NewWaitTool(cfg, nil)

	result, err := tool.Execute(context.Background(), map[string]any{
		"condition":       "unknown",
		"timeout_seconds": float64(5),
	})
	if err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}
	if result.Success {
		t.Error("Execute() should fail for unknown condition")
	}
	if !strings.Contains(result.Error, "unknown condition") {
		t.Errorf("Execute() error = %q, want containing %q", result.Error, "unknown condition")
	}
}

func TestWaitTool_Execute_ShellsNoShells(t *testing.T) {
	cfg := testWaitConfig()
	// No shell service - should return "no_shells"
	tool := NewWaitTool(cfg, nil)

	result, err := tool.Execute(context.Background(), map[string]any{
		"condition":       "shells",
		"timeout_seconds": float64(5),
	})
	if err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("Execute() should succeed with no shells, got error: %s", result.Error)
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		t.Fatalf("Execute() Data is not a map")
	}
	if reason, _ := data["reason"].(string); reason != "no_shells" {
		t.Errorf("Execute() reason = %q, want %q", reason, "no_shells")
	}
}

func TestWaitTool_Execute_FileTimeout(t *testing.T) {
	cfg := testWaitConfig()
	tool := NewWaitTool(cfg, nil)

	// Use a very short timeout so the wait times out quickly
	result, err := tool.Execute(context.Background(), map[string]any{
		"condition":       "file",
		"timeout_seconds": float64(1),
		"path":            "/tmp/nonexistent-wait-test-file-XXXX",
	})
	if err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}
	if result.Success {
		t.Error("Execute() should fail on timeout")
	}
	if !strings.Contains(result.Error, "timed out") {
		t.Errorf("Execute() error = %q, want containing %q", result.Error, "timed out")
	}
}

func TestWaitTool_Execute_CommandTimeout(t *testing.T) {
	cfg := testWaitConfig()
	cfg.Tools.Wait.CommandPollIntervalMs = 50
	tool := NewWaitTool(cfg, nil)

	// Run a command that will never exit 0
	result, err := tool.Execute(context.Background(), map[string]any{
		"condition":       "command",
		"timeout_seconds": float64(1),
		"command":         "false",
	})
	if err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}
	if result.Success {
		t.Error("Execute() should fail on timeout")
	}
	if !strings.Contains(result.Error, "timed out") {
		t.Errorf("Execute() error = %q, want containing %q", result.Error, "timed out")
	}
}

func TestWaitTool_Execute_CommandSuccess(t *testing.T) {
	cfg := testWaitConfig()
	cfg.Tools.Wait.CommandPollIntervalMs = 50
	tool := NewWaitTool(cfg, nil)

	// Run a command that exits 0 immediately
	result, err := tool.Execute(context.Background(), map[string]any{
		"condition":       "command",
		"timeout_seconds": float64(5),
		"command":         "true",
	})
	if err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("Execute() should succeed, got error: %s", result.Error)
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		t.Fatalf("Execute() Data is not a map")
	}
	if reason, _ := data["reason"].(string); reason != "condition_met" {
		t.Errorf("Execute() reason = %q, want %q", reason, "condition_met")
	}
}

func TestWaitTool_Execute_FileEvent(t *testing.T) {
	cfg := testWaitConfig()
	tool := NewWaitTool(cfg, nil)

	// Create a temp dir and file to watch
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test-wait.txt")

	// Start the wait in a goroutine since it blocks
	type waitResult struct {
		result *domain.ToolExecutionResult
		err    error
	}
	done := make(chan waitResult, 1)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		r, e := tool.Execute(ctx, map[string]any{
			"condition":       "file",
			"timeout_seconds": float64(5),
			"path":            testFile,
			"event":           "create",
		})
		done <- waitResult{r, e}
	}()

	// Give the watcher time to start
	time.Sleep(200 * time.Millisecond)

	// Create the file to trigger the event
	if err := os.WriteFile(testFile, []byte("hello"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	select {
	case wr := <-done:
		if wr.err != nil {
			t.Fatalf("Execute() unexpected error: %v", wr.err)
		}
		if !wr.result.Success {
			t.Errorf("Execute() should succeed, got error: %s", wr.result.Error)
		}
		data, ok := wr.result.Data.(map[string]any)
		if !ok {
			t.Fatalf("Execute() Data is not a map")
		}
		if reason, _ := data["reason"].(string); reason != "condition_met" {
			t.Errorf("Execute() reason = %q, want %q", reason, "condition_met")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for file event")
	}
}

func TestWaitTool_Execute_Cancellation(t *testing.T) {
	cfg := testWaitConfig()
	tool := NewWaitTool(cfg, nil)

	ctx, cancel := context.WithCancel(context.Background())

	type waitResult struct {
		result *domain.ToolExecutionResult
		err    error
	}
	done := make(chan waitResult, 1)

	go func() {
		r, e := tool.Execute(ctx, map[string]any{
			"condition":       "file",
			"timeout_seconds": float64(30),
			"path":            "/tmp/nonexistent-cancel-test",
		})
		done <- waitResult{r, e}
	}()

	// Cancel the context to trigger cancellation
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case wr := <-done:
		if wr.err != nil {
			t.Fatalf("Execute() unexpected error: %v", wr.err)
		}
		if wr.result.Success {
			t.Error("Execute() should fail on cancellation")
		}
		if !strings.Contains(wr.result.Error, "cancelled") {
			t.Errorf("Execute() error = %q, want containing %q", wr.result.Error, "cancelled")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for cancellation")
	}
}

func TestWaitTool_Execute_ShellsWithShellService(t *testing.T) {
	cfg := testWaitConfig()
	fake := &domainmocks.FakeBackgroundShellService{}
	exitZero := 0
	fake.GetShellReturns(&domain.BackgroundShell{
		ShellID:  "test-shell-1",
		Command:  "echo hello",
		State:    domain.ShellStateCompleted,
		ExitCode: &exitZero,
	})
	tool := NewWaitTool(cfg, fake)

	result, err := tool.Execute(context.Background(), map[string]any{
		"condition":       "shells",
		"timeout_seconds": float64(5),
		"shell_ids":       []any{"test-shell-1"},
	})
	if err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("Execute() should succeed, got error: %s", result.Error)
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		t.Fatalf("Execute() Data is not a map")
	}
	if reason, _ := data["reason"].(string); reason != "condition_met" {
		t.Errorf("Execute() reason = %q, want %q", reason, "condition_met")
	}
}
