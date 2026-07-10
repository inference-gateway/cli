package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	fsnotify "github.com/fsnotify/fsnotify"

	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
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
				"command":         "curl -sf localhost:8080/health",
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(tt.args)
			assertValidateError(t, err, tt.wantErr)
		})
	}
}

// assertValidateError checks that err matches the expected error substring.
// An empty wantErr means no error is expected.
func assertValidateError(t *testing.T, err error, wantErr string) {
	t.Helper()
	if wantErr == "" {
		if err != nil {
			t.Errorf("Validate() unexpected error: %v", err)
		}
		return
	}
	if err == nil {
		t.Errorf("Validate() expected error containing %q, got nil", wantErr)
	} else if !strings.Contains(err.Error(), wantErr) {
		t.Errorf("Validate() error = %q, want containing %q", err.Error(), wantErr)
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

func TestWaitTool_FormatForUI(t *testing.T) {
	cfg := testWaitConfig()
	tool := NewWaitTool(cfg, nil)

	tests := []struct {
		name   string
		result *domain.ToolExecutionResult
		checks []string
	}{
		{
			name:   "nil result",
			result: nil,
			checks: []string{"Tool execution result unavailable"},
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
				Arguments: map[string]any{
					"condition":       "shells",
					"timeout_seconds": float64(30),
				},
			},
			checks: []string{"Wait", "shells", "condition met after"},
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
				Arguments: map[string]any{
					"condition":       "file",
					"timeout_seconds": float64(30),
					"path":            "/tmp/test.txt",
				},
			},
			checks: []string{"Wait", "file", "Wait failed: timed out"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tool.FormatForUI(tt.result)
			for _, check := range tt.checks {
				if !strings.Contains(got, check) {
					t.Errorf("FormatForUI() missing %q in output: %s", check, got)
				}
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
		{"unknown event defaults to false", fsnotify.Create, "unknown", false},
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
	if !strings.Contains(result.Error, "condition must be one of") {
		t.Errorf("Execute() error = %q, want containing %q", result.Error, "condition must be one of")
	}
}

func TestWaitTool_Execute_ShellsNoShells(t *testing.T) {
	cfg := testWaitConfig()
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

	result, err := tool.Execute(context.Background(), map[string]any{
		"condition":       "command",
		"timeout_seconds": float64(1),
		"command":         "sleep 10",
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

	result, err := tool.Execute(context.Background(), map[string]any{
		"condition":       "command",
		"timeout_seconds": float64(5),
		"command":         "echo hello",
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

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test-wait.txt")

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

	time.Sleep(200 * time.Millisecond)

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

func TestWaitTool_Validate_PendingExitCodes(t *testing.T) {
	cfg := testWaitConfig()
	tool := NewWaitTool(cfg, nil)

	tests := []struct {
		name    string
		codes   any
		wantErr string
	}{
		{name: "valid codes", codes: []any{float64(8)}, wantErr: ""},
		{name: "not an array", codes: "8", wantErr: "pending_exit_codes must be an array of numbers"},
		{name: "non-numeric entry", codes: []any{"8"}, wantErr: "pending_exit_codes must be an array of numbers"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(map[string]any{
				"condition":          "command",
				"timeout_seconds":    float64(30),
				"command":            "echo hi",
				"pending_exit_codes": tt.codes,
			})
			assertValidateError(t, err, tt.wantErr)
		})
	}
}

func TestWaitTool_Execute_CommandCheckFailed(t *testing.T) {
	cfg := testWaitConfig()
	cfg.Tools.Wait.CommandPollIntervalMs = 50
	tool := NewWaitTool(cfg, nil)

	ctx := domain.WithAgentMode(context.Background(), domain.AgentModeAutoAccept)
	start := time.Now()
	result, err := tool.Execute(ctx, map[string]any{
		"condition":          "command",
		"timeout_seconds":    float64(10),
		"command":            "exit 3",
		"pending_exit_codes": []any{float64(8)},
	})
	if err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Error("Execute() should return immediately on check_failed, not wait for timeout")
	}
	if result.Success {
		t.Error("Execute() should fail on check_failed")
	}
	if !strings.Contains(result.Error, "exit code 3") {
		t.Errorf("Execute() error = %q, want containing %q", result.Error, "exit code 3")
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		t.Fatalf("Execute() Data is not a map")
	}
	if reason, _ := data["reason"].(string); reason != "check_failed" {
		t.Errorf("Execute() reason = %q, want %q", reason, "check_failed")
	}
	if code, _ := data["last_exit_code"].(int); code != 3 {
		t.Errorf("Execute() last_exit_code = %d, want 3", code)
	}
}

func TestWaitTool_Execute_CommandPendingThenSuccess(t *testing.T) {
	cfg := testWaitConfig()
	cfg.Tools.Wait.CommandPollIntervalMs = 50
	tool := NewWaitTool(cfg, nil)

	marker := filepath.Join(t.TempDir(), "done")
	go func() {
		time.Sleep(200 * time.Millisecond)
		_ = os.WriteFile(marker, []byte("ok"), 0o600)
	}()

	ctx := domain.WithAgentMode(context.Background(), domain.AgentModeAutoAccept)
	result, err := tool.Execute(ctx, map[string]any{
		"condition":          "command",
		"timeout_seconds":    float64(10),
		"command":            "test -f " + marker,
		"pending_exit_codes": []any{float64(1)},
	})
	if err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("Execute() should succeed once the check flips to 0, got error: %s", result.Error)
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		t.Fatalf("Execute() Data is not a map")
	}
	if reason, _ := data["reason"].(string); reason != "condition_met" {
		t.Errorf("Execute() reason = %q, want %q", reason, "condition_met")
	}
	if attempts, _ := data["attempts"].(int); attempts < 2 {
		t.Errorf("Execute() attempts = %d, want at least 2", attempts)
	}
}

func TestWaitTool_Execute_CommandModeFromContext(t *testing.T) {
	cfg := testWaitConfig()
	tool := NewWaitTool(cfg, nil)

	result, err := tool.Execute(context.Background(), map[string]any{
		"condition":       "command",
		"timeout_seconds": float64(5),
		"command":         "exit 0",
	})
	if err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}
	data, _ := result.Data.(map[string]any)
	if reason, _ := data["reason"].(string); reason != "not_allowed" {
		t.Errorf("Execute() reason without mode = %q, want %q (standard allow-list)", reason, "not_allowed")
	}

	ctx := domain.WithAgentMode(context.Background(), domain.AgentModeAutoAccept)
	result, err = tool.Execute(ctx, map[string]any{
		"condition":       "command",
		"timeout_seconds": float64(5),
		"command":         "exit 0",
	})
	if err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("Execute() in auto mode should allow any command, got error: %s", result.Error)
	}
}

func TestWaitTool_FormatForLLM_FailureKeepsDetails(t *testing.T) {
	cfg := testWaitConfig()
	tool := NewWaitTool(cfg, nil)

	result := &domain.ToolExecutionResult{
		Success:  false,
		Duration: 2 * time.Second,
		Error:    "check command failed with exit code 1 (not in pending_exit_codes)",
		Data: map[string]any{
			"condition":       "command",
			"reason":          "check_failed",
			"elapsed_seconds": float64(2.0),
			"command":         "gh pr checks",
			"last_exit_code":  1,
			"last_output":     "lint\tfail\t1m2s",
		},
	}

	got := tool.FormatForLLM(result)
	for _, want := range []string{"Error:", "Outcome: check_failed", "Last exit code: 1", "lint\tfail"} {
		if !strings.Contains(got, want) {
			t.Errorf("FormatForLLM missing %q, got: %s", want, got)
		}
	}
}

func TestWaitTool_Validate_PendingExitCodesIncludeZero(t *testing.T) {
	cfg := testWaitConfig()
	tool := NewWaitTool(cfg, nil)

	tests := []struct {
		name    string
		codes   any
		wantErr string
	}{
		{name: "valid codes with 0", codes: []any{float64(0), float64(8)}, wantErr: ""},
		{name: "valid codes without 0", codes: []any{float64(8)}, wantErr: ""},
		{name: "not an array", codes: "0", wantErr: "pending_exit_codes must be an array of numbers"},
		{name: "non-numeric entry", codes: []any{"0"}, wantErr: "pending_exit_codes must be an array of numbers"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(map[string]any{
				"condition":          "command",
				"timeout_seconds":    float64(30),
				"command":            "echo hi",
				"pending_exit_codes": tt.codes,
			})
			assertValidateError(t, err, tt.wantErr)
		})
	}
}

func TestWaitTool_Execute_CommandPendingIncludeZero(t *testing.T) {
	cfg := testWaitConfig()
	cfg.Tools.Wait.CommandPollIntervalMs = 50
	tool := NewWaitTool(cfg, nil)

	ctx := domain.WithAgentMode(context.Background(), domain.AgentModeAutoAccept)
	start := time.Now()
	result, err := tool.Execute(ctx, map[string]any{
		"condition":          "command",
		"timeout_seconds":    float64(1),
		"command":            "exit 0",
		"pending_exit_codes": []any{float64(0)},
	})
	if err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}
	if result.Success {
		t.Error("Execute() should fail on timeout when exit 0 is treated as pending")
	}
	if !strings.Contains(result.Error, "timed out") && !strings.Contains(result.Error, "check command failed") {
		t.Errorf("Execute() error = %q, want containing %q or %q", result.Error, "timed out", "check command failed")
	}
	if time.Since(start) < 500*time.Millisecond {
		t.Error("Execute() returned too quickly - should have polled until timeout")
	}
}
