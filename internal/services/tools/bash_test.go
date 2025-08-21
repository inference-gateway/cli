package tools

import (
	"context"
	"testing"

	"github.com/inference-gateway/cli/config"
)

func TestBashTool_Definition(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
		},
	}

	tool := NewBashTool(cfg)
	def := tool.Definition()

	if def.Name != "Bash" {
		t.Errorf("Expected tool name 'Bash', got %s", def.Name)
	}

	if def.Description == "" {
		t.Error("Tool description should not be empty")
	}

	if def.Parameters == nil {
		t.Error("Tool parameters should not be nil")
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

			tool := NewBashTool(cfg)
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
				Whitelist: config.ToolWhitelistConfig{
					Commands: []string{"echo", "pwd"},
					Patterns: []string{"^git status$"},
				},
			},
		},
	}

	tool := NewBashTool(cfg)

	tests := []struct {
		name      string
		args      map[string]any
		wantError bool
	}{
		{
			name: "valid whitelisted command",
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
			name: "invalid command not whitelisted",
			args: map[string]any{
				"command": "rm -rf /",
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

func TestBashTool_Execute(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Bash: config.BashToolConfig{
				Enabled: true,
				Whitelist: config.ToolWhitelistConfig{
					Commands: []string{"echo"},
				},
			},
		},
	}

	tool := NewBashTool(cfg)
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

func TestBashTool_GitPushValidation(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Bash: config.BashToolConfig{
				Enabled: true,
				Whitelist: config.ToolWhitelistConfig{
					Patterns: []string{
						"^git push( --set-upstream)?( origin)? (feature|fix|bugfix|hotfix|chore|docs|test|refactor|build|ci|perf|style)/[a-zA-Z0-9/_.-]+$",
						"^git push( --set-upstream)?( origin)? develop$",
						"^git push( --set-upstream)?( origin)? staging$",
						"^git push( --set-upstream)?( origin)? release/[a-zA-Z0-9._-]+$",
					},
				},
			},
		},
	}

	tool := NewBashTool(cfg)

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
