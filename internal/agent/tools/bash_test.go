package tools

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
)

func TestBashTool_Definition(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
		},
		Prompts: *config.DefaultPromptsConfig(),
	}

	tool := NewBashTool(cfg, nil)
	def := tool.Definition()

	if def.Function.Name != "Bash" {
		t.Errorf("Expected tool name 'Bash', got %s", def.Function.Name)
	}

	if *def.Function.Description == "" {
		t.Error("Tool description should not be empty")
	}

	if def.Function.Parameters == nil {
		t.Error("Tool parameters should not be nil")
	}

	props, ok := (*def.Function.Parameters)["properties"].(map[string]any)
	if !ok {
		t.Fatal("parameters.properties is not a map")
	}

	if _, ok := props["detached"]; !ok {
		t.Error("Expected 'detached' parameter in tool definition")
	}

	detached, ok := props["detached"].(map[string]any)
	if !ok {
		t.Fatal("detached parameter is not a map")
	}
	if detached["type"] != "boolean" {
		t.Errorf("Expected detached type 'boolean', got %v", detached["type"])
	}
}

func TestBashTool_IsEnabled(t *testing.T) {
	tests := []struct {
		name          string
		toolsEnabled  bool
		expectedState bool
	}{
		{
			name:          "enabled when tools enabled",
			toolsEnabled:  true,
			expectedState: true,
		},
		{
			name:          "disabled when tools disabled",
			toolsEnabled:  false,
			expectedState: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Tools: config.ToolsConfig{
					Enabled: tt.toolsEnabled,
					Bash: config.BashToolConfig{
						Enabled: true,
					},
				},
			}

			tool := NewBashTool(cfg, nil)
			if tool.IsEnabled() != tt.expectedState {
				t.Errorf("Expected IsEnabled() = %v, got %v", tt.expectedState, tool.IsEnabled())
			}
		})
	}
}

func TestBashTool_Validate(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Bash: config.BashToolConfig{
				Enabled: true,
				Mode: config.BashModesConfig{
					All: config.BashModeAllowConfig{Allow: []string{"echo( .*)?", "pwd( .*)?", "git status( .*)?"}},
				},
			},
		},
	}

	tool := NewBashTool(cfg, nil)

	tests := []struct {
		name      string
		args      map[string]any
		wantError bool
	}{
		{
			name: "valid allowed command",
			args: map[string]any{
				"command": "echo hello",
			},
			wantError: false,
		},
		{
			name: "valid pattern command",
			args: map[string]any{
				"command": "git status",
			},
			wantError: false,
		},
		{
			name: "invalid command not allowed",
			args: map[string]any{
				"command": "rm -rf /",
			},
			wantError: true,
		},
		{
			name: "file redirect on allowed command is rejected",
			args: map[string]any{
				"command": "echo hello > /tmp/evil",
			},
			wantError: true,
		},
		{
			name:      "missing command parameter",
			args:      map[string]any{},
			wantError: true,
		},
		{
			name: "command parameter wrong type",
			args: map[string]any{
				"command": 123,
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(tt.args)
			if (err != nil) != tt.wantError {
				t.Errorf("Validate() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestBashTool_Validate_RedirectFeedback(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Bash: config.BashToolConfig{
				Enabled: true,
				Mode: config.BashModesConfig{
					All: config.BashModeAllowConfig{Allow: []string{"echo( .*)?"}},
				},
			},
		},
	}
	tool := NewBashTool(cfg, nil)

	err := tool.Validate(map[string]any{"command": "echo secret > /tmp/evil"})
	if err == nil {
		t.Fatal("expected an error for a file-redirect command")
	}
	if !strings.Contains(err.Error(), "redirection") {
		t.Errorf("expected redirection guidance in error, got %q", err.Error())
	}
}

func TestBashTool_Execute(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Bash: config.BashToolConfig{
				Enabled: true,
				Mode: config.BashModesConfig{
					All: config.BashModeAllowConfig{Allow: []string{"echo( .*)?"}},
				},
			},
		},
	}

	tool := NewBashTool(cfg, nil)
	ctx := context.Background()

	args := map[string]any{
		"command": "echo hello",
	}

	result, err := tool.Execute(ctx, args)
	if err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	if result.ToolName != "Bash" {
		t.Errorf("Expected tool name 'Bash', got %s", result.ToolName)
	}
}

