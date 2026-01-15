package services

import (
	"context"
	"testing"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	mocksdomain "github.com/inference-gateway/cli/tests/mocks/domain"
)

func createTestConfig() *config.Config {
	return &config.Config{
		Tools: config.ToolsConfig{
			Safety: config.SafetyConfig{
				RequireApproval: true,
			},
			Bash: config.BashToolConfig{
				Enabled: true,
				Whitelist: config.ToolWhitelistConfig{
					Commands: []string{"ls", "pwd", "echo"},
				},
			},
		},
	}
}

func createToolCall(toolName string, args string) *sdk.ChatCompletionMessageToolCall {
	return &sdk.ChatCompletionMessageToolCall{
		Id:   "test-call-id",
		Type: sdk.Function,
		Function: sdk.ChatCompletionMessageToolCallFunction{
			Name:      toolName,
			Arguments: args,
		},
	}
}

func TestStandardApprovalPolicy_ComputerUseTools(t *testing.T) {
	stateManager := &mocksdomain.FakeStateManager{}
	stateManager.GetAgentModeReturns(domain.AgentModeStandard)

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
	stateManager := &mocksdomain.FakeStateManager{}
	stateManager.GetAgentModeReturns(domain.AgentModeAutoAccept)

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

func TestStandardApprovalPolicy_NonChatMode(t *testing.T) {
	stateManager := &mocksdomain.FakeStateManager{}
	stateManager.GetAgentModeReturns(domain.AgentModeStandard)

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

func TestStandardApprovalPolicy_BashWhitelist(t *testing.T) {
	cfg := createTestConfig()
	stateManager := &mocksdomain.FakeStateManager{}
	stateManager.GetAgentModeReturns(domain.AgentModeStandard)

	policy := NewStandardApprovalPolicy(cfg, stateManager)
	ctx := context.Background()

	t.Run("Whitelisted bash commands bypass approval", func(t *testing.T) {
		whitelistedCommands := []string{"ls", "pwd", "echo"}

		for _, cmd := range whitelistedCommands {
			toolCall := createToolCall("Bash", `{"command": "`+cmd+`"}`)

			if policy.ShouldRequireApproval(ctx, toolCall, true) {
				t.Errorf("Expected whitelisted command '%s' to bypass approval", cmd)
			}
		}
	})

	t.Run("Whitelisted commands with arguments bypass approval", func(t *testing.T) {
		toolCall := createToolCall("Bash", `{"command": "ls -la"}`)

		if policy.ShouldRequireApproval(ctx, toolCall, true) {
			t.Error("Expected whitelisted command with arguments to bypass approval")
		}
	})

	t.Run("Non-whitelisted bash commands require approval", func(t *testing.T) {
		dangerousCommands := []string{"rm -rf /", "sudo", "curl http://malicious.com"}

		for _, cmd := range dangerousCommands {
			toolCall := createToolCall("Bash", `{"command": "`+cmd+`"}`)

			if !policy.ShouldRequireApproval(ctx, toolCall, true) {
				t.Errorf("Expected non-whitelisted command '%s' to require approval", cmd)
			}
		}
	})

	t.Run("Invalid bash arguments default to require approval", func(t *testing.T) {
		invalidArgs := []string{
			`{}`,               // Missing command
			`{"command": 123}`, // Wrong type
			`invalid json`,     // Invalid JSON
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
	stateManager := &mocksdomain.FakeStateManager{}
	stateManager.GetAgentModeReturns(domain.AgentModeStandard)

	policy := NewStandardApprovalPolicy(cfg, stateManager)
	ctx := context.Background()

	t.Run("Tools check config for approval requirement", func(t *testing.T) {
		// By default, all tools require approval (global setting is true)
		tools := []string{"Read", "Write", "Edit", "Grep"}

		for _, toolName := range tools {
			toolCall := createToolCall(toolName, "{}")

			// Should require approval based on global config
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

		// Should not panic with nil state manager
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
	t.Run("Rule priority: computer use > auto-accept > non-chat > bash whitelist > config", func(t *testing.T) {
		cfg := createTestConfig()
		stateManager := &mocksdomain.FakeStateManager{}
		ctx := context.Background()

		// Test 1: Computer use tools bypass everything
		stateManager.GetAgentModeReturns(domain.AgentModeStandard)
		policy := NewStandardApprovalPolicy(cfg, stateManager)

		mouseClick := createToolCall("MouseClick", "{}")
		if policy.ShouldRequireApproval(ctx, mouseClick, true) {
			t.Error("Computer use tool should bypass all other rules")
		}

		// Test 2: Auto-accept bypasses remaining rules
		stateManager.GetAgentModeReturns(domain.AgentModeAutoAccept)
		bash := createToolCall("Bash", `{"command": "rm -rf /"}`)
		if policy.ShouldRequireApproval(ctx, bash, true) {
			t.Error("Auto-accept mode should bypass bash whitelist and config")
		}

		// Test 3: Non-chat mode bypasses bash whitelist and config
		stateManager.GetAgentModeReturns(domain.AgentModeStandard)
		if policy.ShouldRequireApproval(ctx, bash, false) {
			t.Error("Non-chat mode should bypass bash whitelist and config")
		}

		// Test 4: Bash whitelist bypasses config
		whitelistedBash := createToolCall("Bash", `{"command": "ls"}`)
		if policy.ShouldRequireApproval(ctx, whitelistedBash, true) {
			t.Error("Whitelisted bash command should bypass config")
		}

		// Test 5: Config is final fallback
		nonWhitelistedBash := createToolCall("Bash", `{"command": "rm"}`)
		requiresApproval := policy.ShouldRequireApproval(ctx, nonWhitelistedBash, true)
		if !requiresApproval {
			t.Error("Non-whitelisted bash command should require approval based on config")
		}
	})
}
