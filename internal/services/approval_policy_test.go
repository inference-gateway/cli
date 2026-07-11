package services

import (
	"context"
	"testing"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
)

func createTestConfig() *config.Config {
	return &config.Config{
		Tools: config.ToolsConfig{
			Safety: config.SafetyConfig{
				RequireApproval: true,
			},
			Bash: config.BashToolConfig{
				Enabled: true,
				Mode: config.BashModesConfig{
					All: config.BashModeAllowConfig{Allow: []string{"ls( .*)?", "pwd( .*)?", "echo( .*)?"}},
				},
			},
		},
	}
}

func createToolCall(toolName string, args string) *sdk.ChatCompletionMessageToolCall {
	return &sdk.ChatCompletionMessageToolCall{
		ID:   "test-call-id",
		Type: sdk.Function,
		Function: sdk.ChatCompletionMessageToolCallFunction{
			Name:      toolName,
			Arguments: args,
		},
	}
}

func TestStandardApprovalPolicy_ComputerUseTools(t *testing.T) {
	stateManager := NewStateManager(false)
	stateManager.SetAgentMode(domain.AgentModeStandard)

	policy := NewStandardApprovalPolicy(createTestConfig(), stateManager)
	ctx := context.Background()

	computerUseTools := []string{
		"MouseMove",
		"MouseClick",
		"MouseScroll",
		"KeyboardType",
		"GetFocusedApp",
		"ActivateApp",
		"GetLatestScreenshot",
	}

	for _, toolName := range computerUseTools {
		t.Run(toolName+" bypasses approval", func(t *testing.T) {
			toolCall := createToolCall(toolName, "{}")

			if policy.ShouldRequireApproval(ctx, toolCall, true) {
				t.Errorf("Expected %s to bypass approval (computer use tool)", toolName)
			}
		})
	}
}

func TestStandardApprovalPolicy_AutoAcceptMode(t *testing.T) {
	stateManager := NewStateManager(false)
	stateManager.SetAgentMode(domain.AgentModeAutoAccept)

	policy := NewStandardApprovalPolicy(createTestConfig(), stateManager)
	ctx := context.Background()

	t.Run("All tools bypass approval in auto-accept mode", func(t *testing.T) {
		tools := []string{"Bash", "Read", "Write", "Edit", "Grep"}

		for _, toolName := range tools {
			toolCall := createToolCall(toolName, `{"command": "rm -rf /"}`)

			if policy.ShouldRequireApproval(ctx, toolCall, true) {
				t.Errorf("Expected %s to bypass approval in auto-accept mode", toolName)
			}
		}
	})
}

func TestStandardApprovalPolicy_ReadOnlyMode(t *testing.T) {
	stateManager := NewStateManager(false)
	stateManager.SetAgentMode(domain.AgentModeReadOnly)

	policy := NewStandardApprovalPolicy(createTestConfig(), stateManager)
	ctx := context.Background()

	t.Run("ReadOnly subagent bypasses approval in chat mode", func(t *testing.T) {
		for _, toolName := range []string{"Read", "Grep", "Tree", "WebFetch", "Write"} {
			toolCall := createToolCall(toolName, `{}`)
			if policy.ShouldRequireApproval(ctx, toolCall, true) {
				t.Errorf("Expected %s to bypass approval in read-only mode", toolName)
			}
		}
	})
}

func TestStandardApprovalPolicy_NonChatMode(t *testing.T) {
	stateManager := NewStateManager(false)
	stateManager.SetAgentMode(domain.AgentModeStandard)

	policy := NewStandardApprovalPolicy(createTestConfig(), stateManager)
	ctx := context.Background()

	t.Run("All tools bypass approval in non-chat mode", func(t *testing.T) {
		tools := []string{"Bash", "Read", "Write", "Edit"}

		for _, toolName := range tools {
			toolCall := createToolCall(toolName, "{}")

			if policy.ShouldRequireApproval(ctx, toolCall, false) {
				t.Errorf("Expected %s to bypass approval in non-chat mode", toolName)
			}
		}
	})
}

func TestStandardApprovalPolicy_BashAllowedList(t *testing.T) {
	cfg := createTestConfig()
	stateManager := NewStateManager(false)
	stateManager.SetAgentMode(domain.AgentModeStandard)

	policy := NewStandardApprovalPolicy(cfg, stateManager)
	ctx := context.Background()

	t.Run("allowed bash commands bypass approval", func(t *testing.T) {
		allowedlistCommands := []string{"ls", "pwd", "echo"}

		for _, cmd := range allowedlistCommands {
			toolCall := createToolCall("Bash", `{"command": "`+cmd+`"}`)

			if policy.ShouldRequireApproval(ctx, toolCall, true) {
				t.Errorf("Expected allowed command '%s' to bypass approval", cmd)
			}
		}
	})

	t.Run("allowed commands with arguments bypass approval", func(t *testing.T) {
		toolCall := createToolCall("Bash", `{"command": "ls -la"}`)

		if policy.ShouldRequireApproval(ctx, toolCall, true) {
			t.Error("expected allowed command with arguments to bypass approval")
		}
	})

	t.Run("disallowed bash commands require approval", func(t *testing.T) {
		dangerousCommands := []string{"rm -rf /", "sudo", "curl http://malicious.com"}

		for _, cmd := range dangerousCommands {
			toolCall := createToolCall("Bash", `{"command": "`+cmd+`"}`)

			if !policy.ShouldRequireApproval(ctx, toolCall, true) {
				t.Errorf("Expected disallowed command '%s' to require approval", cmd)
			}
		}
	})

	t.Run("Invalid bash arguments default to require approval", func(t *testing.T) {
		invalidArgs := []string{
			`{}`,
			`{"command": 123}`,
			`invalid json`,
		}

		for _, args := range invalidArgs {
			toolCall := createToolCall("Bash", args)

			if !policy.ShouldRequireApproval(ctx, toolCall, true) {
				t.Errorf("Expected invalid bash args to require approval: %s", args)
			}
		}
	})
}