func TestBashTool_Execute_TraceEnv(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Bash: config.BashToolConfig{
				Enabled: true,
				Mode: config.BashModesConfig{
					All: config.BashModeAllowConfig{Allow: []string{"env"}},
				},
			},
		},
	}
	tool := NewBashTool(cfg, nil)

	tests := []struct {
		name     string
		ctx      context.Context
		contains bool
	}{
		{name: "trace env exported", ctx: domain.WithTraceEnv(context.Background(), []string{"TRACEPARENT=00-abc-def-01", "BAGGAGE=infer.session.id=s1"}), contains: true},
		{name: "no trace env without ctx value", ctx: context.Background(), contains: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tool.Execute(tt.ctx, map[string]any{"command": "env"})
			if err != nil {
				t.Fatalf("Execute() failed: %v", err)
			}
			output := result.Data.(*domain.BashToolResult).Output
			if got := strings.Contains(output, "TRACEPARENT=00-abc-def-01"); got != tt.contains {
				t.Errorf("TRACEPARENT in child env=%v, want %v", got, tt.contains)
			}
		})
	}
}

func TestBashTool_Execute_NonZeroExitSurfacesError(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Bash: config.BashToolConfig{
				Enabled: true,
				Mode: config.BashModesConfig{
					All: config.BashModeAllowConfig{Allow: []string{"ls( .*)?"}},
				},
			},
		},
	}

	tool := NewBashTool(cfg, nil)

	result, err := tool.Execute(context.Background(), map[string]any{
		"command": "ls /no/such/path/xyz-12345",
	})
	if err != nil {
		t.Fatalf("Execute() returned a Go error: %v", err)
	}
	if result.Success {
		t.Fatal("expected Success=false for a non-zero exit")
	}
	if result.Error == "" {
		t.Fatal("expected a non-empty result.Error so the model sees why the command failed")
	}
	if !strings.Contains(result.Error, "exit status") {
		t.Errorf("expected result.Error to include the exit status, got %q", result.Error)
	}
	if !strings.Contains(result.Error, "No such") {
		t.Errorf("expected result.Error to include the command's stderr, got %q", result.Error)
	}
}

func TestBashTool_GitPushValidation(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Bash: config.BashToolConfig{
				Enabled: true,
				Mode: config.BashModesConfig{
					All: config.BashModeAllowConfig{Allow: []string{
						"^git push( --set-upstream)?( origin)? (feature|fix|bugfix|hotfix|chore|docs|test|refactor|build|ci|perf|style)/[a-zA-Z0-9/_.-]+$",
						"^git push( --set-upstream)?( origin)? develop$",
						"^git push( --set-upstream)?( origin)? staging$",
						"^git push( --set-upstream)?( origin)? release/[a-zA-Z0-9._-]+$",
					}},
				},
			},
		},
	}

	tool := NewBashTool(cfg, nil)

	tests := []struct {
		name      string
		command   string
		wantError bool
		reason    string
	}{
		{
			name:      "allow push to feature branch",
			command:   "git push origin feature/user-auth",
			wantError: false,
			reason:    "should allow pushing to feature/ branches",
		},
		{
			name:      "allow push with set-upstream to feature branch",
			command:   "git push --set-upstream origin feature/user-auth",
			wantError: false,
			reason:    "should allow pushing with --set-upstream to feature/ branches",
		},
		{
			name:      "block push to main",
			command:   "git push origin main",
			wantError: true,
			reason:    "should block pushing to main branch",
		},
		{
			name:      "block push to master",
			command:   "git push origin master",
			wantError: true,
			reason:    "should block pushing to master branch",
		},
		{
			name:      "block push with set-upstream to main",
			command:   "git push --set-upstream origin main",
			wantError: true,
			reason:    "should block pushing with --set-upstream to main",
		},
		{
			name:      "block push with set-upstream to master",
			command:   "git push --set-upstream origin master",
			wantError: true,
			reason:    "should block pushing with --set-upstream to master",
		},
		{
			name:      "allow push to develop branch",
			command:   "git push origin develop",
			wantError: false,
			reason:    "should allow pushing to develop branch",
		},
		{
			name:      "allow push to release branch",
			command:   "git push origin release/v1.0.0",
			wantError: false,
			reason:    "should allow pushing to release branches",
		},
		{
			name:      "allow push to fix branch",
			command:   "git push origin fix/critical-bug",
			wantError: false,
			reason:    "should allow pushing to fix/ branches",
		},
		{
			name:      "block push to arbitrary branch",
			command:   "git push origin random-branch-name",
			wantError: true,
			reason:    "should block pushing to non-standard branch names",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := map[string]any{
				"command": tt.command,
			}

			err := tool.Validate(args)
			if (err != nil) != tt.wantError {
				t.Errorf("Validate() error = %v, wantError %v, reason: %s", err, tt.wantError, tt.reason)
			}
		})
	}
}

func TestBashTool_StreamingOutput(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Bash: config.BashToolConfig{
				Enabled: true,
				Mode: config.BashModesConfig{
					All: config.BashModeAllowConfig{Allow: []string{"echo( .*)?", "printf( .*)?", "seq( .*)?"}},
				},
			},
		},
	}

	tool := NewBashTool(cfg, nil)

	t.Run("streaming callback receives output", func(t *testing.T) {
		var receivedOutput []string
		var mu sync.Mutex

		callback := func(output string) {
			mu.Lock()
			receivedOutput = append(receivedOutput, output)
			mu.Unlock()
		}

		ctx := context.WithValue(context.Background(), domain.BashOutputCallbackKey, domain.BashOutputCallback(callback))

		args := map[string]any{
			"command": `printf 'line 1\nline 2\nline 3\n'`,
		}

		result, err := tool.Execute(ctx, args)
		if err != nil {
			t.Fatalf("Execute() failed: %v", err)
		}

		if result == nil {
			t.Fatal("Expected non-nil result")
		}

		if !result.Success {
			t.Errorf("Expected successful execution, got error: %s", result.Error)
		}

		mu.Lock()
		callbackCount := len(receivedOutput)
		combinedOutput := strings.Join(receivedOutput, "\n")
		mu.Unlock()

		if callbackCount == 0 {
			t.Errorf("Expected at least 1 callback, got 0")
		}

		if !strings.Contains(combinedOutput, "line 1") ||
			!strings.Contains(combinedOutput, "line 2") ||
			!strings.Contains(combinedOutput, "line 3") {
			t.Errorf("Expected output to contain all 3 lines, got: %s", combinedOutput)
		}
	})

	t.Run("works without streaming callback", func(t *testing.T) {
		ctx := context.Background()

		args := map[string]any{
			"command": "echo hello",
		}

		result, err := tool.Execute(ctx, args)
		if err != nil {
			t.Fatalf("Execute() failed: %v", err)
		}

		if result == nil {
			t.Fatal("Expected non-nil result")
		}

		if !result.Success {
			t.Errorf("Expected successful execution, got error: %s", result.Error)
		}
	})

	t.Run("coalesces large output into bounded callbacks", func(t *testing.T) {
		const lineCount = 5000

		var mu sync.Mutex
		var calls int
		var combined strings.Builder

		callback := func(output string) {
			mu.Lock()
			calls++
			combined.WriteString(output)
			combined.WriteString("\n")
			mu.Unlock()
		}

		ctx := context.WithValue(context.Background(), domain.BashOutputCallbackKey, domain.BashOutputCallback(callback))

		args := map[string]any{
			"command": fmt.Sprintf("seq 1 %d", lineCount),
		}

		result, err := tool.Execute(ctx, args)
		if err != nil {
			t.Fatalf("Execute() failed: %v", err)
		}
		if result == nil || !result.Success {
			t.Fatalf("expected successful execution, got %+v", result)
		}

		mu.Lock()
		gotCalls := calls
		gotCombined := combined.String()
		mu.Unlock()

		if gotCalls == 0 {
			t.Fatal("expected at least one callback")
		}
		if gotCalls >= 200 {
			t.Errorf("expected %d lines to coalesce into far fewer callbacks, got %d", lineCount, gotCalls)
		}

		streamed := strings.Split(strings.TrimRight(gotCombined, "\n"), "\n")
		if len(streamed) != lineCount {
			t.Fatalf("expected %d streamed lines, got %d", lineCount, len(streamed))
		}
		if streamed[0] != "1" || streamed[len(streamed)-1] != fmt.Sprintf("%d", lineCount) {
			t.Errorf("expected streamed lines 1..%d, got first=%q last=%q", lineCount, streamed[0], streamed[len(streamed)-1])
		}

		data, ok := result.Data.(*domain.BashToolResult)
		if !ok {
			t.Fatalf("expected result.Data to be *domain.BashToolResult, got %T", result.Data)
		}
		if got := strings.Count(data.Output, "\n"); got != lineCount {
			t.Errorf("expected captured output to contain %d lines, got %d", lineCount, got)
		}
	})
}

// TestBashTool_Validate_RedirectionAndCompound confirms the tool delegates to
// config.IsBashCommandAllowed: a benign redirection validates, while command
// substitution and any compound or piped command are rejected by the
// single-command policy - even when each segment would be allowed on its own.
func TestBashTool_Validate_RedirectionAndCompound(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Bash: config.BashToolConfig{
				Enabled: true,
				Mode: config.BashModesConfig{
					All: config.BashModeAllowConfig{Allow: []string{"^echo( |$)", `^git log( --oneline)?$`}},
				},
			},
		},
	}
	tool := NewBashTool(cfg, nil)

	tests := []struct {
		command   string
		wantError bool
	}{
		{"git log --oneline 2>&1", false},
		{"echo hi && echo bye", true},
		{"echo hi || echo failed", true},
		{"echo a | echo b", true},
		{"echo $(rm -rf /)", true},
		{"echo hi && rm -rf /", true},
		{"git log --graph", true},
	}
	for _, tt := range tests {
		err := tool.Validate(map[string]any{"command": tt.command})
		if (err != nil) != tt.wantError {
			t.Errorf("Validate(%q) error = %v, wantError %v", tt.command, err, tt.wantError)
		}
	}
}