func TestStandardApprovalPolicy_ConfigBasedApproval(t *testing.T) {
	cfg := createTestConfig()
	stateManager := NewStateManager(false)
	stateManager.SetAgentMode(domain.AgentModeStandard)

	policy := NewStandardApprovalPolicy(cfg, stateManager)
	ctx := context.Background()

	t.Run("Tools check config for approval requirement", func(t *testing.T) {
		tools := []string{"Read", "Write", "Edit", "Grep"}

		for _, toolName := range tools {
			toolCall := createToolCall(toolName, "{}")

			requiresApproval := policy.ShouldRequireApproval(ctx, toolCall, true)
			configRequiresApproval := cfg.IsApprovalRequired(toolName)

			if requiresApproval != configRequiresApproval {
				t.Errorf("Expected %s approval requirement to match config: policy=%v, config=%v",
					toolName, requiresApproval, configRequiresApproval)
			}
		}
	})
}

func TestStandardApprovalPolicy_WithNilStateManager(t *testing.T) {
	t.Run("Handles nil state manager gracefully", func(t *testing.T) {
		policy := NewStandardApprovalPolicy(createTestConfig(), nil)
		ctx := context.Background()

		toolCall := createToolCall("Read", "{}")
		_ = policy.ShouldRequireApproval(ctx, toolCall, true)
	})
}

func TestPermissiveApprovalPolicy(t *testing.T) {
	policy := NewPermissiveApprovalPolicy()
	ctx := context.Background()

	t.Run("All tools bypass approval", func(t *testing.T) {
		tools := []string{"Bash", "Read", "Write", "Edit", "Grep", "MouseClick"}

		for _, toolName := range tools {
			toolCall := createToolCall(toolName, `{"command": "rm -rf /"}`)

			if policy.ShouldRequireApproval(ctx, toolCall, true) {
				t.Errorf("Expected permissive policy to bypass approval for %s", toolName)
			}
		}
	})

	t.Run("Works in both chat and non-chat modes", func(t *testing.T) {
		toolCall := createToolCall("Bash", `{"command": "dangerous"}`)

		if policy.ShouldRequireApproval(ctx, toolCall, true) {
			t.Error("Expected permissive policy to bypass approval in chat mode")
		}

		if policy.ShouldRequireApproval(ctx, toolCall, false) {
			t.Error("Expected permissive policy to bypass approval in non-chat mode")
		}
	})
}

func TestStrictApprovalPolicy(t *testing.T) {
	policy := NewStrictApprovalPolicy()
	ctx := context.Background()

	t.Run("All tools require approval except computer use", func(t *testing.T) {
		regularTools := []string{"Bash", "Read", "Write", "Edit", "Grep"}

		for _, toolName := range regularTools {
			toolCall := createToolCall(toolName, "{}")

			if !policy.ShouldRequireApproval(ctx, toolCall, true) {
				t.Errorf("Expected strict policy to require approval for %s", toolName)
			}
		}
	})

	t.Run("Computer use tools still bypass approval", func(t *testing.T) {
		computerUseTools := []string{"MouseMove", "MouseClick", "KeyboardType"}

		for _, toolName := range computerUseTools {
			toolCall := createToolCall(toolName, "{}")

			if policy.ShouldRequireApproval(ctx, toolCall, true) {
				t.Errorf("Expected strict policy to bypass approval for computer use tool %s", toolName)
			}
		}
	})

	t.Run("Works in both chat and non-chat modes", func(t *testing.T) {
		toolCall := createToolCall("Bash", `{"command": "ls"}`)

		if !policy.ShouldRequireApproval(ctx, toolCall, true) {
			t.Error("Expected strict policy to require approval in chat mode")
		}

		if !policy.ShouldRequireApproval(ctx, toolCall, false) {
			t.Error("Expected strict policy to require approval in non-chat mode")
		}
	})
}

func TestApprovalPolicy_PriorityOrder(t *testing.T) {
	t.Run("Rule priority: computer use > auto-accept > non-chat > bash allowedlist > config", func(t *testing.T) {
		cfg := createTestConfig()
		stateManager := NewStateManager(false)
		ctx := context.Background()

		stateManager.SetAgentMode(domain.AgentModeStandard)
		policy := NewStandardApprovalPolicy(cfg, stateManager)

		mouseClick := createToolCall("MouseClick", "{}")
		if policy.ShouldRequireApproval(ctx, mouseClick, true) {
			t.Error("Computer use tool should bypass all other rules")
		}

		stateManager.SetAgentMode(domain.AgentModeAutoAccept)
		bash := createToolCall("Bash", `{"command": "rm -rf /"}`)
		if policy.ShouldRequireApproval(ctx, bash, true) {
			t.Error("Auto-accept mode should bypass bash allowedlist and config")
		}

		stateManager.SetAgentMode(domain.AgentModeStandard)
		if policy.ShouldRequireApproval(ctx, bash, false) {
			t.Error("Non-chat mode should bypass bash allowedlist and config")
		}

		allowedBash := createToolCall("Bash", `{"command": "ls"}`)
		if policy.ShouldRequireApproval(ctx, allowedBash, true) {
			t.Error("Allowed bash command should bypass config")
		}

		disallowedBash := createToolCall("Bash", `{"command": "rm"}`)
		requiresApproval := policy.ShouldRequireApproval(ctx, disallowedBash, true)
		if !requiresApproval {
			t.Error("disallowed bash command should require approval based on config")
		}
	})
}
